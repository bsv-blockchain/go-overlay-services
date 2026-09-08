package mongodb

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/engine"
)

type admissionRejectionError struct {
	code engine.AdmissionRejectionCode
}

func (r admissionRejectionError) Error() string { return string(r.code) }

func reject(code engine.AdmissionRejectionCode) error {
	return admissionRejectionError{code: code}
}

type steakTopicDocument struct {
	OutputsToAdmit []uint32 `json:"outputsToAdmit"`
	CoinsToRetain  []uint32 `json:"coinsToRetain"`
	CoinsRemoved   []uint32 `json:"coinsRemoved"`
}

type outputDocument struct {
	ID           string    `bson:"_id"`
	Version      int32     `bson:"version"`
	Scope        string    `bson:"scope"`
	Topic        string    `bson:"topic"`
	TxID         string    `bson:"txid"`
	OutputIndex  string    `bson:"outputIndex"`
	Satoshis     string    `bson:"satoshis"`
	Score        string    `bson:"score"`
	EngineScore  float64   `bson:"engineScore"`
	Spent        bool      `bson:"spent"`
	Serving      bool      `bson:"serving"`
	SpendVersion string    `bson:"spendVersion"`
	SpentBy      string    `bson:"spentBy,omitempty"`
	MerkleState  string    `bson:"merkleState"`
	ScriptDigest string    `bson:"scriptDigest,omitempty"`
	ScriptKind   string    `bson:"scriptKind,omitempty"`
	ScriptOffset string    `bson:"scriptOffset,omitempty"`
	ScriptLength string    `bson:"scriptLength,omitempty"`
	BlockHeight  string    `bson:"blockHeight,omitempty"`
	BlockIndex   string    `bson:"blockIndex,omitempty"`
	MerkleRoot   string    `bson:"merkleRoot,omitempty"`
	Ancillary    []string  `bson:"ancillaryTxids,omitempty"`
	CreatedAt    time.Time `bson:"createdAt"`
	UpdatedAt    time.Time `bson:"updatedAt"`
}

type edgeDocument struct {
	ID            string    `bson:"_id"`
	Version       int32     `bson:"version"`
	Scope         string    `bson:"scope"`
	Topic         string    `bson:"topic"`
	SourceTxID    string    `bson:"sourceTxid"`
	SourceIndex   string    `bson:"sourceIndex"`
	ConsumerTxID  string    `bson:"consumerTxid"`
	ConsumerIndex string    `bson:"consumerIndex"`
	CreatedAt     time.Time `bson:"createdAt"`
}

type appliedDocument struct {
	ID              string    `bson:"_id"`
	Version         int32     `bson:"version"`
	Scope           string    `bson:"scope"`
	Topic           string    `bson:"topic"`
	TxID            string    `bson:"txid"`
	Proven          bool      `bson:"proven"`
	FirstSeenHeight string    `bson:"firstSeenHeight,omitempty"`
	ProofDigest     string    `bson:"proofDigest,omitempty"`
	ProofKind       string    `bson:"proofKind,omitempty"`
	ProofLength     string    `bson:"proofLength,omitempty"`
	BlockHeight     string    `bson:"blockHeight,omitempty"`
	BlockHash       string    `bson:"blockHash,omitempty"`
	BlockIndex      string    `bson:"blockIndex,omitempty"`
	MerkleRoot      string    `bson:"merkleRoot,omitempty"`
	CreatedAt       time.Time `bson:"createdAt"`
	UpdatedAt       time.Time `bson:"updatedAt"`
}

type fenceDocument struct {
	ID                     string    `bson:"_id"`
	Version                int32     `bson:"version"`
	Scope                  string    `bson:"scope"`
	Topic                  string    `bson:"topic"`
	ChainEpoch             string    `bson:"chainEpoch"`
	TopicHistoryGeneration string    `bson:"topicHistoryGeneration"`
	AffectedFromHeight     string    `bson:"affectedFromHeight,omitempty"`
	Checkpoint             string    `bson:"checkpoint,omitempty"`
	CreatedAt              time.Time `bson:"createdAt"`
	UpdatedAt              time.Time `bson:"updatedAt"`
}

type leaseDocument struct {
	ID                     string    `bson:"_id"`
	Version                int32     `bson:"version"`
	Scope                  string    `bson:"scope"`
	Topic                  string    `bson:"topic"`
	PeerID                 string    `bson:"peerId"`
	JobID                  string    `bson:"jobId"`
	ChainEpoch             string    `bson:"chainEpoch"`
	TopicHistoryGeneration string    `bson:"topicHistoryGeneration"`
	Token                  string    `bson:"token"`
	ExpiresAtMS            string    `bson:"expiresAtMs"`
	CreatedAt              time.Time `bson:"createdAt"`
	UpdatedAt              time.Time `bson:"updatedAt"`
}

type readDocument struct {
	ID          string    `bson:"_id"`
	Version     int32     `bson:"version"`
	Scope       string    `bson:"scope"`
	Topic       string    `bson:"topic"`
	Key         string    `bson:"key"`
	ReadVersion string    `bson:"readVersion"`
	CreatedAt   time.Time `bson:"createdAt"`
	UpdatedAt   time.Time `bson:"updatedAt"`
}

type payloadRefDocument struct {
	Digest     string `bson:"digest"`
	ByteLength string `bson:"byteLength"`
	Kind       string `bson:"kind"`
}

type outboxDocument struct {
	ID          string                     `bson:"_id"`
	Version     int32                      `bson:"version"`
	Scope       string                     `bson:"scope"`
	EventID     string                     `bson:"eventId"`
	Kind        engine.AdmissionOutboxKind `bson:"kind"`
	Target      string                     `bson:"target"`
	State       string                     `bson:"state"`
	OperationID string                     `bson:"operationID"`
	Payloads    []payloadRefDocument       `bson:"payloads,omitempty"`
	CreatedAt   time.Time                  `bson:"createdAt"`
	UpdatedAt   time.Time                  `bson:"updatedAt"`
}

// CommitAdmission atomically validates and persists one admission plan.
// Matching committed retries return the saved receipt before predicate checks.
func (s *Store) CommitAdmission(ctx context.Context, plan engine.AdmissionCommit) (engine.AdmissionCommitResult, error) {
	if mongo.SessionFromContext(ctx) != nil {
		return engine.AdmissionCommitResult{}, ErrNestedTransaction
	}
	if code := s.prevalidateAdmission(plan); code != "" {
		return engine.AdmissionCommitResult{State: engine.AdmissionCommitStateRejected, RejectionCode: code}, nil
	}
	result, err := s.ExecuteOperation(ctx, plan.Key, func(sessionCtx context.Context) (engine.AdmissionReceipt, error) {
		return s.applyAdmission(sessionCtx, plan)
	})
	if err != nil {
		var rejected admissionRejectionError
		if errors.As(err, &rejected) {
			return engine.AdmissionCommitResult{State: engine.AdmissionCommitStateRejected, RejectionCode: rejected.code}, nil
		}
		return result, err
	}
	return result, nil
}

// ReconcileAdmission recovers the same opaque attempt without starting a new body.
func (s *Store) ReconcileAdmission(ctx context.Context, key engine.AdmissionOperationKey, attemptID *string) (engine.AdmissionReconcileResult, error) {
	return s.ReconcileOperation(ctx, key, attemptID)
}

func (s *Store) prevalidateAdmission(plan engine.AdmissionCommit) engine.AdmissionRejectionCode {
	if plan.Key.Scope != s.config.Scope || plan.Identity.Scope != plan.Key.Scope {
		return engine.AdmissionRejectionDigestMismatch
	}
	digest, err := engine.AdmissionSemanticDigest(plan.Identity)
	if err != nil || digest != plan.Key.SemanticDigest {
		return engine.AdmissionRejectionDigestMismatch
	}
	if s.projector != nil && engine.GetReplaySafeProjection(s.projector) == nil {
		return engine.AdmissionRejectionUnsupportedProjection
	}
	return ""
}

func (s *Store) planShapeValid(plan engine.AdmissionCommit) bool {
	topics := make(map[string]struct{}, len(plan.Identity.Topics))
	for _, topic := range plan.Identity.Topics {
		if _, exists := topics[topic.Topic]; exists || !validText(topic.Topic) || !validText(topic.PolicyID) {
			return false
		}
		topics[topic.Topic] = struct{}{}
	}
	if len(plan.Decisions) != len(topics) {
		return false
	}
	seenDecisions := make(map[string]struct{}, len(plan.Decisions))
	for _, decision := range plan.Decisions {
		if _, exists := seenDecisions[decision.Topic]; exists || !validText(decision.Topic) {
			return false
		}
		if _, ok := topics[decision.Topic]; !ok {
			return false
		}
		seenDecisions[decision.Topic] = struct{}{}
	}
	eventIDs := make(map[string]struct{}, len(plan.Outbox))
	for _, intent := range plan.Outbox {
		if _, exists := eventIDs[intent.EventID]; exists || !validText(intent.EventID) || !validText(intent.Target) {
			return false
		}
		if intent.Kind != engine.AdmissionOutboxLookup && intent.Kind != engine.AdmissionOutboxPropagation {
			return false
		}
		if plan.Identity.Mode == engine.AdmissionModeHistorical && intent.Kind == engine.AdmissionOutboxPropagation {
			return false
		}
		eventIDs[intent.EventID] = struct{}{}
	}
	if !s.planEffectsValid(plan) {
		return false
	}
	return s.steakBound(plan)
}

func (s *Store) planEffectsValid(plan engine.AdmissionCommit) bool {
	if s.admissionReferences(plan) == nil {
		return false
	}
	for _, decision := range plan.Decisions {
		if _, err := engine.ParseStorageUint64(decision.ExpectedHistory.ChainEpoch); err != nil {
			return false
		}
		if _, err := engine.ParseStorageUint64(decision.ExpectedHistory.TopicHistoryGeneration); err != nil {
			return false
		}
		for _, spend := range decision.Spends {
			if !validOutpoint(spend.Outpoint) || spend.Spender != plan.Identity.TxID || !validText(spend.ExpectedVersion) {
				return false
			}
		}
		for _, eviction := range decision.Evictions {
			if !validOutpoint(eviction) {
				return false
			}
		}
		for _, output := range decision.Outputs {
			if output.TxID != plan.Identity.TxID || !validAdmissionOutput(output) {
				return false
			}
		}
		for _, edge := range decision.Edges {
			if !validOutpoint(edge.Source) || !validOutpoint(edge.Consumer) {
				return false
			}
		}
		if !validApplied(decision.Applied, plan.Identity.TxID) {
			return false
		}
		if decision.HistoryUpdate != nil && !validHistoryUpdate(*decision.HistoryUpdate, decision.ExpectedHistory) {
			return false
		}
	}
	return true
}

func (s *Store) steakBound(plan engine.AdmissionCommit) bool {
	var steak map[string]steakTopicDocument
	if json.Unmarshal([]byte(plan.Steak), &steak) != nil || len(steak) != len(plan.Decisions) {
		return false
	}
	for _, decision := range plan.Decisions {
		entry, ok := steak[decision.Topic]
		if !ok || entry.OutputsToAdmit == nil || entry.CoinsToRetain == nil || entry.CoinsRemoved == nil {
			return false
		}
		if len(entry.OutputsToAdmit) != len(decision.Outputs) {
			return false
		}
		for i, output := range decision.Outputs {
			index, err := engine.ParseStorageOutputIndex(output.OutputIndex)
			if err != nil || entry.OutputsToAdmit[i] != index {
				return false
			}
		}
	}
	return true
}

func (s *Store) admissionReferences(plan engine.AdmissionCommit) []engine.AdmissionPayloadRef {
	refs := append([]engine.AdmissionPayloadRef(nil), plan.Payloads...)
	for _, decision := range plan.Decisions {
		for _, output := range decision.Outputs {
			refs = append(refs, output.Script.Payload)
		}
		if decision.Applied.Proof != nil {
			refs = append(refs, *decision.Applied.Proof)
		}
	}
	for _, intent := range plan.Outbox {
		refs = append(refs, intent.Payloads...)
	}
	for _, ref := range refs {
		if _, err := s.validatePayload(ref); err != nil {
			return nil
		}
	}
	return refs
}

func (s *Store) applyAdmission(ctx context.Context, plan engine.AdmissionCommit) (engine.AdmissionReceipt, error) {
	if !s.planShapeValid(plan) {
		return engine.AdmissionReceipt{}, reject(engine.AdmissionRejectionInvalidPlan)
	}
	refs := s.admissionReferences(plan)
	if refs == nil {
		return engine.AdmissionReceipt{}, reject(engine.AdmissionRejectionInvalidPlan)
	}
	for _, ref := range uniquePayloads(refs) {
		if err := s.touchReadyPayload(ctx, ref); err != nil {
			if errors.Is(err, ErrPayloadUnavailable) {
				return engine.AdmissionReceipt{}, reject(engine.AdmissionRejectionPayloadNotReady)
			}
			return engine.AdmissionReceipt{}, err
		}
	}
	if err := s.validateAdmissionState(ctx, plan); err != nil {
		return engine.AdmissionReceipt{}, err
	}
	for _, ref := range uniquePayloads(refs) {
		owner := ReferenceOwner{Kind: ReferencePin, ID: plan.Key.OperationID}
		if err := s.pinPayload(ctx, ref, owner); err != nil {
			return engine.AdmissionReceipt{}, err
		}
	}
	if err := s.writeAdmissionEffects(ctx, plan); err != nil {
		return engine.AdmissionReceipt{}, err
	}
	return makeAdmissionReceipt(plan), nil
}

func (s *Store) validateAdmissionState(ctx context.Context, plan engine.AdmissionCommit) error {
	for _, intent := range plan.Outbox {
		err := s.db.Collection(outboxCollection).FindOne(ctx, bson.D{{Key: fieldID, Value: s.outboxID(intent.EventID)}}).Err()
		if err == nil {
			return reject(engine.AdmissionRejectionInvalidPlan)
		}
		if !errors.Is(err, mongo.ErrNoDocuments) {
			return err
		}
	}
	for _, decision := range plan.Decisions {
		if err := s.validateDecisionState(ctx, plan.Identity.TxID, decision); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) validateDecisionState(ctx context.Context, txid string, decision engine.AdmissionTopicDecision) error {
	fence, err := s.loadFence(ctx, decision.Topic)
	if errors.Is(err, mongo.ErrNoDocuments) || (err == nil && !fenceEquals(fence, decision.ExpectedHistory)) {
		return reject(engine.AdmissionRejectionReadConflict)
	}
	if err != nil {
		return err
	}
	for _, read := range decision.Reads {
		if err = s.validateRead(ctx, decision.Topic, read); err != nil {
			return err
		}
	}
	for _, spend := range decision.Spends {
		if err = s.validateSpend(ctx, decision.Topic, spend); err != nil {
			return err
		}
	}
	for _, eviction := range decision.Evictions {
		var current outputDocument
		findErr := s.db.Collection(outputCollection).FindOne(ctx, bson.D{{Key: fieldID, Value: s.outputID(decision.Topic, eviction)}}).Decode(&current)
		if errors.Is(findErr, mongo.ErrNoDocuments) {
			return reject(engine.AdmissionRejectionInvalidPlan)
		}
		if findErr != nil {
			return findErr
		}
	}
	for _, output := range decision.Outputs {
		findErr := s.db.Collection(outputCollection).FindOne(ctx, bson.D{{Key: fieldID, Value: s.outputID(decision.Topic, output.AdmissionOutpoint)}}).Err()
		if findErr == nil {
			return reject(engine.AdmissionRejectionInvalidPlan)
		}
		if !errors.Is(findErr, mongo.ErrNoDocuments) {
			return findErr
		}
	}
	appliedErr := s.db.Collection(appliedCollection).FindOne(ctx, bson.D{{Key: fieldID, Value: s.appliedID(decision.Topic, txid)}}).Err()
	if appliedErr == nil {
		return reject(engine.AdmissionRejectionInvalidPlan)
	}
	if !errors.Is(appliedErr, mongo.ErrNoDocuments) {
		return appliedErr
	}
	if decision.HistoryUpdate != nil && decision.HistoryUpdate.Handoff != nil {
		if err = s.validateHandoff(ctx, decision.Topic, decision.ExpectedHistory, *decision.HistoryUpdate.Handoff); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) validateRead(ctx context.Context, topic string, read engine.AdmissionReadPredicate) error {
	if !validText(read.Key) {
		return reject(engine.AdmissionRejectionInvalidPlan)
	}
	var current readDocument
	err := s.db.Collection(readCollection).FindOne(ctx, bson.D{{Key: fieldID, Value: s.readID(topic, read.Key)}}).Decode(&current)
	if errors.Is(err, mongo.ErrNoDocuments) {
		if read.ExpectedVersion != nil {
			return reject(engine.AdmissionRejectionReadConflict)
		}
		return nil
	}
	if err != nil {
		return err
	}
	if read.ExpectedVersion == nil || current.ReadVersion != *read.ExpectedVersion {
		return reject(engine.AdmissionRejectionReadConflict)
	}
	return nil
}

func (s *Store) validateSpend(ctx context.Context, topic string, spend engine.AdmissionSpend) error {
	var current outputDocument
	err := s.db.Collection(outputCollection).FindOne(ctx, bson.D{{Key: fieldID, Value: s.outputID(topic, spend.Outpoint)}}).Decode(&current)
	if errors.Is(err, mongo.ErrNoDocuments) || (err == nil && (current.SpendVersion != spend.ExpectedVersion || current.Spent || current.SpentBy != "")) {
		return reject(engine.AdmissionRejectionSpendConflict)
	}
	return err
}

func (s *Store) validateHandoff(ctx context.Context, topic string, expected engine.HistoryFence, handoff engine.HistoryRevisionHandoff) error {
	nowMS, err := s.admissionNowMS(ctx)
	if err != nil {
		return err
	}
	current, err := s.loadLease(ctx, handoff.Expected)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return reject(engine.AdmissionRejectionReadConflict)
		}
		return err
	}
	if current.Scope != s.scopeID || current.Topic != topic || !fenceEquals(fenceFromLease(current), expected) {
		return reject(engine.AdmissionRejectionReadConflict)
	}
	ok, leaseErr := engine.IsRecoveryLeaseCurrent(handoff.Expected, leaseFromDocument(current, s.config.Scope), nowMS)
	if leaseErr != nil {
		return leaseErr
	}
	if !ok {
		return reject(engine.AdmissionRejectionReadConflict)
	}
	return nil
}

func (s *Store) writeAdmissionEffects(ctx context.Context, plan engine.AdmissionCommit) error {
	now := time.Now().UTC()
	for _, decision := range plan.Decisions {
		if err := s.writeDecisionEffects(ctx, decision, now); err != nil {
			return err
		}
	}
	for _, intent := range plan.Outbox {
		payloads, payloadErr := encodePayloadRefs(intent.Payloads)
		if payloadErr != nil {
			return payloadErr
		}
		doc := outboxDocument{
			ID: s.outboxID(intent.EventID), Version: schemaVersion, Scope: s.scopeID, EventID: intent.EventID,
			Kind: intent.Kind, Target: intent.Target, State: outboxStatePending, OperationID: plan.Key.OperationID,
			Payloads: payloads, CreatedAt: now, UpdatedAt: now,
		}
		if _, err := s.db.Collection(outboxCollection).InsertOne(ctx, doc); err != nil {
			if mongo.IsDuplicateKeyError(err) {
				return reject(engine.AdmissionRejectionInvalidPlan)
			}
			return err
		}
	}
	return nil
}

func (s *Store) writeDecisionEffects(ctx context.Context, decision engine.AdmissionTopicDecision, now time.Time) error {
	for _, spend := range decision.Spends {
		filter := bson.D{{Key: fieldID, Value: s.outputID(decision.Topic, spend.Outpoint)}, {Key: fieldSpendVersion, Value: spend.ExpectedVersion}, {Key: fieldSpent, Value: false}}
		result, err := s.db.Collection(outputCollection).UpdateOne(ctx, filter, bson.D{{Key: fieldSet, Value: bson.D{{Key: fieldSpent, Value: true}, {Key: fieldSpentBy, Value: spend.Spender}, {Key: fieldUpdatedAt, Value: now}}}})
		if err != nil {
			return err
		}
		if result.MatchedCount != 1 {
			return reject(engine.AdmissionRejectionSpendConflict)
		}
	}
	for _, eviction := range decision.Evictions {
		result, err := s.db.Collection(outputCollection).DeleteOne(ctx, bson.D{{Key: fieldID, Value: s.outputID(decision.Topic, eviction)}})
		if err != nil {
			return err
		}
		if result.DeletedCount != 1 {
			return reject(engine.AdmissionRejectionInvalidPlan)
		}
	}
	for _, output := range decision.Outputs {
		if err := s.insertAdmissionOutput(ctx, decision.Topic, output, now); err != nil {
			return err
		}
	}
	for _, edge := range decision.Edges {
		if err := s.insertAdmissionEdge(ctx, decision.Topic, edge, now); err != nil {
			return err
		}
	}
	if err := s.insertApplied(ctx, decision.Topic, decision.Applied, now); err != nil {
		return err
	}
	if decision.HistoryUpdate != nil {
		return s.applyHistoryUpdate(ctx, decision.Topic, decision.ExpectedHistory, *decision.HistoryUpdate, now)
	}
	return nil
}

func (s *Store) insertAdmissionOutput(ctx context.Context, topic string, output engine.AdmissionOutput, now time.Time) error {
	index, err := encodeOutpointIndex(output.OutputIndex)
	if err != nil {
		return reject(engine.AdmissionRejectionInvalidPlan)
	}
	satoshis, err := EncodeUint64(output.Satoshis)
	if err != nil {
		return reject(engine.AdmissionRejectionInvalidPlan)
	}
	score, err := EncodeUint64(output.Score)
	if err != nil {
		return reject(engine.AdmissionRejectionInvalidPlan)
	}
	offset, err := EncodeUint64(output.Script.Offset)
	if err != nil {
		return reject(engine.AdmissionRejectionInvalidPlan)
	}
	length, err := EncodeUint64(output.Script.ByteLength)
	if err != nil {
		return reject(engine.AdmissionRejectionInvalidPlan)
	}
	engineScore := engineScoreFromUint(output.Score)
	doc := outputDocument{
		ID: s.outputID(topic, output.AdmissionOutpoint), Version: schemaVersion, Scope: s.scopeID, Topic: topic,
		TxID: output.TxID, OutputIndex: index, Satoshis: satoshis, Score: score, EngineScore: engineScore,
		Spent: false, Serving: true, SpendVersion: spendVersionInitial, MerkleState: merkleStateUnmined,
		ScriptDigest: output.Script.Payload.Digest, ScriptKind: string(output.Script.Payload.Kind),
		ScriptOffset: offset, ScriptLength: length, CreatedAt: now, UpdatedAt: now,
	}
	if _, err = s.db.Collection(outputCollection).InsertOne(ctx, doc); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return reject(engine.AdmissionRejectionInvalidPlan)
		}
		return err
	}
	return nil
}

func (s *Store) insertAdmissionEdge(ctx context.Context, topic string, edge engine.AdmissionEdge, now time.Time) error {
	sourceIndex, err := encodeOutpointIndex(edge.Source.OutputIndex)
	if err != nil {
		return reject(engine.AdmissionRejectionInvalidPlan)
	}
	consumerIndex, err := encodeOutpointIndex(edge.Consumer.OutputIndex)
	if err != nil {
		return reject(engine.AdmissionRejectionInvalidPlan)
	}
	doc := edgeDocument{
		ID: s.edgeID(topic, edge), Version: schemaVersion, Scope: s.scopeID, Topic: topic,
		SourceTxID: edge.Source.TxID, SourceIndex: sourceIndex, ConsumerTxID: edge.Consumer.TxID,
		ConsumerIndex: consumerIndex, CreatedAt: now,
	}
	if _, err = s.db.Collection(edgeCollection).InsertOne(ctx, doc); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return nil
		}
		return err
	}
	return nil
}

func (s *Store) insertApplied(ctx context.Context, topic string, applied engine.AdmissionAppliedTransaction, now time.Time) error {
	doc := appliedDocument{ID: s.appliedID(topic, applied.TxID), Version: schemaVersion, Scope: s.scopeID, Topic: topic, TxID: applied.TxID, Proven: applied.Block != nil, CreatedAt: now, UpdatedAt: now}
	if applied.FirstSeenHeight != nil {
		encoded, err := EncodeUint64(*applied.FirstSeenHeight)
		if err != nil {
			return reject(engine.AdmissionRejectionInvalidPlan)
		}
		doc.FirstSeenHeight = encoded
	}
	if applied.Proof != nil {
		length, err := EncodeUint64(applied.Proof.ByteLength)
		if err != nil {
			return reject(engine.AdmissionRejectionInvalidPlan)
		}
		doc.ProofDigest = applied.Proof.Digest
		doc.ProofKind = string(applied.Proof.Kind)
		doc.ProofLength = length
	}
	if applied.Block != nil {
		height, err := EncodeUint64(applied.Block.Height)
		if err != nil {
			return reject(engine.AdmissionRejectionInvalidPlan)
		}
		index, err := EncodeUint64(applied.Block.Index)
		if err != nil {
			return reject(engine.AdmissionRejectionInvalidPlan)
		}
		doc.BlockHeight = height
		doc.BlockHash = applied.Block.Hash
		doc.BlockIndex = index
		doc.MerkleRoot = applied.Block.MerkleRoot
	}
	if _, err := s.db.Collection(appliedCollection).InsertOne(ctx, doc); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return reject(engine.AdmissionRejectionInvalidPlan)
		}
		return err
	}
	return nil
}

func (s *Store) applyHistoryUpdate(ctx context.Context, topic string, expected engine.HistoryFence, update engine.AdmissionHistoryUpdate, now time.Time) error {
	expectedEpoch, err := EncodeUint64(expected.ChainEpoch)
	if err != nil {
		return reject(engine.AdmissionRejectionInvalidPlan)
	}
	expectedGen, err := EncodeUint64(expected.TopicHistoryGeneration)
	if err != nil {
		return reject(engine.AdmissionRejectionInvalidPlan)
	}
	nextGen, err := EncodeUint64(update.NextTopicHistoryGeneration)
	if err != nil {
		return reject(engine.AdmissionRejectionInvalidPlan)
	}
	affected, err := EncodeUint64(update.AffectedFromHeight)
	if err != nil {
		return reject(engine.AdmissionRejectionInvalidPlan)
	}
	set := bson.D{{Key: fieldTopicHistoryGeneration, Value: nextGen}, {Key: fieldAffectedFromHeight, Value: affected}, {Key: fieldUpdatedAt, Value: now}}
	if update.Handoff != nil {
		set = append(set, bson.E{Key: fieldCheckpoint, Value: update.Handoff.Checkpoint})
	}
	filter := bson.D{{Key: fieldID, Value: s.fenceID(topic)}, {Key: fieldChainEpoch, Value: expectedEpoch}, {Key: fieldTopicHistoryGeneration, Value: expectedGen}}
	result, err := s.db.Collection(fenceCollection).UpdateOne(ctx, filter, bson.D{{Key: fieldSet, Value: set}})
	if err != nil {
		return err
	}
	if result.MatchedCount != 1 {
		return reject(engine.AdmissionRejectionReadConflict)
	}
	if update.Handoff == nil {
		return nil
	}
	leaseFilter := bson.D{{Key: fieldID, Value: s.leaseID(update.Handoff.Expected)}, {Key: fieldToken, Value: mustEncodeUint64(update.Handoff.Expected.LeaseToken)}}
	leaseSet := bson.D{{Key: fieldTopicHistoryGeneration, Value: nextGen}, {Key: fieldUpdatedAt, Value: now}}
	_, err = s.db.Collection(leaseCollection).UpdateOne(ctx, leaseFilter, bson.D{{Key: fieldSet, Value: leaseSet}})
	return err
}

func (s *Store) loadFence(ctx context.Context, topic string) (engine.HistoryFence, error) {
	var doc fenceDocument
	err := s.db.Collection(fenceCollection).FindOne(ctx, bson.D{{Key: fieldID, Value: s.fenceID(topic)}}).Decode(&doc)
	if err != nil {
		return engine.HistoryFence{}, err
	}
	epoch, err := DecodeUint64(doc.ChainEpoch)
	if err != nil {
		return engine.HistoryFence{}, err
	}
	generation, err := DecodeUint64(doc.TopicHistoryGeneration)
	if err != nil {
		return engine.HistoryFence{}, err
	}
	return engine.HistoryFence{ChainEpoch: epoch, TopicHistoryGeneration: generation}, nil
}

func (s *Store) loadLease(ctx context.Context, lease engine.RecoveryLease) (leaseDocument, error) {
	var doc leaseDocument
	err := s.db.Collection(leaseCollection).FindOne(ctx, bson.D{{Key: fieldID, Value: s.leaseID(lease)}}).Decode(&doc)
	return doc, err
}

func (s *Store) admissionNowMS(ctx context.Context) (engine.StorageUint64, error) {
	var clock struct {
		NowMS string `bson:"nowMs"`
	}
	err := s.db.Collection(schemaCollection).FindOne(ctx, bson.D{{Key: fieldID, Value: tupleID("clock", s.scopeID)}}).Decode(&clock)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return engine.StorageUint64(strconv.FormatUint(uint64(time.Now().UnixMilli()), 10)), nil
	}
	if err != nil {
		return "", err
	}
	return DecodeUint64(clock.NowMS)
}

// CurrentHistoryFence returns the topic's durable fence, or the zero fence when none exists.
func (s *Store) CurrentHistoryFence(ctx context.Context, topic string) (engine.HistoryFence, error) {
	fence, err := s.loadFence(ctx, topic)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return engine.HistoryFence{ChainEpoch: "0", TopicHistoryGeneration: "0"}, nil
	}
	return fence, err
}

// EnsureHistoryFence inserts the fence when the topic has none. An existing fence is left unchanged.
func (s *Store) EnsureHistoryFence(ctx context.Context, topic string, fence engine.HistoryFence) error {
	if !validText(topic) {
		return ErrInvalidConfig
	}
	epoch, err := EncodeUint64(fence.ChainEpoch)
	if err != nil {
		return err
	}
	generation, err := EncodeUint64(fence.TopicHistoryGeneration)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	doc := fenceDocument{ID: s.fenceID(topic), Version: schemaVersion, Scope: s.scopeID, Topic: topic, ChainEpoch: epoch, TopicHistoryGeneration: generation, CreatedAt: now, UpdatedAt: now}
	_, err = s.db.Collection(fenceCollection).UpdateOne(ctx, bson.D{{Key: fieldID, Value: doc.ID}}, bson.D{{Key: "$setOnInsert", Value: doc}}, options.UpdateOne().SetUpsert(true))
	if mongo.IsDuplicateKeyError(err) {
		return nil
	}
	return err
}

func (s *Store) outputID(topic string, outpoint engine.AdmissionOutpoint) string {
	index, err := encodeOutpointIndex(outpoint.OutputIndex)
	if err != nil {
		index = string(outpoint.OutputIndex)
	}
	return tupleID("output", s.scopeID, topic, outpoint.TxID, index)
}

func (s *Store) edgeID(topic string, edge engine.AdmissionEdge) string {
	source, _ := encodeOutpointIndex(edge.Source.OutputIndex)
	consumer, _ := encodeOutpointIndex(edge.Consumer.OutputIndex)
	return tupleID("edge", s.scopeID, topic, edge.Source.TxID, source, edge.Consumer.TxID, consumer)
}

func (s *Store) appliedID(topic, txid string) string {
	return tupleID("applied", s.scopeID, topic, txid)
}

func (s *Store) outboxID(eventID string) string {
	return tupleID("outbox", s.scopeID, eventID)
}

func (s *Store) readID(topic, key string) string {
	return tupleID("read", s.scopeID, topic, key)
}

func (s *Store) fenceID(topic string) string {
	return tupleID("fence", s.scopeID, topic)
}

func (s *Store) leaseID(lease engine.RecoveryLease) string {
	return tupleID("lease", s.scopeID, lease.Topic, lease.PeerID, lease.JobID)
}

func (s *Store) cursorID(host, topic string) string {
	return tupleID("cursor", s.scopeID, host, topic)
}

func (s *Store) transactionID(txid string) string {
	return tupleID("transaction", s.chainID, txid)
}

func encodeOutpointIndex(value engine.StorageUint64) (string, error) {
	if _, err := engine.ParseStorageOutputIndex(value); err != nil {
		return "", err
	}
	return EncodeUint64(value)
}

func validOutpoint(outpoint engine.AdmissionOutpoint) bool {
	if !validHash(outpoint.TxID) {
		return false
	}
	_, err := engine.ParseStorageOutputIndex(outpoint.OutputIndex)
	return err == nil
}

func validAdmissionOutput(output engine.AdmissionOutput) bool {
	if !validOutpoint(output.AdmissionOutpoint) {
		return false
	}
	for _, value := range []engine.StorageUint64{output.Satoshis, output.Score, output.Script.Offset, output.Script.ByteLength} {
		if _, err := engine.ParseStorageUint64(value); err != nil {
			return false
		}
	}
	if _, err := engine.ParseStorageUint64(output.Script.Payload.ByteLength); err != nil || !validHash(output.Script.Payload.Digest) {
		return false
	}
	offset, _ := engine.ParseStorageUint64(output.Script.Offset)
	length, _ := engine.ParseStorageUint64(output.Script.ByteLength)
	total, _ := engine.ParseStorageUint64(output.Script.Payload.ByteLength)
	return offset+length <= total
}

func validApplied(applied engine.AdmissionAppliedTransaction, txid string) bool {
	if applied.TxID != txid || !validHash(applied.TxID) {
		return false
	}
	if applied.FirstSeenHeight != nil {
		if _, err := engine.ParseStorageUint64(*applied.FirstSeenHeight); err != nil {
			return false
		}
	}
	if applied.Proof != nil {
		if !validHash(applied.Proof.Digest) {
			return false
		}
		if _, err := engine.ParseStorageUint64(applied.Proof.ByteLength); err != nil {
			return false
		}
	}
	if applied.Block == nil {
		return true
	}
	if !validHash(applied.Block.Hash) || !validHash(applied.Block.MerkleRoot) {
		return false
	}
	_, heightErr := engine.ParseStorageUint64(applied.Block.Height)
	_, indexErr := engine.ParseStorageUint64(applied.Block.Index)
	return heightErr == nil && indexErr == nil
}

func validHistoryUpdate(update engine.AdmissionHistoryUpdate, expected engine.HistoryFence) bool {
	next, err := engine.ParseStorageUint64(update.NextTopicHistoryGeneration)
	if err != nil {
		return false
	}
	current, err := engine.ParseStorageUint64(expected.TopicHistoryGeneration)
	if err != nil || next <= current {
		return false
	}
	_, err = engine.ParseStorageUint64(update.AffectedFromHeight)
	return err == nil && (update.Handoff == nil || validText(update.Handoff.Checkpoint))
}

func fenceEquals(actual engine.HistoryFence, expected engine.HistoryFence) bool {
	return actual.ChainEpoch == expected.ChainEpoch && actual.TopicHistoryGeneration == expected.TopicHistoryGeneration
}

func fenceFromLease(doc leaseDocument) engine.HistoryFence {
	epoch, _ := DecodeUint64(doc.ChainEpoch)
	generation, _ := DecodeUint64(doc.TopicHistoryGeneration)
	return engine.HistoryFence{ChainEpoch: epoch, TopicHistoryGeneration: generation}
}

func leaseFromDocument(doc leaseDocument, scope engine.StorageScope) engine.RecoveryLease {
	token, _ := DecodeUint64(doc.Token)
	expires, _ := DecodeUint64(doc.ExpiresAtMS)
	return engine.RecoveryLease{
		HistoryFence: fenceFromLease(doc),
		Scope:        scope,
		Topic:        doc.Topic,
		PeerID:       doc.PeerID,
		JobID:        doc.JobID,
		LeaseToken:   token,
		ExpiresAtMS:  expires,
	}
}

func encodePayloadRefs(refs []engine.AdmissionPayloadRef) ([]payloadRefDocument, error) {
	out := make([]payloadRefDocument, 0, len(refs))
	for _, ref := range refs {
		length, err := EncodeUint64(ref.ByteLength)
		if err != nil {
			return nil, reject(engine.AdmissionRejectionInvalidPlan)
		}
		out = append(out, payloadRefDocument{Digest: ref.Digest, ByteLength: length, Kind: string(ref.Kind)})
	}
	return out, nil
}

func uniquePayloads(refs []engine.AdmissionPayloadRef) []engine.AdmissionPayloadRef {
	seen := make(map[string]struct{}, len(refs))
	out := make([]engine.AdmissionPayloadRef, 0, len(refs))
	for _, ref := range refs {
		key := ref.Digest + string(ref.Kind) + string(ref.ByteLength)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ref)
	}
	return out
}

func makeAdmissionReceipt(plan engine.AdmissionCommit) engine.AdmissionReceipt {
	indexes := make([]engine.AdmissionIndexStatus, 0)
	seen := make(map[string]struct{})
	propagation := engine.AdmissionPropagationNotRequested
	for _, intent := range plan.Outbox {
		if intent.Kind == engine.AdmissionOutboxPropagation {
			propagation = engine.AdmissionPropagationPending
		}
		if intent.Kind != engine.AdmissionOutboxLookup {
			continue
		}
		if _, exists := seen[intent.Target]; exists {
			continue
		}
		seen[intent.Target] = struct{}{}
		indexes = append(indexes, engine.AdmissionIndexStatus{Target: intent.Target, State: engine.AdmissionIndexPending})
	}
	return engine.AdmissionReceipt{
		OperationID:    plan.Key.OperationID,
		SemanticDigest: plan.Key.SemanticDigest,
		Durability:     engine.AdmissionDurabilityAtomicLocal,
		Steak:          plan.Steak,
		Indexes:        indexes,
		Propagation:    propagation,
	}
}

func engineScoreFromUint(value engine.StorageUint64) float64 {
	parsed, err := engine.ParseStorageUint64(value)
	if err != nil || parsed > 1<<53 {
		return 0
	}
	return float64(parsed)
}

func mustEncodeUint64(value engine.StorageUint64) string {
	encoded, err := EncodeUint64(value)
	if err != nil {
		return string(value)
	}
	return encoded
}

func (s *Store) seedReadyPayload(ctx context.Context, ref engine.AdmissionPayloadRef) error {
	if _, err := s.validatePayload(ref); err != nil {
		return err
	}
	length, err := EncodeUint64(ref.ByteLength)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	doc := payloadDocument{ID: s.payloadID(ref), Version: schemaVersion, Chain: s.chainID, Digest: ref.Digest, Length: length, State: payloadStateReady, Owner: s.ownerID, Token: "00000000000000000001", Guard: bson.NewObjectID(), CreatedAt: now, UpdatedAt: now, LeaseUntil: now.Add(s.config.LeaseDuration)}
	_, err = s.db.Collection(payloadCollection).UpdateOne(ctx, bson.D{{Key: fieldID, Value: doc.ID}}, bson.D{{Key: "$setOnInsert", Value: doc}}, options.UpdateOne().SetUpsert(true))
	return err
}

func (s *Store) seedRead(ctx context.Context, topic, key, version string) error {
	now := time.Now().UTC()
	doc := readDocument{ID: s.readID(topic, key), Version: schemaVersion, Scope: s.scopeID, Topic: topic, Key: key, ReadVersion: version, CreatedAt: now, UpdatedAt: now}
	_, err := s.db.Collection(readCollection).UpdateOne(ctx, bson.D{{Key: fieldID, Value: doc.ID}}, bson.D{{Key: fieldSet, Value: doc}}, options.UpdateOne().SetUpsert(true))
	return err
}

func (s *Store) seedSpendable(ctx context.Context, topic string, outpoint engine.AdmissionOutpoint, version string) error {
	index, err := encodeOutpointIndex(outpoint.OutputIndex)
	if err != nil {
		return err
	}
	zero, err := EncodeUint64("0")
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	doc := outputDocument{
		ID: s.outputID(topic, outpoint), Version: schemaVersion, Scope: s.scopeID, Topic: topic, TxID: outpoint.TxID,
		OutputIndex: index, Satoshis: zero, Score: zero, EngineScore: 0, Spent: false, Serving: true, SpendVersion: version,
		MerkleState: merkleStateUnmined, CreatedAt: now, UpdatedAt: now,
	}
	_, err = s.db.Collection(outputCollection).UpdateOne(ctx, bson.D{{Key: fieldID, Value: doc.ID}}, bson.D{{Key: fieldSet, Value: doc}}, options.UpdateOne().SetUpsert(true))
	return err
}

func (s *Store) seedLease(ctx context.Context, lease engine.RecoveryLease) error {
	epoch, err := EncodeUint64(lease.ChainEpoch)
	if err != nil {
		return err
	}
	generation, err := EncodeUint64(lease.TopicHistoryGeneration)
	if err != nil {
		return err
	}
	token, err := EncodeUint64(lease.LeaseToken)
	if err != nil {
		return err
	}
	expires, err := EncodeUint64(lease.ExpiresAtMS)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	doc := leaseDocument{
		ID: s.leaseID(lease), Version: schemaVersion, Scope: s.scopeID, Topic: lease.Topic, PeerID: lease.PeerID, JobID: lease.JobID,
		ChainEpoch: epoch, TopicHistoryGeneration: generation, Token: token, ExpiresAtMS: expires, CreatedAt: now, UpdatedAt: now,
	}
	_, err = s.db.Collection(leaseCollection).UpdateOne(ctx, bson.D{{Key: fieldID, Value: doc.ID}}, bson.D{{Key: fieldSet, Value: doc}}, options.UpdateOne().SetUpsert(true))
	return err
}

func (s *Store) seedAdmissionClock(ctx context.Context, nowMS engine.StorageUint64) error {
	encoded, err := EncodeUint64(nowMS)
	if err != nil {
		return err
	}
	doc := bson.D{{Key: fieldID, Value: tupleID("clock", s.scopeID)}, {Key: "type", Value: "clock"}, {Key: "scopeID", Value: s.scopeID}, {Key: fieldNowMS, Value: encoded}, {Key: fieldCreatedAt, Value: time.Now().UTC()}}
	_, err = s.db.Collection(schemaCollection).UpdateOne(ctx, bson.D{{Key: fieldID, Value: tupleID("clock", s.scopeID)}}, bson.D{{Key: fieldSet, Value: doc}}, options.UpdateOne().SetUpsert(true))
	return err
}
