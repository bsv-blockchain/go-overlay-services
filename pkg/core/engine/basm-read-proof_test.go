package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/basm"
)

func TestBASMReadServiceRejectsRedundantOwnParentProofOffset(t *testing.T) {
	fx := newReadFixture(t)
	// Four leaves, with only the first leaf and its sibling supplied at the
	// base. Level-one offset 1 is needed; offset 0 is their redundant parent.
	leftRoot := mustHash(t, "b4100303b9e99ada4b479b0bb93d9b549cb057a1c4be08896bc982debe20ce39")
	rightRoot, err := basm.Root(context.Background(), fx.leaves[2:4], 2)
	require.NoError(t, err)
	legal := [][]proofNode{
		{{offset: 0, hash: fx.leaves[0], txid: true}, {offset: 1, hash: fx.leaves[1]}},
		{{offset: 1, hash: rightRoot}},
	}
	fx.view.proofs[fx.leaves[0]] = bumpBytes(1, legal)
	service := newReadService(t, fx.storage(), fx.headers, fx.limits)
	_, err = service.ProvideCompoundMerklePath(context.Background(), fx.topic, 1, []basm.Hash{fx.leaves[0]})
	require.NoError(t, err, "necessary interior sibling remains valid")

	legal[1] = append(legal[1], proofNode{offset: 0, hash: leftRoot})
	fx.view.proofs[fx.leaves[0]] = bumpBytes(1, legal)
	closedBefore := fx.view.closed
	response, err := service.ProvideCompoundMerklePath(context.Background(), fx.topic, 1, []basm.Hash{fx.leaves[0]})
	require.ErrorIs(t, err, ErrBASMInvalidData)
	assert.Empty(t, response.MerklePath)
	assert.Equal(t, closedBefore+1, fx.view.closed)
}

func TestBASMReadServiceRejectsDisconnectedUnrequestedBaseLeaf(t *testing.T) {
	fx := newReadFixture(t)
	rightRoot, err := basm.Root(context.Background(), fx.leaves[2:4], 2)
	require.NoError(t, err)
	// The first leaf still has a complete valid proof through the supplied
	// right-subtree root. The unrelated fourth leaf has no sibling at offset 2
	// and cannot be authenticated by that opaque root.
	fx.view.proofs[fx.leaves[0]] = bumpBytes(1, [][]proofNode{
		{{offset: 0, hash: fx.leaves[0], txid: true}, {offset: 1, hash: fx.leaves[1]}, {offset: 3, hash: fx.leaves[3]}},
		{{offset: 1, hash: rightRoot}},
	})
	service := newReadService(t, fx.storage(), fx.headers, fx.limits)
	response, err := service.ProvideCompoundMerklePath(context.Background(), fx.topic, 1, []basm.Hash{fx.leaves[0]})
	require.ErrorIs(t, err, ErrBASMInvalidData)
	assert.Empty(t, response.MerklePath)
	assert.Equal(t, 1, fx.view.closed)
}
