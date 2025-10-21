// Package gasp implements the Graph Aware Sync Protocol for synchronizing transaction graphs between nodes.
package gasp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"

	"github.com/bsv-blockchain/go-sdk/chainhash"
	"github.com/bsv-blockchain/go-sdk/transaction"
)

// MaxConcurrency defines the maximum number of concurrent GASP operations allowed.
const MaxConcurrency = 16

// ErrNodeNilInProcessOutgoingNode is returned when a nil node is passed to processOutgoingNode.
var ErrNodeNilInProcessOutgoingNode = errors.New("node is nil in processOutgoingNode")

// NodeRequest represents a request for a specific node in the GASP graph.
type NodeRequest struct {
	GraphID     *transaction.Outpoint `json:"graphID"`
	Txid        *chainhash.Hash       `json:"txid"`
	OutputIndex uint32                `json:"outputIndex"`
	Metadata    bool                  `json:"metadata"`
}

// Params contains the parameters for creating a new GASP instance.
type Params struct {
	Storage         Storage
	Remote          Remote
	LastInteraction float64
	Version         *int
	LogPrefix       *string
	Unidirectional  bool
	LogLevel        slog.Level
	Concurrency     int
}

// GASP implements the Graph Aware Sync Protocol for synchronizing transaction graphs.
type GASP struct {
	Version         int
	Remote          Remote
	Storage         Storage
	LastInteraction float64
	LogPrefix       string
	Unidirectional  bool
	LogLevel        slog.Level
	limiter         chan struct{}
}

// NewGASP creates a new GASP instance with the provided parameters.
func NewGASP(params Params) *GASP {
	gasp := &GASP{
		Storage:         params.Storage,
		Remote:          params.Remote,
		LastInteraction: params.LastInteraction,
		Unidirectional:  params.Unidirectional,
		// Sequential:      params.Sequential,
	}
	if params.Concurrency > 1 {
		gasp.limiter = make(chan struct{}, params.Concurrency)
	} else {
		gasp.limiter = make(chan struct{}, 1)
	}
	if params.Version != nil {
		gasp.Version = *params.Version
	} else {
		gasp.Version = 1
	}
	if params.LogPrefix != nil {
		gasp.LogPrefix = *params.LogPrefix
	} else {
		gasp.LogPrefix = "[GASP] "
	}
	slog.SetLogLoggerLevel(slog.LevelInfo)
	return gasp
}

// Sync performs a GASP synchronization with the specified host.
func (g *GASP) Sync(ctx context.Context, _ string, limit uint32) error {
	slog.Info(fmt.Sprintf("%sStarting sync process. Last interaction timestamp: %f", g.LogPrefix, g.LastInteraction))

	localUTXOs, err := g.Storage.FindKnownUTXOs(ctx, 0, 0)
	if err != nil {
		return err
	}

	// Find which UTXOs we already have
	knownOutpoints := make(map[string]struct{})
	for _, utxo := range localUTXOs {
		outpoint := fmt.Sprintf("%s.%d", utxo.Txid, utxo.OutputIndex)
		knownOutpoints[outpoint] = struct{}{}
	}
	sharedOutpoints := make(map[string]struct{})

	var initialResponse *InitialResponse
	for {
		initialRequest := &InitialRequest{
			Version: g.Version,
			Since:   g.LastInteraction,
			Limit:   limit,
		}
		initialResponse, err = g.Remote.GetInitialResponse(ctx, initialRequest)
		if err != nil {
			return err
		}

		var ingestQueue []*Output
		for _, utxo := range initialResponse.UTXOList {
			if utxo.Score > g.LastInteraction {
				g.LastInteraction = utxo.Score
			}
			outpoint := utxo.OutpointString()
			if _, exists := knownOutpoints[outpoint]; exists {
				sharedOutpoints[outpoint] = struct{}{}
				delete(knownOutpoints, outpoint)
			} else if _, shared := sharedOutpoints[outpoint]; !shared {
				ingestQueue = append(ingestQueue, utxo)
			}
		}

		var wg sync.WaitGroup
		for _, utxo := range ingestQueue {
			wg.Add(1)
			g.limiter <- struct{}{}
			go func(utxo *Output) {
				defer func() {
					<-g.limiter
					wg.Done()
				}()
				outpoint := utxo.Outpoint()
				resolvedNode, err := g.Remote.RequestNode(ctx, outpoint, outpoint, true)
				if err != nil {
					slog.Warn(fmt.Sprintf("%sError with incoming UTXO %s: %v", g.LogPrefix, outpoint, err))
					return
				}
				slog.Debug(fmt.Sprintf("%sReceived unspent graph node from remote: %v", g.LogPrefix, resolvedNode))
				if err = g.processIncomingNode(ctx, resolvedNode, nil, &sync.Map{}); err != nil {
					slog.Warn(fmt.Sprintf("%sError processing incoming node %s: %v", g.LogPrefix, outpoint, err))
					return
				}
				if err = g.CompleteGraph(ctx, resolvedNode.GraphID); err != nil {
					slog.Warn(fmt.Sprintf("%sError completing graph for %s: %v", g.LogPrefix, outpoint, err))
					return
				}
				sharedOutpoints[outpoint.String()] = struct{}{}
			}(utxo)
		}
		wg.Wait()

		// Check if we have more pages to fetch
		// If we got fewer items than we requested (or no limit was set), we've reached the end
		if limit == 0 || len(initialResponse.UTXOList) < int(limit) {
			break
		}
	}
	// 2. Only do the "reply" half if unidirectional is disabled
	if !g.Unidirectional && initialResponse != nil {
		// Filter localUTXOs for those after initialResponse.since and not in sharedOutpoints
		var replyUTXOs []*Output
		for _, utxo := range localUTXOs {
			outpoint := fmt.Sprintf("%s.%d", utxo.Txid, utxo.OutputIndex)
			if utxo.Score >= initialResponse.Since {
				if _, shared := sharedOutpoints[outpoint]; !shared {
					replyUTXOs = append(replyUTXOs, utxo)
				}
			}
		}

		if len(replyUTXOs) > 0 {
			var wg sync.WaitGroup
			for _, utxo := range replyUTXOs {
				wg.Add(1)
				g.limiter <- struct{}{}
				go func(utxo *Output) {
					defer func() {
						<-g.limiter
						wg.Done()
					}()
					slog.Info(fmt.Sprintf("%sHydrating GASP node for UTXO: %s.%d", g.LogPrefix, utxo.Txid, utxo.OutputIndex))
					outpoint := utxo.Outpoint()
					outgoingNode, err := g.Storage.HydrateGASPNode(ctx, outpoint, outpoint, true)
					if err != nil {
						slog.Warn(fmt.Sprintf("%sError hydrating outgoing UTXO %s.%d: %v", g.LogPrefix, utxo.Txid, utxo.OutputIndex, err))
						return
					}
					if outgoingNode == nil {
						slog.Debug(fmt.Sprintf("%sSkipping outgoing UTXO %s.%d: not found in storage", g.LogPrefix, utxo.Txid, utxo.OutputIndex))
						return
					}
					slog.Debug(fmt.Sprintf("%sSending unspent graph node for remote: %v", g.LogPrefix, outgoingNode))
					if err = g.processOutgoingNode(ctx, outgoingNode, &sync.Map{}); err != nil {
						slog.Warn(fmt.Sprintf("%sError processing outgoing node %s.%d: %v", g.LogPrefix, utxo.Txid, utxo.OutputIndex, err))
					}
				}(utxo)
			}
			wg.Wait()
		}
	}

	slog.Info(fmt.Sprintf("%sSync completed!", g.LogPrefix))
	return nil
}

// GetInitialResponse processes an initial GASP request and returns known UTXOs.
func (g *GASP) GetInitialResponse(ctx context.Context, request *InitialRequest) (resp *InitialResponse, err error) {
	slog.Info(fmt.Sprintf("%sReceived initial request: %v", g.LogPrefix, request))
	if request.Version != g.Version {
		slog.Error(fmt.Sprintf("%sGASP version mismatch", g.LogPrefix))
		return nil, NewVersionMismatchError(
			g.Version,
			request.Version,
		)
	}
	utxos, err := g.Storage.FindKnownUTXOs(ctx, request.Since, request.Limit)
	if err != nil {
		return nil, err
	}

	resp = &InitialResponse{
		Since:    g.LastInteraction,
		UTXOList: utxos,
	}
	slog.Debug(fmt.Sprintf("%sBuilt initial response: %v", g.LogPrefix, resp))
	return resp, nil
}

// GetInitialReply processes an initial response and returns UTXOs not in the response list.
func (g *GASP) GetInitialReply(ctx context.Context, response *InitialResponse) (resp *InitialReply, err error) {
	slog.Info(fmt.Sprintf("%sReceived initial response: %v", g.LogPrefix, response))
	knownUtxos, err := g.Storage.FindKnownUTXOs(ctx, response.Since, 0)
	if err != nil {
		return nil, err
	}

	slog.Info(fmt.Sprintf("%sFound %d known UTXOs since %f", g.LogPrefix, len(knownUtxos), response.Since))
	resp = &InitialReply{
		UTXOList: make([]*Output, 0),
	}
	// Return UTXOs we have that are NOT in the response list
	for _, knownUtxo := range knownUtxos {
		if !slices.ContainsFunc(response.UTXOList, func(responseUtxo *Output) bool {
			return responseUtxo.Txid == knownUtxo.Txid && responseUtxo.OutputIndex == knownUtxo.OutputIndex
		}) {
			resp.UTXOList = append(resp.UTXOList, knownUtxo)
		}
	}
	slog.Info(fmt.Sprintf("%sBuilt initial reply: %v", g.LogPrefix, resp))
	return resp, nil
}

// RequestNode handles a request for a specific node in the GASP graph.
func (g *GASP) RequestNode(ctx context.Context, graphID, outpoint *transaction.Outpoint, metadata bool) (node *Node, err error) {
	slog.Info(fmt.Sprintf("%sRemote is requesting node with graphID: %s, txid: %s, outputIndex: %d, metadata: %v", g.LogPrefix, graphID.String(), outpoint.Txid.String(), outpoint.Index, metadata))
	if node, err = g.Storage.HydrateGASPNode(ctx, graphID, outpoint, metadata); err != nil {
		return nil, err
	}
	slog.Debug(fmt.Sprintf("%sReturning node: %v", g.LogPrefix, node))
	return node, nil
}

// SubmitNode processes a submitted node and returns any needed inputs.
func (g *GASP) SubmitNode(ctx context.Context, node *Node) (requestedInputs *NodeResponse, err error) {
	slog.Info(fmt.Sprintf("%sRemote is submitting node: %v", g.LogPrefix, node))
	if err = g.Storage.AppendToGraph(ctx, node, nil); err != nil {
		return nil, err
	} else if requestedInputs, err = g.Storage.FindNeededInputs(ctx, node); err != nil {
		return nil, err
	} else if requestedInputs != nil {
		slog.Debug(fmt.Sprintf("%sRequested inputs: %v", g.LogPrefix, requestedInputs))
		if err := g.CompleteGraph(ctx, node.GraphID); err != nil {
			return nil, err
		}
	}
	return requestedInputs, nil
}

// CompleteGraph finalizes a newly-synced graph by hydrating and storing outputs.
func (g *GASP) CompleteGraph(ctx context.Context, graphID *transaction.Outpoint) (err error) {
	slog.Info(fmt.Sprintf("%sCompleting newly-synced graph: %s", g.LogPrefix, graphID.String()))
	if err = g.Storage.ValidateGraphAnchor(ctx, graphID); err == nil {
		slog.Debug(fmt.Sprintf("%sGraph validated for node: %s", g.LogPrefix, graphID.String()))
		if finalizeErr := g.Storage.FinalizeGraph(ctx, graphID); finalizeErr == nil {
			return nil
		}
		slog.Info(fmt.Sprintf("%sGraph finalized for node: %s", g.LogPrefix, graphID.String()))
	}
	slog.Warn(fmt.Sprintf("%sError completing graph %s: %v", g.LogPrefix, graphID.String(), err))
	return g.Storage.DiscardGraph(ctx, graphID)
}

func (g *GASP) processIncomingNode(ctx context.Context, node *Node, spentBy *transaction.Outpoint, seenNodes *sync.Map) error {
	txid, err := g.computeTxID(node.RawTx)
	if err != nil {
		return err
	}
	nodeID := (&transaction.Outpoint{
		Txid:  *txid,
		Index: node.OutputIndex,
	}).String()
	slog.Debug(fmt.Sprintf("%sProcessing incoming node: %v, spentBy: %v", g.LogPrefix, node, spentBy))
	if _, ok := seenNodes.Load(nodeID); ok {
		slog.Debug(fmt.Sprintf("%sNode %s already processed, skipping.", g.LogPrefix, nodeID))
		return nil
	}
	seenNodes.Store(nodeID, struct{}{})
	if appendErr := g.Storage.AppendToGraph(ctx, node, spentBy); appendErr != nil {
		return appendErr
	}
	neededInputs, err := g.Storage.FindNeededInputs(ctx, node)
	if err != nil {
		return err
	}
	if neededInputs != nil {
		slog.Debug(fmt.Sprintf("%sNeeded inputs for node %s: %v", g.LogPrefix, nodeID, neededInputs))
		var wg sync.WaitGroup
		errors := make(chan error)
		for outpointStr, data := range neededInputs.RequestedInputs {
			wg.Add(1)
			g.limiter <- struct{}{}
			go func(outpointStr string, data *NodeResponseData) {
				defer func() {
					<-g.limiter
					wg.Done()
				}()
				slog.Info(fmt.Sprintf("%sRequesting new node for outpoint: %s, metadata: %v", g.LogPrefix, outpointStr, data.Metadata))
				if outpoint, err := transaction.OutpointFromString(outpointStr); err != nil {
					errors <- err
				} else if newNode, err := g.Remote.RequestNode(ctx, node.GraphID, outpoint, data.Metadata); err != nil {
					errors <- err
				} else {
					slog.Debug(fmt.Sprintf("%sReceived new node: %v", g.LogPrefix, newNode))
					// Create outpoint for the current node that is spending this input
					spendingOutpoint := &transaction.Outpoint{
						Txid:  *txid,
						Index: node.OutputIndex,
					}
					if err := g.processIncomingNode(ctx, newNode, spendingOutpoint, seenNodes); err != nil {
						errors <- err
					}
				}
			}(outpointStr, data)
		}
		go func() {
			wg.Wait()
			close(errors)
		}()
		for err := range errors {
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (g *GASP) processOutgoingNode(ctx context.Context, node *Node, seenNodes *sync.Map) error {
	if g.Unidirectional {
		slog.Debug(fmt.Sprintf("%sSkipping outgoing node processing in unidirectional mode.", g.LogPrefix))
		return nil
	}
	if node == nil {
		return ErrNodeNilInProcessOutgoingNode
	}
	txid, err := g.computeTxID(node.RawTx)
	if err != nil {
		return err
	}
	nodeID := (&transaction.Outpoint{
		Txid:  *txid,
		Index: node.OutputIndex,
	}).String()
	slog.Debug(fmt.Sprintf("%sProcessing outgoing node: %v", g.LogPrefix, node))
	if _, ok := seenNodes.Load(nodeID); ok {
		slog.Debug(fmt.Sprintf("%sNode %s already processed, skipping.", g.LogPrefix, nodeID))
		return nil
	}
	seenNodes.Store(nodeID, struct{}{})
	response, err := g.Remote.SubmitNode(ctx, node)
	if err != nil {
		return err
	}
	if response != nil {
		var wg sync.WaitGroup
		for outpointStr, data := range response.RequestedInputs {
			wg.Add(1)
			g.limiter <- struct{}{}
			go func(outpointStr string, data *NodeResponseData) {
				defer func() {
					<-g.limiter
					wg.Done()
				}()
				var outpoint *transaction.Outpoint
				var err error
				if outpoint, err = transaction.OutpointFromString(outpointStr); err == nil {
					var hydratedNode *Node
					slog.Info(fmt.Sprintf("%sHydrating node for outpoint: %s, metadata: %v", g.LogPrefix, outpoint, data.Metadata))
					if hydratedNode, err = g.Storage.HydrateGASPNode(ctx, node.GraphID, outpoint, data.Metadata); err == nil {
						slog.Debug(fmt.Sprintf("%sSending hydrated node: %v", g.LogPrefix, hydratedNode))
						if err = g.processOutgoingNode(ctx, hydratedNode, seenNodes); err == nil {
							return
						}
					}
				}
				slog.Error(fmt.Sprintf("%sError hydrating node: %v", g.LogPrefix, err))
			}(outpointStr, data)
		}
		wg.Wait()
	}
	return nil
}

func (g *GASP) computeTxID(rawtx string) (*chainhash.Hash, error) {
	tx, err := transaction.NewTransactionFromHex(rawtx)
	if err != nil {
		return nil, err
	}
	return tx.TxID(), nil
}
