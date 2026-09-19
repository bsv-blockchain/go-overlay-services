package basm

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	basmJSONGenesisTxID = "4a5e1e4baab89f3a32518a88c31bc87f618f76673e2cc77ab2127b7afdeda33b"
	basmJSONZeroHash    = "0000000000000000000000000000000000000000000000000000000000000000"
)

func TestDecodeAnchorJSONAcceptsExtensionsAndPairedUnicodeEscapes(t *testing.T) {
	data := []byte(`{"\u0074opic":"café\ud83c\udf0d","blockHeight":4294967295,"blockHash":"` + basmJSONGenesisTxID + `","basmRoot":"` + basmJSONZeroHash + `","admittedCount":0,"extension":{"emoji":"\ud83d\ude00","items":[1,2,3]}}`)
	original := bytes.Clone(data)

	anchor, err := DecodeAnchorJSON(data, DefaultLimits())

	require.NoError(t, err)
	assert.Equal(t, "café🌍", anchor.Topic)
	assert.Equal(t, ^uint32(0), anchor.BlockHeight)
	assert.Equal(t, uint64(0), anchor.AdmittedCount)
	assert.Equal(t, original, data)
}

func TestDecodeAnchorJSONRejectsUnpairedSurrogates(t *testing.T) {
	for _, surrogate := range []string{`\ud800`, `\udc00`} {
		t.Run(surrogate, func(t *testing.T) {
			data := strings.Replace(basmValidAnchorJSON("0", "0"), `"topic":"topic"`, `"topic":"`+surrogate+`"`, 1)

			_, err := DecodeAnchorJSON([]byte(data), DefaultLimits())
			require.Error(t, err)
		})
	}
}

func TestDecodeAnchorJSONRejectsRequiredFieldAndHashFailures(t *testing.T) {
	valid := basmValidAnchorJSON("0", "0")
	tests := []struct {
		name string
		data string
	}{
		{
			name: "missing required fields",
			data: `{}`,
		},
		{
			name: "null topic",
			data: strings.Replace(valid, `"topic":"topic"`, `"topic":null`, 1),
		},
		{
			name: "null block hash",
			data: strings.Replace(valid, `"blockHash":"`+basmJSONGenesisTxID+`"`, `"blockHash":null`, 1),
		},
		{
			name: "uppercase block hash",
			data: strings.Replace(valid, basmJSONGenesisTxID, strings.ToUpper(basmJSONGenesisTxID), 1),
		},
		{
			name: "malformed root hash",
			data: strings.Replace(valid, basmJSONZeroHash, strings.Repeat("g", 64), 1),
		},
		{
			name: "escaped duplicate required key",
			data: strings.Replace(valid, "{", `{"\u0074opic":"other",`, 1),
		},
		{
			name: "trailing JSON value",
			data: valid + ` {}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodeAnchorJSON([]byte(tt.data), DefaultLimits())
			require.Error(t, err)
		})
	}
}

func TestDecodeAnchorJSONRejectsNonIntegerAndOutOfRangeNumbers(t *testing.T) {
	for _, field := range []string{"blockHeight", "admittedCount"} {
		for _, number := range []string{"1.0", "1e0", "-1", "-0"} {
			t.Run(field+" "+number, func(t *testing.T) {
				data := basmValidAnchorJSON("0", "0")
				data = strings.Replace(data, `"`+field+`":0`, `"`+field+`":`+number, 1)

				_, err := DecodeAnchorJSON([]byte(data), DefaultLimits())
				require.Error(t, err)
			})
		}
	}

	tooLargeHeight := basmValidAnchorJSON("4294967296", "0")
	_, err := DecodeAnchorJSON([]byte(tooLargeHeight), DefaultLimits())
	require.Error(t, err)

	tooLargeUint64 := basmValidAnchorJSON("0", "18446744073709551616")
	_, err = DecodeAnchorJSON([]byte(tooLargeUint64), DefaultLimits())
	require.Error(t, err)
}

func TestDecodeAnchorJSONRejectsByteAndFieldBoundsAndInvalidUTF8(t *testing.T) {
	valid := []byte(basmValidAnchorJSON("0", "0"))

	maxJSONBytes := uint32(len(valid) - 1) //nolint:gosec // the test fixture is far below uint32's maximum
	_, err := DecodeAnchorJSON(valid, Limits{MaxAdmitted: 1, MaxRange: 1, MaxJSONBytes: maxJSONBytes, MaxTopicBytes: 32})
	require.Error(t, err)

	countOverLimit := basmValidAnchorJSON("0", "2")
	_, err = DecodeAnchorJSON([]byte(countOverLimit), Limits{MaxAdmitted: 1, MaxRange: 1, MaxJSONBytes: 1024, MaxTopicBytes: 32})
	require.Error(t, err)

	fields64 := basmValidAnchorJSON("0", "0")
	for i := 0; i < 59; i++ {
		fields64 = strings.Replace(fields64, "}", fmt.Sprintf(`,"extra%d":true}`, i), 1)
	}
	_, err = DecodeAnchorJSON([]byte(fields64), Limits{MaxAdmitted: 1, MaxRange: 1, MaxJSONBytes: 16 << 20, MaxTopicBytes: 32})
	require.NoError(t, err)

	fields65 := strings.Replace(fields64, "}", `,"oneMore":true}`, 1)
	_, err = DecodeAnchorJSON([]byte(fields65), Limits{MaxAdmitted: 1, MaxRange: 1, MaxJSONBytes: 16 << 20, MaxTopicBytes: 32})
	require.Error(t, err)

	invalidUTF8 := append([]byte(`{"topic":"`), byte(0xff))
	invalidUTF8 = append(invalidUTF8, []byte(`","blockHeight":0,"blockHash":"`+basmJSONGenesisTxID+`","basmRoot":"`+basmJSONZeroHash+`","admittedCount":0}`)...)
	_, err = DecodeAnchorJSON(invalidUTF8, DefaultLimits())
	require.Error(t, err)
}

func TestDecodeAdmittedListJSONRejectsMalformedAndUnsafeValues(t *testing.T) {
	valid := `[{"txid":"` + basmJSONGenesisTxID + `","blockIndex":0}]`
	tests := []struct {
		name string
		data string
	}{
		{
			name: "non-array",
			data: `{}`,
		},
		{
			name: "trailing data",
			data: valid + ` []`,
		},
		{
			name: "missing txid",
			data: `[{"blockIndex":0}]`,
		},
		{
			name: "uppercase txid",
			data: strings.Replace(valid, basmJSONGenesisTxID, strings.ToUpper(basmJSONGenesisTxID), 1),
		},
		{
			name: "fractional index",
			data: strings.Replace(valid, `"blockIndex":0`, `"blockIndex":0.5`, 1),
		},
		{
			name: "exponent index",
			data: strings.Replace(valid, `"blockIndex":0`, `"blockIndex":1e0`, 1),
		},
		{
			name: "negative zero index",
			data: strings.Replace(valid, `"blockIndex":0`, `"blockIndex":-0`, 1),
		},
		{
			name: "unsafe JavaScript integer index",
			data: strings.Replace(valid, `"blockIndex":0`, `"blockIndex":9007199254740992`, 1),
		},
		{
			name: "duplicate escaped txid key",
			data: `[{"txid":"` + basmJSONGenesisTxID + `","\u0074xid":"` + basmJSONGenesisTxID + `","blockIndex":0}]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodeAdmittedListJSON(context.Background(), []byte(tt.data), 1, DefaultLimits())
			require.Error(t, err)
		})
	}

	_, err := DecodeAdmittedListJSON(context.Background(), []byte(`[{"txid":"`+basmJSONGenesisTxID+`","blockIndex":0},{"txid":"`+basmJSONZeroHash+`","blockIndex":1}]`), 2, Limits{MaxAdmitted: 1, MaxRange: 1, MaxJSONBytes: 1024, MaxTopicBytes: 1})
	require.Error(t, err)

	var nilContext context.Context
	_, err = DecodeAdmittedListJSON(nilContext, []byte(valid), 1, DefaultLimits())
	require.Error(t, err)
}

func TestDecodeAdmittedListJSONPreservesValidatedReferences(t *testing.T) {
	data := []byte(`[{"txid":"` + basmJSONGenesisTxID + `","blockIndex":0},{"txid":"` + basmJSONZeroHash + `","blockIndex":2}]`)
	original := bytes.Clone(data)

	admitted, err := DecodeAdmittedListJSON(context.Background(), data, 3, DefaultLimits())

	require.NoError(t, err)
	assert.Equal(t, []uint64{0, 2}, []uint64{admitted[0].BlockIndex, admitted[1].BlockIndex})
	assert.Equal(t, original, data)
}

func TestDecodeRangeJSONRejectsInvalidNumbersAndUint32Overflow(t *testing.T) {
	tests := []string{
		`{}`,
		`{"fromHeight":null,"toHeight":1}`,
		`{"fromHeight":1.0,"toHeight":1}`,
		`{"fromHeight":1e0,"toHeight":1}`,
		`{"fromHeight":-1,"toHeight":1}`,
		`{"fromHeight":-0,"toHeight":1}`,
		`{"fromHeight":4294967296,"toHeight":4294967296}`,
		`{"fromHeight":1,"toHeight":1,"\u0074oHeight":1}`,
		`{"fromHeight":1,"toHeight":1} {}`,
	}

	for _, data := range tests {
		t.Run(data, func(t *testing.T) {
			_, _, err := DecodeRangeJSON([]byte(data), DefaultLimits())
			require.Error(t, err)
		})
	}

	from, to, err := DecodeRangeJSON([]byte(`{"fromHeight":4294967295,"toHeight":4294967295,"extension":true}`), Limits{MaxAdmitted: 1, MaxRange: 1, MaxJSONBytes: 1024, MaxTopicBytes: 1})
	require.NoError(t, err)
	assert.Equal(t, ^uint32(0), from)
	assert.Equal(t, ^uint32(0), to)
}

func basmValidAnchorJSON(height, count string) string {
	return `{"topic":"topic","blockHeight":` + height + `,"blockHash":` + `"` + basmJSONGenesisTxID + `"` + `,"basmRoot":` + `"` + basmJSONZeroHash + `"` + `,"admittedCount":` + count + `}`
}
