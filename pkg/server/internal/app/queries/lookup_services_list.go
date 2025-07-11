package queries

import (
	"fmt"
	"net/http"

	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app/jsonutil"
	"github.com/bsv-blockchain/go-sdk/overlay"
)

// LookupMetadata represents the metadata for a lookup service provider.
type LookupMetadata struct {
	Name             string  `json:"name"`
	ShortDescription string  `json:"shortDescription"`
	IconURL          *string `json:"iconURL,omitempty"`
	Version          *string `json:"version,omitempty"`
	InformationURL   *string `json:"informationURL,omitempty"`
}

// LookupServicesListHandlerResponse defines the response body content that
// will be sent in JSON format after successfully processing the handler logic.
type LookupServicesListHandlerResponse map[string]LookupMetadata

// LookupServicesListProvider defines the contract that must be fulfilled
// to retrieve a list of lookup service providers from the overlay engine.
type LookupServicesListProvider interface {
	ListLookupServiceProviders() map[string]*overlay.MetaData
}

// LookupServicesListHandler orchestrates the processing flow of a lookup service provider list
// request, returning a map of lookup service provider metadata with appropriate HTTP status.
type LookupServicesListHandler struct {
	provider LookupServicesListProvider
}

// Handle processes the lookup service provider list request and sends a JSON response.
func (l *LookupServicesListHandler) Handle(w http.ResponseWriter, r *http.Request) {
	engineLookupProviders := l.provider.ListLookupServiceProviders()
	result := make(LookupServicesListHandlerResponse, len(engineLookupProviders))

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

	for name, metadata := range engineLookupProviders {
		lookupMetadata := LookupMetadata{
			Name:             name,
			ShortDescription: "No description available",
		}

		if metadata != nil {
			lookupMetadata.ShortDescription = coalesce(metadata.Description, "No description available")
			lookupMetadata.IconURL = setIfNotEmpty(metadata.Icon)
			lookupMetadata.Version = setIfNotEmpty(metadata.Version)
			lookupMetadata.InformationURL = setIfNotEmpty(metadata.InfoUrl)
		}

		result[name] = lookupMetadata
	}

	jsonutil.SendHTTPResponse(w, http.StatusOK, result)
}

// NewLookupServicesListHandler returns a new LookupServicesListHandler
// initialized with the given provider. It panics if the provider is nil.
func NewLookupServicesListHandler(provider LookupServicesListProvider) (*LookupServicesListHandler, error) {
	if provider == nil {
		return nil, fmt.Errorf("lookup services list provider cannot be nil")
	}
	return &LookupServicesListHandler{provider: provider}, nil
}
