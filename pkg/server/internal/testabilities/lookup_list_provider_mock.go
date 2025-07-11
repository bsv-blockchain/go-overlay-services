package testabilities

import (
	"testing"

	"github.com/bsv-blockchain/go-sdk/overlay"
	"github.com/stretchr/testify/require"
)

// LookupListProviderMockExpectations defines the expected behavior of the mock,
// including the expected metadata to return and whether the method is expected to be called.
type LookupListProviderMockExpectations struct {
	Metadata                       map[string]*overlay.MetaData // Metadata to return when ListLookupServiceProviders is called.
	ListLookupServiceProvidersCall bool                         // Whether ListLookupServiceProviders is expected to be called.
}

// LookupListProviderMock is a mock implementation of the LookupListProvider interface.
// It records whether ListLookupServiceProviders was called and returns predefined metadata.
type LookupListProviderMock struct {
	t            *testing.T                         // Test context used for assertions.
	expectations LookupListProviderMockExpectations // Expected behavior configuration.
	called       bool                               // Tracks if ListLookupServiceProviders was invoked.
}

// ListLookupServiceProviders returns the expected metadata and records that the method was called.
func (l *LookupListProviderMock) ListLookupServiceProviders() map[string]*overlay.MetaData {
	l.t.Helper()
	l.called = true

	return l.expectations.Metadata
}

// AssertCalled asserts that ListLookupServiceProviders was called or not,
// matching the expectation set in the mock configuration.
func (l *LookupListProviderMock) AssertCalled() {
	l.t.Helper()

	require.Equal(l.t, l.expectations.ListLookupServiceProvidersCall, l.called, "Discrepancy between expected and actual ListLookupServiceProvidersCall")
}

// NewLookupListProviderMock constructs a new mock with the provided test context and expectations.
func NewLookupListProviderMock(t *testing.T, expectations LookupListProviderMockExpectations) *LookupListProviderMock {
	return &LookupListProviderMock{
		t:            t,
		expectations: expectations,
	}
}
