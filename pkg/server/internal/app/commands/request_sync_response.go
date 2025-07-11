package commands

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/gasp"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app/jsonutil"
)

// XBSVTopicHeader is the HTTP header name for BSV topic
const XBSVTopicHeader = "x-bsv-topic"

// ContentTypeHeader is the HTTP header name for content type
const ContentTypeHeader = "Content-Type"

// ContentTypeJSON is the HTTP content type for JSON
const ContentTypeJSON = "application/json"

// ErrMissingXBSVTopicHeader is returned when the x-bsv-topic header is missing from the request.
var ErrMissingXBSVTopicHeader = errors.New("missing 'x-bsv-topic' header")

// ErrSyncResponseMethodNotAllowed is returned when an unsupported HTTP method is used for sync response
var ErrSyncResponseMethodNotAllowed = errors.New("method not allowed")

// ErrSyncResponseInvalidRequestBody is returned when the sync response request body cannot be decoded
var ErrSyncResponseInvalidRequestBody = errors.New("invalid request body")

// ForeignSyncResponseProvider defines the contract for providing a foreign sync response.
type ForeignSyncResponseProvider interface {
	ProvideForeignSyncResponse(ctx context.Context, initialRequest *gasp.InitialRequest, topic string) (*gasp.InitialResponse, error)
}

// RequestSyncResponseHandler orchestrates the /request-sync-response flow.
type RequestSyncResponseHandler struct {
	provider ForeignSyncResponseProvider
}

// NewRequestSyncResponseHandler creates a new instance of the handler.
func NewRequestSyncResponseHandler(provider ForeignSyncResponseProvider) (*RequestSyncResponseHandler, error) {
	if provider == nil {
		return nil, fmt.Errorf("foreign sync response provider cannot be nil")
	}
	return &RequestSyncResponseHandler{provider: provider}, nil
}

// Handle processes the HTTP POST /request-sync-response request.
func (h *RequestSyncResponseHandler) Handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, ErrSyncResponseMethodNotAllowed.Error(), http.StatusMethodNotAllowed)
		return
	}

	topic := r.Header.Get(XBSVTopicHeader)
	if topic == "" {
		http.Error(w, ErrMissingXBSVTopicHeader.Error(), http.StatusBadRequest)
		return
	}

	var initialRequest gasp.InitialRequest
	if err := jsonutil.DecodeRequestBody(r, &initialRequest); err != nil {
		http.Error(w, ErrSyncResponseInvalidRequestBody.Error(), http.StatusBadRequest)
		return
	}

	resp, err := h.provider.ProvideForeignSyncResponse(r.Context(), &initialRequest, topic)
	if err != nil {
		jsonutil.SendHTTPInternalServerErrorTextResponse(w)
		return
	}

	jsonutil.SendHTTPResponse(w, http.StatusOK, resp)
}
