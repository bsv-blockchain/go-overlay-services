package commands_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/4chain-ag/go-overlay-services/pkg/core/gasp/core"
	"github.com/4chain-ag/go-overlay-services/pkg/server/internal/app/commands"
	"github.com/4chain-ag/go-overlay-services/pkg/server/internal/app/jsonutil"
	"github.com/bsv-blockchain/go-sdk/transaction"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubEngine struct{}

func (s *stubEngine) ProvideForeignGASPNode(ctx context.Context, graphID, outpoint *transaction.Outpoint, topic string) (*core.GASPNode, error) {
	return &core.GASPNode{}, nil
}

func TestRequestForeignGASPNodeHandler_ValidInput_ReturnsGASPNode(t *testing.T) {
	// Given:
	handler, err := commands.NewRequestForeignGASPNodeHandler(&stubEngine{})
	require.NoError(t, err)
	ts := httptest.NewServer(http.HandlerFunc(handler.Handle))
	defer ts.Close()

	payload := commands.RequestForeignGASPNodeHandlerPayload{
		GraphID:     "0000000000000000000000000000000000000000000000000000000000000000.1",
		TxID:        "0000000000000000000000000000000000000000000000000000000000000000",
		OutputIndex: 1,
	}
	req, err := http.NewRequest("POST", ts.URL, NewRequestPayload(t, payload))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-BSV-Topic", "test-topic")
	// When:
	resp, err := http.DefaultClient.Do(req)

	// Then:
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var actual core.GASPNode
	expected := core.GASPNode{}
	require.NoError(t, jsonutil.DecodeResponseBody(resp, &actual))
	assert.EqualValues(t, expected, actual)
}

func TestRequestForeignGASPNodeHandler_InvalidJSON_Returns400(t *testing.T) {
	// Given:
	handler, err := commands.NewRequestForeignGASPNodeHandler(&stubEngine{})
	require.NoError(t, err)
	ts := httptest.NewServer(http.HandlerFunc(handler.Handle))
	defer ts.Close()

	// When:
	resp, err := http.Post(ts.URL, "application/json", bytes.NewBufferString(`INVALID_JSON`))

	// Then:
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestRequestForeignGASPNodeHandler_MissingFields_StillReturnsOK(t *testing.T) {
	// Given:
	handler, err := commands.NewRequestForeignGASPNodeHandler(&stubEngine{})
	require.NoError(t, err)
	ts := httptest.NewServer(http.HandlerFunc(handler.Handle))
	defer ts.Close()

	// When:
	resp, err := http.Post(ts.URL, "application/json", bytes.NewBufferString(`{}`))

	// Then:
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestRequestForeignGASPNodeHandler_InvalidHTTPMethod_Returns405(t *testing.T) {
	// Given:
	handler, err := commands.NewRequestForeignGASPNodeHandler(&stubEngine{})
	require.NoError(t, err)
	ts := httptest.NewServer(http.HandlerFunc(handler.Handle))
	defer ts.Close()

	// When:
	req, _ := http.NewRequest(http.MethodGet, ts.URL, nil)
	resp, err := http.DefaultClient.Do(req)

	// Then:
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

// NewRequestPayload creates a new request payload from the given value.
func NewRequestPayload(t *testing.T, v any) *bytes.Buffer {
	t.Helper()
	bb, err := json.Marshal(v)
	require.NoError(t, err)
	return bytes.NewBuffer(bb)
}
