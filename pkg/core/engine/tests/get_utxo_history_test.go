package engine_test

import (
	"context"
	"errors"
	"testing"

	"github.com/4chain-ag/go-overlay-services/pkg/core/engine"
	"github.com/bsv-blockchain/go-sdk/transaction"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEngine_GetUTXOHistory_ShouldReturnImmediateOutput_WhenSelectorIsNil(t *testing.T) {
	// given
	output := &engine.Output{Beef: []byte("beef")}
	sut := &engine.Engine{}

	// when
	result, err := sut.GetUTXOHistory(context.Background(), output, nil, 0)

	// then
	require.NoError(t, err)
	assert.Equal(t, output, result)
}

func TestEngine_GetUTXOHistory_ShouldReturnNil_WhenSelectorReturnsFalse(t *testing.T) {
	// given
	output := &engine.Output{Beef: []byte("beef")}
	sut := &engine.Engine{}

	historySelector := func(beef []byte, outputIndex uint32, currentDepth uint32) bool {
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
		Beef:            []byte("beef"),
		OutputsConsumed: nil,
	}
	sut := &engine.Engine{}

	historySelector := func(beef []byte, outputIndex uint32, currentDepth uint32) bool {
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

	childBeef := createDummyBEEF(t)
	parentBeef := createDummyBEEF(t)

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
			findOutputFunc: func(ctx context.Context, outpoint *transaction.Outpoint, topic *string, spent *bool, includeBEEF bool) (*engine.Output, error) {
				if outpoint.String() == childOutpoint.String() {
					return childOutput, nil
				}
				return nil, errors.New("unexpected output")
			},
		},
	}

	historySelector := func(beef []byte, outputIndex uint32, currentDepth uint32) bool {
		return true
	}

	// when
	result, err := sut.GetUTXOHistory(ctx, parentOutput, historySelector, 0)

	// then
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.NotEmpty(t, result.Beef)
}

func TestEngine_GetUTXOHistory_ShouldReturnError_WhenStorageFails(t *testing.T) {
	// given
	ctx := context.Background()

	parentOutpoint := &transaction.Outpoint{Txid: fakeTxID(t), Index: 0}
	childOutpoint := &transaction.Outpoint{Txid: fakeTxID(t), Index: 1}

	parentOutput := &engine.Output{
		Outpoint:        *parentOutpoint,
		Beef:            []byte("parent beef"),
		OutputsConsumed: []*transaction.Outpoint{childOutpoint},
	}

	sut := &engine.Engine{
		Storage: fakeStorage{
			findOutputFunc: func(ctx context.Context, outpoint *transaction.Outpoint, topic *string, spent *bool, includeBEEF bool) (*engine.Output, error) {
				return nil, errors.New("storage error")
			},
		},
	}

	historySelector := func(beef []byte, outputIndex uint32, currentDepth uint32) bool {
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

	// Create a chain of 3 outputs
	output3 := &engine.Output{
		Outpoint: transaction.Outpoint{Txid: fakeTxID(t), Index: 3},
		Beef:     createDummyBEEF(t),
	}

	output2 := &engine.Output{
		Outpoint:        transaction.Outpoint{Txid: fakeTxID(t), Index: 2},
		Beef:            createDummyBEEF(t),
		OutputsConsumed: []*transaction.Outpoint{&output3.Outpoint},
	}

	output1 := &engine.Output{
		Outpoint:        transaction.Outpoint{Txid: fakeTxID(t), Index: 1},
		Beef:            createDummyBEEF(t),
		OutputsConsumed: []*transaction.Outpoint{&output2.Outpoint},
	}

	sut := &engine.Engine{
		Storage: fakeStorage{
			findOutputFunc: func(ctx context.Context, outpoint *transaction.Outpoint, topic *string, spent *bool, includeBEEF bool) (*engine.Output, error) {
				switch outpoint.Index {
				case 2:
					return output2, nil
				case 3:
					return output3, nil
				default:
					return nil, errors.New("unexpected output")
				}
			},
		},
	}

	// History selector that stops at depth 2
	historySelector := func(beef []byte, outputIndex uint32, currentDepth uint32) bool {
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

	// Create multiple consumed outputs
	consumed1 := &engine.Output{
		Outpoint: transaction.Outpoint{Txid: fakeTxID(t), Index: 10},
		Beef:     createDummyBEEF(t),
	}

	consumed2 := &engine.Output{
		Outpoint: transaction.Outpoint{Txid: fakeTxID(t), Index: 11},
		Beef:     createDummyBEEF(t),
	}

	parentOutput := &engine.Output{
		Outpoint: transaction.Outpoint{Txid: fakeTxID(t), Index: 1},
		Beef:     createDummyBEEF(t),
		OutputsConsumed: []*transaction.Outpoint{
			&consumed1.Outpoint,
			&consumed2.Outpoint,
		},
	}

	findOutputCallCount := 0
	sut := &engine.Engine{
		Storage: fakeStorage{
			findOutputFunc: func(ctx context.Context, outpoint *transaction.Outpoint, topic *string, spent *bool, includeBEEF bool) (*engine.Output, error) {
				findOutputCallCount++
				switch outpoint.Index {
				case 10:
					return consumed1, nil
				case 11:
					return consumed2, nil
				default:
					return nil, errors.New("unexpected output")
				}
			},
		},
	}

	historySelector := func(beef []byte, outputIndex uint32, currentDepth uint32) bool {
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
		Beef:            createDummyBEEF(t),
		OutputsConsumed: []*transaction.Outpoint{output2},
	}

	output2Data := &engine.Output{
		Outpoint:        *output2,
		Beef:            createDummyBEEF(t),
		OutputsConsumed: []*transaction.Outpoint{output1}, // Circular reference
	}

	maxCalls := 10
	callCount := 0
	sut := &engine.Engine{
		Storage: fakeStorage{
			findOutputFunc: func(ctx context.Context, outpoint *transaction.Outpoint, topic *string, spent *bool, includeBEEF bool) (*engine.Output, error) {
				callCount++
				if callCount > maxCalls {
					// Prevent infinite loop in test
					return nil, errors.New("max calls exceeded")
				}

				switch outpoint.Index {
				case 1:
					return output1Data, nil
				case 2:
					return output2Data, nil
				default:
					return nil, errors.New("unexpected output")
				}
			},
		},
	}

	historySelector := func(beef []byte, outputIndex uint32, currentDepth uint32) bool {
		// Limit depth to prevent infinite recursion
		return currentDepth < 5
	}

	// when
	result, err := sut.GetUTXOHistory(ctx, output1Data, historySelector, 0)

	// then
	// Should handle gracefully without infinite recursion
	assert.True(t, err != nil || result != nil)
	assert.True(t, callCount <= maxCalls)
}

func TestEngine_GetUTXOHistory_ShouldHandleEmptyOutputsConsumed(t *testing.T) {
	// given
	output := &engine.Output{
		Beef:            []byte("beef"),
		OutputsConsumed: []*transaction.Outpoint{}, // Empty slice
	}
	sut := &engine.Engine{}

	historySelector := func(beef []byte, outputIndex uint32, currentDepth uint32) bool {
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

	expectedBeef := []byte("expected beef")
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
	historySelector := func(beef []byte, outputIndex uint32, currentDepth uint32) bool {
		selectorCalled = true
		assert.Equal(t, expectedBeef, beef)
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
