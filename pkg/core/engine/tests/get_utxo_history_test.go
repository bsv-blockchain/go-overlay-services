package engine_test

import (
	"context"
	"errors"
	"testing"

	"github.com/bsv-blockchain/go-sdk/chainhash"
	"github.com/bsv-blockchain/go-sdk/script"
	"github.com/bsv-blockchain/go-sdk/transaction"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/engine"
)

var (
	errUnexpectedOutput = errors.New("unexpected output")
	errStorageError     = errors.New("storage error")
	errMaxCallsExceeded = errors.New("max calls exceeded")
)

// Helper to create a simple test beef (empty, for tests that don't traverse history)
func createTestBeef() *transaction.Beef {
	return &transaction.Beef{
		Version:      transaction.BEEF_V2,
		Transactions: make(map[chainhash.Hash]*transaction.BeefTx),
	}
}

// Helper to create a BEEF containing a transaction with the given txid
// This creates a transaction with no inputs (coinbase-like) since GetUTXOHistory
// will try to rebuild BEEF and expects all source transactions to be present.
// The OutputsConsumed field on Output tracks the logical chain, not the tx inputs.
func createBeefWithTransaction(t *testing.T, txid *chainhash.Hash) *transaction.Beef {
	t.Helper()
	tx := transaction.NewTransaction()

	// Add a dummy output (no inputs needed for test)
	tx.AddOutput(&transaction.TransactionOutput{
		Satoshis:      1000,
		LockingScript: &script.Script{},
	})

	beef := &transaction.Beef{
		Version:      transaction.BEEF_V2,
		Transactions: make(map[chainhash.Hash]*transaction.BeefTx),
	}
	beef.Transactions[*txid] = &transaction.BeefTx{
		Transaction: tx,
	}
	return beef
}

func TestEngine_GetUTXOHistory_ShouldReturnImmediateOutput_WhenSelectorIsNil(t *testing.T) {
	// given
	output := &engine.Output{Beef: createTestBeef()}
	sut := &engine.Engine{}

	// when
	result, err := sut.GetUTXOHistory(context.Background(), output, nil, 0)

	// then
	require.NoError(t, err)
	assert.Equal(t, output, result)
}

func TestEngine_GetUTXOHistory_ShouldReturnNil_WhenSelectorReturnsFalse(t *testing.T) {
	// given
	output := &engine.Output{Beef: createTestBeef()}
	sut := &engine.Engine{}

	historySelector := func(_ *transaction.Beef, _, _ uint32) bool {
		return false
	}

	// when
	result, err := sut.GetUTXOHistory(context.Background(), output, historySelector, 0)

	// then
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestEngine_GetUTXOHistory_ShouldReturnOutput_WhenNoOutputsConsumed(t *testing.T) {
	// given
	output := &engine.Output{
		Beef:            createTestBeef(),
		OutputsConsumed: nil,
	}
	sut := &engine.Engine{}

	historySelector := func(_ *transaction.Beef, _, _ uint32) bool {
		return true
	}

	// when
	result, err := sut.GetUTXOHistory(context.Background(), output, historySelector, 0)

	// then
	require.NoError(t, err)
	assert.Equal(t, output, result)
}

func TestEngine_GetUTXOHistory_ShouldTravelRecursively_WhenOutputsConsumedPresent(t *testing.T) {
	// given
	ctx := context.Background()

	parentOutpoint := &transaction.Outpoint{Txid: fakeTxID(t), Index: 0}
	childOutpoint := &transaction.Outpoint{Txid: fakeTxID(t), Index: 1}

	// Child has no inputs (leaf node)
	childBeef := createBeefWithTransaction(t, &childOutpoint.Txid)
	// Parent consumes the child output (OutputsConsumed tracks the logical chain)
	parentBeef := createBeefWithTransaction(t, &parentOutpoint.Txid)

	childOutput := &engine.Output{
		Outpoint: *childOutpoint,
		Beef:     childBeef,
	}
	parentOutput := &engine.Output{
		Outpoint:        *parentOutpoint,
		Beef:            parentBeef,
		OutputsConsumed: []*transaction.Outpoint{childOutpoint},
	}

	sut := &engine.Engine{
		Storage: fakeStorage{
			findOutputFunc: func(_ context.Context, outpoint *transaction.Outpoint, _ *string, _ *bool, _ bool) (*engine.Output, error) {
				if outpoint.String() == childOutpoint.String() {
					return childOutput, nil
				}
				return nil, errUnexpectedOutput
			},
		},
	}

	historySelector := func(_ *transaction.Beef, _, _ uint32) bool {
		return true
	}

	// when
	result, err := sut.GetUTXOHistory(ctx, parentOutput, historySelector, 0)

	// then
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.NotNil(t, result.Beef)
}

func TestEngine_GetUTXOHistory_ShouldReturnError_WhenStorageFails(t *testing.T) {
	// given
	ctx := context.Background()

	parentOutpoint := &transaction.Outpoint{Txid: fakeTxID(t), Index: 0}
	childOutpoint := &transaction.Outpoint{Txid: fakeTxID(t), Index: 1}

	parentOutput := &engine.Output{
		Outpoint:        *parentOutpoint,
		Beef:            createTestBeef(),
		OutputsConsumed: []*transaction.Outpoint{childOutpoint},
	}

	sut := &engine.Engine{
		Storage: fakeStorage{
			findOutputFunc: func(_ context.Context, _ *transaction.Outpoint, _ *string, _ *bool, _ bool) (*engine.Output, error) {
				return nil, errStorageError
			},
		},
	}

	historySelector := func(_ *transaction.Beef, _, _ uint32) bool {
		return true
	}

	// when
	result, err := sut.GetUTXOHistory(ctx, parentOutput, historySelector, 0)

	// then
	require.Error(t, err)
	assert.Nil(t, result)
	assert.EqualError(t, err, "storage error")
}

func TestEngine_GetUTXOHistory_ShouldRespectDepthInHistorySelector(t *testing.T) {
	// given
	ctx := context.Background()

	// Create a chain of 3 outputs with proper txids
	txid3 := fakeTxID(t)
	txid2 := fakeTxID(t)
	txid1 := fakeTxID(t)

	outpoint3 := transaction.Outpoint{Txid: txid3, Index: 3}
	outpoint2 := transaction.Outpoint{Txid: txid2, Index: 2}
	outpoint1 := transaction.Outpoint{Txid: txid1, Index: 1}

	// output3 is a leaf node (no inputs)
	output3 := &engine.Output{
		Outpoint: outpoint3,
		Beef:     createBeefWithTransaction(t, &txid3),
	}

	// output2 consumes output3
	output2 := &engine.Output{
		Outpoint:        outpoint2,
		Beef:            createBeefWithTransaction(t, &txid2),
		OutputsConsumed: []*transaction.Outpoint{&output3.Outpoint},
	}

	// output1 consumes output2
	output1 := &engine.Output{
		Outpoint:        outpoint1,
		Beef:            createBeefWithTransaction(t, &txid1),
		OutputsConsumed: []*transaction.Outpoint{&output2.Outpoint},
	}

	sut := &engine.Engine{
		Storage: fakeStorage{
			findOutputFunc: func(_ context.Context, outpoint *transaction.Outpoint, _ *string, _ *bool, _ bool) (*engine.Output, error) {
				switch outpoint.String() {
				case output2.Outpoint.String():
					return output2, nil
				case output3.Outpoint.String():
					return output3, nil
				default:
					return nil, errUnexpectedOutput
				}
			},
		},
	}

	// History selector that stops at depth 2
	historySelector := func(_ *transaction.Beef, _, currentDepth uint32) bool {
		return currentDepth < 2
	}

	// when
	result, err := sut.GetUTXOHistory(ctx, output1, historySelector, 0)

	// then
	require.NoError(t, err)
	assert.NotNil(t, result)
	// Should traverse to output3 (depth 0 -> 1 -> 2, stops at 2)
}

func TestEngine_GetUTXOHistory_ShouldHandleMultipleOutputsConsumed(t *testing.T) {
	// given
	ctx := context.Background()

	// Create txids for all outputs
	txid1 := fakeTxID(t)
	txid2 := fakeTxID(t)
	parentTxid := fakeTxID(t)

	// Create multiple consumed outputs (leaf nodes)
	consumed1 := &engine.Output{
		Outpoint: transaction.Outpoint{Txid: txid1, Index: 10},
		Beef:     createBeefWithTransaction(t, &txid1),
	}

	consumed2 := &engine.Output{
		Outpoint: transaction.Outpoint{Txid: txid2, Index: 11},
		Beef:     createBeefWithTransaction(t, &txid2),
	}

	// Parent consumes both outputs
	parentOutput := &engine.Output{
		Outpoint: transaction.Outpoint{Txid: parentTxid, Index: 1},
		Beef:     createBeefWithTransaction(t, &parentTxid),
		OutputsConsumed: []*transaction.Outpoint{
			&consumed1.Outpoint,
			&consumed2.Outpoint,
		},
	}

	findOutputCallCount := 0
	sut := &engine.Engine{
		Storage: fakeStorage{
			findOutputFunc: func(_ context.Context, outpoint *transaction.Outpoint, _ *string, _ *bool, _ bool) (*engine.Output, error) {
				findOutputCallCount++
				switch outpoint.String() {
				case consumed1.Outpoint.String():
					return consumed1, nil
				case consumed2.Outpoint.String():
					return consumed2, nil
				default:
					return nil, errUnexpectedOutput
				}
			},
		},
	}

	historySelector := func(_ *transaction.Beef, _, _ uint32) bool {
		return true
	}

	// when
	result, err := sut.GetUTXOHistory(ctx, parentOutput, historySelector, 0)

	// then
	require.NoError(t, err)
	assert.NotNil(t, result)
	// Should process all consumed outputs
	assert.Equal(t, 2, findOutputCallCount)
}

func TestEngine_GetUTXOHistory_ShouldHandleCircularReferences(t *testing.T) {
	// given
	ctx := context.Background()

	// Create outputs that reference each other (which shouldn't happen in practice)
	output1 := &transaction.Outpoint{Txid: fakeTxID(t), Index: 1}
	output2 := &transaction.Outpoint{Txid: fakeTxID(t), Index: 2}

	output1Data := &engine.Output{
		Outpoint:        *output1,
		Beef:            createTestBeef(),
		OutputsConsumed: []*transaction.Outpoint{output2},
	}

	output2Data := &engine.Output{
		Outpoint:        *output2,
		Beef:            createTestBeef(),
		OutputsConsumed: []*transaction.Outpoint{output1}, // Circular reference
	}

	maxCalls := 10
	callCount := 0
	sut := &engine.Engine{
		Storage: fakeStorage{
			findOutputFunc: func(_ context.Context, outpoint *transaction.Outpoint, _ *string, _ *bool, _ bool) (*engine.Output, error) {
				callCount++
				if callCount > maxCalls {
					// Prevent infinite loop in test
					return nil, errMaxCallsExceeded
				}

				switch outpoint.Index {
				case 1:
					return output1Data, nil
				case 2:
					return output2Data, nil
				default:
					return nil, errUnexpectedOutput
				}
			},
		},
	}

	historySelector := func(_ *transaction.Beef, _, currentDepth uint32) bool {
		// Limit depth to prevent infinite recursion
		return currentDepth < 5
	}

	// when
	result, err := sut.GetUTXOHistory(ctx, output1Data, historySelector, 0)

	// then
	// Should handle gracefully without infinite recursion
	assert.True(t, err != nil || result != nil)
	assert.LessOrEqual(t, callCount, maxCalls)
}

func TestEngine_GetUTXOHistory_ShouldHandleEmptyOutputsConsumed(t *testing.T) {
	// given
	output := &engine.Output{
		Beef:            createTestBeef(),
		OutputsConsumed: []*transaction.Outpoint{}, // Empty slice
	}
	sut := &engine.Engine{}

	historySelector := func(_ *transaction.Beef, _, _ uint32) bool {
		return true
	}

	// when
	result, err := sut.GetUTXOHistory(context.Background(), output, historySelector, 0)

	// then
	require.NoError(t, err)
	assert.Equal(t, output, result)
}

func TestEngine_GetUTXOHistory_ShouldInvokeHistorySelectorWithCorrectParameters(t *testing.T) {
	// given
	ctx := context.Background()

	expectedBeef := createTestBeef()
	expectedOutputIndex := uint32(42)
	initialDepth := uint32(3)

	output := &engine.Output{
		Beef: expectedBeef,
		Outpoint: transaction.Outpoint{
			Index: expectedOutputIndex,
		},
	}
	sut := &engine.Engine{}

	selectorCalled := false
	historySelector := func(beef *transaction.Beef, outputIndex, currentDepth uint32) bool {
		selectorCalled = true
		assert.NotNil(t, beef)
		assert.Equal(t, expectedOutputIndex, outputIndex)
		assert.Equal(t, initialDepth, currentDepth)
		return false
	}

	// when
	result, err := sut.GetUTXOHistory(ctx, output, historySelector, initialDepth)

	// then
	require.NoError(t, err)
	assert.True(t, selectorCalled)
	assert.Nil(t, result)
}
