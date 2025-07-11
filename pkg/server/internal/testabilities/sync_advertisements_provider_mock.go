package testabilities

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// SyncAdvertisementsProviderMockExpectations defines the expected behavior
// of the SyncAdvertisementsProviderMock during a test.
type SyncAdvertisementsProviderMockExpectations struct {
	// Err is the error to return from SyncAdvertisements. If set, it will be returned by the mock.
	Err error

	// SyncAdvertisementsCall indicates whether the SyncAdvertisements method is expected to be called.
	SyncAdvertisementsCall bool
}

// DefaultSyncAdvertisementsProviderMockExpectations provides default expectations
// for SyncAdvertisementsProviderMock: no error and expecting the method to be called.
var DefaultSyncAdvertisementsProviderMockExpectations = SyncAdvertisementsProviderMockExpectations{
	Err:                    nil,
	SyncAdvertisementsCall: true,
}

// SyncAdvertisementsProviderMock is a mock implementation of a provider
// responsible for syncing advertisements, used for testing.
type SyncAdvertisementsProviderMock struct {
	t *testing.T

	// expectations defines the expected behavior and outcomes for this mock.
	expectations *SyncAdvertisementsProviderMockExpectations

	// called is true if the SyncAdvertisements method was called.
	called bool
}

// SyncAdvertisements simulates a sync advertisements request.
// It records that it was called and returns the configured error, if any.
func (s *SyncAdvertisementsProviderMock) SyncAdvertisements(ctx context.Context) error {
	s.called = true
	if s.expectations.Err != nil {
		return s.expectations.Err
	}
	return nil
}

// AssertCalled verifies that the SyncAdvertisements method was called
// if it was expected to be. It fails the test if the expectation was not met.
func (s *SyncAdvertisementsProviderMock) AssertCalled() {
	s.t.Helper()
	require.Equal(s.t, s.expectations.SyncAdvertisementsCall, s.called, "Discrepancy between expected and actual SyncAdvertisements call")
}

// NewSyncAdvertisementsProviderMock creates a new instance of SyncAdvertisementsProviderMock
// with the provided expectations.
func NewSyncAdvertisementsProviderMock(t *testing.T, expectations SyncAdvertisementsProviderMockExpectations) *SyncAdvertisementsProviderMock {
	mock := &SyncAdvertisementsProviderMock{
		t:            t,
		expectations: &expectations,
	}
	return mock
}
