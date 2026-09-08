package mongodb

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/engine"
)

type payloadDocument struct {
	ID         string        `bson:"_id"`
	Version    int32         `bson:"version"`
	Chain      string        `bson:"chain"`
	Digest     string        `bson:"digest"`
	Length     string        `bson:"length"`
	State      string        `bson:"state"`
	Owner      string        `bson:"owner"`
	Token      string        `bson:"token"`
	LeaseUntil time.Time     `bson:"leaseUntil"`
	Guard      bson.ObjectID `bson:"guard"`
	CreatedAt  time.Time     `bson:"createdAt"`
	UpdatedAt  time.Time     `bson:"updatedAt"`
	FileID     bson.ObjectID `bson:"fileId,omitempty"`
	Inline     *bson.Binary  `bson:"inlineData,omitempty"`
	BlobOwner  string        `bson:"blobOwner,omitempty"`
	BlobToken  string        `bson:"blobToken,omitempty"`
}

// ReferenceOwner identifies a durable reason to retain a payload. Owner IDs are
// literal values, never BSON field names. References do not expire implicitly:
// their owner must explicitly release them after its durable work is complete.
type ReferenceOwner struct {
	Kind ReferenceOwnerKind
	ID   string
}

// ReferenceOwnerKind enumerates every retention domain. GC reconciles the one
// reference collection across all these kinds and all nodes in the chain.
type ReferenceOwnerKind string

// These owner kinds enumerate every supported durable retention domain.
const (
	ReferenceTransaction       ReferenceOwnerKind = "transaction"
	ReferenceAppliedHistory    ReferenceOwnerKind = "applied-history"
	ReferenceOutput            ReferenceOwnerKind = "output"
	ReferenceGASPGraph         ReferenceOwnerKind = "gasp-graph"
	ReferenceGASPNode          ReferenceOwnerKind = "gasp-node"
	ReferenceBASMJob           ReferenceOwnerKind = "basm-job"
	ReferenceLookupOutbox      ReferenceOwnerKind = "lookup-outbox"
	ReferencePropagationOutbox ReferenceOwnerKind = "propagation-outbox"
	ReferenceManifest          ReferenceOwnerKind = "manifest"
	ReferencePin               ReferenceOwnerKind = "pin"
)

type referenceDocument struct {
	ID        string                      `bson:"_id"`
	Version   int32                       `bson:"version"`
	Chain     string                      `bson:"chain"`
	Scope     string                      `bson:"scope"`
	Digest    string                      `bson:"digest"`
	Length    string                      `bson:"length"`
	Kind      engine.AdmissionPayloadKind `bson:"kind"`
	OwnerKind ReferenceOwnerKind          `bson:"ownerKind"`
	OwnerID   string                      `bson:"ownerID"`
	CreatedAt time.Time                   `bson:"createdAt"`
}

func (s *Store) validatePayload(ref engine.AdmissionPayloadRef) (uint64, error) {
	if !validHash(ref.Digest) {
		return 0, ErrInvalidPayload
	}
	switch ref.Kind {
	case engine.AdmissionPayloadRawTransaction, engine.AdmissionPayloadMerklePath, engine.AdmissionPayloadBEEFManifest, engine.AdmissionPayloadLockingScript, engine.AdmissionPayloadOutboxData:
	default:
		return 0, ErrInvalidPayload
	}
	length, err := engine.ParseStorageUint64(ref.ByteLength)
	if err != nil || length > s.config.MaxPayloadBytes {
		return 0, ErrInvalidPayload
	}
	return length, nil
}

func validOwner(owner ReferenceOwner) bool {
	if !validText(owner.ID) {
		return false
	}
	switch owner.Kind {
	case ReferenceTransaction, ReferenceAppliedHistory, ReferenceOutput, ReferenceGASPGraph, ReferenceGASPNode, ReferenceBASMJob, ReferenceLookupOutbox, ReferencePropagationOutbox, ReferenceManifest, ReferencePin:
		return true
	}
	return false
}

func (s *Store) payloadID(ref engine.AdmissionPayloadRef) string {
	return tupleID("payload", s.chainID, ref.Digest)
}

func (s *Store) referenceID(ref engine.AdmissionPayloadRef, owner ReferenceOwner) string {
	return tupleID("reference", s.scopeID, string(owner.Kind), owner.ID, ref.Digest, string(ref.Kind))
}

func nextToken(token string) (string, error) {
	value, err := DecodeUint64(token)
	if err != nil {
		return "", err
	}
	parsed, err := engine.ParseStorageUint64(value)
	if err != nil || parsed == ^uint64(0) {
		return "", ErrConflict
	}
	return EncodeUint64(engine.StorageUint64(strconv.FormatUint(parsed+1, 10)))
}

// PublishPayload verifies the addressed bytes and publishes them before any
// durable reference can be created. GridFS is streamed outside transactions;
// small content uses bounded BSON BinData. Existing ready content is reused
// across topics, kinds and nodes in this chain. The caller retains reader
// ownership and must provide a reader whose Read respects its own I/O deadline.
func (s *Store) PublishPayload(ctx context.Context, ref engine.AdmissionPayloadRef, reader io.Reader) error {
	if mongo.SessionFromContext(ctx) != nil {
		return ErrNestedTransaction
	}
	length, err := s.validatePayload(ref)
	if err != nil {
		return err
	}
	if reader == nil {
		return ErrInvalidPayload
	}
	payload, err := s.reservePayload(ctx, ref, length)
	if err != nil {
		return err
	}
	if payload.State == payloadStateReady {
		return nil
	}
	var inline *bson.Binary
	if length <= s.config.InlineLimit {
		data, readErr := readInlinePayload(ctx, reader, ref, length)
		if readErr != nil {
			return readErr
		}
		inline = &bson.Binary{Subtype: 0, Data: data}
	} else {
		if err = s.blobs.remove(ctx, payload.FileID); err != nil {
			return err
		}
		metadata := s.blobMetadata(payload, ref)
		if err = s.blobs.upload(ctx, payload.FileID, metadata, reader); err != nil {
			return err
		}
	}
	filter := append(bson.D{{Key: fieldID, Value: payload.ID}, {Key: fieldState, Value: "uploading"}, {Key: fieldOwner, Value: s.ownerID}, {Key: fieldToken, Value: payload.Token}}, leaseCurrent()...)
	set := bson.D{{Key: fieldState, Value: payloadStateReady}, {Key: fieldUpdatedAt, Value: serverNow}, {Key: fieldLeaseUntil, Value: serverLease(s.config.LeaseDuration)}, {Key: fieldGuard, Value: bson.NewObjectID()}}
	if inline != nil {
		set = append(set, bson.E{Key: "inlineData", Value: bson.D{{Key: "$literal", Value: inline}}})
	}
	result, err := s.db.Collection(payloadCollection).UpdateOne(ctx, filter, mongo.Pipeline{bson.D{{Key: fieldSet, Value: set}}})
	if err != nil {
		return fmt.Errorf("publish payload metadata: %w", err)
	}
	if result.MatchedCount != 1 {
		return ErrConflict
	}
	return nil
}

func readInlinePayload(ctx context.Context, reader io.Reader, ref engine.AdmissionPayloadRef, length uint64) ([]byte, error) {
	if length > 1<<20 {
		return nil, ErrInvalidPayload
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(reader, int64(length)+1))
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	hash := sha256.Sum256(data)
	if uint64(len(data)) != length || hex.EncodeToString(hash[:]) != ref.Digest {
		return nil, ErrInvalidPayload
	}
	return data, nil
}

func (s *Store) reservePayload(ctx context.Context, ref engine.AdmissionPayloadRef, length uint64) (payloadDocument, error) {
	id := s.payloadID(ref)
	var existing payloadDocument
	err := s.db.Collection(payloadCollection).FindOne(ctx, bson.D{{Key: fieldID, Value: id}}).Decode(&existing)
	filter := bson.D{{Key: fieldID, Value: id}, {Key: fieldVersion, Value: bson.D{{Key: "$exists", Value: false}}}}
	token := "00000000000000000001"
	if err == nil {
		if existing.Chain != s.chainID || existing.Digest != ref.Digest {
			return payloadDocument{}, ErrInvalidPayload
		}
		encoded, _ := EncodeUint64(ref.ByteLength)
		if existing.Length != encoded {
			return payloadDocument{}, ErrInvalidPayload
		}
		if existing.State == payloadStateReady {
			return existing, nil
		}
		if existing.State == "uploading" && existing.Owner == s.ownerID {
			resumed, resumeErr := s.resumeUpload(ctx, existing)
			if resumeErr != nil {
				return payloadDocument{}, resumeErr
			}
			return resumed, nil
		}
		if existing.State != "deleted" {
			return payloadDocument{}, ErrConflict
		}
		token, err = nextToken(existing.Token)
		if err != nil {
			return payloadDocument{}, err
		}
		filter = bson.D{{Key: fieldID, Value: id}, {Key: fieldState, Value: "deleted"}, {Key: fieldToken, Value: existing.Token}}
	} else if !errors.Is(err, mongo.ErrNoDocuments) {
		return payloadDocument{}, err
	}
	encoded, err := EncodeUint64(ref.ByteLength)
	if err != nil {
		return payloadDocument{}, err
	}
	payload := payloadDocument{ID: id, Version: schemaVersion, Chain: s.chainID, Digest: ref.Digest, Length: encoded, State: "uploading", Owner: s.ownerID, Token: token, Guard: bson.NewObjectID()}
	if length > s.config.InlineLimit {
		payload.FileID = bson.NewObjectID()
		payload.BlobOwner = s.ownerID
		payload.BlobToken = token
	}
	pipeline := replaceWithServerDates(payload, s.config.LeaseDuration)
	err = s.db.Collection(payloadCollection).FindOneAndUpdate(ctx, filter, pipeline, options.FindOneAndUpdate().SetUpsert(existing.ID == "").SetReturnDocument(options.After)).Decode(&payload)
	if mongo.IsDuplicateKeyError(err) || errors.Is(err, mongo.ErrNoDocuments) {
		return payloadDocument{}, ErrConflict
	}
	return payload, err
}

func (s *Store) resumeUpload(ctx context.Context, existing payloadDocument) (payloadDocument, error) {
	filter := bson.D{{Key: fieldID, Value: existing.ID}, {Key: fieldState, Value: "uploading"}, {Key: fieldOwner, Value: s.ownerID}, {Key: fieldToken, Value: existing.Token}}
	update := mongo.Pipeline{bson.D{{Key: fieldSet, Value: bson.D{{Key: fieldUpdatedAt, Value: serverNow}, {Key: fieldLeaseUntil, Value: serverLease(s.config.LeaseDuration)}, {Key: fieldGuard, Value: bson.NewObjectID()}}}}}
	var resumed payloadDocument
	err := s.db.Collection(payloadCollection).FindOneAndUpdate(ctx, filter, update, options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&resumed)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return payloadDocument{}, ErrConflict
	}
	return resumed, err
}

func replaceWithServerDates(document any, lease time.Duration) mongo.Pipeline {
	return mongo.Pipeline{bson.D{{Key: "$replaceWith", Value: bson.D{{Key: "$mergeObjects", Value: bson.A{bson.D{{Key: "$literal", Value: document}}, bson.D{{Key: fieldCreatedAt, Value: serverNow}, {Key: fieldUpdatedAt, Value: serverNow}, {Key: fieldLeaseUntil, Value: serverLease(lease)}}}}}}}}
}

func (s *Store) blobMetadata(payload payloadDocument, ref engine.AdmissionPayloadRef) blobMetadata {
	return blobMetadata{ChainID: s.chainID, Digest: ref.Digest, ByteLength: string(ref.ByteLength), OwnerID: payload.BlobOwner, Token: payload.BlobToken, State: "published"}
}

// PinPayload atomically retains ready content for one explicit owner. Every pin
// and release writes the content guard in the same transaction as its reference,
// serializing with deletion even when two nodes use the same bytes.
func (s *Store) PinPayload(ctx context.Context, ref engine.AdmissionPayloadRef, owner ReferenceOwner) error {
	if _, err := s.validatePayload(ref); err != nil {
		return err
	}
	if !validOwner(owner) {
		return ErrInvalidPayload
	}
	_, err := s.runTransaction(ctx, func(sessionCtx context.Context) error { return s.pinPayload(sessionCtx, ref, owner) })
	return err
}

func (s *Store) pinPayload(ctx context.Context, ref engine.AdmissionPayloadRef, owner ReferenceOwner) error {
	if err := s.touchReadyPayload(ctx, ref); err != nil {
		return err
	}
	length, err := EncodeUint64(ref.ByteLength)
	if err != nil {
		return err
	}
	document := referenceDocument{ID: s.referenceID(ref, owner), Version: schemaVersion, Chain: s.chainID, Scope: s.scopeID, Digest: ref.Digest, Length: length, Kind: ref.Kind, OwnerKind: owner.Kind, OwnerID: owner.ID}
	_, err = s.db.Collection(referenceCollection).UpdateOne(ctx, bson.D{{Key: fieldID, Value: document.ID}}, mongo.Pipeline{bson.D{{Key: "$replaceWith", Value: bson.D{{Key: "$mergeObjects", Value: bson.A{bson.D{{Key: "$literal", Value: document}}, bson.D{{Key: fieldCreatedAt, Value: bson.D{{Key: "$ifNull", Value: bson.A{"$createdAt", "$$NOW"}}}}}}}}}}}, options.UpdateOne().SetUpsert(true))
	return err
}

func (s *Store) touchReadyPayload(ctx context.Context, ref engine.AdmissionPayloadRef) error {
	length, err := EncodeUint64(ref.ByteLength)
	if err != nil {
		return err
	}
	result, err := s.db.Collection(payloadCollection).UpdateOne(ctx, bson.D{{Key: fieldID, Value: s.payloadID(ref)}, {Key: fieldChain, Value: s.chainID}, {Key: fieldDigest, Value: ref.Digest}, {Key: fieldLength, Value: length}, {Key: fieldState, Value: payloadStateReady}}, bson.D{{Key: fieldSet, Value: bson.D{{Key: fieldGuard, Value: bson.NewObjectID()}}}})
	if err != nil {
		return err
	}
	if result.MatchedCount != 1 {
		return ErrPayloadUnavailable
	}
	return nil
}

// ReleasePayload releases only this scope's exact owner/kind reference. A
// retry is idempotent. An uncertain commit remains an error, not proof that the
// reference was removed; retrying or querying the durable owner resolves it.
func (s *Store) ReleasePayload(ctx context.Context, ref engine.AdmissionPayloadRef, owner ReferenceOwner) error {
	if _, err := s.validatePayload(ref); err != nil {
		return err
	}
	if !validOwner(owner) {
		return ErrInvalidPayload
	}
	_, err := s.runTransaction(ctx, func(sessionCtx context.Context) error {
		var document referenceDocument
		findErr := s.db.Collection(referenceCollection).FindOne(sessionCtx, bson.D{{Key: fieldID, Value: s.referenceID(ref, owner)}}).Decode(&document)
		if errors.Is(findErr, mongo.ErrNoDocuments) {
			return nil
		}
		if findErr != nil {
			return findErr
		}
		if guardErr := s.touchReadyPayload(sessionCtx, ref); guardErr != nil {
			return guardErr
		}
		_, deleteErr := s.db.Collection(referenceCollection).DeleteOne(sessionCtx, bson.D{{Key: fieldID, Value: document.ID}})
		return deleteErr
	})
	return err
}

// CopyPayload pins content during its verified streaming read. The writer may
// have received partial bytes when corruption or I/O failure is returned; only
// a nil error establishes a complete digest-checked copy. Temporary read pins
// are conservatively retained if a process dies before cleanup.
func (s *Store) CopyPayload(ctx context.Context, ref engine.AdmissionPayloadRef, writer io.Writer) (err error) {
	if writer == nil {
		return ErrInvalidPayload
	}
	owner := ReferenceOwner{Kind: ReferencePin, ID: "read:" + s.ownerID + ":" + uuid.NewString()}
	if err = s.PinPayload(ctx, ref, owner); err != nil {
		return err
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		err = errors.Join(err, s.ReleasePayload(cleanupCtx, ref, owner))
	}()
	var payload payloadDocument
	if err = s.db.Collection(payloadCollection).FindOne(ctx, bson.D{{Key: fieldID, Value: s.payloadID(ref)}, {Key: fieldState, Value: payloadStateReady}}).Decode(&payload); err != nil {
		return err
	}
	if payload.Inline != nil {
		length, parseErr := s.validatePayload(ref)
		if parseErr != nil {
			return parseErr
		}
		verified, verifyErr := readInlinePayload(ctx, bytes.NewReader(payload.Inline.Data), ref, length)
		if verifyErr != nil {
			return verifyErr
		}
		written, writeErr := writer.Write(verified)
		if writeErr == nil && written != len(verified) {
			return io.ErrShortWrite
		}
		return writeErr
	}
	return s.blobs.copy(ctx, payload.FileID, s.blobMetadata(payload, ref), writer)
}

// CollectPayload claims an expired, unreferenced upload or ready payload for
// deletion in a transaction that writes the same guard as every reference.
// Physical deletion follows the committed claim and can be retried after a
// crash. Retention is explicit: referenced history, jobs and outboxes all block
// collection, regardless of which node owns their reference.
func (s *Store) CollectPayload(ctx context.Context, ref engine.AdmissionPayloadRef) error {
	if mongo.SessionFromContext(ctx) != nil {
		return ErrNestedTransaction
	}
	if _, err := s.validatePayload(ref); err != nil {
		return err
	}
	var candidate payloadDocument
	err := s.db.Collection(payloadCollection).FindOne(ctx, bson.D{{Key: fieldID, Value: s.payloadID(ref)}}).Decode(&candidate)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil
	}
	if err != nil {
		return err
	}
	encoded, _ := EncodeUint64(ref.ByteLength)
	if candidate.Length != encoded {
		return ErrInvalidPayload
	}
	if candidate.State == "deleted" {
		return nil
	}
	if candidate.State != "deleting" {
		next, tokenErr := nextToken(candidate.Token)
		if tokenErr != nil {
			return tokenErr
		}
		old := candidate
		outcome, claimErr := s.runTransaction(ctx, func(sessionCtx context.Context) error {
			filter := bson.D{{Key: fieldID, Value: old.ID}, {Key: fieldState, Value: old.State}, {Key: fieldToken, Value: old.Token}}
			filter = append(filter, leaseExpired()...)
			update := mongo.Pipeline{bson.D{{Key: fieldSet, Value: bson.D{{Key: fieldState, Value: "deleting"}, {Key: fieldOwner, Value: bson.D{{Key: "$literal", Value: s.ownerID}}}, {Key: fieldToken, Value: next}, {Key: fieldGuard, Value: bson.NewObjectID()}, {Key: fieldUpdatedAt, Value: serverNow}, {Key: fieldLeaseUntil, Value: serverLease(s.config.LeaseDuration)}}}}}
			if updateErr := s.db.Collection(payloadCollection).FindOneAndUpdate(sessionCtx, filter, update, options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&candidate); updateErr != nil {
				return updateErr
			}
			count, countErr := s.db.Collection(referenceCollection).CountDocuments(sessionCtx, bson.D{{Key: fieldChain, Value: s.chainID}, {Key: fieldDigest, Value: ref.Digest}}, options.Count().SetLimit(1))
			if countErr != nil {
				return countErr
			}
			if count != 0 {
				return ErrConflict
			}
			return nil
		})
		if claimErr != nil {
			if outcome != transactionPending {
				return claimErr
			}
			// Only a majority-observed matching claim authorizes physical deletion.
			if readErr := s.db.Collection(payloadCollection).FindOne(ctx, bson.D{{Key: fieldID, Value: old.ID}, {Key: fieldState, Value: "deleting"}, {Key: fieldToken, Value: next}, {Key: fieldOwner, Value: s.ownerID}}).Decode(&candidate); readErr != nil {
				return claimErr
			}
		}
	}
	if !candidate.FileID.IsZero() {
		if err = s.blobs.remove(ctx, candidate.FileID); err != nil {
			return err
		}
	}
	result, err := s.db.Collection(payloadCollection).UpdateOne(ctx, bson.D{{Key: fieldID, Value: candidate.ID}, {Key: fieldState, Value: "deleting"}, {Key: fieldToken, Value: candidate.Token}}, mongo.Pipeline{bson.D{{Key: fieldSet, Value: bson.D{{Key: fieldState, Value: "deleted"}, {Key: fieldUpdatedAt, Value: serverNow}, {Key: fieldGuard, Value: bson.NewObjectID()}}}}, bson.D{{Key: "$unset", Value: bson.A{"inlineData"}}}})
	if err != nil {
		return err
	}
	if result.MatchedCount != 1 {
		return ErrConflict
	}
	return nil
}
