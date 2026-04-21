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
		if manager == nil || config.Type != SyncConfigurationPeers {
			continue
		}
		switch {
		case name == "tm_ship" && len(e.SHIPTrackers) > 0:
			config.Peers = mergePeers(e.SHIPTrackers, config.Peers)
			e.SyncConfiguration[name] = config
		case name == "tm_slap" && len(e.SLAPTrackers) > 0:
			config.Peers = mergePeers(e.SLAPTrackers, config.Peers)
			e.SyncConfiguration[name] = config
		}
	}

	return e
}

// mergePeers deduplicates two peer lists into a single slice.
func mergePeers(trackers, existing []string) []string {
	combined := make(map[string]struct{}, len(trackers)+len(existing))
	for _, peer := range trackers {
		combined[peer] = struct{}{}
	}
	for _, peer := range existing {
		combined[peer] = struct{}{}
	}
	result := make([]string, 0, len(combined))
	for peer := range combined {
		result = append(result, peer)
	}
	return result
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

// SubmitParsedBeefParams holds the parameters for SubmitParsedBeef to reduce function arity.
type submitParsedBeefParams struct {
	Beef           *transaction.Beef
	Txid           *chainhash.Hash
	Topics         []string
	AtomicBeef     []byte
	OffChainValues []byte
	Mode           SumbitMode
	OnSteakReady   OnSteakReady
}

// SubmitParsedBeef processes a pre-parsed BEEF transaction for submission to overlay topics.
// This is the core submission logic; Submit() is a convenience wrapper that parses TaggedBEEF first.
// The AtomicBeef parameter is the original serialized bytes for use in lookup service notifications.
func (e *Engine) SubmitParsedBeef(ctx context.Context, beef *transaction.Beef, txid *chainhash.Hash, topics []string, atomicBeef, offChainValues []byte, mode SumbitMode, onSteakReady OnSteakReady) (overlay.Steak, error) {
	return e.submitParsedBeefInternal(ctx, &submitParsedBeefParams{
		Beef: beef, Txid: txid, Topics: topics,
		AtomicBeef: atomicBeef, OffChainValues: offChainValues,
		Mode: mode, OnSteakReady: onSteakReady,
	})
}

// submitParsedBeefInternal is the core implementation of SubmitParsedBeef.
func (e *Engine) submitParsedBeefInternal(ctx context.Context, p *submitParsedBeefParams) (overlay.Steak, error) {
	managers, err := e.validateTopicsAndGetManagers(p.Topics)
	if err != nil {
		return nil, err
	}

	tx := p.Beef.FindTransactionForSigningByHash(p.Txid)
	if tx == nil {
		slog.Error("invalid BEEF in Submit - tx is nil", "error", ErrInvalidBeef)
		return nil, ErrInvalidBeef
	}
	if err := e.verifyTransaction(ctx, tx, p.Txid); err != nil {
		return nil, err
	}

	steak := make(overlay.Steak, len(p.Topics))
	topicInputs := make(map[string]map[uint32]*Output, len(tx.Inputs))
	inpoints := buildInpoints(tx)
	dupeTopics := make(map[string]struct{}, len(p.Topics))

	if err := e.identifyAdmissibleOutputsPerTopic(ctx, p, managers, inpoints, steak, topicInputs, dupeTopics); err != nil {
		return nil, err
	}

	if err := e.markSpentAndNotify(ctx, p.Topics, dupeTopics, topicInputs, tx, p.Txid, p.AtomicBeef); err != nil {
		return nil, err
	}

	if err := e.broadcastIfNeeded(tx, p.Txid, p.Mode); err != nil {
		return nil, err
	}

	if p.OnSteakReady != nil {
		p.OnSteakReady(&steak)
	}
	if p.Mode != SubmitModeHistorical && e.OnAdmission != nil {
		e.OnAdmission(p.Txid, &steak, p.AtomicBeef)
	}

	if err := e.commitAdmittedOutputs(ctx, p, steak, topicInputs, dupeTopics); err != nil {
		return nil, err
	}

	e.propagateToNetwork(ctx, tx, p.Txid, steak, dupeTopics, p.Mode)
	return steak, nil
}

// validateTopicsAndGetManagers validates that all topics exist and returns a manager snapshot.
func (e *Engine) validateTopicsAndGetManagers(topics []string) (map[string]TopicManager, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	managers := make(map[string]TopicManager, len(topics))
	for _, t := range topics {
		manager, ok := e.managers[t]
		if !ok {
			slog.Error("unknown topic in Submit", "topic", t, "error", ErrUnknownTopic)
			return nil, ErrUnknownTopic
		}
		managers[t] = manager
	}
	return managers, nil
}

// verifyTransaction performs SPV verification on the transaction.
func (e *Engine) verifyTransaction(ctx context.Context, tx *transaction.Transaction, txid *chainhash.Hash) error {
	valid, err := spv.Verify(ctx, tx, e.ChainTracker, nil)
	if err != nil {
		slog.Error("SPV verification failed in Submit", "txid", txid, "error", err)
		return err
	}
	if !valid {
		slog.Error("invalid transaction in Submit", "txid", txid, "error", ErrInvalidTransaction)
		return ErrInvalidTransaction
	}
	return nil
}

// buildInpoints constructs the list of outpoints from transaction inputs.
func buildInpoints(tx *transaction.Transaction) []*transaction.Outpoint {
	inpoints := make([]*transaction.Outpoint, 0, len(tx.Inputs))
	for _, input := range tx.Inputs {
		inpoints = append(inpoints, &transaction.Outpoint{
			Txid:  *input.SourceTXID,
			Index: input.SourceTxOutIndex,
		})
	}
	return inpoints
}

// identifyAdmissibleOutputsPerTopic processes each topic to find duplicate transactions,
// merge BEEFs from existing outputs, and identify admissible outputs via topic managers.
func (e *Engine) identifyAdmissibleOutputsPerTopic(
	ctx context.Context,
	p *submitParsedBeefParams,
	managers map[string]TopicManager,
	inpoints []*transaction.Outpoint,
	steak overlay.Steak,
	topicInputs map[string]map[uint32]*Output,
	dupeTopics map[string]struct{},
) error {
	for _, t := range p.Topics {
		exists, err := e.Storage.DoesAppliedTransactionExist(ctx, &overlay.AppliedTransaction{Txid: p.Txid, Topic: t})
		if err != nil {
			slog.Error("failed to check if transaction exists", "txid", p.Txid, "topic", t, "error", err)
			return err
		}
		if exists {
			steak[t] = &overlay.AdmittanceInstructions{}
			dupeTopics[t] = struct{}{}
			continue
		}
		previousCoins, err := e.mergeExistingOutputs(ctx, p.Beef, inpoints, t, topicInputs)
		if err != nil {
			return err
		}
		topicBeef := p.Beef.Clone()
		admit, err := managers[t].IdentifyAdmissibleOutputs(ctx, topicBeef, p.Txid, previousCoins)
		if err != nil {
			slog.Error("failed to identify admissible outputs", "txid", p.Txid.String(), "topic", t, "mode", string(p.Mode), "error", err)
			return err
		}
		steak[t] = &admit
	}
	return nil
}

// mergeExistingOutputs finds existing outputs for a topic and merges their BEEF data.
func (e *Engine) mergeExistingOutputs(
	ctx context.Context,
	beef *transaction.Beef,
	inpoints []*transaction.Outpoint,
	topic string,
	topicInputs map[string]map[uint32]*Output,
) ([]uint32, error) {
	topicInputs[topic] = make(map[uint32]*Output, len(inpoints))
	previousCoins := make([]uint32, 0, len(inpoints))
	outputs, err := e.Storage.FindOutputs(ctx, inpoints, topic, nil, true)
	if err != nil {
		slog.Error("failed to find outputs", "topic", topic, "error", err)
		return nil, err
	}
	for vin, output := range outputs {
		if output == nil {
			continue
		}
		if output.Beef != nil {
			if mergeErr := beef.MergeBeef(output.Beef); mergeErr != nil {
				return nil, fmt.Errorf("failed to merge BEEF for input %d: %w", vin, mergeErr)
			}
		}
		previousCoins = append(previousCoins, uint32(vin))
		topicInputs[topic][uint32(vin)] = output
	}
	return previousCoins, nil
}

// markSpentAndNotify marks UTXOs as spent and notifies lookup services for each topic.
func (e *Engine) markSpentAndNotify(
	ctx context.Context,
	topics []string,
	dupeTopics map[string]struct{},
	topicInputs map[string]map[uint32]*Output,
	tx *transaction.Transaction,
	txid *chainhash.Hash,
	atomicBeef []byte,
) error {
	for _, t := range topics {
		if _, ok := dupeTopics[t]; ok {
			continue
		}
		if err := e.markTopicUTXOsSpent(ctx, topicInputs[t], t, txid); err != nil {
			return err
		}
		if err := e.notifySpentOutputs(ctx, topicInputs[t], t, tx, txid, atomicBeef); err != nil {
			return err
		}
	}
	return nil
}

// markTopicUTXOsSpent marks UTXOs as spent for a single topic.
func (e *Engine) markTopicUTXOsSpent(ctx context.Context, inputs map[uint32]*Output, topic string, txid *chainhash.Hash) error {
	if len(inputs) == 0 {
		return nil
	}
	topicInpoints := make([]*transaction.Outpoint, 0, len(inputs))
	for _, output := range inputs {
		topicInpoints = append(topicInpoints, &output.Outpoint)
	}
	if err := e.Storage.MarkUTXOsAsSpent(ctx, topicInpoints, topic, txid); err != nil {
		slog.Error("failed to mark UTXOs as spent", "topic", topic, "txid", txid, "error", err)
		return err
	}
	return nil
}

// notifySpentOutputs notifies lookup services about spent outputs for a single topic.
func (e *Engine) notifySpentOutputs(ctx context.Context, inputs map[uint32]*Output, topic string, tx *transaction.Transaction, txid *chainhash.Hash, atomicBeef []byte) error {
	lookupServices := e.getLookupServicesSnapshot()
	for vin, output := range inputs {
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
				return err
			}
		}
	}
	return nil
}

// broadcastIfNeeded broadcasts the transaction if not in historical mode.
func (e *Engine) broadcastIfNeeded(tx *transaction.Transaction, txid *chainhash.Hash, mode SumbitMode) error {
	if mode == SubmitModeHistorical || e.Broadcaster == nil {
		return nil
	}
	if _, failure := e.Broadcaster.Broadcast(tx); failure != nil {
		slog.Error("failed to broadcast transaction", "txid", txid, "mode", string(mode), "error", failure)
		return failure
	}
	return nil
}

// commitAdmittedOutputs persists admitted outputs, updates consumed-by references, and records applied transactions.
func (e *Engine) commitAdmittedOutputs(ctx context.Context, p *submitParsedBeefParams, steak overlay.Steak, topicInputs map[string]map[uint32]*Output, dupeTopics map[string]struct{}) error {
	for _, t := range p.Topics {
		if _, ok := dupeTopics[t]; ok {
			continue
		}
		if err := e.commitTopicOutputs(ctx, t, p, steak[t], topicInputs[t]); err != nil {
			return err
		}
	}
	return nil
}

// commitTopicOutputs handles the per-topic commit logic: delete spent UTXOs, insert new outputs,
// notify lookup services, update consumed-by references, and record the applied transaction.
func (e *Engine) commitTopicOutputs(ctx context.Context, topic string, p *submitParsedBeefParams, admit *overlay.AdmittanceInstructions, inputs map[uint32]*Output) error {
	outputsConsumed, outpointsConsumed := e.separateRetainedCoins(inputs, admit.CoinsToRetain)

	for vin, output := range inputs {
		if err := e.deleteUTXODeep(ctx, output); err != nil {
			slog.Error("failed to delete UTXO deep", "topic", topic, "outpoint", output.Outpoint.String(), "error", err)
			return err
		}
		admit.CoinsRemoved = append(admit.CoinsRemoved, vin)
	}

	if err := e.Storage.InsertOutputs(ctx, topic, p.Txid, admit.OutputsToAdmit, outpointsConsumed, p.Beef, admit.AncillaryTxids); err != nil {
		slog.Error("failed to insert outputs", "topic", topic, "txid", p.Txid.String(), "error", err)
		return err
	}

	newOutpoints, err := e.notifyAdmittedOutputs(ctx, topic, p.Txid, admit.OutputsToAdmit, p.AtomicBeef, p.OffChainValues)
	if err != nil {
		return err
	}

	if err := e.updateConsumedByReferences(ctx, outputsConsumed, newOutpoints); err != nil {
		return err
	}

	if err := e.Storage.InsertAppliedTransaction(ctx, &overlay.AppliedTransaction{Txid: p.Txid, Topic: topic}); err != nil {
		slog.Error("failed to insert applied transaction", "topic", topic, "txid", p.Txid, "error", err)
		return err
	}
	return nil
}

// separateRetainedCoins splits topic inputs into retained (consumed) outputs and remaining (to be removed).
// Retained outputs are removed from the topicInputs map in-place.
func (e *Engine) separateRetainedCoins(topicInputs map[uint32]*Output, coinsToRetain []uint32) ([]*Output, []*transaction.Outpoint) {
	outputsConsumed := make([]*Output, 0, len(coinsToRetain))
	outpointsConsumed := make([]*transaction.Outpoint, 0, len(coinsToRetain))

	// Fast path: nothing to do if there are no topic inputs or no coins to retain.
	if len(topicInputs) == 0 || len(coinsToRetain) == 0 {
		return outputsConsumed, outpointsConsumed
	}

	// Build a set of coins to retain for O(1) membership checks.
	retainSet := make(map[uint32]struct{}, len(coinsToRetain))
	for _, coin := range coinsToRetain {
		retainSet[coin] = struct{}{}
	}

	for vin, output := range topicInputs {
		if _, ok := retainSet[vin]; !ok {
			continue
		}
		outputsConsumed = append(outputsConsumed, output)
		outpointsConsumed = append(outpointsConsumed, &output.Outpoint)
		delete(topicInputs, vin)
	}

	return outputsConsumed, outpointsConsumed
}

// notifyAdmittedOutputs notifies lookup services about admitted outputs and returns the new outpoints.
func (e *Engine) notifyAdmittedOutputs(ctx context.Context, topic string, txid *chainhash.Hash, outputsToAdmit []uint32, atomicBeef, offChainValues []byte) ([]*transaction.Outpoint, error) {
	newOutpoints := make([]*transaction.Outpoint, 0, len(outputsToAdmit))
	lookupServices := e.getLookupServicesSnapshot()
	for _, vout := range outputsToAdmit {
		outpoint := &transaction.Outpoint{Txid: *txid, Index: vout}
		newOutpoints = append(newOutpoints, outpoint)
		for _, l := range lookupServices {
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
	return newOutpoints, nil
}

// updateConsumedByReferences updates the consumed-by references for retained outputs.
func (e *Engine) updateConsumedByReferences(ctx context.Context, outputsConsumed []*Output, newOutpoints []*transaction.Outpoint) error {
	for _, output := range outputsConsumed {
		output.ConsumedBy = append(output.ConsumedBy, newOutpoints...)
		if err := e.Storage.UpdateConsumedBy(ctx, &output.Outpoint, output.Topic, output.ConsumedBy); err != nil {
			slog.Error("failed to update consumed by", "topic", output.Topic, "outpoint", output.Outpoint.String(), "error", err)
			return err
		}
	}
	return nil
}

// propagateToNetwork propagates the transaction to other overlay nodes via SHIP/SLAP.
func (e *Engine) propagateToNetwork(ctx context.Context, tx *transaction.Transaction, txid *chainhash.Hash, steak overlay.Steak, dupeTopics map[string]struct{}, mode SumbitMode) {
	if e.Advertiser == nil || mode == SubmitModeHistorical {
		return
	}

	relevantTopics := make([]string, 0, len(steak))
	for t, s := range steak {
		if s.OutputsToAdmit == nil && s.CoinsToRetain == nil {
			continue
		}
		if _, ok := dupeTopics[t]; !ok {
			relevantTopics = append(relevantTopics, t)
		}
	}
	if len(relevantTopics) == 0 {
		return
	}

	broadcasterCfg := &topic.BroadcasterConfig{}
	if len(e.SLAPTrackers) > 0 {
		broadcasterCfg.Resolver = lookup.NewLookupResolver(&lookup.LookupResolver{
			SLAPTrackers: e.SLAPTrackers,
		})
	}

	if broadcaster, err := topic.NewBroadcaster(relevantTopics, broadcasterCfg); err != nil {
		slog.Error("failed to create broadcaster for propagation", "topics", relevantTopics, "error", err)
	} else if _, failure := broadcaster.BroadcastCtx(ctx, tx); failure != nil {
		slog.Error("failed to propagate transaction to other nodes", "txid", txid, "error", failure)
	}
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
	hydratedOutputs, err := e.hydrateFormulas(ctx, result.Formulas)
	if err != nil {
		return nil, err
	}
	return &lookup.LookupAnswer{
		Type:    lookup.AnswerTypeOutputList,
		Outputs: hydratedOutputs,
	}, nil
}

// hydrateFormulas loads and hydrates UTXO history for each lookup formula.
func (e *Engine) hydrateFormulas(ctx context.Context, formulas []lookup.LookupFormula) ([]*lookup.OutputListItem, error) {
	hydratedOutputs := make([]*lookup.OutputListItem, 0, len(formulas))
	for i := range formulas {
		item, err := e.hydrateOneFormula(ctx, &formulas[i])
		if err != nil {
			return nil, err
		}
		if item != nil {
			hydratedOutputs = append(hydratedOutputs, item)
		}
	}
	return hydratedOutputs, nil
}

// hydrateOneFormula loads and hydrates a single lookup formula into an OutputListItem.
func (e *Engine) hydrateOneFormula(ctx context.Context, formula *lookup.LookupFormula) (*lookup.OutputListItem, error) {
	output, err := e.Storage.FindOutput(ctx, formula.Outpoint, nil, nil, true)
	if err != nil {
		slog.Error("failed to find output in Lookup", "outpoint", formula.Outpoint.String(), "error", err)
		return nil, err
	}
	if output == nil || output.Beef == nil {
		return nil, nil //nolint:nilnil // nil output with no error is valid when output is missing
	}
	if loadErr := e.Storage.LoadAncillaryBeef(ctx, output); loadErr != nil {
		slog.Error("failed to load ancillary beef in Lookup", "outpoint", formula.Outpoint.String(), "error", loadErr)
		return nil, loadErr
	}
	hydratedOutput, err := e.GetUTXOHistory(ctx, output, formula.History, 0)
	if err != nil {
		slog.Error("failed to get UTXO history in Lookup", "outpoint", formula.Outpoint.String(), "error", err)
		return nil, err
	}
	if hydratedOutput == nil || hydratedOutput.Beef == nil {
		return nil, nil //nolint:nilnil // nil output with no error is valid when history selector filters
	}
	beefBytes, err := hydratedOutput.Beef.AtomicBytes(&hydratedOutput.Outpoint.Txid)
	if err != nil {
		slog.Error("failed to serialize BEEF in Lookup", "outpoint", formula.Outpoint.String(), "error", err)
		return nil, err
	}
	return &lookup.OutputListItem{
		Beef:        beefBytes,
		OutputIndex: hydratedOutput.Outpoint.Index,
	}, nil
}

// GetUTXOHistory retrieves the history of a UTXO
func (e *Engine) GetUTXOHistory(ctx context.Context, output *Output, historySelector func(beef *transaction.Beef, outputIndex, currentDepth uint32) bool, currentDepth uint32) (*Output, error) {
	if historySelector == nil {
		return output, nil
	}
	if !historySelector(output.Beef, output.Outpoint.Index, currentDepth) {
		return nil, nil //nolint:nilnil // returning nil output with no error is valid when selector returns false
	}
	if output != nil && len(output.OutputsConsumed) == 0 {
		return output, nil
	}

	childHistories, err := e.collectChildHistories(ctx, output.OutputsConsumed, historySelector, currentDepth)
	if err != nil {
		return nil, err
	}

	tx := output.Beef.FindTransactionForSigningByHash(&output.Outpoint.Txid)
	if tx == nil {
		slog.Error("failed to find transaction in BEEF in GetUTXOHistory", "outpoint", output.Outpoint.String())
		return nil, ErrMissingBeef
	}

	if stitchErr := stitchSourceTransactions(tx, childHistories); stitchErr != nil {
		return nil, stitchErr
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

// collectChildHistories recursively collects UTXO history for consumed outputs.
func (e *Engine) collectChildHistories(
	ctx context.Context,
	outputsConsumed []*transaction.Outpoint,
	historySelector func(beef *transaction.Beef, outputIndex, currentDepth uint32) bool,
	currentDepth uint32,
) (map[string]*Output, error) {
	childHistories := make(map[string]*Output, len(outputsConsumed))
	for _, outpoint := range outputsConsumed {
		childOutput, err := e.Storage.FindOutput(ctx, outpoint, nil, nil, true)
		if err != nil {
			slog.Error("failed to find output in GetUTXOHistory", "outpoint", outpoint.String(), "error", err)
			return nil, err
		}
		if childOutput == nil {
			continue
		}
		if loadErr := e.Storage.LoadAncillaryBeef(ctx, childOutput); loadErr != nil {
			slog.Error("failed to load ancillary beef in GetUTXOHistory", "outpoint", outpoint.String(), "error", loadErr)
			return nil, loadErr
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
	return childHistories, nil
}

// stitchSourceTransactions links child BEEF histories into the parent transaction's inputs.
func stitchSourceTransactions(tx *transaction.Transaction, childHistories map[string]*Output) error {
	for _, txin := range tx.Inputs {
		outpoint := &transaction.Outpoint{
			Txid:  *txin.SourceTXID,
			Index: txin.SourceTxOutIndex,
		}
		input := childHistories[outpoint.String()]
		if input == nil {
			continue
		}
		if input.Beef == nil {
			slog.Error("missing BEEF in GetUTXOHistory", "outpoint", outpoint.String(), "error", ErrMissingBeef)
			return ErrMissingBeef
		}
		txin.SourceTransaction = input.Beef.FindTransactionForSigningByHash(&outpoint.Txid)
		if txin.SourceTransaction == nil {
			slog.Error("failed to find source transaction in BEEF", "outpoint", outpoint.String())
			return ErrMissingBeef
		}
	}
	return nil
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
			peers, err := e.discoverSHIPPeers(ctx, topic)
			if err != nil {
				return err
			}
			syncEndpoints.Peers = peers
		} else {
			slog.Info(fmt.Sprintf("[GASP SYNC] Skipping topic peer discovery \"%s\" - sync type is not SHIP (type: \"%s\")", topic, syncEndpoints.Type))
		}

		if len(syncEndpoints.Peers) == 0 {
			slog.Info(fmt.Sprintf("[GASP SYNC] No peers found for topic \"%s\", skipping sync", topic))
			continue
		}
		slog.Info(fmt.Sprintf("[GASP SYNC] Will attempt to sync with %d peer(s)", len(syncEndpoints.Peers)), "topic", topic)

		for _, peer := range syncEndpoints.Peers {
			if err := e.syncWithPeer(ctx, topic, peer, syncEndpoints.Concurrency); err != nil {
				return err
			}
		}
	}
	return nil
}

// discoverSHIPPeers discovers peers for a topic using SHIP lookup.
func (e *Engine) discoverSHIPPeers(ctx context.Context, topic string) ([]string, error) {
	slog.Info(fmt.Sprintf("[GASP SYNC] Discovering peers for topic \"%s\" using SHIP lookup", topic))

	e.LookupResolver.SetSLAPTrackers(e.SLAPTrackers)
	slog.Debug(fmt.Sprintf("[GASP SYNC] Current SLAP trackers after setting: %v", e.LookupResolver.SLAPTrackers()))

	query, err := json.Marshal(map[string]any{"topics": []string{topic}})
	if err != nil {
		slog.Error("failed to marshal query for GASP sync", "topic", topic, "error", err)
		return nil, err
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	lookupAnswer, err := e.LookupResolver.Query(timeoutCtx, &lookup.LookupQuestion{Service: "ls_ship", Query: query})
	if err != nil {
		slog.Error("failed to query lookup resolver for GASP sync", "topic", topic, "error", err)
		return nil, err
	}

	if lookupAnswer.Type != lookup.AnswerTypeOutputList {
		slog.Warn(fmt.Sprintf("[GASP SYNC] Unexpected answer type \"%s\" for topic \"%s\"", lookupAnswer.Type, topic))
		return nil, nil // nil peers with no error means no peers discovered
	}

	endpointSet := e.extractPeerEndpoints(topic, lookupAnswer.Outputs)

	peers := make([]string, 0, len(endpointSet))
	for endpoint := range endpointSet {
		if endpoint != e.HostingURL {
			peers = append(peers, endpoint)
		}
	}
	slog.Info(fmt.Sprintf("[GASP SYNC] Discovered %d unique peer endpoint(s) for topic \"%s\"", len(peers), topic))
	return peers, nil
}

// expectedProtocolForTopic returns the expected overlay protocol for a given topic name.
func expectedProtocolForTopic(topic string) (overlay.Protocol, bool) {
	switch topic {
	case "tm_ship":
		return overlay.ProtocolSHIP, true
	case "tm_slap":
		return overlay.ProtocolSLAP, true
	default:
		return "", false
	}
}

// extractPeerEndpoints parses advertisement outputs and returns a set of peer endpoint URLs.
func (e *Engine) extractPeerEndpoints(topic string, outputs []*lookup.OutputListItem) map[string]struct{} {
	endpointSet := make(map[string]struct{}, len(outputs))
	expectedProto, knownTopic := expectedProtocolForTopic(topic)
	if !knownTopic {
		slog.Warn("unknown topic, cannot determine expected protocol", "topic", topic)
		return endpointSet
	}

	for _, output := range outputs {
		domain := e.parseAdvertisementDomain(topic, output, expectedProto)
		if domain != "" {
			endpointSet[domain] = struct{}{}
		}
	}
	return endpointSet
}

// parseAdvertisementDomain extracts the domain from a single advertisement output, returning "" if invalid.
func (e *Engine) parseAdvertisementDomain(topic string, output *lookup.OutputListItem, expectedProto overlay.Protocol) string {
	beef, _, txID, err := transaction.ParseBeef(output.Beef)
	if err != nil {
		slog.Error("failed to parse advertisement output BEEF", "topic", topic, "error", err)
		return ""
	}
	if txID == nil {
		slog.Error("missing transaction ID in advertisement output BEEF", "topic", topic)
		return ""
	}

	tx := beef.FindTransactionByHash(txID)
	if tx == nil || tx.Outputs == nil || len(tx.Outputs) <= int(output.OutputIndex) {
		return ""
	}
	txOut := tx.Outputs[output.OutputIndex]
	if txOut == nil || txOut.LockingScript == nil || e.Advertiser == nil {
		return ""
	}

	advertisement, err := e.Advertiser.ParseAdvertisement(txOut.LockingScript)
	if err != nil || advertisement == nil {
		return ""
	}

	if advertisement.Protocol == expectedProto {
		return advertisement.Domain
	}
	return ""
}

// syncWithPeer performs paginated GASP sync with a single peer.
func (e *Engine) syncWithPeer(ctx context.Context, topic, peer string, concurrency int) error {
	logPrefix := "[GASP Sync of " + topic + " with " + peer + "]"
	slog.Info(fmt.Sprintf("[GASP SYNC] Starting sync for topic \"%s\" with peer \"%s\"", topic, peer))

	lastInteraction, err := e.Storage.GetLastInteraction(ctx, peer, topic)
	if err != nil {
		slog.Error("Failed to get last interaction", "topic", topic, "peer", peer, "error", err)
		return err
	}

	gaspProvider := gasp.NewGASP(gasp.Params{ //nolint:contextcheck // NewGASP spawns a long-lived worker
		Storage:         NewOverlayGASPStorage(topic, e, nil),
		Remote:          NewOverlayGASPRemote(peer, topic, http.DefaultClient, 8),
		LastInteraction: lastInteraction,
		LogPrefix:       &logPrefix,
		Unidirectional:  true,
		Concurrency:     concurrency,
		Topic:           topic,
	})
	defer gaspProvider.Close()

	for {
		previousLastInteraction := gaspProvider.LastInteraction

		if err := gaspProvider.Sync(ctx, peer, DefaultGASPSyncLimit); err != nil {
			slog.Error("failed to sync with peer", "topic", topic, "peer", peer, "error", err)
			break
		}

		if gaspProvider.LastInteraction > previousLastInteraction {
			if err := e.Storage.UpdateLastInteraction(ctx, peer, topic, gaspProvider.LastInteraction); err != nil {
				slog.Error("Failed to update last interaction", "topic", topic, "peer", peer, "error", err)
			}
		} else {
			slog.Info(logPrefix + " Sync completed")
			break
		}
	}
	return nil
}

// SyncInvalidatedOutputs finds outputs with invalidated merkle proofs and syncs them with remote peers
func (e *Engine) SyncInvalidatedOutputs(ctx context.Context, topic string) error {
	invalidatedOutpoints, err := e.Storage.FindOutpointsByMerkleState(ctx, topic, MerkleStateInvalidated, 1000)
	if err != nil {
		slog.Error("Failed to find invalidated outputs", "topic", topic, "error", err)
		return err
	}
	if len(invalidatedOutpoints) == 0 {
		return nil
	}

	syncConfig, ok := e.SyncConfiguration[topic]
	if !ok || len(syncConfig.Peers) == 0 {
		slog.Warn("No peers configured for topic", "topic", topic)
		return nil
	}

	// Group outpoints by transaction ID to avoid duplicate merkle proof requests
	txidsToUpdate := groupOutpointsByTxid(invalidatedOutpoints)

	var successCount int
	for txid, outpoint := range txidsToUpdate {
		if e.syncMerkleProofFromPeers(ctx, topic, txid, outpoint, syncConfig.Peers) {
			successCount++
		}
	}

	if successCount == 0 && len(txidsToUpdate) > 0 {
		slog.Warn("Could not update all invalidated outputs", "topic", topic, "remaining", len(txidsToUpdate))
	}
	return nil
}

// groupOutpointsByTxid deduplicates outpoints by transaction ID, keeping the first for each txid.
func groupOutpointsByTxid(outpoints []*transaction.Outpoint) map[chainhash.Hash]*transaction.Outpoint {
	result := make(map[chainhash.Hash]*transaction.Outpoint)
	for _, outpoint := range outpoints {
		if _, exists := result[outpoint.Txid]; !exists {
			result[outpoint.Txid] = outpoint
		}
	}
	return result
}

// syncMerkleProofFromPeers tries each peer to get a valid merkle proof for a transaction.
// Returns true if a proof was successfully synced.
func (e *Engine) syncMerkleProofFromPeers(ctx context.Context, topic string, txid chainhash.Hash, outpoint *transaction.Outpoint, peers []string) bool {
	for _, peer := range peers {
		if peer == e.HostingURL {
			continue
		}
		remote := NewOverlayGASPRemote(peer, topic, http.DefaultClient, 8)
		node, err := remote.RequestNode(ctx, outpoint, outpoint, true)
		if err != nil || node.Proof == nil {
			continue
		}
		merklePath, err := transaction.NewMerklePathFromHex(*node.Proof)
		if err != nil {
			slog.Error("Failed to parse merkle proof", "txid", txid.String(), "error", err)
			continue
		}
		if err := e.HandleNewMerkleProof(ctx, &txid, merklePath); err != nil {
			slog.Error("Failed to update merkle proof", "txid", txid.String(), "error", err)
			continue
		}
		return true
	}
	slog.Warn("Failed to sync transaction from any peer", "txid", txid.String(), "peers_tried", len(peers))
	return false
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
	slog.Debug("ProvideForeignGASPNode called",
		"graphID", graphID.String(),
		"outpoint", outpoint.String(),
		"topic", topic)

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
	return e.hydrateGASPNode(ctx, output, graphID, outpoint, topic, 0)
}

// hydrateGASPNode converts an output into a GASP Node, searching BEEF and consumed outputs.
// maxDepth limits recursive lookups of consumed outputs (currently capped at 1).
func (e *Engine) hydrateGASPNode(ctx context.Context, output *Output, graphID, outpoint *transaction.Outpoint, topic string, depth uint32) (*gasp.Node, error) {
	if output.Beef == nil {
		slog.Error("missing BEEF in ProvideForeignGASPNode hydrator", "outpoint", output.Outpoint.String(), "error", ErrMissingInput)
		return nil, ErrMissingInput
	}

	if err := e.Storage.LoadAncillaryBeef(ctx, output); err != nil {
		slog.Error("failed to load ancillary beef in ProvideForeignGASPNode hydrator", "outpoint", output.Outpoint.String(), "error", err)
		return nil, err
	}

	if correctTx := output.Beef.FindTransactionByHash(&outpoint.Txid); correctTx != nil {
		return buildGASPNode(graphID, outpoint, correctTx), nil
	}

	// Recursive lookups of missing transactions is heavy; limit to depth 1
	if depth > 0 {
		return nil, ErrMissingOutput
	}
	for _, consumedOutpoint := range output.OutputsConsumed {
		consumedOutput, err := e.Storage.FindOutput(ctx, consumedOutpoint, &topic, nil, true)
		if err != nil || consumedOutput == nil {
			continue
		}
		node, err := e.hydrateGASPNode(ctx, consumedOutput, graphID, outpoint, topic, depth+1)
		if err == nil {
			return node, nil
		}
	}
	return nil, ErrMissingOutput
}

// buildGASPNode constructs a gasp.Node from a found transaction.
func buildGASPNode(graphID, outpoint *transaction.Outpoint, tx *transaction.Transaction) *gasp.Node {
	node := &gasp.Node{
		GraphID:     graphID,
		RawTx:       tx.Hex(),
		OutputIndex: outpoint.Index,
	}
	if tx.MerklePath != nil {
		proof := tx.MerklePath.Hex()
		node.Proof = &proof
	}
	return node
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
		slog.Error("missing BEEF in updateMerkleProof", "outpoint", output.Outpoint.String(), "error", ErrMissingBeef)
		return ErrMissingBeef
	}
	tx := output.Beef.FindTransactionForSigningByHash(&output.Outpoint.Txid)
	if tx == nil {
		slog.Error("missing transaction in updateMerkleProof", "outpoint", output.Outpoint.String(), "error", ErrMissingTransaction)
		return ErrMissingTransaction
	}
	if unchanged, err := isMerkleRootUnchanged(tx, txid, proof); err != nil {
		return err
	} else if unchanged {
		return nil
	}

	if err := e.updateInputProofs(ctx, tx, txid, proof); err != nil {
		slog.Error("failed to update input proofs in updateMerkleProof", "txid", txid, "error", err)
		return err
	}
	updatedBeef, err := rebuildBeefFromTx(tx, txid)
	if err != nil {
		return err
	}

	updateOutputBlockInfo(output, proof)

	if err := e.Storage.UpdateTransactionBEEF(ctx, &output.Outpoint.Txid, updatedBeef); err != nil {
		slog.Error("failed to update transaction BEEF", "txid", output.Outpoint.Txid, "error", err)
		return err
	}
	return e.propagateMerkleProofToConsumers(ctx, output.ConsumedBy, txid, proof)
}

// isMerkleRootUnchanged checks if the proof produces the same merkle root as the existing path.
func isMerkleRootUnchanged(tx *transaction.Transaction, txid chainhash.Hash, proof *transaction.MerklePath) (bool, error) {
	if tx.MerklePath == nil {
		return false, nil
	}
	oldRoot, err := tx.MerklePath.ComputeRoot(&txid)
	if err != nil {
		slog.Error("failed to compute old merkle root", "txid", txid, "error", err)
		return false, err
	}
	newRoot, err := proof.ComputeRoot(&txid)
	if err != nil {
		slog.Error("failed to compute new merkle root", "txid", txid, "error", err)
		return false, err
	}
	return oldRoot.Equal(*newRoot), nil
}

// rebuildBeefFromTx serializes and re-parses a transaction to get an updated BEEF.
func rebuildBeefFromTx(tx *transaction.Transaction, txid chainhash.Hash) (*transaction.Beef, error) {
	atomicBytes, err := tx.AtomicBEEF(false)
	if err != nil {
		slog.Error("failed to get atomic BEEF", "txid", txid, "error", err)
		return nil, err
	}
	updatedBeef, _, _, parseErr := transaction.ParseBeef(atomicBytes)
	if parseErr != nil {
		slog.Error("failed to parse updated BEEF", "txid", txid, "error", parseErr)
		return nil, parseErr
	}
	return updatedBeef, nil
}

// updateOutputBlockInfo updates block height and index from a merkle proof.
func updateOutputBlockInfo(output *Output, proof *transaction.MerklePath) {
	output.BlockHeight = proof.BlockHeight
	for _, leaf := range proof.Path[0] {
		if leaf.Hash != nil && leaf.Hash.Equal(output.Outpoint.Txid) {
			output.BlockIdx = leaf.Offset
			break
		}
	}
}

// propagateMerkleProofToConsumers propagates a merkle proof update to consuming outputs.
func (e *Engine) propagateMerkleProofToConsumers(ctx context.Context, consumedBy []*transaction.Outpoint, txid chainhash.Hash, proof *transaction.MerklePath) error {
	for _, outpoint := range consumedBy {
		consumingOutputs, err := e.Storage.FindOutputsForTransaction(ctx, &outpoint.Txid, true)
		if err != nil {
			slog.Error("failed to find consuming outputs", "txid", outpoint.Txid, "error", err)
			return err
		}
		for _, consuming := range consumingOutputs {
			if consumingAlreadyMined(consuming) {
				continue
			}
			if err := e.updateMerkleProof(ctx, consuming, txid, proof); err != nil {
				slog.Error("failed to update merkle proof for consuming output", "consumingTxid", consuming.Outpoint.Txid, "error", err)
				return err
			}
		}
	}
	return nil
}

// consumingAlreadyMined checks if a consuming output's transaction has its own merkle path.
func consumingAlreadyMined(output *Output) bool {
	if output.Beef == nil {
		return false
	}
	tx := output.Beef.FindTransactionForSigningByHash(&output.Outpoint.Txid)
	return tx != nil && tx.MerklePath != nil
}

// HandleNewMerkleProof handles a new Merkle proof
func (e *Engine) HandleNewMerkleProof(ctx context.Context, txid *chainhash.Hash, proof *transaction.MerklePath) error {
	if err := e.validateMerkleProof(ctx, txid, proof); err != nil {
		return err
	}

	outputs, err := e.Storage.FindOutputsForTransaction(ctx, txid, true)
	if err != nil {
		slog.Error("failed to find outputs for transaction in HandleNewMerkleProof", "txid", txid, "error", err)
		return err
	}
	if len(outputs) == 0 {
		return nil
	}

	blockIdx := findBlockIdx(proof, *txid)
	if blockIdx == nil {
		err := fmt.Errorf("not found in proof: %s", txid) //nolint:err113 // dynamic error needed for context
		slog.Error("transaction not found in merkle proof", "txid", txid, "error", err)
		return err
	}

	for _, output := range outputs {
		if err := e.updateMerkleProof(ctx, output, *txid, proof); err != nil {
			slog.Error("failed to update merkle proof in HandleNewMerkleProof", "outpoint", output.Outpoint.String(), "error", err)
			return err
		}
		if err := e.Storage.UpdateOutputBlockHeight(ctx, &output.Outpoint, output.Topic, output.BlockHeight, output.BlockIdx); err != nil {
			slog.Error("failed to update output block height", "outpoint", output.Outpoint.String(), "error", err)
			return err
		}
	}

	lookupServices := e.getLookupServicesSnapshot()
	for _, l := range lookupServices {
		if err := l.OutputBlockHeightUpdated(ctx, txid, proof.BlockHeight, *blockIdx); err != nil {
			slog.Error("failed to notify lookup service about block height update", "txid", txid, "blockHeight", proof.BlockHeight, "error", err)
			return err
		}
	}
	return nil
}

// validateMerkleProof validates a merkle proof against the chain tracker.
func (e *Engine) validateMerkleProof(ctx context.Context, txid *chainhash.Hash, proof *transaction.MerklePath) error {
	merkleRoot, err := proof.ComputeRoot(txid)
	if err != nil {
		slog.Error("failed to compute merkle root from proof", "txid", txid, "error", err)
		return err
	}
	valid, err := e.ChainTracker.IsValidRootForHeight(ctx, merkleRoot, proof.BlockHeight)
	if err != nil {
		slog.Error("error validating merkle root for height", "txid", txid, "blockHeight", proof.BlockHeight, "error", err)
		return err
	}
	if !valid {
		slog.Error("merkle proof validation failed", "txid", txid, "blockHeight", proof.BlockHeight)
		return fmt.Errorf("%w: transaction %s at block height %d", ErrInvalidMerkleProof, txid, proof.BlockHeight)
	}
	return nil
}

// findBlockIdx finds the block index for a transaction in a merkle proof path.
func findBlockIdx(proof *transaction.MerklePath, txid chainhash.Hash) *uint64 {
	for _, leaf := range proof.Path[0] {
		if leaf.Hash != nil && leaf.Hash.Equal(txid) {
			return &leaf.Offset
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
