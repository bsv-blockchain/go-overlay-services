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

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/gasp"
	"github.com/bsv-blockchain/go-sdk/transaction"
	"github.com/bsv-blockchain/go-sdk/util"
)

type inflightNodeRequest struct {
	wg     *sync.WaitGroup
	result *gasp.Node
	err    error
}

type OverlayGASPRemote struct {
	endpointUrl    string
	topic          string
	httpClient     util.HTTPClient
	inflightMap    sync.Map      // Map to track in-flight node requests
	networkLimiter chan struct{} // Controls max concurrent network requests
}

func NewOverlayGASPRemote(endpointUrl, topic string, httpClient util.HTTPClient, maxConcurrency int) *OverlayGASPRemote {
	if maxConcurrency <= 0 {
		maxConcurrency = 8 // Default network concurrency
	}

	return &OverlayGASPRemote{
		endpointUrl:    endpointUrl,
		topic:          topic,
		httpClient:     httpClient,
		networkLimiter: make(chan struct{}, maxConcurrency),
	}
}

func (r *OverlayGASPRemote) GetInitialResponse(ctx context.Context, request *gasp.InitialRequest) (*gasp.InitialResponse, error) {
	requestJSON, err := json.Marshal(request)
	if err != nil {
		slog.Error("failed to encode GASP initial request", "endpoint", r.endpointUrl, "topic", r.topic, "error", err)
		return nil, err
	}

	if req, err := http.NewRequest("POST", r.endpointUrl+"/requestSyncResponse", bytes.NewReader(requestJSON)); err != nil {
		slog.Error("failed to create HTTP request for GASP initial response", "endpoint", r.endpointUrl, "topic", r.topic, "error", err)
		return nil, err
	} else {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-BSV-Topic", r.topic)
		if resp, err := r.httpClient.Do(req); err != nil {
			return nil, err
		} else {
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
					Err:        fmt.Errorf("server error: %s", string(body)),
				}
			}
			result := &gasp.InitialResponse{}
			if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
				return nil, err
			}
			return result, nil
		}
	}
}

func (r *OverlayGASPRemote) RequestNode(ctx context.Context, graphID *transaction.Outpoint, outpoint *transaction.Outpoint, metadata bool) (*gasp.Node, error) {
	outpointStr := outpoint.String()
	var wg sync.WaitGroup
	wg.Add(1)
	defer wg.Done()

	// Check if there's already an in-flight request for this outpoint
	if inflight, loaded := r.inflightMap.LoadOrStore(outpointStr, &inflightNodeRequest{wg: &wg}); loaded {
		req := inflight.(*inflightNodeRequest)
		req.wg.Wait()
		return req.result, req.err
	} else {
		req := inflight.(*inflightNodeRequest)
		req.result, req.err = r.doNodeRequest(ctx, graphID, outpoint, metadata)

		// Clean up inflight map
		r.inflightMap.Delete(outpointStr)
		return req.result, req.err
	}
}

func (r *OverlayGASPRemote) doNodeRequest(ctx context.Context, graphID *transaction.Outpoint, outpoint *transaction.Outpoint, metadata bool) (*gasp.Node, error) {
	// Acquire network limiter
	select {
	case r.networkLimiter <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-r.networkLimiter }()
	if j, err := json.Marshal(&gasp.NodeRequest{
		GraphID:     graphID,
		Txid:        &outpoint.Txid,
		OutputIndex: outpoint.Index,
		Metadata:    metadata,
	}); err != nil {
		return nil, err
	} else if req, err := http.NewRequest("POST", r.endpointUrl+"/requestForeignGASPNode", bytes.NewReader(j)); err != nil {
		return nil, err
	} else {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-BSV-Topic", r.topic)
		if resp, err := r.httpClient.Do(req); err != nil {
			return nil, err
		} else {
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
				var graphId string
				if graphID != nil {
					graphId = graphID.String()
				}
				slog.Error("RequestNode failed",
					"status", resp.StatusCode,
					"body", string(body),
					"graphID", graphId,
					"outpoint", outpoint.String(),
					"metadata", metadata,
					"endpoint", r.endpointUrl,
					"topic", r.topic)
				return nil, &util.HTTPError{
					StatusCode: resp.StatusCode,
					Err:        fmt.Errorf("server error: %s", string(body)),
				}
			}
			result := &gasp.Node{}
			if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
				return nil, err
			}
			return result, nil
		}
	}
}

func (r *OverlayGASPRemote) GetInitialReply(ctx context.Context, response *gasp.InitialResponse) (*gasp.InitialReply, error) {
	return nil, errors.New("not-implemented")
}

func (r *OverlayGASPRemote) SubmitNode(ctx context.Context, node *gasp.Node) (*gasp.NodeResponse, error) {
	return nil, errors.New("not-implemented")
}
