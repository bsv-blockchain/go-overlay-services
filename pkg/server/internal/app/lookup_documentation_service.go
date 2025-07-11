package app

import (
	"context"
)

// LookupServiceDocumentationProvider defines the contract for retrieving documentation
// for a lookup service provider.
type LookupServiceDocumentationProvider interface {
	GetDocumentationForLookupServiceProvider(lookupServiceName string) (string, error)
}

// LookupDocumentationService provides functionality for retrieving lookup service provider documentation.
type LookupDocumentationService struct {
	provider LookupServiceDocumentationProvider
}

// GetDocumentation retrieves documentation for a specific lookup service provider.
// Returns the documentation string on success, or an error if:
// - The lookup service name is empty (ErrorTypeIncorrectInput).
// - The provider fails to retrieve documentation (ErrorTypeProviderFailure).
func (s *LookupDocumentationService) GetDocumentation(ctx context.Context, lookupServiceName string) (string, error) {
	if lookupServiceName == "" {
		return "", NewEmptyLookupServiceNameError()
	}

	documentation, err := s.provider.GetDocumentationForLookupServiceProvider(lookupServiceName)
	if err != nil {
		return "", NewLookupServiceProviderDocumentationError(err)
	}

	return documentation, nil
}

// NewLookupDocumentationService creates a new LookupDocumentationService with the given provider.
// Panics if the provider is nil.
func NewLookupDocumentationService(provider LookupServiceDocumentationProvider) *LookupDocumentationService {
	if provider == nil {
		panic("lookup service provider documentation provider cannot be nil")
	}

	return &LookupDocumentationService{
		provider: provider,
	}
}

// NewEmptyLookupServiceNameError returns an Error indicating that the lookup service name is empty,
// which is invalid input when retrieving documentation.
func NewEmptyLookupServiceNameError() Error {
	return Error{
		errorType: ErrorTypeIncorrectInput,
		err:       "lookup service name cannot be empty",
		slug:      "A valid lookupService must be provided to retrieve documentation.",
	}
}

// NewLookupServiceProviderDocumentationError returns an Error indicating that the configured provider
// failed to retrieve documentation for the lookup service.
func NewLookupServiceProviderDocumentationError(err error) Error {
	return Error{
		errorType: ErrorTypeProviderFailure,
		err:       "unable to retrieve documentation for lookup service provider",
		slug:      "Unable to retrieve documentation for lookup service provider due to an internal error. Please try again later or contact the support team.",
	}
}
