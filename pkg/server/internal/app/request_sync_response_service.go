package app

import (
	"context"

	"github.com/4chain-ag/go-overlay-services/pkg/core/gasp/core"
)

// OutpointDTO represents a single unspent transaction output (UTXO) reference,
// including the transaction ID and its output index within the transaction.
type OutpointDTO struct {
	TxID        string
	OutputIndex uint32
}

// RequestSyncResponseDTO is a transport-friendly structure that encapsulates
// the response to a sync request, including a list of UTXO outpoints and the
// latest processed sync height (Since).
type RequestSyncResponseDTO struct {
	UTXOList []OutpointDTO
	Since    uint32
}

// Topic represents a named communication or synchronization channel identifier.
type Topic string

// NewTopic constructs a new Topic from a raw string value.
func NewTopic(s string) Topic { return Topic(s) }

// IsEmpty returns true if the Topic is an empty string.
func (t Topic) IsEmpty() bool { return len(t) == 0 }

// String returns the string representation of the Topic.
func (t Topic) String() string { return string(t) }

// Version represents a version identifier in integer form.
type Version int

// NewVersion constructs a new Version from an integer value.
func NewVersion(v int) Version { return Version(v) }

// IsGreaterThanZero returns true if the Version is strictly greater than zero.
func (v Version) IsGreaterThanZero() bool { return v > 0 }

// Int returns the raw integer value of the Version.
func (v Version) Int() int { return int(v) }

// Since represents a sync position or offset marker, typically used for incremental updates.
type Since uint32

// NewSince constructs a new Since value from an unsigned 32-bit integer.
func NewSince(v uint32) Since { return Since(v) }

// Unit32 returns the raw uint32 value of the Since marker.
func (s Since) Unit32() uint32 { return uint32(s) }

// RequestSyncResponseProvider defines the interface for components that can
// fulfill requests for foreign sync responses. It abstracts the underlying
// sync logic and data source.
type RequestSyncResponseProvider interface {
	ProvideForeignSyncResponse(ctx context.Context, initialRequest *core.GASPInitialRequest, topic string) (*core.GASPInitialResponse, error)
}

// RequestSyncResponseService coordinates the sync response operation within the
// application layer. It validates inputs, delegates the core logic to a provider,
// and adapts the response into a client-facing DTO.
type RequestSyncResponseService struct {
	provider RequestSyncResponseProvider
}

// RequestSyncResponse performs a foreign sync request for a given topic.
// It validates the input parameters, constructs the initial request payload,
// and delegates the operation to the provider. The response is transformed
// into a DTO suitable for external use.
func (s *RequestSyncResponseService) RequestSyncResponse(ctx context.Context, topic Topic, version Version, since Since) (*RequestSyncResponseDTO, error) {
	if topic.IsEmpty() {
		return nil, NewIncorrectInputWithFieldError("topic")
	}
	if !version.IsGreaterThanZero() {
		return nil, NewIncorrectInputWithFieldError("version")
	}

	response, err := s.provider.ProvideForeignSyncResponse(ctx, &core.GASPInitialRequest{Version: version.Int(), Since: since.Unit32()}, topic.String())
	if err != nil {
		return nil, NewRequestSyncResponseProviderError(err)
	}
	return NewRequestSyncResponseDTO(response), nil
}

// NewRequestSyncResponseDTO transforms the core GASP sync response into a
// client-friendly DTO, preserving only the required UTXO and sync progress data.
func NewRequestSyncResponseDTO(response *core.GASPInitialResponse) *RequestSyncResponseDTO {
	outpoints := make([]OutpointDTO, 0, len(response.UTXOList))
	for _, utxo := range response.UTXOList {
		outpoints = append(outpoints, OutpointDTO{
			TxID:        utxo.Txid.String(),
			OutputIndex: utxo.OutputIndex,
		})
	}

	return &RequestSyncResponseDTO{
		UTXOList: outpoints,
		Since:    response.Since,
	}
}

// NewRequestSyncResponseService constructs a new RequestSyncResponseService with the
// provided provider. It panics if the provider is nil to enforce safe initialization.
func NewRequestSyncResponseService(provider RequestSyncResponseProvider) *RequestSyncResponseService {
	if provider == nil {
		panic("request sync response provider is nil")
	}
	return &RequestSyncResponseService{provider: provider}
}

// NewRequestSyncResponseProviderError wraps a low-level provider error that occurred
// during a sync response request. The resulting error is classified as a provider failure
// and returns a generic slug message suitable for client-facing usage.
func NewRequestSyncResponseProviderError(err error) Error {
	return Error{
		errorType: ErrorTypeProviderFailure,
		err:       err.Error(),
		slug:      "Unable to process sync response request due to an error in the overlay engine.",
	}
}
