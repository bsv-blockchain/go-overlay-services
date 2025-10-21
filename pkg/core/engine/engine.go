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

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/advertiser"
	"github.com/bsv-blockchain/go-overlay-services/pkg/core/gasp"
	"github.com/bsv-blockchain/go-sdk/chainhash"
	"github.com/bsv-blockchain/go-sdk/overlay"
	"github.com/bsv-blockchain/go-sdk/overlay/lookup"
	"github.com/bsv-blockchain/go-sdk/overlay/topic"
	"github.com/bsv-blockchain/go-sdk/spv"
	"github.com/bsv-blockchain/go-sdk/transaction"
	"github.com/bsv-blockchain/go-sdk/transaction/chaintracker"
)

// DefaultGASPSyncLimit is the default limit for GASP synchronization
const DefaultGASPSyncLimit = 10000

var (
	// TRUE is a boolean true value
	TRUE = true
	// FALSE is a boolean false value
	FALSE = false
)

// SumbitMode represents the mode for transaction submission
type SumbitMode string

var (
	// SubmitModeHistorical is the mode for submitting historical transactions
	SubmitModeHistorical SumbitMode = "historical-tx"
	// SubmitModeCurrent is the mode for submitting current transactions
	SubmitModeCurrent SumbitMode = "current-tx"
)

// SyncConfigurationType represents the type of synchronization configuration
type SyncConfigurationType int

const (
	// SyncConfigurationPeers indicates peer-based synchronization
	SyncConfigurationPeers SyncConfigurationType = iota
	// SyncConfigurationSHIP indicates SHIP-based synchronization
	SyncConfigurationSHIP
	// SyncConfigurationNone indicates no synchronization
	SyncConfigurationNone
)

// SyncConfiguration represents the configuration for synchronization
type SyncConfiguration struct {
	Type        SyncConfigurationType
	Peers       []string
	Concurrency int
}

// OnSteakReady is a callback function that is called when a steak is ready
type OnSteakReady func(steak *overlay.Steak)

// LookupResolverProvider is an interface for looking up and resolving blockchain data
type LookupResolverProvider interface {
	SLAPTrackers() []string
	SetSLAPTrackers(trackers []string)
	Query(ctx context.Context, question *lookup.LookupQuestion) (*lookup.LookupAnswer, error)
}

// Engine is the core overlay services engine
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
	// Logger				  Logger //TODO: Implement Logger Interface
}

// NewEngine creates and returns a new Engine instance
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

var (
	// ErrUnknownTopic is returned when a topic is not found in the engine
	ErrUnknownTopic = errors.New("unknown-topic")
	// ErrInvalidBeef is returned when BEEF data is invalid
	ErrInvalidBeef = errors.New("invalid-beef")
	// ErrInvalidTransaction is returned when a transaction is invalid
	ErrInvalidTransaction = errors.New("invalid-transaction")
	// ErrMissingInput is returned when an input is missing
	ErrMissingInput = errors.New("missing-input")
	// ErrMissingOutput is returned when an output is missing
	ErrMissingOutput = errors.New("missing-output")
	// ErrInputSpent is returned when an input has already been spent
	ErrInputSpent = errors.New("input-spent")
	// ErrMissingDependencyTx is returned when a dependency transaction is missing
	ErrMissingDependencyTx = errors.New("missing dependency transaction")
	// ErrMissingBeef is returned when BEEF data is missing
	ErrMissingBeef = errors.New("missing beef")
	// ErrUnableToFindOutput is returned when an output cannot be found
	ErrUnableToFindOutput = errors.New("unable to find output")
	// ErrMissingSourceTransaction is returned when a source transaction is missing
	ErrMissingSourceTransaction = errors.New("missing source transaction")
	// ErrMissingTransaction is returned when a transaction is missing
	ErrMissingTransaction = errors.New("missing transaction")
	// ErrNoDocumentationFound is returned when no documentation is found
	ErrNoDocumentationFound = errors.New("no documentation found")
)

// Submit submits a transaction to the overlay service
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
	if valid, err := spv.Verify(ctx, tx, e.ChainTracker, nil); err != nil {
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
		}
		topicInputs[topic] = make(map[uint32]*Output, len(tx.Inputs))
		previousCoins := make(map[uint32]*transaction.TransactionOutput, len(tx.Inputs))
		outputs, err := e.Storage.FindOutputs(ctx, inpoints, topic, nil, false)
		if err != nil {
			slog.Error("failed to find outputs", "topic", topic, "error", err)
			return nil, err
		}
		for vin := 0; vin < len(outputs); vin++ {
			output := outputs[vin]
			if output != nil {
				previousCoins[uint32(vin)] = &transaction.TransactionOutput{ //nolint:gosec // index bounded by slice length
					LockingScript: output.Script,
					Satoshis:      output.Satoshis,
				}
				topicInputs[topic][uint32(vin)] = output //nolint:gosec // index bounded by slice length
			}
		}

		admit, err := e.Managers[topic].IdentifyAdmissibleOutputs(ctx, taggedBEEF.Beef, previousCoins)
		if err != nil {
			slog.Error("failed to identify admissible outputs", "topic", topic, "error", err)
			return nil, err
		}
		slog.Debug("admissible outputs identified", "duration", time.Since(start))
		start = time.Now()
		if len(admit.AncillaryTxids) > 0 {
			ancillaryBeef := transaction.Beef{
				Version:      transaction.BEEF_V2,
				Transactions: make(map[chainhash.Hash]*transaction.BeefTx, len(admit.AncillaryTxids)),
			}
			for _, txid := range admit.AncillaryTxids {
				if foundTx := beef.FindTransaction(txid.String()); foundTx == nil {
					missingErr := ErrMissingDependencyTx
					slog.Error("missing dependency transaction", "txid", txid, "error", missingErr)
					return nil, missingErr
				} else if beefBytes, err := foundTx.BEEF(); err != nil {
					slog.Error("failed to get BEEF bytes", "txid", txid, "error", err)
					return nil, err
				} else if err := ancillaryBeef.MergeBeefBytes(beefBytes); err != nil {
					slog.Error("failed to merge BEEF bytes", "txid", txid, "error", err)
					return nil, err
				}
			}
			beefBytes, err := ancillaryBeef.Bytes()
			if err != nil {
				slog.Error("failed to get ancillary BEEF bytes", "topic", topic, "error", err)
				return nil, err
			}
			ancillaryBeefs[topic] = beefBytes
		}
		steak[topic] = &admit
	}

	for _, topic := range taggedBEEF.Topics {
		if _, ok := dupeTopics[topic]; ok {
			continue
		}
		if err := e.Storage.MarkUTXOsAsSpent(ctx, inpoints, topic, txid); err != nil {
			slog.Error("failed to mark UTXOs as spent", "topic", topic, "txid", txid, "error", err)
			return nil, err
		}
		for vin := 0; vin < len(inpoints); vin++ {
			outpoint := inpoints[vin]
			for _, l := range e.LookupServices {
				if err := l.OutputSpent(ctx, &OutputSpent{
					Outpoint:           outpoint,
					Topic:              topic,
					SpendingTxid:       txid,
					InputIndex:         uint32(vin), //nolint:gosec // index bounded by slice length
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
			admit.CoinsRemoved = append(admit.CoinsRemoved, vin)
		}

		newOutpoints := make([]*transaction.Outpoint, 0, len(admit.OutputsToAdmit))
		for _, vout := range admit.OutputsToAdmit {
			out := tx.Outputs[vout]
			output := &Output{
				Outpoint: transaction.Outpoint{
					Txid:  *txid,
					Index: vout,
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

// Lookup performs a lookup query on the overlay service
func (e *Engine) Lookup(ctx context.Context, question *lookup.LookupQuestion) (*lookup.LookupAnswer, error) {
	l, ok := e.LookupServices[question.Service]
	if !ok {
		slog.Error("unknown lookup service", "service", question.Service, "error", ErrUnknownTopic)
		return nil, ErrUnknownTopic
	}
	result, err := l.Lookup(ctx, question)
	if err != nil {
		slog.Error("lookup service failed", "service", question.Service, "error", err)
		return nil, err
	}
	if result.Type == lookup.AnswerTypeFreeform || result.Type == lookup.AnswerTypeOutputList {
		return result, nil
	}
	hydratedOutputs := make([]*lookup.OutputListItem, 0, len(result.Outputs))
	for _, formula := range result.Formulas {
		if output, err := e.Storage.FindOutput(ctx, formula.Outpoint, nil, nil, true); err != nil {
			slog.Error("failed to find output in Lookup", "outpoint", formula.Outpoint.String(), "error", err)
			return nil, err
		} else if output != nil && output.Beef != nil {
			if hydratedOutput, err := e.GetUTXOHistory(ctx, output, formula.History, 0); err != nil {
				slog.Error("failed to get UTXO history in Lookup", "outpoint", formula.Outpoint.String(), "error", err)
				return nil, err
			} else if hydratedOutput != nil {
				hydratedOutputs = append(hydratedOutputs, &lookup.OutputListItem{
					Beef:        hydratedOutput.Beef,
					OutputIndex: hydratedOutput.Outpoint.Index,
				})
			}
		}
	}
	return &lookup.LookupAnswer{
		Type:    lookup.AnswerTypeOutputList,
		Outputs: hydratedOutputs,
	}, nil
}

// GetUTXOHistory retrieves the history of a UTXO
func (e *Engine) GetUTXOHistory(ctx context.Context, output *Output, historySelector func(beef []byte, outputIndex, currentDepth uint32) bool, currentDepth uint32) (*Output, error) {
	if historySelector == nil {
		return output, nil
	}
	shouldTravelHistory := historySelector(output.Beef, output.Outpoint.Index, currentDepth)
	if !shouldTravelHistory {
		return nil, nil //nolint:nilnil // returning nil output with no error is valid when selector returns false
	}
	if output != nil && len(output.OutputsConsumed) == 0 {
		return output, nil
	}
	outputsConsumed := output.OutputsConsumed[:]
	childHistories := make(map[string]*Output, len(outputsConsumed))
	for _, outpoint := range outputsConsumed {
		if childOutput, err := e.Storage.FindOutput(ctx, outpoint, nil, nil, true); err != nil {
			slog.Error("failed to find output in GetUTXOHistory", "outpoint", outpoint.String(), "error", err)
			return nil, err
		} else if childOutput != nil {
			if child, err := e.GetUTXOHistory(ctx, childOutput, historySelector, currentDepth+1); err != nil {
				slog.Error("failed to get child UTXO history", "outpoint", outpoint.String(), "depth", currentDepth+1, "error", err)
				return nil, err
			} else if child != nil {
				childHistories[child.Outpoint.String()] = child
			}
		}
	}

	tx, err := transaction.NewTransactionFromBEEF(output.Beef)
	if err != nil {
		slog.Error("failed to create transaction from BEEF in GetUTXOHistory", "outpoint", output.Outpoint.String(), "error", err)
		return nil, err
	}
	for _, txin := range tx.Inputs {
		outpoint := &transaction.Outpoint{
			Txid:  *txin.SourceTXID,
			Index: txin.SourceTxOutIndex,
		}
		if input := childHistories[outpoint.String()]; input != nil {
			if input.Beef == nil {
				beefErr := ErrMissingBeef
				slog.Error("missing BEEF in GetUTXOHistory", "outpoint", outpoint.String(), "error", beefErr)
				return nil, beefErr
			} else if txin.SourceTransaction, err = transaction.NewTransactionFromBEEF(input.Beef); err != nil {
				slog.Error("failed to create source transaction from BEEF", "outpoint", outpoint.String(), "error", err)
				return nil, err
			}
		}
	}
	beef, err := tx.BEEF()
	if err != nil {
		slog.Error("failed to get BEEF from transaction in GetUTXOHistory", "outpoint", output.Outpoint.String(), "error", err)
		return nil, err
	}
	output.Beef = beef
	return output, nil
}

// SyncAdvertisements synchronizes advertisements from topic managers
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

// StartGASPSync starts the GASP synchronization process
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

				slog.Info("GASP sync starting", "topic", topic, "peer", peer)

				// Read the last interaction score from storage
				lastInteraction, err := e.Storage.GetLastInteraction(ctx, peer, topic)
				if err != nil {
					slog.Error("Failed to get last interaction", "topic", topic, "peer", peer, "error", err)
					return err
				}

				// Create a new GASP provider for each peer to avoid state conflicts
				gaspProvider := gasp.NewGASP(gasp.Params{
					Storage: NewOverlayGASPStorage(topic, e, nil),
					Remote: &OverlayGASPRemote{
						EndpointURL: peer,
						Topic:       topic,
						HTTPClient:  http.DefaultClient,
					},
					LastInteraction: lastInteraction,
					LogPrefix:       &logPrefix,
					Unidirectional:  true,
					Concurrency:     syncEndpoints.Concurrency,
				})

				if err := gaspProvider.Sync(ctx, peer, DefaultGASPSyncLimit); err != nil {
					slog.Error("failed to sync with peer", "topic", topic, "peer", peer, "error", err)
				} else {
					slog.Info("GASP sync successful", "topic", topic, "peer", peer)

					// Save the updated last interaction score
					if gaspProvider.LastInteraction > lastInteraction {
						if err := e.Storage.UpdateLastInteraction(ctx, peer, topic, gaspProvider.LastInteraction); err == nil {
							slog.Info("Updated last interaction score", "topic", topic, "peer", peer, "score", gaspProvider.LastInteraction)
						}
					}
				}
			}
		}
	}
	return nil
}

// ProvideForeignSyncResponse provides a synchronization response for foreign peers
func (e *Engine) ProvideForeignSyncResponse(ctx context.Context, initialRequest *gasp.InitialRequest, topic string) (*gasp.InitialResponse, error) {
	utxos, err := e.Storage.FindUTXOsForTopic(ctx, topic, initialRequest.Since, initialRequest.Limit, false)
	if err != nil {
		slog.Error("failed to find UTXOs for topic in ProvideForeignSyncResponse", "topic", topic, "error", err)
		return nil, err
	}
	// Convert to GASPOutput format
	gaspOutputs := make([]*gasp.Output, 0, len(utxos))
	for _, utxo := range utxos {
		gaspOutputs = append(gaspOutputs, &gasp.Output{
			Txid:        utxo.Outpoint.Txid,
			OutputIndex: utxo.Outpoint.Index,
			Score:       utxo.Score,
		})
	}

	return &gasp.InitialResponse{
		UTXOList: gaspOutputs,
		Since:    initialRequest.Since,
	}, nil
}

// ProvideForeignGASPNode provides a GASP node for foreign peers
func (e *Engine) ProvideForeignGASPNode(ctx context.Context, graphID, outpoint *transaction.Outpoint, topic string) (*gasp.Node, error) {
	var hydrator func(ctx context.Context, output *Output) (*gasp.Node, error)
	hydrator = func(ctx context.Context, output *Output) (*gasp.Node, error) {
		if output.Beef == nil {
			slog.Error("missing BEEF in ProvideForeignGASPNode hydrator", "outpoint", output.Outpoint.String(), "error", ErrMissingInput)
			return nil, ErrMissingInput
		}
		_, tx, _, err := transaction.ParseBeef(output.Beef)
		if err != nil {
			slog.Error("failed to parse BEEF in ProvideForeignGASPNode hydrator", "outpoint", output.Outpoint.String(), "error", err)
			return nil, err
		}
		if tx == nil {
			for _, outpoint := range output.OutputsConsumed {
				if foundOutput, err := e.Storage.FindOutput(ctx, outpoint, &topic, nil, false); err == nil {
					return hydrator(ctx, foundOutput)
				}
			}
			err := ErrUnableToFindOutput
			slog.Error("unable to find output in ProvideForeignGASPNode", "graphID", graphID.String(), "error", err)
			return nil, err
		}
		node := &gasp.Node{
			GraphID:       graphID,
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
	output, err := e.Storage.FindOutput(ctx, graphID, &topic, nil, true)
	if err != nil {
		slog.Error("failed to find output in ProvideForeignGASPNode", "graphID", graphID.String(), "topic", topic, "error", err)
		return nil, err
	}
	if output == nil {
		return nil, ErrMissingOutput
	}
	return hydrator(ctx, output)
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

//nolint:unparam // ctx is used in recursive call at line 823
func (e *Engine) updateInputProofs(ctx context.Context, tx *transaction.Transaction, txid chainhash.Hash, proof *transaction.MerklePath) (err error) {
	if tx.MerklePath != nil {
		tx.MerklePath = proof
		return nil
	}

	if tx.TxID().Equal(txid) {
		tx.MerklePath = proof
	} else {
		for _, input := range tx.Inputs {
			if input.SourceTransaction == nil {
				sourceErr := ErrMissingSourceTransaction
				slog.Error("missing source transaction in updateInputProofs", "txid", txid, "error", sourceErr)
				return sourceErr
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
		err := ErrMissingBeef
		slog.Error("missing BEEF in updateMerkleProof", "outpoint", output.Outpoint.String(), "error", err)
		return err
	}
	beef, tx, _, err := transaction.ParseBeef(output.Beef)
	if err != nil {
		slog.Error("failed to parse BEEF in updateMerkleProof", "outpoint", output.Outpoint.String(), "error", err)
		return err
	} else if tx == nil {
		txErr := ErrMissingTransaction
		slog.Error("missing transaction in updateMerkleProof", "outpoint", output.Outpoint.String(), "error", txErr)
		return txErr
	}
	if tx.MerklePath != nil {
		if oldRoot, rootErr := tx.MerklePath.ComputeRoot(&txid); rootErr != nil {
			slog.Error("failed to compute old merkle root", "txid", txid, "error", rootErr)
			return rootErr
		} else if newRoot, proofErr := proof.ComputeRoot(&txid); proofErr != nil {
			slog.Error("failed to compute new merkle root", "txid", txid, "error", proofErr)
			return proofErr
		} else if oldRoot.Equal(*newRoot) {
			return nil
		}
	}
	if err = e.updateInputProofs(ctx, tx, txid, proof); err != nil {
		slog.Error("failed to update input proofs in updateMerkleProof", "txid", txid, "error", err)
		return err
	}
	atomicBytes, atomicErr := tx.AtomicBEEF(false)
	if atomicErr != nil {
		slog.Error("failed to get atomic BEEF", "txid", txid, "error", atomicErr)
		return atomicErr
	}
	if len(output.AncillaryTxids) > 0 {
		ancillaryBeef := transaction.Beef{
			Version:      transaction.BEEF_V2,
			Transactions: make(map[chainhash.Hash]*transaction.BeefTx, len(output.AncillaryTxids)),
		}
		for _, dep := range output.AncillaryTxids {
			if depTx := beef.FindTransaction(dep.String()); depTx == nil {
				depErr := ErrMissingDependencyTx
				slog.Error("missing dependency transaction in updateMerkleProof", "dep", dep, "error", depErr)
				return depErr
			} else if depBeefBytes, depBeefErr := depTx.BEEF(); depBeefErr != nil {
				slog.Error("failed to get dependency BEEF bytes", "dep", dep, "error", depBeefErr)
				return depBeefErr
			} else if mergeErr := ancillaryBeef.MergeBeefBytes(depBeefBytes); mergeErr != nil {
				slog.Error("failed to merge dependency BEEF bytes", "dep", dep, "error", mergeErr)
				return mergeErr
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
		consumingOutputs, err := e.Storage.FindOutputsForTransaction(ctx, &outpoint.Txid, true)
		if err != nil {
			slog.Error("failed to find consuming outputs", "txid", outpoint.Txid, "error", err)
			return err
		}
		for _, consuming := range consumingOutputs {
			if err := e.updateMerkleProof(ctx, consuming, txid, proof); err != nil {
				slog.Error("failed to update merkle proof for consuming output", "consumingTxid", consuming.Outpoint.Txid, "error", err)
				return err
			}
		}
	}
	return nil
}

// HandleNewMerkleProof handles a new Merkle proof
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
			err := fmt.Errorf("not found in proof: %s", txid) //nolint:err113 // dynamic error needed for context
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

// ListTopicManagers returns a list of topic managers and their metadata
func (e *Engine) ListTopicManagers() map[string]*overlay.MetaData {
	result := make(map[string]*overlay.MetaData, len(e.Managers))
	for name, manager := range e.Managers {
		result[name] = manager.GetMetaData()
	}
	return result
}

// ListLookupServiceProviders returns a list of lookup service providers and their metadata
func (e *Engine) ListLookupServiceProviders() map[string]*overlay.MetaData {
	result := make(map[string]*overlay.MetaData, len(e.LookupServices))
	for name, provider := range e.LookupServices {
		result[name] = provider.GetMetaData()
	}
	return result
}

// GetDocumentationForTopicManager returns documentation for a topic manager
func (e *Engine) GetDocumentationForTopicManager(manager string) (string, error) {
	tm, ok := e.Managers[manager]
	if !ok {
		err := ErrNoDocumentationFound
		slog.Error("topic manager not found", "manager", manager, "error", err)
		return "", err
	}
	return tm.GetDocumentation(), nil
}

// GetDocumentationForLookupServiceProvider returns documentation for a lookup service provider
func (e *Engine) GetDocumentationForLookupServiceProvider(provider string) (string, error) {
	l, ok := e.LookupServices[provider]
	if !ok {
		err := ErrNoDocumentationFound
		slog.Error("lookup service provider not found", "provider", provider, "error", err)
		return "", err
	}
	return l.GetDocumentation(), nil
}
