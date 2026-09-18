package identity

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/bsv-blockchain/go-sdk/auth/certificates"
	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"
	"github.com/bsv-blockchain/go-sdk/script"
	"github.com/bsv-blockchain/go-sdk/transaction"
	"github.com/bsv-blockchain/go-sdk/transaction/template/pushdrop"
	"github.com/bsv-blockchain/go-sdk/wallet"
	"golang.org/x/text/encoding/unicode"
)

type certificateEnvelope struct {
	Type               string        `json:"type"`
	SerialNumber       string        `json:"serialNumber"`
	Subject            string        `json:"subject"`
	Certifier          string        `json:"certifier"`
	RevocationOutpoint string        `json:"revocationOutpoint"`
	Fields             orderedFields `json:"fields"`
	Keyring            orderedFields `json:"keyring"`
	Signature          string        `json:"signature"`
}

// UnmarshalJSON deliberately avoids encoding/json's case-insensitive struct
// matching. TS reads exact property names: an unknown TYPE must not overwrite
// type, and SUBJECT cannot stand in for a missing subject. Map decoding retains
// JSON's last-value behavior for repeated keys without changing signed bytes.
func (e *certificateEnvelope) UnmarshalJSON(data []byte) error {
	var properties map[string]json.RawMessage
	if err := json.Unmarshal(data, &properties); err != nil {
		return err
	}
	if properties == nil {
		return ErrInvalidOutput
	}
	for _, property := range []struct {
		name  string
		value *string
	}{
		{"type", &e.Type},
		{"serialNumber", &e.SerialNumber},
		{"subject", &e.Subject},
		{"certifier", &e.Certifier},
		{"revocationOutpoint", &e.RevocationOutpoint},
		{"signature", &e.Signature},
	} {
		raw, exists := properties[property.name]
		trimmed := bytes.TrimSpace(raw)
		if !exists || len(trimmed) == 0 || trimmed[0] != '"' {
			return fmt.Errorf("%w: certificate %s must be a string", ErrInvalidOutput, property.name)
		}
		if err := json.Unmarshal(raw, property.value); err != nil {
			return err
		}
	}
	if err := json.Unmarshal(properties["fields"], &e.Fields); err != nil {
		return err
	}
	return json.Unmarshal(properties["keyring"], &e.Keyring)
}

// orderedFields preserves JS property enumeration for the searchable text
// concatenation, including numeric property names and duplicate-key last values.
type orderedFields struct {
	values map[string]string
	keys   []string
}

func (f *orderedFields) UnmarshalJSON(data []byte) error {
	d := json.NewDecoder(bytes.NewReader(data))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return fmt.Errorf("%w: fields must be an object", ErrInvalidOutput)
	}
	f.values = make(map[string]string)
	f.keys = nil
	for d.More() {
		token, err = d.Token()
		if err != nil {
			return err
		}
		key, ok := token.(string)
		if !ok {
			return ErrInvalidOutput
		}
		var value json.RawMessage
		if err = d.Decode(&value); err != nil {
			return err
		}
		if len(value) == 0 || value[0] != '"' {
			return fmt.Errorf("%w: field values must be strings", ErrInvalidOutput)
		}
		var decoded string
		if err = json.Unmarshal(value, &decoded); err != nil {
			return err
		}
		if _, exists := f.values[key]; !exists {
			f.keys = append(f.keys, key)
		}
		f.values[key] = decoded
	}
	if _, err = d.Token(); err != nil {
		return err
	}
	if _, err = d.Token(); !errors.Is(err, io.EOF) {
		return ErrInvalidOutput
	}
	// ECMAScript enumerates canonical uint32 property names (except 2^32-1)
	// first, numerically. Stable sort retains insertion order of other names.
	sort.SliceStable(f.keys, func(i, j int) bool {
		a, aOK := propertyIndex(f.keys[i])
		b, bOK := propertyIndex(f.keys[j])
		return aOK && (!bOK || a < b)
	})
	return nil
}

func propertyIndex(key string) (uint64, bool) {
	n, err := strconv.ParseUint(key, 10, 32)
	return n, err == nil && n < 1<<32-1 && strconv.FormatUint(n, 10) == key
}

// ProjectOutput validates an identity lockingScript and derives its public
// projection at outpoint. It performs no storage writes. Callers must first
// independently verify the containing transaction and select the actual output.
// ctx cancels application work; policy bounds parsing and cryptographic work.
// The returned record is suitable for an atomic admission/projection intent.
func ProjectOutput(ctx context.Context, outpoint transaction.Outpoint, lockingScript *script.Script, policy AdmissionPolicy) (Record, error) {
	if err := policy.validate(); err != nil {
		return Record{}, err
	}
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	decoded, envelope, err := decodeCertificateEnvelope(lockingScript, policy)
	if err != nil {
		return Record{}, err
	}
	certificate, err := envelope.certificate(policy)
	if err != nil {
		return Record{}, err
	}
	// A nil root key is the SDK's public "anyone" key. The completed wrapper
	// satisfies DecryptFields' wallet.Interface; only crypto methods are used.
	anyone, err := wallet.NewCompletedProtoWallet(nil)
	if err != nil {
		return Record{}, err
	}
	if err = verifyEnvelopeSignature(ctx, anyone, decoded, certificate); err != nil {
		return Record{}, err
	}
	// The certificate subject uses the derived-key identity protocol. It need
	// not equal the raw PushDrop locking key; the envelope is verified above.
	if err = certificate.Verify(ctx); err != nil {
		return Record{}, fmt.Errorf("%w: %w", ErrCertificateVerification, err)
	}
	if err = ctx.Err(); err != nil {
		return Record{}, err
	}
	publicFields, err := decryptPublicFields(ctx, anyone, certificate)
	if err != nil {
		return Record{}, err
	}
	if err = ctx.Err(); err != nil {
		return Record{}, err
	}
	return Record{
		Outpoint: outpoint,
		Certificate: Certificate{
			Type: envelope.Type, SerialNumber: envelope.SerialNumber,
			Subject: envelope.Subject, Certifier: envelope.Certifier,
			RevocationOutpoint: envelope.RevocationOutpoint, Fields: publicFields,
		},
		SearchableAttributes: searchableAttributes(envelope.Keyring.keys, publicFields),
	}, nil
}

func decodeCertificateEnvelope(lockingScript *script.Script, policy AdmissionPolicy) (*pushdrop.PushDropData, certificateEnvelope, error) {
	if lockingScript == nil || len(*lockingScript) == 0 {
		return nil, certificateEnvelope{}, ErrInvalidOutput
	}
	if len(*lockingScript) > policy.MaxScriptBytes {
		return nil, certificateEnvelope{}, ErrAdmissionBudget
	}
	decoded := pushdrop.Decode(lockingScript)
	if decoded == nil || len(decoded.Fields) < 2 {
		return nil, certificateEnvelope{}, ErrInvalidOutput
	}
	if len(decoded.Fields)-1 > policy.MaxFields {
		return nil, certificateEnvelope{}, ErrAdmissionBudget
	}
	var envelope certificateEnvelope
	// TS parses with TextDecoder: strip an initial BOM and replace ill-formed
	// UTF-8. Signature verification still uses the original field bytes.
	envelopeJSON, err := unicode.UTF8BOM.NewDecoder().Bytes(decoded.Fields[0])
	if err != nil {
		return nil, certificateEnvelope{}, fmt.Errorf("%w: UTF-8 envelope: %w", ErrInvalidOutput, err)
	}
	if err = json.Unmarshal(envelopeJSON, &envelope); err != nil {
		return nil, certificateEnvelope{}, fmt.Errorf("%w: certificate envelope: %w", ErrInvalidOutput, err)
	}
	return decoded, envelope, nil
}

func verifyEnvelopeSignature(ctx context.Context, anyone *wallet.CompletedProtoWallet, decoded *pushdrop.PushDropData, certificate *certificates.VerifiableCertificate) error {
	signature, err := ec.ParseSignature(decoded.Fields[len(decoded.Fields)-1])
	if err != nil {
		return fmt.Errorf("%w: envelope signature: %w", ErrInvalidOutput, err)
	}
	verification, err := anyone.VerifySignature(ctx, wallet.VerifySignatureArgs{
		EncryptionArgs: wallet.EncryptionArgs{
			ProtocolID:   wallet.Protocol{SecurityLevel: wallet.SecurityLevelEveryApp, Protocol: "identity"},
			KeyID:        "1",
			Counterparty: wallet.Counterparty{Type: wallet.CounterpartyTypeOther, Counterparty: &certificate.Subject},
		},
		Data:      bytes.Join(decoded.Fields[:len(decoded.Fields)-1], nil),
		Signature: signature,
	}, "")
	if err != nil {
		return fmt.Errorf("%w: envelope verification: %w", ErrInvalidOutput, err)
	}
	if verification == nil || !verification.Valid {
		return fmt.Errorf("%w: envelope signature", ErrInvalidOutput)
	}
	return nil
}

func decryptPublicFields(ctx context.Context, anyone *wallet.CompletedProtoWallet, certificate *certificates.VerifiableCertificate) (map[string]string, error) {
	publicFields, err := certificate.DecryptFields(ctx, anyone, false, "")
	if err != nil {
		return nil, fmt.Errorf("%w: public field decryption: %w", ErrInvalidOutput, err)
	}
	if len(publicFields) == 0 {
		return nil, fmt.Errorf("%w: no public attributes", ErrInvalidOutput)
	}
	// This is text decoding of authenticated plaintext, not normalization of
	// the signed certificate field names or encrypted values.
	for key, value := range publicFields {
		publicFields[key], err = unicode.UTF8BOM.NewDecoder().String(value)
		if err != nil {
			return nil, fmt.Errorf("%w: UTF-8 public field: %w", ErrInvalidOutput, err)
		}
	}
	return publicFields, nil
}

func searchableAttributes(keys []string, publicFields map[string]string) string {
	searchable := make([]string, 0, len(publicFields))
	for _, key := range keys {
		if key != "profilePhoto" && key != "icon" {
			searchable = append(searchable, publicFields[key])
		}
	}
	return strings.Join(searchable, " ")
}

func (e *certificateEnvelope) certificate(policy AdmissionPolicy) (*certificates.VerifiableCertificate, error) {
	if len(e.Fields.values) > policy.MaxFields || len(e.Keyring.values) > policy.MaxFields {
		return nil, ErrAdmissionBudget
	}
	for _, value := range []string{e.Type, e.SerialNumber} {
		decoded, err := base64.StdEncoding.DecodeString(value)
		if err != nil || len(decoded) != 32 {
			return nil, fmt.Errorf("%w: type and serial must encode 32 bytes", ErrInvalidOutput)
		}
	}
	subject, err := parsePublicKey(e.Subject)
	if err != nil {
		return nil, err
	}
	certifier, err := parsePublicKey(e.Certifier)
	if err != nil {
		return nil, err
	}
	if len(e.RevocationOutpoint) < 66 || e.RevocationOutpoint[64] != '.' {
		return nil, fmt.Errorf("%w: revocation outpoint", ErrInvalidOutput)
	}
	outpoint, err := transaction.OutpointFromString(e.RevocationOutpoint)
	if err != nil {
		return nil, fmt.Errorf("%w: revocation outpoint: %w", ErrInvalidOutput, err)
	}
	signature, err := hex.DecodeString(e.Signature)
	if err != nil || len(signature) == 0 {
		return nil, fmt.Errorf("%w: certificate signature", ErrInvalidOutput)
	}
	fields := make(map[wallet.CertificateFieldNameUnder50Bytes]wallet.StringBase64, len(e.Fields.values))
	for key, value := range e.Fields.values {
		fields[wallet.CertificateFieldNameUnder50Bytes(key)] = wallet.StringBase64(value)
	}
	keyring := make(map[wallet.CertificateFieldNameUnder50Bytes]wallet.StringBase64, len(e.Keyring.values))
	for key, value := range e.Keyring.values {
		keyring[wallet.CertificateFieldNameUnder50Bytes(key)] = wallet.StringBase64(value)
	}
	return certificates.NewVerifiableCertificate(certificates.NewCertificate(
		wallet.StringBase64(e.Type), wallet.StringBase64(e.SerialNumber), *subject, *certifier,
		outpoint, fields, signature,
	), keyring), nil
}

func parsePublicKey(value string) (*ec.PublicKey, error) {
	if len(value) != 66 {
		return nil, fmt.Errorf("%w: compressed public key required", ErrInvalidOutput)
	}
	key, err := ec.PublicKeyFromString(value)
	if err != nil {
		return nil, fmt.Errorf("%w: public key: %w", ErrInvalidOutput, err)
	}
	return key, nil
}
