package testabilities

import (
	"context"
	"testing"
	"time"

	"github.com/4chain-ag/go-overlay-services/pkg/core/engine"
	"github.com/bsv-blockchain/go-sdk/overlay"
	"github.com/stretchr/testify/require"
)

// SubmitTransactionProviderMockExpectations defines the expected behavior of the SubmitTransactionProviderMock during a test.
type SubmitTransactionProviderMockExpectations struct {
	// STEAK is the mock value that will be passed to the callback when submission succeeds.
	STEAK *overlay.Steak

	// Error is the error to return from Submit. If set, the callback will not be invoked.
	Error error

	// SubmitCall indicates whether the Submit method is expected to be called during the test.
	SubmitCall bool

	// TriggerCallbackAfter specifies the duration after which the callback should be invoked.
	TriggerCallbackAfter time.Duration
}

// DefaultSubmitTransactionProviderMockExpectations provides default expectations for SubmitTransactionProviderMock,
// including a non-nil STEAK, no error, and a default delay for triggering the callback.
var DefaultSubmitTransactionProviderMockExpectations = SubmitTransactionProviderMockExpectations{
	STEAK:                &overlay.Steak{},
	Error:                nil,
	SubmitCall:           true,
	TriggerCallbackAfter: time.Millisecond,
}

// SubmitTransactionProviderMock is a mock implementation of a transaction submission provider,
// used for testing the behavior of components that depend on transaction submission.
type SubmitTransactionProviderMock struct {
	t *testing.T

	// expectations defines the expected behavior and outcomes for this mock.
	expectations SubmitTransactionProviderMockExpectations

	// called is true if the Submit method was called.
	called bool

	// callbackInvoked is true if the provided callback was invoked.
	callbackInvoked bool

	// calledTaggedBEEF stores the TaggedBEEF argument passed to Submit.
	calledTaggedBEEF overlay.TaggedBEEF

	// calledSubmitMode stores the SubmitMode argument passed to Submit.
	calledSubmitMode engine.SumbitMode
}

// Submit simulates the submission of a transaction. It records the call, returns
// the predefined error if set, and optionally invokes the callback with the mock STEAK after a delay.
func (s *SubmitTransactionProviderMock) Submit(ctx context.Context, taggedBEEF overlay.TaggedBEEF, mode engine.SumbitMode, callback engine.OnSteakReady) (overlay.Steak, error) {
	s.t.Helper()

	s.called = true
	s.calledTaggedBEEF = taggedBEEF
	s.calledSubmitMode = mode
	s.callbackInvoked = false

	if s.expectations.Error != nil {
		return nil, s.expectations.Error
	}

	time.AfterFunc(s.expectations.TriggerCallbackAfter, func() {
		callback(s.expectations.STEAK)
		s.callbackInvoked = true
	})

	return overlay.Steak{}, nil
}

// AssertCalled verifies that the Submit method was called if it was expected to be.
func (s *SubmitTransactionProviderMock) AssertCalled() {
	s.t.Helper()
	require.Equal(s.t, s.expectations.SubmitCall, s.called, "Discrepancy between expected and actual Submit call")
}

// NewSubmitTransactionProviderMock creates a new instance of SubmitTransactionProviderMock with the given expectations.
func NewSubmitTransactionProviderMock(t *testing.T, expectations SubmitTransactionProviderMockExpectations) *SubmitTransactionProviderMock {
	mock := &SubmitTransactionProviderMock{
		t:            t,
		expectations: expectations,
	}
	return mock
}
