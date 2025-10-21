package app

import "fmt"

// ErrorType represents a generic category of error used as descriptor
// to clarify the nature of a failure that occurred in dependencies.
type ErrorType struct {
	s string
}

var (
	// ErrorTypeProviderFailure indicates a failure in a service dependency or provider.
	ErrorTypeProviderFailure = ErrorType{"provider-failure"}
	// ErrorTypeAuthorization indicates an authentication or authorization failure.
	ErrorTypeAuthorization = ErrorType{"authorization"}
	// ErrorTypeAccessForbidden indicates that access to a resource is forbidden.
	ErrorTypeAccessForbidden = ErrorType{"access-forbidden"}
	// ErrorTypeIncorrectInput indicates that the provided input is invalid or malformed.
	ErrorTypeIncorrectInput = ErrorType{"incorrect-input"}
	// ErrorTypeUnknown indicates an unclassified or unexpected error.
	ErrorTypeUnknown = ErrorType{"unknown"}
	// ErrorTypeOperationTimeout indicates that an operation exceeded its time limit.
	ErrorTypeOperationTimeout = ErrorType{"operation-timeout"}
	// ErrorTypeRawDataProcessing indicates an error during raw data processing.
	ErrorTypeRawDataProcessing = ErrorType{"raw-data-processing"}
	// ErrorTypeUnsupportedOperation indicates that the requested operation is not supported.
	ErrorTypeUnsupportedOperation = ErrorType{"unsupported-operation"}
)

// Error defines a generic application-layer error that should be translated
// into a specific response format for the requester.
//
// The error includes a err source message, a type indicating the category
// of the failure, and a slug string representing the error message content
// to be returned to the requester. The error type is used during translation
// process in the error-handling implementation.
//
// The source error message may contain internal details, so it is not recommended
// to include it in the final response to avoid exposing sensitive information.
// Instead, it is highly recommended to use the slug string, which is intended
// for the response, ensuring no sensitive data is leaked to the requester.
type Error struct {
	err       string
	slug      string
	errorType ErrorType
}

// Slug returns the error slug identifier.
func (e Error) Slug() string { return e.slug }

// IsZero returns true if the error is the zero value.
func (e Error) IsZero() bool { return e == Error{} }

// Error returns the error message string.
func (e Error) Error() string { return e.err }

// ErrorType returns the type of error.
func (e Error) ErrorType() ErrorType { return e.errorType }

// NewUnsupportedOperationError creates an error for unsupported operations.
func NewUnsupportedOperationError(err, slug string) Error {
	return Error{
		slug:      slug,
		err:       err,
		errorType: ErrorTypeUnsupportedOperation,
	}
}

// NewIncorrectInputError returns an error that handles invalid input data,
// typically caused by partial state, inappropriate data formats, or other
// issues related to incorrect input.
func NewIncorrectInputError(err, slug string) Error {
	return Error{
		slug:      slug,
		err:       err,
		errorType: ErrorTypeIncorrectInput,
	}
}

// NewProviderFailureError returns an error that handles service dependency failures,
// internal processing issues, unavailability, connection problems, or other issues
// that should not be exposed to the requester.
func NewProviderFailureError(err, slug string) Error {
	return Error{
		slug:      slug,
		err:       err,
		errorType: ErrorTypeProviderFailure,
	}
}

// NewAuthorizationError returns an error that handles authorization failures,
// such as missing or invalid credentials when attempting to access a restricted resource.
func NewAuthorizationError(err, slug string) Error {
	return Error{
		slug:      slug,
		err:       err,
		errorType: ErrorTypeAuthorization,
	}
}

// NewAccessForbiddenError returns an error that handles access control failures,
// such as valid credentials without the necessary permissions to access a resource.
func NewAccessForbiddenError(err, slug string) Error {
	return Error{
		slug:      slug,
		err:       err,
		errorType: ErrorTypeAccessForbidden,
	}
}

// NewRawDataProcessingError returns an error that handles issues encountered
// during raw data processing, such as invalid or corrupt input data that prevents
// successful processing.
func NewRawDataProcessingError(err, slug string) Error {
	return Error{
		slug:      slug,
		errorType: ErrorTypeRawDataProcessing,
		err:       err,
	}
}

// NewRawDataProcessingWithFieldError returns an error that handles issues encountered
// during raw data processing related to the specific field. Such as invalid or corrupt input
// data that prevents successful processing.
func NewRawDataProcessingWithFieldError(err error, field string) Error {
	return NewRawDataProcessingError(
		err.Error(),
		fmt.Sprintf("Unable to process data structure the '%s' field. Please verify the content and try again later.", field),
	)
}

// NewUnknownError returns an error that represents an unexpected or unclassified
// issue that doesn't fall into predefined error categories. Useful as a fallback
// when the exact nature of the error is unclear.
func NewUnknownError(err, slug string) Error {
	return Error{
		slug:      slug,
		errorType: ErrorTypeUnknown,
		err:       err,
	}
}

// NewIncorrectInputWithFieldError returns an error indicating that a specific input field is invalid.
// This is typically caused by partial state, incorrect data formats, or other issues related to user input.
func NewIncorrectInputWithFieldError(field string) Error {
	msg := fmt.Sprintf("Unable to process the '%s' field. Please verify the content and try again.", field)
	return NewIncorrectInputError(
		msg,
		msg,
	)
}

// NewContextCancellationError returns an error indicating that the submitted request exceeded the context timeout limit or
// that a context cancellation signal was emitted.
func NewContextCancellationError() Error {
	const msg = "The submitted request context has been canceled or exceeds the timeout limit."
	return Error{
		errorType: ErrorTypeOperationTimeout,
		err:       msg,
		slug:      msg,
	}
}
