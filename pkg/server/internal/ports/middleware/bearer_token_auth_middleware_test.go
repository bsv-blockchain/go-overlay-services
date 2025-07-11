package middleware_test

import (
	"testing"

	"github.com/bsv-blockchain/go-overlay-services/pkg/server"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/ports/middleware"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/ports/openapi"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/testabilities"
	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/require"
)

func TestBearerTokenAuthMiddleware_ValidCases(t *testing.T) {
	testPaths := []struct {
		endpoint               string
		method                 string
		expectedProviderToCall testabilities.TestOverlayEngineStubOption
	}{
		{
			endpoint:               "/api/v1/admin/syncAdvertisements",
			method:                 fiber.MethodPost,
			expectedProviderToCall: testabilities.WithSyncAdvertisementsProvider(testabilities.NewSyncAdvertisementsProviderMock(t, testabilities.SyncAdvertisementsProviderMockExpectations{SyncAdvertisementsCall: true})),
		},
		{
			endpoint:               "/api/v1/admin/startGASPSync",
			method:                 fiber.MethodPost,
			expectedProviderToCall: testabilities.WithStartGASPSyncProvider(testabilities.NewStartGASPSyncProviderMock(t, testabilities.StartGASPSyncProviderMockExpectations{StartGASPSyncCall: true})),
		},
	}

	const bearerToken = "valid_admin_token"

	tests := map[string]struct {
		expectedStatus int
		headers        map[string]string
	}{
		"Authorization header with a valid HTTP server token": {
			expectedStatus: fiber.StatusOK,
			headers: map[string]string{
				fiber.HeaderAuthorization: "Bearer " + bearerToken,
			},
		},
	}

	for _, path := range testPaths {
		for name, tc := range tests {
			t.Run(name, func(t *testing.T) {
				// given:
				stub := testabilities.NewTestOverlayEngineStub(t, path.expectedProviderToCall)
				fixture := server.NewTestFixture(
					t,
					server.WithAdminBearerToken(bearerToken),
					server.WithEngine(stub),
				)

				// when:
				res, _ := fixture.Client().
					R().
					SetHeaders(tc.headers).
					Execute(path.method, path.endpoint)

				// then:
				require.Equal(t, tc.expectedStatus, res.StatusCode())
				stub.AssertProvidersState()
			})
		}
	}
}

func TestBearerTokenAuthMiddleware_InvalidCases(t *testing.T) {
	testPaths := []struct {
		endpoint string
		method   string
	}{
		{
			endpoint: "/api/v1/admin/syncAdvertisements",
			method:   fiber.MethodPost,
		},
		{
			endpoint: "/api/v1/admin/startGASPSync",
			method:   fiber.MethodPost,
		},
	}

	const bearerToken = "valid_admin_token"

	tests := map[string]struct {
		expectedResponse openapi.BadRequestResponse
		expectedStatus   int
		headers          map[string]string
	}{
		"Authorization header with invalid HTTP server token": {
			expectedStatus:   fiber.StatusForbidden,
			expectedResponse: testabilities.NewTestOpenapiErrorResponse(t, middleware.NewInvalidBearerTokenValueError()),
			headers: map[string]string{
				fiber.HeaderAuthorization: "Bearer " + "1234",
			},
		},
		"Missing Authorization header in the HTTP request": {
			expectedStatus:   fiber.StatusUnauthorized,
			expectedResponse: testabilities.NewTestOpenapiErrorResponse(t, middleware.NewMissingAuthorizationHeaderError()),
			headers: map[string]string{
				"RandomHeader": "Bearer " + bearerToken,
			},
		},
		"Missing Authorization header value in the HTTP request": {
			expectedStatus:   fiber.StatusUnauthorized,
			expectedResponse: testabilities.NewTestOpenapiErrorResponse(t, middleware.NewMissingAuthorizationHeaderError()),
			headers: map[string]string{
				fiber.HeaderAuthorization: "",
			},
		},
		"Invalid Bearer scheme in the Authorization header appended to the HTTP request": {
			expectedStatus:   fiber.StatusUnauthorized,
			expectedResponse: testabilities.NewTestOpenapiErrorResponse(t, middleware.NewMissingBearerTokenValueError()),
			headers: map[string]string{
				fiber.HeaderAuthorization: "InvalidScheme " + bearerToken,
			},
		},
	}

	for _, path := range testPaths {
		for name, tc := range tests {
			t.Run(name, func(t *testing.T) {
				// given:
				stub := testabilities.NewTestOverlayEngineStub(t)
				fixture := server.NewTestFixture(
					t,
					server.WithAdminBearerToken(bearerToken),
					server.WithEngine(stub),
				)

				// when:
				var actual openapi.BadRequestResponse

				res, _ := fixture.Client().
					R().
					SetHeaders(tc.headers).
					SetError(&actual).
					Execute(path.method, path.endpoint)

				// then:
				require.Equal(t, tc.expectedStatus, res.StatusCode())
				require.Equal(t, tc.expectedResponse, actual)
				stub.AssertProvidersState()
			})
		}
	}
}
