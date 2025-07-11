package ports_test

import (
	"testing"

	"github.com/bsv-blockchain/go-overlay-services/pkg/server"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/ports/openapi"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/testabilities"
	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/require"
)

func TestLookupServiceProviderDocumentationHandler_InvalidCases(t *testing.T) {
	tests := map[string]struct {
		expectedStatusCode int
		queryParams        map[string]string
		expectedResponse   openapi.Error
		expectations       testabilities.LookupServiceDocumentationProviderMockExpectations
	}{
		"Lookup documentation service fails to handle request - empty lookup service name": {
			expectedStatusCode: fiber.StatusBadRequest,
			queryParams:        map[string]string{"lookupService": ""},
			expectedResponse:   testabilities.NewTestOpenapiErrorResponse(t, app.NewEmptyLookupServiceNameError()),
			expectations: testabilities.LookupServiceDocumentationProviderMockExpectations{
				DocumentationCall: false,
			},
		},
		"Lookup documentation service fails to handle request - internal error": {
			expectedStatusCode: fiber.StatusInternalServerError,
			queryParams:        map[string]string{"lookupService": "test-lookup-service"},
			expectedResponse:   testabilities.NewTestOpenapiErrorResponse(t, app.NewLookupServiceProviderDocumentationError(nil)),
			expectations: testabilities.LookupServiceDocumentationProviderMockExpectations{
				DocumentationCall: true,
				Error:             app.NewLookupServiceProviderDocumentationError(nil),
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			// given:
			stub := testabilities.NewTestOverlayEngineStub(t, testabilities.WithLookupDocumentationProvider(testabilities.NewLookupServiceDocumentationProviderMock(t, tc.expectations)))
			fixture := server.NewTestFixture(t, server.WithEngine(stub))

			// when:
			var actualResponse openapi.BadRequestResponse

			res, _ := fixture.Client().
				R().
				SetQueryParams(tc.queryParams).
				SetError(&actualResponse).
				Get("/api/v1/getDocumentationForLookupServiceProvider")

			// then:
			require.Equal(t, tc.expectedStatusCode, res.StatusCode())
			require.Equal(t, &tc.expectedResponse, &actualResponse)
			stub.AssertProvidersState()
		})
	}
}

func TestLookupServiceProviderDocumentationHandler_ValidCase(t *testing.T) {
	// given:
	mock := testabilities.NewLookupServiceDocumentationProviderMock(t, testabilities.LookupServiceDocumentationProviderMockExpectations{
		DocumentationCall: true,
		Documentation:     testabilities.DefaultLookupServiceDocumentationProviderMockExpectations.Documentation,
	})
	stub := testabilities.NewTestOverlayEngineStub(t, testabilities.WithLookupDocumentationProvider(mock))
	fixture := server.NewTestFixture(t, server.WithEngine(stub))

	// when:
	var actualResponse openapi.LookupServiceProviderDocumentationResponse
	res, _ := fixture.Client().
		R().
		SetResult(&actualResponse).
		Get("/api/v1/getDocumentationForLookupServiceProvider?lookupService=testProvider")

	// then:
	require.Equal(t, fiber.StatusOK, res.StatusCode())
	require.Equal(t, testabilities.DefaultLookupServiceDocumentationProviderMockExpectations.Documentation, actualResponse.Documentation)
	mock.AssertCalled()
}
