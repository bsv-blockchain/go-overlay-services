// Package middleware provides HTTP middleware components for request processing and server configuration.
package middleware

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/healthcheck"
	"github.com/gofiber/fiber/v2/middleware/idempotency"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/pprof"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/fiber/v2/middleware/requestid"
)

// BasicMiddlewareGroupConfig defines configuration options for building the middleware group.
type BasicMiddlewareGroupConfig struct {
	OctetStreamLimit int64  // Max allowed body size for octet-stream requests.
	EnableStackTrace bool   // Enable stack traces in panic recovery middleware.
	IncludeLogger    bool   // Include request logger middleware. Default is false to avoid duplicate logging.
	BaseURL          string // Base URL prefix for pprof and other path-dependent middleware.
}

// BasicMiddlewareGroup returns a list of preconfigured middleware for the HTTP server.
// It includes CORS, request ID generation, panic recovery, PProf, request size limiting, health check.
// Optionally includes logging based on configuration.
func BasicMiddlewareGroup(cfg BasicMiddlewareGroupConfig) []fiber.Handler {
	handlers := []fiber.Handler{
		requestid.New(),
		idempotency.New(),
		cors.New(),
		recover.New(recover.Config{EnableStackTrace: cfg.EnableStackTrace}),
	}

	// Only include logger if explicitly requested
	if cfg.IncludeLogger {
		handlers = append(handlers, logger.New(logger.Config{
			Format:     "date=${time} request_id=${locals:requestid} status=${status} method=${method} path=${path} err=${error}\n",
			TimeFormat: "02-Jan-2006 15:04:05",
		}))
	}

	handlers = append(handlers,
		healthcheck.New(),
		pprof.New(pprof.Config{Prefix: cfg.BaseURL}),
		LimitOctetStreamBodyMiddleware(cfg.OctetStreamLimit),
	)

	return handlers
}
