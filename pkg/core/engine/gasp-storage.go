package engine

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"sync"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/gasp"
	"github.com/bsv-blockchain/go-sdk/chainhash"
	"github.com/bsv-blockchain/go-sdk/overlay"
	"github.com/bsv-blockchain/go-sdk/spv"
	"github.com/bsv-blockchain/go-sdk/transaction"
)

var (
	// ErrGraphFull indicates the graph has reached its maximum size
	ErrGraphFull = errors.New("graph is full")

	// ErrParsedBEEFReturnedNilTx indicates that parsing BEEF returned a nil transaction
	ErrParsedBEEFReturnedNilTx = errors.New("parsed BEEF returned nil transaction")

	// ErrGraphAnchorInvalidTx indicates that the graph anchor is not a valid transaction
	ErrGraphAnchorInvalidTx = errors.New("graph anchor is not a valid transaction")

	// ErrGraphNoTopicalAdmittance indicates that the graph did not result in topical admittance of the root node
	ErrGraphNoTopicalAdmittance = errors.New("graph did not result in topical admittance of the root node. rejecting")
	// ErrUnableToFindRootNodeInGraph indicates that the root node could not be found in the graph for finalization
	ErrUnableToFindRootNodeInGraph = errors.New("unable to find root node in graph for finalization")
	// ErrRequiredInputNodeNotFoundInTempGraph indicates that a required input node was not found in the temporary graph store
	ErrRequiredInputNodeNotFoundInTempGraph = errors.New("required input node for unproven parent not found in temporary graph store")
)

// GraphNode represents a node in the GASP graph
type GraphNode struct {
	gasp.Node

	Txid     *chainhash.Hash `json:"txid"`
	SpentBy  *chainhash.Hash `json:"spentBy"`
	Children []*GraphNode    `json:"children"`
	Parent   *GraphNode      `json:"parent"`
}

// OverlayGASPStorage implements GASP storage using the overlay engine
type OverlayGASPStorage struct {
	Topic              string
	Engine             *Engine
	MaxNodesInGraph    *int
	tempGraphNodeRefs  sync.Map
	tempGraphNodeCount int
}

// NewOverlayGASPStorage creates a new OverlayGASPStorage instance
func NewOverlayGASPStorage(topic string, engine *Engine, maxNodesInGraph *int) *OverlayGASPStorage {
	return &OverlayGASPStorage{
		Topic:           topic,
		Engine:          engine,
		MaxNodesInGraph: maxNodesInGraph,
	}
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

// HydrateGASPNode hydrates a GASP node from storage
func (s *OverlayGASPStorage) HydrateGASPNode(ctx context.Context, graphID, outpoint *transaction.Outpoint, _ bool) (*gasp.Node, error) {
	output, err := s.Engine.Storage.FindOutput(ctx, outpoint, nil, nil, true)
	if err != nil {
		return nil, err
	}
	if output == nil || output.Beef == nil {
		return nil, ErrMissingInput
	}
	// Parse BEEF to get the transaction
	_, tx, _, err := transaction.ParseBeef(output.Beef)
	if err != nil {
		return nil, err
	}

	// Check if we got a valid transaction
	if tx == nil {
		return nil, ErrParsedBEEFReturnedNilTx
	}

	node := &gasp.Node{
		GraphID:     graphID,
		OutputIndex: outpoint.Index,
		RawTx:       tx.Hex(),
	}
	if tx.MerklePath != nil {
		proof := tx.MerklePath.Hex()
		node.Proof = &proof
	}
	return node, nil
}

// ErrNoNeededInputs is returned when no inputs are needed
var ErrNoNeededInputs = errors.New("no needed inputs")

// FindNeededInputs determines which inputs are needed for a GASP transaction
func (s *OverlayGASPStorage) FindNeededInputs(ctx context.Context, gaspTx *gasp.Node) (*gasp.NodeResponse, error) {
	response := &gasp.NodeResponse{
		RequestedInputs: make(map[string]*gasp.NodeResponseData),
	}
	tx, err := transaction.NewTransactionFromHex(gaspTx.RawTx)
	if err != nil {
		return nil, err
	}
	if gaspTx.Proof == nil {
		for _, input := range tx.Inputs {
			outpoint := &transaction.Outpoint{
				Txid:  *input.SourceTXID,
				Index: input.SourceTxOutIndex,
			}
			response.RequestedInputs[outpoint.String()] = &gasp.NodeResponseData{
				Metadata: false,
			}
		}

		return s.stripAlreadyKnowInputs(ctx, response)
	}
	tx.MerklePath, err = transaction.NewMerklePathFromHex(*gaspTx.Proof)
	if err != nil {
		return nil, err
	}
	beef, err := transaction.NewBeefFromTransaction(tx)
	if err != nil {
		return nil, err
	}
	if len(gaspTx.AncillaryBeef) > 0 {
		if mergeErr := beef.MergeBeefBytes(gaspTx.AncillaryBeef); mergeErr != nil {
			return nil, mergeErr
		}
	}
	inpoints := make([]*transaction.Outpoint, len(tx.Inputs))
	for vin, input := range tx.Inputs {
		inpoints[vin] = &transaction.Outpoint{
			Txid:  *input.SourceTXID,
			Index: input.SourceTxOutIndex,
		}
	}
	previousCoins := make(map[uint32]*transaction.TransactionOutput, len(tx.Inputs))
	outputs, err := s.Engine.Storage.FindOutputs(ctx, inpoints, s.Topic, nil, false)
	if err != nil {
		return nil, err
	}
	for vin, output := range outputs {
		if output != nil {
			previousCoins[uint32(vin)] = &transaction.TransactionOutput{ // #nosec G115
				LockingScript: output.Script,
				Satoshis:      output.Satoshis,
			}
		}
	}

	beefBytes, err := beef.AtomicBytes(tx.TxID())
	if err != nil {
		return nil, err
	}
	admit, err := s.Engine.Managers[s.Topic].IdentifyAdmissibleOutputs(ctx, beefBytes, previousCoins)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(admit.OutputsToAdmit, gaspTx.OutputIndex) {
		neededInputs, err := s.Engine.Managers[s.Topic].IdentifyNeededInputs(ctx, beefBytes)
		if err != nil {
			return nil, err
		}
		for _, outpoint := range neededInputs {
			response.RequestedInputs[outpoint.String()] = &gasp.NodeResponseData{
				Metadata: true,
			}
		}
		return s.stripAlreadyKnowInputs(ctx, response)
	}

	return response, nil
}

// ErrNoInputsToStrip is returned when there are no inputs to strip
var ErrNoInputsToStrip = errors.New("no inputs to strip")

func (s *OverlayGASPStorage) stripAlreadyKnowInputs(ctx context.Context, response *gasp.NodeResponse) (*gasp.NodeResponse, error) {
	if response == nil {
		return nil, ErrNoInputsToStrip
	}
	for outpointStr := range response.RequestedInputs {
		outpoint, err := transaction.OutpointFromString(outpointStr)
		if err != nil {
			return nil, err
		}
		found, err := s.Engine.Storage.FindOutput(ctx, outpoint, &s.Topic, nil, false)
		if err != nil {
			return nil, err
		}
		if found != nil {
			delete(response.RequestedInputs, outpointStr)
		}
	}
	if len(response.RequestedInputs) == 0 {
		return nil, ErrNoInputsToStrip
	}
	return response, nil
}

// AppendToGraph adds a GASP node to the temporary graph store for later validation and finalization.
func (s *OverlayGASPStorage) AppendToGraph(_ context.Context, gaspTx *gasp.Node, spentBy *transaction.Outpoint) error {
	if s.MaxNodesInGraph != nil && s.tempGraphNodeCount >= *s.MaxNodesInGraph {
		return ErrGraphFull
	}

	tx, err := transaction.NewTransactionFromHex(gaspTx.RawTx)
	if err != nil {
		return err
	}
	txid := tx.TxID()
	if gaspTx.Proof != nil {
		tx.MerklePath, err = transaction.NewMerklePathFromHex(*gaspTx.Proof)
		if err != nil {
			return err
		}
	}
	newGraphNode := &GraphNode{
		Node:     *gaspTx,
		Txid:     txid,
		Children: []*GraphNode{},
	}
	if spentBy == nil {
		if _, ok := s.tempGraphNodeRefs.LoadOrStore(gaspTx.GraphID.String(), newGraphNode); !ok {
			s.tempGraphNodeCount++
		}
	} else {
		// Find parent node by spentBy outpoint
		parentNode, ok := s.tempGraphNodeRefs.Load(spentBy.String())
		if !ok {
			return ErrMissingInput
		}
		parentNode.(*GraphNode).Children = append(parentNode.(*GraphNode).Children, newGraphNode)
		newGraphNode.Parent = parentNode.(*GraphNode)
		newGraphOutpoint := &transaction.Outpoint{
			Txid:  *txid,
			Index: gaspTx.OutputIndex,
		}
		if _, ok := s.tempGraphNodeRefs.LoadOrStore(newGraphOutpoint.String(), newGraphNode); !ok {
			s.tempGraphNodeCount++
		}
	}
	return nil
}

// ValidateGraphAnchor verifies that the graph anchor transaction is valid and results in topical admittance.
func (s *OverlayGASPStorage) ValidateGraphAnchor(ctx context.Context, graphID *transaction.Outpoint) error {
	if rootNode, ok := s.tempGraphNodeRefs.Load(graphID.String()); !ok {
		return ErrMissingInput
	} else if beef, err := s.getBEEFForNode(rootNode.(*GraphNode)); err != nil {
		return err
	} else if tx, err := transaction.NewTransactionFromBEEF(beef); err != nil {
		return err
	} else if valid, err := spv.Verify(ctx, tx, s.Engine.ChainTracker, nil); err != nil {
		return err
	} else if !valid {
		return ErrGraphAnchorInvalidTx
	}
	beefs, beefsErr := s.computeOrderedBEEFsForGraph(ctx, graphID)
	if beefsErr != nil {
		return beefsErr
	}
	coins := make(map[string]struct{})
	for _, beefBytes := range beefs {
		tx, err := transaction.NewTransactionFromBEEF(beefBytes)
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
		previousCoins := make(map[uint32]*transaction.TransactionOutput, len(tx.Inputs))
		outputs, findErr := s.Engine.Storage.FindOutputs(ctx, inpoints, s.Topic, nil, false)
		if findErr != nil {
			return findErr
		}
		for vin, output := range outputs {
			if output != nil {
				previousCoins[uint32(vin)] = &transaction.TransactionOutput{ // #nosec G115
					LockingScript: output.Script,
					Satoshis:      output.Satoshis,
				}
			}
		}
		admit, admitErr := s.Engine.Managers[s.Topic].IdentifyAdmissibleOutputs(ctx, beefBytes, previousCoins)
		if admitErr != nil {
			return admitErr
		}
		for _, vout := range admit.OutputsToAdmit {
			outpoint := &transaction.Outpoint{
				Txid:  *tx.TxID(),
				Index: vout,
			}
			coins[outpoint.String()] = struct{}{}
		}
	}
	if _, ok := coins[graphID.String()]; !ok {
		return ErrGraphNoTopicalAdmittance
	}
	return nil
}

// DiscardGraph removes all nodes associated with the specified graph from the temporary storage.
func (s *OverlayGASPStorage) DiscardGraph(_ context.Context, graphID *transaction.Outpoint) error {
	// First, find all nodes that belong to this graph
	nodesToDelete := make([]string, 0)
	s.tempGraphNodeRefs.Range(func(nodeId, graphRef any) bool {
		node := graphRef.(*GraphNode)
		if node.GraphID.Equal(graphID) {
			// Recursively collect all child nodes
			collectNodes := func(n *GraphNode) {
				nodesToDelete = append(nodesToDelete, nodeId.(string))
				for _, child := range n.Children {
					outpoint := &transaction.Outpoint{
						Txid:  *child.Txid,
						Index: child.OutputIndex,
					}
					nodesToDelete = append(nodesToDelete, outpoint.String())
				}
			}
			collectNodes(node)
		}
		return true
	})

	// Delete all collected nodes
	for _, nodeID := range nodesToDelete {
		s.tempGraphNodeRefs.Delete(nodeID)
		s.tempGraphNodeCount--
	}

	return nil
}

// FinalizeGraph submits all transactions in the graph to the overlay engine for processing.
func (s *OverlayGASPStorage) FinalizeGraph(ctx context.Context, graphID *transaction.Outpoint) error {
	beefs, err := s.computeOrderedBEEFsForGraph(ctx, graphID)
	if err != nil {
		return err
	}
	for _, beef := range beefs {
		if _, err := s.Engine.Submit(
			ctx,
			overlay.TaggedBEEF{
				Topics: []string{s.Topic},
				Beef:   beef,
			},
			SubmitModeHistorical,
			nil,
		); err != nil {
			return err
		}
	}
	return nil
}

func (s *OverlayGASPStorage) computeOrderedBEEFsForGraph(_ context.Context, graphID *transaction.Outpoint) ([][]byte, error) {
	beefs := make([][]byte, 0)
	var hydrator func(node *GraphNode) error
	hydrator = func(node *GraphNode) error {
		currentBeef, err := s.getBEEFForNode(node)
		if err != nil {
			return err
		}
		if slices.IndexFunc(beefs, func(beef []byte) bool {
			return bytes.Equal(beef, currentBeef)
		}) == -1 {
			beefs = append([][]byte{currentBeef}, beefs...)
		}
		for _, child := range node.Children {
			if err := hydrator(child); err != nil {
				return err
			}
		}
		return nil
	}

	foundRoot, ok := s.tempGraphNodeRefs.Load(graphID.String())
	if !ok {
		return nil, ErrUnableToFindRootNodeInGraph
	}
	if err := hydrator(foundRoot.(*GraphNode)); err != nil {
		return nil, err
	}
	return beefs, nil
}

func (s *OverlayGASPStorage) getBEEFForNode(node *GraphNode) ([]byte, error) {
	var hydrator func(node *GraphNode) (*transaction.Transaction, error)
	hydrator = func(node *GraphNode) (*transaction.Transaction, error) {
		tx, err := transaction.NewTransactionFromHex(node.RawTx)
		if err != nil {
			return nil, err
		}
		if node.Proof != nil {
			if tx.MerklePath, err = transaction.NewMerklePathFromHex(*node.Proof); err != nil {
				return nil, err
			}
			return tx, nil
		}
		for vin, input := range tx.Inputs {
			outpoint := &transaction.Outpoint{
				Txid:  *input.SourceTXID,
				Index: input.SourceTxOutIndex,
			}
			foundNode, ok := s.tempGraphNodeRefs.Load(outpoint.String())
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
	if len(node.AncillaryBeef) > 0 {
		if err := beef.MergeBeefBytes(node.AncillaryBeef); err != nil {
			return nil, err
		}
	}
	return beef.AtomicBytes(tx.TxID())
}
