package testabilities

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// StartGASPSyncProviderMockExpectations defines the expected behavior of the StartGASPSyncProviderMock during a test.
type StartGASPSyncProviderMockExpectations struct {
	// Error is the error to return from StartGASPSync.
	Error error

	// StartGASPSyncCall indicates whether the StartGASPSync method is expected to be called during the test.
	StartGASPSyncCall bool
}

// StartGASPSyncProviderMock is a mock implementation of a GASP sync provider,
// used for testing the behavior of components that depend on GASP synchronization.
type StartGASPSyncProviderMock struct {
	t *testing.T

	// expectations defines the expected behavior and outcomes for this mock.
	expectations StartGASPSyncProviderMockExpectations

	// called is true if the StartGASPSync method was called.
	called bool
}

// StartGASPSync simulates the initiation of GASP synchronization. It records the call
// and returns the predefined error if set.
func (s *StartGASPSyncProviderMock) StartGASPSync(ctx context.Context) error {
	s.t.Helper()
	s.called = true

	if s.expectations.Error != nil {
		return s.expectations.Error
	}

	return nil
}

// AssertCalled verifies that the StartGASPSync method was called if it was expected to be.
func (s *StartGASPSyncProviderMock) AssertCalled() {
	s.t.Helper()
	require.Equal(s.t, s.expectations.StartGASPSyncCall, s.called, "Discrepancy between expected and actual StartGASPSync call")
}

// NewStartGASPSyncProviderMock creates a new instance of StartGASPSyncProviderMock with the given expectations.
func NewStartGASPSyncProviderMock(t *testing.T, expectations StartGASPSyncProviderMockExpectations) *StartGASPSyncProviderMock {
	mock := &StartGASPSyncProviderMock{
		t:            t,
		expectations: expectations,
	}

	return mock
}
