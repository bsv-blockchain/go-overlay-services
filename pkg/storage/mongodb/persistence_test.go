package mongodb

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/engine"
	"github.com/bsv-blockchain/go-overlay-services/pkg/storage/mongodb/internal/mongotest"
)

func foundationConfig(database, node string) Config {
	return Config{Database: database, Scope: engine.StorageScope{Network: "test:𐀀.net", GenesisHash: strings.Repeat("0", 64), NodeID: node}}
}

func payloadRef(data []byte, kind engine.AdmissionPayloadKind) engine.AdmissionPayloadRef {
	digest := sha256.Sum256(data)
	return engine.AdmissionPayloadRef{Digest: hex.EncodeToString(digest[:]), ByteLength: engine.StorageUint64(strconv.Itoa(len(data))), Kind: kind}
}

func expirePayload(ctx context.Context, t *testing.T, store *Store, ref engine.AdmissionPayloadRef) {
	t.Helper()
	_, err := store.db.Collection(payloadCollection).UpdateOne(ctx, bson.D{{Key: fieldID, Value: store.payloadID(ref)}}, bson.D{{Key: fieldSet, Value: bson.D{{Key: fieldLeaseUntil, Value: time.Unix(1, 0)}}}})
	require.NoError(t, err)
}

func TestPersistenceFoundation(t *testing.T) {
	replica := mongotest.New(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	require.Equal(t, 3, replica.MemberCount())
	store, err := New(ctx, replica.Client, foundationConfig("foundation", "node-a"))
	require.NoError(t, err)
	other, err := New(ctx, replica.Client, foundationConfig("foundation", "node-b"))
	require.NoError(t, err)
	require.Same(t, store, engine.GetAdmissionStorage(store))

	t.Run("SharedContentKindsNodesAndRetention", func(t *testing.T) {
		testSharedContentKindsNodesAndRetention(ctx, t, store, other)
	})

	t.Run("LargePayloadReadyAndDeletionRecovery", func(t *testing.T) {
		testLargePayloadReadyAndDeletionRecovery(ctx, t, replica, store)
	})

	t.Run("RejectedUploadNeverBecomesReady", func(t *testing.T) {
		testRejectedUploadNeverBecomesReady(ctx, t, store, other)
	})

	t.Run("PinAndDeletionRace", func(t *testing.T) {
		testPinAndDeletionRace(ctx, t, store, other)
	})

	t.Run("OperationReplayDigestAndAtomicBody", func(t *testing.T) {
		testOperationReplayDigestAndAtomicBody(ctx, t, replica, store)
	})

	t.Run("PendingReconciliationAndAbsenceFence", func(t *testing.T) {
		testPendingReconciliationAndAbsenceFence(ctx, t, store, other)
	})

	t.Run("AbortFenceMismatchReturnsPending", func(t *testing.T) {
		testAbortFenceMismatchReturnsPending(ctx, t, store)
	})

	t.Run("ConnectFactoryAndUnknownCommitCAS", func(t *testing.T) {
		testConnectFactoryAndUnknownCommitCAS(ctx, t, replica)
	})

	t.Run("TransientBodyRetry", func(t *testing.T) {
		testTransientBodyRetry(ctx, t, replica, store)
	})

	t.Run("FailedGridFSPublishCanReplaceReservation", func(t *testing.T) {
		testFailedGridFSPublishCanReplaceReservation(ctx, t, replica)
	})
}

func testReceipt(key engine.AdmissionOperationKey) engine.AdmissionReceipt {
	return engine.AdmissionReceipt{OperationID: key.OperationID, SemanticDigest: key.SemanticDigest, Durability: engine.AdmissionDurabilityAtomicLocal, Steak: " {\n\"tx\": \"𐀀\", \"admitted\": []}\n", Indexes: []engine.AdmissionIndexStatus{{Target: "local", State: engine.AdmissionIndexVisible}, {Target: "external", State: engine.AdmissionIndexPending}}, Propagation: engine.AdmissionPropagationPending}
}

func testSharedContentKindsNodesAndRetention(ctx context.Context, t *testing.T, store, other *Store) {
	t.Helper()
	data := []byte("content retained by history and outbox, topics.$are.values")
	ref := payloadRef(data, engine.AdmissionPayloadRawTransaction)
	require.NoError(t, store.PublishPayload(ctx, ref, bytes.NewReader(data)))
	require.NoError(t, other.PublishPayload(ctx, ref, bytes.NewReader(data)))
	scriptRef := ref
	scriptRef.Kind = engine.AdmissionPayloadLockingScript
	owner := ReferenceOwner{Kind: ReferenceAppliedHistory, ID: "topic.a:$history"}
	outbox := ReferenceOwner{Kind: ReferencePropagationOutbox, ID: "event:other-node"}
	require.NoError(t, store.PinPayload(ctx, ref, owner))
	require.NoError(t, other.PinPayload(ctx, scriptRef, outbox))
	require.NoError(t, other.PinPayload(ctx, scriptRef, outbox))
	count, countErr := store.db.Collection(payloadCollection).CountDocuments(ctx, bson.D{{Key: fieldDigest, Value: ref.Digest}})
	require.NoError(t, countErr)
	require.EqualValues(t, 1, count)
	count, countErr = store.db.Collection(referenceCollection).CountDocuments(ctx, bson.D{{Key: fieldDigest, Value: ref.Digest}})
	require.NoError(t, countErr)
	require.EqualValues(t, 2, count)
	expirePayload(ctx, t, store, ref)
	require.ErrorIs(t, store.CollectPayload(ctx, ref), ErrConflict)
	require.NoError(t, store.ReleasePayload(ctx, ref, owner))
	require.ErrorIs(t, store.CollectPayload(ctx, ref), ErrConflict)
	var copied bytes.Buffer
	require.NoError(t, other.CopyPayload(ctx, ref, &copied))
	require.Equal(t, data, copied.Bytes())
	require.NoError(t, other.ReleasePayload(ctx, scriptRef, outbox))
	require.NoError(t, store.CollectPayload(ctx, ref))
	require.ErrorIs(t, store.PinPayload(ctx, ref, owner), ErrPayloadUnavailable)
	require.NoError(t, store.CollectPayload(ctx, ref))
	require.NoError(t, other.PublishPayload(ctx, ref, bytes.NewReader(data)))
	require.NoError(t, other.CopyPayload(ctx, ref, io.Discard))
}

func testLargePayloadReadyAndDeletionRecovery(ctx context.Context, t *testing.T, replica *mongotest.ReplicaSet, store *Store) {
	t.Helper()
	const length = 17 << 20
	ref := engine.AdmissionPayloadRef{Digest: repeatedSHA256(length, 'x'), ByteLength: engine.StorageUint64(strconv.Itoa(length)), Kind: engine.AdmissionPayloadRawTransaction}
	require.NoError(t, store.PublishPayload(ctx, ref, &repeatedByteReader{remaining: length, value: 'x'}))
	require.NoError(t, store.CopyPayload(ctx, ref, io.Discard))
	var payload payloadDocument
	require.NoError(t, store.db.Collection(payloadCollection).FindOne(ctx, bson.D{{Key: fieldID, Value: store.payloadID(ref)}}).Decode(&payload))
	require.Nil(t, payload.Inline)
	require.False(t, payload.FileID.IsZero())
	// Reproduce the durable crash point after a committed deleting claim.
	_, updateErr := store.db.Collection(payloadCollection).UpdateOne(ctx, bson.D{{Key: fieldID, Value: payload.ID}}, bson.D{{Key: fieldSet, Value: bson.D{{Key: fieldState, Value: "deleting"}}}})
	require.NoError(t, updateErr)
	restarted, restartErr := New(ctx, replica.Client, foundationConfig("foundation", "node-a"))
	require.NoError(t, restartErr)
	require.NoError(t, restarted.CollectPayload(ctx, ref))
	count, countErr := store.db.Collection(blobBucketName+".chunks").CountDocuments(ctx, bson.D{{Key: fieldFilesID, Value: payload.FileID}})
	require.NoError(t, countErr)
	require.Zero(t, count)
}

func testRejectedUploadNeverBecomesReady(ctx context.Context, t *testing.T, store, other *Store) {
	t.Helper()
	data := []byte("must match")
	ref := payloadRef(data, engine.AdmissionPayloadMerklePath)
	require.ErrorIs(t, store.PublishPayload(ctx, ref, strings.NewReader("wrong data")), ErrInvalidPayload)
	require.ErrorIs(t, store.PinPayload(ctx, ref, ReferenceOwner{Kind: ReferenceBASMJob, ID: "repair"}), ErrPayloadUnavailable)
	require.ErrorIs(t, other.PublishPayload(ctx, ref, bytes.NewReader(data)), ErrConflict)
	require.NoError(t, store.PublishPayload(ctx, ref, bytes.NewReader(data)))
	require.NoError(t, store.CopyPayload(ctx, ref, io.Discard))
	wrong := ref
	wrong.ByteLength = "1"
	require.ErrorIs(t, store.PinPayload(ctx, wrong, ReferenceOwner{Kind: ReferencePin, ID: "wrong-size"}), ErrPayloadUnavailable)
}

func testPinAndDeletionRace(ctx context.Context, t *testing.T, store, other *Store) {
	t.Helper()
	for i := range 8 {
		data := []byte("race-" + strconv.Itoa(i))
		ref := payloadRef(data, engine.AdmissionPayloadOutboxData)
		require.NoError(t, store.PublishPayload(ctx, ref, bytes.NewReader(data)))
		expirePayload(ctx, t, store, ref)
		owner := ReferenceOwner{Kind: ReferenceLookupOutbox, ID: "race-owner"}
		start := make(chan struct{})
		var group sync.WaitGroup
		group.Add(2)
		var pinErr, collectErr error
		go func() { defer group.Done(); <-start; pinErr = other.PinPayload(ctx, ref, owner) }()
		go func() { defer group.Done(); <-start; collectErr = store.CollectPayload(ctx, ref) }()
		close(start)
		group.Wait()
		if pinErr == nil {
			require.Error(t, collectErr)
			require.NoError(t, other.CopyPayload(ctx, ref, io.Discard))
			require.NoError(t, other.ReleasePayload(ctx, ref, owner))
			require.NoError(t, store.CollectPayload(ctx, ref))
		} else {
			require.NoError(t, collectErr)
			require.ErrorIs(t, other.CopyPayload(ctx, ref, io.Discard), ErrPayloadUnavailable)
		}
	}
}

func testOperationReplayDigestAndAtomicBody(ctx context.Context, t *testing.T, replica *mongotest.ReplicaSet, store *Store) {
	t.Helper()
	key := engine.AdmissionOperationKey{Scope: store.Scope(), OperationID: "receipt.$:𐀀", SemanticDigest: strings.Repeat("1", 64)}
	effects := store.db.Collection("test_effects")
	require.NoError(t, store.db.CreateCollection(ctx, "test_effects"))
	var calls atomic.Int32
	body := func(sessionCtx context.Context) (engine.AdmissionReceipt, error) {
		calls.Add(1)
		_, insertErr := effects.InsertOne(sessionCtx, bson.D{{Key: fieldID, Value: key.OperationID}})
		return testReceipt(key), insertErr
	}
	result, executeErr := store.ExecuteOperation(ctx, key, body)
	require.NoError(t, executeErr)
	require.Equal(t, engine.AdmissionCommitStateCommitted, result.State)
	restarted, restartErr := New(ctx, replica.Client, foundationConfig("foundation", "node-a"))
	require.NoError(t, restartErr)
	replay, replayErr := restarted.ExecuteOperation(ctx, key, body)
	require.NoError(t, replayErr)
	require.Equal(t, result, replay)
	require.EqualValues(t, 1, calls.Load())
	mismatch := key
	mismatch.SemanticDigest = strings.Repeat("2", 64)
	rejected, rejectErr := store.ExecuteOperation(ctx, mismatch, body)
	require.NoError(t, rejectErr)
	require.Equal(t, engine.AdmissionRejectionDigestMismatch, rejected.RejectionCode)
	abortedKey := key
	abortedKey.OperationID = "aborted"
	aborted, abortErr := store.ExecuteOperation(ctx, abortedKey, func(sessionCtx context.Context) (engine.AdmissionReceipt, error) {
		_, writeErr := effects.InsertOne(sessionCtx, bson.D{{Key: fieldID, Value: "rolled-back"}})
		if writeErr != nil {
			return engine.AdmissionReceipt{}, writeErr
		}
		return engine.AdmissionReceipt{}, ErrConflict
	})
	require.ErrorIs(t, abortErr, ErrConflict)
	require.Empty(t, aborted.State)
	require.Empty(t, aborted.AttemptID)
	require.ErrorIs(t, effects.FindOne(ctx, bson.D{{Key: fieldID, Value: "rolled-back"}}).Err(), mongo.ErrNoDocuments)
	retried, retryErr := store.ExecuteOperation(ctx, abortedKey, func(context.Context) (engine.AdmissionReceipt, error) { return testReceipt(abortedKey), nil })
	require.NoError(t, retryErr)
	require.Equal(t, engine.AdmissionCommitStateCommitted, retried.State)
}

func testPendingReconciliationAndAbsenceFence(ctx context.Context, t *testing.T, store, other *Store) {
	t.Helper()
	key := engine.AdmissionOperationKey{Scope: store.Scope(), OperationID: "expired-claim", SemanticDigest: strings.Repeat("3", 64)}
	operation, claimed, claimErr := store.claimOperation(ctx, key)
	require.NoError(t, claimErr)
	require.True(t, claimed)
	calls := 0
	pending, pendingErr := other.ExecuteOperation(ctx, engine.AdmissionOperationKey{Scope: other.Scope(), OperationID: key.OperationID, SemanticDigest: key.SemanticDigest}, func(context.Context) (engine.AdmissionReceipt, error) {
		calls++
		return engine.AdmissionReceipt{}, ErrConflict
	})
	require.ErrorIs(t, pendingErr, ErrConflict)
	require.Empty(t, pending.State)
	require.Equal(t, 1, calls, "other node has independent operation authority")
	replay, replayErr := store.ExecuteOperation(ctx, key, func(context.Context) (engine.AdmissionReceipt, error) { calls++; return testReceipt(key), nil })
	require.NoError(t, replayErr)
	require.Equal(t, operation.Attempt, replay.AttemptID)
	require.Equal(t, 1, calls)
	before, beforeErr := store.ReconcileOperation(ctx, key, &operation.Attempt)
	require.NoError(t, beforeErr)
	require.Equal(t, engine.AdmissionCommitStatePending, before.State)
	_, expireErr := store.db.Collection(operationCollection).UpdateOne(ctx, bson.D{{Key: fieldID, Value: operation.ID}}, bson.D{{Key: fieldSet, Value: bson.D{{Key: fieldLeaseUntil, Value: time.Unix(1, 0)}}}})
	require.NoError(t, expireErr)
	after, afterErr := store.ReconcileOperation(ctx, key, &operation.Attempt)
	require.NoError(t, afterErr)
	require.Equal(t, engine.AdmissionCommitStateAborted, after.State)
	_, staleErr := store.runTransaction(ctx, func(sessionCtx context.Context) error {
		updated, updateErr := store.db.Collection(operationCollection).UpdateOne(sessionCtx, operationPredicate(operation), bson.D{{Key: fieldSet, Value: bson.D{{Key: fieldGuard, Value: bson.NewObjectID()}}}})
		if updateErr != nil {
			return updateErr
		}
		if updated.MatchedCount != 1 {
			return ErrConflict
		}
		return nil
	})
	require.ErrorIs(t, staleErr, ErrConflict)
	absent := key
	absent.OperationID = "lost-initial-claim"
	attempt := "opaque-lost-attempt"
	fenced, fenceErr := store.ReconcileOperation(ctx, absent, &attempt)
	require.NoError(t, fenceErr)
	require.Equal(t, engine.AdmissionCommitStateAborted, fenced.State)
}

func testAbortFenceMismatchReturnsPending(ctx context.Context, t *testing.T, store *Store) {
	t.Helper()
	key := engine.AdmissionOperationKey{Scope: store.Scope(), OperationID: "fence-miss", SemanticDigest: strings.Repeat("6", 64)}
	operation, claimed, claimErr := store.claimOperation(ctx, key)
	require.NoError(t, claimErr)
	require.True(t, claimed)
	next, tokenErr := nextToken(operation.Token)
	require.NoError(t, tokenErr)
	_, tokenUpdateErr := store.db.Collection(operationCollection).UpdateOne(ctx, bson.D{{Key: fieldID, Value: operation.ID}}, bson.D{{Key: fieldSet, Value: bson.D{{Key: fieldToken, Value: next}}}})
	require.NoError(t, tokenUpdateErr)
	result, fenceErr := store.fenceAbortedOperation(ctx, key, operation, ErrConflict)
	require.ErrorIs(t, fenceErr, ErrConflict)
	require.Equal(t, engine.AdmissionCommitStatePending, result.State)
	require.Equal(t, operation.Attempt, result.AttemptID)
	var stored operationDocument
	require.NoError(t, store.db.Collection(operationCollection).FindOne(ctx, bson.D{{Key: fieldID, Value: operation.ID}}).Decode(&stored))
	require.Equal(t, "pending", stored.State)
	require.Equal(t, next, stored.Token)
}

func testConnectFactoryAndUnknownCommitCAS(ctx context.Context, t *testing.T, replica *mongotest.ReplicaSet) {
	t.Helper()
	connected, connectErr := Connect(ctx, replica.URI, foundationConfig("foundation_connect", "node-connect"))
	require.NoError(t, connectErr)
	t.Cleanup(func() { _ = connected.Close(context.WithoutCancel(ctx)) })
	require.Same(t, connected, engine.GetAdmissionStorage(connected))

	key := engine.AdmissionOperationKey{Scope: connected.Scope(), OperationID: "unknown-commit", SemanticDigest: strings.Repeat("4", 64)}
	effects := connected.db.Collection("unknown_commit_effects")
	var calls atomic.Int32
	body := func(sessionCtx context.Context) (engine.AdmissionReceipt, error) {
		calls.Add(1)
		_, insertErr := effects.InsertOne(sessionCtx, bson.D{{Key: fieldID, Value: key.OperationID}})
		return testReceipt(key), insertErr
	}
	require.NoError(t, replica.FailCommand(ctx, 20, []string{"commitTransaction"}, 91, []string{"UnknownTransactionCommitResult", "TransientTransactionError"}))
	t.Cleanup(func() { _ = replica.DisableFailPoint(context.WithoutCancel(ctx)) })
	result, executeErr := connected.ExecuteOperation(ctx, key, body)
	require.NoError(t, replica.DisableFailPoint(ctx))
	require.EqualValues(t, 1, calls.Load(), "unknown commit must not rerun the body")
	if result.State == engine.AdmissionCommitStateCommitted {
		require.NoError(t, executeErr)
		replay, replayErr := connected.ExecuteOperation(ctx, key, body)
		require.NoError(t, replayErr)
		require.Equal(t, result, replay)
		require.EqualValues(t, 1, calls.Load())
		return
	}
	require.ErrorIs(t, executeErr, ErrCommitPending)
	require.Equal(t, engine.AdmissionCommitStatePending, result.State)
	attempt := result.AttemptID
	before, beforeErr := connected.ReconcileOperation(ctx, key, &attempt)
	require.NoError(t, beforeErr)
	if before.State == engine.AdmissionCommitStateCommitted {
		require.NotNil(t, before.Receipt)
		return
	}
	require.Equal(t, engine.AdmissionCommitStatePending, before.State)
	_, expireErr := connected.db.Collection(operationCollection).UpdateOne(ctx, bson.D{{Key: fieldID, Value: connected.operationID(key)}}, bson.D{{Key: fieldSet, Value: bson.D{{Key: fieldLeaseUntil, Value: time.Unix(1, 0)}}}})
	require.NoError(t, expireErr)
	after, afterErr := connected.ReconcileOperation(ctx, key, &attempt)
	require.NoError(t, afterErr)
	require.Contains(t, []engine.AdmissionCommitState{engine.AdmissionCommitStateAborted, engine.AdmissionCommitStateCommitted}, after.State)
	if after.State == engine.AdmissionCommitStateAborted {
		require.ErrorIs(t, effects.FindOne(ctx, bson.D{{Key: fieldID, Value: key.OperationID}}).Err(), mongo.ErrNoDocuments)
	}
}

func testTransientBodyRetry(ctx context.Context, t *testing.T, replica *mongotest.ReplicaSet, store *Store) {
	t.Helper()
	key := engine.AdmissionOperationKey{Scope: store.Scope(), OperationID: "transient-body", SemanticDigest: strings.Repeat("5", 64)}
	effects := store.db.Collection("transient_effects")
	var calls atomic.Int32
	result, executeErr := store.ExecuteOperation(ctx, key, func(sessionCtx context.Context) (engine.AdmissionReceipt, error) {
		n := calls.Add(1)
		if n == 1 {
			require.NoError(t, replica.FailCommand(ctx, 1, []string{"insert"}, 112, []string{"TransientTransactionError"}))
		}
		_, insertErr := effects.InsertOne(sessionCtx, bson.D{{Key: fieldID, Value: key.OperationID}, {Key: "call", Value: n}})
		return testReceipt(key), insertErr
	})
	require.NoError(t, replica.DisableFailPoint(ctx))
	require.NoError(t, executeErr)
	require.Equal(t, engine.AdmissionCommitStateCommitted, result.State)
	require.GreaterOrEqual(t, calls.Load(), int32(1))
	count, countErr := effects.CountDocuments(ctx, bson.D{{Key: fieldID, Value: key.OperationID}})
	require.NoError(t, countErr)
	require.EqualValues(t, 1, count)
}

func testFailedGridFSPublishCanReplaceReservation(ctx context.Context, t *testing.T, replica *mongotest.ReplicaSet) {
	t.Helper()
	config := foundationConfig("foundation_resume", "node-resume")
	config.InlineLimit = 8
	resuming, resumeErr := New(ctx, replica.Client, config)
	require.NoError(t, resumeErr)
	data := []byte("gridfs-resume-bytes")
	ref := payloadRef(data, engine.AdmissionPayloadRawTransaction)
	require.ErrorIs(t, resuming.PublishPayload(ctx, ref, bytes.NewReader(bytes.Repeat([]byte{'y'}, len(data)))), ErrBlobCorrupt)
	require.ErrorIs(t, resuming.PinPayload(ctx, ref, ReferenceOwner{Kind: ReferencePin, ID: "before-retry"}), ErrPayloadUnavailable)
	require.NoError(t, resuming.PublishPayload(ctx, ref, bytes.NewReader(data)))
	require.NoError(t, resuming.CopyPayload(ctx, ref, io.Discard))
}
