// Package testabilities provides mock implementations and test utilities for testing
// the overlay services server components. It includes mocks for various providers
// such as ARC ingest, lookup services, GASP sync, and transaction submission.
package testabilities

import (
	"context"
	"testing"

	"github.com/bsv-blockchain/go-sdk/chainhash"
	"github.com/bsv-blockchain/go-sdk/transaction"
	"github.com/stretchr/testify/require"
)

// ARCIngestProviderMockExpectations defines the expected behavior for ARCIngestProviderMock.
type ARCIngestProviderMockExpectations struct {
	Error                    error
	HandleNewMerkleProofCall bool
}

// ARCIngestProviderMock is a mock implementation for testing ARC ingest provider behavior.
type ARCIngestProviderMock struct {
	t            *testing.T
	expectations ARCIngestProviderMockExpectations
	called       bool
}

// HandleNewMerkleProof simulates the behavior of the ARCIngestProvider.
// It returns the error set in expectations if provided, otherwise it returns nil.
func (a *ARCIngestProviderMock) HandleNewMerkleProof(_ context.Context, _ *chainhash.Hash, _ *transaction.MerklePath) error {
	a.t.Helper()
	a.called = true

	if a.expectations.Error != nil {
		return a.expectations.Error
	}

	return nil
}

// AssertCalled verifies that the HandleNewMerkleProof method was called as expected.
func (a *ARCIngestProviderMock) AssertCalled() {
	a.t.Helper()
	require.Equal(a.t, a.expectations.HandleNewMerkleProofCall, a.called, "Discrepancy between expected and actual HandleNewMerkleProof call")
}

// NewARCIngestProviderMock creates a new ARCIngestProviderMock instance.
// It initializes the mock with the provided expectations and a flag to track if the method has been called.
func NewARCIngestProviderMock(t *testing.T, expectations ARCIngestProviderMockExpectations) *ARCIngestProviderMock {
	return &ARCIngestProviderMock{
		t:            t,
		expectations: expectations,
		called:       false,
	}
}
