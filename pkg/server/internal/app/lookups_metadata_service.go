package app

import (
	"github.com/bsv-blockchain/go-sdk/overlay"
)

// LookupListProvider defines the interface for retrieving
// a list of lookup service providers from the overlay engine.
type LookupListProvider interface {
	ListLookupServiceProviders() map[string]*overlay.MetaData
}

// LookupsMetadataService provides metadata about lookup-list services.
// It implements the MetadataProvider interface and is responsible for
// handling metadata requests of type LookupsMetadataServiceMetadataType.
type LookupsMetadataService struct {
	provider LookupListProvider
}

// GetMetadata retrieves metadata from the lookup list provider and converts it
// into a MetadataDTO for external consumption (e.g., API responses).
func (m *LookupsMetadataService) GetMetadata() MetadataDTO {
	lookups := m.provider.ListLookupServiceProviders()
	dto := make(MetadataDTO, len(lookups))

	for service, metadata := range lookups {
		dto[service] = ServiceMetadataDTO{
			Name:        metadata.Name,
			Description: metadata.Description,
			IconURL:     metadata.Icon,
			Version:     metadata.Version,
			InfoURL:     metadata.InfoUrl,
		}
	}
	return dto
}

// CanBeApplied returns true if this service handles the given metadata type.
func (m *LookupsMetadataService) CanBeApplied(t MetadataType) bool {
	return t == LookupsMetadataServiceMetadataType
}

// NewLookupListService constructs a LookupsMetadataService instance
// using the provided LookupListProvider. Panics if the provider is nil.
func NewLookupListService(provider LookupListProvider) *LookupsMetadataService {
	if provider == nil {
		panic("lookup list provider is nil")
	}
	return &LookupsMetadataService{provider: provider}
}
