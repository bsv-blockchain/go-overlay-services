package gasp_test

import (
	"context"
	"encoding/hex"
	"errors"
	"sync"
	"testing"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/gasp"
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
	findKnownUTXOsFunc      func(ctx context.Context, sinceWhen float64, limit uint32) ([]*gasp.Output, error)
	hydrateGASPNodeFunc     func(ctx context.Context, graphID *transaction.Outpoint, outpoint *transaction.Outpoint, metadata bool) (*gasp.Node, error)
	appendToGraphFunc       func(ctx context.Context, tx *gasp.Node, spentBy *transaction.Outpoint) error
	validateGraphAnchorFunc func(ctx context.Context, graphID *transaction.Outpoint) error
	discardGraphFunc        func(ctx context.Context, graphID *transaction.Outpoint) error
	finalizeGraphFunc       func(ctx context.Context, graphID *transaction.Outpoint) error
	findNeededInputsFunc    func(ctx context.Context, tx *gasp.Node) (*gasp.NodeResponse, error)
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

func (m *mockGASPStorage) FindKnownUTXOs(ctx context.Context, sinceWhen float64, limit uint32) ([]*gasp.Output, error) {
	if m.findKnownUTXOsFunc != nil {
		return m.findKnownUTXOsFunc(ctx, sinceWhen, limit)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	var result []*gasp.Output

	for _, utxo := range m.knownStore {
		if float64(utxo.Time) >= sinceWhen {
			result = append(result, &gasp.Output{
				Txid:        *utxo.Txid,
				OutputIndex: utxo.OutputIndex,
				Score:       float64(utxo.Time),
			})
		}
	}

	// Apply limit if specified
	if limit > 0 && len(result) > int(limit) {
		result = result[:limit]
	}

	return result, nil
}

func (m *mockGASPStorage) HydrateGASPNode(ctx context.Context, graphID *transaction.Outpoint, outpoint *transaction.Outpoint, metadata bool) (*gasp.Node, error) {
	if m.hydrateGASPNodeFunc != nil {
		return m.hydrateGASPNodeFunc(ctx, graphID, outpoint, metadata)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check in known store
	for _, utxo := range m.knownStore {
		if utxo.GraphID.String() == outpoint.String() {
			node := &gasp.Node{
				GraphID:     graphID,
				RawTx:       utxo.RawTx,
				OutputIndex: utxo.OutputIndex,
				Inputs:      make(map[string]*gasp.Input),
			}

			// Add inputs
			for id, input := range utxo.Inputs {
				node.Inputs[id] = &gasp.Input{
					Hash: input.Txid.String(),
				}
			}

			return node, nil
		}
	}

	// Check in temp store
	if tempUTXO, exists := m.tempGraphStore[outpoint.String()]; exists {
		return &gasp.Node{
			GraphID:     graphID,
			RawTx:       tempUTXO.RawTx,
			OutputIndex: tempUTXO.OutputIndex,
			Inputs:      make(map[string]*gasp.Input),
		}, nil
	}

	return nil, nil
}

func (m *mockGASPStorage) FindNeededInputs(ctx context.Context, tx *gasp.Node) (*gasp.NodeResponse, error) {
	if m.findNeededInputsFunc != nil {
		return m.findNeededInputsFunc(ctx, tx)
	}

	// Default: no inputs needed
	return &gasp.NodeResponse{
		RequestedInputs: make(map[string]*gasp.NodeResponseData),
	}, nil
}

func (m *mockGASPStorage) AppendToGraph(ctx context.Context, tx *gasp.Node, spentBy *transaction.Outpoint) error {
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
	targetGASP          *gasp.GASP
	initialResponseFunc func(ctx context.Context, request *gasp.InitialRequest) (*gasp.InitialResponse, error)
	requestNodeFunc     func(ctx context.Context, graphID, outpoint *transaction.Outpoint, metadata bool) (*gasp.Node, error)
}

func (m *mockGASPRemote) GetInitialResponse(ctx context.Context, request *gasp.InitialRequest) (*gasp.InitialResponse, error) {
	if m.initialResponseFunc != nil {
		return m.initialResponseFunc(ctx, request)
	}

	if m.targetGASP != nil {
		return m.targetGASP.GetInitialResponse(ctx, request)
	}

	return nil, nil
}

func (m *mockGASPRemote) GetInitialReply(ctx context.Context, response *gasp.InitialResponse) (*gasp.InitialReply, error) {
	if m.targetGASP != nil {
		return m.targetGASP.GetInitialReply(ctx, response)
	}

	// Default implementation
	return &gasp.InitialReply{
		UTXOList: []*gasp.Output{},
	}, nil
}

func (m *mockGASPRemote) RequestNode(ctx context.Context, graphID, outpoint *transaction.Outpoint, metadata bool) (*gasp.Node, error) {
	if m.requestNodeFunc != nil {
		return m.requestNodeFunc(ctx, graphID, outpoint, metadata)
	}

	if m.targetGASP != nil {
		// Use the storage to hydrate the node
		return m.targetGASP.Storage.HydrateGASPNode(ctx, graphID, outpoint, metadata)
	}

	return nil, nil
}

func (m *mockGASPRemote) SubmitNode(ctx context.Context, node *gasp.Node) (*gasp.NodeResponse, error) {
	if m.targetGASP != nil {
		return m.targetGASP.SubmitNode(ctx, node)
	}

	// Default implementation
	return &gasp.NodeResponse{
		RequestedInputs: make(map[string]*gasp.NodeResponseData),
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

		gasp1 := gasp.NewGASP(gasp.GASPParams{
			Storage: storage1,
			Version: intPtr(2), // Different version
		})
		gasp2 := gasp.NewGASP(gasp.GASPParams{
			Storage: storage2,
			Version: intPtr(1),
		})

		gasp1.Remote = &mockGASPRemote{targetGASP: gasp2}

		// when & then
		err := gasp1.Sync(ctx, "test-host", 0)
		require.Error(t, err)
		require.Contains(t, err.Error(), "GASP version mismatch")
	})

	t.Run("bidirectional sync should share UTXOs both ways", func(t *testing.T) {
		// given
		ctx := context.Background()
		utxo1 := createMockUTXO("mock_sender1_rawtx1", 0, 111)

		storage1 := newMockGASPStorage([]*mockUTXO{utxo1}) // Alice has UTXO
		storage2 := newMockGASPStorage([]*mockUTXO{})      // Bob has no UTXOs

		gasp1 := gasp.NewGASP(gasp.GASPParams{Storage: storage1})
		gasp2 := gasp.NewGASP(gasp.GASPParams{Storage: storage2})

		// Bob syncs from Alice
		gasp2.Remote = &mockGASPRemote{targetGASP: gasp1}

		// when
		err := gasp2.Sync(ctx, "test-host", 0)

		// then
		require.NoError(t, err)

		result1, _ := storage1.FindKnownUTXOs(ctx, 0, 0)
		result2, _ := storage2.FindKnownUTXOs(ctx, 0, 0)

		require.Len(t, result2, 1)
		require.Equal(t, len(result1), len(result2))
	})

	t.Run("should synchronize a single UTXO from Bob to Alice", func(t *testing.T) {
		// given
		ctx := context.Background()
		utxo1 := createMockUTXO("mock_sender1_rawtx1", 0, 111)

		storage1 := newMockGASPStorage([]*mockUTXO{})      // Alice has no UTXOs
		storage2 := newMockGASPStorage([]*mockUTXO{utxo1}) // Bob has UTXO

		gasp1 := gasp.NewGASP(gasp.GASPParams{Storage: storage1})
		gasp2 := gasp.NewGASP(gasp.GASPParams{Storage: storage2})

		gasp1.Remote = &mockGASPRemote{targetGASP: gasp2}

		// when
		err := gasp1.Sync(ctx, "test-host", 0)

		// then
		require.NoError(t, err)

		result1, _ := storage1.FindKnownUTXOs(ctx, 0, 0)
		result2, _ := storage2.FindKnownUTXOs(ctx, 0, 0)

		require.Len(t, result1, 1)
		require.Equal(t, len(result1), len(result2))
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

		gasp1 := gasp.NewGASP(gasp.GASPParams{Storage: storage1})
		gasp2 := gasp.NewGASP(gasp.GASPParams{Storage: storage2})

		// Bob syncs from Alice
		gasp2.Remote = &mockGASPRemote{targetGASP: gasp1}

		// when
		err := gasp2.Sync(ctx, "test-host", 0)

		// then
		require.NoError(t, err) // Sync should complete despite validation failure

		result2, _ := storage2.FindKnownUTXOs(ctx, 0, 0)
		require.Len(t, result2, 0) // No UTXOs should be synchronized
		require.True(t, discardGraphCalled)
	})

	t.Run("should synchronize multiple graphs", func(t *testing.T) {
		// given
		ctx := context.Background()
		utxo1 := createMockUTXO("mock_sender1_rawtx1", 0, 111)
		utxo2 := createMockUTXO("mock_sender2_rawtx1", 0, 222)

		storage1 := newMockGASPStorage([]*mockUTXO{utxo1, utxo2})
		storage2 := newMockGASPStorage([]*mockUTXO{})

		gasp1 := gasp.NewGASP(gasp.GASPParams{Storage: storage1})
		gasp2 := gasp.NewGASP(gasp.GASPParams{Storage: storage2})

		// Bob syncs from Alice
		gasp2.Remote = &mockGASPRemote{targetGASP: gasp1}

		// when
		err := gasp2.Sync(ctx, "test-host", 0)

		// then
		require.NoError(t, err)

		result1, _ := storage1.FindKnownUTXOs(ctx, 0, 0)
		result2, _ := storage2.FindKnownUTXOs(ctx, 0, 0)

		require.Len(t, result2, 2)
		require.Equal(t, len(result1), len(result2))
	})

	t.Run("should synchronize only UTXOs created after the specified since timestamp", func(t *testing.T) {
		// given
		ctx := context.Background()
		oldUTXO := createMockUTXO("old_rawtx", 0, 100) // Timestamp 100
		newUTXO := createMockUTXO("new_rawtx", 1, 200) // Timestamp 200

		storage1 := newMockGASPStorage([]*mockUTXO{oldUTXO, newUTXO})
		storage2 := newMockGASPStorage([]*mockUTXO{})

		gasp1 := gasp.NewGASP(gasp.GASPParams{
			Storage:         storage1,
			LastInteraction: 0,
		})
		gasp2 := gasp.NewGASP(gasp.GASPParams{
			Storage:         storage2,
			LastInteraction: 150, // Bob only wants UTXOs newer than 150
		})

		// Bob syncs from Alice (who has both old and new UTXOs)
		gasp2.Remote = &mockGASPRemote{targetGASP: gasp1}

		// when
		err := gasp2.Sync(ctx, "test-host", 0)

		// then
		require.NoError(t, err)

		result2, _ := storage2.FindKnownUTXOs(ctx, 0, 0)
		require.Len(t, result2, 1) // Only new UTXO should be synchronized

		// Verify it's the new UTXO
		require.Equal(t, newUTXO.GraphID.Index, result2[0].OutputIndex)
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

		gasp1 := gasp.NewGASP(gasp.GASPParams{Storage: storage1})
		gasp2 := gasp.NewGASP(gasp.GASPParams{Storage: storage2})

		gasp1.Remote = &mockGASPRemote{targetGASP: gasp2}

		// when
		err := gasp1.Sync(ctx, "test-host", 0)

		// then
		require.NoError(t, err)

		result1, _ := storage1.FindKnownUTXOs(ctx, 0, 0)
		result2, _ := storage2.FindKnownUTXOs(ctx, 0, 0)

		require.Len(t, result1, 1)
		require.Len(t, result2, 1)
		require.False(t, finalizeGraphCalled1, "FinalizeGraph should not be called when no sync needed")
		require.False(t, finalizeGraphCalled2, "FinalizeGraph should not be called when no sync needed")
	})
}

// Helper function to create int pointer
func intPtr(i int) *int {
	return &i
}
