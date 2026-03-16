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
	"sync"
	"time"

	"github.com/bsv-blockchain/go-sdk/chainhash"
	"github.com/bsv-blockchain/go-sdk/overlay"
	"github.com/bsv-blockchain/go-sdk/overlay/lookup"
	"github.com/bsv-blockchain/go-sdk/overlay/topic"
	"github.com/bsv-blockchain/go-sdk/spv"
	"github.com/bsv-blockchain/go-sdk/transaction"
	"github.com/bsv-blockchain/go-sdk/transaction/chaintracker"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/advertiser"
	"github.com/bsv-blockchain/go-overlay-services/pkg/core/gasp"
)

// DefaultGASPSyncLimit is the default limit for GASP synchronization
const DefaultGASPSyncLimit = 1000

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

// String returns the string representation of SyncConfigurationType
func (s SyncConfigurationType) String() string {
	switch s {
	case SyncConfigurationPeers:
		return "Peers"
	case SyncConfigurationSHIP:
		return "SHIP"
	case SyncConfigurationNone:
		return "None"
	default:
		return "Unknown"
	}
}

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
	// managers holds the registered topic managers (access via thread-safe methods)
	managers map[string]TopicManager
	// lookupServices holds the registered lookup services (access via thread-safe methods)
	lookupServices          map[string]LookupService
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
	OnAdmission             func(txid *chainhash.Hash, steak *overlay.Steak, beef []byte)

	// mu protects managers and lookupServices maps for concurrent access
	mu sync.RWMutex
}

// Config holds configuration for creating a new Engine.
// Use NewEngine with this config to create an Engine instance.
type Config struct {
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
}

// NewEngine creates and returns a new Engine instance
func NewEngine(cfg *Config) *Engine {
	if cfg == nil {
		cfg = &Config{}
	}

	e := &Engine{
		managers:                make(map[string]TopicManager),
		lookupServices:          make(map[string]LookupService),
		Storage:                 cfg.Storage,
		ChainTracker:            cfg.ChainTracker,
		HostingURL:              cfg.HostingURL,
		SHIPTrackers:            cfg.SHIPTrackers,
		SLAPTrackers:            cfg.SLAPTrackers,
		Broadcaster:             cfg.Broadcaster,
		Advertiser:              cfg.Advertiser,
		SyncConfiguration:       cfg.SyncConfiguration,
		LogTime:                 cfg.LogTime,
		LogPrefix:               cfg.LogPrefix,
		ErrorOnBroadcastFailure: cfg.ErrorOnBroadcastFailure,
		BroadcastFacilitator:    cfg.BroadcastFacilitator,
		LookupResolver:          cfg.LookupResolver,
	}

	if e.SyncConfiguration == nil {
		e.SyncConfiguration = make(map[string]SyncConfiguration)
	}
	if e.LookupResolver == nil {
		e.LookupResolver = NewLookupResolver()
	}

	// Register managers using thread-safe method
	for name, manager := range cfg.Managers {
		e.managers[name] = manager
	}

	// Register lookup services using thread-safe method
	for name, service := range cfg.LookupServices {
		e.lookupServices[name] = service
	}

	// Process sync configuration for tm_ship and tm_slap
	for name, manager := range cfg.Managers {
		config := e.SyncConfiguration[name]

		if name == "tm_ship" && len(e.SHIPTrackers) > 0 && manager != nil && config.Type == SyncConfigurationPeers {
			combined := make(map[string]struct{}, len(e.SHIPTrackers)+len(config.Peers))
			for _, peer := range e.SHIPTrackers {
				combined[peer] = struct{}{}
			}
			for _, peer := range config.Peers {
				combined[peer] = struct{}{}
			}
			config.Peers = make([]string, 0, len(combined))
			for peer := range combined {
				config.Peers = append(config.Peers, peer)
			}
			e.SyncConfiguration[name] = config
		} else if name == "tm_slap" && len(e.SLAPTrackers) > 0 && manager != nil && config.Type == SyncConfigurationPeers {
			combined := make(map[string]struct{}, len(e.SHIPTrackers)+len(config.Peers))
			for _, peer := range e.SLAPTrackers {
				combined[peer] = struct{}{}
			}
			for _, peer := range config.Peers {
				combined[peer] = struct{}{}
			}
			config.Peers = make([]string, 0, len(combined))
			for peer := range combined {
				config.Peers = append(config.Peers, peer)
			}
			e.SyncConfiguration[name] = config
		}
	}

	return e
}

// RegisterTopicManager adds a topic manager (thread-safe)
func (e *Engine) RegisterTopicManager(name string, manager TopicManager) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.managers[name] = manager
}

// UnregisterTopicManager removes a topic manager (thread-safe)
func (e *Engine) UnregisterTopicManager(name string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.managers, name)
}

// GetTopicManager returns a topic manager by name (thread-safe)
func (e *Engine) GetTopicManager(name string) (TopicManager, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	tm, ok := e.managers[name]
	return tm, ok
}

// HasTopicManager checks if a topic manager exists (thread-safe)
func (e *Engine) HasTopicManager(name string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	_, ok := e.managers[name]
	return ok
}

// RegisterLookupService adds a lookup service (thread-safe)
func (e *Engine) RegisterLookupService(name string, service LookupService) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.lookupServices[name] = service
}

// UnregisterLookupService removes a lookup service (thread-safe)
func (e *Engine) UnregisterLookupService(name string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.lookupServices, name)
}

// GetLookupService returns a lookup service by name (thread-safe)
func (e *Engine) GetLookupService(name string) (LookupService, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	ls, ok := e.lookupServices[name]
	return ls, ok
}

// HasLookupService checks if a lookup service exists (thread-safe)
func (e *Engine) HasLookupService(name string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	_, ok := e.lookupServices[name]
	return ok
}

// getLookupServicesSnapshot returns a snapshot of lookup services for safe iteration
func (e *Engine) getLookupServicesSnapshot() []LookupService {
	e.mu.RLock()
	defer e.mu.RUnlock()
	services := make([]LookupService, 0, len(e.lookupServices))
	for _, ls := range e.lookupServices {
		services = append(services, ls)
	}
	return services
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
	// ErrInvalidMerkleProof is returned when a merkle proof is invalid
	ErrInvalidMerkleProof = errors.New("invalid merkle proof")
)

// Submit submits a transaction to the overlay service
func (e *Engine) Submit(ctx context.Context, taggedBEEF overlay.TaggedBEEF, mode SumbitMode, onSteakReady OnSteakReady) (overlay.Steak, error) {
	// Parse the BEEF bytes once at the entry point
	beef, tx, txid, err := transaction.ParseBeef(taggedBEEF.Beef)
	if err != nil {
		slog.Error("failed to parse BEEF in Submit", "error", err)
		return nil, err
	} else if tx == nil {
		slog.Error("invalid BEEF in Submit - tx is nil", "error", ErrInvalidBeef)
		return nil, ErrInvalidBeef
	}
	// Delegate to SubmitParsedBeef with the parsed objects
	return e.SubmitParsedBeef(ctx, beef, txid, taggedBEEF.Topics, taggedBEEF.Beef, taggedBEEF.OffChainValues, mode, onSteakReady)
}

// SubmitParsedBeef processes a pre-parsed BEEF transaction for submission to overlay topics.
// This is the core submission logic; Submit() is a convenience wrapper that parses TaggedBEEF first.
// The atomicBeef parameter is the original serialized bytes for use in lookup service notifications.
func (e *Engine) SubmitParsedBeef(ctx context.Context, beef *transaction.Beef, txid *chainhash.Hash, topics []string, atomicBeef []byte, offChainValues []byte, mode SumbitMode, onSteakReady OnSteakReady) (overlay.Steak, error) {
	// Validate topics exist and get managers snapshot (thread-safe)
	e.mu.RLock()
	managers := make(map[string]TopicManager, len(topics))
	for _, topic := range topics {
		manager, ok := e.managers[topic]
		if !ok {
			e.mu.RUnlock()
			slog.Error("unknown topic in Submit", "topic", topic, "error", ErrUnknownTopic)
			return nil, ErrUnknownTopic
		}
		managers[topic] = manager
	}
	e.mu.RUnlock()

	// Get the transaction from the parsed BEEF
	tx := beef.FindTransactionForSigningByHash(txid)
	if tx == nil {
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
	steak := make(overlay.Steak, len(topics))
	topicInputs := make(map[string]map[uint32]*Output, len(tx.Inputs))
	inpoints := make([]*transaction.Outpoint, 0, len(tx.Inputs))
	for _, input := range tx.Inputs {
		inpoints = append(inpoints, &transaction.Outpoint{
			Txid:  *input.SourceTXID,
			Index: input.SourceTxOutIndex,
		})
	}
	dupeTopics := make(map[string]struct{}, len(topics))
	for _, topic := range topics {
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
		previousCoins := make([]uint32, 0, len(tx.Inputs))
		outputs, err := e.Storage.FindOutputs(ctx, inpoints, topic, nil, true)
		if err != nil {
			slog.Error("failed to find outputs", "topic", topic, "error", err)
			return nil, err
		}
		for vin, output := range outputs {
			if output != nil {
				if output.Beef != nil {
					if mergeErr := beef.MergeBeef(output.Beef); mergeErr != nil {
						return nil, fmt.Errorf("failed to merge BEEF for input %d: %w", vin, mergeErr)
					}
				}
				previousCoins = append(previousCoins, uint32(vin))
				topicInputs[topic][uint32(vin)] = output
			}
		}
		// Clone beef so topic managers cannot modify the shared instance
		topicBeef := beef.Clone()
		admit, err := managers[topic].IdentifyAdmissibleOutputs(ctx, topicBeef, txid, previousCoins)
		if err != nil {
			slog.Error("failed to identify admissible outputs", "txid", txid.String(), "topic", topic, "mode", string(mode), "error", err)
			return nil, err
		}
		steak[topic] = &admit
	}

	for _, topic := range topics {
		if _, ok := dupeTopics[topic]; ok {
			continue
		}
		// Build list of inputs that actually exist in this topic's storage
		if len(topicInputs[topic]) > 0 {
			topicInpoints := make([]*transaction.Outpoint, 0, len(topicInputs[topic]))
			for _, output := range topicInputs[topic] {
				topicInpoints = append(topicInpoints, &output.Outpoint)
			}
			if err := e.Storage.MarkUTXOsAsSpent(ctx, topicInpoints, topic, txid); err != nil {
				slog.Error("failed to mark UTXOs as spent", "topic", topic, "txid", txid, "error", err)
				return nil, err
			}
		}
		// Notify lookup services about spent outputs
		lookupServices := e.getLookupServicesSnapshot()
		for vin, output := range topicInputs[topic] {
			for _, l := range lookupServices {
				if err := l.OutputSpent(ctx, &OutputSpent{
					Outpoint:           &output.Outpoint,
					Topic:              topic,
					SpendingTxid:       txid,
					InputIndex:         vin,
					UnlockingScript:    tx.Inputs[vin].UnlockingScript,
					SequenceNumber:     tx.Inputs[vin].SequenceNumber,
					SpendingAtomicBEEF: atomicBeef,
				}); err != nil {
					slog.Error("failed to notify lookup service about spent output", "topic", topic, "txid", txid, "error", err)
					return nil, err
				}
			}
		}
	}
	if mode != SubmitModeHistorical && e.Broadcaster != nil {
		if _, failure := e.Broadcaster.Broadcast(tx); failure != nil {
			slog.Error("failed to broadcast transaction", "txid", txid, "mode", string(mode), "error", failure)
			return nil, failure
		}
	}

	if onSteakReady != nil {
		onSteakReady(&steak)
	}

	if mode != SubmitModeHistorical && e.OnAdmission != nil {
		e.OnAdmission(txid, &steak, atomicBeef)
	}

	for _, topic := range topics {
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

		// Insert all outputs in a single batch call
		if err := e.Storage.InsertOutputs(ctx, topic, txid, admit.OutputsToAdmit, outpointsConsumed, beef, admit.AncillaryTxids); err != nil {
			slog.Error("failed to insert outputs", "topic", topic, "txid", txid.String(), "error", err)
			return nil, err
		}

		// Build outpoints for consumed-by tracking and notify lookup services
		newOutpoints := make([]*transaction.Outpoint, 0, len(admit.OutputsToAdmit))
		lookupServicesForAdmit := e.getLookupServicesSnapshot()
		for _, vout := range admit.OutputsToAdmit {
			outpoint := &transaction.Outpoint{Txid: *txid, Index: vout}
			newOutpoints = append(newOutpoints, outpoint)
			for _, l := range lookupServicesForAdmit {
				if err := l.OutputAdmittedByTopic(ctx, &OutputAdmittedByTopic{
					Topic:          topic,
					OutputIndex:    vout,
					AtomicBEEF:     atomicBeef,
					OffChainValues: offChainValues,
				}); err != nil {
					slog.Error("failed to notify lookup service about admitted output", "topic", topic, "outpoint", outpoint.String(), "error", err)
					return nil, err
				}
			}
		}

		for _, output := range outputsConsumed {
			output.ConsumedBy = append(output.ConsumedBy, newOutpoints...)

			if err := e.Storage.UpdateConsumedBy(ctx, &output.Outpoint, output.Topic, output.ConsumedBy); err != nil {
				slog.Error("failed to update consumed by", "topic", output.Topic, "outpoint", output.Outpoint.String(), "error", err)
				return nil, err
			}
		}

		if err := e.Storage.InsertAppliedTransaction(ctx, &overlay.AppliedTransaction{
			Txid:  txid,
			Topic: topic,
		}); err != nil {
			slog.Error("failed to insert applied transaction", "topic", topic, "txid", txid, "error", err)
			return nil, err
		}
	}
	if e.Advertiser == nil || mode == SubmitModeHistorical {
		return steak, nil
	}

	releventTopics := make([]string, 0, len(topics))
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
	l, ok := e.GetLookupService(question.Service)
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
		output, err := e.Storage.FindOutput(ctx, formula.Outpoint, nil, nil, true)
		if err != nil {
			slog.Error("failed to find output in Lookup", "outpoint", formula.Outpoint.String(), "error", err)
			return nil, err
		}
		if output != nil && output.Beef != nil {
			// Load ancillary transactions into the BEEF for full SPV verification
			if err := e.Storage.LoadAncillaryBeef(ctx, output); err != nil {
				slog.Error("failed to load ancillary beef in Lookup", "outpoint", formula.Outpoint.String(), "error", err)
				return nil, err
			}
			hydratedOutput, err := e.GetUTXOHistory(ctx, output, formula.History, 0)
			if err != nil {
				slog.Error("failed to get UTXO history in Lookup", "outpoint", formula.Outpoint.String(), "error", err)
				return nil, err
			}
			if hydratedOutput != nil && hydratedOutput.Beef != nil {
				beefBytes, err := hydratedOutput.Beef.AtomicBytes(&hydratedOutput.Outpoint.Txid)
				if err != nil {
					slog.Error("failed to serialize BEEF in Lookup", "outpoint", formula.Outpoint.String(), "error", err)
					return nil, err
				}
				hydratedOutputs = append(hydratedOutputs, &lookup.OutputListItem{
					Beef:        beefBytes,
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
func (e *Engine) GetUTXOHistory(ctx context.Context, output *Output, historySelector func(beef *transaction.Beef, outputIndex uint32, currentDepth uint32) bool, currentDepth uint32) (*Output, error) {
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
		childOutput, err := e.Storage.FindOutput(ctx, outpoint, nil, nil, true)
		if err != nil {
			slog.Error("failed to find output in GetUTXOHistory", "outpoint", outpoint.String(), "error", err)
			return nil, err
		}
		if childOutput != nil {
			// Load ancillary transactions into the BEEF for full SPV verification
			if err := e.Storage.LoadAncillaryBeef(ctx, childOutput); err != nil {
				slog.Error("failed to load ancillary beef in GetUTXOHistory", "outpoint", outpoint.String(), "error", err)
				return nil, err
			}
			child, err := e.GetUTXOHistory(ctx, childOutput, historySelector, currentDepth+1)
			if err != nil {
				slog.Error("failed to get child UTXO history", "outpoint", outpoint.String(), "depth", currentDepth+1, "error", err)
				return nil, err
			}
			if child != nil {
				childHistories[child.Outpoint.String()] = child
			}
		}
	}

	tx := output.Beef.FindTransactionForSigningByHash(&output.Outpoint.Txid)
	if tx == nil {
		slog.Error("failed to find transaction in BEEF in GetUTXOHistory", "outpoint", output.Outpoint.String())
		return nil, ErrMissingBeef
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
			}
			txin.SourceTransaction = input.Beef.FindTransactionForSigningByHash(&outpoint.Txid)
			if txin.SourceTransaction == nil {
				slog.Error("failed to find source transaction in BEEF", "outpoint", outpoint.String())
				return nil, ErrMissingBeef
			}
		}
	}
	beefBytes, err := tx.BEEF()
	if err != nil {
		slog.Error("failed to get BEEF from transaction in GetUTXOHistory", "outpoint", output.Outpoint.String(), "error", err)
		return nil, err
	}
	output.Beef, _, _, err = transaction.ParseBeef(beefBytes)
	if err != nil {
		slog.Error("failed to parse rebuilt BEEF in GetUTXOHistory", "outpoint", output.Outpoint.String(), "error", err)
		return nil, err
	}
	return output, nil
}

// SyncAdvertisements synchronizes advertisements from topic managers
func (e *Engine) SyncAdvertisements(ctx context.Context) error {
	if e.Advertiser == nil {
		return nil
	}
	// Take snapshot of configured topics and services under read lock
	e.mu.RLock()
	requiredSHIPAdvertisements := make(map[string]struct{}, len(e.managers))
	for name := range e.managers {
		requiredSHIPAdvertisements[name] = struct{}{}
	}
	requiredSLAPAdvertisements := make(map[string]struct{}, len(e.lookupServices))
	for name := range e.lookupServices {
		requiredSLAPAdvertisements[name] = struct{}{}
	}
	e.mu.RUnlock()
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

		slog.Info(fmt.Sprintf("[GASP SYNC] Processing topic \"%s\" with sync type \"%s\"", topic, syncEndpoints.Type))

		if syncEndpoints.Type == SyncConfigurationSHIP {
			slog.Info(fmt.Sprintf("[GASP SYNC] Discovering peers for topic \"%s\" using SHIP lookup", topic))
			slog.Info(fmt.Sprintf("[GASP SYNC] Setting SLAP trackers for topic \"%s\", count: %d", topic, len(e.SLAPTrackers)))
			if len(e.SLAPTrackers) > 0 {
				for i, tracker := range e.SLAPTrackers {
					slog.Info(fmt.Sprintf("[GASP SYNC] SLAP tracker %d: %s", i, tracker))
				}
			} else {
				slog.Warn(fmt.Sprintf("[GASP SYNC] No SLAP trackers configured for topic \"%s\"", topic))
			}
			e.LookupResolver.SetSLAPTrackers(e.SLAPTrackers)
			slog.Debug(fmt.Sprintf("[GASP SYNC] Current SLAP trackers after setting: %v", e.LookupResolver.SLAPTrackers()))

			query, err := json.Marshal(map[string]any{"topics": []string{topic}})
			if err != nil {
				slog.Error("failed to marshal query for GASP sync", "topic", topic, "error", err)
				return err
			}

			slog.Info(fmt.Sprintf("[GASP SYNC] Querying lookup resolver for topic \"%s\" with service \"ls_ship\"", topic))
			slog.Debug(fmt.Sprintf("[GASP SYNC] Query payload: %s", string(query)))

			timeoutCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
			defer cancel()

			slog.Debug(fmt.Sprintf("[GASP SYNC] About to call LookupResolver.Query for topic \"%s\"", topic))
			lookupAnswer, err := e.LookupResolver.Query(timeoutCtx, &lookup.LookupQuestion{Service: "ls_ship", Query: query})
			slog.Debug(fmt.Sprintf("[GASP SYNC] LookupResolver.Query returned for topic \"%s\", err: %v", topic, err))

			if err != nil {
				slog.Error("failed to query lookup resolver for GASP sync", "topic", topic, "error", err)
				return err
			}

			slog.Info(fmt.Sprintf("[GASP SYNC] Lookup query completed for topic \"%s\", answer type: %s, outputs count: %d", topic, lookupAnswer.Type, len(lookupAnswer.Outputs)))

			if lookupAnswer.Type == lookup.AnswerTypeOutputList {
				endpointSet := make(map[string]struct{}, len(lookupAnswer.Outputs))
				for _, output := range lookupAnswer.Outputs {
					beef, _, txID, err := transaction.ParseBeef(output.Beef)
					if err != nil {
						slog.Error("failed to parse advertisement output BEEF", "topic", topic, "error", err)
						continue
					} else if txID == nil {
						slog.Error("error parsing advertisement output BEEF, no txID", "topic", topic)
						continue
					}
					slog.Info(fmt.Sprintf("[GASP SYNC] Successfully parsed BEEF for topic \"%s\", transaction count: %d, txID: %s\n", topic, len(beef.Transactions), txID.String()))

					// Find the transaction that matches the txid
					tx := beef.FindTransactionByHash(txID)
					if tx == nil {
						slog.Error("failed to find transaction with matching txid in BEEF", "topic", topic, "txid", txID.String())
						continue
					}

					// Verify the output index exists
					if tx.Outputs == nil || len(tx.Outputs) <= int(output.OutputIndex) {
						slog.Error("transaction found but output index out of bounds", "topic", topic, "txid", txID.String(), "outputIndex", output.OutputIndex, "outputsLength", len(tx.Outputs))
						continue
					}

					if tx.Outputs[output.OutputIndex] == nil {
						slog.Error("output at index is nil", "topic", topic, "outputIndex", output.OutputIndex)
						continue
					}

					if tx.Outputs[output.OutputIndex].LockingScript == nil {
						slog.Error("locking script is nil", "topic", topic, "outputIndex", output.OutputIndex)
						continue
					}

					if e.Advertiser == nil {
						slog.Warn("advertiser is nil, skipping advertisement parsing", "topic", topic)
						continue
					}

					slog.Debug("parsing advertisement from locking script", "topic", topic, "outputIndex", output.OutputIndex)
					advertisement, err := e.Advertiser.ParseAdvertisement(tx.Outputs[output.OutputIndex].LockingScript)
					if err != nil {
						slog.Error("failed to parse advertisement from locking script", "topic", topic, "error", err)
						continue
					}

					if advertisement == nil {
						slog.Debug("advertisement parsed as nil", "topic", topic)
						continue
					}

					slog.Debug("successfully parsed advertisement", "topic", topic, "protocol", advertisement.Protocol, "domain", advertisement.Domain)

					// Determine expected protocol based on topic
					var expectedProtocol overlay.Protocol
					switch topic {
					case "tm_ship":
						expectedProtocol = overlay.ProtocolSHIP
					case "tm_slap":
						expectedProtocol = overlay.ProtocolSLAP
					default:
						slog.Warn("unknown topic, cannot determine expected protocol", "topic", topic)
						continue
					}

					if advertisement.Protocol == expectedProtocol {
						slog.Debug("found matching advertisement", "topic", topic, "protocol", advertisement.Protocol, "domain", advertisement.Domain)
						endpointSet[advertisement.Domain] = struct{}{}
					} else {
						slog.Debug("skipping advertisement with mismatched protocol", "topic", topic, "expected", expectedProtocol, "actual", advertisement.Protocol, "domain", advertisement.Domain)
					}
				}

				syncEndpoints.Peers = make([]string, 0, len(endpointSet))
				for endpoint := range endpointSet {
					if endpoint != e.HostingURL {
						syncEndpoints.Peers = append(syncEndpoints.Peers, endpoint)
					}
				}
				// Determine protocol name for logging
				var protocolName string
				switch topic {
				case "tm_ship":
					protocolName = "SHIP"
				case "tm_slap":
					protocolName = "SLAP"
				default:
					protocolName = "UNKNOWN"
				}
				slog.Info(fmt.Sprintf("[GASP SYNC] Discovered %d unique %s peer endpoint(s) for topic \"%s\"", len(syncEndpoints.Peers), protocolName, topic))
			} else {
				slog.Warn(fmt.Sprintf("[GASP SYNC] Unexpected answer type \"%s\" for topic \"%s\", expected \"%s\"", lookupAnswer.Type, topic, lookup.AnswerTypeOutputList))
			}
		} else {
			slog.Info(fmt.Sprintf("[GASP SYNC] Skipping topic peer discovery \"%s\" - sync type is not SHIP (type: \"%s\")", topic, syncEndpoints.Type))
		}

		if len(syncEndpoints.Peers) > 0 {
			// Log the number of peers we will attempt to sync with
			plural := ""
			if len(syncEndpoints.Peers) != 1 {
				plural = "s"
			}
			slog.Info(fmt.Sprintf("[GASP SYNC] Will attempt to sync with %d peer%s", len(syncEndpoints.Peers), plural), "topic", topic)
		} else {
			slog.Info(fmt.Sprintf("[GASP SYNC] No peers found for topic \"%s\", skipping sync", topic))
			continue
		}

		for _, peer := range syncEndpoints.Peers {
			logPrefix := "[GASP Sync of " + topic + " with " + peer + "]"

			slog.Info(fmt.Sprintf("[GASP SYNC] Starting sync for topic \"%s\" with peer \"%s\"", topic, peer))

			// Read the last interaction score from storage
			lastInteraction, err := e.Storage.GetLastInteraction(ctx, peer, topic)
			if err != nil {
				slog.Error("Failed to get last interaction", "topic", topic, "peer", peer, "error", err)
				return err
			}

			// Create a GASP provider for this peer
			gaspProvider := gasp.NewGASP(gasp.Params{ //nolint:contextcheck // NewGASP spawns a long-lived worker
				Storage:         NewOverlayGASPStorage(topic, e, nil),
				Remote:          NewOverlayGASPRemote(peer, topic, http.DefaultClient, 8),
				LastInteraction: lastInteraction,
				LogPrefix:       &logPrefix,
				Unidirectional:  true,
				Concurrency:     syncEndpoints.Concurrency,
				Topic:           topic,
			})
			defer gaspProvider.Close()

			// Paginate through GASP sync, saving progress after each successful page
			for {
				previousLastInteraction := gaspProvider.LastInteraction

				// Sync one page
				if err := gaspProvider.Sync(ctx, peer, DefaultGASPSyncLimit); err != nil {
					slog.Error("failed to sync with peer", "topic", topic, "peer", peer, "error", err)
					break // Exit loop on error
				}

				// Save progress after successful page
				if gaspProvider.LastInteraction > previousLastInteraction {
					if err := e.Storage.UpdateLastInteraction(ctx, peer, topic, gaspProvider.LastInteraction); err != nil {
						slog.Error("Failed to update last interaction", "topic", topic, "peer", peer, "error", err)
						// Continue anyway - we don't want to lose progress
					}
				} else {
					// No progress made, we're done syncing
					slog.Info(logPrefix + " Sync completed")
					break
				}
			}
		}
	}
	return nil
}

// SyncInvalidatedOutputs finds outputs with invalidated merkle proofs and syncs them with remote peers
func (e *Engine) SyncInvalidatedOutputs(ctx context.Context, topic string) error {
	// Find outpoints with invalidated merkle proofs
	invalidatedOutpoints, err := e.Storage.FindOutpointsByMerkleState(ctx, topic, MerkleStateInvalidated, 1000)
	if err != nil {
		slog.Error("Failed to find invalidated outputs", "topic", topic, "error", err)
		return err
	}

	if len(invalidatedOutpoints) == 0 {
		return nil
	}

	// Get sync configuration for this topic
	syncConfig, ok := e.SyncConfiguration[topic]
	if !ok || len(syncConfig.Peers) == 0 {
		slog.Warn("No peers configured for topic", "topic", topic)
		return nil
	}

	// Group outpoints by transaction ID to avoid duplicate merkle proof requests
	txidsToUpdate := make(map[chainhash.Hash]*transaction.Outpoint)
	for _, outpoint := range invalidatedOutpoints {
		if _, exists := txidsToUpdate[outpoint.Txid]; !exists {
			// Use the first outpoint for this txid as representative
			txidsToUpdate[outpoint.Txid] = outpoint
		}
	}

	// Try to get updated merkle proofs from peers
	var successCount int

	// For each transaction that needs updating
	for txid, outpoint := range txidsToUpdate {
		var syncSuccess bool

		// Try each peer until we get a valid merkle proof for this transaction
		for _, peer := range syncConfig.Peers {
			if peer == e.HostingURL {
				continue // Skip self
			}

			// Create a remote client for this peer
			remote := NewOverlayGASPRemote(peer, topic, http.DefaultClient, 8)

			// Request node with metadata to get merkle proof
			node, err := remote.RequestNode(ctx, outpoint, outpoint, true)
			if err != nil {
				continue // Try next peer
			}

			// If we got a merkle proof, update it for the transaction
			if node.Proof != nil {

				merklePath, err := transaction.NewMerklePathFromHex(*node.Proof)
				if err != nil {
					slog.Error("Failed to parse merkle proof", "txid", txid.String(), "error", err)
					continue // Try next peer
				}

				// Update the merkle proof using the existing handler (updates all outputs for this transaction)
				if err := e.HandleNewMerkleProof(ctx, &txid, merklePath); err != nil {
					slog.Error("Failed to update merkle proof", "txid", txid.String(), "error", err)
					continue // Try next peer
				}

				successCount++
				syncSuccess = true
				break // Got valid proof, move to next transaction
			}
		}

		if !syncSuccess {
			slog.Warn("Failed to sync transaction from any peer", "txid", txid.String(), "peers_tried", len(syncConfig.Peers))
		}
	}

	if successCount == 0 && len(txidsToUpdate) > 0 {
		slog.Warn("Could not update all invalidated outputs", "topic", topic, "remaining", len(txidsToUpdate))
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
func (e *Engine) ProvideForeignGASPNode(ctx context.Context, graphID *transaction.Outpoint, outpoint *transaction.Outpoint, topic string) (*gasp.Node, error) {
	slog.Debug("ProvideForeignGASPNode called",
		"graphID", graphID.String(),
		"outpoint", outpoint.String(),
		"topic", topic)

	var depth uint32
	var hydrator func(ctx context.Context, output *Output) (*gasp.Node, error)
	hydrator = func(ctx context.Context, output *Output) (*gasp.Node, error) {
		if output.Beef == nil {
			slog.Error("missing BEEF in ProvideForeignGASPNode hydrator", "outpoint", output.Outpoint.String(), "error", ErrMissingInput)
			return nil, ErrMissingInput
		}

		// Load ancillary transactions into the BEEF for full SPV verification
		if err := e.Storage.LoadAncillaryBeef(ctx, output); err != nil {
			slog.Error("failed to load ancillary beef in ProvideForeignGASPNode hydrator", "outpoint", output.Outpoint.String(), "error", err)
			return nil, err
		}

		// Search through the BEEF transaction tree
		// If found in BEEF, return the node
		if correctTx := output.Beef.FindTransactionByHash(&outpoint.Txid); correctTx != nil {
			node := &gasp.Node{
				GraphID:     graphID,
				RawTx:       correctTx.Hex(),
				OutputIndex: outpoint.Index,
			}
			if correctTx.MerklePath != nil {
				proof := correctTx.MerklePath.Hex()
				node.Proof = &proof
			}
			return node, nil
		}

		// TODO: recursive lookups of missing transactions is very heavy. Skipping recursive for now
		if depth == 0 {
			depth++
			// If not found in BEEF, recursively search through outputsConsumed
			for _, consumedOutpoint := range output.OutputsConsumed {
				if consumedOutput, err := e.Storage.FindOutput(ctx, consumedOutpoint, &topic, nil, true); err == nil && consumedOutput != nil {
					if node, err := hydrator(ctx, consumedOutput); err == nil {
						return node, nil
					}
				}
			}
		}

		return nil, ErrMissingOutput
	}
	output, err := e.Storage.FindOutput(ctx, graphID, &topic, nil, true)
	if err != nil {
		slog.Error("failed to find output in ProvideForeignGASPNode",
			"graphID", graphID.String(),
			"outpoint", outpoint.String(),
			"topic", topic,
			"error", err)
		return nil, err
	}
	if output == nil {
		slog.Warn("Output not found in storage",
			"graphID", graphID.String(),
			"outpoint", outpoint.String(),
			"topic", topic)
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
		lookupServices := e.getLookupServicesSnapshot()
		for _, l := range lookupServices {
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

func (e *Engine) updateInputProofs(ctx context.Context, tx *transaction.Transaction, txid chainhash.Hash, proof *transaction.MerklePath) (err error) { //nolint:unparam // ctx passed through recursive calls
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
	if output.Beef == nil {
		err := ErrMissingBeef
		slog.Error("missing BEEF in updateMerkleProof", "outpoint", output.Outpoint.String(), "error", err)
		return err
	}
	tx := output.Beef.FindTransactionForSigningByHash(&output.Outpoint.Txid)
	if tx == nil {
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
	if err := e.updateInputProofs(ctx, tx, txid, proof); err != nil {
		slog.Error("failed to update input proofs in updateMerkleProof", "txid", txid, "error", err)
		return err
	}
	atomicBytes, atomicErr := tx.AtomicBEEF(false)
	if atomicErr != nil {
		slog.Error("failed to get atomic BEEF", "txid", txid, "error", atomicErr)
		return atomicErr
	}
	updatedBeef, _, _, parseErr := transaction.ParseBeef(atomicBytes)
	if parseErr != nil {
		slog.Error("failed to parse updated BEEF", "txid", txid, "error", parseErr)
		return parseErr
	}

	output.BlockHeight = proof.BlockHeight
	for _, leaf := range proof.Path[0] {
		if leaf.Hash != nil && leaf.Hash.Equal(output.Outpoint.Txid) {
			output.BlockIdx = leaf.Offset
			break
		}
	}
	if err := e.Storage.UpdateTransactionBEEF(ctx, &output.Outpoint.Txid, updatedBeef); err != nil {
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
			// Check if consuming transaction has its own merkle path
			// If it does, it's mined and doesn't include parent transactions anymore
			if consuming.Beef != nil {
				consumingTx := consuming.Beef.FindTransactionForSigningByHash(&consuming.Outpoint.Txid)
				if consumingTx != nil && consumingTx.MerklePath != nil {
					continue
				}
			}

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
	// Validate the merkle proof before processing
	if merkleRoot, err := proof.ComputeRoot(txid); err != nil {
		slog.Error("failed to compute merkle root from proof", "txid", txid, "error", err)
		return err
	} else if valid, err := e.ChainTracker.IsValidRootForHeight(ctx, merkleRoot, proof.BlockHeight); err != nil {
		slog.Error("error validating merkle root for height", "txid", txid, "blockHeight", proof.BlockHeight, "error", err)
		return err
	} else if !valid {
		slog.Error("merkle proof validation failed", "txid", txid, "blockHeight", proof.BlockHeight)
		return fmt.Errorf("%w: transaction %s at block height %d", ErrInvalidMerkleProof, txid, proof.BlockHeight)
	}

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
			} else if err := e.Storage.UpdateOutputBlockHeight(ctx, &output.Outpoint, output.Topic, output.BlockHeight, output.BlockIdx); err != nil {
				slog.Error("failed to update output block height", "outpoint", output.Outpoint.String(), "error", err)
				return err
			}
		}
		lookupServices := e.getLookupServicesSnapshot()
		for _, l := range lookupServices {
			if err := l.OutputBlockHeightUpdated(ctx, txid, blockHeight, *blockIdx); err != nil {
				slog.Error("failed to notify lookup service about block height update", "txid", txid, "blockHeight", blockHeight, "error", err)
				return err
			}
		}
	}
	return nil
}

// ListTopicManagers returns a list of topic managers and their metadata (thread-safe)
func (e *Engine) ListTopicManagers() map[string]*overlay.MetaData {
	e.mu.RLock()
	defer e.mu.RUnlock()
	result := make(map[string]*overlay.MetaData, len(e.managers))
	for name, manager := range e.managers {
		result[name] = manager.GetMetaData()
	}
	return result
}

// ListLookupServiceProviders returns a list of lookup service providers and their metadata (thread-safe)
func (e *Engine) ListLookupServiceProviders() map[string]*overlay.MetaData {
	e.mu.RLock()
	defer e.mu.RUnlock()
	result := make(map[string]*overlay.MetaData, len(e.lookupServices))
	for name, provider := range e.lookupServices {
		result[name] = provider.GetMetaData()
	}
	return result
}

// GetDocumentationForTopicManager returns documentation for a topic manager (thread-safe)
func (e *Engine) GetDocumentationForTopicManager(manager string) (string, error) {
	tm, ok := e.GetTopicManager(manager)
	if !ok {
		slog.Error("topic manager not found", "manager", manager)
		return "", ErrNoDocumentationFound
	}
	return tm.GetDocumentation(), nil
}

// GetDocumentationForLookupServiceProvider returns documentation for a lookup service provider (thread-safe)
func (e *Engine) GetDocumentationForLookupServiceProvider(provider string) (string, error) {
	l, ok := e.GetLookupService(provider)
	if !ok {
		slog.Error("lookup service provider not found", "provider", provider)
		return "", ErrNoDocumentationFound
	}
	return l.GetDocumentation(), nil
}
