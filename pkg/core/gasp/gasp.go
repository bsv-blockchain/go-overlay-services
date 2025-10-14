package gasp

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sync"

	"github.com/bsv-blockchain/go-sdk/chainhash"
	"github.com/bsv-blockchain/go-sdk/transaction"
	"golang.org/x/sync/errgroup"
)

const MAX_CONCURRENCY = 16

// utxoProcessingState tracks the state of a UTXO processing operation with result sharing
type utxoProcessingState struct {
	wg  sync.WaitGroup
	err error
}

type NodeRequest struct {
	GraphID     *transaction.Outpoint `json:"graphID"`
	Txid        *chainhash.Hash       `json:"txid"`
	OutputIndex uint32                `json:"outputIndex"`
	Metadata    bool                  `json:"metadata"`
}

type GASPParams struct {
	Storage         Storage
	Remote          Remote
	LastInteraction float64
	Version         *int
	LogPrefix       *string
	Unidirectional  bool
	LogLevel        slog.Level
	Concurrency     int
	Topic           string
}

type GASP struct {
	Version         int
	Remote          Remote
	Storage         Storage
	LastInteraction float64
	LogPrefix       string
	Unidirectional  bool
	LogLevel        slog.Level
	Topic           string
	limiter         chan struct{} // Concurrency limiter controlled by Concurrency config

	// Unified UTXO processing with result sharing
	utxoProcessingMap sync.Map // map[transaction.Outpoint]*utxoProcessingState
}

func NewGASP(params GASPParams) *GASP {
	gasp := &GASP{
		Storage:         params.Storage,
		Remote:          params.Remote,
		LastInteraction: params.LastInteraction,
		Unidirectional:  params.Unidirectional,
		Topic:           params.Topic,
	}
	// Concurrency limiter controlled by Concurrency config
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

func (g *GASP) Sync(ctx context.Context, host string, limit uint32) error {
	var sharedOutpoints sync.Map

	initialRequest := &InitialRequest{
		Version: g.Version,
		Since:   g.LastInteraction,
		Limit:   limit,
	}
	initialResponse, err := g.Remote.GetInitialResponse(ctx, initialRequest)
	if err != nil {
		return err
	}

	if len(initialResponse.UTXOList) == 0 {
		// No more UTXOs to process
		return nil
	}

	// Extract outpoints from current page for efficient batch lookup
	pageOutpoints := make([]*transaction.Outpoint, len(initialResponse.UTXOList))
	for i, utxo := range initialResponse.UTXOList {
		pageOutpoints[i] = utxo.Outpoint()
	}

	// Check which outpoints we already have
	hasOutputs, err := g.Storage.HasOutputs(ctx, pageOutpoints)
	if err != nil {
		return err
	}

	var ingestQueue []*Output
	for i, utxo := range initialResponse.UTXOList {
		if utxo.Score > g.LastInteraction {
			g.LastInteraction = utxo.Score
		}
		outpoint := utxo.Outpoint()

		// Check if we already have this output using the same index
		if hasOutputs[i] {
			// Already have it, mark as shared to avoid re-processing
			sharedOutpoints.Store(*outpoint, struct{}{})
		} else {
			// Don't have it - need to ingest
			if _, shared := sharedOutpoints.Load(*outpoint); !shared {
				ingestQueue = append(ingestQueue, utxo)
			}
		}
	}

	// Process all UTXOs from this batch with shared deduplication
	processingGroup, processingCtx := errgroup.WithContext(ctx)
	seenNodes := &sync.Map{} // Shared across all UTXOs in this batch

	for _, utxo := range ingestQueue {
		g.limiter <- struct{}{}
		utxo := utxo // capture loop variable
		processingGroup.Go(func() error {
			outpoint := utxo.Outpoint()
			defer func() {
				<-g.limiter
			}()

			if err := g.processUTXOToCompletion(processingCtx, outpoint, nil, seenNodes); err != nil {
				slog.Error("error processing UTXO", "outpoint", outpoint, "error", err)
				return nil
			}
			sharedOutpoints.Store(*outpoint, struct{}{})
			return nil
		})
	}
	slog.Info(fmt.Sprintf("%s Processing GASP page: %d UTXOs (since: %.0f)", g.LogPrefix, len(ingestQueue), initialRequest.Since))
	if err := processingGroup.Wait(); err != nil {
		return err
	}
	// 2. Only do the "reply" half if unidirectional is disabled
	if !g.Unidirectional && initialResponse != nil {
		// Load local UTXOs only newer than what the peer already knows about
		localUTXOs, err := g.Storage.FindKnownUTXOs(ctx, initialResponse.Since, 0)
		if err != nil {
			return err
		}

		// Filter localUTXOs for those not in sharedOutpoints
		var replyUTXOs []*Output
		for _, utxo := range localUTXOs {
			outpoint := utxo.Outpoint()
			if _, shared := sharedOutpoints.Load(*outpoint); !shared {
				replyUTXOs = append(replyUTXOs, utxo)
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
					slog.Info(fmt.Sprintf("%s Hydrating GASP node for UTXO: %s.%d", g.LogPrefix, utxo.Txid, utxo.OutputIndex))
					outpoint := utxo.Outpoint()
					outgoingNode, err := g.Storage.HydrateGASPNode(ctx, outpoint, outpoint, true)
					if err != nil {
						slog.Warn(fmt.Sprintf("%s Error hydrating outgoing UTXO %s.%d: %v", g.LogPrefix, utxo.Txid, utxo.OutputIndex, err))
						return
					}
					if outgoingNode == nil {
						slog.Debug(fmt.Sprintf("%s Skipping outgoing UTXO %s.%d: not found in storage", g.LogPrefix, utxo.Txid, utxo.OutputIndex))
						return
					}
					slog.Debug(fmt.Sprintf("%s Sending unspent graph node for remote: %v", g.LogPrefix, outgoingNode))
					if err = g.processOutgoingNode(ctx, outgoingNode, &sync.Map{}); err != nil {
						slog.Warn(fmt.Sprintf("%s Error processing outgoing node %s.%d: %v", g.LogPrefix, utxo.Txid, utxo.OutputIndex, err))
					}
				}(utxo)
			}
			wg.Wait()
		}
	}

	return nil
}

func (g *GASP) GetInitialResponse(ctx context.Context, request *InitialRequest) (resp *InitialResponse, err error) {
	slog.Info(fmt.Sprintf("%s Received initial request: %v", g.LogPrefix, request))
	if request.Version != g.Version {
		slog.Error(fmt.Sprintf("%s GASP version mismatch", g.LogPrefix))
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
	slog.Debug(fmt.Sprintf("%s Built initial response: %v", g.LogPrefix, resp))
	return resp, nil
}

func (g *GASP) GetInitialReply(ctx context.Context, response *InitialResponse) (resp *InitialReply, err error) {
	slog.Info(fmt.Sprintf("%s Received initial response: %v", g.LogPrefix, response))
	knownUtxos, err := g.Storage.FindKnownUTXOs(ctx, response.Since, 0)
	if err != nil {
		return nil, err
	}

	slog.Info(fmt.Sprintf("%s Found %d known UTXOs since %f", g.LogPrefix, len(knownUtxos), response.Since))
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
	slog.Info(fmt.Sprintf("%s Built initial reply: %v", g.LogPrefix, resp))
	return resp, nil
}

func (g *GASP) RequestNode(ctx context.Context, graphID *transaction.Outpoint, outpoint *transaction.Outpoint, metadata bool) (node *Node, err error) {
	slog.Info(fmt.Sprintf("%s Remote is requesting node with graphID: %s, txid: %s, outputIndex: %d, metadata: %v", g.LogPrefix, graphID.String(), outpoint.Txid.String(), outpoint.Index, metadata))
	if node, err = g.Storage.HydrateGASPNode(ctx, graphID, outpoint, metadata); err != nil {
		return nil, err
	}
	slog.Debug(fmt.Sprintf("%s Returning node: %v", g.LogPrefix, node))
	return node, nil
}

func (g *GASP) SubmitNode(ctx context.Context, node *Node) (requestedInputs *NodeResponse, err error) {
	slog.Info(fmt.Sprintf("%s Remote is submitting node: %v", g.LogPrefix, node))
	if err = g.Storage.AppendToGraph(ctx, node, nil); err != nil {
		return nil, err
	} else if requestedInputs, err = g.Storage.FindNeededInputs(ctx, node); err != nil {
		return nil, err
	} else if requestedInputs != nil {
		slog.Debug(fmt.Sprintf("%s Requested inputs: %v", g.LogPrefix, requestedInputs))
		if err := g.CompleteGraph(ctx, node.GraphID); err != nil {
			return nil, err
		}
	}
	return requestedInputs, nil
}

func (g *GASP) CompleteGraph(ctx context.Context, graphID *transaction.Outpoint) (err error) {
	if err = g.Storage.ValidateGraphAnchor(ctx, graphID); err == nil {
		slog.Debug(fmt.Sprintf("%s Graph validated for node: %s", g.LogPrefix, graphID.String()))
		if err := g.Storage.FinalizeGraph(ctx, graphID); err == nil {
			slog.Info(fmt.Sprintf("%s Graph finalized for node: %s", g.LogPrefix, graphID.String()))
			return nil
		}
	}
	slog.Warn(fmt.Sprintf("%s Error completing graph %s: %v", g.LogPrefix, graphID.String(), err))
	return g.Storage.DiscardGraph(ctx, graphID)
}

func (g *GASP) processIncomingNode(ctx context.Context, node *Node, spentBy *transaction.Outpoint, seenNodes *sync.Map) error {
	if txid, err := g.computeTxID(node.RawTx); err != nil {
		return err
	} else {
		nodeOutpoint := &transaction.Outpoint{
			Txid:  *txid,
			Index: node.OutputIndex,
		}
		nodeId := nodeOutpoint.String()

		slog.Debug(fmt.Sprintf("%s Processing incoming node: %v, spentBy: %v", g.LogPrefix, node, spentBy))

		// Per-graph cycle detection
		if _, ok := seenNodes.Load(nodeId); ok {
			slog.Debug(fmt.Sprintf("%s Node %s already seen in this graph, skipping.", g.LogPrefix, nodeId))
			return nil
		}
		seenNodes.Store(nodeId, struct{}{})

		if err := g.Storage.AppendToGraph(ctx, node, spentBy); err != nil {
			return err
		} else if neededInputs, err := g.Storage.FindNeededInputs(ctx, node); err != nil {
			return err
		} else if neededInputs != nil {
			slog.Debug(fmt.Sprintf("%s Needed inputs for node %s: %v", g.LogPrefix, nodeId, neededInputs))
			for outpoint, data := range neededInputs.RequestedInputs {
				slog.Info(fmt.Sprintf("%s Processing dependency for outpoint: %s, metadata: %v", g.LogPrefix, outpoint.String(), data.Metadata))
				if err := g.processUTXOToCompletion(ctx, &outpoint, nodeOutpoint, seenNodes); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (g *GASP) processOutgoingNode(ctx context.Context, node *Node, seenNodes *sync.Map) error {
	if g.Unidirectional {
		slog.Debug(fmt.Sprintf("%s Skipping outgoing node processing in unidirectional mode.", g.LogPrefix))
		return nil
	}
	if node == nil {
		return fmt.Errorf("node is nil in processOutgoingNode")
	}
	if txid, err := g.computeTxID(node.RawTx); err != nil {
		return err
	} else {
		nodeId := (&transaction.Outpoint{
			Txid:  *txid,
			Index: node.OutputIndex,
		}).String()
		slog.Debug(fmt.Sprintf("%s Processing outgoing node: %v", g.LogPrefix, node))
		if _, ok := seenNodes.Load(nodeId); ok {
			slog.Debug(fmt.Sprintf("%s Node %s already processed, skipping.", g.LogPrefix, nodeId))
			return nil
		}
		seenNodes.Store(nodeId, struct{}{})
		if response, err := g.Remote.SubmitNode(ctx, node); err != nil {
			return err
		} else if response != nil {
			var wg sync.WaitGroup
			for outpoint, data := range response.RequestedInputs {
				wg.Add(1)
				go func(outpoint transaction.Outpoint, data *NodeResponseData) {
					defer wg.Done()
					var hydratedNode *Node
					var err error
					slog.Info(fmt.Sprintf("%s Hydrating node for outpoint: %s, metadata: %v", g.LogPrefix, outpoint.String(), data.Metadata))
					if hydratedNode, err = g.Storage.HydrateGASPNode(ctx, node.GraphID, &outpoint, data.Metadata); err == nil {
						slog.Debug(fmt.Sprintf("%s Sending hydrated node: %v", g.LogPrefix, hydratedNode))
						if err = g.processOutgoingNode(ctx, hydratedNode, seenNodes); err == nil {
							return
						}
					}
					if err != nil {
						slog.Error(fmt.Sprintf("%s Error hydrating node: %v", g.LogPrefix, err))
					}
				}(outpoint, data)
			}
			wg.Wait()
		}
	}
	return nil
}

// processUTXOToCompletion handles the complete UTXO processing pipeline with result sharing deduplication
func (g *GASP) processUTXOToCompletion(ctx context.Context, outpoint *transaction.Outpoint, spentBy *transaction.Outpoint, seenNodes *sync.Map) error {
	// Pre-initialize the processing state to avoid race conditions
	newState := &utxoProcessingState{}
	newState.wg.Add(1)

	// Check if there's already an in-flight operation for this outpoint
	if inflight, loaded := g.utxoProcessingMap.LoadOrStore(*outpoint, newState); loaded {
		state := inflight.(*utxoProcessingState)
		state.wg.Wait()
		return state.err
	} else {
		state := newState
		defer state.wg.Done() // Signal completion when we're done

		// We're the first to process this outpoint, do the complete processing

		// Request node from remote
		resolvedNode, err := g.Remote.RequestNode(ctx, spentBy, outpoint, true)
		if err != nil {
			state.err = fmt.Errorf("error with incoming UTXO %s: %w", outpoint, err)
			return state.err
		}
		// Process dependencies
		if err = g.processIncomingNode(ctx, resolvedNode, spentBy, seenNodes); err != nil {
			state.err = fmt.Errorf("error processing incoming node %s: %w", outpoint, err)
			return state.err
		}

		// Complete the graph (submit to engine)
		if err = g.CompleteGraph(ctx, resolvedNode.GraphID); err != nil {
			state.err = fmt.Errorf("error completing graph for %s: %w", outpoint, err)
			return state.err
		}

		// Success - don't clean up immediately, handle externally
		return nil
	}
}

func (g *GASP) computeTxID(rawtx string) (*chainhash.Hash, error) {
	if tx, err := transaction.NewTransactionFromHex(rawtx); err != nil {
		return nil, err
	} else {
		return tx.TxID(), nil
	}
}
