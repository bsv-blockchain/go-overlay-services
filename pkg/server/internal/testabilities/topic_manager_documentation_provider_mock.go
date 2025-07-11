package testabilities

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TopicManagerDocumentationProviderMockExpectations defines the expected behavior and outcomes for a TopicManagerDocumentationProviderMock.
type TopicManagerDocumentationProviderMockExpectations struct {
	DocumentationCall bool
	Error             error
	Documentation     string
}

// DefaultTopicManagerDocumentationProviderMockExpectations is a default set of expectations
// for a TopicManagerDocumentationProviderMock.
var DefaultTopicManagerDocumentationProviderMockExpectations = TopicManagerDocumentationProviderMockExpectations{
	DocumentationCall: true,
	Error:             nil,
	Documentation:     "# Topic Manager Documentation\nThis is a test markdown document.",
}

// TopicManagerDocumentationProviderMock is a simple mock implementation for testing
// the behavior of a TopicManagerDocumentationProvider.
type TopicManagerDocumentationProviderMock struct {
	t            *testing.T
	expectations TopicManagerDocumentationProviderMockExpectations
	called       bool
}

// GetDocumentationForTopicManager simulates a documentation retrieval operation
// and returns the expected documentation string and error.
func (m *TopicManagerDocumentationProviderMock) GetDocumentationForTopicManager(topicManagerName string) (string, error) {
	m.t.Helper()
	m.called = true

	if m.expectations.Error != nil {
		return "", m.expectations.Error
	}

	return m.expectations.Documentation, nil
}

// AssertCalled checks if the GetDocumentationForTopicManager method was called
// with the expected arguments.
func (m *TopicManagerDocumentationProviderMock) AssertCalled() {
	m.t.Helper()
	require.Equal(m.t, m.expectations.DocumentationCall, m.called, "Discrepancy between expected and actual DocumentationCall")
}

// NewTopicManagerDocumentationProviderMock creates a new TopicManagerDocumentationProviderMock with the given expectations.
func NewTopicManagerDocumentationProviderMock(t *testing.T, expectations TopicManagerDocumentationProviderMockExpectations) *TopicManagerDocumentationProviderMock {
	return &TopicManagerDocumentationProviderMock{
		t:            t,
		expectations: expectations,
	}
}
