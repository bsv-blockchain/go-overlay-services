package engine_test

import (
	"context"
	"errors"
	"testing"

	"github.com/4chain-ag/go-overlay-services/pkg/core/engine"
	"github.com/bsv-blockchain/go-sdk/chainhash"
	"github.com/bsv-blockchain/go-sdk/overlay"
	"github.com/bsv-blockchain/go-sdk/overlay/lookup"
	"github.com/bsv-blockchain/go-sdk/script"
	"github.com/bsv-blockchain/go-sdk/transaction"
	"github.com/stretchr/testify/require"
)

func TestEngine_HandleNewMerkleProof(t *testing.T) {
	t.Run("should handle simple proof", func(t *testing.T) {
		// given
		ctx := context.Background()

		// Create a transaction with outputs
		tx := transaction.NewTransaction()
		tx.AddOutput(&transaction.TransactionOutput{
			Satoshis:      1000,
			LockingScript: &script.Script{},
		})
		txid := tx.TxID()

		// Create BEEF from the transaction
		beef, err := transaction.NewBeefFromTransaction(tx)
		require.NoError(t, err)
		beefBytes, err := beef.AtomicBytes(txid)
		require.NoError(t, err)

		// Create merkle path
		merklePath := &transaction.MerklePath{
			BlockHeight: 814435,
			Path: [][]*transaction.PathElement{{
				{
					Hash:   txid,
					Offset: 123,
				},
			}},
		}

		// Create output
		output := &engine.Output{
			Outpoint: transaction.Outpoint{
				Txid:  *txid,
				Index: 0,
			},
			Topic:       "test-topic",
			Satoshis:    1000,
			BlockHeight: 0,
			BlockIdx:    0,
			Beef:        beefBytes,
		}

		// Mock storage
		mockStorage := &mockHandleMerkleProofStorage{
			findOutputsForTransactionFunc: func(ctx context.Context, txid *chainhash.Hash, includeBEEF bool) ([]*engine.Output, error) {
				return []*engine.Output{output}, nil
			},
			updateOutputBlockHeightFunc: func(ctx context.Context, outpoint *transaction.Outpoint, topic string, blockHeight uint32, blockIdx uint64, ancillaryBeef []byte) error {
				// Verify the block height and index are updated
				require.Equal(t, uint32(814435), blockHeight)
				require.Equal(t, uint64(123), blockIdx)
				return nil
			},
		}

		// Mock lookup service
		mockLookupService := &mockLookupService{
			outputBlockHeightUpdatedFunc: func(ctx context.Context, txid *chainhash.Hash, blockHeight uint32, blockIdx uint64) error {
				// Verify notification is sent
				require.Equal(t, uint32(814435), blockHeight)
				require.Equal(t, uint64(123), blockIdx)
				return nil
			},
		}

		sut := &engine.Engine{
			Storage:        mockStorage,
			LookupServices: map[string]engine.LookupService{"test-service": mockLookupService},
		}

		// when
		err = sut.HandleNewMerkleProof(ctx, txid, merklePath)

		// then
		require.NoError(t, err)
	})

	t.Run("should return error when transaction not found in proof", func(t *testing.T) {
		// given
		ctx := context.Background()

		tx := transaction.NewTransaction()
		tx.AddOutput(&transaction.TransactionOutput{
			Satoshis:      1000,
			LockingScript: &script.Script{},
		})
		txid := tx.TxID()

		// Create merkle path without the transaction
		differentTxid := &chainhash.Hash{1, 2, 3}
		merklePath := &transaction.MerklePath{
			BlockHeight: 814435,
			Path: [][]*transaction.PathElement{{
				{
					Hash:   differentTxid, // Different transaction ID
					Offset: 123,
				},
			}},
		}

		output := &engine.Output{
			Outpoint: transaction.Outpoint{
				Txid:  *txid,
				Index: 0,
			},
		}

		mockStorage := &mockHandleMerkleProofStorage{
			findOutputsForTransactionFunc: func(ctx context.Context, txid *chainhash.Hash, includeBEEF bool) ([]*engine.Output, error) {
				return []*engine.Output{output}, nil
			},
		}

		sut := &engine.Engine{
			Storage: mockStorage,
		}

		// when
		err := sut.HandleNewMerkleProof(ctx, txid, merklePath)

		// then
		require.Error(t, err)
		require.Contains(t, err.Error(), "not found in proof")
	})

	t.Run("should handle no outputs found", func(t *testing.T) {
		// given
		ctx := context.Background()
		txid := &chainhash.Hash{1, 2, 3}
		merklePath := &transaction.MerklePath{}

		mockStorage := &mockHandleMerkleProofStorage{
			findOutputsForTransactionFunc: func(ctx context.Context, txid *chainhash.Hash, includeBEEF bool) ([]*engine.Output, error) {
				return []*engine.Output{}, nil // No outputs
			},
		}

		sut := &engine.Engine{
			Storage: mockStorage,
		}

		// when
		err := sut.HandleNewMerkleProof(ctx, txid, merklePath)

		// then
		require.NoError(t, err)
	})

	t.Run("should handle storage error", func(t *testing.T) {
		// given
		ctx := context.Background()
		txid := &chainhash.Hash{1, 2, 3}
		merklePath := &transaction.MerklePath{}
		expectedErr := errors.New("storage error")

		mockStorage := &mockHandleMerkleProofStorage{
			findOutputsForTransactionFunc: func(ctx context.Context, txid *chainhash.Hash, includeBEEF bool) ([]*engine.Output, error) {
				return nil, expectedErr
			},
		}

		sut := &engine.Engine{
			Storage: mockStorage,
		}

		// when
		err := sut.HandleNewMerkleProof(ctx, txid, merklePath)

		// then
		require.Error(t, err)
		require.Equal(t, expectedErr, err)
	})

	t.Run("should update consumedBy relationships for chain of transactions", func(t *testing.T) {
		// given
		ctx := context.Background()

		// Create a chain of transactions
		tx1 := transaction.NewTransaction()
		tx1.AddOutput(&transaction.TransactionOutput{
			Satoshis:      1000,
			LockingScript: &script.Script{},
		})
		txid1 := tx1.TxID()

		tx2 := transaction.NewTransaction()
		tx2.AddInput(&transaction.TransactionInput{
			SourceTXID:       txid1,
			SourceTxOutIndex: 0,
		})
		tx2.AddOutput(&transaction.TransactionOutput{
			Satoshis:      900,
			LockingScript: &script.Script{},
		})
		txid2 := tx2.TxID()

		// Create BEEF for tx2 that includes tx1 as input
		beef := &transaction.Beef{
			Version: transaction.BEEF_V2,
			Transactions: map[string]*transaction.BeefTx{
				txid1.String(): {Transaction: tx1},
				txid2.String(): {Transaction: tx2},
			},
		}
		beef2Bytes, err := beef.AtomicBytes(txid2)
		require.NoError(t, err)

		// Create merkle path for tx2
		merklePath := &transaction.MerklePath{
			BlockHeight: 814436,
			Path: [][]*transaction.PathElement{{
				{
					Hash:   txid2,
					Offset: 456,
				},
			}},
		}

		// Create outputs with consumedBy relationship
		output1 := &engine.Output{
			Outpoint: transaction.Outpoint{
				Txid:  *txid1,
				Index: 0,
			},
			Topic:      "test-topic",
			ConsumedBy: []*transaction.Outpoint{{Txid: *txid2, Index: 0}},
		}

		output2 := &engine.Output{
			Outpoint: transaction.Outpoint{
				Txid:  *txid2,
				Index: 0,
			},
			Topic:           "test-topic",
			OutputsConsumed: []*transaction.Outpoint{{Txid: *txid1, Index: 0}},
			Beef:            beef2Bytes,
		}

		updateCount := 0
		mockStorage := &mockHandleMerkleProofStorage{
			findOutputsForTransactionFunc: func(ctx context.Context, txid *chainhash.Hash, includeBEEF bool) ([]*engine.Output, error) {
				if txid.Equal(*txid2) {
					return []*engine.Output{output2}, nil
				}
				return nil, nil
			},
			findOutputFunc: func(ctx context.Context, outpoint *transaction.Outpoint, topic *string, spent *bool, includeBEEF bool) (*engine.Output, error) {
				if outpoint.Txid.Equal(*txid1) {
					return output1, nil
				}
				return nil, nil
			},
			updateOutputBlockHeightFunc: func(ctx context.Context, outpoint *transaction.Outpoint, topic string, blockHeight uint32, blockIdx uint64, ancillaryBeef []byte) error {
				updateCount++
				return nil
			},
		}

		sut := &engine.Engine{
			Storage:        mockStorage,
			LookupServices: map[string]engine.LookupService{},
		}

		// when
		err = sut.HandleNewMerkleProof(ctx, txid2, merklePath)

		// then
		require.NoError(t, err)
		require.Equal(t, 1, updateCount) // Should update the output
	})
}

// Mock storage for HandleNewMerkleProof tests
type mockHandleMerkleProofStorage struct {
	findOutputsForTransactionFunc func(ctx context.Context, txid *chainhash.Hash, includeBEEF bool) ([]*engine.Output, error)
	findOutputFunc                func(ctx context.Context, outpoint *transaction.Outpoint, topic *string, spent *bool, includeBEEF bool) (*engine.Output, error)
	updateOutputBlockHeightFunc   func(ctx context.Context, outpoint *transaction.Outpoint, topic string, blockHeight uint32, blockIdx uint64, ancillaryBeef []byte) error
}

func (m *mockHandleMerkleProofStorage) FindOutputsForTransaction(ctx context.Context, txid *chainhash.Hash, includeBEEF bool) ([]*engine.Output, error) {
	if m.findOutputsForTransactionFunc != nil {
		return m.findOutputsForTransactionFunc(ctx, txid, includeBEEF)
	}
	return nil, nil
}

func (m *mockHandleMerkleProofStorage) FindOutput(ctx context.Context, outpoint *transaction.Outpoint, topic *string, spent *bool, includeBEEF bool) (*engine.Output, error) {
	if m.findOutputFunc != nil {
		return m.findOutputFunc(ctx, outpoint, topic, spent, includeBEEF)
	}
	return nil, nil
}

func (m *mockHandleMerkleProofStorage) UpdateOutputBlockHeight(ctx context.Context, outpoint *transaction.Outpoint, topic string, blockHeight uint32, blockIdx uint64, ancillaryBeef []byte) error {
	if m.updateOutputBlockHeightFunc != nil {
		return m.updateOutputBlockHeightFunc(ctx, outpoint, topic, blockHeight, blockIdx, ancillaryBeef)
	}
	return nil
}

// Implement remaining Storage interface methods
func (m *mockHandleMerkleProofStorage) SetIncoming(ctx context.Context, txs []*transaction.Transaction) error {
	return nil
}
func (m *mockHandleMerkleProofStorage) SetOutgoing(ctx context.Context, tx *transaction.Transaction, steak *overlay.Steak) error {
	return nil
}
func (m *mockHandleMerkleProofStorage) UpdateConsumedBy(ctx context.Context, outpoint *transaction.Outpoint, topic string, consumedBy []*transaction.Outpoint) error {
	return nil
}
func (m *mockHandleMerkleProofStorage) DeleteOutput(ctx context.Context, outpoint *transaction.Outpoint, topic string) error {
	return nil
}
func (m *mockHandleMerkleProofStorage) FindTransaction(ctx context.Context, txid chainhash.Hash, requireProof bool) (*transaction.Transaction, error) {
	return nil, nil
}
func (m *mockHandleMerkleProofStorage) FindTransactionsCreatingUtxos(ctx context.Context) ([]*chainhash.Hash, error) {
	return nil, nil
}
func (m *mockHandleMerkleProofStorage) FindUTXOsForTopic(ctx context.Context, topic string, since uint32, includeBEEF bool) ([]*engine.Output, error) {
	return nil, nil
}
func (m *mockHandleMerkleProofStorage) FindOutputs(ctx context.Context, outpoints []*transaction.Outpoint, topic string, spent *bool, includeBEEF bool) ([]*engine.Output, error) {
	return nil, nil
}

func (m *mockHandleMerkleProofStorage) InsertOutput(ctx context.Context, utxo *engine.Output) error {
	return nil
}

func (m *mockHandleMerkleProofStorage) InsertAppliedTransaction(ctx context.Context, tx *overlay.AppliedTransaction) error {
	return nil
}

func (m *mockHandleMerkleProofStorage) DoesAppliedTransactionExist(ctx context.Context, tx *overlay.AppliedTransaction) (bool, error) {
	return false, nil
}

func (m *mockHandleMerkleProofStorage) MarkUTXOsAsSpent(ctx context.Context, outpoints []*transaction.Outpoint, topic string, spendTxid *chainhash.Hash) error {
	return nil
}

func (m *mockHandleMerkleProofStorage) UpdateTransactionBEEF(ctx context.Context, txid *chainhash.Hash, beef []byte) error {
	return nil
}

// Mock lookup service
type mockLookupService struct {
	outputBlockHeightUpdatedFunc func(ctx context.Context, txid *chainhash.Hash, blockHeight uint32, blockIdx uint64) error
}

func (m *mockLookupService) OutputBlockHeightUpdated(ctx context.Context, txid *chainhash.Hash, blockHeight uint32, blockIdx uint64) error {
	if m.outputBlockHeightUpdatedFunc != nil {
		return m.outputBlockHeightUpdatedFunc(ctx, txid, blockHeight, blockIdx)
	}
	return nil
}

// Implement remaining LookupService interface methods
func (m *mockLookupService) Lookup(ctx context.Context, question *lookup.LookupQuestion) (*lookup.LookupAnswer, error) {
	return nil, nil
}
func (m *mockLookupService) GetMetaData() *overlay.MetaData {
	return nil
}

func (m *mockLookupService) GetDocumentation() string {
	return ""
}

func (m *mockLookupService) OutputAdmittedByTopic(ctx context.Context, payload *engine.OutputAdmittedByTopic) error {
	return nil
}

func (m *mockLookupService) OutputSpent(ctx context.Context, payload *engine.OutputSpent) error {
	return nil
}

func (m *mockLookupService) OutputNoLongerRetainedInHistory(ctx context.Context, outpoint *transaction.Outpoint, topic string) error {
	return nil
}

func (m *mockLookupService) OutputEvicted(ctx context.Context, outpoint *transaction.Outpoint) error {
	return nil
}
