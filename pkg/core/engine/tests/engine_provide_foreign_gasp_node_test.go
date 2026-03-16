package engine_test

import (
	"context"
	"errors"
	"testing"

	"github.com/bsv-blockchain/go-sdk/chainhash"
	"github.com/bsv-blockchain/go-sdk/transaction"
	"github.com/stretchr/testify/require"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/engine"
	"github.com/bsv-blockchain/go-overlay-services/pkg/core/gasp"
)

func TestEngine_ProvideForeignGASPNode_Success(t *testing.T) {
	// given:
	ctx := context.Background()
	graphID := &transaction.Outpoint{}
	beefBytes := createDummyBEEF(t)
	beef, tx, _, err := transaction.ParseBeef(beefBytes)
	require.NoError(t, err)
	txid := tx.TxID()

	// The outpoint must match a transaction that exists in the BEEF
	outpoint := &transaction.Outpoint{
		Txid:  *txid,
		Index: 0,
	}

	expectedNode := &gasp.Node{
		GraphID:     graphID,
		RawTx:       tx.Hex(),
		OutputIndex: outpoint.Index,
	}

	sut := &engine.Engine{
		Storage: fakeStorage{
			findOutputFunc: func(_ context.Context, op *transaction.Outpoint, _ *string, _ *bool, _ bool) (*engine.Output, error) {
				return &engine.Output{
					Outpoint: *op,
					Beef:     beef,
				}, nil
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
	expectedErr := errors.New("forced error") //nolint:err113 // test sentinel

	sut := &engine.Engine{
		Storage: fakeStorage{
			findOutputFunc: func(_ context.Context, _ *transaction.Outpoint, _ *string, _ *bool, _ bool) (*engine.Output, error) {
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

	// Create an empty BEEF with no transactions
	emptyBeef := &transaction.Beef{
		Version:      transaction.BEEF_V2,
		Transactions: make(map[chainhash.Hash]*transaction.BeefTx),
	}

	sut := &engine.Engine{
		Storage: fakeStorage{
			findOutputFunc: func(_ context.Context, _ *transaction.Outpoint, _ *string, _ *bool, _ bool) (*engine.Output, error) {
				return &engine.Output{Beef: emptyBeef}, nil
			},
		},
	}

	// when:
	node, err := sut.ProvideForeignGASPNode(ctx, graphID, outpoint, "test-topic")

	// then:
	require.Error(t, err)
	require.Nil(t, node)
}
