package queries

import (
	"fmt"
	"net/http"

	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app/jsonutil"
	"github.com/bsv-blockchain/go-sdk/overlay"
)

// TopicManagerMetadata represents the metadata for a topic manager.
type TopicManagerMetadata struct {
	Name           string  `json:"name"`
	Description    string  `json:"shortDescription"`
	IconURL        *string `json:"iconURL,omitempty"`
	Version        *string `json:"version,omitempty"`
	InformationURL *string `json:"informationURL,omitempty"`
}

// TopicManagersListHandlerResponse defines the response body content that
// will be sent in JSON format after successfully processing the handler logic.
type TopicManagersListHandlerResponse map[string]TopicManagerMetadata

// TopicManagerListProvider defines the contract that must be fulfilled
// to retrieve a list of topic managers from the overlay engine.
type TopicManagerListProvider interface {
	ListTopicManagers() map[string]*overlay.MetaData
}

// TopicManagersListHandler orchestrates the processing flow of a topic manager list
// request, returning a map of topic manager metadata with appropriate HTTP status.
type TopicManagersListHandler struct {
	provider TopicManagerListProvider
}

// Handle processes the topic manager list request and sends a JSON response.
func (t *TopicManagersListHandler) Handle(w http.ResponseWriter, r *http.Request) {
	engineTopicManagers := t.provider.ListTopicManagers()
	result := make(TopicManagersListHandlerResponse, len(engineTopicManagers))

	setIfNotEmpty := func(s string) *string {
		if s == "" {
			return nil
		}
		return &s
	}

	coalesce := func(primary, fallback string) string {
		if primary != "" {
			return primary
		}
		return fallback
	}

	for name, metadata := range engineTopicManagers {
		tmMetadata := TopicManagerMetadata{
			Name:        name,
			Description: "No description available",
		}

		if metadata != nil {
			tmMetadata.Description = coalesce(metadata.Description, "No description available")
			tmMetadata.IconURL = setIfNotEmpty(metadata.Icon)
			tmMetadata.Version = setIfNotEmpty(metadata.Version)
			tmMetadata.InformationURL = setIfNotEmpty(metadata.InfoUrl)
		}

		result[name] = tmMetadata
	}

	jsonutil.SendHTTPResponse(w, http.StatusOK, result)
}

// NewTopicManagersListHandler returns an instance of TopicManagerListHandler.
// If the provided provider is nil, it panics.
func NewTopicManagersListHandler(provider TopicManagerListProvider) (*TopicManagersListHandler, error) {
	if provider == nil {
		return nil, fmt.Errorf("topic manager list provider cannot be nil")
	}
	return &TopicManagersListHandler{provider: provider}, nil
}
