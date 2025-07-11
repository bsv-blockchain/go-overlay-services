package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/testabilities"
	"github.com/stretchr/testify/require"
)

func TestTopicManagerDocumentationService_InvalidCases(t *testing.T) {
	tests := map[string]struct {
		expectedError app.Error
		expectations  testabilities.TopicManagerDocumentationProviderMockExpectations
		topicManager  string
	}{
		"Topic manager documentation service fails to handle request - empty topic manager name": {
			topicManager: "",
			expectations: testabilities.TopicManagerDocumentationProviderMockExpectations{
				DocumentationCall: false,
			},
			expectedError: app.NewEmptyTopicManagerNameError(),
		},
		"Topic manager documentation service fails to handle request - internal error": {
			topicManager: "test-topic-manager",
			expectations: testabilities.TopicManagerDocumentationProviderMockExpectations{
				DocumentationCall: true,
				Error:             errors.New("internal topic manager documentation provider test error"),
			},
			expectedError: app.NewTopicManagerDocumentationProviderError(errors.New("internal topic manager documentation provider test error")),
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			// given:
			mock := testabilities.NewTopicManagerDocumentationProviderMock(t, tc.expectations)
			service := app.NewTopicManagerDocumentationService(mock)

			// when:
			document, err := service.GetDocumentation(context.Background(), tc.topicManager)

			// then:
			var actualErr app.Error
			require.ErrorAs(t, err, &actualErr)
			require.Equal(t, tc.expectedError, actualErr)

			require.Empty(t, document)
			mock.AssertCalled()
		})
	}
}

func TestTopicManagerDocumentationService_ValidCase(t *testing.T) {
	// given:
	mock := testabilities.NewTopicManagerDocumentationProviderMock(t, testabilities.DefaultTopicManagerDocumentationProviderMockExpectations)
	service := app.NewTopicManagerDocumentationService(mock)

	// when:
	documentation, err := service.GetDocumentation(context.Background(), "test-topic-manager")

	// then:
	require.NoError(t, err)
	require.Equal(t, testabilities.DefaultTopicManagerDocumentationProviderMockExpectations.Documentation, documentation)
	mock.AssertCalled()
}
