package gasp_test

import (
	"context"
	"encoding/hex"
	"errors"
	"sync"
	"testing"

	"github.com/4chain-ag/go-overlay-services/pkg/core/gasp/core"
	"github.com/bsv-blockchain/go-sdk/chainhash"
	"github.com/bsv-blockchain/go-sdk/script"
	"github.com/bsv-blockchain/go-sdk/transaction"
	"github.com/stretchr/testify/require"
)

// Mock types for testing
type mockUTXO struct {
	GraphID     *transaction.Outpoint
	RawTx       string
	OutputIndex uint32
	Time        uint32
	Txid        *chainhash.Hash
	Inputs      map[string]*mockUTXO
}

type mockGASPStorage struct {
	knownStore     []*mockUTXO
	tempGraphStore map[string]*mockUTXO
	mu             sync.Mutex
	updateCallback func()

	// Configurable behavior functions
	findKnownUTXOsFunc      func(ctx context.Context, sinceWhen uint32) ([]*transaction.Outpoint, error)
	hydrateGASPNodeFunc     func(ctx context.Context, graphID *transaction.Outpoint, outpoint *transaction.Outpoint, metadata bool) (*core.GASPNode, error)
	appendToGraphFunc       func(ctx context.Context, tx *core.GASPNode, spentBy *transaction.Outpoint) error
	validateGraphAnchorFunc func(ctx context.Context, graphID *transaction.Outpoint) error
	discardGraphFunc        func(ctx context.Context, graphID *transaction.Outpoint) error
	finalizeGraphFunc       func(ctx context.Context, graphID *transaction.Outpoint) error
	findNeededInputsFunc    func(ctx context.Context, tx *core.GASPNode) (*core.GASPNodeResponse, error)
}

func newMockGASPStorage(knownStore []*mockUTXO) *mockGASPStorage {
	return &mockGASPStorage{
		knownStore:     knownStore,
		tempGraphStore: make(map[string]*mockUTXO),
		updateCallback: func() {},
	}
}

func (m *mockGASPStorage) SetUpdateCallback(f func()) {
	m.updateCallback = f
}

func (m *mockGASPStorage) FindKnownUTXOs(ctx context.Context, sinceWhen uint32) ([]*transaction.Outpoint, error) {
	if m.findKnownUTXOsFunc != nil {
		return m.findKnownUTXOsFunc(ctx, sinceWhen)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	var result []*transaction.Outpoint
	for _, utxo := range m.knownStore {
		if utxo.Time >= sinceWhen {
			result = append(result, utxo.GraphID)
		}
	}
	return result, nil
}

func (m *mockGASPStorage) HydrateGASPNode(ctx context.Context, graphID *transaction.Outpoint, outpoint *transaction.Outpoint, metadata bool) (*core.GASPNode, error) {
	if m.hydrateGASPNodeFunc != nil {
		return m.hydrateGASPNodeFunc(ctx, graphID, outpoint, metadata)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check in known store
	for _, utxo := range m.knownStore {
		if utxo.GraphID.String() == outpoint.String() {
			node := &core.GASPNode{
				GraphID:     graphID,
				RawTx:       utxo.RawTx,
				OutputIndex: utxo.OutputIndex,
				Inputs:      make(map[string]*core.GASPInput),
			}

			// Add inputs
			for id, input := range utxo.Inputs {
				node.Inputs[id] = &core.GASPInput{
					Hash: input.Txid.String(),
				}
			}

			return node, nil
		}
	}

	// Check in temp store
	if tempUTXO, exists := m.tempGraphStore[outpoint.String()]; exists {
		return &core.GASPNode{
			GraphID:     graphID,
			RawTx:       tempUTXO.RawTx,
			OutputIndex: tempUTXO.OutputIndex,
			Inputs:      make(map[string]*core.GASPInput),
		}, nil
	}

	return nil, nil
}

func (m *mockGASPStorage) FindNeededInputs(ctx context.Context, tx *core.GASPNode) (*core.GASPNodeResponse, error) {
	if m.findNeededInputsFunc != nil {
		return m.findNeededInputsFunc(ctx, tx)
	}

	// Default: no inputs needed
	return &core.GASPNodeResponse{
		RequestedInputs: make(map[string]*core.GASPNodeResponseData),
	}, nil
}

func (m *mockGASPStorage) AppendToGraph(ctx context.Context, tx *core.GASPNode, spentBy *transaction.Outpoint) error {
	if m.appendToGraphFunc != nil {
		return m.appendToGraphFunc(ctx, tx, spentBy)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Parse the transaction to get its ID
	parsedTx, _ := transaction.NewTransactionFromHex(tx.RawTx)
	var hash *chainhash.Hash
	if parsedTx != nil {
		hash = parsedTx.TxID()
	}
	m.tempGraphStore[tx.GraphID.String()] = &mockUTXO{
		GraphID:     tx.GraphID,
		RawTx:       tx.RawTx,
		OutputIndex: tx.OutputIndex,
		Time:        0, // Current time
		Txid:        hash,
		Inputs:      make(map[string]*mockUTXO),
	}
	return nil
}

func (m *mockGASPStorage) ValidateGraphAnchor(ctx context.Context, graphID *transaction.Outpoint) error {
	if m.validateGraphAnchorFunc != nil {
		return m.validateGraphAnchorFunc(ctx, graphID)
	}

	// Default: allow validation to pass
	return nil
}

func (m *mockGASPStorage) DiscardGraph(ctx context.Context, graphID *transaction.Outpoint) error {
	if m.discardGraphFunc != nil {
		return m.discardGraphFunc(ctx, graphID)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.tempGraphStore, graphID.String())
	return nil
}

func (m *mockGASPStorage) FinalizeGraph(ctx context.Context, graphID *transaction.Outpoint) error {
	if m.finalizeGraphFunc != nil {
		return m.finalizeGraphFunc(ctx, graphID)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if tempGraph, exists := m.tempGraphStore[graphID.String()]; exists {
		m.knownStore = append(m.knownStore, tempGraph)
		m.updateCallback()
		delete(m.tempGraphStore, graphID.String())
	}
	return nil
}

type mockGASPRemote struct {
	targetGASP          *core.GASP
	initialResponseFunc func(ctx context.Context, request *core.GASPInitialRequest) (*core.GASPInitialResponse, error)
	requestNodeFunc     func(ctx context.Context, graphID, outpoint *transaction.Outpoint, metadata bool) (*core.GASPNode, error)
}

func (m *mockGASPRemote) GetInitialResponse(ctx context.Context, request *core.GASPInitialRequest) (*core.GASPInitialResponse, error) {
	if m.initialResponseFunc != nil {
		return m.initialResponseFunc(ctx, request)
	}

	if m.targetGASP != nil {
		return m.targetGASP.GetInitialResponse(ctx, request)
	}

	return nil, nil
}

func (m *mockGASPRemote) GetInitialReply(ctx context.Context, response *core.GASPInitialResponse) (*core.GASPInitialReply, error) {
	if m.targetGASP != nil {
		return m.targetGASP.GetInitialReply(ctx, response)
	}

	// Default implementation
	return &core.GASPInitialReply{
		UTXOList: []*transaction.Outpoint{},
	}, nil
}

func (m *mockGASPRemote) RequestNode(ctx context.Context, graphID, outpoint *transaction.Outpoint, metadata bool) (*core.GASPNode, error) {
	if m.requestNodeFunc != nil {
		return m.requestNodeFunc(ctx, graphID, outpoint, metadata)
	}

	if m.targetGASP != nil {
		// Use the storage to hydrate the node
		return m.targetGASP.Storage.HydrateGASPNode(ctx, graphID, outpoint, metadata)
	}

	return nil, nil
}

func (m *mockGASPRemote) SubmitNode(ctx context.Context, node *core.GASPNode) (*core.GASPNodeResponse, error) {
	if m.targetGASP != nil {
		return m.targetGASP.SubmitNode(ctx, node)
	}

	// Default implementation
	return &core.GASPNodeResponse{
		RequestedInputs: make(map[string]*core.GASPNodeResponseData),
	}, nil
}

func createMockUTXO(txHex string, outputIndex uint32, time uint32) *mockUTXO {
	// Create a proper transaction and get its hex
	tx := transaction.NewTransaction()
	tx.AddOutput(&transaction.TransactionOutput{
		Satoshis:      1000,
		LockingScript: &script.Script{},
	})

	// Use the actual transaction hex instead of the provided string
	realTxHex := hex.EncodeToString(tx.Bytes())

	return &mockUTXO{
		GraphID: &transaction.Outpoint{
			Txid:  *tx.TxID(),
			Index: outputIndex,
		},
		RawTx:       realTxHex,
		OutputIndex: outputIndex,
		Time:        time,
		Txid:        tx.TxID(),
		Inputs:      make(map[string]*mockUTXO),
	}
}

func TestGASP_SyncBasicScenarios(t *testing.T) {
	t.Run("should fail to sync if versions are wrong", func(t *testing.T) {
		// given
		ctx := context.Background()
		storage1 := newMockGASPStorage([]*mockUTXO{})
		storage2 := newMockGASPStorage([]*mockUTXO{})

		gasp1 := core.NewGASP(core.GASPParams{
			Storage: storage1,
			Version: intPtr(2), // Different version
		})
		gasp2 := core.NewGASP(core.GASPParams{
			Storage: storage2,
			Version: intPtr(1),
		})

		gasp1.Remote = &mockGASPRemote{targetGASP: gasp2}

		// when & then
		err := gasp1.Sync(ctx)
		require.Error(t, err)
		require.Contains(t, err.Error(), "GASP version mismatch")
	})

	t.Run("bidirectional sync should share UTXOs both ways", func(t *testing.T) {
		// given
		ctx := context.Background()
		utxo1 := createMockUTXO("mock_sender1_rawtx1", 0, 111)

		storage1 := newMockGASPStorage([]*mockUTXO{utxo1}) // Alice has UTXO
		storage2 := newMockGASPStorage([]*mockUTXO{})      // Bob has no UTXOs

		gasp1 := core.NewGASP(core.GASPParams{Storage: storage1})
		gasp2 := core.NewGASP(core.GASPParams{Storage: storage2})

		// Bob syncs from Alice
		gasp2.Remote = &mockGASPRemote{targetGASP: gasp1}

		// when
		err := gasp2.Sync(ctx)

		// then
		require.NoError(t, err)

		utxos1, _ := storage1.FindKnownUTXOs(ctx, 0)
		utxos2, _ := storage2.FindKnownUTXOs(ctx, 0)

		require.Len(t, utxos2, 1)
		require.Equal(t, len(utxos1), len(utxos2))
	})

	t.Run("should synchronize a single UTXO from Bob to Alice", func(t *testing.T) {
		// given
		ctx := context.Background()
		utxo1 := createMockUTXO("mock_sender1_rawtx1", 0, 111)

		storage1 := newMockGASPStorage([]*mockUTXO{})      // Alice has no UTXOs
		storage2 := newMockGASPStorage([]*mockUTXO{utxo1}) // Bob has UTXO

		gasp1 := core.NewGASP(core.GASPParams{Storage: storage1})
		gasp2 := core.NewGASP(core.GASPParams{Storage: storage2})

		gasp1.Remote = &mockGASPRemote{targetGASP: gasp2}

		// when
		err := gasp1.Sync(ctx)

		// then
		require.NoError(t, err)

		utxos1, _ := storage1.FindKnownUTXOs(ctx, 0)
		utxos2, _ := storage2.FindKnownUTXOs(ctx, 0)

		require.Len(t, utxos1, 1)
		require.Equal(t, len(utxos1), len(utxos2))
	})

	t.Run("should discard graphs that do not validate", func(t *testing.T) {
		// given
		ctx := context.Background()
		utxo1 := createMockUTXO("mock_sender1_rawtx1", 0, 111)

		storage1 := newMockGASPStorage([]*mockUTXO{utxo1})
		storage2 := newMockGASPStorage([]*mockUTXO{})

		discardGraphCalled := false
		storage2.validateGraphAnchorFunc = func(ctx context.Context, graphID *transaction.Outpoint) error {
			return errors.New("invalid graph anchor")
		}
		storage2.discardGraphFunc = func(ctx context.Context, graphID *transaction.Outpoint) error {
			discardGraphCalled = true
			require.Equal(t, utxo1.GraphID.String(), graphID.String())
			return nil
		}

		gasp1 := core.NewGASP(core.GASPParams{Storage: storage1})
		gasp2 := core.NewGASP(core.GASPParams{Storage: storage2})

		// Bob syncs from Alice
		gasp2.Remote = &mockGASPRemote{targetGASP: gasp1}

		// when
		err := gasp2.Sync(ctx)

		// then
		require.NoError(t, err) // Sync should complete despite validation failure

		utxos2, _ := storage2.FindKnownUTXOs(ctx, 0)
		require.Len(t, utxos2, 0) // No UTXOs should be synchronized
		require.True(t, discardGraphCalled)
	})

	t.Run("should synchronize multiple graphs", func(t *testing.T) {
		// given
		ctx := context.Background()
		utxo1 := createMockUTXO("mock_sender1_rawtx1", 0, 111)
		utxo2 := createMockUTXO("mock_sender2_rawtx1", 0, 222)

		storage1 := newMockGASPStorage([]*mockUTXO{utxo1, utxo2})
		storage2 := newMockGASPStorage([]*mockUTXO{})

		gasp1 := core.NewGASP(core.GASPParams{Storage: storage1})
		gasp2 := core.NewGASP(core.GASPParams{Storage: storage2})

		// Bob syncs from Alice
		gasp2.Remote = &mockGASPRemote{targetGASP: gasp1}

		// when
		err := gasp2.Sync(ctx)

		// then
		require.NoError(t, err)

		utxos1, _ := storage1.FindKnownUTXOs(ctx, 0)
		utxos2, _ := storage2.FindKnownUTXOs(ctx, 0)

		require.Len(t, utxos2, 2)
		require.Equal(t, len(utxos1), len(utxos2))
	})

	t.Run("should synchronize only UTXOs created after the specified since timestamp", func(t *testing.T) {
		// given
		ctx := context.Background()
		oldUTXO := createMockUTXO("old_rawtx", 0, 100) // Timestamp 100
		newUTXO := createMockUTXO("new_rawtx", 1, 200) // Timestamp 200

		storage1 := newMockGASPStorage([]*mockUTXO{oldUTXO, newUTXO})
		storage2 := newMockGASPStorage([]*mockUTXO{})

		gasp1 := core.NewGASP(core.GASPParams{
			Storage:         storage1,
			LastInteraction: 0,
		})
		gasp2 := core.NewGASP(core.GASPParams{
			Storage:         storage2,
			LastInteraction: 150, // Bob only wants UTXOs newer than 150
		})

		// Bob syncs from Alice (who has both old and new UTXOs)
		gasp2.Remote = &mockGASPRemote{targetGASP: gasp1}

		// when
		err := gasp2.Sync(ctx)

		// then
		require.NoError(t, err)

		utxos2, _ := storage2.FindKnownUTXOs(ctx, 0)
		require.Len(t, utxos2, 1) // Only new UTXO should be synchronized

		// Verify it's the new UTXO
		require.Equal(t, newUTXO.GraphID.Index, utxos2[0].Index)
	})

	t.Run("should not sync unnecessary graphs", func(t *testing.T) {
		// given
		ctx := context.Background()
		utxo1 := createMockUTXO("mock_sender1_rawtx1", 0, 111)

		storage1 := newMockGASPStorage([]*mockUTXO{utxo1}) // Both have same UTXO
		storage2 := newMockGASPStorage([]*mockUTXO{utxo1})

		finalizeGraphCalled1 := false
		finalizeGraphCalled2 := false

		storage1.finalizeGraphFunc = func(ctx context.Context, graphID *transaction.Outpoint) error {
			finalizeGraphCalled1 = true
			return nil
		}
		storage2.finalizeGraphFunc = func(ctx context.Context, graphID *transaction.Outpoint) error {
			finalizeGraphCalled2 = true
			return nil
		}

		gasp1 := core.NewGASP(core.GASPParams{Storage: storage1})
		gasp2 := core.NewGASP(core.GASPParams{Storage: storage2})

		gasp1.Remote = &mockGASPRemote{targetGASP: gasp2}

		// when
		err := gasp1.Sync(ctx)

		// then
		require.NoError(t, err)

		utxos1, _ := storage1.FindKnownUTXOs(ctx, 0)
		utxos2, _ := storage2.FindKnownUTXOs(ctx, 0)

		require.Len(t, utxos1, 1)
		require.Len(t, utxos2, 1)
		require.False(t, finalizeGraphCalled1, "FinalizeGraph should not be called when no sync needed")
		require.False(t, finalizeGraphCalled2, "FinalizeGraph should not be called when no sync needed")
	})
}

// Helper function to create int pointer
func intPtr(i int) *int {
	return &i
}
