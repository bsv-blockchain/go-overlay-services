package ports_test

import (
	"testing"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/gasp"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/ports"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/ports/openapi"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/testabilities"
	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/require"
)

func TestRequestForeignGASPNodeHandler_InvalidCases(t *testing.T) {
	tests := map[string]struct {
		payload            any
		headers            map[string]string
		expectations       testabilities.RequestForeignGASPNodeProviderMockExpectations
		expectedStatusCode int
		expectedResponse   openapi.Error
	}{
		"Request foreign GASP node service fails to handle the request - internal error": {
			payload: openapi.RequestForeignGASPNodeBody{
				GraphID:     testabilities.DefaultValidGraphID,
				OutputIndex: testabilities.DefaultValidOutputIndex,
				TxID:        testabilities.DefaultValidTxID,
			},
			headers: map[string]string{
				fiber.HeaderContentType: fiber.MIMEApplicationJSON,
				"X-BSV-Topic":           testabilities.DefaultValidTopic,
			},
			expectations: testabilities.RequestForeignGASPNodeProviderMockExpectations{
				ProvideForeignGASPNodeCall: true,
				Error:                      testabilities.ErrTestNoopOpFailure,
			},
			expectedStatusCode: fiber.StatusInternalServerError,
			expectedResponse: testabilities.NewTestOpenapiErrorResponse(t,
				app.NewForeignGASPNodeProviderError(testabilities.ErrTestNoopOpFailure),
			),
		},
		"Malformed request body content in the HTTP request": {
			payload: "INVALID_JSON",
			headers: map[string]string{
				fiber.HeaderContentType: fiber.MIMEApplicationJSON,
				"X-BSV-Topic":           testabilities.DefaultValidTopic,
			},
			expectations: testabilities.RequestForeignGASPNodeProviderMockExpectations{
				ProvideForeignGASPNodeCall: false,
			},
			expectedStatusCode: fiber.StatusInternalServerError,
			expectedResponse:   testabilities.NewTestOpenapiErrorResponse(t, ports.NewRequestBodyParserError(testabilities.ErrTestNoopOpFailure)),
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			// given:
			stub := testabilities.NewTestOverlayEngineStub(t, testabilities.WithRequestForeignGASPNodeProvider(
				testabilities.NewRequestForeignGASPNodeProviderMock(t, tc.expectations),
			))
			fixture := server.NewTestFixture(t, server.WithEngine(stub))

			// when:
			var actualResponse openapi.BadRequestResponse
			res, _ := fixture.Client().
				R().
				SetHeaders(tc.headers).
				SetBody(tc.payload).
				SetError(&actualResponse).
				Post("/api/v1/requestForeignGASPNode")

			// then:
			require.Equal(t, tc.expectedStatusCode, res.StatusCode())
			require.Equal(t, &tc.expectedResponse, &actualResponse)
			stub.AssertProvidersState()
		})
	}
}

func TestRequestForeignGASPNodeHandler_ValidCase(t *testing.T) {
	// given:
	expectations := testabilities.RequestForeignGASPNodeProviderMockExpectations{
		ProvideForeignGASPNodeCall: true,
		Node:                       &gasp.Node{},
	}

	stub := testabilities.NewTestOverlayEngineStub(t, testabilities.WithRequestForeignGASPNodeProvider(
		testabilities.NewRequestForeignGASPNodeProviderMock(t, expectations),
	))
	fixture := server.NewTestFixture(t, server.WithEngine(stub))
	expectedResponse := ports.NewRequestForeignGASPNodeSuccessResponse(expectations.Node)

	// when:
	var actualResponse openapi.GASPNode
	res, _ := fixture.Client().
		R().
		SetHeaders(map[string]string{
			"X-BSV-Topic":           testabilities.DefaultValidTopic,
			fiber.HeaderContentType: fiber.MIMEApplicationJSON,
		}).
		SetBody(openapi.RequestForeignGASPNodeBody{
			GraphID:     testabilities.DefaultValidGraphID,
			OutputIndex: testabilities.DefaultValidOutputIndex,
			TxID:        testabilities.DefaultValidTxID,
		}).
		SetResult(&actualResponse).
		Post("/api/v1/requestForeignGASPNode")

	// then:
	require.Equal(t, fiber.StatusOK, res.StatusCode())
	require.Equal(t, expectedResponse, actualResponse)
	stub.AssertProvidersState()
}
