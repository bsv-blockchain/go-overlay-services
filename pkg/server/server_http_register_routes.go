package server

import (
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/engine"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/adapters"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/ports"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/ports/decorators"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/ports/middleware"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/ports/openapi"
)

// GetErrorHandler returns the error handler that translates application-level errors
// into appropriate HTTP status codes and JSON responses.
// Use this in your fiber.Config when creating the app if you want proper error handling.
func GetErrorHandler() fiber.ErrorHandler {
	return ports.ErrorHandler()
}

// DefaultRegisterRoutesConfig provides a default configuration with reasonable values for local development.
var DefaultRegisterRoutesConfig = RegisterRoutesConfig{
	ARCAPIKey:        "",
	ARCCallbackToken: uuid.NewString(),
	AdminBearerToken: uuid.NewString(),
	Engine:           adapters.NewNoopEngineProvider(),
	OctetStreamLimit: middleware.ReadBodyLimit1GB,
}

// RegisterRoutesConfig holds the configuration settings for the Overlay Engine HTTP API.
type RegisterRoutesConfig struct {
	// ARCAPIKey is the API key used for ARC service integration.
	ARCAPIKey string

	// ARCCallbackToken is the token used to authenticating ARC callback requests.
	ARCCallbackToken string

	// AdminBearerToken is the token required to access admin-only endpoints.
	AdminBearerToken string

	// Engine is a custom implementation of the overlay engine that serves
	// as the main processor for incoming HTTP requests.
	Engine engine.OverlayEngineProvider

	// OctetStreamLimit defines the maximum size (in bytes) for reading applicaction/octet-stream
	// request bodies. By default, it is set to 1GB to protect against excessively large payloads.
	OctetStreamLimit int64

	// IncludeLogger determines whether to include the built-in request logger middleware.
	// When false (default), the app owner should provide their own logger.
	// When true, includes a logger with request_id, status, method, path, and error logging.
	IncludeLogger bool

	// BaseURL is the base path prefix for all routes (e.g., "/api/v1").
	// When empty, routes are registered at the root level.
	BaseURL string
}

// RegisterRoutesWithErrorHandler wraps RegisterRoutes by injecting a predefined error handler
// that translates application-level errors into appropriate HTTP status codes and JSON responses.
func RegisterRoutesWithErrorHandler(app *fiber.App, cfg *RegisterRoutesConfig) *fiber.App {
	if app == nil {
		panic("fiber app is nil: expected a valid *fiber.App instance")
	}
	if cfg == nil {
		panic("register routes config is nil: expected a non-nil config")
	}

	extendedCfg := app.Config()
	extendedCfg.ErrorHandler = ports.ErrorHandler()
	extendedApp := fiber.New(extendedCfg)
	return RegisterRoutes(extendedApp, cfg)
}

// RegisterRoutes returns a new instance of fiber.App with the provided settings.
// It accepts a RegisterRoutesConfig to set up API keys, processing limits,
// and a custom implementation of the overlay engine.
//
// The returned instance does not include a predefined error handler that
// translates application-level errors into appropriate HTTP status codes and JSON
// responses. To handle such errors, the error handling configuration of the provided
// fiber.App instance must be extended externally before calling this function.
// To use the predefined error handler, use RegisterRoutesWithErrorHandler instead.
func RegisterRoutes(app *fiber.App, cfg *RegisterRoutesConfig) *fiber.App {
	if app == nil {
		panic("fiber app is nil: expected a valid *fiber.App instance")
	}
	if cfg == nil {
		panic("register routes config is nil: expected a non-nil config")
	}

	registry := ports.NewHandlerRegistryService(cfg.Engine, &decorators.ARCAuthorizationDecoratorConfig{
		APIKey:        cfg.ARCAPIKey,
		CallbackToken: cfg.ARCCallbackToken,
		Scheme:        "Bearer ",
	})

	openapi.RegisterHandlersWithOptions(app, registry, openapi.FiberServerOptions{
		BaseURL: cfg.BaseURL,
		HandlerMiddleware: []fiber.Handler{
			middleware.BearerTokenAuthorizationMiddleware(cfg.AdminBearerToken),
		},
		GlobalMiddleware: middleware.BasicMiddlewareGroup(middleware.BasicMiddlewareGroupConfig{
			EnableStackTrace: true,
			OctetStreamLimit: cfg.OctetStreamLimit,
			IncludeLogger:    cfg.IncludeLogger,
			BaseURL:          cfg.BaseURL,
		}),
	})

	return app
}
