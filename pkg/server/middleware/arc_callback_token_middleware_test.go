package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app/jsonutil"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/middleware"
	"github.com/stretchr/testify/require"
)

func TestArcCallbackTokenMiddleware(t *testing.T) {
	tests := map[string]struct {
		setupRequest          func(r *http.Request)
		expectedStatus        int
		expectedCallbackToken string
		expectedArcApiKey     string
		expectedResponse      middleware.FailureResponse
	}{
		"should succeed with 200 when Arc api key is provided and Arc callback token matches the configured key": {
			setupRequest: func(r *http.Request) {
				r.Header.Set("Authorization", "Bearer valid-callback-token")
			},
			expectedStatus:        http.StatusOK,
			expectedArcApiKey:     "valid-arc-api-key",
			expectedCallbackToken: "valid-callback-token",
		},
		"should succeed with 200 when Arc api key is provided and Arc callback token is empty": {
			setupRequest: func(r *http.Request) {
				r.Header.Set("Authorization", "Bearer valid-callback-token")
			},
			expectedStatus:        http.StatusOK,
			expectedArcApiKey:     "valid-arc-api-key",
			expectedCallbackToken: "",
		},
		"should fail with 404 when Arc api key token is not configured and Arc callback token matches the configured key": {
			setupRequest: func(r *http.Request) {
				r.Header.Set("Authorization", "Bearer valid-callback-token")
			},
			expectedStatus:        http.StatusNotFound,
			expectedCallbackToken: "valid-callback-token",
			expectedArcApiKey:     "",
			expectedResponse:      middleware.EndpointNotSupportedResponse,
		},
		"should fail with 404 when Arc api key token is not configured and Arc callback token is empty": {
			setupRequest: func(r *http.Request) {
				r.Header.Set("Authorization", "")
			},
			expectedStatus:        http.StatusNotFound,
			expectedCallbackToken: "",
			expectedArcApiKey:     "",
			expectedResponse:      middleware.EndpointNotSupportedResponse,
		},
		"should fail with 401 when Authorization header is missing": {
			setupRequest:          func(r *http.Request) {},
			expectedStatus:        http.StatusUnauthorized,
			expectedCallbackToken: "valid-callback-token",
			expectedArcApiKey:     "valid-arc-api-key",
			expectedResponse:      middleware.MissingAuthHeaderResponse,
		},
		"should fail with 401 when Authorization header doesn't have Bearer prefix": {
			setupRequest: func(r *http.Request) {
				r.Header.Set("Authorization", "IncorrectPrefix valid-callback-token")
			},
			expectedStatus:        http.StatusUnauthorized,
			expectedCallbackToken: "valid-callback-token",
			expectedArcApiKey:     "valid-arc-api-key",
			expectedResponse:      middleware.MissingAuthHeaderValueResponse,
		},
		"should fail with 401 when Authorization header has Bearer prefix but no token": {
			setupRequest: func(r *http.Request) {
				r.Header.Set("Authorization", "Bearer ")
			},
			expectedStatus:        http.StatusUnauthorized,
			expectedCallbackToken: "valid-callback-token",
			expectedArcApiKey:     "valid-arc-api-key",
			expectedResponse:      middleware.MissingAuthHeaderValueResponse,
		},
		"should fail with 403 when call back token doesn't match expected token": {
			setupRequest: func(r *http.Request) {
				r.Header.Set("Authorization", "Bearer wrong-callback-token")
			},
			expectedStatus:        http.StatusForbidden,
			expectedCallbackToken: "valid-callback-token",
			expectedArcApiKey:     "valid-arc-api-key",
			expectedResponse:      middleware.InvalidBearerTokenValueResponse,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			// given:
			handler := middleware.ArcCallbackTokenMiddleware(tc.expectedCallbackToken, tc.expectedArcApiKey)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			ts := httptest.NewServer(handler)
			defer ts.Close()

			req, err := http.NewRequest(http.MethodGet, ts.URL, nil)
			require.NoError(t, err)

			if tc.setupRequest != nil {
				tc.setupRequest(req)
			}

			// when:
			resp, err := ts.Client().Do(req)

			// then:
			require.NoError(t, err)
			defer resp.Body.Close()

			require.Equal(t, tc.expectedStatus, resp.StatusCode)

			if resp.StatusCode != http.StatusOK {
				var actual middleware.FailureResponse
				require.NoError(t, jsonutil.DecodeResponseBody(resp, &actual))
				require.Equal(t, tc.expectedResponse, actual)
			}
		})
	}
}
