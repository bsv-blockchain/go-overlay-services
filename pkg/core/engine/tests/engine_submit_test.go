package engine_test

import (
	"context"
	"errors"
	"testing"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/engine"
	"github.com/bsv-blockchain/go-sdk/chainhash"
	"github.com/bsv-blockchain/go-sdk/overlay"
	"github.com/bsv-blockchain/go-sdk/script"
	"github.com/bsv-blockchain/go-sdk/transaction"
	"github.com/stretchr/testify/require"
)

var errInsertFailed = errors.New("insert-failed")

func TestEngine_Submit_Success(t *testing.T) {
	// given:
	ctx := context.Background()

	sut := &engine.Engine{
		Managers: map[string]engine.TopicManager{
			"test-topic": fakeManager{
				identifyAdmissibleOutputsFunc: func(_ context.Context, _ []byte, _ map[uint32]*transaction.TransactionOutput) (overlay.AdmittanceInstructions, error) {
					return overlay.AdmittanceInstructions{
						OutputsToAdmit: []uint32{0},
					}, nil
				},
			},
		},
		Storage: fakeStorage{
			deleteOutputFunc: func(_ context.Context, _ *transaction.Outpoint, _ string) error {
				return nil
			},
			findOutputFunc: func(_ context.Context, _ *transaction.Outpoint, _ *string, _ *bool, _ bool) (*engine.Output, error) {
				return &engine.Output{}, nil
			},
			findOutputsFunc: func(_ context.Context, _ []*transaction.Outpoint, _ string, _ *bool, _ bool) ([]*engine.Output, error) {
				return []*engine.Output{{}}, nil
			},
			doesAppliedTransactionExistFunc: func(_ context.Context, _ *overlay.AppliedTransaction) (bool, error) {
				return false, nil
			},
			markUTXOsAsSpentFunc: func(_ context.Context, _ []*transaction.Outpoint, _ string, _ *chainhash.Hash) error {
				return nil
			},
			insertOutputFunc: func(_ context.Context, _ *engine.Output) error {
				return nil
			},
			insertAppliedTransactionFunc: func(_ context.Context, _ *overlay.AppliedTransaction) error {
				return nil
			},
		},
		ChainTracker: fakeChainTracker{
			isValidRootForHeight: func(_ context.Context, _ *chainhash.Hash, _ uint32) (bool, error) {
				return true, nil
			},
		},
	}

	taggedBEEF := overlay.TaggedBEEF{
		Topics: []string{"test-topic"},
		Beef:   createDummyBEEF(t),
	}

	expectedSteak := overlay.Steak{
		"test-topic": &overlay.AdmittanceInstructions{
			OutputsToAdmit: []uint32{0},
			CoinsRemoved:   []uint32{0},
		},
	}

	// when:
	steak, err := sut.Submit(ctx, taggedBEEF, engine.SubmitModeCurrent, nil)

	// then:
	require.NoError(t, err)
	require.Equal(t, expectedSteak, steak)
}

func TestEngine_Submit_InvalidBeef_ShouldReturnError(t *testing.T) {
	// given:
	ctx := context.Background()
	sut := &engine.Engine{
		Managers: map[string]engine.TopicManager{
			"test-topic": fakeManager{
				identifyAdmissibleOutputsFunc: func(_ context.Context, _ []byte, _ map[uint32]*transaction.TransactionOutput) (overlay.AdmittanceInstructions, error) {
					return overlay.AdmittanceInstructions{
						OutputsToAdmit: []uint32{0},
					}, nil
				},
			},
		},
		Storage:      fakeStorage{},
		ChainTracker: fakeChainTracker{},
	}

	taggedBEEF := overlay.TaggedBEEF{
		Topics: []string{"test-topic"},
		Beef:   []byte{0xFF}, // invalid beef
	}

	// when:
	steak, err := sut.Submit(ctx, taggedBEEF, engine.SubmitModeCurrent, nil)

	// then:
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid-version") // temp fix for SPV failure Submit need to be fixed by wrapping the error to use ErrorIs
	require.Nil(t, steak)
}

func TestEngine_Submit_SPVFail_ShouldReturnError(t *testing.T) {
	// given:
	ctx := context.Background()
	sut := &engine.Engine{
		Managers: map[string]engine.TopicManager{
			"test-topic": fakeManager{
				identifyAdmissibleOutputsFunc: func(_ context.Context, _ []byte, _ map[uint32]*transaction.TransactionOutput) (overlay.AdmittanceInstructions, error) {
					return overlay.AdmittanceInstructions{
						OutputsToAdmit: []uint32{0},
					}, nil
				},
			},
		},
		Storage: fakeStorage{
			findOutputFunc: func(_ context.Context, outpoint *transaction.Outpoint, _ *string, _ *bool, _ bool) (*engine.Output, error) {
				return &engine.Output{
					Outpoint: *outpoint,
					Satoshis: 1000,
					Script:   &script.Script{script.OpTRUE},
				}, nil
			},
			findOutputsFunc: func(_ context.Context, outpoints []*transaction.Outpoint, _ string, _ *bool, _ bool) ([]*engine.Output, error) {
				return []*engine.Output{
					{
						Outpoint: *outpoints[0],
						Satoshis: 1000,
						Script:   &script.Script{script.OpTRUE},
					},
				}, nil
			},
		},
		ChainTracker: fakeChainTrackerSPVFail{},
	}

	taggedBEEF := overlay.TaggedBEEF{
		Topics: []string{"test-topic"},
		Beef:   createDummyBeefWithInputs(t),
	}

	// when:
	steak, err := sut.Submit(ctx, taggedBEEF, engine.SubmitModeCurrent, nil)

	// then:
	require.Error(t, err)
	require.Equal(t, "input 0 has no source transaction", err.Error()) // temp fix for SPV failure Submit need to be fixed by wrapping the error to use ErrorIs
	require.Nil(t, steak)
}

func TestEngine_Submit_DuplicateTransaction_ShouldReturnEmptySteak(t *testing.T) {
	// given:
	ctx := context.Background()
	sut := &engine.Engine{
		Managers: map[string]engine.TopicManager{
			"test-topic": fakeManager{},
		},
		Storage: fakeStorage{
			doesAppliedTransactionExistFunc: func(_ context.Context, _ *overlay.AppliedTransaction) (bool, error) {
				return true, nil
			},
		},
		ChainTracker: fakeChainTracker{
			isValidRootForHeight: func(_ context.Context, _ *chainhash.Hash, _ uint32) (bool, error) {
				return true, nil
			},
		},
	}
	taggedBEEF := overlay.TaggedBEEF{
		Topics: []string{"test-topic"},
		Beef:   createDummyBEEF(t),
	}

	expectedSteak := overlay.Steak{
		"test-topic": &overlay.AdmittanceInstructions{
			OutputsToAdmit: nil,
		},
	}

	// when:
	steak, err := sut.Submit(ctx, taggedBEEF, engine.SubmitModeCurrent, nil)

	// then:
	require.NoError(t, err)
	require.Equal(t, expectedSteak, steak)
}

func TestEngine_Submit_MissingTopic_ShouldReturnError(t *testing.T) {
	// given:
	ctx := context.Background()
	sut := &engine.Engine{
		Managers:     map[string]engine.TopicManager{},
		Storage:      fakeStorage{},
		ChainTracker: fakeChainTracker{},
	}
	taggedBEEF := overlay.TaggedBEEF{
		Topics: []string{"unknown-topic"},
		Beef:   createDummyBEEF(t),
	}

	// when:
	steak, err := sut.Submit(ctx, taggedBEEF, engine.SubmitModeCurrent, nil)

	// then:
	require.ErrorIs(t, err, engine.ErrUnknownTopic)
	require.Nil(t, steak)
}

func TestEngine_Submit_BroadcastFails_ShouldReturnError(t *testing.T) {
	// given:
	ctx := context.Background()
	sut := &engine.Engine{
		Managers: map[string]engine.TopicManager{
			"test-topic": fakeManager{
				identifyAdmissibleOutputsFunc: func(_ context.Context, _ []byte, _ map[uint32]*transaction.TransactionOutput) (overlay.AdmittanceInstructions, error) {
					return overlay.AdmittanceInstructions{
						OutputsToAdmit: []uint32{0},
					}, nil
				},
			},
		},
		Storage: fakeStorage{
			findOutputFunc: func(_ context.Context, _ *transaction.Outpoint, _ *string, _ *bool, _ bool) (*engine.Output, error) {
				return &engine.Output{}, nil
			},
			findOutputsFunc: func(_ context.Context, _ []*transaction.Outpoint, _ string, _ *bool, _ bool) ([]*engine.Output, error) {
				return []*engine.Output{{}}, nil
			},
			doesAppliedTransactionExistFunc: func(_ context.Context, _ *overlay.AppliedTransaction) (bool, error) {
				return false, nil
			},
			markUTXOsAsSpentFunc: func(_ context.Context, _ []*transaction.Outpoint, _ string, _ *chainhash.Hash) error {
				return nil
			},
		},
		ChainTracker: fakeChainTracker{
			verifyFunc: func(_ *transaction.Transaction, _ ...any) (bool, error) {
				return true, nil
			},
			isValidRootForHeight: func(_ context.Context, _ *chainhash.Hash, _ uint32) (bool, error) {
				return true, nil
			},
		},
		Broadcaster: fakeBroadcasterFail{
			broadcastFunc: func(_ *transaction.Transaction) (*transaction.BroadcastSuccess, *transaction.BroadcastFailure) {
				return nil, &transaction.BroadcastFailure{Description: "forced failure for testing"}
			},
			broadcastCtxFunc: func(_ context.Context, _ *transaction.Transaction) (*transaction.BroadcastSuccess, *transaction.BroadcastFailure) {
				return nil, &transaction.BroadcastFailure{Description: "forced failure for testing"}
			},
		},
	}

	taggedBEEF := overlay.TaggedBEEF{
		Topics: []string{"test-topic"},
		Beef:   createDummyBEEF(t),
	}

	// when:
	steak, err := sut.Submit(ctx, taggedBEEF, engine.SubmitModeCurrent, nil)

	// then:
	require.Error(t, err)
	require.Nil(t, steak)
	require.EqualError(t, err, "forced failure for testing")
}

func TestEngine_Submit_OutputInsertFails_ShouldReturnError(t *testing.T) {
	// given:
	ctx := context.Background()
	taggedBEEF, prevTxID := createDummyValidTaggedBEEF(t)

	sut := &engine.Engine{
		Managers: map[string]engine.TopicManager{
			"test-topic": fakeManager{
				identifyAdmissibleOutputsFunc: func(_ context.Context, _ []byte, _ map[uint32]*transaction.TransactionOutput) (overlay.AdmittanceInstructions, error) {
					return overlay.AdmittanceInstructions{
						OutputsToAdmit: []uint32{0},
					}, nil
				},
			},
		},
		Storage: fakeStorage{
			findOutputFunc: func(_ context.Context, _ *transaction.Outpoint, _ *string, _ *bool, _ bool) (*engine.Output, error) {
				return &engine.Output{
					Outpoint: transaction.Outpoint{
						Txid:  *prevTxID,
						Index: 0,
					},
					Satoshis: 1000,
					Script:   &script.Script{script.OpTRUE},
					Topic:    "test-topic",
				}, nil
			},
			findOutputsFunc: func(_ context.Context, _ []*transaction.Outpoint, _ string, _ *bool, _ bool) ([]*engine.Output, error) {
				return []*engine.Output{
					{
						Outpoint: transaction.Outpoint{
							Txid:  *prevTxID,
							Index: 0,
						},
						Satoshis: 1000,
						Script:   &script.Script{script.OpTRUE},
						Topic:    "test-topic",
					},
				}, nil
			},
			doesAppliedTransactionExistFunc: func(_ context.Context, _ *overlay.AppliedTransaction) (bool, error) {
				return false, nil
			},
			markUTXOsAsSpentFunc: func(_ context.Context, _ []*transaction.Outpoint, _ string, _ *chainhash.Hash) error {
				return nil
			},
			insertOutputFunc: func(_ context.Context, _ *engine.Output) error {
				return errInsertFailed
			},
			deleteOutputFunc: func(_ context.Context, _ *transaction.Outpoint, _ string) error {
				return nil
			},
		},
		ChainTracker: fakeChainTracker{},
	}

	// when:
	steak, err := sut.Submit(ctx, taggedBEEF, engine.SubmitModeCurrent, nil)

	// then:
	require.ErrorIs(t, err, errInsertFailed)
	require.Nil(t, steak)
}
