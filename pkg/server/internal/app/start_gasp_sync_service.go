package app

import (
	"context"
)

// StartGASPSyncProvider defines the interface for triggering GASP sync.
type StartGASPSyncProvider interface {
	StartGASPSync(ctx context.Context) error
}

// StartGASPSyncService coordinates the GASP synchronization process.
type StartGASPSyncService struct {
	provider StartGASPSyncProvider
}

// StartGASPSync initiates the GASP synchronization process using the configured provider.
// Returns nil on success, an error if the provider fails.
func (s *StartGASPSyncService) StartGASPSync(ctx context.Context) error {
	if err := s.provider.StartGASPSync(ctx); err != nil {
		return NewStartGASPSyncProviderError(err)
	}
	return nil
}

// NewStartGASPSyncService creates a new StartGASPSyncService with the given provider.
// Returns an error if the provider is nil.
func NewStartGASPSyncService(provider StartGASPSyncProvider) *StartGASPSyncService {
	if provider == nil {
		panic("provider is nil")
	}

	return &StartGASPSyncService{ provider: provider }
}

// NewStartGASPSyncProviderError returns an Error indicating that the configured provider
// failed to process a GASP sync request.
func NewStartGASPSyncProviderError(err error) Error {
	return Error{
		errorType: ErrorTypeProviderFailure,
		err:       err.Error(),
		slug:      "Unable to synchronize GASP due to an internal error. Please try again later or contact the support team.",
	}
}
