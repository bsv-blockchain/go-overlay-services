package middleware

import (
	"fmt"
	"slices"
	"strings"

	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/ports/openapi"
	"github.com/gofiber/fiber/v2"
)

// BearerTokenAuthorizationMiddleware returns a fiber.Handler that validates the
// Bearer token present in Authorization header of incoming HTTP requests.
// It also conditionally check if the requests is authorized based on OpenAPI
// security scopes.
func BearerTokenAuthorizationMiddleware(expectedToken string) fiber.Handler {
	const scheme = "Bearer "
	const adminScope = "admin"

	return func(c *fiber.Ctx) error {
		ctx := c.Context()
		scopes, ok := ctx.UserValue(openapi.BearerAuthScopes).([]string)
		if !ok {
			return NewBearerAuthScopesAssertionError()
		}
		if len(scopes) == 0 {
			return NewEmptyAccessScopesAssertionError()
		}
		if !slices.Contains(scopes, adminScope) {
			return nil
		}

		auth := c.Get(fiber.HeaderAuthorization)
		if auth == "" {
			return NewMissingAuthorizationHeaderError()
		}

		if !strings.HasPrefix(auth, scheme) {
			return NewMissingBearerTokenValueError()
		}

		token := strings.TrimPrefix(auth, scheme)
		if token != expectedToken {
			return NewInvalidBearerTokenValueError()
		}

		return nil
	}
}

// NewMissingAuthorizationHeaderError returns an app.Error indicating that the
// Authorization header is missing from the request.
func NewMissingAuthorizationHeaderError() app.Error {
	const str = "Unauthorized access: Missing Authorization header in the request"
	return app.NewAuthorizationError(str, str)
}

// NewMissingBearerTokenValueError returns an app.Error indicating that the
// Bearer token value is missing from the Authorization header.
func NewMissingBearerTokenValueError() app.Error {
	const str = "Unauthorized access: Missing Authorization header Bearer token value"
	return app.NewAuthorizationError(str, str)
}

// NewInvalidBearerTokenValueError returns an app.Error indicating that the
// Bearer token provided is invalid or not recognized.
func NewInvalidBearerTokenValueError() app.Error {
	const str = "Forbidden access: Invalid Bearer token value"
	return app.NewAccessForbiddenError(str, str)
}

// NewBearerAuthScopesAssertionError returns an app.Error indicating that the
// authorization scopes assertion failed, usually due to missing or
// improperly formatted OpenAPI scope data in the request context.
//
// This error typically arises when the context key `openapi.BearerAuthScopes`
// is expected to hold a `[]string`, but the value is either missing or of an unexpected type.
func NewBearerAuthScopesAssertionError() app.Error {
	return app.NewAuthorizationError(
		fmt.Sprintf("Authorization scope assertion failure: expected to get string slice under %s user context key to properly extract the request scope.", openapi.BearerAuthScopes),
		"Unable to process request to the endpoint. Please verify the request content and try again later.")
}

// NewEmptyAccessScopesAssertionError returns an app.Error indicating that the
// authorization scope list exists in the context, but it is empty.
//
// This error is triggered when a `[]string` is found under the `openapi.BearerAuthScopes`
// context key, but the slice contains no elements, preventing proper access control evaluation.
func NewEmptyAccessScopesAssertionError() app.Error {
	return app.NewAuthorizationError(
		fmt.Sprintf("Authorization scope assertion failure: expected to get non empty string slice under %s user context key.", openapi.BearerAuthScopes),
		"Unable to process request to the endpoint. Please verify the request content and try again later.")
}
