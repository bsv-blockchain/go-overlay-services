package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/engine"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app/jsonutil"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/middleware"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/adaptor"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/idempotency"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/google/uuid"
	"github.com/gookit/slog"
)

// Config holds the configuration settings for the HTTP server
type Config struct {
	// AppName is the name of the application.
	AppName string `mapstructure:"app_name"`

	// Port is the TCP port on which the server will listen.
	Port int `mapstructure:"port"`

	// Addr is the address the server will bind to.
	Addr string `mapstructure:"addr"`

	// ServerHeader is the value of the Server header returned in HTTP responses.
	ServerHeader string `mapstructure:"server_header"`

	// AdminBearerToken is the token required to access admin-only endpoints.
	AdminBearerToken string `mapstructure:"admin_bearer_token"`

	// Token for authenticating to ARC when submitting transactions.
	ArcApiKey string `mapstructure:"arc_api_key"`

	// Token for authenticating requests from ARC to callback endpoints.
	ArcCallbackToken string `mapstructure:"arc_callback_token"`
}

// DefaultConfig provides a default configuration with reasonable values for local development.
var DefaultConfig = Config{
	AppName:          "Overlay API v0.0.0",
	Port:             3000,
	Addr:             "localhost",
	ServerHeader:     "Overlay API",
	AdminBearerToken: uuid.NewString(),
	ArcCallbackToken: uuid.NewString(),
	ArcApiKey:        "",
}

// HTTPOption defines a functional option for configuring an HTTP server.
// These options allow for flexible setup of middlewares and configurations.
type HTTPOption func(*HTTP) error

// WithMiddleware adds custom middleware to the HTTP server.
// The execution order depends on the sequence in which the middlewares are passed
func WithMiddleware(f func(http.Handler) http.Handler) HTTPOption {
	return func(h *HTTP) error {
		h.middleware = append(h.middleware, adaptor.HTTPMiddleware(f))
		return nil
	}
}

// WithFiberMiddleware adds Fiber middleware to the HTTP server.
func WithFiberMiddleware(f fiber.Handler) HTTPOption {
	return func(h *HTTP) error {
		h.middleware = append(h.middleware, f)
		return nil
	}
}

// WithConfig sets the configuration for the HTTP server.
func WithConfig(cfg *Config) HTTPOption {
	return func(h *HTTP) error {
		h.cfg = cfg
		h.app = fiber.New(fiber.Config{
			CaseSensitive: true,
			StrictRouting: true,
			ServerHeader:  cfg.ServerHeader,
			AppName:       cfg.AppName,
		})
		return nil
	}
}

// WithEngine sets the overlay engine provider for the HTTP server.
func WithEngine(engineProvider engine.OverlayEngineProvider) HTTPOption {
	return func(h *HTTP) error {
		if engineProvider == nil {
			return fmt.Errorf("engine provider is nil")
		}
		overlayAPI, err := app.New(engineProvider)
		if err != nil {
			return fmt.Errorf("failed to create overlay API: %w", err)
		}
		h.api = overlayAPI
		return nil
	}
}

// WithBodyClose is a middleware that ensures the request body is closed after processing.
// This is important for memory management and preventing resource leaks.
// It is particularly useful when using http.HandlerFunc to handle requests.
func WithBodyClose(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := r.Body.Close(); err != nil {
				slog.Error("failed to close request body", "error", err)
			}
		}()
		h(w, r)
	}
}

// SafeHandler is a wrapper for http.HandlerFunc that ensures the request body is closed after processing.
// This is important for memory management and preventing resource leaks.
// It is particularly useful when using http.HandlerFunc to handle requests.
// It is a convenience function that combines WithBodyClose with adaptor.HTTPHandlerFunc.
func SafeHandler(h http.HandlerFunc) fiber.Handler {
	return adaptor.HTTPHandlerFunc(WithBodyClose(h))
}

// HTTP manages connections to the overlay server instance. It accepts and responds to client sockets,
// using idempotency to improve fault tolerance and mitigate duplicated requests.
// It applies all configured options along with the list of middlewares.
type HTTP struct {
	middleware  []fiber.Handler
	app         *fiber.App
	cfg         *Config
	api         *app.Application
	Router      fiber.Router
	AdminRouter fiber.Router
}

// New returns an instance of the HTTP server and applies all specified functional options before starting it.
func New(opts ...HTTPOption) (*HTTP, error) {
	http := &HTTP{
		cfg: &DefaultConfig,
		app: fiber.New(fiber.Config{
			CaseSensitive: true,
			StrictRouting: true,
			ServerHeader:  "Overlay API",
			AppName:       "Overlay API v0.0.0",
		}),
		middleware: []fiber.Handler{
			idempotency.New(),
			cors.New(),
			recover.New(
				recover.Config{
					EnableStackTrace: true,
				},
			),
		},
	}

	for _, o := range opts {
		if err := o(http); err != nil {
			return nil, err
		}
	}

	for _, m := range http.middleware {
		http.app.Use(m)
	}

	if http.api == nil {
		overlayAPI, err := app.New(NewNoopEngineProvider())
		if err != nil {
			return nil, fmt.Errorf("failed to create overlay API: %w", err)
		}
		http.api = overlayAPI
	}

	// Routes...
	api := http.app.Group("/api")
	v1 := api.Group("/v1")
	http.Router = v1

	// Non-Admin:
	v1.Post("/arc-ingest", adaptor.HTTPMiddleware(middleware.ArcCallbackTokenMiddleware(http.cfg.ArcCallbackToken, http.cfg.ArcApiKey)), SafeHandler(http.api.Commands.ArcIngestHandler.Handle))
	v1.Get("/getDocumentationForTopicManager", SafeHandler(http.api.Queries.TopicManagerDocumentationHandler.Handle))
	v1.Get("/getDocumentationForLookupServiceProvider", SafeHandler(http.api.Queries.LookupServiceDocumentationHandler.Handle))
	v1.Get("/listLookupServiceProviders", SafeHandler(http.api.Queries.LookupServicesListHandler.Handle))
	v1.Get("/listTopicManagers", SafeHandler(http.api.Queries.TopicManagerDocumentationHandler.Handle))

	v1.Post("/submit", SafeHandler(http.api.Commands.SubmitTransactionHandler.Handle))
	v1.Post("/lookup", SafeHandler(http.api.Commands.LookupQuestionHandler.Handle))
	v1.Post("/requestSyncResponse", SafeHandler(http.api.Commands.RequestSyncResponseHandler.Handle))
	v1.Post("/requestForeignGASPNode", SafeHandler(http.api.Commands.RequestForeignGASPNodeHandler.Handle))

	// Admin:
	admin := v1.Group("/admin", adaptor.HTTPMiddleware(AdminAuth(http.cfg.AdminBearerToken)))
	http.AdminRouter = admin
	admin.Post("/advertisements-sync", SafeHandler(http.api.Commands.SyncAdvertismentsHandler.Handle))
	admin.Post("/start-gasp-sync", SafeHandler(http.api.Commands.StartGASPSyncHandler.Handle))

	return http, nil
}

// SocketAddr builds the address string for binding.
func (h *HTTP) SocketAddr() string {
	return fmt.Sprintf("%s:%d", h.cfg.Addr, h.cfg.Port)
}

// ListenAndServe handles HTTP requests from the configured socket address.
func (h *HTTP) ListenAndServe() error {
	if err := h.app.Listen(h.SocketAddr()); err != nil {
		return fmt.Errorf("http server: fiber app listen failed: %w", err)
	}
	return nil
}

// AdminAuth is a middleware that checks the Authorization header for a valid Bearer token.
// protects the HTTP server from unauthorized access.
// It checks for a Bearer token in the Authorization header and compares it to the expected value.
func AdminAuth(expectedToken string) func(http.Handler) http.Handler {
	type AuthorizationFailureResponse struct {
		Status  string `json:"error"`
		Message string `json:"message"`
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get("Authorization")
			if auth == "" {
				jsonutil.SendHTTPResponse(w, http.StatusUnauthorized, AuthorizationFailureResponse{
					Status:  http.StatusText(http.StatusUnauthorized),
					Message: "Missing Authorization header in the request",
				})
				return
			}

			if !strings.HasPrefix(auth, "Bearer ") {
				jsonutil.SendHTTPResponse(w, http.StatusUnauthorized, AuthorizationFailureResponse{
					Status:  http.StatusText(http.StatusUnauthorized),
					Message: "Missing Authorization header Bearer token value",
				})
				return
			}

			token := strings.TrimPrefix(auth, "Bearer ")
			if token != expectedToken {
				jsonutil.SendHTTPResponse(w, http.StatusForbidden, AuthorizationFailureResponse{
					Status:  http.StatusText(http.StatusForbidden),
					Message: "Invalid Bearer token value",
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// StartWithGracefulShutdown starts the HTTP server and listens for termination signals.
// It returns a channel that will be closed once the shutdown is complete.
func (h *HTTP) StartWithGracefulShutdown(ctx context.Context) <-chan struct{} {
	idleConnsClosed := make(chan struct{})

	go func() {
		sigint := make(chan os.Signal, 1)
		signal.Notify(sigint, os.Interrupt, syscall.SIGTERM)

		select {
		case <-sigint:
			slog.Info("Shutdown signal received. Cleaning up...")
		case <-ctx.Done():
			slog.Info("Shutdown context canceled. Cleaning up...")
		}

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := h.app.ShutdownWithContext(shutdownCtx); err != nil {
			slog.Errorf("HTTP shutdown error: %v", err)
		}

		close(idleConnsClosed)
	}()

	go func() {
		if err := h.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Errorf("HTTP server error: %v", err)
		}
	}()

	return idleConnsClosed
}
