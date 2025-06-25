package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/4chain-ag/go-overlay-services/pkg/core/advertiser"
	"github.com/4chain-ag/go-overlay-services/pkg/core/gasp/core"
	"github.com/bsv-blockchain/go-sdk/chainhash"
	"github.com/bsv-blockchain/go-sdk/overlay"
	"github.com/bsv-blockchain/go-sdk/overlay/lookup"
	"github.com/bsv-blockchain/go-sdk/overlay/topic"
	"github.com/bsv-blockchain/go-sdk/spv"
	"github.com/bsv-blockchain/go-sdk/transaction"
	"github.com/bsv-blockchain/go-sdk/transaction/chaintracker"
)

var TRUE = true
var FALSE = false

type SumbitMode string

var (
	SubmitModeHistorical SumbitMode = "historical-tx"
	SubmitModeCurrent    SumbitMode = "current-tx"
)

type SyncConfigurationType int

const (
	SyncConfigurationPeers SyncConfigurationType = iota
	SyncConfigurationSHIP
	SyncConfigurationNone
)

type SyncConfiguration struct {
	Type        SyncConfigurationType
	Peers       []string
	Concurrency int
}

type OnSteakReady func(steak *overlay.Steak)

type LookupResolverProvider interface {
	SLAPTrackers() []string
	SetSLAPTrackers(trackers []string)
	Query(ctx context.Context, question *lookup.LookupQuestion) (*lookup.LookupAnswer, error)
}

type GASPProvider interface {
	Sync(ctx context.Context) error
}

type Engine struct {
	Managers                map[string]TopicManager
	LookupServices          map[string]LookupService
	Storage                 Storage
	ChainTracker            chaintracker.ChainTracker
	HostingURL              string
	SHIPTrackers            []string
	SLAPTrackers            []string
	Broadcaster             transaction.Broadcaster
	Advertiser              advertiser.Advertiser
	SyncConfiguration       map[string]SyncConfiguration
	LogTime                 bool
	LogPrefix               string
	ErrorOnBroadcastFailure bool
	BroadcastFacilitator    topic.Facilitator
	LookupResolver          LookupResolverProvider
	GASPProvider            GASPProvider
	// Logger				  Logger //TODO: Implement Logger Interface
}

func NewEngine(cfg Engine) *Engine {
	if cfg.SyncConfiguration == nil {
		cfg.SyncConfiguration = make(map[string]SyncConfiguration)
	}
	if cfg.Managers == nil {
		cfg.Managers = make(map[string]TopicManager)
	}
	if cfg.LookupServices == nil {
		cfg.LookupServices = make(map[string]LookupService)
	}
	if cfg.LookupResolver == nil {
		cfg.LookupResolver = NewLookupResolver()
	}

	for name, manager := range cfg.Managers {
		config := cfg.SyncConfiguration[name]

		if name == "tm_ship" && len(cfg.SHIPTrackers) > 0 && manager != nil && config.Type == SyncConfigurationPeers {
			combined := make(map[string]struct{}, len(cfg.SHIPTrackers)+len(config.Peers))
			for _, peer := range cfg.SHIPTrackers {
				combined[peer] = struct{}{}
			}
			for _, peer := range config.Peers {
				combined[peer] = struct{}{}
			}
			config.Peers = make([]string, 0, len(combined))
			for peer := range combined {
				config.Peers = append(config.Peers, peer)
			}
			cfg.SyncConfiguration[name] = config
		} else if name == "tm_slap" && len(cfg.SLAPTrackers) > 0 && manager != nil && config.Type == SyncConfigurationPeers {
			combined := make(map[string]struct{}, len(cfg.SHIPTrackers)+len(config.Peers))
			for _, peer := range cfg.SLAPTrackers {
				combined[peer] = struct{}{}
			}
			for _, peer := range config.Peers {
				combined[peer] = struct{}{}
			}
			config.Peers = make([]string, 0, len(combined))
			for peer := range combined {
				config.Peers = append(config.Peers, peer)
			}
			cfg.SyncConfiguration[name] = config
		}
	}

	return &cfg
}

var ErrUnknownTopic = errors.New("unknown-topic")
var ErrInvalidBeef = errors.New("invalid-beef")
var ErrInvalidTransaction = errors.New("invalid-transaction")
var ErrMissingInput = errors.New("missing-input")
var ErrInputSpent = errors.New("input-spent")

func (e *Engine) Submit(ctx context.Context, taggedBEEF overlay.TaggedBEEF, mode SumbitMode, onSteakReady OnSteakReady) (overlay.Steak, error) {
	start := time.Now()
	for _, topic := range taggedBEEF.Topics {
		if _, ok := e.Managers[topic]; !ok {
			slog.Error("unknown topic in Submit", "topic", topic, "error", ErrUnknownTopic)
			return nil, ErrUnknownTopic
		}
	}

	var tx *transaction.Transaction
	beef, tx, txid, err := transaction.ParseBeef(taggedBEEF.Beef)
	if err != nil {
		slog.Error("failed to parse BEEF in Submit", "error", err)
		return nil, err
	} else if tx == nil {
		slog.Error("invalid BEEF in Submit - tx is nil", "error", ErrInvalidBeef)
		return nil, ErrInvalidBeef
	}
	if valid, err := spv.Verify(tx, e.ChainTracker, nil); err != nil {
		slog.Error("SPV verification failed in Submit", "txid", txid, "error", err)
		return nil, err
	} else if !valid {
		slog.Error("invalid transaction in Submit", "txid", txid, "error", ErrInvalidTransaction)
		return nil, ErrInvalidTransaction
	}
	slog.Debug("transaction validated", "duration", time.Since(start))
	start = time.Now()
	steak := make(overlay.Steak, len(taggedBEEF.Topics))
	topicInputs := make(map[string]map[uint32]*Output, len(tx.Inputs))
	inpoints := make([]*transaction.Outpoint, 0, len(tx.Inputs))
	ancillaryBeefs := make(map[string][]byte, len(taggedBEEF.Topics))
	for _, input := range tx.Inputs {
		inpoints = append(inpoints, &transaction.Outpoint{
			Txid:  *input.SourceTXID,
			Index: input.SourceTxOutIndex,
		})
	}
	dupeTopics := make(map[string]struct{}, len(taggedBEEF.Topics))
	for _, topic := range taggedBEEF.Topics {
		if exists, err := e.Storage.DoesAppliedTransactionExist(ctx, &overlay.AppliedTransaction{
			Txid:  txid,
			Topic: topic,
		}); err != nil {
			slog.Error("failed to check if transaction exists", "txid", txid, "topic", topic, "error", err)
			return nil, err
		} else if exists {
			steak[topic] = &overlay.AdmittanceInstructions{}
			dupeTopics[topic] = struct{}{}
			continue
		} else {
			topicInputs[topic] = make(map[uint32]*Output, len(tx.Inputs))
			previousCoins := make(map[uint32]*transaction.TransactionOutput, len(tx.Inputs))
			outputs, err := e.Storage.FindOutputs(ctx, inpoints, topic, nil, false)
			if err != nil {
				slog.Error("failed to find outputs", "topic", topic, "error", err)
				return nil, err
			}
			for vin, output := range outputs {
				if output != nil {
					previousCoins[uint32(vin)] = &transaction.TransactionOutput{
						LockingScript: output.Script,
						Satoshis:      output.Satoshis,
					}
					topicInputs[topic][uint32(vin)] = output
				}
			}

			if admit, err := e.Managers[topic].IdentifyAdmissibleOutputs(ctx, taggedBEEF.Beef, previousCoins); err != nil {
				slog.Error("failed to identify admissible outputs", "topic", topic, "error", err)
				return nil, err
			} else {
				slog.Debug("admissible outputs identified", "duration", time.Since(start))
				start = time.Now()
				if len(admit.AncillaryTxids) > 0 {
					ancillaryBeef := transaction.Beef{
						Version:      transaction.BEEF_V2,
						Transactions: make(map[string]*transaction.BeefTx, len(admit.AncillaryTxids)),
					}
					for _, txid := range admit.AncillaryTxids {
						if tx := beef.FindTransaction(txid.String()); tx == nil {
							err := errors.New("missing dependency transaction")
							slog.Error("missing dependency transaction", "txid", txid, "error", err)
							return nil, err
						} else if beefBytes, err := tx.BEEF(); err != nil {
							slog.Error("failed to get BEEF bytes", "txid", txid, "error", err)
							return nil, err
						} else if err := ancillaryBeef.MergeBeefBytes(beefBytes); err != nil {
							slog.Error("failed to merge BEEF bytes", "txid", txid, "error", err)
							return nil, err
						}
					}
					if beefBytes, err := ancillaryBeef.Bytes(); err != nil {
						slog.Error("failed to get ancillary BEEF bytes", "topic", topic, "error", err)
						return nil, err
					} else {
						ancillaryBeefs[topic] = beefBytes
					}
				}
				steak[topic] = &admit
			}
		}
	}

	for _, topic := range taggedBEEF.Topics {
		if _, ok := dupeTopics[topic]; ok {
			continue
		}
		if err := e.Storage.MarkUTXOsAsSpent(ctx, inpoints, topic, txid); err != nil {
			slog.Error("failed to mark UTXOs as spent", "topic", topic, "txid", txid, "error", err)
			return nil, err
		}
		for vin, outpoint := range inpoints {
			for _, l := range e.LookupServices {
				if err := l.OutputSpent(ctx, &OutputSpent{
					Outpoint:           outpoint,
					Topic:              topic,
					SpendingTxid:       txid,
					InputIndex:         uint32(vin),
					UnlockingScript:    tx.Inputs[vin].UnlockingScript,
					SequenceNumber:     tx.Inputs[vin].SequenceNumber,
					SpendingAtomicBEEF: taggedBEEF.Beef,
				}); err != nil {
					slog.Error("failed to notify lookup service about spent output", "topic", topic, "txid", txid, "error", err)
					return nil, err
				}
			}
		}
	}
	slog.Debug("UTXOs marked as spent", "duration", time.Since(start))
	start = time.Now()
	if mode != SubmitModeHistorical && e.Broadcaster != nil {
		if _, failure := e.Broadcaster.Broadcast(tx); failure != nil {
			slog.Error("failed to broadcast transaction", "txid", txid, "error", failure)
			return nil, failure
		}
	}

	if onSteakReady != nil {
		onSteakReady(&steak)
	}

	for _, topic := range taggedBEEF.Topics {
		if _, ok := dupeTopics[topic]; ok {
			continue
		}
		admit := steak[topic]
		outputsConsumed := make([]*Output, 0, len(admit.CoinsToRetain))
		outpointsConsumed := make([]*transaction.Outpoint, 0, len(admit.CoinsToRetain))
		for vin, output := range topicInputs[topic] {
			for _, coin := range admit.CoinsToRetain {
				if vin == coin {
					outputsConsumed = append(outputsConsumed, output)
					outpointsConsumed = append(outpointsConsumed, &output.Outpoint)
					delete(topicInputs[topic], vin)
					break
				}
			}
		}

		for vin, output := range topicInputs[topic] {
			if err := e.deleteUTXODeep(ctx, output); err != nil {
				slog.Error("failed to delete UTXO deep", "topic", topic, "outpoint", output.Outpoint.String(), "error", err)
				return nil, err
			}
			admit.CoinsRemoved = append(admit.CoinsRemoved, uint32(vin))
		}

		newOutpoints := make([]*transaction.Outpoint, 0, len(admit.OutputsToAdmit))
		for _, vout := range admit.OutputsToAdmit {
			out := tx.Outputs[vout]
			output := &Output{
				Outpoint: transaction.Outpoint{
					Txid:  *txid,
					Index: uint32(vout),
				},
				Script:          out.LockingScript,
				Satoshis:        out.Satoshis,
				Topic:           topic,
				OutputsConsumed: outpointsConsumed,
				Beef:            taggedBEEF.Beef,
				AncillaryTxids:  admit.AncillaryTxids,
				AncillaryBeef:   ancillaryBeefs[topic],
			}
			if tx.MerklePath != nil {
				output.BlockHeight = tx.MerklePath.BlockHeight
				for _, leaf := range tx.MerklePath.Path[0] {
					if leaf.Hash != nil && leaf.Hash.Equal(output.Outpoint.Txid) {
						output.BlockIdx = leaf.Offset
						break
					}
				}
			}
			if err := e.Storage.InsertOutput(ctx, output); err != nil {
				slog.Error("failed to insert output", "topic", topic, "outpoint", output.Outpoint.String(), "error", err)
				return nil, err
			}
			newOutpoints = append(newOutpoints, &output.Outpoint)
			for _, l := range e.LookupServices {
				if err := l.OutputAdmittedByTopic(ctx, &OutputAdmittedByTopic{
					Topic:         topic,
					Outpoint:      &output.Outpoint,
					Satoshis:      output.Satoshis,
					LockingScript: output.Script,
					AtomicBEEF:    taggedBEEF.Beef,
				}); err != nil {
					slog.Error("failed to notify lookup service about admitted output", "topic", topic, "outpoint", output.Outpoint.String(), "error", err)
					return nil, err
				}
			}
		}
		slog.Debug("outputs added", "duration", time.Since(start))
		start = time.Now()
		for _, output := range outputsConsumed {
			output.ConsumedBy = append(output.ConsumedBy, newOutpoints...)

			if err := e.Storage.UpdateConsumedBy(ctx, &output.Outpoint, output.Topic, output.ConsumedBy); err != nil {
				slog.Error("failed to update consumed by", "topic", output.Topic, "outpoint", output.Outpoint.String(), "error", err)
				return nil, err
			}
		}
		slog.Debug("consumed by references updated", "duration", time.Since(start))
		start = time.Now()
		if err := e.Storage.InsertAppliedTransaction(ctx, &overlay.AppliedTransaction{
			Txid:  txid,
			Topic: topic,
		}); err != nil {
			slog.Error("failed to insert applied transaction", "topic", topic, "txid", txid, "error", err)
			return nil, err
		}
		slog.Debug("transaction applied", "duration", time.Since(start))
	}
	if e.Advertiser == nil || mode == SubmitModeHistorical {
		return steak, nil
	}

	releventTopics := make([]string, 0, len(taggedBEEF.Topics))
	for topic, steak := range steak {
		if steak.OutputsToAdmit == nil && steak.CoinsToRetain == nil {
			continue
		}
		if _, ok := dupeTopics[topic]; !ok {
			releventTopics = append(releventTopics, topic)
		}
	}
	if len(releventTopics) == 0 {
		return steak, nil
	}

	broadcasterCfg := &topic.BroadcasterConfig{}
	if len(e.SLAPTrackers) > 0 {
		broadcasterCfg.Resolver = lookup.NewLookupResolver(&lookup.LookupResolver{
			SLAPTrackers: e.SLAPTrackers,
		})
	}

	if broadcaster, err := topic.NewBroadcaster(releventTopics, broadcasterCfg); err != nil {
		slog.Error("failed to create broadcaster for propagation", "topics", releventTopics, "error", err)
	} else if _, failure := broadcaster.BroadcastCtx(ctx, tx); failure != nil {
		slog.Error("failed to propagate transaction to other nodes", "txid", txid, "error", failure)
	}
	return steak, nil
}

func (e *Engine) Lookup(ctx context.Context, question *lookup.LookupQuestion) (*lookup.LookupAnswer, error) {
	if l, ok := e.LookupServices[question.Service]; !ok {
		slog.Error("unknown lookup service", "service", question.Service, "error", ErrUnknownTopic)
		return nil, ErrUnknownTopic
	} else if result, err := l.Lookup(ctx, question); err != nil {
		slog.Error("lookup service failed", "service", question.Service, "error", err)
		return nil, err
	} else if result.Type == lookup.AnswerTypeFreeform || result.Type == lookup.AnswerTypeOutputList {
		return result, nil
	} else {
		hydratedOutputs := make([]*lookup.OutputListItem, 0, len(result.Outputs))
		for _, formula := range result.Formulas {
			if output, err := e.Storage.FindOutput(ctx, formula.Outpoint, nil, nil, true); err != nil {
				slog.Error("failed to find output in Lookup", "outpoint", formula.Outpoint.String(), "error", err)
				return nil, err
			} else if output != nil && output.Beef != nil {
				if output, err := e.GetUTXOHistory(ctx, output, formula.History, 0); err != nil {
					slog.Error("failed to get UTXO history in Lookup", "outpoint", formula.Outpoint.String(), "error", err)
					return nil, err
				} else if output != nil {
					hydratedOutputs = append(hydratedOutputs, &lookup.OutputListItem{
						Beef:        output.Beef,
						OutputIndex: output.Outpoint.Index,
					})
				}
			}
		}
		return &lookup.LookupAnswer{
			Type:    lookup.AnswerTypeOutputList,
			Outputs: hydratedOutputs,
		}, nil
	}
}

func (e *Engine) GetUTXOHistory(ctx context.Context, output *Output, historySelector func(beef []byte, outputIndex uint32, currentDepth uint32) bool, currentDepth uint32) (*Output, error) {
	if historySelector == nil {
		return output, nil
	}
	shouldTravelHistory := historySelector(output.Beef, output.Outpoint.Index, currentDepth)
	if !shouldTravelHistory {
		return nil, nil
	}
	if output != nil && len(output.OutputsConsumed) == 0 {
		return output, nil
	}
	outputsConsumed := output.OutputsConsumed[:]
	childHistories := make(map[string]*Output, len(outputsConsumed))
	for _, outpoint := range outputsConsumed {
		if output, err := e.Storage.FindOutput(ctx, outpoint, nil, nil, true); err != nil {
			slog.Error("failed to find output in GetUTXOHistory", "outpoint", outpoint.String(), "error", err)
			return nil, err
		} else if output != nil {
			if child, err := e.GetUTXOHistory(ctx, output, historySelector, currentDepth+1); err != nil {
				slog.Error("failed to get child UTXO history", "outpoint", outpoint.String(), "depth", currentDepth+1, "error", err)
				return nil, err
			} else if child != nil {
				childHistories[child.Outpoint.String()] = child
			}
		}
	}

	if tx, err := transaction.NewTransactionFromBEEF(output.Beef); err != nil {
		slog.Error("failed to create transaction from BEEF in GetUTXOHistory", "outpoint", output.Outpoint.String(), "error", err)
		return nil, err
	} else {
		for _, txin := range tx.Inputs {
			outpoint := &transaction.Outpoint{
				Txid:  *txin.SourceTXID,
				Index: txin.SourceTxOutIndex,
			}
			if input := childHistories[outpoint.String()]; input != nil {
				if input.Beef == nil {
					err := errors.New("missing beef")
					slog.Error("missing BEEF in GetUTXOHistory", "outpoint", outpoint.String(), "error", err)
					return nil, err
				} else if txin.SourceTransaction, err = transaction.NewTransactionFromBEEF(input.Beef); err != nil {
					slog.Error("failed to create source transaction from BEEF", "outpoint", outpoint.String(), "error", err)
					return nil, err
				}
			}
		}
		if beef, err := tx.BEEF(); err != nil {
			slog.Error("failed to get BEEF from transaction in GetUTXOHistory", "outpoint", output.Outpoint.String(), "error", err)
			return nil, err
		} else {
			output.Beef = beef
			return output, nil
		}
	}
}

func (e *Engine) SyncAdvertisements(ctx context.Context) error {
	if e.Advertiser == nil {
		return nil
	}
	configuredTopics := make([]string, 0, len(e.Managers))
	requiredSHIPAdvertisements := make(map[string]struct{}, len(configuredTopics))
	for name := range e.Managers {
		configuredTopics = append(configuredTopics, name)
		requiredSHIPAdvertisements[name] = struct{}{}
	}
	configuredServices := make([]string, 0, len(e.LookupServices))
	requiredSLAPAdvertisements := make(map[string]struct{}, len(configuredServices))
	for name := range e.LookupServices {
		configuredServices = append(configuredServices, name)
		requiredSLAPAdvertisements[name] = struct{}{}
	}
	currentSHIPAdvertisements, err := e.Advertiser.FindAllAdvertisements("SHIP")
	if err != nil {
		slog.Error("failed to find SHIP advertisements", "error", err)
		return err
	}
	shipsToCreate := make([]string, 0, len(requiredSHIPAdvertisements))
	for topic := range requiredSHIPAdvertisements {
		if slices.IndexFunc(currentSHIPAdvertisements, func(ad *advertiser.Advertisement) bool {
			return ad.TopicOrService == topic && ad.Domain == e.HostingURL
		}) == -1 {
			shipsToCreate = append(shipsToCreate, topic)
		}
	}
	shipsToRevoke := make([]*advertiser.Advertisement, 0, len(currentSHIPAdvertisements))
	for _, ad := range currentSHIPAdvertisements {
		if _, ok := requiredSHIPAdvertisements[ad.TopicOrService]; !ok {
			shipsToRevoke = append(shipsToRevoke, ad)
		}
	}

	currentSLAPAdvertisements, err := e.Advertiser.FindAllAdvertisements("SLAP")
	if err != nil {
		slog.Error("failed to find SLAP advertisements", "error", err)
		return err
	}
	slapsToCreate := make([]string, 0, len(requiredSLAPAdvertisements))
	for service := range requiredSLAPAdvertisements {
		if slices.IndexFunc(currentSLAPAdvertisements, func(ad *advertiser.Advertisement) bool {
			return ad.TopicOrService == service && ad.Domain == e.HostingURL
		}) == -1 {
			slapsToCreate = append(slapsToCreate, service)
		}
	}
	slapsToRevoke := make([]*advertiser.Advertisement, 0, len(currentSLAPAdvertisements))
	for _, ad := range currentSLAPAdvertisements {
		if _, ok := requiredSLAPAdvertisements[ad.TopicOrService]; !ok {
			slapsToRevoke = append(slapsToRevoke, ad)
		}
	}
	advertisementData := make([]*advertiser.AdvertisementData, 0, len(shipsToCreate)+len(slapsToCreate))
	for _, topic := range shipsToCreate {
		advertisementData = append(advertisementData, &advertiser.AdvertisementData{
			Protocol:           "SHIP",
			TopicOrServiceName: topic,
		})
	}
	for _, service := range slapsToCreate {
		advertisementData = append(advertisementData, &advertiser.AdvertisementData{
			Protocol:           "SLAP",
			TopicOrServiceName: service,
		})
	}
	if len(advertisementData) > 0 {
		if taggedBEEF, err := e.Advertiser.CreateAdvertisements(advertisementData); err != nil {
			slog.Error("failed to create SHIP/SLAP advertisements", "error", err)
		} else if _, err := e.Submit(ctx, taggedBEEF, SubmitModeCurrent, nil); err != nil {
			slog.Error("failed to submit SHIP/SLAP advertisements", "error", err)
		}
	}
	revokeData := make([]*advertiser.Advertisement, 0, len(shipsToRevoke)+len(slapsToRevoke))
	revokeData = append(revokeData, shipsToRevoke...)
	revokeData = append(revokeData, slapsToRevoke...)
	if len(revokeData) > 0 {
		if taggedBEEF, err := e.Advertiser.RevokeAdvertisements(revokeData); err != nil {
			slog.Error("failed to revoke SHIP/SLAP advertisements", "error", err)
		} else if _, err := e.Submit(ctx, taggedBEEF, SubmitModeCurrent, nil); err != nil {
			slog.Error("failed to submit SHIP/SLAP advertisement revocation", "error", err)
		}
	}
	return nil
}

func (e *Engine) StartGASPSync(ctx context.Context) error {
	for topic := range e.SyncConfiguration {
		syncEndpoints, ok := e.SyncConfiguration[topic]
		if !ok {
			continue
		}

		if syncEndpoints.Type == SyncConfigurationSHIP {
			e.LookupResolver.SetSLAPTrackers(e.SLAPTrackers)

			query, err := json.Marshal(map[string]any{"topics": []string{topic}})
			if err != nil {
				slog.Error("failed to marshal query for GASP sync", "topic", topic, "error", err)
				return err
			}

			timeoutCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			lookupAnswer, err := e.LookupResolver.Query(timeoutCtx, &lookup.LookupQuestion{Service: "ls_ship", Query: query})
			if err != nil {
				slog.Error("failed to query lookup resolver for GASP sync", "topic", topic, "error", err)
				return err
			}

			if lookupAnswer.Type == lookup.AnswerTypeOutputList {
				endpointSet := make(map[string]struct{}, len(lookupAnswer.Outputs))
				for _, output := range lookupAnswer.Outputs {
					tx, err := transaction.NewTransactionFromBEEF(output.Beef)
					if err != nil {
						slog.Error("failed to parse advertisement output BEEF", "topic", topic, "error", err)
						continue
					}

					advertisement, err := e.Advertiser.ParseAdvertisement(tx.Outputs[output.OutputIndex].LockingScript)
					if err != nil {
						slog.Error("failed to parse advertisement from locking script", "topic", topic, "error", err)
						continue
					}

					if advertisement != nil && advertisement.Protocol == "SHIP" {
						endpointSet[advertisement.Domain] = struct{}{}
					}
				}

				syncEndpoints.Peers = make([]string, 0, len(endpointSet))
				for endpoint := range endpointSet {
					if endpoint != e.HostingURL {
						syncEndpoints.Peers = append(syncEndpoints.Peers, endpoint)
					}
				}
			}
		}

		if len(syncEndpoints.Peers) > 0 {
			peers := make([]string, 0, len(syncEndpoints.Peers))
			for _, peer := range syncEndpoints.Peers {
				if peer != e.HostingURL {
					peers = append(peers, peer)
				}
			}

			for _, peer := range peers {
				logPrefix := "[GASP Sync of " + topic + " with " + peer + "]"

				if e.GASPProvider == nil {
					e.GASPProvider = core.NewGASP(core.GASPParams{
						Storage: NewOverlayGASPStorage(topic, e, nil),
						Remote: &OverlayGASPRemote{
							EndpointUrl: peer,
							Topic:       topic,
							HttpClient:  http.DefaultClient,
						},
						LogPrefix:      &logPrefix,
						Unidirectional: true,
						Concurrency:    syncEndpoints.Concurrency,
					})
				}

				if err := e.GASPProvider.Sync(ctx); err != nil {
					slog.Error("failed to sync with peer", "topic", topic, "peer", peer, "error", err)
				}
			}
		}
	}
	return nil
}

func (e *Engine) ProvideForeignSyncResponse(ctx context.Context, initialRequest *core.GASPInitialRequest, topic string) (*core.GASPInitialResponse, error) {
	if utxos, err := e.Storage.FindUTXOsForTopic(ctx, topic, initialRequest.Since, false); err != nil {
		slog.Error("failed to find UTXOs for topic in ProvideForeignSyncResponse", "topic", topic, "error", err)
		return nil, err
	} else {
		utxoList := make([]*transaction.Outpoint, 0, len(utxos))
		for _, utxo := range utxos {
			utxoList = append(utxoList, &utxo.Outpoint)
		}
		return &core.GASPInitialResponse{
			UTXOList: utxoList,
		}, nil
	}
}

func (e *Engine) ProvideForeignGASPNode(ctx context.Context, graphId *transaction.Outpoint, outpoint *transaction.Outpoint, topic string) (*core.GASPNode, error) {
	var hydrator func(ctx context.Context, output *Output) (*core.GASPNode, error)
	hydrator = func(ctx context.Context, output *Output) (*core.GASPNode, error) {
		if output.Beef == nil {
			slog.Error("missing BEEF in ProvideForeignGASPNode hydrator", "outpoint", output.Outpoint.String(), "error", ErrMissingInput)
			return nil, ErrMissingInput
		} else if _, tx, _, err := transaction.ParseBeef(output.Beef); err != nil {
			slog.Error("failed to parse BEEF in ProvideForeignGASPNode hydrator", "outpoint", output.Outpoint.String(), "error", err)
			return nil, err
		} else if tx == nil {
			for _, outpoint := range output.OutputsConsumed {
				if output, err := e.Storage.FindOutput(ctx, outpoint, &topic, nil, false); err == nil {
					return hydrator(ctx, output)
				}
			}
			err := errors.New("unable to find output")
			slog.Error("unable to find output in ProvideForeignGASPNode", "graphId", graphId.String(), "error", err)
			return nil, err
		} else {
			node := &core.GASPNode{
				GraphID:       graphId,
				RawTx:         tx.Hex(),
				OutputIndex:   outpoint.Index,
				AncillaryBeef: output.AncillaryBeef,
			}
			if tx.MerklePath != nil {
				proof := tx.MerklePath.Hex()
				node.Proof = &proof
			}
			return node, nil

		}

	}
	if output, err := e.Storage.FindOutput(ctx, graphId, &topic, nil, true); err != nil {
		slog.Error("failed to find output in ProvideForeignGASPNode", "graphId", graphId.String(), "topic", topic, "error", err)
		return nil, err
	} else {
		return hydrator(ctx, output)
	}
}

func (e *Engine) deleteUTXODeep(ctx context.Context, output *Output) error {
	if len(output.ConsumedBy) == 0 {
		if err := e.Storage.DeleteOutput(ctx, &output.Outpoint, output.Topic); err != nil {
			slog.Error("failed to delete output in deleteUTXODeep", "outpoint", output.Outpoint.String(), "topic", output.Topic, "error", err)
			return err
		}
		for _, l := range e.LookupServices {
			if err := l.OutputNoLongerRetainedInHistory(ctx, &output.Outpoint, output.Topic); err != nil {
				slog.Error("failed to notify lookup service about output removal", "outpoint", output.Outpoint.String(), "topic", output.Topic, "error", err)
				return err
			}
		}
	}
	if len(output.OutputsConsumed) == 0 {
		return nil
	}

	for _, outpoint := range output.OutputsConsumed {
		staleOutput, err := e.Storage.FindOutput(ctx, outpoint, &output.Topic, nil, false)
		if err != nil {
			slog.Error("failed to find stale output in deleteUTXODeep", "outpoint", outpoint.String(), "topic", output.Topic, "error", err)
			return err
		} else if staleOutput == nil {
			continue
		}
		if len(staleOutput.ConsumedBy) > 0 {
			consumedBy := staleOutput.ConsumedBy
			staleOutput.ConsumedBy = make([]*transaction.Outpoint, 0, len(consumedBy))
			for _, outpoint := range consumedBy {
				if !bytes.Equal(outpoint.TxBytes(), output.Outpoint.TxBytes()) {
					staleOutput.ConsumedBy = append(staleOutput.ConsumedBy, outpoint)
				}
			}
			if err := e.Storage.UpdateConsumedBy(ctx, &staleOutput.Outpoint, staleOutput.Topic, staleOutput.ConsumedBy); err != nil {
				slog.Error("failed to update consumed by in deleteUTXODeep", "outpoint", staleOutput.Outpoint.String(), "topic", staleOutput.Topic, "error", err)
				return err
			}
		}

		if err := e.deleteUTXODeep(ctx, staleOutput); err != nil {
			slog.Error("failed recursive deleteUTXODeep", "outpoint", staleOutput.Outpoint.String(), "topic", staleOutput.Topic, "error", err)
			return err
		}
	}
	return nil
}

func (e *Engine) updateInputProofs(ctx context.Context, tx *transaction.Transaction, txid chainhash.Hash, proof *transaction.MerklePath) (err error) {
	if tx.MerklePath != nil {
		tx.MerklePath = proof
		return
	}

	if tx.TxID().Equal(txid) {
		tx.MerklePath = proof
	} else {
		for _, input := range tx.Inputs {
			if input.SourceTransaction == nil {
				err := errors.New("missing source transaction")
				slog.Error("missing source transaction in updateInputProofs", "txid", txid, "error", err)
				return err
			} else if err = e.updateInputProofs(ctx, input.SourceTransaction, txid, proof); err != nil {
				slog.Error("failed to update input proofs recursively", "txid", txid, "error", err)
				return err
			}
		}
	}
	return nil
}

func (e *Engine) updateMerkleProof(ctx context.Context, output *Output, txid chainhash.Hash, proof *transaction.MerklePath) error {
	if len(output.Beef) == 0 {
		err := errors.New("missing beef")
		slog.Error("missing BEEF in updateMerkleProof", "outpoint", output.Outpoint.String(), "error", err)
		return err
	}
	beef, tx, _, err := transaction.ParseBeef(output.Beef)
	if err != nil {
		slog.Error("failed to parse BEEF in updateMerkleProof", "outpoint", output.Outpoint.String(), "error", err)
		return err
	} else if tx == nil {
		err := errors.New("missing transaction")
		slog.Error("missing transaction in updateMerkleProof", "outpoint", output.Outpoint.String(), "error", err)
		return err
	}
	if tx.MerklePath != nil {
		if oldRoot, err := tx.MerklePath.ComputeRoot(&txid); err != nil {
			slog.Error("failed to compute old merkle root", "txid", txid, "error", err)
			return err
		} else if newRoot, err := proof.ComputeRoot(&txid); err != nil {
			slog.Error("failed to compute new merkle root", "txid", txid, "error", err)
			return err
		} else if oldRoot.Equal(*newRoot) {
			return nil
		}
	}
	if err = e.updateInputProofs(ctx, tx, txid, proof); err != nil {
		slog.Error("failed to update input proofs in updateMerkleProof", "txid", txid, "error", err)
		return err
	} else if atomicBytes, err := tx.AtomicBEEF(false); err != nil {
		slog.Error("failed to get atomic BEEF", "txid", txid, "error", err)
		return err
	} else {
		if len(output.AncillaryTxids) > 0 {
			ancillaryBeef := transaction.Beef{
				Version:      transaction.BEEF_V2,
				Transactions: make(map[string]*transaction.BeefTx, len(output.AncillaryTxids)),
			}
			for _, dep := range output.AncillaryTxids {
				if depTx := beef.FindTransaction(dep.String()); depTx == nil {
					err := errors.New("missing dependency transaction")
					slog.Error("missing dependency transaction in updateMerkleProof", "dep", dep, "error", err)
					return err
				} else if depBeefBytes, err := depTx.BEEF(); err != nil {
					slog.Error("failed to get dependency BEEF bytes", "dep", dep, "error", err)
					return err
				} else if err := ancillaryBeef.MergeBeefBytes(depBeefBytes); err != nil {
					slog.Error("failed to merge dependency BEEF bytes", "dep", dep, "error", err)
					return err
				}
			}
			if output.AncillaryBeef, err = ancillaryBeef.Bytes(); err != nil {
				slog.Error("failed to get ancillary BEEF bytes in updateMerkleProof", "outpoint", output.Outpoint.String(), "error", err)
				return err
			}
		} else {
			output.AncillaryBeef = nil
		}

		output.BlockHeight = proof.BlockHeight
		for _, leaf := range proof.Path[0] {
			if leaf.Hash != nil && leaf.Hash.Equal(output.Outpoint.Txid) {
				output.BlockIdx = leaf.Offset
				break
			}
		}
		if err = e.Storage.UpdateTransactionBEEF(ctx, &output.Outpoint.Txid, atomicBytes); err != nil {
			slog.Error("failed to update transaction BEEF", "txid", output.Outpoint.Txid, "error", err)
			return err
		}
		for _, outpoint := range output.ConsumedBy {
			if consumingOutputs, err := e.Storage.FindOutputsForTransaction(ctx, &outpoint.Txid, true); err != nil {
				slog.Error("failed to find consuming outputs", "txid", outpoint.Txid, "error", err)
				return err
			} else {
				for _, consuming := range consumingOutputs {
					if err := e.updateMerkleProof(ctx, consuming, txid, proof); err != nil {
						slog.Error("failed to update merkle proof for consuming output", "consumingTxid", consuming.Outpoint.Txid, "error", err)
						return err
					}
				}
			}
		}
	}
	return nil

}

func (e *Engine) HandleNewMerkleProof(ctx context.Context, txid *chainhash.Hash, proof *transaction.MerklePath) error {
	if outputs, err := e.Storage.FindOutputsForTransaction(ctx, txid, true); err != nil {
		slog.Error("failed to find outputs for transaction in HandleNewMerkleProof", "txid", txid, "error", err)
		return err
	} else if len(outputs) > 0 {
		var blockIdx *uint64
		for _, leaf := range proof.Path[0] {
			if leaf.Hash != nil && leaf.Hash.Equal(*txid) {
				blockIdx = &leaf.Offset
				break
			}
		}
		if blockIdx == nil {
			err := fmt.Errorf("not found in proof: %s", txid)
			slog.Error("transaction not found in merkle proof", "txid", txid, "error", err)
			return err
		}
		blockHeight := proof.BlockHeight
		for _, output := range outputs {
			if err := e.updateMerkleProof(ctx, output, *txid, proof); err != nil {
				slog.Error("failed to update merkle proof in HandleNewMerkleProof", "outpoint", output.Outpoint.String(), "error", err)
				return err
			} else if err := e.Storage.UpdateOutputBlockHeight(ctx, &output.Outpoint, output.Topic, output.BlockHeight, output.BlockIdx, output.AncillaryBeef); err != nil {
				slog.Error("failed to update output block height", "outpoint", output.Outpoint.String(), "error", err)
				return err
			}
		}
		for _, l := range e.LookupServices {
			if err := l.OutputBlockHeightUpdated(ctx, txid, blockHeight, *blockIdx); err != nil {
				slog.Error("failed to notify lookup service about block height update", "txid", txid, "blockHeight", blockHeight, "error", err)
				return err
			}
		}
	}
	return nil
}

func (e *Engine) ListTopicManagers() map[string]*overlay.MetaData {
	result := make(map[string]*overlay.MetaData, len(e.Managers))
	for name, manager := range e.Managers {
		result[name] = manager.GetMetaData()
	}
	return result
}

func (e *Engine) ListLookupServiceProviders() map[string]*overlay.MetaData {
	result := make(map[string]*overlay.MetaData, len(e.LookupServices))
	for name, provider := range e.LookupServices {
		result[name] = provider.GetMetaData()
	}
	return result
}

func (e *Engine) GetDocumentationForTopicManager(manager string) (string, error) {
	if tm, ok := e.Managers[manager]; !ok {
		err := errors.New("no documentation found")
		slog.Error("topic manager not found", "manager", manager, "error", err)
		return "", err
	} else {
		return tm.GetDocumentation(), nil
	}
}

func (e *Engine) GetDocumentationForLookupServiceProvider(provider string) (string, error) {
	if l, ok := e.LookupServices[provider]; !ok {
		err := errors.New("no documentation found")
		slog.Error("lookup service provider not found", "provider", provider, "error", err)
		return "", err
	} else {
		return l.GetDocumentation(), nil
	}
}
