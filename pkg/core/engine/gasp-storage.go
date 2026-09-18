package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/bsv-blockchain/go-sdk/chainhash"
	"github.com/bsv-blockchain/go-sdk/overlay"
	"github.com/bsv-blockchain/go-sdk/spv"
	"github.com/bsv-blockchain/go-sdk/transaction"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/gasp"
)

var (
	// ErrGraphFull indicates the graph has reached its maximum size
	ErrGraphFull = errors.New("graph is full")

	// ErrParsedBEEFReturnedNilTx indicates that parsing BEEF returned a nil transaction
	ErrParsedBEEFReturnedNilTx = errors.New("parsed BEEF returned nil transaction")

	// ErrGraphAnchorInvalidTx indicates that the graph anchor is not a valid transaction
	ErrGraphAnchorInvalidTx = errors.New("graph anchor is not a valid transaction")

	// ErrGraphNoTopicalAdmittance is an alias for gasp.ErrGraphNoTopicalAdmittance for backward compatibility.
	ErrGraphNoTopicalAdmittance = gasp.ErrGraphNoTopicalAdmittance
	// ErrUnableToFindRootNodeInGraph indicates that the root node could not be found in the graph for finalization
	ErrUnableToFindRootNodeInGraph = errors.New("unable to find root node in graph for finalization")
	// ErrRequiredInputNodeNotFoundInTempGraph indicates that a required input node was not found in the temporary graph store
	ErrRequiredInputNodeNotFoundInTempGraph = errors.New("required input node for unproven parent not found in temporary graph store")

	// ErrMissingGraphID is returned when a GASP node carries no graph ID
	ErrMissingGraphID = errors.New("gasp node is missing its graph ID")
	// ErrGraphDependencyCycle indicates the graph's transactions could not be dependency-ordered
	ErrGraphDependencyCycle = errors.New("dependency cycle in graph transactions")

	// ErrNoManagerForTopic is returned when no topic manager is registered for the requested topic
	ErrNoManagerForTopic = errors.New("no manager for topic")
	// ErrNoTransactionInBEEF is returned when a BEEF contains no transaction
	ErrNoTransactionInBEEF = errors.New("no transaction in BEEF")
	// ErrNilNode is returned when a nil graph node is passed to BEEF construction
	ErrNilNode = errors.New("nil graph node")
)

// submissionState tracks the state of a transaction submission
type submissionState struct {
	wg  sync.WaitGroup
	err error
}

// GraphNode represents a node in the GASP graph
type GraphNode struct {
	gasp.Node

	Txid     *chainhash.Hash `json:"txid"`
	SpentBy  *chainhash.Hash `json:"spentBy"`
	Children sync.Map        `json:"-"` // map[string]*GraphNode - concurrent safe
	Parent   *GraphNode      `json:"parent"`
}

// graphContext holds the temporary node graph for a single GASP traversal,
// keyed by the traversal's root outpoint. Each traversal owns its context
// exclusively, so concurrent traversals cannot disturb each other's nodes;
// DiscardGraph drops the whole context in one step.
type graphContext struct {
	nodes sync.Map // map[transaction.Outpoint]*GraphNode
	count atomic.Int64
}

// OverlayGASPStorage implements GASP storage using the overlay engine
type OverlayGASPStorage struct {
	Topic             string
	Engine            *Engine
	MaxNodesInGraph   *int
	Logger            *slog.Logger // Optional; defaults to slog.Default() when nil.
	graphs            sync.Map     // map[transaction.Outpoint]*graphContext
	submissionTracker sync.Map     // map[chainhash.Hash]*submissionState
}

// NewOverlayGASPStorage creates a new OverlayGASPStorage instance
func NewOverlayGASPStorage(topic string, engine *Engine, maxNodesInGraph *int) *OverlayGASPStorage {
	return &OverlayGASPStorage{
		Topic:           topic,
		Engine:          engine,
		MaxNodesInGraph: maxNodesInGraph,
	}
}

// log returns the configured logger or slog.Default() when no logger has been set.
func (s *OverlayGASPStorage) log() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}

// ErrNoKnownUTXOs is returned when no UTXOs are found
var ErrNoKnownUTXOs = errors.New("no known UTXOs")

// FindKnownUTXOs retrieves known UTXOs for the topic
func (s *OverlayGASPStorage) FindKnownUTXOs(ctx context.Context, since float64, limit uint32) ([]*gasp.Output, error) {
	utxos, err := s.Engine.Storage.FindUTXOsForTopic(ctx, s.Topic, since, limit, false)
	if err != nil {
		return nil, err
	}
	gaspOutputs := make([]*gasp.Output, len(utxos))

	for i, utxo := range utxos {
		gaspOutputs[i] = &gasp.Output{
			Txid:        utxo.Outpoint.Txid,
			OutputIndex: utxo.Outpoint.Index,
			Score:       utxo.Score,
		}
	}

	return gaspOutputs, nil
}

// HasOutputs checks whether the given outpoints exist in storage.
func (s *OverlayGASPStorage) HasOutputs(ctx context.Context, outpoints []*transaction.Outpoint) ([]bool, error) {
	// Use FindOutputs to check existence - don't need BEEF for existence check
	outputs, err := s.Engine.Storage.FindOutputs(ctx, outpoints, s.Topic, nil, false)
	if err != nil {
		return nil, err
	}

	// Convert to boolean array - true if output exists, false if nil
	result := make([]bool, len(outputs))
	for i, output := range outputs {
		result[i] = output != nil
	}
	return result, nil
}

// HydrateGASPNode hydrates a GASP node from storage
func (s *OverlayGASPStorage) HydrateGASPNode(ctx context.Context, graphID, outpoint *transaction.Outpoint, _ bool) (*gasp.Node, error) {
	output, err := s.Engine.Storage.FindOutput(ctx, outpoint, nil, nil, true)
	if err != nil {
		return nil, err
	}
	if output == nil || output.Beef == nil {
		return nil, ErrMissingInput
	}
	// Get the transaction from BEEF
	tx := output.Beef.FindTransactionForSigningByHash(&outpoint.Txid)
	if tx == nil {
		return nil, ErrParsedBEEFReturnedNilTx
	}

	node := &gasp.Node{
		GraphID:     graphID,
		OutputIndex: outpoint.Index,
		RawTx:       tx.Bytes(),
	}
	if tx.MerklePath != nil {
		node.Proof = tx.MerklePath.Bytes()
	}
	return node, nil
}

// ErrNoNeededInputs is returned when no inputs are needed
var ErrNoNeededInputs = errors.New("no needed inputs")

// FindNeededInputs determines which inputs are needed for a GASP transaction
func (s *OverlayGASPStorage) FindNeededInputs(ctx context.Context, gaspTx *gasp.Node) (*gasp.NodeResponse, error) {
	response := &gasp.NodeResponse{
		RequestedInputs: make(map[transaction.Outpoint]*gasp.NodeResponseData),
	}
	tx, err := transaction.NewTransactionFromBytes(gaspTx.RawTx)
	if err != nil {
		return nil, err
	}
	// Commented out: This was requesting ALL inputs for unmined transactions
	// but should use IdentifyNeededInputs to get only relevant inputs
	if len(gaspTx.Proof) == 0 {
		for _, input := range tx.Inputs {
			outpoint := &transaction.Outpoint{
				Txid:  *input.SourceTXID,
				Index: input.SourceTxOutIndex,
			}
			response.RequestedInputs[*outpoint] = &gasp.NodeResponseData{
				Metadata: false,
			}
		}

		return s.stripAlreadyKnowInputs(ctx, response)
	}

	// Process merkle proof if present
	if len(gaspTx.Proof) > 0 {
		if tx.MerklePath, err = transaction.NewMerklePathFromBinary(gaspTx.Proof); err != nil {
			return nil, err
		}
	}

	var beef *transaction.Beef
	if tx.MerklePath != nil {
		// If we have a merkle path, create BEEF from transaction
		if beef, err = transaction.NewBeefFromTransaction(tx); err != nil {
			return nil, err
		}
	}

	if beef != nil {
		inpoints := make([]*transaction.Outpoint, len(tx.Inputs))
		for vin, input := range tx.Inputs {
			inpoints[vin] = &transaction.Outpoint{
				Txid:  *input.SourceTXID,
				Index: input.SourceTxOutIndex,
			}
		}
		previousCoins := make([]uint32, 0, len(tx.Inputs))
		outputs, err := s.Engine.Storage.FindOutputs(ctx, inpoints, s.Topic, nil, true)
		if err != nil {
			return nil, err
		}
		for vin, output := range outputs {
			if output != nil {
				if output.Beef != nil {
					if err := beef.MergeBeef(output.Beef); err != nil {
						return nil, fmt.Errorf("failed to merge BEEF for input %d: %w", vin, err)
					}
				}
				previousCoins = append(previousCoins, uint32(vin))
			}
		}

		txid := tx.TxID()
		admit, _ := s.IdentifyAdmissibleOutputs(ctx, beef, txid, previousCoins)
		if !slices.Contains(admit.OutputsToAdmit, gaspTx.OutputIndex) {
			neededInputs, err := s.IdentifyNeededInputs(ctx, beef, txid)
			if err != nil {
				return nil, err
			}
			for _, outpoint := range neededInputs {
				response.RequestedInputs[*outpoint] = &gasp.NodeResponseData{
					Metadata: true,
				}
			}
			return s.stripAlreadyKnowInputs(ctx, response)
		}
	}

	return response, nil
}

// IdentifyAdmissibleOutputs delegates to the topic manager to determine which outputs are admissible.
func (s *OverlayGASPStorage) IdentifyAdmissibleOutputs(ctx context.Context, beef *transaction.Beef, txid *chainhash.Hash, previousCoins []uint32) (overlay.AdmittanceInstructions, error) {
	manager, ok := s.Engine.GetTopicManager(s.Topic)
	if !ok {
		return overlay.AdmittanceInstructions{}, fmt.Errorf("%w (identify admissible outputs): %s", ErrNoManagerForTopic, s.Topic)
	}
	return manager.IdentifyAdmissibleOutputs(ctx, beef, txid, previousCoins)
}

// IdentifyNeededInputs delegates to the topic manager to determine which inputs are needed.
func (s *OverlayGASPStorage) IdentifyNeededInputs(ctx context.Context, beef *transaction.Beef, txid *chainhash.Hash) ([]*transaction.Outpoint, error) {
	manager, ok := s.Engine.GetTopicManager(s.Topic)
	if !ok {
		return nil, fmt.Errorf("%w (identify needed inputs): %s", ErrNoManagerForTopic, s.Topic)
	}
	return manager.IdentifyNeededInputs(ctx, beef, txid)
}

func (s *OverlayGASPStorage) stripAlreadyKnowInputs(ctx context.Context, response *gasp.NodeResponse) (*gasp.NodeResponse, error) {
	for outpoint := range response.RequestedInputs {
		if found, err := s.Engine.Storage.FindOutput(ctx, &outpoint, &s.Topic, nil, false); err != nil {
			return nil, err
		} else if found != nil {
			delete(response.RequestedInputs, outpoint)
		}
	}
	return response, nil
}

// AppendToGraph adds a GASP node to its traversal's graph for later validation and finalization.
func (s *OverlayGASPStorage) AppendToGraph(_ context.Context, gaspTx *gasp.Node, spentBy *transaction.Outpoint) error {
	if gaspTx.GraphID == nil {
		return ErrMissingGraphID
	}
	graphAny, _ := s.graphs.LoadOrStore(*gaspTx.GraphID, &graphContext{})
	graph := graphAny.(*graphContext)
	if s.MaxNodesInGraph != nil && graph.count.Load() >= int64(*s.MaxNodesInGraph) {
		return ErrGraphFull
	}

	tx, err := transaction.NewTransactionFromBytes(gaspTx.RawTx)
	if err != nil {
		return err
	}
	txid := tx.TxID()
	if len(gaspTx.Proof) > 0 {
		if tx.MerklePath, err = transaction.NewMerklePathFromBinary(gaspTx.Proof); err != nil {
			s.log().Error("Failed to parse merkle path", "error", err, "proofLength", len(gaspTx.Proof))
			return err
		}
	}
	newGraphNode := &GraphNode{
		Node: *gaspTx,
		Txid: txid,
	}
	// Compute the actual outpoint from the returned transaction
	newGraphOutpoint := &transaction.Outpoint{
		Txid:  *txid,
		Index: gaspTx.OutputIndex,
	}

	nodeAny, loaded := graph.nodes.LoadOrStore(*newGraphOutpoint, newGraphNode)
	if !loaded {
		graph.count.Add(1)
	}
	node := nodeAny.(*GraphNode)

	// If this node has a parent, link them together
	if spentBy != nil {
		parentNode, ok := graph.nodes.Load(*spentBy)
		if !ok {
			return ErrMissingInput
		}
		parent := parentNode.(*GraphNode)
		parent.Children.Store(*newGraphOutpoint, node)
		node.Parent = parent
	}
	return nil
}

// ValidateGraphAnchor verifies that the graph anchor transaction is valid and results in topical admittance.
// The check is self-contained: it walks the graph's transactions oldest-first, and outputs admitted earlier
// in the pass count as previous coins for later transactions, mirroring what submission will produce.
func (s *OverlayGASPStorage) ValidateGraphAnchor(ctx context.Context, graphID *transaction.Outpoint) error {
	graph, ok := s.loadGraph(graphID)
	if !ok {
		return ErrMissingInput
	}
	if rootNode, ok := graph.nodes.Load(*graphID); !ok {
		return ErrMissingInput
	} else if beef, err := s.getBEEFForNode(graph, rootNode.(*GraphNode)); err != nil {
		return err
	} else if tx, err := transaction.NewTransactionFromBEEF(beef); err != nil {
		return err
	} else if valid, err := spv.Verify(ctx, tx, s.Engine.ChainTracker, nil); err != nil {
		return err
	} else if !valid {
		return ErrGraphAnchorInvalidTx
	}
	beefs, beefsErr := s.computeOrderedBEEFsForGraph(ctx, graph)
	if beefsErr != nil {
		return beefsErr
	}
	coins := make(map[transaction.Outpoint]struct{})
	graphBeefs := make(map[chainhash.Hash][]byte)
	for _, beefBytes := range beefs {
		beef, tx, txid, err := transaction.ParseBeef(beefBytes)
		if err != nil {
			return err
		}
		inpoints := make([]*transaction.Outpoint, len(tx.Inputs))
		for vin, input := range tx.Inputs {
			inpoints[vin] = &transaction.Outpoint{
				Txid:  *input.SourceTXID,
				Index: input.SourceTxOutIndex,
			}
		}
		previousCoins := make([]uint32, 0, len(tx.Inputs))
		outputs, err := s.Engine.Storage.FindOutputs(ctx, inpoints, s.Topic, nil, true)
		if err != nil {
			return err
		}
		for vin, output := range outputs {
			if output != nil {
				if output.Beef != nil {
					if mergeErr := beef.MergeBeef(output.Beef); mergeErr != nil {
						return fmt.Errorf("failed to merge BEEF for input %d: %w", vin, mergeErr)
					}
				}
				previousCoins = append(previousCoins, uint32(vin))
			} else if _, inGraph := coins[*inpoints[vin]]; inGraph {
				if ancestorBeef, ok := graphBeefs[inpoints[vin].Txid]; ok {
					if mergeErr := beef.MergeBeefBytes(ancestorBeef); mergeErr != nil {
						return fmt.Errorf("failed to merge graph BEEF for input %d: %w", vin, mergeErr)
					}
				}
				previousCoins = append(previousCoins, uint32(vin))
			}
		}
		admit, err := s.IdentifyAdmissibleOutputs(ctx, beef, txid, previousCoins)
		if err != nil {
			s.log().Error("[GASP] ValidateGraphAnchor failed to identify admissible outputs", "error", err)
			return err
		}
		for _, vout := range admit.OutputsToAdmit {
			outpoint := &transaction.Outpoint{
				Txid:  *txid,
				Index: vout,
			}
			coins[*outpoint] = struct{}{}
		}
		if len(admit.OutputsToAdmit) > 0 {
			graphBeefs[*txid] = beefBytes
		}
	}
	if _, ok := coins[*graphID]; !ok {
		return ErrGraphNoTopicalAdmittance
	}
	return nil
}

// loadGraph returns the graph context for a traversal root, if one exists.
func (s *OverlayGASPStorage) loadGraph(graphID *transaction.Outpoint) (*graphContext, bool) {
	if graphID == nil {
		return nil, false
	}
	graph, ok := s.graphs.Load(*graphID)
	if !ok {
		return nil, false
	}
	return graph.(*graphContext), true
}

// DiscardGraph drops the specified traversal's entire graph.
func (s *OverlayGASPStorage) DiscardGraph(_ context.Context, graphID *transaction.Outpoint) error {
	s.graphs.Delete(*graphID)
	return nil
}

// FinalizeGraph submits all transactions in the graph to the overlay engine for processing.
func (s *OverlayGASPStorage) FinalizeGraph(ctx context.Context, graphID *transaction.Outpoint) error {
	graph, ok := s.loadGraph(graphID)
	if !ok {
		return ErrUnableToFindRootNodeInGraph
	}
	beefs, err := s.computeOrderedBEEFsForGraph(ctx, graph)
	if err != nil {
		return err
	}
	for _, beef := range beefs {
		if err := s.submitBeef(ctx, beef); err != nil {
			return err
		}
	}
	return nil
}

// submitBeef submits a single BEEF to the engine, deduplicating concurrent
// submissions of the same transaction across graphs. The tracker entry lives
// only while the submission is in flight.
func (s *OverlayGASPStorage) submitBeef(ctx context.Context, beef []byte) error {
	_, tx, txid, err := transaction.ParseBeef(beef)
	if err != nil {
		return err
	}
	if tx == nil {
		return ErrNoTransactionInBEEF
	}

	// Pre-initialize the submission state to avoid race conditions
	newState := &submissionState{}
	newState.wg.Add(1)

	if existing, loaded := s.submissionTracker.LoadOrStore(*txid, newState); loaded {
		// Another goroutine is already submitting this transaction, wait for it
		state := existing.(*submissionState)
		state.wg.Wait()
		return state.err
	}

	state := newState
	defer func() {
		state.wg.Done()
		s.submissionTracker.Delete(*txid)
	}()

	_, state.err = s.Engine.Submit(
		ctx,
		overlay.TaggedBEEF{
			Topics: []string{s.Topic},
			Beef:   beef,
		},
		SubmitModeHistorical,
		nil,
	)
	if state.err != nil {
		s.log().Error("[GASP] Failed to submit transaction", "txid", txid.String(), "error", state.err)
		return state.err
	}
	s.log().Debug(fmt.Sprintf("[GASP] Transaction processed: %s", txid.String()))
	return nil
}

// computeOrderedBEEFsForGraph returns one BEEF per transaction in the graph, ordered so that
// every transaction appears after all of its in-graph inputs. Validation and submission both
// rely on this order: a transaction's inputs must be dealt with before the transaction itself.
func (s *OverlayGASPStorage) computeOrderedBEEFsForGraph(_ context.Context, graph *graphContext) ([][]byte, error) {
	nodes := make(map[chainhash.Hash]*GraphNode)
	graph.nodes.Range(func(_, value any) bool {
		node := value.(*GraphNode)
		nodes[*node.Txid] = node
		return true
	})
	if len(nodes) == 0 {
		return nil, ErrUnableToFindRootNodeInGraph
	}

	deps := make(map[chainhash.Hash]map[chainhash.Hash]struct{}, len(nodes))
	for txid, node := range nodes {
		tx, err := transaction.NewTransactionFromBytes(node.RawTx)
		if err != nil {
			return nil, err
		}
		txDeps := make(map[chainhash.Hash]struct{})
		for _, input := range tx.Inputs {
			if _, inGraph := nodes[*input.SourceTXID]; inGraph && *input.SourceTXID != txid {
				txDeps[*input.SourceTXID] = struct{}{}
			}
		}
		deps[txid] = txDeps
	}

	beefs := make([][]byte, 0, len(nodes))
	emitted := make(map[chainhash.Hash]struct{}, len(nodes))
	for len(emitted) < len(nodes) {
		progress := false
		for txid, txDeps := range deps {
			if _, done := emitted[txid]; done {
				continue
			}
			ready := true
			for dep := range txDeps {
				if _, done := emitted[dep]; !done {
					ready = false
					break
				}
			}
			if !ready {
				continue
			}
			beef, err := s.getBEEFForNode(graph, nodes[txid])
			if err != nil {
				return nil, err
			}
			beefs = append(beefs, beef)
			emitted[txid] = struct{}{}
			progress = true
		}
		if !progress {
			return nil, ErrGraphDependencyCycle
		}
	}
	return beefs, nil
}

func (s *OverlayGASPStorage) getBEEFForNode(graph *graphContext, node *GraphNode) ([]byte, error) {
	if node == nil {
		s.log().Error("getBEEFForNode called with nil node", "goroutines", runtime.NumGoroutine())
		return nil, ErrNilNode
	}

	var hydrator func(node *GraphNode) (*transaction.Transaction, error)
	hydrator = func(node *GraphNode) (*transaction.Transaction, error) {
		if node == nil {
			s.log().Error("hydrator called with nil node", "goroutines", runtime.NumGoroutine())
			return nil, ErrNilNode
		}
		tx, err := transaction.NewTransactionFromBytes(node.RawTx)
		if err != nil {
			return nil, err
		}
		if len(node.Proof) > 0 {
			if tx.MerklePath, err = transaction.NewMerklePathFromBinary(node.Proof); err != nil {
				return nil, err
			}
			return tx, nil
		}
		for vin, input := range tx.Inputs {
			outpoint := &transaction.Outpoint{
				Txid:  *input.SourceTXID,
				Index: input.SourceTxOutIndex,
			}
			foundNode, ok := graph.nodes.Load(*outpoint)
			if !ok {
				return nil, ErrRequiredInputNodeNotFoundInTempGraph
			}
			if tx.Inputs[vin].SourceTransaction, err = hydrator(foundNode.(*GraphNode)); err != nil {
				return nil, err
			}
		}
		return tx, nil
	}
	tx, err := hydrator(node)
	if err != nil {
		return nil, err
	}
	beef, err := transaction.NewBeefFromTransaction(tx)
	if err != nil {
		return nil, err
	}
	return beef.AtomicBytes(tx.TxID())
}
