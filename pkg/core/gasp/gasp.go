// Package gasp implements the Graph Aware Sync Protocol for synchronizing transaction graphs.
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
	"golang.org/x/sync/errgroup"
)

// MaxConcurrency defines the maximum number of concurrent GASP operations allowed.
const MaxConcurrency = 16

var (
	// ErrNodeNilInProcessOutgoingNode is returned when a nil node is passed to processOutgoingNode.
	ErrNodeNilInProcessOutgoingNode = errors.New("node is nil in processOutgoingNode")
	// ErrTransactionParsingPanic is returned when transaction parsing triggers a panic.
	ErrTransactionParsingPanic = errors.New("panic during transaction parsing")
	// ErrTransactionHexTooShort is returned when transaction hex is too short to be valid.
	ErrTransactionHexTooShort = errors.New("transaction hex too short")
	// ErrTransactionHexTooLong is returned when transaction hex exceeds maximum size.
	ErrTransactionHexTooLong = errors.New("transaction hex too long")

	// ErrGraphNoTopicalAdmittance indicates that the graph did not result in topical admittance of the root node.
	ErrGraphNoTopicalAdmittance = errors.New("graph did not result in topical admittance of the root node")
	// ErrUnresolvedInputs is returned when dependency processing fails to resolve all required inputs
	ErrUnresolvedInputs = errors.New("not all inputs could be resolved")
	// ErrUTXOQueueFull is returned when the UTXO processing queue cannot accept more items
	ErrUTXOQueueFull = errors.New("UTXO processing queue full")
)

// utxoProcessingState tracks the state of a UTXO processing operation with result sharing
type utxoProcessingState struct {
	wg  sync.WaitGroup
	err error
}

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
	Topic           string
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
	Topic           string
	limiter         chan struct{} // Concurrency limiter controlled by Concurrency config

	// Unified UTXO processing with result sharing
	utxoProcessingMap sync.Map // map[transaction.Outpoint]*utxoProcessingState

	// Individual UTXO processing queue (hidden from external callers)
	utxoQueue chan *transaction.Outpoint
	done      chan struct{} // signals runProcessingWorker to stop
}

// NewGASP creates a new GASP instance with the provided parameters.
func NewGASP(params Params) *GASP {
	gasp := &GASP{
		Storage:         params.Storage,
		Remote:          params.Remote,
		LastInteraction: params.LastInteraction,
		Unidirectional:  params.Unidirectional,
		Topic:           params.Topic,
		utxoQueue:       make(chan *transaction.Outpoint, 1000),
		done:            make(chan struct{}),
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

	// Start the always-running worker for individual UTXO processing
	go gasp.runProcessingWorker()

	return gasp
}

// Sync performs a GASP synchronization with the specified host.
func (g *GASP) Sync(ctx context.Context, _ string, limit uint32) error {
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
		processingGroup.Go(func() error {
			outpoint := utxo.Outpoint()
			defer func() {
				<-g.limiter
			}()

			if err := g.ProcessUTXOToCompletion(processingCtx, outpoint, nil, seenNodes); err != nil {
				slog.Error("error processing UTXO", "outpoint", outpoint, "error", err)
				return fmt.Errorf("error processing UTXO %s: %w", outpoint, err)
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
					slog.Debug(fmt.Sprintf("%s Hydrating GASP node for UTXO: %s.%d", g.LogPrefix, utxo.Txid, utxo.OutputIndex))
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

// GetInitialResponse processes an initial GASP request and returns known UTXOs.
func (g *GASP) GetInitialResponse(ctx context.Context, request *InitialRequest) (resp *InitialResponse, err error) {
	slog.Debug(fmt.Sprintf("%s Received initial request: %v", g.LogPrefix, request))
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

// GetInitialReply processes an initial response and returns UTXOs not in the response list.
func (g *GASP) GetInitialReply(ctx context.Context, response *InitialResponse) (resp *InitialReply, err error) {
	slog.Debug(fmt.Sprintf("%s Received initial response: %v", g.LogPrefix, response))
	knownUtxos, err := g.Storage.FindKnownUTXOs(ctx, response.Since, 0)
	if err != nil {
		return nil, err
	}

	slog.Debug(fmt.Sprintf("%s Found %d known UTXOs since %f", g.LogPrefix, len(knownUtxos), response.Since))
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
	slog.Debug(fmt.Sprintf("%s Built initial reply: %v", g.LogPrefix, resp))
	return resp, nil
}

// RequestNode handles a request for a specific node in the GASP graph.
func (g *GASP) RequestNode(ctx context.Context, graphID, outpoint *transaction.Outpoint, metadata bool) (node *Node, err error) {
	slog.Debug(fmt.Sprintf("%s Remote is requesting node with graphID: %s, txid: %s, outputIndex: %d, metadata: %v", g.LogPrefix, graphID.String(), outpoint.Txid.String(), outpoint.Index, metadata))
	if node, err = g.Storage.HydrateGASPNode(ctx, graphID, outpoint, metadata); err != nil {
		return nil, err
	}
	slog.Debug(fmt.Sprintf("%s Returning node: %v", g.LogPrefix, node))
	return node, nil
}

// SubmitNode processes a submitted node and returns any needed inputs.
func (g *GASP) SubmitNode(ctx context.Context, node *Node) (requestedInputs *NodeResponse, err error) {
	slog.Debug(fmt.Sprintf("%s Remote is submitting node: %v", g.LogPrefix, node))
	if err = g.Storage.AppendToGraph(ctx, node, nil); err != nil {
		return nil, err
	} else if requestedInputs, err = g.Storage.FindNeededInputs(ctx, node); err != nil {
		return nil, err
	} else if requestedInputs != nil {
		slog.Debug(fmt.Sprintf("%s Requested inputs: %v", g.LogPrefix, requestedInputs))
		if completeErr := g.CompleteGraph(ctx, node.GraphID); completeErr != nil {
			return nil, completeErr
		}
	}
	return requestedInputs, nil
}

// CompleteGraph finalizes a newly-synced graph by hydrating and storing outputs.
func (g *GASP) CompleteGraph(ctx context.Context, graphID *transaction.Outpoint) error {
	if err := g.Storage.ValidateGraphAnchor(ctx, graphID); err != nil {
		slog.Warn(fmt.Sprintf("%s Error completing graph %s: %v", g.LogPrefix, graphID.String(), err))
		_ = g.Storage.DiscardGraph(ctx, graphID)
		return err
	}
	slog.Debug(fmt.Sprintf("%s Graph validated for node: %s", g.LogPrefix, graphID.String()))
	if err := g.Storage.FinalizeGraph(ctx, graphID); err != nil {
		slog.Warn(fmt.Sprintf("%s Error completing graph %s: %v", g.LogPrefix, graphID.String(), err))
		_ = g.Storage.DiscardGraph(ctx, graphID)
		return err
	}
	slog.Debug(fmt.Sprintf("%s Graph finalized for node: %s", g.LogPrefix, graphID.String()))
	_ = g.Storage.DiscardGraph(ctx, graphID)
	return nil
}

func (g *GASP) processIncomingNode(ctx context.Context, node *Node, spentBy *transaction.Outpoint, seenNodes *sync.Map) error {
	txid, err := g.computeTxID(node.RawTx)
	if err != nil {
		return err
	}
	nodeOutpoint := &transaction.Outpoint{
		Txid:  *txid,
		Index: node.OutputIndex,
	}

	slog.Debug(fmt.Sprintf("%s Processing incoming node: %v, spentBy: %v", g.LogPrefix, node, spentBy))

	// Per-graph cycle detection
	if _, ok := seenNodes.Load(*nodeOutpoint); ok {
		slog.Debug(fmt.Sprintf("%s Node %s already seen in this graph, skipping.", g.LogPrefix, nodeOutpoint.String()))
		return nil
	}
	seenNodes.Store(*nodeOutpoint, struct{}{})

	if appendErr := g.Storage.AppendToGraph(ctx, node, spentBy); appendErr != nil {
		return appendErr
	}
	neededInputs, err := g.Storage.FindNeededInputs(ctx, node)
	if err != nil {
		return err
	}
	if neededInputs != nil && len(neededInputs.RequestedInputs) > 0 {
		slog.Debug(fmt.Sprintf("%s Needed inputs for node %s: %v", g.LogPrefix, nodeOutpoint.String(), neededInputs))
		for outpoint, data := range neededInputs.RequestedInputs {
			slog.Debug(fmt.Sprintf("%s Processing dependency for outpoint: %s, metadata: %v", g.LogPrefix, outpoint.String(), data.Metadata))
			if processErr := g.ProcessUTXOToCompletion(ctx, &outpoint, nodeOutpoint, seenNodes); processErr != nil {
				if errors.Is(processErr, ErrGraphNoTopicalAdmittance) {
					return fmt.Errorf("dependency %s not admitted: %w", outpoint.String(), processErr)
				}
				slog.Warn(fmt.Sprintf("%s Error processing dependency %s: %v", g.LogPrefix, outpoint.String(), processErr))
			}
		}
		neededInputs, err = g.Storage.FindNeededInputs(ctx, node)
		if err != nil {
			slog.Error(fmt.Sprintf("%s Error re-checking needed inputs for node %s: %v", g.LogPrefix, nodeOutpoint.String(), err))
			return err
		}
		if neededInputs != nil && len(neededInputs.RequestedInputs) > 0 {
			return fmt.Errorf("%w for node %s after processing dependencies", ErrUnresolvedInputs, nodeOutpoint.String())
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
		return ErrNodeNilInProcessOutgoingNode
	}
	txid, err := g.computeTxID(node.RawTx)
	if err != nil {
		return err
	}
	nodeID := transaction.Outpoint{
		Txid:  *txid,
		Index: node.OutputIndex,
	}
	slog.Debug(fmt.Sprintf("%s Processing outgoing node: %v", g.LogPrefix, node))
	if _, ok := seenNodes.Load(nodeID); ok {
		slog.Debug(fmt.Sprintf("%s Node %s already processed, skipping.", g.LogPrefix, nodeID.String()))
		return nil
	}
	seenNodes.Store(nodeID, struct{}{})
	response, err := g.Remote.SubmitNode(ctx, node)
	if err != nil {
		return err
	}
	if response != nil {
		var wg sync.WaitGroup
		for outpoint, data := range response.RequestedInputs {
			wg.Add(1)
			go func(outpoint transaction.Outpoint, data *NodeResponseData) {
				defer wg.Done()
				var hydratedNode *Node
				var err error
				slog.Debug(fmt.Sprintf("%s Hydrating node for outpoint: %s, metadata: %v", g.LogPrefix, outpoint.String(), data.Metadata))
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
	return nil
}

// ProcessUTXOToCompletion handles the complete UTXO processing pipeline with result sharing deduplication.
func (g *GASP) ProcessUTXOToCompletion(ctx context.Context, outpoint, spentBy *transaction.Outpoint, seenNodes *sync.Map) error {
	// Pre-initialize the processing state to avoid race conditions
	newState := &utxoProcessingState{}
	newState.wg.Add(1)

	// Check if there's already an in-flight operation for this outpoint
	if inflight, loaded := g.utxoProcessingMap.LoadOrStore(*outpoint, newState); loaded {
		state := inflight.(*utxoProcessingState)
		state.wg.Wait()
		return state.err
	}

	state := newState
	defer func() {
		state.wg.Done()
		g.utxoProcessingMap.Delete(*outpoint)
	}()

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

	// Complete the graph (submit to engine) using the outpoint we requested
	if err = g.CompleteGraph(ctx, outpoint); err != nil {
		state.err = fmt.Errorf("error completing graph for %s: %w", outpoint, err)
		return state.err
	}

	return nil
}

func (g *GASP) computeTxID(rawtx string) (txID *chainhash.Hash, err error) {
	// Recover from panics in transaction parsing (e.g., malformed VarInts in go-sdk)
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%w: %v", ErrTransactionParsingPanic, r)
			txID = nil
		}
	}()

	// Validate input length to prevent problematic VarInt patterns that cause EOF errors
	// during fuzz test minimization. Minimum valid transaction is ~10 bytes (20 hex chars):
	// 4 bytes version + 1 byte input count + 1 byte output count + 4 bytes locktime
	if len(rawtx) < 20 {
		return nil, fmt.Errorf("%w: %d characters (minimum 20)", ErrTransactionHexTooShort, len(rawtx))
	}

	// Check hex string length before decoding to prevent memory exhaustion
	// 2 hex chars = 1 byte, so 200M hex chars = 100MB decoded
	if len(rawtx) > 200_000_000 {
		return nil, fmt.Errorf("%w: %d characters (maximum 200,000,000)", ErrTransactionHexTooLong, len(rawtx))
	}

	tx, err := transaction.NewTransactionFromHex(rawtx)
	if err != nil {
		return nil, err
	}
	return tx.TxID(), nil
}

// ProcessUTXO queues a single UTXO for processing outside of the sync workflow.
// UTXOs are processed with shared deduplication state to ensure each transaction
// is only submitted once, even if multiple outputs are queued concurrently.
// This method is non-blocking - if the internal queue is full, the UTXO is dropped.
// Does NOT update LastInteraction score.
func (g *GASP) ProcessUTXO(_ context.Context, outpoint *transaction.Outpoint) error {
	select {
	case g.utxoQueue <- outpoint:
		return nil
	default:
		slog.Warn(fmt.Sprintf("%s UTXO processing queue full, dropping UTXO %s", g.LogPrefix, outpoint.String()))
		return fmt.Errorf("%w: %s", ErrUTXOQueueFull, outpoint.String())
	}
}

// Close stops the processing worker goroutine and drains the queue.
func (g *GASP) Close() {
	close(g.utxoQueue)
	<-g.done
}

// runProcessingWorker is the background worker that processes queued UTXOs.
// It exits when utxoQueue is closed (via Close).
func (g *GASP) runProcessingWorker() {
	defer close(g.done)
	seenNodes := &sync.Map{}

	for outpoint := range g.utxoQueue {
		g.limiter <- struct{}{}
		go func(op *transaction.Outpoint) {
			defer func() {
				<-g.limiter
			}()

			ctx := context.Background()
			if err := g.ProcessUTXOToCompletion(ctx, op, nil, seenNodes); err != nil {
				slog.Error(fmt.Sprintf("%s Error processing UTXO %s: %v", g.LogPrefix, op, err))
			}

			// Cleanup all seenNodes entries after processing completes
			seenNodes.Range(func(key, _ any) bool {
				seenNodes.Delete(key)
				return true
			})
		}(outpoint)
	}
}
