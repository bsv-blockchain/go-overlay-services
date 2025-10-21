// Package decorators provides HTTP handler decorators for authentication and authorization.
package decorators

import (
	"strings"

	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app"
	"github.com/gofiber/fiber/v2"
)

// Handler defines an interface for handling a fiber.Ctx.
// This abstraction allows decorators or middleware to wrap around other handlers.
type Handler interface {
	Handle(c *fiber.Ctx) error
}

// ARCAuthorizationDecoratorConfig contains the configuration required
// to enable and validate ARC-style authorization on an endpoint.
type ARCAuthorizationDecoratorConfig struct {
	APIKey        string // ARC API key required to enable this endpoint.
	CallbackToken string // Expected token value to authorize the request.
	Scheme        string // Authorization scheme prefix (usually "Bearer ").
}

// ARCAuthorizationDecorator is a middleware that enforces ARC-style authorization
// based on a configured API key and expected callback token.
// If authorization is valid, it delegates the request to the next handler.
type ARCAuthorizationDecorator struct {
	cfg  *ARCAuthorizationDecoratorConfig
	next Handler
}

// Handle enforces ARC-style authorization by validating the presence and correctness
// of the Authorization header against the provided configuration. Returns appropriate
// errors if any validation step fails. If valid, it forwards the request to the next handler.
func (a *ARCAuthorizationDecorator) Handle(c *fiber.Ctx) error {
	if a.cfg.APIKey == "" {
		return NewUnsupportedEndpointError()
	}

	auth := c.Get(fiber.HeaderAuthorization)
	if auth == "" {
		return NewMissingAuthHeaderError()
	}

	if !strings.HasPrefix(auth, a.cfg.Scheme) {
		return NewInvalidBearerTokenSchema()
	}

	token := strings.TrimPrefix(auth, a.cfg.Scheme)
	if token != a.cfg.CallbackToken {
		return NewInvalidBearerTokenError()
	}

	return a.next.Handle(c)
}

// NewArcAuthorizationDecorator constructs a new ARCAuthorizationDecorator,
// wrapping a given handler with authorization logic. Panics if either `next` or `cfg` is nil.
func NewArcAuthorizationDecorator(next Handler, cfg *ARCAuthorizationDecoratorConfig) *ARCAuthorizationDecorator {
	if next == nil {
		panic("next handler cannot be nil")
	}

	if cfg == nil {
		panic("arc authorization decorator config cannot be nil")
	}

	return &ARCAuthorizationDecorator{next: next, cfg: cfg}
}

// NewInvalidBearerTokenSchema returns an authorization error indicating that
// the Authorization header does not follow the expected "Bearer <token>" schema.
// This typically occurs when the header is present but does not begin with "Bearer ".
func NewInvalidBearerTokenSchema() app.Error {
	const msg = "Invalid Authorization header format: expected 'Bearer <token>'."
	return app.NewAuthorizationError(msg, msg)
}

// NewMissingAuthHeaderError returns an authorization error indicating that
// the Authorization header is completely missing from the request.
// This typically means the client failed to include any credentials.
func NewMissingAuthHeaderError() app.Error {
	const msg = "Authorization header is missing from the request."
	return app.NewAuthorizationError(msg, msg)
}

// NewInvalidBearerTokenError returns a forbidden access error indicating that
// the provided Bearer token is present but invalid (e.g., malformed, expired, or unrecognized).
func NewInvalidBearerTokenError() app.Error {
	const msg = "The Bearer token provided is invalid or has expired."
	return app.NewAccessForbiddenError(msg, msg)
}

// NewUnsupportedEndpointError returns an error indicating that
// the endpoint is not enabled or allowed in the current deployment or configuration.
// This is useful for API stubs, disabled features, or restricted environments.
func NewUnsupportedEndpointError() app.Error {
	const msg = "This endpoint is not supported by the current service configuration."
	return app.NewUnsupportedOperationError(msg, msg)
}
