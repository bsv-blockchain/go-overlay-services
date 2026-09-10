package engine

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strconv"

	"github.com/bsv-blockchain/go-sdk/overlay"
	"github.com/bsv-blockchain/go-sdk/transaction"
)

type admissionHost interface {
	Storage
	Scope() StorageScope
	PublishPayload(ctx context.Context, ref AdmissionPayloadRef, reader io.Reader) error
	CurrentHistoryFence(ctx context.Context, topic string) (HistoryFence, error)
	EnsureHistoryFence(ctx context.Context, topic string, fence HistoryFence) error
}

type steakJSONEntry struct {
	OutputsToAdmit []uint32 `json:"outputsToAdmit"`
	CoinsToRetain  []uint32 `json:"coinsToRetain"`
	CoinsRemoved   []uint32 `json:"coinsRemoved"`
}

func (e *Engine) submitWithAdmission(ctx context.Context, p *submitParsedBeefParams, tx *transaction.Transaction, steak overlay.Steak, topicInputs map[string]map[uint32]*Output) (overlay.Steak, error) {
	admission := GetAdmissionStorage(e.Storage)
	host, ok := e.Storage.(admissionHost)
	if admission == nil || !ok {
		return nil, ErrAdmissionUnsupported
	}
	if err := e.broadcastIfNeeded(tx, p.Txid, p.Mode); err != nil {
		return nil, err
	}
	plan, err := e.buildAdmissionPlan(ctx, host, p, tx, steak, topicInputs)
	if err != nil {
		return nil, err
	}
	result, err := admission.CommitAdmission(ctx, plan)
	if err != nil {
		slog.Error("admission commit failed", "txid", p.Txid, "error", err)
		return nil, err
	}
	switch result.State {
	case AdmissionCommitStateCommitted:
		if result.Receipt == nil {
			return nil, ErrAdmissionRejected
		}
		committed, parseErr := parseSavedSteak(result.Receipt.Steak)
		if parseErr != nil {
			return nil, parseErr
		}
		if p.OnSteakReady != nil {
			p.OnSteakReady(&committed)
		}
		if p.Mode != SubmitModeHistorical && e.OnAdmission != nil {
			e.OnAdmission(p.Txid, &committed, p.AtomicBeef)
		}
		return committed, nil
	case AdmissionCommitStatePending:
		return nil, fmt.Errorf("%w: %s", ErrAdmissionPending, result.AttemptID)
	case AdmissionCommitStateRejected:
		return nil, fmt.Errorf("%w: %s", ErrAdmissionRejected, result.RejectionCode)
	case AdmissionCommitStateAborted:
		return nil, fmt.Errorf("%w: %s", ErrAdmissionRejected, AdmissionCommitStateAborted)
	default:
		return nil, ErrAdmissionPending
	}
}

func (e *Engine) buildAdmissionPlan(ctx context.Context, host admissionHost, p *submitParsedBeefParams, tx *transaction.Transaction, steak overlay.Steak, topicInputs map[string]map[uint32]*Output) (AdmissionCommit, error) {
	mode := AdmissionModeLive
	if p.Mode == SubmitModeHistorical {
		mode = AdmissionModeHistorical
	}
	topics := make([]AdmissionTopic, 0, len(p.Topics))
	seen := make(map[string]struct{}, len(p.Topics))
	for _, topic := range p.Topics {
		if _, exists := seen[topic]; exists {
			continue
		}
		seen[topic] = struct{}{}
		topics = append(topics, AdmissionTopic{Topic: topic, PolicyID: topic})
	}
	identity := AdmissionIdentity{
		Scope:         host.Scope(),
		TxID:          hex.EncodeToString(p.Txid[:]),
		Mode:          mode,
		ContextDigest: contextDigest(p.OffChainValues),
		Topics:        topics,
	}
	digest, err := AdmissionSemanticDigest(identity)
	if err != nil {
		return AdmissionCommit{}, err
	}
	payloads, payloadIndex, err := e.publishAdmissionPayloads(ctx, host, p, tx, steak)
	if err != nil {
		return AdmissionCommit{}, err
	}
	decisions := make([]AdmissionTopicDecision, 0, len(topics))
	outbox := make([]AdmissionOutboxIntent, 0)
	for _, topic := range topics {
		decision, decisionErr := e.buildTopicDecision(ctx, topicDecisionParams{
			host: host, parsed: p, tx: tx, topic: topic.Topic, admit: steak[topic.Topic], inputs: topicInputs[topic.Topic], payloads: payloadIndex,
		})
		if decisionErr != nil {
			return AdmissionCommit{}, decisionErr
		}
		decisions = append(decisions, decision)
	}
	operationID := admissionOperationID(mode, identity.TxID, p.Topics)
	if mode == AdmissionModeLive {
		outbox = append(outbox, AdmissionOutboxIntent{EventID: operationID + ":propagate", Kind: AdmissionOutboxPropagation, Target: "overlay-network", Payloads: []AdmissionPayloadRef{payloadIndex.raw}})
	}
	e.mu.RLock()
	lookupNames := make([]string, 0, len(e.lookupServices))
	for name := range e.lookupServices {
		lookupNames = append(lookupNames, name)
	}
	e.mu.RUnlock()
	for _, name := range lookupNames {
		outbox = append(outbox, AdmissionOutboxIntent{EventID: operationID + ":lookup:" + name, Kind: AdmissionOutboxLookup, Target: name, Payloads: []AdmissionPayloadRef{payloadIndex.raw}})
	}
	encodedSteak, err := encodeSteakJSON(steak)
	if err != nil {
		return AdmissionCommit{}, err
	}
	return AdmissionCommit{
		Key:       AdmissionOperationKey{Scope: identity.Scope, OperationID: operationID, SemanticDigest: digest},
		Identity:  identity,
		Payloads:  payloads,
		Decisions: decisions,
		Outbox:    outbox,
		Steak:     encodedSteak,
	}, nil
}

type admissionPayloads struct {
	raw     AdmissionPayloadRef
	scripts map[uint32]AdmissionPayloadRef
}

func (e *Engine) publishAdmissionPayloads(ctx context.Context, host admissionHost, p *submitParsedBeefParams, tx *transaction.Transaction, steak overlay.Steak) ([]AdmissionPayloadRef, admissionPayloads, error) {
	rawBytes := tx.Bytes()
	raw, err := publishBytes(ctx, host, rawBytes, AdmissionPayloadRawTransaction)
	if err != nil {
		return nil, admissionPayloads{}, err
	}
	payloads := []AdmissionPayloadRef{raw}
	scripts := make(map[uint32]AdmissionPayloadRef)
	for _, admit := range steak {
		if admit == nil {
			continue
		}
		for _, vout := range admit.OutputsToAdmit {
			if int(vout) >= len(tx.Outputs) || tx.Outputs[vout] == nil || tx.Outputs[vout].LockingScript == nil {
				continue
			}
			if _, exists := scripts[vout]; exists {
				continue
			}
			scriptRef, scriptErr := publishBytes(ctx, host, []byte(*tx.Outputs[vout].LockingScript), AdmissionPayloadLockingScript)
			if scriptErr != nil {
				return nil, admissionPayloads{}, scriptErr
			}
			scripts[vout] = scriptRef
			payloads = append(payloads, scriptRef)
		}
	}
	if len(p.AtomicBeef) > 0 {
		beefRef, beefErr := publishBytes(ctx, host, p.AtomicBeef, AdmissionPayloadBEEFManifest)
		if beefErr != nil {
			return nil, admissionPayloads{}, beefErr
		}
		payloads = append(payloads, beefRef)
	}
	return payloads, admissionPayloads{raw: raw, scripts: scripts}, nil
}

type topicDecisionParams struct {
	host     admissionHost
	parsed   *submitParsedBeefParams
	tx       *transaction.Transaction
	topic    string
	admit    *overlay.AdmittanceInstructions
	inputs   map[uint32]*Output
	payloads admissionPayloads
}

func (e *Engine) buildTopicDecision(ctx context.Context, p topicDecisionParams) (AdmissionTopicDecision, error) {
	admit := p.admit
	if admit == nil {
		admit = &overlay.AdmittanceInstructions{}
	}
	fence, err := p.host.CurrentHistoryFence(ctx, p.topic)
	if err != nil {
		return AdmissionTopicDecision{}, err
	}
	if err = p.host.EnsureHistoryFence(ctx, p.topic, fence); err != nil {
		return AdmissionTopicDecision{}, err
	}
	txid := hex.EncodeToString(p.parsed.Txid[:])
	outputs := admissionOutputs(p.tx, txid, admit, p.payloads)
	_, consumed := e.separateRetainedCoins(copyTopicInputs(p.inputs), admit.CoinsToRetain)
	return AdmissionTopicDecision{
		Topic:           p.topic,
		ExpectedHistory: fence,
		Reads:           nil,
		Spends:          admissionSpends(p.inputs, txid),
		Evictions:       nil,
		Outputs:         outputs,
		Edges:           admissionEdges(consumed, outputs),
		Applied:         AdmissionAppliedTransaction{TxID: txid},
	}, nil
}

func admissionSpends(inputs map[uint32]*Output, spender string) []AdmissionSpend {
	spends := make([]AdmissionSpend, 0, len(inputs))
	for _, output := range inputs {
		spends = append(spends, AdmissionSpend{
			Outpoint:        admissionOutpointFrom(&output.Outpoint),
			ExpectedVersion: "1",
			Spender:         spender,
		})
	}
	return spends
}

func admissionAncillaryIDs(admit *overlay.AdmittanceInstructions) []string {
	ancillary := make([]string, 0, len(admit.AncillaryTxids))
	for _, hash := range admit.AncillaryTxids {
		if hash == nil {
			continue
		}
		ancillary = append(ancillary, hex.EncodeToString(hash[:]))
	}
	return ancillary
}

func admissionOutputs(tx *transaction.Transaction, txid string, admit *overlay.AdmittanceInstructions, payloads admissionPayloads) []AdmissionOutput {
	ancillary := admissionAncillaryIDs(admit)
	outputs := make([]AdmissionOutput, 0, len(admit.OutputsToAdmit))
	for _, vout := range admit.OutputsToAdmit {
		scriptRef, ok := payloads.scripts[vout]
		if !ok {
			scriptRef = payloads.raw
		}
		satoshis, scriptLen := admittedOutputValue(tx, vout, scriptRef)
		outputs = append(outputs, AdmissionOutput{
			AdmissionOutpoint: AdmissionOutpoint{TxID: txid, OutputIndex: StorageUint64(strconv.FormatUint(uint64(vout), 10))},
			Satoshis:          satoshis,
			Score:             "0",
			Script:            AdmissionScriptRange{Payload: scriptRef, Offset: "0", ByteLength: scriptLen},
			Ancillary:         append([]string(nil), ancillary...),
		})
	}
	return outputs
}

func admittedOutputValue(tx *transaction.Transaction, vout uint32, scriptRef AdmissionPayloadRef) (StorageUint64, StorageUint64) {
	satoshis := StorageUint64("0")
	scriptLen := scriptRef.ByteLength
	if int(vout) >= len(tx.Outputs) || tx.Outputs[vout] == nil {
		return satoshis, scriptLen
	}
	satoshis = StorageUint64(strconv.FormatUint(tx.Outputs[vout].Satoshis, 10))
	if tx.Outputs[vout].LockingScript != nil {
		scriptLen = StorageUint64(strconv.Itoa(len(*tx.Outputs[vout].LockingScript)))
	}
	return satoshis, scriptLen
}

func admissionEdges(sources []*transaction.Outpoint, outputs []AdmissionOutput) []AdmissionEdge {
	edges := make([]AdmissionEdge, 0, len(sources)*len(outputs))
	for _, source := range sources {
		for _, output := range outputs {
			edges = append(edges, AdmissionEdge{Source: admissionOutpointFrom(source), Consumer: output.AdmissionOutpoint})
		}
	}
	return edges
}

func publishBytes(ctx context.Context, host admissionHost, data []byte, kind AdmissionPayloadKind) (AdmissionPayloadRef, error) {
	sum := sha256.Sum256(data)
	ref := AdmissionPayloadRef{Digest: hex.EncodeToString(sum[:]), ByteLength: StorageUint64(strconv.Itoa(len(data))), Kind: kind}
	if err := host.PublishPayload(ctx, ref, bytes.NewReader(data)); err != nil {
		return AdmissionPayloadRef{}, err
	}
	return ref, nil
}

func encodeSteakJSON(steak overlay.Steak) (string, error) {
	doc := make(map[string]steakJSONEntry, len(steak))
	for topic, admit := range steak {
		entry := steakJSONEntry{OutputsToAdmit: []uint32{}, CoinsToRetain: []uint32{}, CoinsRemoved: []uint32{}}
		if admit != nil {
			if admit.OutputsToAdmit != nil {
				entry.OutputsToAdmit = admit.OutputsToAdmit
			}
			if admit.CoinsToRetain != nil {
				entry.CoinsToRetain = admit.CoinsToRetain
			}
			if admit.CoinsRemoved != nil {
				entry.CoinsRemoved = admit.CoinsRemoved
			}
		}
		doc[topic] = entry
	}
	encoded, err := json.Marshal(doc)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func parseSavedSteak(raw string) (overlay.Steak, error) {
	var doc map[string]steakJSONEntry
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return nil, err
	}
	steak := make(overlay.Steak, len(doc))
	for topic, entry := range doc {
		admit := overlay.AdmittanceInstructions{OutputsToAdmit: entry.OutputsToAdmit, CoinsToRetain: entry.CoinsToRetain, CoinsRemoved: entry.CoinsRemoved}
		steak[topic] = &admit
	}
	return steak, nil
}

func admissionOperationID(mode AdmissionMode, txid string, topics []string) string {
	sorted := append([]string(nil), topics...)
	sort.Strings(sorted)
	sum := sha256.New()
	_, _ = io.WriteString(sum, string(mode))
	_, _ = io.WriteString(sum, txid)
	for _, topic := range sorted {
		_, _ = io.WriteString(sum, topic)
	}
	return hex.EncodeToString(sum.Sum(nil))
}

func contextDigest(values []byte) string {
	sum := sha256.Sum256(values)
	return hex.EncodeToString(sum[:])
}

func admissionOutpointFrom(outpoint *transaction.Outpoint) AdmissionOutpoint {
	return AdmissionOutpoint{TxID: hex.EncodeToString(outpoint.Txid[:]), OutputIndex: StorageUint64(strconv.FormatUint(uint64(outpoint.Index), 10))}
}

func copyTopicInputs(inputs map[uint32]*Output) map[uint32]*Output {
	copied := make(map[uint32]*Output, len(inputs))
	for vin, output := range inputs {
		copied[vin] = output
	}
	return copied
}
