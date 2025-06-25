package core

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sync"

	"github.com/bsv-blockchain/go-sdk/chainhash"
	"github.com/bsv-blockchain/go-sdk/transaction"
)

const MAX_CONCURRENCY = 16

type GASPNodeRequest struct {
	GraphID     *transaction.Outpoint `json:"graphID"`
	Txid        *chainhash.Hash       `json:"txid"`
	OutputIndex uint32                `json:"outputIndex"`
	Metadata    bool                  `json:"metadata"`
}

type GASPParams struct {
	Storage         GASPStorage
	Remote          GASPRemote
	LastInteraction uint32
	Version         *int
	LogPrefix       *string
	Unidirectional  bool
	LogLevel        slog.Level
	Concurrency     int
}

type GASP struct {
	Version         int
	Remote          GASPRemote
	Storage         GASPStorage
	LastInteraction uint32
	LogPrefix       string
	Unidirectional  bool
	LogLevel        slog.Level
	limiter         chan struct{}
}

func NewGASP(params GASPParams) *GASP {
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

func (g *GASP) Sync(ctx context.Context) error {
	slog.Info(fmt.Sprintf("%sStarting sync process. Last interaction timestamp: %d", g.LogPrefix, g.LastInteraction))
	initialRequest := &GASPInitialRequest{
		Version: g.Version,
		Since:   g.LastInteraction,
	}
	initialResponse, err := g.Remote.GetInitialResponse(ctx, initialRequest)
	if err != nil {
		return err
	} else if len(initialResponse.UTXOList) > 0 {
		if foreignUTXOs, err := g.Storage.FindKnownUTXOs(ctx, 0); err != nil {
			return err
		} else {
			var wg sync.WaitGroup
			for _, outpoint := range initialResponse.UTXOList {
				if slices.ContainsFunc(foreignUTXOs, func(foreign *transaction.Outpoint) bool {
					return outpoint.Equal(foreign)
				}) {
					continue
				}
				wg.Add(1)
				g.limiter <- struct{}{}
				go func(outpoint *transaction.Outpoint) {
					defer func() {
						<-g.limiter
						wg.Done()
					}()
					slog.Info(fmt.Sprintf("%sRequesting node for UTXO: %s", g.LogPrefix, outpoint.String()))
					var resolvedNode *GASPNode
					var err error
					if resolvedNode, err = g.Remote.RequestNode(ctx, outpoint, outpoint, true); err == nil {
						slog.Debug(fmt.Sprintf("%sReceived unspent graph node from remote: %v", g.LogPrefix, resolvedNode))
						if err = g.processIncomingNode(ctx, resolvedNode, nil, &sync.Map{}); err == nil {
							if err = g.CompleteGraph(ctx, resolvedNode.GraphID); err == nil {
								return
							}
						}
					}
					slog.Warn(fmt.Sprintf("%sError with incoming UTXO %s: %v", g.LogPrefix, outpoint.String(), err))
				}(outpoint)
			}
			wg.Wait()
		}
	}
	if !g.Unidirectional {
		if initialReply, err := g.Remote.GetInitialReply(ctx, initialResponse); err != nil {
			return err
		} else {
			slog.Info(fmt.Sprintf("%sReceived initial reply: %v", g.LogPrefix, initialReply))
			var wg sync.WaitGroup
			for _, outpoint := range initialReply.UTXOList {
				wg.Add(1)
				g.limiter <- struct{}{}
				go func(outpoint *transaction.Outpoint) {
					defer func() {
						<-g.limiter
						wg.Done()
					}()
					var outgoingNode *GASPNode
					slog.Info(fmt.Sprintf("%sHydrating GASP node for UTXO: %s", g.LogPrefix, outpoint.String()))
					if outgoingNode, err = g.Storage.HydrateGASPNode(ctx, outpoint, outpoint, true); err == nil && outgoingNode != nil {
						slog.Debug(fmt.Sprintf("%sSending unspent graph node for remote: %v", g.LogPrefix, outgoingNode))
						if err = g.processOutgoingNode(ctx, outgoingNode, &sync.Map{}); err == nil {
							return
						}
					} else if err != nil {
						slog.Warn(fmt.Sprintf("%sError hydrating outgoing UTXO %s: %v", g.LogPrefix, outpoint, err))
					} else {
						slog.Debug(fmt.Sprintf("%sSkipping outgoing UTXO %s: not found in storage", g.LogPrefix, outpoint))
					}
				}(outpoint)
			}
			wg.Wait()
		}
	}
	slog.Info(fmt.Sprintf("%sSync completed!", g.LogPrefix))
	return nil
}

func (g *GASP) GetInitialResponse(ctx context.Context, request *GASPInitialRequest) (resp *GASPInitialResponse, err error) {
	slog.Info(fmt.Sprintf("%sReceived initial request: %v", g.LogPrefix, request))
	if request.Version != g.Version {
		slog.Error(fmt.Sprintf("%sGASP version mismatch", g.LogPrefix))
		return nil, NewGASPVersionMismatchError(
			g.Version,
			request.Version,
		)
	}
	resp = &GASPInitialResponse{
		Since: g.LastInteraction,
	}
	if resp.UTXOList, err = g.Storage.FindKnownUTXOs(ctx, request.Since); err != nil {
		return nil, err
	}
	slog.Debug(fmt.Sprintf("%sBuilt initial response: %v", g.LogPrefix, resp))
	return resp, nil
}

func (g *GASP) GetInitialReply(ctx context.Context, response *GASPInitialResponse) (resp *GASPInitialReply, err error) {
	slog.Info(fmt.Sprintf("%sReceived initial response: %v", g.LogPrefix, response))
	if knownUtxos, err := g.Storage.FindKnownUTXOs(ctx, response.Since); err != nil {
		return nil, err
	} else {
		slog.Info(fmt.Sprintf("%sFound %d known UTXOs since %d", g.LogPrefix, len(knownUtxos), response.Since))
		resp = &GASPInitialReply{
			UTXOList: make([]*transaction.Outpoint, 0),
		}
		// Return UTXOs we have that are NOT in the response list
		for _, knownUtxo := range knownUtxos {
			if !slices.ContainsFunc(response.UTXOList, func(responseUtxo *transaction.Outpoint) bool {
				return responseUtxo.Equal(knownUtxo)
			}) {
				resp.UTXOList = append(resp.UTXOList, knownUtxo)
			}
		}
		slog.Info(fmt.Sprintf("%sBuilt initial reply: %v", g.LogPrefix, resp))
		return resp, nil
	}
}

func (g *GASP) RequestNode(ctx context.Context, graphID *transaction.Outpoint, outpoint *transaction.Outpoint, metadata bool) (node *GASPNode, err error) {
	slog.Info(fmt.Sprintf("%sRemote is requesting node with graphID: %s, txid: %s, outputIndex: %d, metadata: %v", g.LogPrefix, graphID.String(), outpoint.Txid.String(), outpoint.Index, metadata))
	if node, err = g.Storage.HydrateGASPNode(ctx, graphID, outpoint, metadata); err != nil {
		return nil, err
	}
	slog.Debug(fmt.Sprintf("%sReturning node: %v", g.LogPrefix, node))
	return node, nil
}

func (g *GASP) SubmitNode(ctx context.Context, node *GASPNode) (requestedInputs *GASPNodeResponse, err error) {
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

func (g *GASP) CompleteGraph(ctx context.Context, graphID *transaction.Outpoint) (err error) {
	slog.Info(fmt.Sprintf("%sCompleting newly-synced graph: %s", g.LogPrefix, graphID.String()))
	if err = g.Storage.ValidateGraphAnchor(ctx, graphID); err == nil {
		slog.Debug(fmt.Sprintf("%sGraph validated for node: %s", g.LogPrefix, graphID.String()))
		if err := g.Storage.FinalizeGraph(ctx, graphID); err == nil {
			return nil
		}
		slog.Info(fmt.Sprintf("%sGraph finalized for node: %s", g.LogPrefix, graphID.String()))
	}
	slog.Warn(fmt.Sprintf("%sError completing graph %s: %v", g.LogPrefix, graphID.String(), err))
	return g.Storage.DiscardGraph(ctx, graphID)
}

func (g *GASP) processIncomingNode(ctx context.Context, node *GASPNode, spentBy *transaction.Outpoint, seenNodes *sync.Map) error {
	if txid, err := g.computeTxID(node.RawTx); err != nil {
		return err
	} else {
		nodeId := (&transaction.Outpoint{
			Txid:  *txid,
			Index: node.OutputIndex,
		}).String()
		slog.Debug(fmt.Sprintf("%sProcessing incoming node: %v, spentBy: %v", g.LogPrefix, node, spentBy))
		if _, ok := seenNodes.Load(nodeId); ok {
			slog.Debug(fmt.Sprintf("%sNode %s already processed, skipping.", g.LogPrefix, nodeId))
			return nil
		}
		seenNodes.Store(nodeId, struct{}{})
		if err := g.Storage.AppendToGraph(ctx, node, spentBy); err != nil {
			return err
		} else if neededInputs, err := g.Storage.FindNeededInputs(ctx, node); err != nil {
			return err
		} else if neededInputs != nil {
			slog.Debug(fmt.Sprintf("%sNeeded inputs for node %s: %v", g.LogPrefix, nodeId, neededInputs))
			var wg sync.WaitGroup
			errors := make(chan error)
			for outpointStr, data := range neededInputs.RequestedInputs {
				wg.Add(1)
				g.limiter <- struct{}{}
				go func(outpointStr string, data *GASPNodeResponseData) {
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
	}
	return nil
}

func (g *GASP) processOutgoingNode(ctx context.Context, node *GASPNode, seenNodes *sync.Map) error {
	if g.Unidirectional {
		slog.Debug(fmt.Sprintf("%sSkipping outgoing node processing in unidirectional mode.", g.LogPrefix))
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
		slog.Debug(fmt.Sprintf("%sProcessing outgoing node: %v", g.LogPrefix, node))
		if _, ok := seenNodes.Load(nodeId); ok {
			slog.Debug(fmt.Sprintf("%sNode %s already processed, skipping.", g.LogPrefix, nodeId))
			return nil
		}
		seenNodes.Store(nodeId, struct{}{})
		if response, err := g.Remote.SubmitNode(ctx, node); err != nil {
			return err
		} else if response != nil {
			var wg sync.WaitGroup
			for outpointStr, data := range response.RequestedInputs {
				wg.Add(1)
				g.limiter <- struct{}{}
				go func(outpointStr string, data *GASPNodeResponseData) {
					defer func() {
						<-g.limiter
						wg.Done()
					}()
					var outpoint *transaction.Outpoint
					var err error
					if outpoint, err = transaction.OutpointFromString(outpointStr); err == nil {
						var hydratedNode *GASPNode
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
	}
	return nil
}

func (g *GASP) computeTxID(rawtx string) (*chainhash.Hash, error) {
	if tx, err := transaction.NewTransactionFromHex(rawtx); err != nil {
		return nil, err
	} else {
		return tx.TxID(), nil
	}
}
