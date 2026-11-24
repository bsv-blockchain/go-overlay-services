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

// admittingStorage is a storage that holds nothing and accepts every write:
// the calls a first-time submission makes all succeed.
type admittingStorage struct{ fakeStorage }

func (admittingStorage) FindOutput(context.Context, *transaction.Outpoint, *string, *bool, bool) (*engine.Output, error) {
	return &engine.Output{Beef: emptyBeef}, nil
}

func (admittingStorage) FindOutputs(context.Context, []*transaction.Outpoint, string, *bool, bool) ([]*engine.Output, error) {
	return []*engine.Output{{Beef: emptyBeef}}, nil
}

func (admittingStorage) DoesAppliedTransactionExist(context.Context, *overlay.AppliedTransaction) (bool, error) {
	return false, nil
}

func (admittingStorage) InsertAppliedTransaction(context.Context, *overlay.AppliedTransaction) error {
	return nil
}

func (admittingStorage) MarkUTXOsAsSpent(context.Context, []*transaction.Outpoint, string, *chainhash.Hash) error {
	return nil
}

func (admittingStorage) InsertOutputs(context.Context, string, *chainhash.Hash, []uint32, []*transaction.Outpoint, *transaction.Beef, []*chainhash.Hash) error {
	return nil
}

func (admittingStorage) DeleteOutput(context.Context, *transaction.Outpoint, string) error {
	return nil
}

// newVerifierTestEngine returns an engine whose topic manager admits output 0
// of any transaction, counting its calls in admissions, and whose chain
// tracker panics when asked to validate a merkle root: a submission that
// reaches spv.Verify fails the test.
func newVerifierTestEngine(t *testing.T, admissions *int) *engine.Engine {
	t.Helper()

	manager := fakeManager{
		identifyAdmissibleOutputsFunc: func(context.Context, *transaction.Beef, *chainhash.Hash, []uint32) (overlay.AdmittanceInstructions, error) {
			*admissions++
			return overlay.AdmittanceInstructions{OutputsToAdmit: []uint32{0}}, nil
		},
	}
	return engine.NewEngine(&engine.Config{
		Managers:     map[string]engine.TopicManager{testTopic: manager},
		Storage:      admittingStorage{},
		ChainTracker: fakeChainTrackerSPVFail{},
	})
}

func TestEngine_Submit_Verifier_Accepts_ShouldReplaceSPVVerify(t *testing.T) {
	// given:
	ctx := context.Background()
	var admissions int
	sut := newVerifierTestEngine(t, &admissions)

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
	require.Equal(t, 1, admissions)
}

func TestEngine_Submit_Verifier_Rejects_ShouldReturnItsError(t *testing.T) {
	// given:
	ctx := context.Background()
	expectedErr := errors.New("verifier-refused") //nolint:err113 // test sentinel

	var admissions int
	sut := newVerifierTestEngine(t, &admissions)
	sut.Verifier = func(_ context.Context, _ *transaction.Transaction, _ *chainhash.Hash) error {
		return expectedErr
	}

	// when:
	steak, err := sut.Submit(ctx, overlay.TaggedBEEF{Topics: []string{testTopic}, Beef: createDummyBEEF(t)}, engine.SubmitModeCurrent, nil)

	// then: the verifier's error is returned as is, and nothing past
	// verification ran: the topic manager was never asked.
	require.ErrorIs(t, err, expectedErr)
	require.Nil(t, steak)
	require.Zero(t, admissions)
}

func TestEngine_Submit_NilVerifier_ShouldUseSPVVerify(t *testing.T) {
	// given: the same engine and submission as the accepting test, with no
	// Verifier set.
	ctx := context.Background()
	var admissions int
	sut := newVerifierTestEngine(t, &admissions)
	require.Nil(t, sut.Verifier)

	// when / then: the engine falls back to spv.Verify, which consults the
	// chain tracker.
	require.PanicsWithValue(t, "func not defined", func() {
		_, _ = sut.Submit(ctx, overlay.TaggedBEEF{Topics: []string{testTopic}, Beef: createDummyBEEF(t)}, engine.SubmitModeCurrent, nil)
	})
	require.Zero(t, admissions)
}
