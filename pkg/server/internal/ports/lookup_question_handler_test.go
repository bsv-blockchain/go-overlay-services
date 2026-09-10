package ports_test

import (
	"encoding/json"
	"testing"

	"github.com/bsv-blockchain/go-sdk/overlay/lookup"
	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/require"

	"github.com/bsv-blockchain/go-overlay-services/pkg/server"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/ports"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/ports/openapi"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/testabilities"
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

func TestLookupQuestionHandlerOutputListUsesBRC24ByteArray(t *testing.T) {
	stub := testabilities.NewTestOverlayEngineStub(t, testabilities.WithLookupQuestionProvider(testabilities.NewLookupQuestionProviderMock(t, testabilities.LookupQuestionProviderMockExpectations{
		LookupQuestionCall: true,
		Answer: &lookup.LookupAnswer{
			Type: lookup.AnswerTypeOutputList,
			Outputs: []*lookup.OutputListItem{{
				Beef:        []byte{1, 2, 255},
				OutputIndex: 7,
			}},
		},
	})))
	fixture := server.NewTestFixture(t, server.WithEngine(stub))

	res, err := fixture.Client().
		R().
		SetHeader("Content-Type", "application/json").
		SetBody(openapi.LookupQuestionJSONRequestBody{
			Query:   map[string]any{},
			Service: "test-service",
		}).
		Post("/api/v1/lookup")

	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, res.StatusCode())
	var body struct {
		Outputs []struct {
			Beef json.RawMessage `json:"beef"`
		} `json:"outputs"`
	}
	require.NoError(t, json.Unmarshal(res.Body(), &body))
	require.Len(t, body.Outputs, 1)
	require.JSONEq(t, `[1,2,255]`, string(body.Outputs[0].Beef))

	stub.AssertProvidersState()
}
