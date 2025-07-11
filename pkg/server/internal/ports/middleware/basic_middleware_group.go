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
	OctetStreamLimit int64 // Max allowed body size for octet-stream requests.
	EnableStackTrace bool  // Enable stack traces in panic recovery middleware.
}

// BasicMiddlewareGroup returns a list of preconfigured middleware for the HTTP server.
// It includes logging, CORS, request ID generation, panic recovery, PProf, request size limiting, health check.
func BasicMiddlewareGroup(cfg BasicMiddlewareGroupConfig) []fiber.Handler {
	return []fiber.Handler{
		requestid.New(),
		idempotency.New(),
		cors.New(),
		recover.New(recover.Config{EnableStackTrace: cfg.EnableStackTrace}),
		logger.New(logger.Config{
			Format:     "date=${time} request_id=${locals:requestid} status=${status} method=${method} path=${path} err=${error}\n",
			TimeFormat: "02-Jan-2006 15:04:05",
		}),
		healthcheck.New(),
		pprof.New(pprof.Config{Prefix: "/api/v1"}),
		LimitOctetStreamBodyMiddleware(cfg.OctetStreamLimit),
	}
}
