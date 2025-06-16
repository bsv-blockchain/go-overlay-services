package commands_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/4chain-ag/go-overlay-services/pkg/core/gasp/core"
	"github.com/4chain-ag/go-overlay-services/pkg/server/internal/app/commands"
	"github.com/4chain-ag/go-overlay-services/pkg/server/internal/app/commands/testutil"
	"github.com/bsv-blockchain/go-sdk/transaction"
	"github.com/stretchr/testify/require"
)

// Constants for tests
const exampleTopic = "example-topic"

// setSyncResponseRequestHeaders sets the headers required for GASP sync response requests in tests
func setSyncResponseRequestHeaders(req *http.Request, includeBSVTopic bool) {
	req.Header.Set(commands.ContentTypeHeader, commands.ContentTypeJSON)
	if includeBSVTopic {
		req.Header.Set(commands.XBSVTopicHeader, exampleTopic)
	}
}

// Mock provider that always succeeds.
type foreignSyncProviderAlwaysSuccess struct{}

func (foreignSyncProviderAlwaysSuccess) ProvideForeignSyncResponse(ctx context.Context, initialRequest *core.GASPInitialRequest, topic string) (*core.GASPInitialResponse, error) {
	return &core.GASPInitialResponse{
		UTXOList: []*transaction.Outpoint{},
		Since:    initialRequest.Since,
	}, nil
}

// Mock provider that always fails.
type foreignSyncProviderAlwaysFailure struct{}

func (foreignSyncProviderAlwaysFailure) ProvideForeignSyncResponse(ctx context.Context, initialRequest *core.GASPInitialRequest, topic string) (*core.GASPInitialResponse, error) {
	return nil, fmt.Errorf("simulated sync failure")
}

func TestRequestSyncResponseHandler_Success(t *testing.T) {
	// Given:
	handler, err := commands.NewRequestSyncResponseHandler(&foreignSyncProviderAlwaysSuccess{})
	require.NoError(t, err)
	ts := httptest.NewServer(http.HandlerFunc(handler.Handle))
	defer ts.Close()

	payload := core.GASPInitialRequest{
		Version: 1,
		Since:   1000,
	}

	// When:
	req, err := http.NewRequest(http.MethodPost, ts.URL, testutil.RequestBody(t, payload))
	require.NoError(t, err)
	setSyncResponseRequestHeaders(req, true)

	resp, err := ts.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Then:
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestRequestSyncResponseHandler_MissingTopic(t *testing.T) {
	// Given:
	handler, err := commands.NewRequestSyncResponseHandler(&foreignSyncProviderAlwaysSuccess{})
	require.NoError(t, err)
	ts := httptest.NewServer(http.HandlerFunc(handler.Handle))
	defer ts.Close()

	payload := core.GASPInitialRequest{
		Version: 1,
		Since:   1000,
	}

	// When:
	req, err := http.NewRequest(http.MethodPost, ts.URL, testutil.RequestBody(t, payload))
	require.NoError(t, err)
	setSyncResponseRequestHeaders(req, false)

	resp, err := ts.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Then:
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)

	actualErr := testutil.ParseToError(t, resp.Body)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	require.Equal(t, commands.ErrMissingXBSVTopicHeader, actualErr)
}

func TestRequestSyncResponseHandler_InvalidJSON(t *testing.T) {
	// Given:
	handler, err := commands.NewRequestSyncResponseHandler(&foreignSyncProviderAlwaysSuccess{})
	require.NoError(t, err)
	ts := httptest.NewServer(http.HandlerFunc(handler.Handle))
	defer ts.Close()

	// When:
	req, err := http.NewRequest(http.MethodPost, ts.URL, testutil.RequestBody(t, `{invalid-json}`))
	require.NoError(t, err)
	setSyncResponseRequestHeaders(req, true)

	resp, err := ts.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Then:
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)

	actualErr := testutil.ParseToError(t, resp.Body)
	require.Equal(t, commands.ErrSyncResponseInvalidRequestBody, actualErr)
}

func TestRequestSyncResponseHandler_MethodNotAllowed(t *testing.T) {
	// Given:
	handler, err := commands.NewRequestSyncResponseHandler(&foreignSyncProviderAlwaysSuccess{})
	require.NoError(t, err)
	ts := httptest.NewServer(http.HandlerFunc(handler.Handle))
	defer ts.Close()

	// When:
	req, err := http.NewRequest(http.MethodGet, ts.URL, nil)
	require.NoError(t, err)
	setSyncResponseRequestHeaders(req, true)

	resp, err := ts.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Then:
	require.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)

	actualErr := testutil.ParseToError(t, resp.Body)
	require.Equal(t, commands.ErrSyncResponseMethodNotAllowed, actualErr)
}

func TestRequestSyncResponseHandler_InternalServerError(t *testing.T) {
	// Given:
	handler, err := commands.NewRequestSyncResponseHandler(&foreignSyncProviderAlwaysFailure{})
	require.NoError(t, err)
	ts := httptest.NewServer(http.HandlerFunc(handler.Handle))
	defer ts.Close()

	payload := core.GASPInitialRequest{
		Version: 1,
		Since:   1000,
	}

	// When:
	req, err := http.NewRequest(http.MethodPost, ts.URL, testutil.RequestBody(t, payload))
	require.NoError(t, err)
	setSyncResponseRequestHeaders(req, true)

	resp, err := ts.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Then:
	require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}
