package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app/jsonutil"
	"github.com/bsv-blockchain/go-sdk/overlay/lookup"
)

var (
	// ErrMissingServiceField is returned when the request body is missing the required "service" field.
	ErrMissingServiceField = errors.New("missing required service field in the request body")

	// ErrInvalidRequestBody is returned when the request body cannot be parsed.
	ErrInvalidRequestBody = errors.New("invalid request body")

	// ErrMethodNotAllowed is returned when an unsupported HTTP method is used.
	ErrMethodNotAllowed = errors.New("method not allowed")
)

// LookupQuestionHandlerRequest represents the request body for handling a lookup question,
// containing the service name and the query data.
type LookupQuestionHandlerRequest struct {
	Service string          `json:"service"` // The name of the service.
	Query   json.RawMessage `json:"query"`   // The query data, stored as raw JSON.
}

// ToLookupQuestion converts the LookupQuestionHandlerRequest to a LookupQuestion.
// It returns a pointer to a LookupQuestion containing the same Service and Query values.
func (l LookupQuestionHandlerRequest) ToLookupQuestion() *lookup.LookupQuestion {
	return &lookup.LookupQuestion{
		Service: l.Service,
		Query:   l.Query,
	}
}

// LookupQuestionHandlerResponse defines the response body content that
// will be sent in JSON format after successfully processing the handler logic.
type LookupQuestionHandlerResponse struct {
	*lookup.LookupAnswer `json:"answer"`
}

// LookupQuestionProvider defines the contract that must be fulfilled
// to process lookup questions in the overlay engine.
type LookupQuestionProvider interface {
	Lookup(ctx context.Context, question *lookup.LookupQuestion) (*lookup.LookupAnswer, error)
}

// LookupQuestionHandler orchestrates the processing flow of a lookup question,
// including the request body validation, converting the request body
// into an overlay-engine-compatible format, and applying any other necessary
// logic before invoking the engine.
type LookupQuestionHandler struct {
	provider LookupQuestionProvider
}

func (h *LookupQuestionHandler) createLookupQuestion(r *http.Request) (*lookup.LookupQuestion, error) {
	var question LookupQuestionHandlerRequest
	if err := jsonutil.DecodeRequestBody(r, &question); err != nil {
		return nil, ErrInvalidRequestBody
	}

	if question.Service == "" {
		return nil, ErrMissingServiceField
	}

	return question.ToLookupQuestion(), nil
}

// Handle orchestrates the processing flow of a lookup request. It validates the
// request body, passes it to the engine's lookup method, and returns the result.
func (h *LookupQuestionHandler) Handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, ErrMethodNotAllowed.Error(), http.StatusMethodNotAllowed)
		return
	}

	question, err := h.createLookupQuestion(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	result, err := h.provider.Lookup(r.Context(), question)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	jsonutil.SendHTTPResponse(w, http.StatusOK, LookupQuestionHandlerResponse{LookupAnswer: result})
}

// NewLookupQuestionHandler returns an instance of a LookupHandler, utilizing
// an implementation of LookupQuestionProvider. If the provided argument is nil, it returns an error.
func NewLookupQuestionHandler(provider LookupQuestionProvider) (*LookupQuestionHandler, error) {
	if provider == nil {
		return nil, fmt.Errorf("lookup question provider is nil")
	}
	return &LookupQuestionHandler{
		provider: provider,
	}, nil
}
