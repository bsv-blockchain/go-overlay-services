package identity

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"
	"github.com/bsv-blockchain/go-sdk/script"
	"github.com/bsv-blockchain/go-sdk/transaction"
	"github.com/bsv-blockchain/go-sdk/transaction/template/pushdrop"
	"github.com/bsv-blockchain/go-sdk/wallet"
	"github.com/stretchr/testify/require"
)

func TestProjectOutputTSPropertyEnumeration(t *testing.T) {
	raw := loadTopicCorpus(t).SearchableProfile
	var fixture topicFixture
	require.NoError(t, json.Unmarshal(raw, &fixture))
	var expected struct {
		Fields     map[string]string `json:"expectedFields"`
		Searchable string            `json:"searchableConcatenated"`
	}
	require.NoError(t, json.Unmarshal(raw, &expected))
	beef, txid := readTopicFixture(t, fixture)
	tx := beef.FindTransactionByHash(txid)
	// This fixture is an actual unconfirmed signed spend with a confirmed
	// ancestor. Topic admission here still does not claim graph verification.
	require.Nil(t, tx.MerklePath)
	result, err := NewTopicManager().IdentifyAdmissibleOutputs(t.Context(), beef, txid, nil)
	require.NoError(t, err)
	require.Equal(t, fixture.OutputsToAdmit, result.OutputsToAdmit)
	record, err := ProjectOutput(t.Context(), transaction.Outpoint{Txid: *txid}, tx.Outputs[0].LockingScript, DefaultAdmissionPolicy())
	require.NoError(t, err)
	require.Equal(t, expected.Fields, record.Certificate.Fields)
	require.Equal(t, expected.Searchable, record.SearchableAttributes)
	require.NotContains(t, record.SearchableAttributes, "private-photo")
	require.NotContains(t, record.SearchableAttributes, "avatar-icon")
	// The locking public key is derived and must not be equated to subject.
	decoded := pushdrop.Decode(tx.Outputs[0].LockingScript)
	require.NotEqual(t, record.Certificate.Subject, decoded.LockingPublicKey.ToDERHex())
}

func TestProjectOutputRejectsAlteredPublicRevelation(t *testing.T) {
	fixture := loadTopicCorpus(t).Fixtures[0]
	beef, txid := readTopicFixture(t, fixture)
	decoded := pushdrop.Decode(beef.FindTransactionByHash(txid).Outputs[0].LockingScript)
	for _, test := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{name: "tampered keyring ciphertext", mutate: func(e map[string]any) {
			keyring := e["keyring"].(map[string]any)
			key, err := base64.StdEncoding.DecodeString(keyring["name"].(string))
			require.NoError(t, err)
			key[len(key)-1] ^= 1
			keyring["name"] = base64.StdEncoding.EncodeToString(key)
		}},
		{name: "absent revealed field", mutate: func(e map[string]any) {
			e["keyring"] = map[string]any{"missing": e["keyring"].(map[string]any)["name"]}
		}},
		{name: "invalid type encoding", mutate: func(e map[string]any) { e["type"] = "not base64" }},
		{name: "short serial", mutate: func(e map[string]any) { e["serialNumber"] = "AQ==" }},
		{name: "invalid subject", mutate: func(e map[string]any) { e["subject"] = "03" + strings.Repeat("ff", 32) }},
		{name: "short certifier", mutate: func(e map[string]any) { e["certifier"] = "00" }},
		{name: "outpoint separator", mutate: func(e map[string]any) { e["revocationOutpoint"] = strings.Repeat("01", 32) + "x0" }},
		{name: "outpoint index overflow", mutate: func(e map[string]any) { e["revocationOutpoint"] = strings.Repeat("01", 32) + ".4294967296" }},
		{name: "missing signature", mutate: func(e map[string]any) { delete(e, "signature") }},
		{name: "non-string field", mutate: func(e map[string]any) { e["fields"] = map[string]any{"name": false} }},
		{name: "null keyring", mutate: func(e map[string]any) { e["keyring"] = nil }},
	} {
		t.Run(test.name, func(t *testing.T) {
			var envelope map[string]any
			require.NoError(t, json.Unmarshal(decoded.Fields[0], &envelope))
			test.mutate(envelope)
			encoded, err := json.Marshal(envelope)
			require.NoError(t, err)
			lockingScript := signedEnvelope(t, encoded, wallet.SecurityLevelEveryApp)
			_, err = ProjectOutput(t.Context(), transaction.Outpoint{Txid: *txid}, lockingScript, DefaultAdmissionPolicy())
			require.Error(t, err)
		})
	}
	wrongProtocol := signedEnvelope(t, decoded.Fields[0], wallet.SecurityLevelEveryAppAndCounterparty)
	_, err := ProjectOutput(t.Context(), transaction.Outpoint{}, wrongProtocol, DefaultAdmissionPolicy())
	require.ErrorIs(t, err, ErrInvalidOutput)
}

func signedEnvelope(t *testing.T, raw []byte, level wallet.SecurityLevel) *script.Script {
	t.Helper()
	// Deterministic public test key 11 matches the TS corpus subject.
	key, _ := ec.PrivateKeyFromBytes([]byte{11})
	subject, err := wallet.NewCompletedProtoWallet(key)
	require.NoError(t, err)
	lockingScript, err := (&pushdrop.PushDrop{Wallet: subject}).Lock(t.Context(), [][]byte{raw}, wallet.Protocol{SecurityLevel: level, Protocol: "identity"}, "1", wallet.Counterparty{Type: wallet.CounterpartyTypeAnyone}, true, true, pushdrop.LockBefore)
	require.NoError(t, err)
	return lockingScript
}

func TestProjectOutputRejectsBudgetsAndMalformedScripts(t *testing.T) {
	_, err := ProjectOutput(t.Context(), transaction.Outpoint{}, nil, DefaultAdmissionPolicy())
	require.ErrorIs(t, err, ErrInvalidOutput)
	_, err = ProjectOutput(t.Context(), transaction.Outpoint{}, nil, AdmissionPolicy{})
	require.ErrorIs(t, err, ErrInvalidPolicy)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = ProjectOutput(ctx, transaction.Outpoint{}, nil, DefaultAdmissionPolicy())
	require.ErrorIs(t, err, context.Canceled)
	corpus := loadTopicCorpus(t)
	var searchable topicFixture
	require.NoError(t, json.Unmarshal(corpus.SearchableProfile, &searchable))
	beef, txid := readTopicFixture(t, searchable)
	policy := DefaultAdmissionPolicy()
	policy.MaxFields = 1
	_, err = ProjectOutput(t.Context(), transaction.Outpoint{}, beef.FindTransactionByHash(txid).Outputs[0].LockingScript, policy)
	require.ErrorIs(t, err, ErrAdmissionBudget)
	for _, raw := range [][]byte{[]byte("null"), []byte(`{"fields":{"a":null}}`), {0xff}, []byte(`{"keyring":{"a":"x"},"fields":[]}`)} {
		lockingScript := signedEnvelope(t, raw, wallet.SecurityLevelEveryApp)
		_, err = ProjectOutput(t.Context(), transaction.Outpoint{}, lockingScript, DefaultAdmissionPolicy())
		require.Error(t, err)
	}
}
