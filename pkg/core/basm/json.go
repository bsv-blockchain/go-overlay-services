package basm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"unicode/utf8"
)

// DecodeAnchorJSON reads one normative BRC-136 JSON anchor within limits.
// Required fields cannot be missing/null; hashes must be lowercase display hex;
// numbers must be unsigned decimal integers. Unknown fields (such as the TS tac
// extension) are ignored, but duplicate fields in the parsed object and trailing values are rejected.
// Input bytes are already buffered: future transports must also cap body reads.
// Success establishes shape only. It does not authenticate any peer claim.
func DecodeAnchorJSON(data []byte, limits Limits) (TopicBlockAnchor, error) {
	dec, err := boundedDecoder(data, limits)
	if err != nil {
		return TopicBlockAnchor{}, err
	}
	fields, err := readObject(dec)
	if err != nil {
		return TopicBlockAnchor{}, err
	}
	if err = requireEOF(dec); err != nil {
		return TopicBlockAnchor{}, err
	}
	var a TopicBlockAnchor
	if err = json.Unmarshal(fields["topic"], &a.Topic); err != nil {
		return TopicBlockAnchor{}, fmt.Errorf("topic: %w", err)
	}
	height, err := parseUint(fields["blockHeight"], 32)
	if err != nil {
		return TopicBlockAnchor{}, fmt.Errorf("blockHeight: %w", err)
	}
	a.BlockHeight = uint32(height) //nolint:gosec // parseUint enforces 32 bits
	if a.BlockHash, err = parseJSONHash(fields["blockHash"]); err != nil {
		return TopicBlockAnchor{}, fmt.Errorf("blockHash: %w", err)
	}
	if a.BASMRoot, err = parseJSONHash(fields["basmRoot"]); err != nil {
		return TopicBlockAnchor{}, fmt.Errorf("basmRoot: %w", err)
	}
	if a.AdmittedCount, err = parseUint(fields["admittedCount"], 64); err != nil {
		return TopicBlockAnchor{}, fmt.Errorf("admittedCount: %w", err)
	}
	if err := a.Validate(limits); err != nil {
		return TopicBlockAnchor{}, err
	}
	return a, nil
}

// DecodeAdmittedListJSON reads a bare ordered array of {txid, blockIndex} values
// within limits and validates it against caller-supplied blockTxCount. It returns
// no partial list on error. ctx bounds cooperative parsing and hashing work.
// This is an array primitive, not the full TS HTTP response envelope. Callers
// must bind that envelope's topic/height/hash and independently prove positions.
func DecodeAdmittedListJSON(ctx context.Context, data []byte, blockTxCount uint64, limits Limits) ([]AdmittedTxRef, error) {
	if ctx == nil {
		return nil, fmt.Errorf("%w: context must not be nil", ErrInvalidInput)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dec, err := boundedDecoder(data, limits)
	if err != nil {
		return nil, err
	}
	token, err := dec.Token()
	if err != nil || token != json.Delim('[') {
		return nil, fmt.Errorf("%w: admitted list must be a JSON array", ErrInvalidInput)
	}
	admitted, err := readAdmittedArray(ctx, dec, limits)
	if err != nil {
		return nil, err
	}
	if _, err = dec.Token(); err != nil {
		return nil, fmt.Errorf("admitted list closing delimiter: %w", err)
	}
	if err = requireEOF(dec); err != nil {
		return nil, err
	}
	if _, err := ValidateAdmittedList(ctx, admitted, blockTxCount, limits); err != nil {
		return nil, err
	}
	return admitted, nil
}

func readAdmittedArray(ctx context.Context, dec *json.Decoder, limits Limits) ([]AdmittedTxRef, error) {
	admitted := make([]AdmittedTxRef, 0)
	for dec.More() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if uint64(len(admitted)) >= uint64(limits.MaxAdmitted) {
			return nil, fmt.Errorf("%w: admitted list exceeds local limit", ErrLimitExceeded)
		}
		ref, err := readAdmittedItem(dec)
		if err != nil {
			return nil, err
		}
		admitted = append(admitted, ref)
	}
	return admitted, nil
}

func readAdmittedItem(dec *json.Decoder) (AdmittedTxRef, error) {
	fields, err := readObject(dec)
	if err != nil {
		return AdmittedTxRef{}, err
	}
	txid, err := parseJSONHash(fields["txid"])
	if err != nil {
		return AdmittedTxRef{}, fmt.Errorf("txid: %w", err)
	}
	index, err := parseUint(fields["blockIndex"], 64)
	if err != nil || index > MaxSafeJSONInteger {
		return AdmittedTxRef{}, fmt.Errorf("%w: blockIndex must be an unsigned safe JSON integer", ErrInvalidInput)
	}
	return AdmittedTxRef{TxID: txid, BlockIndex: index}, nil
}

// DecodeRangeJSON reads the existing TS {fromHeight,toHeight} request value and
// validates its inclusive size against limits.MaxRange. uint32 height overflow,
// null/missing/fractional values, duplicate keys, and trailing data are rejected.
func DecodeRangeJSON(data []byte, limits Limits) (fromHeight, toHeight uint32, err error) {
	dec, err := boundedDecoder(data, limits)
	if err != nil {
		return 0, 0, err
	}
	fields, err := readObject(dec)
	if err != nil {
		return 0, 0, err
	}
	if err = requireEOF(dec); err != nil {
		return 0, 0, err
	}
	from, err := parseUint(fields["fromHeight"], 32)
	if err != nil {
		return 0, 0, fmt.Errorf("fromHeight: %w", err)
	}
	to, err := parseUint(fields["toHeight"], 32)
	if err != nil {
		return 0, 0, fmt.Errorf("toHeight: %w", err)
	}
	fromHeight, toHeight = uint32(from), uint32(to) //nolint:gosec // parseUint enforces 32 bits
	if _, err := ValidateRange(fromHeight, toHeight, limits.MaxRange); err != nil {
		return 0, 0, err
	}
	return fromHeight, toHeight, nil
}

func boundedDecoder(data []byte, limits Limits) (*json.Decoder, error) {
	if err := limits.validate(); err != nil {
		return nil, err
	}
	if uint64(len(data)) > uint64(limits.MaxJSONBytes) {
		return nil, fmt.Errorf("%w: JSON byte count", ErrLimitExceeded)
	}
	if !utf8.Valid(data) || !validUnicodeEscapes(data) {
		return nil, fmt.Errorf("%w: JSON must be valid UTF-8 with paired Unicode surrogates", ErrInvalidInput)
	}
	return json.NewDecoder(bytes.NewReader(data)), nil
}

// encoding/json replaces lone UTF-16 surrogates with U+FFFD. Reject them before
// decoding so topic names and object keys cannot silently change identity.
// Other JSON syntax errors remain the decoder's responsibility.
func validUnicodeEscapes(data []byte) bool {
	inString := false
	for i := 0; i < len(data); i++ {
		if data[i] == '"' {
			inString = !inString
			continue
		}
		if !inString || data[i] != '\\' {
			continue
		}
		i++
		if i >= len(data) || data[i] != 'u' {
			continue
		}
		next, ok := consumeUnicodeEscape(data, i)
		if !ok {
			return false
		}
		i = next
	}
	return true
}

func consumeUnicodeEscape(data []byte, uIndex int) (int, bool) {
	if uIndex+4 >= len(data) {
		return 0, false
	}
	unit, err := strconv.ParseUint(string(data[uIndex+1:uIndex+5]), 16, 16)
	if err != nil {
		return 0, false
	}
	i := uIndex + 4
	if unit >= 0xdc00 && unit <= 0xdfff {
		return 0, false
	}
	if unit < 0xd800 || unit > 0xdbff {
		return i, true
	}
	if i+6 >= len(data) || data[i+1] != '\\' || data[i+2] != 'u' {
		return 0, false
	}
	low, err := strconv.ParseUint(string(data[i+3:i+7]), 16, 16)
	if err != nil || low < 0xdc00 || low > 0xdfff {
		return 0, false
	}
	return i + 6, true
}

func readObject(dec *json.Decoder) (map[string]json.RawMessage, error) {
	token, err := dec.Token()
	if err != nil || token != json.Delim('{') {
		return nil, fmt.Errorf("%w: expected JSON object", ErrInvalidInput)
	}
	fields := make(map[string]json.RawMessage)
	for dec.More() {
		// Bound metadata expansion even for a byte-bounded object of tiny keys.
		if len(fields) >= 64 {
			return nil, fmt.Errorf("%w: JSON object exceeds 64 fields", ErrLimitExceeded)
		}
		token, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("JSON object key: %w", err)
		}
		key, ok := token.(string)
		if !ok {
			return nil, fmt.Errorf("%w: JSON object key must be a string", ErrInvalidInput)
		}
		if _, exists := fields[key]; exists {
			return nil, fmt.Errorf("%w: duplicate JSON object field", ErrInvalidInput)
		}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil, fmt.Errorf("JSON object value: %w", err)
		}
		fields[key] = value
	}
	if _, err := dec.Token(); err != nil {
		return nil, fmt.Errorf("JSON object closing delimiter: %w", err)
	}
	return fields, nil
}

func requireEOF(dec *json.Decoder) error {
	var trailing json.RawMessage
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: trailing JSON data", ErrInvalidInput)
	}
	return nil
}

func parseUint(raw json.RawMessage, bits int) (uint64, error) {
	if len(raw) == 0 {
		return 0, fmt.Errorf("%w: missing unsigned integer", ErrInvalidInput)
	}
	for _, b := range raw {
		if b < '0' || b > '9' {
			return 0, fmt.Errorf("%w: expected unsigned decimal integer", ErrInvalidInput)
		}
	}
	value, err := strconv.ParseUint(string(raw), 10, bits)
	if err != nil {
		return 0, fmt.Errorf("unsigned integer overflow: %w", err)
	}
	return value, nil
}

func parseJSONHash(raw json.RawMessage) (Hash, error) {
	var display string
	if err := json.Unmarshal(raw, &display); err != nil {
		return Hash{}, fmt.Errorf("hash string: %w", err)
	}
	return ParseHash(display)
}
