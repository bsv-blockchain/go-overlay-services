package engine_test

import (
	"context"
	"errors"
	"testing"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/engine"
	"github.com/bsv-blockchain/go-overlay-services/pkg/core/gasp"
	"github.com/bsv-blockchain/go-sdk/transaction"
	"github.com/stretchr/testify/require"
)

var errForcedError = errors.New("forced error")

func TestEngine_ProvideForeignGASPNode_Success(t *testing.T) {
	// given:
	ctx := context.Background()
	graphID := &transaction.Outpoint{}
	outpoint := &transaction.Outpoint{Index: 1}
	BEEF := createDummyBEEF(t)

	expectedNode := &gasp.Node{
		GraphID:     graphID,
		RawTx:       parseBEEFToTx(t, BEEF).Hex(),
		OutputIndex: outpoint.Index,
	}

	sut := &engine.Engine{
		Storage: fakeStorage{
			findOutputFunc: func(_ context.Context, _ *transaction.Outpoint, _ *string, _ *bool, _ bool) (*engine.Output, error) {
				return &engine.Output{Beef: BEEF}, nil
			},
		},
	}

	// when:
	node, err := sut.ProvideForeignGASPNode(ctx, graphID, outpoint, "test-topic")

	// then:
	require.NoError(t, err)
	require.Equal(t, expectedNode, node)
}

func TestEngine_ProvideForeignGASPNode_MissingBeef_ShouldReturnError(t *testing.T) {
	// given:
	ctx := context.Background()
	graphID := &transaction.Outpoint{}
	outpoint := &transaction.Outpoint{}

	sut := &engine.Engine{
		Storage: fakeStorage{
			findOutputFunc: func(_ context.Context, _ *transaction.Outpoint, _ *string, _ *bool, _ bool) (*engine.Output, error) {
				return &engine.Output{}, nil // Missing Beef
			},
		},
	}

	// when:
	node, err := sut.ProvideForeignGASPNode(ctx, graphID, outpoint, "test-topic")

	// then:
	require.ErrorIs(t, err, engine.ErrMissingInput)
	require.Nil(t, node)
}

func TestEngine_ProvideForeignGASPNode_CannotFindOutput_ShouldReturnError(t *testing.T) {
	// given:
	ctx := context.Background()
	graphID := &transaction.Outpoint{}
	outpoint := &transaction.Outpoint{}

	sut := &engine.Engine{
		Storage: fakeStorage{
			findOutputFunc: func(_ context.Context, _ *transaction.Outpoint, _ *string, _ *bool, _ bool) (*engine.Output, error) {
				return nil, errForcedError
			},
		},
	}

	// when:
	node, err := sut.ProvideForeignGASPNode(ctx, graphID, outpoint, "test-topic")

	// then:
	require.ErrorIs(t, err, errForcedError)
	require.Nil(t, node)
}

func TestEngine_ProvideForeignGASPNode_TransactionNotFound_ShouldReturnError(t *testing.T) {
	// given:
	ctx := context.Background()
	graphID := &transaction.Outpoint{}
	outpoint := &transaction.Outpoint{}

	sut := &engine.Engine{
		Storage: fakeStorage{
			findOutputFunc: func(_ context.Context, _ *transaction.Outpoint, _ *string, _ *bool, _ bool) (*engine.Output, error) {
				return &engine.Output{Beef: []byte{0x00}}, nil
			},
		},
	}

	// when:
	node, err := sut.ProvideForeignGASPNode(ctx, graphID, outpoint, "test-topic")

	// then:
	require.ErrorContains(t, err, "invalid-version") // temp solution
	require.Nil(t, node)
}
