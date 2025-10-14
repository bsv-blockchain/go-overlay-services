package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"slices"
	"sync"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/gasp"
	"github.com/bsv-blockchain/go-sdk/chainhash"
	"github.com/bsv-blockchain/go-sdk/overlay"
	"github.com/bsv-blockchain/go-sdk/spv"
	"github.com/bsv-blockchain/go-sdk/transaction"
)

var ErrGraphFull = errors.New("graph is full")

// submissionState tracks the state of a transaction submission
type submissionState struct {
	wg  sync.WaitGroup
	err error
}

type GraphNode struct {
	gasp.Node
	Txid     *chainhash.Hash `json:"txid"`
	SpentBy  *chainhash.Hash `json:"spentBy"`
	Children sync.Map        `json:"-"` // map[string]*GraphNode - concurrent safe
	Parent   *GraphNode      `json:"parent"`
}

type OverlayGASPStorage struct {
	Topic              string
	Engine             *Engine
	MaxNodesInGraph    *int
	tempGraphNodeRefs  sync.Map
	tempGraphNodeCount int
	submissionTracker  sync.Map // map[chainhash.Hash]*submissionState
}

func NewOverlayGASPStorage(topic string, engine *Engine, maxNodesInGraph *int) *OverlayGASPStorage {
	return &OverlayGASPStorage{
		Topic:           topic,
		Engine:          engine,
		MaxNodesInGraph: maxNodesInGraph,
	}
}

func (s *OverlayGASPStorage) FindKnownUTXOs(ctx context.Context, since float64, limit uint32) ([]*gasp.Output, error) {
	if utxos, err := s.Engine.Storage.FindUTXOsForTopic(ctx, s.Topic, since, limit, false); err != nil {
		return nil, err
	} else {
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
}

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

func (s *OverlayGASPStorage) HydrateGASPNode(ctx context.Context, graphID *transaction.Outpoint, outpoint *transaction.Outpoint, metadata bool) (*gasp.Node, error) {
	if output, err := s.Engine.Storage.FindOutput(ctx, outpoint, nil, nil, true); err != nil {
		return nil, err
	} else if output == nil || output.Beef == nil {
		return nil, ErrMissingInput
	} else {
		// Parse BEEF to get the transaction
		_, tx, _, err := transaction.ParseBeef(output.Beef)
		if err != nil {
			return nil, err
		}

		// Check if we got a valid transaction
		if tx == nil {
			return nil, errors.New("parsed BEEF returned nil transaction")
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
}

func (s *OverlayGASPStorage) FindNeededInputs(ctx context.Context, gaspTx *gasp.Node) (*gasp.NodeResponse, error) {
	response := &gasp.NodeResponse{
		RequestedInputs: make(map[transaction.Outpoint]*gasp.NodeResponseData),
	}
	tx, err := transaction.NewTransactionFromHex(gaspTx.RawTx)
	if err != nil {
		return nil, err
	}
	// Commented out: This was requesting ALL inputs for unmined transactions
	// but should use IdentifyNeededInputs to get only relevant inputs
	if gaspTx.Proof == nil || *gaspTx.Proof == "" {
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
	if gaspTx.Proof != nil && *gaspTx.Proof != "" {
		if tx.MerklePath, err = transaction.NewMerklePathFromHex(*gaspTx.Proof); err != nil {
			return nil, err
		}
	}

	var beef *transaction.Beef
	if len(gaspTx.AncillaryBeef) > 0 {
		// If we have ancillary BEEF, use it as the base (contains full transaction graph)
		if beef, _, _, err = transaction.ParseBeef(gaspTx.AncillaryBeef); err != nil {
			return nil, err
		}
		// Merge in the transaction we just received
		if _, err = beef.MergeTransaction(tx); err != nil {
			return nil, err
		}
	} else if tx.MerklePath != nil {
		// If we have a merkle path but no ancillary BEEF, create BEEF from transaction
		if beef, err = transaction.NewBeefFromTransaction(tx); err != nil {
			return nil, err
		}
	} /* else {
		// Unmined transaction without ancillary BEEF is an error
		return nil, fmt.Errorf("unmined transaction without ancillary BEEF")
	}*/

	if beef != nil {
		inpoints := make([]*transaction.Outpoint, len(tx.Inputs))
		for vin, input := range tx.Inputs {
			inpoints[vin] = &transaction.Outpoint{
				Txid:  *input.SourceTXID,
				Index: input.SourceTxOutIndex,
			}
		}
		previousCoins := make(map[uint32]*transaction.TransactionOutput, len(tx.Inputs))
		if outputs, err := s.Engine.Storage.FindOutputs(ctx, inpoints, s.Topic, nil, false); err != nil {
			return nil, err
		} else {
			for vin, output := range outputs {
				if output != nil {
					previousCoins[uint32(vin)] = &transaction.TransactionOutput{
						LockingScript: output.Script,
						Satoshis:      output.Satoshis,
					}
				}
			}
		}

		if beefBytes, err := beef.AtomicBytes(tx.TxID()); err != nil {
			return nil, err
		} else if admit, err := s.IdentifyAdmissibleOutputs(ctx, beefBytes, previousCoins); err != nil {
			return nil, err
		} else if !slices.Contains(admit.OutputsToAdmit, gaspTx.OutputIndex) {
			if _, ok := s.Engine.Managers[s.Topic]; !ok {
				return nil, errors.New("no manager for topic (identify needed inputs): " + s.Topic)
			} else if neededInputs, err := s.Engine.Managers[s.Topic].IdentifyNeededInputs(ctx, beefBytes); err != nil {
				return nil, err
			} else {
				for _, outpoint := range neededInputs {
					response.RequestedInputs[*outpoint] = &gasp.NodeResponseData{
						Metadata: true,
					}
				}
				return s.stripAlreadyKnowInputs(ctx, response)
			}
		}
	}

	return response, nil
}

func (s *OverlayGASPStorage) IdentifyAdmissibleOutputs(ctx context.Context, beefBytes []byte, previousCoins map[uint32]*transaction.TransactionOutput) (overlay.AdmittanceInstructions, error) {
	if _, ok := s.Engine.Managers[s.Topic]; !ok {
		return overlay.AdmittanceInstructions{}, errors.New("no manager for topic (identify admissible outputs): " + s.Topic)
	}
	return s.Engine.Managers[s.Topic].IdentifyAdmissibleOutputs(ctx, beefBytes, previousCoins)
}

func (s *OverlayGASPStorage) stripAlreadyKnowInputs(ctx context.Context, response *gasp.NodeResponse) (*gasp.NodeResponse, error) {
	if response == nil {
		return nil, nil
	}
	for outpoint := range response.RequestedInputs {
		if found, err := s.Engine.Storage.FindOutput(ctx, &outpoint, &s.Topic, nil, false); err != nil {
			return nil, err
		} else if found != nil {
			delete(response.RequestedInputs, outpoint)
		}
	}
	if len(response.RequestedInputs) == 0 {
		return nil, nil
	}
	return response, nil
}

func (s *OverlayGASPStorage) AppendToGraph(ctx context.Context, gaspTx *gasp.Node, spentBy *transaction.Outpoint) error {
	if s.MaxNodesInGraph != nil && s.tempGraphNodeCount >= *s.MaxNodesInGraph {
		return ErrGraphFull
	}

	if tx, err := transaction.NewTransactionFromHex(gaspTx.RawTx); err != nil {
		return err
	} else {
		txid := tx.TxID()
		if gaspTx.Proof != nil && *gaspTx.Proof != "" {
			if tx.MerklePath, err = transaction.NewMerklePathFromHex(*gaspTx.Proof); err != nil {
				slog.Error("Failed to parse merkle path", "error", err, "proofLength", len(*gaspTx.Proof))
				return err
			}
		}
		newGraphNode := &GraphNode{
			Node: *gaspTx,
			Txid: txid,
		}
		if spentBy == nil {
			if _, ok := s.tempGraphNodeRefs.LoadOrStore(*gaspTx.GraphID, newGraphNode); !ok {
				s.tempGraphNodeCount++
			}
		} else {
			// Find parent node by spentBy outpoint
			if parentNode, ok := s.tempGraphNodeRefs.Load(*spentBy); !ok {
				return ErrMissingInput
			} else {
				parent := parentNode.(*GraphNode)
				parent.Children.Store(*gaspTx.GraphID, newGraphNode)
				newGraphNode.Parent = parentNode.(*GraphNode)
			}
			newGraphOutpoint := &transaction.Outpoint{
				Txid:  *txid,
				Index: gaspTx.OutputIndex,
			}
			if _, ok := s.tempGraphNodeRefs.LoadOrStore(*newGraphOutpoint, newGraphNode); !ok {
				s.tempGraphNodeCount++
			}
		}
		return nil
	}
}

func (s *OverlayGASPStorage) ValidateGraphAnchor(ctx context.Context, graphID *transaction.Outpoint) error {
	if rootNode, ok := s.tempGraphNodeRefs.Load(*graphID); !ok {
		return ErrMissingInput
	} else if beef, err := s.getBEEFForNode(rootNode.(*GraphNode)); err != nil {
		return err
	} else if tx, err := transaction.NewTransactionFromBEEF(beef); err != nil {
		return err
	} else if valid, err := spv.Verify(ctx, tx, s.Engine.ChainTracker, nil); err != nil {
		return err
	} else if !valid {
		return errors.New("graph anchor is not a valid transaction")
	} else if beefs, err := s.computeOrderedBEEFsForGraph(ctx, graphID); err != nil {
		return err
	} else {
		coins := make(map[transaction.Outpoint]struct{})
		for _, beefBytes := range beefs {
			if tx, err := transaction.NewTransactionFromBEEF(beefBytes); err != nil {
				return err
			} else {
				inpoints := make([]*transaction.Outpoint, len(tx.Inputs))
				for vin, input := range tx.Inputs {
					inpoints[vin] = &transaction.Outpoint{
						Txid:  *input.SourceTXID,
						Index: input.SourceTxOutIndex,
					}
				}
				previousCoins := make(map[uint32]*transaction.TransactionOutput, len(tx.Inputs))
				if outputs, err := s.Engine.Storage.FindOutputs(ctx, inpoints, s.Topic, nil, false); err != nil {
					return err
				} else {
					for vin, output := range outputs {
						if output != nil {
							previousCoins[uint32(vin)] = &transaction.TransactionOutput{
								LockingScript: output.Script,
								Satoshis:      output.Satoshis,
							}
						}
					}
				}
				if admit, err := s.IdentifyAdmissibleOutputs(ctx, beefBytes, previousCoins); err != nil {
					return err
				} else {
					for _, vout := range admit.OutputsToAdmit {
						outpoint := &transaction.Outpoint{
							Txid:  *tx.TxID(),
							Index: vout,
						}
						coins[*outpoint] = struct{}{}
					}
				}
			}
		}
		if _, ok := coins[*graphID]; !ok {
			return errors.New("graph did not result in topical admittance of the root node. rejecting")
		}
		return nil
	}
}

func (s *OverlayGASPStorage) DiscardGraph(_ context.Context, graphID *transaction.Outpoint) error {
	// Find and delete all nodes that belong to this graph
	nodesToDelete := make([]*transaction.Outpoint, 0)

	// First pass: collect all node IDs that belong to this graph
	s.tempGraphNodeRefs.Range(func(nodeId, graphRef any) bool {
		node := graphRef.(*GraphNode)
		if node.GraphID.Equal(graphID) {
			outpoint := nodeId.(transaction.Outpoint)
			nodesToDelete = append(nodesToDelete, &outpoint)
		}
		return true
	})

	// Delete all collected nodes
	for _, nodeId := range nodesToDelete {
		s.tempGraphNodeRefs.Delete(*nodeId)
		s.tempGraphNodeCount--
	}

	return nil
}

func (s *OverlayGASPStorage) FinalizeGraph(ctx context.Context, graphID *transaction.Outpoint) error {
	if beefs, err := s.computeOrderedBEEFsForGraph(ctx, graphID); err != nil {
		return err
	} else {
		for _, beef := range beefs {
			// Extract transaction ID from BEEF for deduplication key
			_, tx, _, err := transaction.ParseBeef(beef)
			if err != nil {
				return err
			}
			if tx == nil {
				return errors.New("no transaction in BEEF")
			}

			txid := *tx.TxID()

			// Deduplicate submissions by transaction ID

			// Pre-initialize the submission state to avoid race conditions
			newState := &submissionState{}
			newState.wg.Add(1)

			if existing, loaded := s.submissionTracker.LoadOrStore(txid, newState); loaded {
				// Another goroutine is already submitting this transaction, wait for it
				state := existing.(*submissionState)
				state.wg.Wait()
				if state.err != nil {
					return state.err
				}
			} else {
				// We're the first caller, do the submission using our pre-initialized state
				state := newState
				defer state.wg.Done() // Signal completion

				// Perform the actual submission
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
					return state.err
				}
				slog.Info(fmt.Sprintf("[GASP] Transaction processed: %s", txid.String()))
			}
		}
		return nil
	}
}

func (s *OverlayGASPStorage) computeOrderedBEEFsForGraph(ctx context.Context, graphID *transaction.Outpoint) ([][]byte, error) {
	beefs := make([][]byte, 0)
	var hydrator func(node *GraphNode) error
	hydrator = func(node *GraphNode) error {
		if currentBeef, err := s.getBEEFForNode(node); err != nil {
			return err
		} else {
			if slices.IndexFunc(beefs, func(beef []byte) bool {
				return bytes.Equal(beef, currentBeef)
			}) == -1 {
				beefs = append([][]byte{currentBeef}, beefs...)
			}
			var childErr error
			node.Children.Range(func(key, value any) bool {
				child := value.(*GraphNode)
				if err := hydrator(child); err != nil {
					childErr = err
					return false
				}
				return true
			})
			if childErr != nil {
				return childErr
			}
		}
		return nil
	}

	if foundRoot, ok := s.tempGraphNodeRefs.Load(*graphID); !ok {
		return nil, errors.New("unable to find root node in graph for finalization")
	} else if err := hydrator(foundRoot.(*GraphNode)); err != nil {
		return nil, err
	} else {
		return beefs, nil
	}
}

func (s *OverlayGASPStorage) getBEEFForNode(node *GraphNode) ([]byte, error) {
	if node == nil {
		panic(fmt.Sprintf("GASP DEBUG: getBEEFForNode called with nil node. Total goroutines: %d", runtime.NumGoroutine()))
	}

	// For unmined transactions (no proof), if ancillaryBeef is provided, use it directly
	// as it contains the complete BEEF for the unmined transaction
	if (node.Proof == nil || *node.Proof == "") && len(node.AncillaryBeef) > 0 {
		// slog.Info("Using ancillaryBeef directly for unmined transaction", "beefSize", len(node.AncillaryBeef))
		return node.AncillaryBeef, nil
	}

	var hydrator func(node *GraphNode) (*transaction.Transaction, error)
	hydrator = func(node *GraphNode) (*transaction.Transaction, error) {
		if node == nil {
			panic(fmt.Sprintf("GASP DEBUG: hydrator called with nil node. Goroutine: %d", runtime.NumGoroutine()))
		}
		if tx, err := transaction.NewTransactionFromHex(node.RawTx); err != nil {
			return nil, err
		} else if node.Proof != nil && *node.Proof != "" {
			if tx.MerklePath, err = transaction.NewMerklePathFromHex(*node.Proof); err != nil {
				return nil, err
			}
			return tx, nil
		} else {
			for vin, input := range tx.Inputs {
				outpoint := &transaction.Outpoint{
					Txid:  *input.SourceTXID,
					Index: input.SourceTxOutIndex,
				}
				if foundNode, ok := s.tempGraphNodeRefs.Load(*outpoint); !ok {
					return nil, errors.New("required input node for unproven parent not found in temporary graph store")
				} else if tx.Inputs[vin].SourceTransaction, err = hydrator(foundNode.(*GraphNode)); err != nil {
					return nil, err
				}
			}
			return tx, nil
		}
	}
	if tx, err := hydrator(node); err != nil {
		return nil, err
	} else if beef, err := transaction.NewBeefFromTransaction(tx); err != nil {
		return nil, err
	} else {
		if len(node.AncillaryBeef) > 0 {
			if err := beef.MergeBeefBytes(node.AncillaryBeef); err != nil {
				return nil, err
			}
		}
		return beef.AtomicBytes(tx.TxID())
	}
}
