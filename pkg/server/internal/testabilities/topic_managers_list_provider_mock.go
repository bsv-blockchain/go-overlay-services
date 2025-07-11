package testabilities

import (
	"testing"

	"github.com/bsv-blockchain/go-sdk/overlay"
	"github.com/stretchr/testify/require"
)

// TopicManagersListProviderMockExpectations defines the expected behavior of the mock,
// including the metadata to return and whether the method is expected to be called.
type TopicManagersListProviderMockExpectations struct {
	Metadata              map[string]*overlay.MetaData // Metadata to return when ListTopicManagers is called.
	ListTopicManagersCall bool                         // Whether ListTopicManagers is expected to be called.
}

// TopicManagersListProviderMock is a mock implementation of the TopicManagersListProvider interface.
// It tracks whether the ListTopicManagers method was called and returns predefined metadata.
type TopicManagersListProviderMock struct {
	t            *testing.T                                // Test context used for assertions.
	expectations TopicManagersListProviderMockExpectations // Expected behavior configuration.
	called       bool                                      // Tracks if ListTopicManagers was invoked.
}

// ListTopicManagers returns the expected metadata and records that the method was called.
func (m *TopicManagersListProviderMock) ListTopicManagers() map[string]*overlay.MetaData {
	m.t.Helper()
	m.called = true

	return m.expectations.Metadata
}

// AssertCalled verifies that the ListTopicManagers method was called or not,
// according to the expectation set in the mock configuration.
func (m *TopicManagersListProviderMock) AssertCalled() {
	m.t.Helper()

	require.Equal(m.t, m.expectations.ListTopicManagersCall, m.called, "Discrepancy between expected and actual ListTopicManagersCall")
}

// NewTopicManagersListProviderMock constructs a new mock with the provided test context and expectations.
func NewTopicManagersListProviderMock(t *testing.T, expectations TopicManagersListProviderMockExpectations) *TopicManagersListProviderMock {
	return &TopicManagersListProviderMock{
		t:            t,
		expectations: expectations,
	}
}
