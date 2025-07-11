package ports

import (
	"errors"

	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/ports/openapi"
	"github.com/gofiber/fiber/v2"
)

// ErrorHandler returns a Fiber error handler that translates application-level errors
// into appropriate HTTP status codes and JSON responses. The handler maps specific
// error types to corresponding HTTP status codes and includes a user-friendly message
// (the slug) in the response body. If an error is unrecognized or zero, the handler
// returns a generic internal server error response.
func ErrorHandler() fiber.ErrorHandler {
	codes := map[app.ErrorType]int{
		app.ErrorTypeAuthorization:        fiber.StatusUnauthorized,
		app.ErrorTypeAccessForbidden:      fiber.StatusForbidden,
		app.ErrorTypeIncorrectInput:       fiber.StatusBadRequest,
		app.ErrorTypeOperationTimeout:     fiber.StatusRequestTimeout,
		app.ErrorTypeProviderFailure:      fiber.StatusInternalServerError,
		app.ErrorTypeRawDataProcessing:    fiber.StatusInternalServerError,
		app.ErrorTypeUnsupportedOperation: fiber.StatusNotFound,
	}

	return func(c *fiber.Ctx, err error) error {
		if err == nil {
			return nil
		}

		var fiberErr *fiber.Error
		if errors.As(err, &fiberErr) {
			return c.Status(fiberErr.Code).JSON(openapi.Error{Message: fiberErr.Message}) // TODO: Add more descriptive responses.
		}

		var appErr app.Error
		if !errors.As(err, &appErr) || appErr.IsZero() {
			return c.Status(fiber.StatusInternalServerError).JSON(NewUnhandledErrorTypeResponse())
		}

		code := codes[appErr.ErrorType()]
		return c.Status(code).JSON(openapi.Error{Message: appErr.Slug()})
	}
}

// NewUnhandledErrorTypeResponse is the default response returned when an error occurs
// that does not match any known or handled ErrorType.
// It represents a generic internal server error to avoid exposing internal details to the client.
func NewUnhandledErrorTypeResponse() openapi.Error {
	return openapi.Error{
		Message: "An internal error occurred during processing the request. Please try again later or contact the support team.",
	}
}

// NewRequestBodyParserError wraps a body parsing failure into a user-friendly application error,
// indicating that the input was malformed or invalid.
func NewRequestBodyParserError(err error) app.Error {
	return app.NewRawDataProcessingError(
		err.Error(),
		"Unable to process request with given request body. Please verify the request content and try again later.",
	)
}
