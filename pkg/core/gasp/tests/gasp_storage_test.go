package gasp_test

import (
	"context"
	"errors"
	"testing"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/engine"
	"github.com/bsv-blockchain/go-overlay-services/pkg/core/gasp"
	"github.com/bsv-blockchain/go-sdk/chainhash"
	"github.com/bsv-blockchain/go-sdk/overlay"
	"github.com/bsv-blockchain/go-sdk/script"
	"github.com/bsv-blockchain/go-sdk/transaction"
	"github.com/stretchr/testify/require"
)

var (
	errDatabaseError  = errors.New("database error")
	errOutputNotFound = errors.New("output not found")
)

func TestOverlayGASPStorage_AppendToGraph(t *testing.T) {
	t.Run("should append a new node to an empty graph", func(t *testing.T) {
		// given
		ctx := context.Background()
		mockEngine := &engine.Engine{
			Storage: &mockStorage{},
		}
		storage := engine.NewOverlayGASPStorage("test-topic", mockEngine, nil)

		// Create a minimal valid transaction
		tx := transaction.NewTransaction()
		tx.AddOutput(&transaction.TransactionOutput{
			Satoshis:      1000,
			LockingScript: &script.Script{},
		})

		graphID := &transaction.Outpoint{
			Txid:  *tx.TxID(),
			Index: 0,
		}

		gaspNode := &gasp.Node{
			RawTx:       tx.Hex(),
			OutputIndex: 0,
			GraphID:     graphID,
		}

		// when
		err := storage.AppendToGraph(ctx, gaspNode, nil)

		// then
		require.NoError(t, err)
		// Verify node was added by trying to append a child
		childTx := transaction.NewTransaction()
		childTx.AddOutput(&transaction.TransactionOutput{
			Satoshis:      500,
			LockingScript: &script.Script{},
		})

		childNode := &gasp.Node{
			RawTx:       childTx.Hex(),
			OutputIndex: 0,
			GraphID:     graphID,
		}

		// The parent outpoint that the child is spending
		parentOutpoint := &transaction.Outpoint{
			Txid:  *tx.TxID(),
			Index: 0,
		}
		err = storage.AppendToGraph(ctx, childNode, parentOutpoint)
		require.NoError(t, err)
	})

	t.Run("should return error when max nodes exceeded", func(t *testing.T) {
		// given
		ctx := context.Background()
		maxNodes := 2
		mockEngine := &engine.Engine{
			Storage: &mockStorage{},
		}
		storage := engine.NewOverlayGASPStorage("test-topic", mockEngine, &maxNodes)

		// Add nodes up to the limit
		for i := 0; i < maxNodes; i++ {
			tx := transaction.NewTransaction()
			tx.AddOutput(&transaction.TransactionOutput{
				Satoshis:      1000,
				LockingScript: &script.Script{},
			})

			graphID := &transaction.Outpoint{
				Txid:  *tx.TxID(),
				Index: uint32(i), // #nosec G115
			}

			gaspNode := &gasp.Node{
				RawTx:       tx.Hex(),
				OutputIndex: uint32(i), // #nosec G115
				GraphID:     graphID,
			}

			err := storage.AppendToGraph(ctx, gaspNode, nil)
			require.NoError(t, err)
		}

		// Try to add one more node
		tx := transaction.NewTransaction()
		tx.AddOutput(&transaction.TransactionOutput{
			Satoshis:      1000,
			LockingScript: &script.Script{},
		})

		graphID := &transaction.Outpoint{
			Txid:  *tx.TxID(),
			Index: 99,
		}

		gaspNode := &gasp.Node{
			RawTx:       tx.Hex(),
			OutputIndex: 99,
			GraphID:     graphID,
		}

		// when
		err := storage.AppendToGraph(ctx, gaspNode, nil)

		// then
		require.Error(t, err)
		require.Equal(t, engine.ErrGraphFull, err)
	})

	t.Run("should return error for invalid transaction hex", func(t *testing.T) {
		// given
		ctx := context.Background()
		mockEngine := &engine.Engine{
			Storage: &mockStorage{},
		}
		storage := engine.NewOverlayGASPStorage("test-topic", mockEngine, nil)

		gaspNode := &gasp.Node{
			RawTx:       "invalid-hex",
			OutputIndex: 0,
			GraphID: &transaction.Outpoint{
				Txid:  chainhash.Hash{},
				Index: 0,
			},
		}

		// when
		err := storage.AppendToGraph(ctx, gaspNode, nil)

		// then
		require.Error(t, err)
	})
}

func TestOverlayGASPStorage_FindKnownUTXOs(t *testing.T) {
	t.Run("should return known UTXOs since given timestamp", func(t *testing.T) {
		// given
		ctx := context.Background()
		since := uint32(1234567890)
		expectedUTXOs := []*engine.Output{
			{
				Outpoint: transaction.Outpoint{
					Txid:  chainhash.Hash{1},
					Index: 0,
				},
			},
			{
				Outpoint: transaction.Outpoint{
					Txid:  chainhash.Hash{2},
					Index: 1,
				},
			},
		}

		mockStorage := &mockStorage{
			findUTXOsForTopicFunc: func(_ context.Context, _ string, _ float64, _ uint32, _ bool) ([]*engine.Output, error) {
				return expectedUTXOs, nil
			},
		}

		mockEngine := &engine.Engine{
			Storage: mockStorage,
		}
		storage := engine.NewOverlayGASPStorage("test-topic", mockEngine, nil)

		// when
		result, err := storage.FindKnownUTXOs(ctx, float64(since), 0)

		// then
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Len(t, result, 2)
		require.Equal(t, expectedUTXOs[0].Outpoint.Txid, result[0].Txid)
		require.Equal(t, expectedUTXOs[0].Outpoint.Index, result[0].OutputIndex)
		require.Equal(t, expectedUTXOs[1].Outpoint.Txid, result[1].Txid)
		require.Equal(t, expectedUTXOs[1].Outpoint.Index, result[1].OutputIndex)
	})

	t.Run("should return limited UTXOs when limit is specified", func(t *testing.T) {
		// given
		ctx := context.Background()
		since := uint32(100)
		limit := uint32(2)
		// Create many UTXOs with different scores
		expectedUTXOs := []*engine.Output{
			{
				Outpoint: transaction.Outpoint{
					Txid:  chainhash.Hash{1},
					Index: 0,
				},
				BlockHeight: 110,
				Score:       110,
			},
			{
				Outpoint: transaction.Outpoint{
					Txid:  chainhash.Hash{2},
					Index: 1,
				},
				BlockHeight: 120,
				Score:       120,
			},
			{
				Outpoint: transaction.Outpoint{
					Txid:  chainhash.Hash{3},
					Index: 2,
				},
				BlockHeight: 130,
				Score:       130,
			},
			{
				Outpoint: transaction.Outpoint{
					Txid:  chainhash.Hash{4},
					Index: 3,
				},
				BlockHeight: 140,
				Score:       140,
			},
		}

		mockStorage := &mockStorage{
			findUTXOsForTopicFunc: func(_ context.Context, _ string, _ float64, limit uint32, _ bool) ([]*engine.Output, error) {
				// Mock should respect the limit
				if limit > 0 && len(expectedUTXOs) > int(limit) {
					return expectedUTXOs[:limit], nil
				}
				return expectedUTXOs, nil
			},
		}

		mockEngine := &engine.Engine{
			Storage: mockStorage,
		}
		storage := engine.NewOverlayGASPStorage("test-topic", mockEngine, nil)

		// when
		result, err := storage.FindKnownUTXOs(ctx, float64(since), limit)

		// then
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Len(t, result, int(limit)) // Should return exactly 'limit' UTXOs

		// Verify we got the first 2 UTXOs
		require.Equal(t, expectedUTXOs[0].Outpoint.Txid, result[0].Txid)
		require.Equal(t, expectedUTXOs[0].Outpoint.Index, result[0].OutputIndex)
		require.Equal(t, expectedUTXOs[1].Outpoint.Txid, result[1].Txid)
		require.Equal(t, expectedUTXOs[1].Outpoint.Index, result[1].OutputIndex)
	})

	t.Run("should handle storage errors", func(t *testing.T) {
		// given
		ctx := context.Background()
		// Use the static error variable

		mockStorage := &mockStorage{
			findUTXOsForTopicFunc: func(_ context.Context, _ string, _ float64, _ uint32, _ bool) ([]*engine.Output, error) {
				return nil, errDatabaseError
			},
		}

		mockEngine := &engine.Engine{
			Storage: mockStorage,
		}
		storage := engine.NewOverlayGASPStorage("test-topic", mockEngine, nil)

		// when
		result, err := storage.FindKnownUTXOs(ctx, 0, 0)

		// then
		require.Error(t, err)
		require.Equal(t, errDatabaseError, err)
		require.Nil(t, result)
	})
}

func TestOverlayGASPStorage_DiscardGraph(t *testing.T) {
	t.Run("should discard graph and all its nodes", func(t *testing.T) {
		// given
		ctx := context.Background()
		mockEngine := &engine.Engine{
			Storage: &mockStorage{},
		}
		storage := engine.NewOverlayGASPStorage("test-topic", mockEngine, nil)

		// Create a graph with root and child nodes
		rootTx := transaction.NewTransaction()
		rootTx.AddOutput(&transaction.TransactionOutput{
			Satoshis:      1000,
			LockingScript: &script.Script{},
		})

		graphID := &transaction.Outpoint{
			Txid:  *rootTx.TxID(),
			Index: 0,
		}

		rootNode := &gasp.Node{
			RawTx:       rootTx.Hex(),
			OutputIndex: 0,
			GraphID:     graphID,
		}

		// Add root node
		err := storage.AppendToGraph(ctx, rootNode, nil)
		require.NoError(t, err)

		// Add child node
		childTx := transaction.NewTransaction()
		childTx.AddOutput(&transaction.TransactionOutput{
			Satoshis:      500,
			LockingScript: &script.Script{},
		})

		childNode := &gasp.Node{
			RawTx:       childTx.Hex(),
			OutputIndex: 0,
			GraphID:     graphID,
		}

		// The parent outpoint that the child is spending
		rootOutpoint := &transaction.Outpoint{
			Txid:  *rootTx.TxID(),
			Index: 0,
		}
		err = storage.AppendToGraph(ctx, childNode, rootOutpoint)
		require.NoError(t, err)

		// when
		err = storage.DiscardGraph(ctx, graphID)

		// then
		require.NoError(t, err)

		// Verify graph is empty by trying to add to the discarded graph
		newNode := &gasp.Node{
			RawTx:       rootTx.Hex(),
			OutputIndex: 1,
			GraphID:     graphID,
		}

		// This should fail because the parent node was discarded
		rootOutpoint2 := &transaction.Outpoint{
			Txid:  *rootTx.TxID(),
			Index: 0,
		}
		err = storage.AppendToGraph(ctx, newNode, rootOutpoint2)
		require.Error(t, err)
	})

	t.Run("should handle non-existent graphID gracefully", func(t *testing.T) {
		// given
		ctx := context.Background()
		mockEngine := &engine.Engine{
			Storage: &mockStorage{},
		}
		storage := engine.NewOverlayGASPStorage("test-topic", mockEngine, nil)

		nonExistentGraphID := &transaction.Outpoint{
			Txid:  chainhash.Hash{99, 99, 99},
			Index: 0,
		}

		// when
		err := storage.DiscardGraph(ctx, nonExistentGraphID)

		// then
		require.NoError(t, err)
	})
}

func TestOverlayGASPStorage_HydrateGASPNode(t *testing.T) {
	t.Run("should return error when no output found", func(t *testing.T) {
		// given
		ctx := context.Background()
		mockStorage := &mockStorage{
			findOutputFunc: func(_ context.Context, _ *transaction.Outpoint, _ *string, _ *bool, _ bool) (*engine.Output, error) {
				return nil, errOutputNotFound // No output found
			},
		}

		mockEngine := &engine.Engine{
			Storage: mockStorage,
		}
		storage := engine.NewOverlayGASPStorage("test-topic", mockEngine, nil)

		graphID := &transaction.Outpoint{
			Txid:  chainhash.Hash{1},
			Index: 0,
		}
		outpoint := &transaction.Outpoint{
			Txid:  chainhash.Hash{2},
			Index: 0,
		}

		// when
		result, err := storage.HydrateGASPNode(ctx, graphID, outpoint, false)

		// then
		require.Error(t, err)
		require.Equal(t, errOutputNotFound, err)
		require.Nil(t, result)
	})

	t.Run("should hydrate node with valid BEEF", func(t *testing.T) {
		// given
		ctx := context.Background()

		// Create a transaction with merkle path
		tx := transaction.NewTransaction()
		tx.AddOutput(&transaction.TransactionOutput{
			Satoshis:      1000,
			LockingScript: &script.Script{},
		})

		// Create mock merkle path
		tx.MerklePath = &transaction.MerklePath{
			BlockHeight: 100,
			Path:        [][]*transaction.PathElement{},
		}

		beef, err := transaction.NewBeefFromTransaction(tx)
		require.NoError(t, err)
		beefBytes, err := beef.AtomicBytes(tx.TxID())
		require.NoError(t, err)

		mockStorage := &mockStorage{
			findOutputFunc: func(_ context.Context, outpoint *transaction.Outpoint, _ *string, _ *bool, _ bool) (*engine.Output, error) {
				return &engine.Output{
					Outpoint: *outpoint,
					Beef:     beefBytes,
				}, nil
			},
		}

		mockEngine := &engine.Engine{
			Storage: mockStorage,
		}
		storage := engine.NewOverlayGASPStorage("test-topic", mockEngine, nil)

		graphID := &transaction.Outpoint{
			Txid:  chainhash.Hash{1},
			Index: 0,
		}
		outpoint := &transaction.Outpoint{
			Txid:  *tx.TxID(),
			Index: 0,
		}

		// when
		result, err := storage.HydrateGASPNode(ctx, graphID, outpoint, false)

		// then
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Equal(t, graphID, result.GraphID)
		require.Equal(t, uint32(0), result.OutputIndex)
		require.Equal(t, tx.Hex(), result.RawTx)
		require.NotNil(t, result.Proof)
	})
}

// Mock storage implementation
type mockStorage struct {
	findUTXOsForTopicFunc func(_ context.Context, topic string, since float64, limit uint32, historical bool) ([]*engine.Output, error)
	findOutputFunc        func(_ context.Context, outpoint *transaction.Outpoint, topic *string, spent *bool, historical bool) (*engine.Output, error)
	findOutputsFunc       func(_ context.Context, outpoints []*transaction.Outpoint, topic string, spent *bool, historical bool) ([]*engine.Output, error)
}

func (m *mockStorage) FindUTXOsForTopic(ctx context.Context, topic string, since float64, limit uint32, historical bool) ([]*engine.Output, error) {
	if m.findUTXOsForTopicFunc != nil {
		return m.findUTXOsForTopicFunc(ctx, topic, since, limit, historical)
	}
	return nil, nil
}

func (m *mockStorage) FindOutput(ctx context.Context, outpoint *transaction.Outpoint, topic *string, spent *bool, historical bool) (*engine.Output, error) {
	if m.findOutputFunc != nil {
		return m.findOutputFunc(ctx, outpoint, topic, spent, historical)
	}
	return nil, nil //nolint:nilnil // mock returns nil for unset implementation
}

func (m *mockStorage) FindOutputs(ctx context.Context, outpoints []*transaction.Outpoint, topic string, spent *bool, historical bool) ([]*engine.Output, error) {
	if m.findOutputsFunc != nil {
		return m.findOutputsFunc(ctx, outpoints, topic, spent, historical)
	}
	return nil, nil
}

// Implement remaining Storage interface methods with empty implementations
func (m *mockStorage) SetIncoming(_ context.Context, _ []*transaction.Transaction) error {
	return nil
}

func (m *mockStorage) SetOutgoing(_ context.Context, _ *transaction.Transaction, _ *overlay.Steak) error {
	return nil
}

func (m *mockStorage) UpdateConsumedBy(_ context.Context, _ *transaction.Outpoint, _ string, _ []*transaction.Outpoint) error {
	return nil
}

func (m *mockStorage) DeleteOutput(_ context.Context, _ *transaction.Outpoint, _ string) error {
	return nil
}

func (m *mockStorage) FindTransaction(_ context.Context, _ chainhash.Hash, _ bool) (*transaction.Transaction, error) {
	return nil, nil //nolint:nilnil // mock returns nil for unset implementation
}

func (m *mockStorage) FindTransactionsCreatingUtxos(_ context.Context) ([]*chainhash.Hash, error) {
	return nil, nil
}

func (m *mockStorage) DoesAppliedTransactionExist(_ context.Context, _ *overlay.AppliedTransaction) (bool, error) {
	return false, nil
}

func (m *mockStorage) InsertAppliedTransaction(_ context.Context, _ *overlay.AppliedTransaction) error {
	return nil
}

func (m *mockStorage) UpdateTransactionBEEF(_ context.Context, _ *chainhash.Hash, _ []byte) error {
	return nil
}

func (m *mockStorage) MarkUTXOsAsSpent(_ context.Context, _ []*transaction.Outpoint, _ string, _ *chainhash.Hash) error {
	return nil
}

func (m *mockStorage) InsertOutput(_ context.Context, _ *engine.Output) error {
	return nil
}

func (m *mockStorage) FindOutputsForTransaction(_ context.Context, _ *chainhash.Hash, _ bool) ([]*engine.Output, error) {
	return nil, nil
}

func (m *mockStorage) UpdateOutputBlockHeight(_ context.Context, _ *transaction.Outpoint, _ string, _ uint32, _ uint64, _ []byte) error {
	return nil
}

func (m *mockStorage) UpdateLastInteraction(_ context.Context, _, _ string, _ float64) error {
	return nil
}

func (m *mockStorage) GetLastInteraction(_ context.Context, _, _ string) (float64, error) {
	return 0, nil
}
