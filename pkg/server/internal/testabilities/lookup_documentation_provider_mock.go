package testabilities

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// LookupServiceDocumentationProviderMockExpectations defines the expected behavior and outcomes for a LookupServiceDocumentationProviderMock.
type LookupServiceDocumentationProviderMockExpectations struct {
	DocumentationCall bool
	Error             error
	Documentation     string
}

// DefaultLookupServiceDocumentationProviderMockExpectations provides default expectations for LookupServiceDocumentationProviderMock,
// including a non-nil Documentation and no error.
var DefaultLookupServiceDocumentationProviderMockExpectations = LookupServiceDocumentationProviderMockExpectations{
	DocumentationCall: true,
	Error:             nil,
	Documentation:     "# Test Documentation\nThis is a test markdown document.",
}

// LookupServiceDocumentationProviderMock is a simple mock implementation for testing
// the behavior of a LookupServiceDocumentationProvider.
type LookupServiceDocumentationProviderMock struct {
	t            *testing.T
	expectations LookupServiceDocumentationProviderMockExpectations
	called       bool
}

// GetDocumentationForLookupServiceProvider simulates a documentation retrieval operation
// for a lookup service provider.
func (m *LookupServiceDocumentationProviderMock) GetDocumentationForLookupServiceProvider(lookupServiceName string) (string, error) {
	m.t.Helper()
	m.called = true

	if m.expectations.Error != nil {
		return "", m.expectations.Error
	}

	return m.expectations.Documentation, nil
}

// AssertCalled checks if the GetDocumentationForLookupServiceProvider method was called
// with the expected arguments.
func (m *LookupServiceDocumentationProviderMock) AssertCalled() {
	m.t.Helper()

	require.Equal(m.t, m.expectations.DocumentationCall, m.called, "Discrepancy between expected and actual DocumentationCall")
}

// NewLookupServiceDocumentationProviderMock creates a new LookupServiceDocumentationProviderMock with the given expectations.
func NewLookupServiceDocumentationProviderMock(t *testing.T, expectations LookupServiceDocumentationProviderMockExpectations) *LookupServiceDocumentationProviderMock {
	return &LookupServiceDocumentationProviderMock{
		t:            t,
		expectations: expectations,
	}
}
