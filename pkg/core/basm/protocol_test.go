package basm

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const protocolHash = "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"

func protocolLimits() ReadLimits {
	return DefaultReadLimits()
}

func TestDecodeReadRequestAcceptsFiveTSKinds(t *testing.T) {
	secondHash := strings.Repeat("ab", hashSize)
	tests := []struct {
		name  string
		kind  string
		body  string
		check func(t *testing.T, request ReadRequest)
	}{
		{
			name: "tip",
			kind: "tip",
			body: `{"\u672a\u6765":"\u5024"}`,
			check: func(t *testing.T, request ReadRequest) {
				assert.Equal(t, ReadRequest{}, request)
			},
		},
		{
			name: "range with additive unicode field",
			kind: "range",
			body: `{"fromHeight":3,"toHeight":5,"\u672a\u6765":{"\u5024":true}}`,
			check: func(t *testing.T, request ReadRequest) {
				assert.Equal(t, uint32(3), request.FromHeight)
				assert.Equal(t, uint32(5), request.ToHeight)
			},
		},
		{
			name: "list with optional hash",
			kind: "list",
			body: fmt.Sprintf(`{"blockHeight":7,"blockHash":%q,"future":null}`, protocolHash),
			check: func(t *testing.T, request ReadRequest) {
				require.NotNil(t, request.BlockHash)
				assert.Equal(t, protocolHash, request.BlockHash.String())
				assert.Equal(t, uint32(7), request.BlockHeight)
			},
		},
		{
			name: "proof",
			kind: "proof",
			body: fmt.Sprintf(`{"blockHeight":8,"txids":[%q,%q]}`, protocolHash, secondHash),
			check: func(t *testing.T, request ReadRequest) {
				assert.Equal(t, uint32(8), request.BlockHeight)
				require.Len(t, request.TxIDs, 2)
				assert.Equal(t, protocolHash, request.TxIDs[0].String())
				assert.Equal(t, secondHash, request.TxIDs[1].String())
			},
		},
		{
			name: "empty raw",
			kind: "raw",
			body: `{"txids":[],"future":"additive"}`,
			check: func(t *testing.T, request ReadRequest) {
				assert.Empty(t, request.TxIDs)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request, err := DecodeReadRequest([]byte(test.body), test.kind, protocolLimits())
			require.NoError(t, err)
			test.check(t, request)
		})
	}
}

func TestDecodeReadRequestRejectsMissingNullDuplicateAndUnsafeValues(t *testing.T) {
	hash := protocolHash
	tests := []struct {
		name string
		kind string
		body string
		want error
	}{
		{name: "range missing fromHeight", kind: "range", body: `{"toHeight":1}`, want: ErrInvalidInput},
		{name: "range null fromHeight", kind: "range", body: `{"fromHeight":null,"toHeight":1}`, want: ErrInvalidInput},
		// strconv.ParseUint reports this overflow directly rather than wrapping
		// ErrInvalidInput, but it must still be rejected.
		{name: "range unsafe uint32", kind: "range", body: `{"fromHeight":0,"toHeight":4294967296}`},
		{name: "range noninteger", kind: "range", body: `{"fromHeight":1.5,"toHeight":2}`, want: ErrInvalidInput},
		{name: "range duplicate field", kind: "range", body: `{"fromHeight":0,"fromHeight":1,"toHeight":1}`, want: ErrInvalidInput},
		{name: "list missing height", kind: "list", body: fmt.Sprintf(`{"blockHash":%q}`, hash), want: ErrInvalidInput},
		{name: "list null height", kind: "list", body: `{"blockHeight":null}`, want: ErrInvalidInput},
		{name: "list null optional hash", kind: "list", body: `{"blockHeight":1,"blockHash":null}`, want: ErrInvalidHash},
		{name: "proof missing txids", kind: "proof", body: `{"blockHeight":1}`, want: ErrInvalidInput},
		{name: "proof null txids", kind: "proof", body: `{"blockHeight":1,"txids":null}`, want: ErrInvalidInput},
		{name: "proof empty", kind: "proof", body: `{"blockHeight":1,"txids":[]}`, want: ErrInvalidInput},
		{name: "raw missing txids", kind: "raw", body: `{}`, want: ErrInvalidInput},
		{name: "raw null txids", kind: "raw", body: `{"txids":null}`, want: ErrInvalidInput},
		{name: "duplicate object field", kind: "raw", body: fmt.Sprintf(`{"txids":[%q],"txids":[]}`, hash), want: ErrInvalidInput},
		{name: "duplicate requested txid", kind: "raw", body: fmt.Sprintf(`{"txids":[%q,%q]}`, hash, hash), want: ErrInvalidInput},
		{name: "uppercase hash", kind: "raw", body: fmt.Sprintf(`{"txids":[%q]}`, strings.ToUpper(hash)), want: ErrInvalidHash},
		{name: "short hash", kind: "raw", body: `{"txids":["01"]}`, want: ErrInvalidHash},
		{name: "nonhex hash", kind: "raw", body: `{"txids":["zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"]}`, want: ErrInvalidHash},
		{name: "trailing JSON", kind: "tip", body: `{} {}`, want: ErrInvalidInput},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := DecodeReadRequest([]byte(test.body), test.kind, protocolLimits())
			require.Error(t, err)
			if test.want != nil {
				assert.ErrorIs(t, err, test.want)
			}
		})
	}
}

func TestDecodeReadRequestEnforcesCountAndRequestByteLimits(t *testing.T) {
	limits := protocolLimits()
	limits.MaxRequestedTxIDs = 1
	hash := protocolHash

	_, err := DecodeReadRequest(
		[]byte(fmt.Sprintf(`{"txids":[%q,%q]}`, hash, strings.Repeat("ab", hashSize))),
		"raw",
		limits,
	)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrLimitExceeded)

	limits = protocolLimits()
	limits.MaxRequestBytes = 16
	_, err = DecodeReadRequest([]byte(`{"txids":[],"padding":"x"}`), "raw", limits)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrLimitExceeded)
}

func TestReadLimitsValidateRequiresFinitePositiveBudgets(t *testing.T) {
	require.NoError(t, DefaultReadLimits().Validate())

	tests := []struct {
		name   string
		mutate func(*ReadLimits)
	}{
		{name: "base admitted", mutate: func(l *ReadLimits) { l.MaxAdmitted = 0 }},
		{name: "requested count", mutate: func(l *ReadLimits) { l.MaxRequestedTxIDs = 0 }},
		{name: "request bytes", mutate: func(l *ReadLimits) { l.MaxRequestBytes = 0 }},
		{name: "proof bytes", mutate: func(l *ReadLimits) { l.MaxProofBytes = 0 }},
		{name: "raw bytes", mutate: func(l *ReadLimits) { l.MaxRawTxBytes = 0 }},
		{name: "response bytes", mutate: func(l *ReadLimits) { l.MaxResponseBytes = 0 }},
		{name: "timeout", mutate: func(l *ReadLimits) { l.RequestTimeout = 0 }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			limits := protocolLimits()
			test.mutate(&limits)
			err := limits.Validate()
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrInvalidInput)
		})
	}
}

func TestDecodeReadRequestRejectsInvalidUnicode(t *testing.T) {
	tests := []string{
		`{"\u672a\u6765":"\ud800"}`,
		`{"\u672a\u6765":"\udc00"}`,
		`{"\u672a\u6765":"\ud800x"}`,
	}
	for _, body := range tests {
		_, err := DecodeReadRequest([]byte(body), "tip", protocolLimits())
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidInput)
	}
}

func TestDecodeReadRequestRejectsUnknownKind(t *testing.T) {
	_, err := DecodeReadRequest([]byte(`{}`), "anchor", protocolLimits())
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInvalidInput)
	assert.NotErrorIs(t, err, ErrLimitExceeded)
}
