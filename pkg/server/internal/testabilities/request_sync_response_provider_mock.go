package testabilities

import (
	"context"
	"testing"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/gasp"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/ports/openapi"
	"github.com/stretchr/testify/require"
)

const DefaultTopic = "test-topic"

const (
	DefaultVersion = 1
	DefaultSince   = float64(100000)
)

// RequestSyncResponseProviderMockExpectations defines mock expectations.
type RequestSyncResponseProviderMockExpectations struct {
	Error                          error
	Response                       *gasp.InitialResponse
	ProvideForeignSyncResponseCall bool
	InitialRequest                 *gasp.InitialRequest
	Topic                          string
}

// RequestSyncResponseProviderMock is a test double that implements the
// behavior of a RequestSyncResponseProvider. It records call data and
// validates expectations defined via RequestSyncResponseProviderMockExpectations.
type RequestSyncResponseProviderMock struct {
	t              *testing.T // The testing context
	expectations   RequestSyncResponseProviderMockExpectations
	called         bool                 // Tracks whether ProvideForeignSyncResponse was called
	topic          string               // Stores the topic passed to ProvideForeignSyncResponse
	initialRequest *gasp.InitialRequest // Stores the request passed to ProvideForeignSyncResponse
}

// NewDefaultGASPInitialResponseTestHelper creates a default GASPInitialResponse instance
// for use in test scenarios.
//
// It includes a sample UTXO with a dummy transaction hash and a fixed "Since" value.
func NewDefaultGASPInitialResponseTestHelper(t *testing.T) *gasp.InitialResponse {
	t.Helper()

	return &gasp.InitialResponse{
		UTXOList: []*gasp.Output{
			{
				Txid:        *DummyTxHash(t, "03895fb984362a4196bc9931629318fcbb2aeba7c6293638119ea653fa31d119"),
				OutputIndex: 0,
				Score:       0,
			},
		},
		Since: 1000000,
	}
}

// ProvideForeignSyncResponse simulates the behavior of a real provider.
// It captures input values and returns either the expected mock response or error.
//
// Implements the same signature as the real method for interchangeability in tests.
func (m *RequestSyncResponseProviderMock) ProvideForeignSyncResponse(ctx context.Context, initialRequest *gasp.InitialRequest, topic string) (*gasp.InitialResponse, error) {
	m.t.Helper()
	m.called = true
	m.topic = topic
	m.initialRequest = initialRequest

	if m.expectations.Error != nil {
		return nil, m.expectations.Error
	}

	return m.expectations.Response, nil
}

// AssertCalled verifies that ProvideForeignSyncResponse was called as expected.
// It compares the actual call data (topic and request) with the expected values
// and fails the test if discrepancies are found.
func (m *RequestSyncResponseProviderMock) AssertCalled() {
	m.t.Helper()
	require.Equal(m.t, m.expectations.ProvideForeignSyncResponseCall, m.called, "Discrepancy between expected and actual ProvideForeignSyncResponseCall")
	require.Equal(m.t, m.expectations.InitialRequest, m.initialRequest, "Discrepancy between expected and actual InitialRequest")
	require.Equal(m.t, m.expectations.Topic, m.topic, "Discrepancy between expected and actual Topic")
}

// NewDefaultRequestSyncResponseBody returns a default RequestSyncResponseBody
// with predefined Version and Since values for use in OpenAPI tests.
func NewDefaultRequestSyncResponseBody() openapi.RequestSyncResponseBody {
	return openapi.RequestSyncResponseBody{
		Version: DefaultVersion,
		Since:   DefaultSince,
	}
}

// NewRequestSyncResponseProviderMock constructs a new RequestSyncResponseProviderMock
// with predefined expectations.
func NewRequestSyncResponseProviderMock(t *testing.T, expectations RequestSyncResponseProviderMockExpectations) *RequestSyncResponseProviderMock {
	return &RequestSyncResponseProviderMock{
		t:            t,
		expectations: expectations,
	}
}
