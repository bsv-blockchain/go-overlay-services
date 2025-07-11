package app_test

import (
	"testing"

	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/testabilities"
	"github.com/bsv-blockchain/go-sdk/overlay/lookup"
	"github.com/stretchr/testify/require"
)

func TestLookupQuestionService_ValidCase(t *testing.T) {
	// given:
	mock := testabilities.NewLookupQuestionProviderMock(t, testabilities.LookupQuestionProviderMockExpectations{
		Answer:             &lookup.LookupAnswer{Type: lookup.AnswerTypeFreeform, Result: map[string]any{"test": "value"}},
		LookupQuestionCall: true,
	})
	service := app.NewLookupQuestionService(mock)
	expectedDTO := &app.LookupAnswerDTO{
		Result: "{\"test\":\"value\"}",
		Type:   string(lookup.AnswerTypeFreeform),
	}

	// when:
	actualDTO, err := service.LookupQuestion(t.Context(), "service1", map[string]any{"key": "value"})

	// then:
	require.NoError(t, err)
	require.Equal(t, expectedDTO, actualDTO)

	mock.AssertCalled()
}

func TestLookupQuestionService_InvalidCases(t *testing.T) {
	tests := map[string]struct {
		expectations  testabilities.LookupQuestionProviderMockExpectations
		service       string
		query         map[string]any
		expectedError app.Error
	}{
		"LookupQuestion should return error when service is empty": {
			expectations: testabilities.LookupQuestionProviderMockExpectations{
				LookupQuestionCall: false,
			},
			service:       "",
			expectedError: app.NewIncorrectInputWithFieldError("service"),
		},
		"LookupQuestion should return error when query is nil": {
			expectations: testabilities.LookupQuestionProviderMockExpectations{
				LookupQuestionCall: false,
			},
			service:       "test-service",
			query:         nil,
			expectedError: app.NewIncorrectInputWithFieldError("query"),
		},
		"LookupQuestion should return error when query is empty": {
			expectations: testabilities.LookupQuestionProviderMockExpectations{
				LookupQuestionCall: false,
			},
			service:       "test-service",
			query:         map[string]any{},
			expectedError: app.NewIncorrectInputWithFieldError("query"),
		},
		"LookupQuestion should return error from provider": {
			expectations: testabilities.LookupQuestionProviderMockExpectations{
				LookupQuestionCall: true,
				Error:              testabilities.ErrTestNoopOpFailure,
			},
			service: "test-service",
			query: map[string]any{
				"query1": "value1",
			},
			expectedError: app.NewLookupQuestionProviderError(testabilities.ErrTestNoopOpFailure),
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			// given:
			mock := testabilities.NewLookupQuestionProviderMock(t, tc.expectations)
			service := app.NewLookupQuestionService(mock)

			// when:
			actualDTO, err := service.LookupQuestion(t.Context(), tc.service, tc.query)

			// then:
			var actualErr app.Error
			require.ErrorAs(t, err, &actualErr)
			require.Equal(t, tc.expectedError, actualErr)

			require.Nil(t, actualDTO)
			mock.AssertCalled()
		})
	}
}
