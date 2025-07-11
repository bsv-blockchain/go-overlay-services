package queries

import (
	"fmt"
	"net/http"

	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app/jsonutil"
)

// LookupServiceDocumentationHandlerResponse defines the response body content that
// will be sent in JSON format after successfully processing the handler logic.
type LookupServiceDocumentationHandlerResponse struct {
	Documentation string `json:"documentation"`
}

// LookupServiceDocumentationProvider defines the contract that must be fulfilled
// to send a lookup service documentation request to the overlay engine for further processing.
// Note: The contract definition is still in development and will be updated after
// migrating the engine code.
type LookupServiceDocumentationProvider interface {
	GetDocumentationForLookupServiceProvider(lookupService string) (string, error)
}

// LookupServiceDocumentationHandler orchestrates the processing flow of a lookup documentation
// request, including the request parameter validation, converting the request
// into an overlay-engine-compatible format, and applying any other necessary
// logic before invoking the engine. It returns the requested lookup service
// documentation in the text/markdown format.
type LookupServiceDocumentationHandler struct {
	provider LookupServiceDocumentationProvider
}

// Handle orchestrates the processing flow of a lookup documentation request.
// It extracts the lookupService query parameter, invokes the engine provider,
// and returns the a Markdown-formatted documentation string as JSON with the appropriate status code.
func (l *LookupServiceDocumentationHandler) Handle(w http.ResponseWriter, r *http.Request) {
	lookupService := r.URL.Query().Get("lookupService")
	if lookupService == "" {
		http.Error(w, "lookupService query parameter is required", http.StatusBadRequest)
		return
	}

	documentation, err := l.provider.GetDocumentationForLookupServiceProvider(lookupService)
	if err != nil {
		jsonutil.SendHTTPInternalServerErrorTextResponse(w)
		return
	}

	jsonutil.SendHTTPResponse(w, http.StatusOK, LookupServiceDocumentationHandlerResponse{
		Documentation: documentation,
	})
}

// NewLookupServiceDocumentationHandler creates a new LookupServiceDocumentationHandler
// using the given LookupDocumentationProvider. It panics if the provider is nil.
func NewLookupServiceDocumentationHandler(provider LookupServiceDocumentationProvider) (*LookupServiceDocumentationHandler, error) {
	if provider == nil {
		return nil, fmt.Errorf("lookup documentation provider cannot be nil")
	}
	return &LookupServiceDocumentationHandler{provider: provider}, nil
}
