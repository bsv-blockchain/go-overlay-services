package app

import "fmt"

// MetadataType defines a category of metadata used to determine
// which service is responsible for handling a given metadata request.
type MetadataType struct {
	s string
}

// Predefined metadata types identifying the appropriate service handlers.
var (
	// LookupsMetadataServiceMetadataType is used for metadata managed by the lookups-metadata-service.
	LookupsMetadataServiceMetadataType = MetadataType{"lookups-metadata-service"}

	// TopicManagersServiceMetadataType is used for metadata managed by the topic-managers-metadata-service.
	TopicManagersServiceMetadataType = MetadataType{"topic-managers-service"}
)

// ServiceMetadataDTO represents descriptive metadata about a service provider.
// This structure is designed for client-facing use, such as API responses.
type ServiceMetadataDTO struct {
	Name        string // Human-readable name of the service.
	Description string // Short summary of the service's purpose or functionality.
	IconURL     string // URL to a visual icon representing the service.
	Version     string // Version identifier of the service implementation.
	InfoURL     string // Link to detailed documentation or external reference.
}

// MetadataDTO maps unique service identifiers to their metadata.
// It provides a lookup-friendly structure suitable for APIs and clients.
type MetadataDTO map[string]ServiceMetadataDTO

// MetadataProvider defines an interface for services that expose metadata.
// Each provider determines whether it can handle a given metadata type and
// returns the corresponding metadata if applicable.
type MetadataProvider interface {
	GetMetadata() MetadataDTO
	CanBeApplied(t MetadataType) bool
}

// MetadataService is the application-layer service responsible for retrieving
// metadata from the appropriate provider based on the requested metadata type.
type MetadataService struct {
	providers []MetadataProvider
}

// GetMetadata returns the metadata from the first provider that can handle
// the specified metadata type. If no suitable provider is found, an error is returned.
func (m *MetadataService) GetMetadata(t MetadataType) (MetadataDTO, error) {
	for _, p := range m.providers {
		if p.CanBeApplied(t) {
			return p.GetMetadata(), nil
		}
	}
	return nil, NewUnrecognizedMetadataType(t)
}

// NewMetadataService creates a new instance of MetadataService with the given providers.
// At least one provider must be specified; otherwise, the function will panic.
func NewMetadataService(providers ...MetadataProvider) *MetadataService {
	if len(providers) == 0 {
		panic("at least one metadata provider must be specified")
	}
	return &MetadataService{providers: providers}
}

// NewUnrecognizedMetadataType returns an error indicating that the requested metadata type
// is not supported by any registered metadata provider.
func NewUnrecognizedMetadataType(t MetadataType) Error {
	return NewUnknownError(
		fmt.Sprintf("No metadata provider is registered for type: %q. Please check the implementation for further diagnostics.", t.s),
		"Unable to process the metadata request. Please try again later or contact support.",
	)
}
