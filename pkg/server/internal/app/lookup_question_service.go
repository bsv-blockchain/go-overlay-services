package app

import (
	"context"
	"encoding/json"

	"github.com/bsv-blockchain/go-sdk/overlay/lookup"
)

// OutputListItemDTO represents an individual output item returned as part of a lookup answer.
// Each output includes the raw binary output ('BEEF') and its index in the overall output sequence.
type OutputListItemDTO struct {
	BEEF        []byte // Binary Encoded External Format (BEEF) of the output data.
	OutputIndex uint32 // Index indicating the position of this output in the result set.
}

// LookupAnswerDTO encapsulates the response of a successful lookup question evaluation.
// It contains a list of output items, a JSON-encoded result, and a string indicating the type of the answer.
type LookupAnswerDTO struct {
	Outputs []OutputListItemDTO // List of output items produced by the lookup operation.
	Result  string              // JSON-encoded string representing the result object.
	Type    string              // Describes the type/category of the answer (e.g., "exact", "partial").
}

// LookupQuestionProvider defines the interface for any provider capable of evaluating
// lookup questions. Implementations encapsulate the business logic to process questions
// and produce corresponding answers.
type LookupQuestionProvider interface {
	// Lookup evaluates the given question and returns a structured answer or an error.
	Lookup(ctx context.Context, question *lookup.LookupQuestion) (*lookup.LookupAnswer, error)
}

// LookupQuestionService provides a higher-level abstraction over a LookupQuestionProvider.
// It performs request validation, transforms query data into a provider-friendly format,
// invokes the provider, and transforms the result into a transport-friendly DTO.
type LookupQuestionService struct {
	provider LookupQuestionProvider
}

// LookupQuestion handles the end-to-end processing of a lookup question request.
// It validates inputs, delegates evaluation to the underlying provider,
// and returns a structured answer suitable for use in the presentation layer.
// Returns an error if the input is invalid, the evaluation fails, or the result cannot be processed.
func (s *LookupQuestionService) LookupQuestion(ctx context.Context, service string, query map[string]any) (*LookupAnswerDTO, error) {
	if len(service) == 0 {
		return nil, NewIncorrectInputWithFieldError("service")
	}
	if len(query) == 0 {
		return nil, NewIncorrectInputWithFieldError("query")
	}
	bb, err := json.Marshal(query)
	if err != nil {
		return nil, NewLookupQuestionParserError(err)
	}

	answer, err := s.provider.Lookup(ctx, &lookup.LookupQuestion{
		Service: service,
		Query:   json.RawMessage(bb),
	})
	if err != nil {
		return nil, NewLookupQuestionProviderError(err)
	}

	return NewLookupQuestionAnswerDTO(answer)
}

// NewLookupQuestionService constructs a LookupQuestionService with the given provider.
// Panics if the provider is nil, as service functionality depends on a valid provider.
func NewLookupQuestionService(provider LookupQuestionProvider) *LookupQuestionService {
	if provider == nil {
		panic("lookup question provider is nil")
	}
	return &LookupQuestionService{provider: provider}
}

// NewLookupQuestionAnswerDTO converts a core LookupAnswer model into a LookupAnswerDTO,
// a transport-layer structure suitable for API responses. It serializes the Result object
// to a JSON string and transforms output entries into DTO-compatible types.
// Returns an error if serialization fails.
func NewLookupQuestionAnswerDTO(answer *lookup.LookupAnswer) (*LookupAnswerDTO, error) {
	var outputs []OutputListItemDTO
	if len(answer.Outputs) > 0 {
		outputs = make([]OutputListItemDTO, len(answer.Outputs))
		for i, output := range answer.Outputs {
			outputs[i] = OutputListItemDTO{
				BEEF:        output.Beef,
				OutputIndex: output.OutputIndex,
			}
		}
	}

	var result string
	if answer.Result != nil {
		bb, err := json.Marshal(answer.Result)
		if err != nil {
			return nil, NewLookupQuestionParserError(err)
		}
		result = string(bb)
	}

	return &LookupAnswerDTO{
		Outputs: outputs,
		Result:  result,
		Type:    string(answer.Type),
	}, nil
}

// NewLookupQuestionParserError creates a structured error to be returned
// when JSON serialization of the lookup query fails. Provides a generic,
// user-friendly error message for external consumers.
func NewLookupQuestionParserError(err error) Error {
	return NewRawDataProcessingError(
		err.Error(),
		"Unable to process the request query params content due to an internal error. Please verify the content, try again later, or contact the support team.",
	)
}

// NewLookupQuestionProviderError wraps an internal error that occurred during provider evaluation.
// Produces a standardized user-facing error message while retaining the original error internally
// for logging or diagnostics.
func NewLookupQuestionProviderError(err error) Error {
	return NewProviderFailureError(
		err.Error(),
		"Unable to process lookup question due to an internal error. Please try again later or contact the support team.",
	)
}
