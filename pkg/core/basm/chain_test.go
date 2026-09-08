package basm

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewChainStartsWithoutTipAtZeroAndNonzeroGenesis(t *testing.T) {
	for _, genesis := range []uint32{0, 19} {
		t.Run("genesis", func(t *testing.T) {
			chain, err := NewChain("topic", genesis, DefaultLimits())
			require.NoError(t, err)

			height, tac, ok := chain.Tip()
			assert.False(t, ok)
			assert.Equal(t, uint32(0), height)
			assert.Equal(t, Hash{}, tac)
		})
	}
}

func TestChainAppendUsesGenesisAndReturnsImmutableReplacement(t *testing.T) {
	chain, err := NewChain("topic", 19, DefaultLimits())
	require.NoError(t, err)

	first := basmChainAnchor("topic", 19)
	updated, err := chain.Append(first)
	require.NoError(t, err)

	_, _, originalOK := chain.Tip()
	height, firstTAC, updatedOK := updated.Tip()
	assert.False(t, originalOK)
	assert.True(t, updatedOK)
	assert.Equal(t, uint32(19), height)
	assert.NotEqual(t, Hash{}, firstTAC)

	second := basmChainAnchor("topic", 20)
	next, err := updated.Append(second)
	require.NoError(t, err)

	updatedHeight, updatedTAC, updatedOK := updated.Tip()
	nextHeight, nextTAC, nextOK := next.Tip()
	assert.True(t, updatedOK)
	assert.True(t, nextOK)
	assert.Equal(t, uint32(19), updatedHeight)
	assert.Equal(t, firstTAC, updatedTAC)
	assert.Equal(t, uint32(20), nextHeight)
	assert.NotEqual(t, updatedTAC, nextTAC)
}

func TestChainAppendAcceptsGenesisHeightZero(t *testing.T) {
	chain, err := NewChain("topic", 0, DefaultLimits())
	require.NoError(t, err)

	updated, err := chain.Append(basmChainAnchor("topic", 0))
	require.NoError(t, err)

	height, _, ok := updated.Tip()
	assert.True(t, ok)
	assert.Equal(t, uint32(0), height)
}

func TestChainAppendRejectsDiscontinuitiesWithoutChangingState(t *testing.T) {
	limits := DefaultLimits()
	chain, err := NewChain("topic", 3, limits)
	require.NoError(t, err)

	first, err := chain.Append(basmChainAnchor("topic", 3))
	require.NoError(t, err)

	tests := []struct {
		name   string
		chain  Chain
		anchor TopicBlockAnchor
	}{
		{
			name:   "gap before genesis",
			chain:  chain,
			anchor: basmChainAnchor("topic", 4),
		},
		{
			name:   "repeated height",
			chain:  first,
			anchor: basmChainAnchor("topic", 3),
		},
		{
			name:   "missing intervening empty height",
			chain:  first,
			anchor: basmChainAnchor("topic", 5),
		},
		{
			name:   "topic mismatch",
			chain:  first,
			anchor: basmChainAnchor("other", 4),
		},
		{
			name:   "invalid empty anchor root",
			chain:  first,
			anchor: TopicBlockAnchor{Topic: "topic", BlockHeight: 4, BASMRoot: Hash{1}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.chain.Append(tt.anchor)
			require.Error(t, err)
			assert.Equal(t, tt.chain, got)
		})
	}
}

func TestChainAppendRejectsHeightAfterUint32MaximumWithoutChangingState(t *testing.T) {
	maxUint32 := ^uint32(0)
	chain, err := NewChain("topic", maxUint32, DefaultLimits())
	require.NoError(t, err)

	tip, err := chain.Append(basmChainAnchor("topic", maxUint32))
	require.NoError(t, err)

	got, err := tip.Append(basmChainAnchor("topic", maxUint32))
	require.Error(t, err)
	assert.Equal(t, tip, got)
}

func basmChainAnchor(topic string, height uint32) TopicBlockAnchor {
	var blockHash Hash
	binary.LittleEndian.PutUint32(blockHash[:4], height)

	return TopicBlockAnchor{
		Topic:       topic,
		BlockHeight: height,
		BlockHash:   blockHash,
	}
}
