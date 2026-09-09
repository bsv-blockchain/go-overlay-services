package basm

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const genesisTxID = "4a5e1e4baab89f3a32518a88c31bc87f618f76673e2cc77ab2127b7afdeda33b"

func TestParseHashGenesisTransactionIDRoundTrip(t *testing.T) {
	txID, err := ParseHash(genesisTxID)
	require.NoError(t, err)

	assert.Equal(t, byte(0x3b), txID[0])
	assert.Equal(t, byte(0x4a), txID[31])
	assert.Equal(t, genesisTxID, txID.String())

	text, err := txID.MarshalText()
	require.NoError(t, err)
	assert.Equal(t, genesisTxID, string(text))

	var unmarshaled Hash
	require.NoError(t, unmarshaled.UnmarshalText(text))
	assert.Equal(t, txID, unmarshaled)
}

func TestParseHashRejectsNonCanonicalDisplayText(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "uppercase",
			input: strings.ToUpper(genesisTxID),
		},
		{
			name:  "short",
			input: genesisTxID[:63],
		},
		{
			name:  "literal null",
			input: "null",
		},
		{
			name:  "embedded null byte",
			input: strings.Repeat("0", 63) + "\x00",
		},
		{
			name:  "non hexadecimal character",
			input: strings.Repeat("0", 63) + "g",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseHash(tt.input)
			require.Error(t, err)
		})
	}
}

func TestHashUnmarshalTextRejectsNilReceiver(t *testing.T) {
	var hash *Hash

	err := hash.UnmarshalText([]byte(genesisTxID))

	require.Error(t, err)
}

func TestHashTACStepHashesThreeInternalHashes(t *testing.T) {
	got := HashTACStep(Hash{}, Hash{}, Hash{})

	assert.Equal(t, "3a464e1e43410c7add1dd81c3f10486f41eb473bb43e8d64feca3c7f0c8028d3", got.String())
}

func TestRootHandlesEmptyAndSingleLeafWithoutMutation(t *testing.T) {
	txID, err := ParseHash(genesisTxID)
	require.NoError(t, err)

	emptyRoot, err := Root(context.Background(), nil, 1)
	require.NoError(t, err)
	assert.Equal(t, Hash{}, emptyRoot)

	leaves := []Hash{txID}
	root, err := Root(context.Background(), leaves, 1)
	require.NoError(t, err)
	assert.Equal(t, txID, root)
	assert.Equal(t, []Hash{txID}, leaves)
}

func TestRootRejectsInvalidBoundsAndCanceledContext(t *testing.T) {
	txID, err := ParseHash(genesisTxID)
	require.NoError(t, err)

	_, err = Root(context.Background(), []Hash{txID}, 0)
	require.Error(t, err)

	_, err = Root(context.Background(), []Hash{txID, txID}, 1)
	require.Error(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = Root(ctx, []Hash{txID, txID}, 2)
	require.ErrorIs(t, err, context.Canceled)
}
