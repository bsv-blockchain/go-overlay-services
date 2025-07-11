package app

import (
	"context"
)

// SyncAdvertisementsProvider defines the contract that must be fulfilled
// to send a synchronize advertisements request to the overlay engine for further processing.
type SyncAdvertisementsProvider interface {
	SyncAdvertisements(ctx context.Context) error
}

// AdvertisementsSyncService is responsible for synchronizing advertisements
// using the configured SyncAdvertisementsProvider.
type AdvertisementsSyncService struct {
	provider SyncAdvertisementsProvider
}

// SyncAdvertisements delegates the advertisement synchronization task to the configured provider.
// If an error occurs, it wraps it as a SyncAdvertisementsProviderError to hide internal details
// and return a slug message to the requester.
func (a *AdvertisementsSyncService) SyncAdvertisements(ctx context.Context) error {
	err := a.provider.SyncAdvertisements(ctx)
	if err != nil {
		return NewSyncAdvertisementsProviderError(err)
	}
	return nil
}

// NewAdvertisementsSyncService creates a new instance of AdvertisementsSyncService
// using the given SyncAdvertisementsProvider. It panics if the provider is nil.
func NewAdvertisementsSyncService(provider SyncAdvertisementsProvider) *AdvertisementsSyncService {
	if provider == nil {
		panic("sync advertisements provider is nil")
	}

	return &AdvertisementsSyncService{provider: provider}
}

// NewSyncAdvertisementsProviderError returns an Error indicating a failure
// in the provider while processing a sync advertisements request.
// Typically used when the overlay engine encounters an issue.
func NewSyncAdvertisementsProviderError(err error) Error {
	return Error{
		errorType: ErrorTypeProviderFailure,
		err:       err.Error(),
		slug:      "Unable to process sync advertisements request due to issues with the overlay engine.",
	}
}
