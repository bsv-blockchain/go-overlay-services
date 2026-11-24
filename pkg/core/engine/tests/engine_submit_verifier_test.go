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

// newVerifierTestEngine returns an engine whose storage and topic manager admit
// output 0 of any transaction, and whose chain tracker panics when asked to
// validate a merkle root: a submission that reaches spv.Verify fails the test.
func newVerifierTestEngine(t *testing.T) *engine.Engine {
	t.Helper()

	return engine.NewEngine(&engine.Config{
		Managers: map[string]engine.TopicManager{
			testTopic: fakeManager{
				identifyAdmissibleOutputsFunc: func(_ context.Context, _ *transaction.Beef, _ *chainhash.Hash, _ []uint32) (overlay.AdmittanceInstructions, error) {
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
				return &engine.Output{Beef: emptyBeef}, nil
			},
			findOutputsFunc: func(_ context.Context, _ []*transaction.Outpoint, _ string, _ *bool, _ bool) ([]*engine.Output, error) {
				return []*engine.Output{{Beef: emptyBeef}}, nil
			},
			doesAppliedTransactionExistFunc: func(_ context.Context, _ *overlay.AppliedTransaction) (bool, error) {
				return false, nil
			},
			markUTXOsAsSpentFunc: func(_ context.Context, _ []*transaction.Outpoint, _ string, _ *chainhash.Hash) error {
				return nil
			},
			insertOutputsFunc: func(_ context.Context, _ string, _ *chainhash.Hash, _ []uint32, _ []*transaction.Outpoint, _ *transaction.Beef, _ []*chainhash.Hash) error {
				return nil
			},
			insertAppliedTransactionFunc: func(_ context.Context, _ *overlay.AppliedTransaction) error {
				return nil
			},
		},
		ChainTracker: fakeChainTrackerSPVFail{},
	})
}

func TestEngine_Submit_Verifier_Accepts_ShouldReplaceSPVVerify(t *testing.T) {
	// given:
	ctx := context.Background()
	sut := newVerifierTestEngine(t)

	beefBytes := createDummyBEEF(t)
	_, _, expectedTxid, err := transaction.ParseBeef(beefBytes)
	require.NoError(t, err)

	var calls int
	sut.Verifier = func(_ context.Context, tx *transaction.Transaction, txid *chainhash.Hash) error {
		calls++
		require.NotNil(t, tx)
		require.Equal(t, expectedTxid, txid)
		require.Equal(t, expectedTxid, tx.TxID())
		return nil
	}

	expectedSteak := overlay.Steak{
		testTopic: &overlay.AdmittanceInstructions{
			OutputsToAdmit: []uint32{0},
			CoinsRemoved:   []uint32{0},
		},
	}

	// when:
	steak, err := sut.Submit(ctx, overlay.TaggedBEEF{Topics: []string{testTopic}, Beef: beefBytes}, engine.SubmitModeCurrent, nil)

	// then: the chain tracker was never consulted (it panics), and the
	// submission was admitted on the verifier's word.
	require.NoError(t, err)
	require.Equal(t, expectedSteak, steak)
	require.Equal(t, 1, calls)
}

func TestEngine_Submit_Verifier_Rejects_ShouldReturnItsError(t *testing.T) {
	// given:
	ctx := context.Background()
	expectedErr := errors.New("verifier-refused") //nolint:err113 // test sentinel

	var admissions int
	sut := engine.NewEngine(&engine.Config{
		Managers: map[string]engine.TopicManager{
			testTopic: fakeManager{
				identifyAdmissibleOutputsFunc: func(_ context.Context, _ *transaction.Beef, _ *chainhash.Hash, _ []uint32) (overlay.AdmittanceInstructions, error) {
					admissions++
					return overlay.AdmittanceInstructions{}, nil
				},
			},
		},
		Storage:      fakeStorage{},
		ChainTracker: fakeChainTrackerSPVFail{},
	})
	sut.Verifier = func(_ context.Context, _ *transaction.Transaction, _ *chainhash.Hash) error {
		return expectedErr
	}

	// when:
	steak, err := sut.Submit(ctx, overlay.TaggedBEEF{Topics: []string{testTopic}, Beef: createDummyBEEF(t)}, engine.SubmitModeCurrent, nil)

	// then: the verifier's error is returned as is, and nothing past
	// verification ran (no topic manager call, no storage call).
	require.ErrorIs(t, err, expectedErr)
	require.Nil(t, steak)
	require.Zero(t, admissions)
}

func TestEngine_Submit_NilVerifier_ShouldUseSPVVerify(t *testing.T) {
	// given: the same engine and submission as the accepting test, with no
	// Verifier set.
	ctx := context.Background()
	sut := newVerifierTestEngine(t)
	require.Nil(t, sut.Verifier)

	// when / then: the engine falls back to spv.Verify, which consults the
	// chain tracker.
	require.PanicsWithValue(t, "func not defined", func() {
		_, _ = sut.Submit(ctx, overlay.TaggedBEEF{Topics: []string{testTopic}, Beef: createDummyBEEF(t)}, engine.SubmitModeCurrent, nil)
	})
}
