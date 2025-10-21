package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/gasp"
	"github.com/bsv-blockchain/go-sdk/transaction"
	"github.com/bsv-blockchain/go-sdk/util"
)

// ErrNotImplemented is returned when a method is not implemented for the OverlayGASPRemote.
var ErrNotImplemented = errors.New("not-implemented")

// OverlayGASPRemote provides a remote GASP implementation that communicates with overlay endpoints.
type OverlayGASPRemote struct {
	EndpointURL string
	Topic       string
	HTTPClient  util.HTTPClient
}

// GetInitialResponse sends a GASP initial request to the remote overlay and returns the response.
func (r *OverlayGASPRemote) GetInitialResponse(ctx context.Context, request *gasp.InitialRequest) (*gasp.InitialResponse, error) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(request); err != nil {
		slog.Error("failed to encode GASP initial request", "endpoint", r.EndpointURL, "topic", r.Topic, "error", err)
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", r.EndpointURL+"/requestSyncResponse", io.NopCloser(&buf))
	if err != nil {
		slog.Error("failed to create HTTP request for GASP initial response", "endpoint", r.EndpointURL, "topic", r.Topic, "error", err)
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-BSV-Topic", r.Topic)
	resp, err := r.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, &util.HTTPError{
			StatusCode: resp.StatusCode,
			Err:        err,
		}
	}
	result := &gasp.InitialResponse{}
	if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
		return nil, err
	}
	return result, nil
}

// RequestNode requests a specific node from the remote overlay.
func (r *OverlayGASPRemote) RequestNode(ctx context.Context, graphID, outpoint *transaction.Outpoint, metadata bool) (*gasp.Node, error) {
	j, err := json.Marshal(&gasp.NodeRequest{
		GraphID:     graphID,
		Txid:        &outpoint.Txid,
		OutputIndex: outpoint.Index,
		Metadata:    metadata,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", r.EndpointURL+"/requestForeignGASPNode", bytes.NewReader(j))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-BSV-Topic", r.Topic)
	resp, err := r.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, &util.HTTPError{
			StatusCode: resp.StatusCode,
			Err:        err,
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
