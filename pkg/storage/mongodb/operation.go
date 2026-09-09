package mongodb

import (
	"context"
	"encoding/json"
	"errors"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/engine"
)

var errInvalidOperation = errors.New("invalid MongoDB operation or receipt")

const maxReceiptBytes = 1 << 20

type operationDocument struct {
	ID          string        `bson:"_id"`
	Version     int32         `bson:"version"`
	Scope       string        `bson:"scope"`
	OperationID string        `bson:"operationID"`
	Digest      string        `bson:"digest"`
	State       string        `bson:"state"`
	Attempt     string        `bson:"attempt"`
	Owner       string        `bson:"owner"`
	Token       string        `bson:"token"`
	LeaseUntil  time.Time     `bson:"leaseUntil"`
	Guard       bson.ObjectID `bson:"guard"`
	CreatedAt   time.Time     `bson:"createdAt"`
	UpdatedAt   time.Time     `bson:"updatedAt"`
	Receipt     *bson.Binary  `bson:"receipt,omitempty"`
}

// OperationBody runs only bounded database work on the provided session context.
// It must use that context for every write, propagate every database error, and
// perform no external I/O, GridFS operation, or irreversible side effect. It may
// run again after a proven transient abort. This low-level helper does not
// validate admission plans, predicates, spends or history fences for the caller.
type OperationBody func(context.Context) (engine.AdmissionReceipt, error)

// ExecuteOperation atomically saves a body and its exact receipt under a scoped
// semantic key. It is a persistence primitive, not AdmissionStorage: engine
// integration must still validate the entire admission plan in the body.
// Unresolved attempts return pending and prohibit a fresh body until explicitly
// reconciled. A digest mismatch rejects reuse of the same operation ID.
func (s *Store) ExecuteOperation(ctx context.Context, key engine.AdmissionOperationKey, body OperationBody) (engine.AdmissionCommitResult, error) {
	if mongo.SessionFromContext(ctx) != nil {
		return engine.AdmissionCommitResult{}, ErrNestedTransaction
	}
	if err := s.validateOperation(key); err != nil {
		return engine.AdmissionCommitResult{}, err
	}
	if body == nil {
		return engine.AdmissionCommitResult{}, errInvalidOperation
	}
	operation, claimed, err := s.claimOperation(ctx, key)
	if err != nil {
		if operation.Attempt == "" {
			return engine.AdmissionCommitResult{}, err
		}
		return pendingOperation(operation.Attempt), err
	}
	if !claimed {
		return s.unclaimedOperationResult(operation, key)
	}
	return s.commitOperationBody(ctx, key, operation, body)
}

func (s *Store) unclaimedOperationResult(operation operationDocument, key engine.AdmissionOperationKey) (engine.AdmissionCommitResult, error) {
	if operation.State == "aborted" && operation.Digest == key.SemanticDigest {
		return engine.AdmissionCommitResult{}, ErrConflict
	}
	return s.operationResult(operation, key)
}

func (s *Store) commitOperationBody(ctx context.Context, key engine.AdmissionOperationKey, operation operationDocument, body OperationBody) (engine.AdmissionCommitResult, error) {
	var receipt engine.AdmissionReceipt
	outcome, transactionErr := s.runTransaction(ctx, func(sessionCtx context.Context) error {
		return s.applyOperationBody(sessionCtx, operation, key, body, &receipt)
	})
	switch outcome {
	case transactionCommitted:
		return engine.AdmissionCommitResult{State: engine.AdmissionCommitStateCommitted, Receipt: &receipt}, nil
	case transactionPending:
		// Error details remain available without changing the unresolved state.
		return pendingOperation(operation.Attempt), transactionErr
	case transactionAborted:
		// No commit was attempted, or a transient label proved the transaction
		// aborted. The majority CAS fences this attempt before allowing a retry.
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		return s.fenceAbortedOperation(cleanupCtx, key, operation, transactionErr)
	}
	return engine.AdmissionCommitResult{}, errInvalidOperation
}

func (s *Store) applyOperationBody(ctx context.Context, operation operationDocument, key engine.AdmissionOperationKey, body OperationBody, receipt *engine.AdmissionReceipt) error {
	filter := operationPredicate(operation)
	filter = append(filter, leaseCurrent()...)
	guarded, err := s.db.Collection(operationCollection).UpdateOne(ctx, filter, bson.D{{Key: fieldSet, Value: bson.D{{Key: fieldGuard, Value: bson.NewObjectID()}}}})
	if err != nil {
		return err
	}
	if guarded.MatchedCount != 1 {
		return ErrConflict
	}
	generated, err := body(ctx)
	if err != nil {
		return err
	}
	encoded, err := encodeReceipt(key, generated)
	if err != nil {
		return err
	}
	saved, err := s.db.Collection(operationCollection).UpdateOne(ctx, filter, mongo.Pipeline{bson.D{{Key: fieldSet, Value: bson.D{{Key: fieldState, Value: "committed"}, {Key: "receipt", Value: bson.D{{Key: fieldLiteral, Value: bson.Binary{Subtype: 0, Data: encoded}}}}, {Key: fieldUpdatedAt, Value: serverNow}, {Key: fieldGuard, Value: bson.NewObjectID()}}}}})
	if err != nil {
		return err
	}
	if saved.MatchedCount != 1 {
		return ErrConflict
	}
	*receipt = newReceiptDocument(generated).receipt()
	return nil
}

func (s *Store) validateOperation(key engine.AdmissionOperationKey) error {
	if key.Scope != s.config.Scope || !validText(key.OperationID) || !validHash(key.SemanticDigest) {
		return errInvalidOperation
	}
	return nil
}

func (s *Store) operationID(key engine.AdmissionOperationKey) string {
	return tupleID("operation", s.scopeID, key.OperationID)
}

func operationPredicate(operation operationDocument) bson.D {
	return bson.D{{Key: fieldID, Value: operation.ID}, {Key: fieldScope, Value: operation.Scope}, {Key: fieldDigest, Value: operation.Digest}, {Key: fieldState, Value: "pending"}, {Key: fieldAttempt, Value: operation.Attempt}, {Key: fieldOwner, Value: operation.Owner}, {Key: fieldToken, Value: operation.Token}}
}

func (s *Store) claimOperation(ctx context.Context, key engine.AdmissionOperationKey) (operationDocument, bool, error) {
	id := s.operationID(key)
	var existing operationDocument
	err := s.db.Collection(operationCollection).FindOne(ctx, bson.D{{Key: fieldID, Value: id}}).Decode(&existing)
	filter := bson.D{{Key: fieldID, Value: id}, {Key: fieldVersion, Value: bson.D{{Key: "$exists", Value: false}}}}
	token := "00000000000000000001"
	if err == nil {
		if !s.validOperationDocument(existing, key) {
			return existing, false, errInvalidOperation
		}
		if existing.Digest != key.SemanticDigest || existing.State != "aborted" {
			return existing, false, nil
		}
		token, err = nextToken(existing.Token)
		if err != nil {
			return existing, false, err
		}
		filter = bson.D{{Key: fieldID, Value: id}, {Key: fieldState, Value: "aborted"}, {Key: fieldAttempt, Value: existing.Attempt}, {Key: fieldToken, Value: existing.Token}, {Key: fieldDigest, Value: key.SemanticDigest}}
	} else if !errors.Is(err, mongo.ErrNoDocuments) {
		return existing, false, err
	}
	operation := operationDocument{ID: id, Version: schemaVersion, Scope: s.scopeID, OperationID: key.OperationID, Digest: key.SemanticDigest, State: "pending", Attempt: uuid.NewString(), Owner: s.ownerID, Token: token, Guard: bson.NewObjectID()}
	err = s.db.Collection(operationCollection).FindOneAndUpdate(ctx, filter, replaceWithServerDates(operation, s.config.LeaseDuration), options.FindOneAndUpdate().SetUpsert(existing.ID == "").SetReturnDocument(options.After)).Decode(&operation)
	if mongo.IsDuplicateKeyError(err) || errors.Is(err, mongo.ErrNoDocuments) {
		err = s.db.Collection(operationCollection).FindOne(ctx, bson.D{{Key: fieldID, Value: id}}).Decode(&existing)
		return existing, false, err
	}
	return operation, err == nil, err
}

func pendingOperation(attempt string) engine.AdmissionCommitResult {
	return engine.AdmissionCommitResult{State: engine.AdmissionCommitStatePending, AttemptID: attempt}
}

func (s *Store) fenceAbortedOperation(ctx context.Context, key engine.AdmissionOperationKey, operation operationDocument, transactionErr error) (engine.AdmissionCommitResult, error) {
	fenced, fenceErr := s.db.Collection(operationCollection).UpdateOne(ctx, operationPredicate(operation), mongo.Pipeline{bson.D{{Key: fieldSet, Value: bson.D{{Key: fieldState, Value: "aborted"}, {Key: fieldUpdatedAt, Value: serverNow}, {Key: fieldGuard, Value: bson.NewObjectID()}}}}})
	if fenceErr != nil {
		return pendingOperation(operation.Attempt), errors.Join(transactionErr, fenceErr)
	}
	if fenced.MatchedCount == 1 {
		return engine.AdmissionCommitResult{}, transactionErr
	}
	var current operationDocument
	if readErr := s.db.Collection(operationCollection).FindOne(ctx, bson.D{{Key: fieldID, Value: operation.ID}}).Decode(&current); readErr != nil {
		return pendingOperation(operation.Attempt), errors.Join(transactionErr, readErr)
	}
	result, resultErr := s.operationResult(current, key)
	if resultErr != nil {
		return pendingOperation(operation.Attempt), errors.Join(transactionErr, resultErr)
	}
	switch result.State {
	case engine.AdmissionCommitStatePending:
		return result, transactionErr
	case engine.AdmissionCommitStateAborted:
		return engine.AdmissionCommitResult{}, transactionErr
	case engine.AdmissionCommitStateCommitted, engine.AdmissionCommitStateRejected:
		return result, nil
	default:
		return pendingOperation(operation.Attempt), transactionErr
	}
}

func (s *Store) operationResult(operation operationDocument, key engine.AdmissionOperationKey) (engine.AdmissionCommitResult, error) {
	if !s.validOperationDocument(operation, key) {
		return engine.AdmissionCommitResult{}, errInvalidOperation
	}
	if operation.Digest != key.SemanticDigest {
		return engine.AdmissionCommitResult{State: engine.AdmissionCommitStateRejected, RejectionCode: engine.AdmissionRejectionDigestMismatch}, nil
	}
	switch operation.State {
	case "pending":
		return pendingOperation(operation.Attempt), nil
	case "aborted":
		return engine.AdmissionCommitResult{State: engine.AdmissionCommitStateAborted}, nil
	case "committed":
		if operation.Receipt == nil || operation.Receipt.Subtype != 0 || len(operation.Receipt.Data) > maxReceiptBytes {
			return engine.AdmissionCommitResult{}, errInvalidOperation
		}
		var saved receiptDocument
		if err := json.Unmarshal(operation.Receipt.Data, &saved); err != nil {
			return engine.AdmissionCommitResult{}, err
		}
		receipt := saved.receipt()
		if _, err := encodeReceipt(key, receipt); err != nil {
			return engine.AdmissionCommitResult{}, err
		}
		return engine.AdmissionCommitResult{State: engine.AdmissionCommitStateCommitted, Receipt: &receipt}, nil
	}
	return engine.AdmissionCommitResult{}, errInvalidOperation
}

func encodeReceipt(key engine.AdmissionOperationKey, receipt engine.AdmissionReceipt) ([]byte, error) {
	if receipt.OperationID != key.OperationID || receipt.SemanticDigest != key.SemanticDigest || receipt.Durability != engine.AdmissionDurabilityAtomicLocal || !utf8.ValidString(receipt.Steak) || len(receipt.Steak) > maxReceiptBytes || len(receipt.Indexes) > 1024 {
		return nil, errInvalidOperation
	}
	switch receipt.Propagation {
	case engine.AdmissionPropagationNotRequested, engine.AdmissionPropagationPending:
	default:
		return nil, errInvalidOperation
	}
	targets := make(map[string]struct{}, len(receipt.Indexes))
	for _, index := range receipt.Indexes {
		if !validText(index.Target) {
			return nil, errInvalidOperation
		}
		if _, duplicate := targets[index.Target]; duplicate {
			return nil, errInvalidOperation
		}
		targets[index.Target] = struct{}{}
		switch index.State {
		case engine.AdmissionIndexVisible, engine.AdmissionIndexPending:
		default:
			return nil, errInvalidOperation
		}
	}
	encoded, err := json.Marshal(newReceiptDocument(receipt))
	if err != nil {
		return nil, err
	}
	if len(encoded) > maxReceiptBytes {
		return nil, errInvalidOperation
	}
	return encoded, nil
}

// ReconcileOperation returns a majority-observed receipt, or attempts a
// majority CAS on the expired pending operation row. All bodies write that same
// row first, so a successful CAS proves the old attempt cannot subsequently
// commit. Lease expiry or an absent receipt alone never proves an abort.
// A non-nil attempt must match; an old or invented attempt cannot fence a newer
// worker. An absent row requires an explicit attempt and an inserted tombstone
// to serialize with any delayed initial claim before it can be called aborted.
func (s *Store) ReconcileOperation(ctx context.Context, key engine.AdmissionOperationKey, attempt *string) (engine.AdmissionReconcileResult, error) {
	if mongo.SessionFromContext(ctx) != nil {
		return engine.AdmissionCommitResult{}, ErrNestedTransaction
	}
	if err := s.validateOperation(key); err != nil {
		return engine.AdmissionCommitResult{}, err
	}
	if attempt != nil && !validText(*attempt) {
		return engine.AdmissionCommitResult{}, errInvalidOperation
	}
	operation, err := s.loadOrTombstoneOperation(ctx, key, attempt)
	if err != nil {
		if attempt != nil {
			return pendingOperation(*attempt), err
		}
		return engine.AdmissionCommitResult{}, err
	}
	return s.reconcileLoadedOperation(ctx, key, attempt, operation)
}

func (s *Store) loadOrTombstoneOperation(ctx context.Context, key engine.AdmissionOperationKey, attempt *string) (operationDocument, error) {
	var operation operationDocument
	err := s.db.Collection(operationCollection).FindOne(ctx, bson.D{{Key: fieldID, Value: s.operationID(key)}}).Decode(&operation)
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return operation, err
	}
	if attempt == nil {
		return operationDocument{}, errInvalidOperation
	}
	return s.insertOperationTombstone(ctx, key, *attempt)
}

func (s *Store) insertOperationTombstone(ctx context.Context, key engine.AdmissionOperationKey, attempt string) (operationDocument, error) {
	tombstone := operationDocument{ID: s.operationID(key), Version: schemaVersion, Scope: s.scopeID, OperationID: key.OperationID, Digest: key.SemanticDigest, State: "aborted", Attempt: attempt, Owner: s.ownerID, Token: "00000000000000000001", Guard: bson.NewObjectID()}
	var operation operationDocument
	err := s.db.Collection(operationCollection).FindOneAndUpdate(ctx, bson.D{{Key: fieldID, Value: tombstone.ID}, {Key: fieldVersion, Value: bson.D{{Key: "$exists", Value: false}}}}, replaceWithServerDates(tombstone, s.config.LeaseDuration), options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After)).Decode(&operation)
	if mongo.IsDuplicateKeyError(err) {
		err = s.db.Collection(operationCollection).FindOne(ctx, bson.D{{Key: fieldID, Value: tombstone.ID}}).Decode(&operation)
	}
	return operation, err
}

func (s *Store) reconcileLoadedOperation(ctx context.Context, key engine.AdmissionOperationKey, attempt *string, operation operationDocument) (engine.AdmissionCommitResult, error) {
	if !s.validOperationDocument(operation, key) {
		return engine.AdmissionCommitResult{}, errInvalidOperation
	}
	if operation.Digest != key.SemanticDigest {
		return s.operationResult(operation, key)
	}
	if attempt != nil && *attempt != operation.Attempt {
		return engine.AdmissionCommitResult{}, ErrConflict
	}
	if operation.State != "pending" {
		return s.operationResult(operation, key)
	}
	return s.fenceExpiredPendingOperation(ctx, key, attempt, operation)
}

func (s *Store) fenceExpiredPendingOperation(ctx context.Context, key engine.AdmissionOperationKey, attempt *string, operation operationDocument) (engine.AdmissionCommitResult, error) {
	filter := operationPredicate(operation)
	filter = append(filter, leaseExpired()...)
	result, err := s.db.Collection(operationCollection).UpdateOne(ctx, filter, mongo.Pipeline{bson.D{{Key: fieldSet, Value: bson.D{{Key: fieldState, Value: "aborted"}, {Key: fieldUpdatedAt, Value: serverNow}, {Key: fieldGuard, Value: bson.NewObjectID()}}}}})
	if err != nil {
		return pendingOperation(operation.Attempt), err
	}
	if result.MatchedCount == 1 {
		return engine.AdmissionCommitResult{State: engine.AdmissionCommitStateAborted}, nil
	}
	// A concurrent commit or fence wins. Re-read rather than translating a
	// failed CAS into absence or an aborted result.
	if err = s.db.Collection(operationCollection).FindOne(ctx, bson.D{{Key: fieldID, Value: operation.ID}}).Decode(&operation); err != nil {
		return pendingOperation(operation.Attempt), err
	}
	if attempt != nil && *attempt != operation.Attempt {
		return engine.AdmissionCommitResult{}, ErrConflict
	}
	return s.operationResult(operation, key)
}

// Receipt encoding is versioned adapter data; the public S01 types intentionally
// carry no persistence tags. STEAK remains an exact UTF-8 string through this DTO.
type receiptDocument struct {
	OperationID    string                           `json:"operationId"`
	SemanticDigest string                           `json:"semanticDigest"`
	Durability     engine.AdmissionDurability       `json:"durability"`
	Steak          string                           `json:"steak"`
	Indexes        []receiptIndexDocument           `json:"indexes"`
	Propagation    engine.AdmissionPropagationState `json:"propagation"`
}
type receiptIndexDocument struct {
	Target string                     `json:"target"`
	State  engine.AdmissionIndexState `json:"state"`
}

func newReceiptDocument(receipt engine.AdmissionReceipt) receiptDocument {
	saved := receiptDocument{OperationID: receipt.OperationID, SemanticDigest: receipt.SemanticDigest, Durability: receipt.Durability, Steak: receipt.Steak, Propagation: receipt.Propagation}
	if receipt.Indexes != nil {
		saved.Indexes = make([]receiptIndexDocument, len(receipt.Indexes))
	}
	for i, index := range receipt.Indexes {
		saved.Indexes[i] = receiptIndexDocument{Target: index.Target, State: index.State}
	}
	return saved
}

func (saved receiptDocument) receipt() engine.AdmissionReceipt {
	receipt := engine.AdmissionReceipt{OperationID: saved.OperationID, SemanticDigest: saved.SemanticDigest, Durability: saved.Durability, Steak: saved.Steak, Propagation: saved.Propagation}
	if saved.Indexes != nil {
		receipt.Indexes = make([]engine.AdmissionIndexStatus, len(saved.Indexes))
	}
	for i, index := range saved.Indexes {
		receipt.Indexes[i] = engine.AdmissionIndexStatus{Target: index.Target, State: index.State}
	}
	return receipt
}

func (s *Store) validOperationDocument(operation operationDocument, key engine.AdmissionOperationKey) bool {
	if operation.ID != s.operationID(key) || operation.Scope != s.scopeID || operation.OperationID != key.OperationID || operation.Version != schemaVersion || !validHash(operation.Digest) || !validText(operation.Attempt) {
		return false
	}
	_, err := DecodeUint64(operation.Token)
	return err == nil
}
