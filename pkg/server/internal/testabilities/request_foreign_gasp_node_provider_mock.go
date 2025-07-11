package testabilities

import (
	"context"
	"testing"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/gasp"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app"
	"github.com/bsv-blockchain/go-sdk/transaction"
	"github.com/stretchr/testify/require"
)

// Default test values for RequestForeignGASPNode operations.
const (
	DefaultValidGraphID     = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef.0"
	DefaultValidTxID        = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	DefaultValidOutputIndex = uint32(0)
	DefaultValidTopic       = "test-topic"
	DefaultInvalidTxID      = "invalid-txid"
	DefaultInvalidGraphID   = "invalid-graphid"
	DefaultEmptyTopic       = ""
)

// ForeignGASPNodeDefaultDTO provides a default DTO for RequestForeignGASPNode tests.
var ForeignGASPNodeDefaultDTO = app.RequestForeignGASPNodeDTO{
	GraphID:     DefaultValidGraphID,
	TxID:        DefaultValidTxID,
	OutputIndex: DefaultValidOutputIndex,
	Topic:       DefaultValidTopic,
}

// Default expectations for successful RequestForeignGASPNode operations
var DefaultRequestForeignGASPNodeProviderMockExpectations = RequestForeignGASPNodeProviderMockExpectations{
	ProvideForeignGASPNodeCall: true,
	Error:                      nil,
	Node:                       &gasp.Node{},
}

// RequestForeignGASPNodeProviderMockExpectations defines the expected behavior of the mock provider.
type RequestForeignGASPNodeProviderMockExpectations struct {
	Error                      error
	Node                       *gasp.Node
	ProvideForeignGASPNodeCall bool
}

// RequestForeignGASPNodeProviderMock is a mock implementation for testing.
type RequestForeignGASPNodeProviderMock struct {
	t            *testing.T
	expectations RequestForeignGASPNodeProviderMockExpectations
	called       bool
}

// ProvideForeignGASPNode mocks the ProvideForeignGASPNode method.
func (m *RequestForeignGASPNodeProviderMock) ProvideForeignGASPNode(ctx context.Context, graphID, outpoint *transaction.Outpoint, topic string) (*gasp.Node, error) {
	m.t.Helper()
	m.called = true

	if m.expectations.Error != nil {
		return nil, m.expectations.Error
	}

	return m.expectations.Node, nil
}

// AssertCalled verifies the method was called as expected.
func (m *RequestForeignGASPNodeProviderMock) AssertCalled() {
	m.t.Helper()
	require.Equal(m.t, m.expectations.ProvideForeignGASPNodeCall, m.called, "Discrepancy between expected and actual ProvideForeignGASPNode call")
}

// NewRequestForeignGASPNodeProviderMock creates a new mock provider.
func NewRequestForeignGASPNodeProviderMock(t *testing.T, expectations RequestForeignGASPNodeProviderMockExpectations) *RequestForeignGASPNodeProviderMock {
	return &RequestForeignGASPNodeProviderMock{
		t:            t,
		expectations: expectations,
	}
}
