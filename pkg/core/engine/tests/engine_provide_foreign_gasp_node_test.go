package engine_test

import (
	"context"
	"errors"
	"testing"

	"github.com/4chain-ag/go-overlay-services/pkg/core/engine"
	"github.com/4chain-ag/go-overlay-services/pkg/core/gasp/core"
	"github.com/bsv-blockchain/go-sdk/transaction"
	"github.com/stretchr/testify/require"
)

func TestEngine_ProvideForeignGASPNode_Success(t *testing.T) {
	// given:
	ctx := context.Background()
	graphID := &transaction.Outpoint{}
	outpoint := &transaction.Outpoint{Index: 1}
	BEEF := createDummyBEEF(t)

	expectedNode := &core.GASPNode{
		GraphID:     graphID,
		RawTx:       parseBEEFToTx(t, BEEF).Hex(),
		OutputIndex: outpoint.Index,
	}

	sut := &engine.Engine{
		Storage: fakeStorage{
			findOutputFunc: func(ctx context.Context, outpoint *transaction.Outpoint, topic *string, spent *bool, includeBEEF bool) (*engine.Output, error) {
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
			findOutputFunc: func(ctx context.Context, outpoint *transaction.Outpoint, topic *string, spent *bool, includeBEEF bool) (*engine.Output, error) {
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
	expectedErr := errors.New("forced error")

	sut := &engine.Engine{
		Storage: fakeStorage{
			findOutputFunc: func(ctx context.Context, outpoint *transaction.Outpoint, topic *string, spent *bool, includeBEEF bool) (*engine.Output, error) {
				return nil, expectedErr
			},
		},
	}

	// when:
	node, err := sut.ProvideForeignGASPNode(ctx, graphID, outpoint, "test-topic")

	// then:
	require.ErrorIs(t, err, expectedErr)
	require.Nil(t, node)
}

func TestEngine_ProvideForeignGASPNode_TransactionNotFound_ShouldReturnError(t *testing.T) {
	// given:
	ctx := context.Background()
	graphID := &transaction.Outpoint{}
	outpoint := &transaction.Outpoint{}

	sut := &engine.Engine{
		Storage: fakeStorage{
			findOutputFunc: func(ctx context.Context, outpoint *transaction.Outpoint, topic *string, spent *bool, includeBEEF bool) (*engine.Output, error) {
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
