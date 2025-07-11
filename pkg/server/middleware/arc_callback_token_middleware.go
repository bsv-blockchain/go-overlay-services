package middleware

import (
	"net/http"
	"strings"

	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app/jsonutil"
)

// FailureResponse defines a standard error response structure
// returned by middleware components when validation fails.
type FailureResponse struct {
	Message string `json:"message"`
}

var (
	// MissingAuthHeaderResponse is returned when the Authorization header
	// is completely missing from the request.
	MissingAuthHeaderResponse = FailureResponse{
		Message: "Authorization header is missing from the request.",
	}

	// MissingAuthHeaderValueResponse is returned when the Authorization header
	// is present but doesn't contain a proper Bearer token.
	MissingAuthHeaderValueResponse = FailureResponse{
		Message: "Authorization header is present, but the Bearer token is missing.",
	}

	// InvalidBearerTokenValueResponse is returned when the provided Bearer token
	// doesn't match the expected token value.
	InvalidBearerTokenValueResponse = FailureResponse{
		Message: "The Bearer token provided is invalid.",
	}

	// EndpointNotSupportedResponse is returned when the endpoint is accessed but
	// is not configured in the current service (arcApiKey is empty).
	EndpointNotSupportedResponse = FailureResponse{
		Message: "This endpoint is not supported by the current service configuration.",
	}
)

func unsupportedEndpointMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			jsonutil.SendHTTPResponse(w, http.StatusNotFound, EndpointNotSupportedResponse)
		})
	}
}

// ArcCallbackTokenMiddleware is a middleware that checks the Authorization header for a valid Bearer token.
// It protects the ARC ingest endpoint from unauthorized access.
// It checks for a Bearer token in the Authorization header and compares it to the expected token value.
// The endpoint will return 404 Not Found if arcApiKey is empty, indicating ARC integration is disabled.
func ArcCallbackTokenMiddleware(arcCallbackToken string, arcApiKey string) func(http.Handler) http.Handler {
	const schema = "Bearer "

	if arcApiKey == "" {
		return unsupportedEndpointMiddleware()
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if arcCallbackToken == "" {
				next.ServeHTTP(w, r)
				return
			}

			auth := r.Header.Get("Authorization")
			if auth == "" {
				jsonutil.SendHTTPResponse(w, http.StatusUnauthorized, MissingAuthHeaderResponse)
				return
			}

			if !strings.HasPrefix(auth, schema) || len(auth) <= len(schema) {
				jsonutil.SendHTTPResponse(w, http.StatusUnauthorized, MissingAuthHeaderValueResponse)
				return
			}

			token := strings.TrimPrefix(auth, schema)
			if token != arcCallbackToken {
				jsonutil.SendHTTPResponse(w, http.StatusForbidden, InvalidBearerTokenValueResponse)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
