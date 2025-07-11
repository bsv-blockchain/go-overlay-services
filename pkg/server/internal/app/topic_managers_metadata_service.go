package app

import "github.com/bsv-blockchain/go-sdk/overlay"

// TopicManagersListProvider defines the interface for retrieving
// metadata about topic manager services from the overlay engine.
//
// Implementations of this interface are expected to return a mapping
// between unique service identifiers and their corresponding metadata.
type TopicManagersListProvider interface {
	// ListTopicManagers returns a map of service identifiers to their associated metadata.
	// This information is used to describe available topic manager services in client-facing APIs.
	ListTopicManagers() map[string]*overlay.MetaData
}

// TopicManagersMetadataService provides metadata about topic manager services.
// It implements the MetadataProvider interface and is responsible for responding
// to metadata requests of type TopicManagersServiceMetadataType.
type TopicManagersMetadataService struct {
	provider TopicManagersListProvider
}

// GetMetadata retrieves and converts topic manager metadata from the provider
// into a MetadataDTO format suitable for API exposure.
func (t *TopicManagersMetadataService) GetMetadata() MetadataDTO {
	managers := t.provider.ListTopicManagers()
	dto := make(MetadataDTO, len(managers))

	for service, metadata := range managers {
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

// CanBeApplied determines whether this metadata provider is responsible
// for handling the specified metadata type.
func (t *TopicManagersMetadataService) CanBeApplied(m MetadataType) bool {
	return m == TopicManagersServiceMetadataType
}

// NewTopicManagersMetadataService creates a new instance of TopicManagersMetadataService.
// Panics if the provided TopicManagersListProvider is nil.
func NewTopicManagersMetadataService(provider TopicManagersListProvider) *TopicManagersMetadataService {
	if provider == nil {
		panic("topic manager list provider is nil")
	}
	return &TopicManagersMetadataService{provider: provider}
}
