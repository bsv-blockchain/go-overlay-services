package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"

	"github.com/bsv-blockchain/go-sdk/transaction"
	"github.com/bsv-blockchain/go-sdk/util"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/gasp"
)

// ErrNotImplemented is returned when a method is not implemented for the OverlayGASPRemote.
var ErrNotImplemented = errors.New("not-implemented")

type inflightNodeRequest struct {
	wg     *sync.WaitGroup
	result *gasp.Node
	err    error
}

// OverlayGASPRemote provides a remote GASP implementation that communicates with overlay endpoints.
type OverlayGASPRemote struct {
	endpointURL    string
	topic          string
	httpClient     util.HTTPClient
	inflightMap    sync.Map      // Map to track in-flight node requests
	networkLimiter chan struct{} // Controls max concurrent network requests
}

// NewOverlayGASPRemote creates a new OverlayGASPRemote with the given endpoint, topic, and HTTP client.
func NewOverlayGASPRemote(endpointURL, topic string, httpClient util.HTTPClient, maxConcurrency int) *OverlayGASPRemote {
	if maxConcurrency <= 0 {
		maxConcurrency = 8 // Default network concurrency
	}

	return &OverlayGASPRemote{
		endpointURL:    endpointURL,
		topic:          topic,
		httpClient:     httpClient,
		networkLimiter: make(chan struct{}, maxConcurrency),
	}
}

// GetInitialResponse sends a GASP initial request to the remote overlay and returns the response.
func (r *OverlayGASPRemote) GetInitialResponse(ctx context.Context, request *gasp.InitialRequest) (*gasp.InitialResponse, error) {
	requestJSON, err := json.Marshal(request)
	if err != nil {
		slog.Error("failed to encode GASP initial request", "endpoint", r.endpointURL, "topic", r.topic, "error", err)
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", r.endpointURL+"/requestSyncResponse", bytes.NewReader(requestJSON))
	if err != nil {
		slog.Error("failed to create HTTP request for GASP initial response", "endpoint", r.endpointURL, "topic", r.topic, "error", err)
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-BSV-Topic", r.topic)

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		// Read error message from response body
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return nil, &util.HTTPError{
				StatusCode: resp.StatusCode,
				Err:        readErr,
			}
		}
		return nil, &util.HTTPError{
			StatusCode: resp.StatusCode,
			Err:        fmt.Errorf("server error: %s", string(body)), //nolint:err113 // dynamic HTTP response body
		}
	}

	result := &gasp.InitialResponse{}
	if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
		return nil, err
	}
	return result, nil
}

// RequestNode fetches a GASP node from the remote overlay endpoint.
func (r *OverlayGASPRemote) RequestNode(ctx context.Context, graphID, outpoint *transaction.Outpoint, metadata bool) (*gasp.Node, error) {
	// If graphID is nil, use outpoint (for root node requests)
	if graphID == nil {
		graphID = outpoint
	}

	var wg sync.WaitGroup
	wg.Add(1)
	defer wg.Done()

	// Check if there's already an in-flight request for this outpoint
	inflight, loaded := r.inflightMap.LoadOrStore(*outpoint, &inflightNodeRequest{wg: &wg})
	req := inflight.(*inflightNodeRequest)

	if loaded {
		req.wg.Wait()
		return req.result, req.err
	}

	req.result, req.err = r.doNodeRequest(ctx, graphID, outpoint, metadata)

	// Clean up inflight map
	r.inflightMap.Delete(*outpoint)
	return req.result, req.err
}

func (r *OverlayGASPRemote) doNodeRequest(ctx context.Context, graphID, outpoint *transaction.Outpoint, metadata bool) (*gasp.Node, error) {
	// Acquire network limiter
	select {
	case r.networkLimiter <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-r.networkLimiter }()

	j, err := json.Marshal(&gasp.NodeRequest{
		GraphID:     graphID,
		Txid:        &outpoint.Txid,
		OutputIndex: outpoint.Index,
		Metadata:    metadata,
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", r.endpointURL+"/requestForeignGASPNode", bytes.NewReader(j))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-BSV-Topic", r.topic)

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		// Read error message from response body
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return nil, &util.HTTPError{
				StatusCode: resp.StatusCode,
				Err:        readErr,
			}
		}
		// Log the full request and response details on failure
		var graphIDStr string
		if graphID != nil {
			graphIDStr = graphID.String()
		}
		slog.Error("RequestNode failed",
			"status", resp.StatusCode,
			"body", string(body),
			"graphID", graphIDStr,
			"outpoint", outpoint.String(),
			"metadata", metadata,
			"endpoint", r.endpointURL,
			"topic", r.topic)
		return nil, &util.HTTPError{
			StatusCode: resp.StatusCode,
			Err:        fmt.Errorf("server error: %s", string(body)), //nolint:err113 // dynamic HTTP response body
		}
	}

	result := &gasp.Node{}
	if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetInitialReply is not implemented for OverlayGASPRemote and returns ErrNotImplemented.
func (r *OverlayGASPRemote) GetInitialReply(_ context.Context, _ *gasp.InitialResponse) (*gasp.InitialReply, error) {
	return nil, ErrNotImplemented
}

// SubmitNode is not implemented for OverlayGASPRemote and returns ErrNotImplemented.
func (r *OverlayGASPRemote) SubmitNode(_ context.Context, _ *gasp.Node) (*gasp.NodeResponse, error) {
	return nil, ErrNotImplemented
}
