package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/4chain-ag/go-overlay-services/pkg/core/gasp/core"
	"github.com/bsv-blockchain/go-sdk/transaction"
	"github.com/bsv-blockchain/go-sdk/util"
)

type OverlayGASPRemote struct {
	EndpointUrl string
	Topic       string
	HttpClient  util.HTTPClient
}

func (r *OverlayGASPRemote) GetInitialResponse(ctx context.Context, request *core.GASPInitialRequest) (*core.GASPInitialResponse, error) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(request); err != nil {
		slog.Error("failed to encode GASP initial request", "endpoint", r.EndpointUrl, "topic", r.Topic, "error", err)
		return nil, err
	} else if req, err := http.NewRequest("POST", r.EndpointUrl+"/requestSyncResponse", io.NopCloser(&buf)); err != nil {
		slog.Error("failed to create HTTP request for GASP initial response", "endpoint", r.EndpointUrl, "topic", r.Topic, "error", err)
		return nil, err
	} else {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-BSV-Topic", r.Topic)
		if resp, err := r.HttpClient.Do(req); err != nil {
			return nil, err
		} else {
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusOK {
				return nil, &util.HTTPError{
					StatusCode: resp.StatusCode,
					Err:        err,
				}
			}
			result := &core.GASPInitialResponse{}
			if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
				return nil, err
			}
			return result, nil
		}
	}
}

func (r *OverlayGASPRemote) RequestNode(ctx context.Context, graphID *transaction.Outpoint, outpoint *transaction.Outpoint, metadata bool) (*core.GASPNode, error) {
	if j, err := json.Marshal(&core.GASPNodeRequest{
		GraphID:     graphID,
		Txid:        &outpoint.Txid,
		OutputIndex: outpoint.Index,
		Metadata:    metadata,
	}); err != nil {
		return nil, err
	} else if req, err := http.NewRequest("POST", r.EndpointUrl+"/requestForeignGASPNode", bytes.NewReader(j)); err != nil {
		return nil, err
	} else {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-BSV-Topic", r.Topic)
		if resp, err := r.HttpClient.Do(req); err != nil {
			return nil, err
		} else {
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusOK {
				return nil, &util.HTTPError{
					StatusCode: resp.StatusCode,
					Err:        err,
				}
			}
			result := &core.GASPNode{}
			if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
				return nil, err
			}
			return result, nil
		}
	}
}

func (r *OverlayGASPRemote) GetInitialReply(ctx context.Context, response *core.GASPInitialResponse) (*core.GASPInitialReply, error) {
	return nil, errors.New("not-implemented")
}

func (r *OverlayGASPRemote) SubmitNode(ctx context.Context, node *core.GASPNode) (*core.GASPNodeResponse, error) {
	return nil, errors.New("not-implemented")
}
