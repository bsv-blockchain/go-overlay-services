package ports_test

import (
	"testing"

	"github.com/bsv-blockchain/go-overlay-services/pkg/server2"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server2/internal/ports"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server2/internal/ports/decorators"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server2/internal/ports/openapi"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server2/internal/testabilities"
	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/require"
)

func TestArcIngestHandler_InvalidCases(t *testing.T) {
	tests := map[string]struct {
		expectedResponse   openapi.Error
		expectedStatusCode int
		headers            map[string]string
	}{
		"Missing Authorization header": {
			expectedStatusCode: fiber.StatusUnauthorized,
			expectedResponse:   testabilities.NewTestOpenapiErrorResponse(t, decorators.NewMissingAuthHeaderError()),
			headers: map[string]string{
				fiber.HeaderContentType: fiber.MIMEApplicationJSON,
			},
		},
		"Authorization header without Bearer prefix": {
			expectedStatusCode: fiber.StatusUnauthorized,
			expectedResponse:   testabilities.NewTestOpenapiErrorResponse(t, decorators.NewInvalidBearerTokenSchema()),
			headers: map[string]string{
				fiber.HeaderContentType:   fiber.MIMEApplicationJSON,
				fiber.HeaderAuthorization: "Basic sometoken",
			},
		},
		"Authorization header with Bearer prefix only": {
			expectedStatusCode: fiber.StatusUnauthorized,
			expectedResponse:   testabilities.NewTestOpenapiErrorResponse(t, decorators.NewInvalidBearerTokenSchema()),
			headers: map[string]string{
				fiber.HeaderContentType:   fiber.MIMEApplicationJSON,
				fiber.HeaderAuthorization: "Bearer",
			},
		},
		"Authorization header with invalid Bearer token": {
			expectedStatusCode: fiber.StatusForbidden,
			expectedResponse:   testabilities.NewTestOpenapiErrorResponse(t, decorators.NewInvalidBearerTokenError()),
			headers: map[string]string{
				fiber.HeaderContentType:   fiber.MIMEApplicationJSON,
				fiber.HeaderAuthorization: "Bearer invalidtoken",
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			// given:
			stub := testabilities.NewTestOverlayEngineStub(t, testabilities.WithARCIngestProvider(
				testabilities.NewARCIngestProviderMock(t, testabilities.ARCIngestProviderMockExpectations{HandleNewMerkleProofCall: false})),
			)

			fixture := server2.NewServerTestFixture(t,
				server2.WithEngine(stub),
				server2.WithARCCallbackToken(testabilities.DefaultARCCallbackToken),
				server2.WithARCAPIKey(testabilities.DefaultARCAPIKey),
			)

			// when:
			var actualResponse openapi.Error

			res, _ := fixture.Client().
				R().
				SetHeaders(tc.headers).
				SetBody(openapi.ArcIngestBody{
					Txid:        testabilities.NewTxID(t),
					MerklePath:  testabilities.NewTestMerklePath(t),
					BlockHeight: testabilities.DefaultBlockHeight,
				}).
				SetError(&actualResponse).
				Post("/api/v1/arc-ingest")

			// then:
			require.Equal(t, tc.expectedStatusCode, res.StatusCode())
			require.Equal(t, tc.expectedResponse, actualResponse)

			stub.AssertProvidersState()
		})
	}
}

func TestArcIngestHandler_ValidCase(t *testing.T) {
	// given:
	expectations := testabilities.ARCIngestProviderMockExpectations{
		HandleNewMerkleProofCall: true,
		Error:                    nil,
	}

	expectedResponse := ports.NewARCIngestSuccessResponse(testabilities.NewTxID(t))

	stub := testabilities.NewTestOverlayEngineStub(t, testabilities.WithARCIngestProvider(testabilities.NewARCIngestProviderMock(t, expectations)))

	fixture := server2.NewServerTestFixture(t,
		server2.WithEngine(stub),
		server2.WithARCCallbackToken(testabilities.DefaultARCCallbackToken),
		server2.WithARCAPIKey(testabilities.DefaultARCAPIKey),
	)

	// when:
	var actualResponse openapi.ArcIngest

	res, _ := fixture.Client().
		R().
		SetHeaders(map[string]string{
			fiber.HeaderContentType:   fiber.MIMEApplicationJSON,
			fiber.HeaderAuthorization: "Bearer " + testabilities.DefaultARCCallbackToken,
		}).
		SetBody(openapi.ArcIngestBody{
			Txid:        testabilities.NewTxID(t),
			MerklePath:  testabilities.NewTestMerklePath(t),
			BlockHeight: testabilities.DefaultBlockHeight,
		}).
		SetResult(&actualResponse).
		Post("/api/v1/arc-ingest")

	// then:
	require.Equal(t, fiber.StatusOK, res.StatusCode())
	require.Equal(t, expectedResponse, &actualResponse)

	stub.AssertProvidersState()
}
