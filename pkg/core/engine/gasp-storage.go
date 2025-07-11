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

var ErrGraphFull = errors.New("graph is full")

type GraphNode struct {
	gasp.Node
	Txid     *chainhash.Hash `json:"txid"`
	SpentBy  *chainhash.Hash `json:"spentBy"`
	Children []*GraphNode    `json:"children"`
	Parent   *GraphNode      `json:"parent"`
}

type OverlayGASPStorage struct {
	Topic              string
	Engine             *Engine
	MaxNodesInGraph    *int
	tempGraphNodeRefs  sync.Map
	tempGraphNodeCount int
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
	} else if tx.MerklePath, err = transaction.NewMerklePathFromHex(*gaspTx.Proof); err != nil {
		return nil, err
	}
	if beef, err := transaction.NewBeefFromTransaction(tx); err != nil {
		return nil, err
	} else {
		if len(gaspTx.AncillaryBeef) > 0 {
			if err := beef.MergeBeefBytes(gaspTx.AncillaryBeef); err != nil {
				return nil, err
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
		} else if admit, err := s.Engine.Managers[s.Topic].IdentifyAdmissibleOutputs(ctx, beefBytes, previousCoins); err != nil {
			return nil, err
		} else if !slices.Contains(admit.OutputsToAdmit, gaspTx.OutputIndex) {
			if neededInputs, err := s.Engine.Managers[s.Topic].IdentifyNeededInputs(ctx, beefBytes); err != nil {
				return nil, err
			} else {
				for _, outpoint := range neededInputs {
					response.RequestedInputs[outpoint.String()] = &gasp.NodeResponseData{
						Metadata: true,
					}
				}
				return s.stripAlreadyKnowInputs(ctx, response)
			}
		}
	}

	return response, nil
}

func (s *OverlayGASPStorage) stripAlreadyKnowInputs(ctx context.Context, response *gasp.NodeResponse) (*gasp.NodeResponse, error) {
	if response == nil {
		return nil, nil
	}
	for outpointStr := range response.RequestedInputs {
		if outpoint, err := transaction.OutpointFromString(outpointStr); err != nil {
			return nil, err
		} else if found, err := s.Engine.Storage.FindOutput(ctx, outpoint, &s.Topic, nil, false); err != nil {
			return nil, err
		} else if found != nil {
			delete(response.RequestedInputs, outpointStr)
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
		if gaspTx.Proof != nil {
			if tx.MerklePath, err = transaction.NewMerklePathFromHex(*gaspTx.Proof); err != nil {
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
			if parentNode, ok := s.tempGraphNodeRefs.Load(spentBy.String()); !ok {
				return ErrMissingInput
			} else {
				parentNode.(*GraphNode).Children = append(parentNode.(*GraphNode).Children, newGraphNode)
				newGraphNode.Parent = parentNode.(*GraphNode)
			}
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
}

func (s *OverlayGASPStorage) ValidateGraphAnchor(ctx context.Context, graphID *transaction.Outpoint) error {
	if rootNode, ok := s.tempGraphNodeRefs.Load(graphID.String()); !ok {
		return ErrMissingInput
	} else if beef, err := s.getBEEFForNode(rootNode.(*GraphNode)); err != nil {
		return err
	} else if tx, err := transaction.NewTransactionFromBEEF(beef); err != nil {
		return err
	} else if valid, err := spv.Verify(tx, s.Engine.ChainTracker, nil); err != nil {
		return err
	} else if !valid {
		return errors.New("graph anchor is not a valid transaction")
	} else if beefs, err := s.computeOrderedBEEFsForGraph(ctx, graphID); err != nil {
		return err
	} else {
		coins := make(map[string]struct{})
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
				if admit, err := s.Engine.Managers[s.Topic].IdentifyAdmissibleOutputs(ctx, beef, previousCoins); err != nil {
					return err
				} else {
					for _, vout := range admit.OutputsToAdmit {
						outpoint := &transaction.Outpoint{
							Txid:  *tx.TxID(),
							Index: vout,
						}
						coins[outpoint.String()] = struct{}{}
					}
				}
			}
		}
		if _, ok := coins[graphID.String()]; !ok {
			return errors.New("graph did not result in topical admittance of the root node. rejecting")
		}
		return nil
	}
}

func (s *OverlayGASPStorage) DiscardGraph(ctx context.Context, graphID *transaction.Outpoint) error {
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
	for _, nodeId := range nodesToDelete {
		s.tempGraphNodeRefs.Delete(nodeId)
		s.tempGraphNodeCount--
	}

	return nil
}

func (s *OverlayGASPStorage) FinalizeGraph(ctx context.Context, graphID *transaction.Outpoint) error {
	if beefs, err := s.computeOrderedBEEFsForGraph(ctx, graphID); err != nil {
		return err
	} else {
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
			for _, child := range node.Children {
				if err := hydrator(child); err != nil {
					return err
				}
			}
		}
		return nil
	}

	if foundRoot, ok := s.tempGraphNodeRefs.Load(graphID.String()); !ok {
		return nil, errors.New("unable to find root node in graph for finalization")
	} else if err := hydrator(foundRoot.(*GraphNode)); err != nil {
		return nil, err
	} else {
		return beefs, nil
	}
}

func (s *OverlayGASPStorage) getBEEFForNode(node *GraphNode) ([]byte, error) {
	var hydrator func(node *GraphNode) (*transaction.Transaction, error)
	hydrator = func(node *GraphNode) (*transaction.Transaction, error) {
		if tx, err := transaction.NewTransactionFromHex(node.RawTx); err != nil {
			return nil, err
		} else if node.Proof != nil {
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
				if foundNode, ok := s.tempGraphNodeRefs.Load(outpoint.String()); !ok {
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
