package engine_test

import (
	"context"
	"errors"
	"testing"

	"github.com/bsv-blockchain/go-sdk/chainhash"
	"github.com/bsv-blockchain/go-sdk/overlay"
	"github.com/bsv-blockchain/go-sdk/transaction"
	"github.com/stretchr/testify/require"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/engine"
)

var emptyBeef = &transaction.Beef{
	Version:      transaction.BEEF_V2,
	BUMPs:        []*transaction.MerklePath{},
	Transactions: make(map[chainhash.Hash]*transaction.BeefTx),
}

func TestEngine_Submit_Success(t *testing.T) {
	// given:
	ctx := context.Background()

	sut := engine.NewEngine(&engine.EngineConfig{
		Managers: map[string]engine.TopicManager{
			"test-topic": fakeManager{
				identifyAdmissibleOutputsFunc: func(ctx context.Context, beef *transaction.Beef, txid *chainhash.Hash, previousCoins []uint32) (overlay.AdmittanceInstructions, error) {
					return overlay.AdmittanceInstructions{
						OutputsToAdmit: []uint32{0},
					}, nil
				},
			},
		},
		Storage: fakeStorage{
			deleteOutputFunc: func(ctx context.Context, outpoint *transaction.Outpoint, topic string) error {
				return nil
			},
			findOutputFunc: func(ctx context.Context, outpoint *transaction.Outpoint, topic *string, spent *bool, includeBEEF bool) (*engine.Output, error) {
				return &engine.Output{Beef: emptyBeef}, nil
			},
			findOutputsFunc: func(ctx context.Context, outpoints []*transaction.Outpoint, topic string, spent *bool, includeBEEF bool) ([]*engine.Output, error) {
				return []*engine.Output{{Beef: emptyBeef}}, nil
			},
			doesAppliedTransactionExistFunc: func(ctx context.Context, tx *overlay.AppliedTransaction) (bool, error) {
				return false, nil
			},
			markUTXOsAsSpentFunc: func(ctx context.Context, outpoints []*transaction.Outpoint, topic string, spendTxid *chainhash.Hash) error {
				return nil
			},
			insertOutputsFunc: func(ctx context.Context, topic string, txid *chainhash.Hash, outputs []uint32, outpointsConsumed []*transaction.Outpoint, beef *transaction.Beef, ancillaryTxids []*chainhash.Hash) error {
				return nil
			},
			insertAppliedTransactionFunc: func(ctx context.Context, tx *overlay.AppliedTransaction) error {
				return nil
			},
		},
		ChainTracker: fakeChainTracker{
			isValidRootForHeight: func(ctx context.Context, root *chainhash.Hash, height uint32) (bool, error) {
				return true, nil
			},
		},
	})

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
	sut := engine.NewEngine(&engine.EngineConfig{
		Managers: map[string]engine.TopicManager{
			"test-topic": fakeManager{
				identifyAdmissibleOutputsFunc: func(ctx context.Context, beef *transaction.Beef, txid *chainhash.Hash, previousCoins []uint32) (overlay.AdmittanceInstructions, error) {
					return overlay.AdmittanceInstructions{
						OutputsToAdmit: []uint32{0},
					}, nil
				},
			},
		},
		Storage:      fakeStorage{},
		ChainTracker: fakeChainTracker{},
	})

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
	sut := engine.NewEngine(&engine.EngineConfig{
		Managers: map[string]engine.TopicManager{
			"test-topic": fakeManager{
				identifyAdmissibleOutputsFunc: func(ctx context.Context, beef *transaction.Beef, txid *chainhash.Hash, previousCoins []uint32) (overlay.AdmittanceInstructions, error) {
					return overlay.AdmittanceInstructions{
						OutputsToAdmit: []uint32{0},
					}, nil
				},
			},
		},
		Storage: fakeStorage{
			findOutputFunc: func(ctx context.Context, outpoint *transaction.Outpoint, topic *string, spent *bool, includeBEEF bool) (*engine.Output, error) {
				return &engine.Output{
					Outpoint: *outpoint,
				}, nil
			},
			findOutputsFunc: func(ctx context.Context, outpoints []*transaction.Outpoint, topic string, spent *bool, includeBEEF bool) ([]*engine.Output, error) {
				return []*engine.Output{
					{
						Outpoint: *outpoints[0],
					},
				}, nil
			},
		},
		ChainTracker: fakeChainTrackerSPVFail{},
	})

	taggedBEEF := overlay.TaggedBEEF{
		Topics: []string{"test-topic"},
		Beef:   createDummyBeefWithInputs(t),
	}

	// when:
	steak, err := sut.Submit(ctx, taggedBEEF, engine.SubmitModeCurrent, nil)

	// then:
	require.Error(t, err)
	require.Equal(t, "missing source transaction: input 0", err.Error()) // temp fix for SPV failure Submit need to be fixed by wrapping the error to use ErrorIs
	require.Nil(t, steak)
}

func TestEngine_Submit_DuplicateTransaction_ShouldReturnEmptySteak(t *testing.T) {
	// given:
	ctx := context.Background()
	sut := engine.NewEngine(&engine.EngineConfig{
		Managers: map[string]engine.TopicManager{
			"test-topic": fakeManager{},
		},
		Storage: fakeStorage{
			doesAppliedTransactionExistFunc: func(ctx context.Context, tx *overlay.AppliedTransaction) (bool, error) {
				return true, nil
			},
		},
		ChainTracker: fakeChainTracker{
			isValidRootForHeight: func(ctx context.Context, root *chainhash.Hash, height uint32) (bool, error) {
				return true, nil
			},
		},
	})
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
	sut := engine.NewEngine(&engine.EngineConfig{
		Managers:     map[string]engine.TopicManager{},
		Storage:      fakeStorage{},
		ChainTracker: fakeChainTracker{},
	})
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
	sut := engine.NewEngine(&engine.EngineConfig{
		Managers: map[string]engine.TopicManager{
			"test-topic": fakeManager{
				identifyAdmissibleOutputsFunc: func(ctx context.Context, beef *transaction.Beef, txid *chainhash.Hash, previousCoins []uint32) (overlay.AdmittanceInstructions, error) {
					return overlay.AdmittanceInstructions{
						OutputsToAdmit: []uint32{0},
					}, nil
				},
			},
		},
		Storage: fakeStorage{
			findOutputFunc: func(ctx context.Context, outpoint *transaction.Outpoint, topic *string, spent *bool, includeBEEF bool) (*engine.Output, error) {
				return &engine.Output{Beef: emptyBeef}, nil
			},
			findOutputsFunc: func(ctx context.Context, outpoints []*transaction.Outpoint, topic string, spent *bool, includeBEEF bool) ([]*engine.Output, error) {
				return []*engine.Output{{Beef: emptyBeef}}, nil
			},
			doesAppliedTransactionExistFunc: func(ctx context.Context, tx *overlay.AppliedTransaction) (bool, error) {
				return false, nil
			},
			markUTXOsAsSpentFunc: func(ctx context.Context, outpoints []*transaction.Outpoint, topic string, spendTxid *chainhash.Hash) error {
				return nil
			},
		},
		ChainTracker: fakeChainTracker{
			verifyFunc: func(tx *transaction.Transaction, options ...any) (bool, error) {
				return true, nil
			},
			isValidRootForHeight: func(ctx context.Context, root *chainhash.Hash, height uint32) (bool, error) {
				return true, nil
			},
		},
		Broadcaster: fakeBroadcasterFail{
			broadcastFunc: func(tx *transaction.Transaction) (*transaction.BroadcastSuccess, *transaction.BroadcastFailure) {
				return nil, &transaction.BroadcastFailure{Description: "forced failure for testing"}
			},
			broadcastCtxFunc: func(ctx context.Context, tx *transaction.Transaction) (*transaction.BroadcastSuccess, *transaction.BroadcastFailure) {
				return nil, &transaction.BroadcastFailure{Description: "forced failure for testing"}
			},
		},
	})

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
	expectedErr := errors.New("insert-failed") //nolint:err113 // test sentinel

	sut := engine.NewEngine(&engine.EngineConfig{
		Managers: map[string]engine.TopicManager{
			"test-topic": fakeManager{
				identifyAdmissibleOutputsFunc: func(ctx context.Context, beef *transaction.Beef, txid *chainhash.Hash, previousCoins []uint32) (overlay.AdmittanceInstructions, error) {
					return overlay.AdmittanceInstructions{
						OutputsToAdmit: []uint32{0},
					}, nil
				},
			},
		},
		Storage: fakeStorage{
			findOutputFunc: func(ctx context.Context, outpoint *transaction.Outpoint, topic *string, spent *bool, includeBEEF bool) (*engine.Output, error) {
				return &engine.Output{
					Outpoint: transaction.Outpoint{
						Txid:  *prevTxID,
						Index: 0,
					},
					Topic: "test-topic",
					Beef:  emptyBeef,
				}, nil
			},
			findOutputsFunc: func(ctx context.Context, outpoints []*transaction.Outpoint, topic string, spent *bool, includeBEEF bool) ([]*engine.Output, error) {
				return []*engine.Output{
					{
						Outpoint: transaction.Outpoint{
							Txid:  *prevTxID,
							Index: 0,
						},
						Topic: "test-topic",
						Beef:  emptyBeef,
					},
				}, nil
			},
			doesAppliedTransactionExistFunc: func(ctx context.Context, tx *overlay.AppliedTransaction) (bool, error) {
				return false, nil
			},
			markUTXOsAsSpentFunc: func(ctx context.Context, outpoints []*transaction.Outpoint, topic string, spendTxid *chainhash.Hash) error {
				return nil
			},
			insertOutputsFunc: func(ctx context.Context, topic string, txid *chainhash.Hash, outputs []uint32, outpointsConsumed []*transaction.Outpoint, beef *transaction.Beef, ancillaryTxids []*chainhash.Hash) error {
				return expectedErr
			},
			deleteOutputFunc: func(ctx context.Context, outpoint *transaction.Outpoint, topic string) error {
				return nil
			},
		},
		ChainTracker: fakeChainTracker{},
	})

	// when:
	steak, err := sut.Submit(ctx, taggedBEEF, engine.SubmitModeCurrent, nil)

	// then:
	require.ErrorIs(t, err, expectedErr)
	require.Nil(t, steak)
}
