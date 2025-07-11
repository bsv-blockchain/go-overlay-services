package ports_test

import (
	"testing"

	"github.com/bsv-blockchain/go-overlay-services/pkg/server"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/ports"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/ports/openapi"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/testabilities"
	"github.com/bsv-blockchain/go-sdk/overlay/lookup"
	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/require"
)

func TestLookupQuestionHandler_InvalidCases(t *testing.T) {
	tests := map[string]struct {
		expectedStatusCode int
		payload            any
		expectedResponse   openapi.Error
		expectations       testabilities.LookupQuestionProviderMockExpectations
	}{
		"Malformed request body content in the HTTP request": {
			expectedStatusCode: fiber.StatusInternalServerError,
			payload:            `{invalid json`,
			expectedResponse:   testabilities.NewTestOpenapiErrorResponse(t, ports.NewRequestBodyParserError(testabilities.ErrTestNoopOpFailure)),
			expectations: testabilities.LookupQuestionProviderMockExpectations{
				LookupQuestionCall: false,
			},
		},
		"Lookup question service fails to handle the request - internal error": {
			expectedStatusCode: fiber.StatusInternalServerError,
			payload:            map[string]any{"service": "test-service", "query": map[string]string{"test": "value"}},
			expectedResponse:   testabilities.NewTestOpenapiErrorResponse(t, app.NewLookupQuestionProviderError(testabilities.ErrTestNoopOpFailure)),
			expectations: testabilities.LookupQuestionProviderMockExpectations{
				LookupQuestionCall: true,
				Error:              testabilities.ErrTestNoopOpFailure,
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			// given:
			stub := testabilities.NewTestOverlayEngineStub(t, testabilities.WithLookupQuestionProvider(testabilities.NewLookupQuestionProviderMock(t, tc.expectations)))
			fixture := server.NewTestFixture(t, server.WithEngine(stub))

			// when:
			var actualResponse openapi.Error

			res, _ := fixture.Client().
				R().
				SetHeader("Content-Type", "application/json").
				SetBody(tc.payload).
				SetError(&actualResponse).
				Post("/api/v1/lookup")

			// then:
			require.Equal(t, tc.expectedStatusCode, res.StatusCode())
			require.Equal(t, tc.expectedResponse, actualResponse)
			stub.AssertProvidersState()
		})
	}
}

func TestLookupQuestionHandler_ValidCase(t *testing.T) {
	// given:
	expectations := testabilities.LookupQuestionProviderMockExpectations{
		LookupQuestionCall: true,
		Answer: &lookup.LookupAnswer{
			Type:   lookup.AnswerTypeFreeform,
			Result: map[string]any{"test": "value"},
		},
	}

	expectedResponse, err := ports.NewLookupQuestionSuccessResponse(&app.LookupAnswerDTO{
		Result: "{\"test\":\"value\"}",
		Type:   "freeform",
	})

	stub := testabilities.NewTestOverlayEngineStub(t, testabilities.WithLookupQuestionProvider(testabilities.NewLookupQuestionProviderMock(t, expectations)))
	fixture := server.NewTestFixture(t, server.WithEngine(stub))

	// when:
	var actualResponse openapi.LookupAnswer

	res, _ := fixture.Client().
		R().
		SetHeader("Content-Type", "application/json").
		SetBody(openapi.LookupQuestionJSONRequestBody{
			Query:   map[string]any{"test": "query"},
			Service: "test-service",
		}).
		SetResult(&actualResponse).
		Post("/api/v1/lookup")

	// then:
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, res.StatusCode())
	require.Equal(t, expectedResponse, &actualResponse)

	stub.AssertProvidersState()
}
