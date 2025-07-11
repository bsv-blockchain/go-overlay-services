package testabilities

import (
	"context"
	"testing"

	"github.com/bsv-blockchain/go-sdk/overlay/lookup"
	"github.com/stretchr/testify/require"
)

// LookupQuestionProviderMockExpectations defines the expected behavior and outcomes for a LookupQuestionProviderMock.
type LookupQuestionProviderMockExpectations struct {
	LookupQuestionCall bool
	Error              error
	Answer             *lookup.LookupAnswer
}

// LookupQuestionProviderMock is a mock implementation for testing the behavior of a LookupQuestionProvider.
type LookupQuestionProviderMock struct {
	t            *testing.T
	expectations LookupQuestionProviderMockExpectations
	called       bool
}

// Lookup simulates a lookup operation and returns the expected answer or error.
func (m *LookupQuestionProviderMock) Lookup(ctx context.Context, question *lookup.LookupQuestion) (*lookup.LookupAnswer, error) {
	m.t.Helper()
	m.called = true

	if m.expectations.Error != nil {
		return nil, m.expectations.Error
	}

	return m.expectations.Answer, nil
}

// AssertCalled checks if the Lookup method was called with the expected arguments.
func (m *LookupQuestionProviderMock) AssertCalled() {
	m.t.Helper()
	require.Equal(m.t, m.expectations.LookupQuestionCall, m.called, "Discrepancy between expected and actual LookupQuestionCall")
}

// NewLookupQuestionProviderMock creates a new LookupQuestionProviderMock with the given options.
func NewLookupQuestionProviderMock(t *testing.T, expectations LookupQuestionProviderMockExpectations) *LookupQuestionProviderMock {
	mock := &LookupQuestionProviderMock{
		t:            t,
		expectations: expectations,
	}
	return mock
}
