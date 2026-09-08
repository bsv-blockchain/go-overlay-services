package mongodb

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bsv-blockchain/go-sdk/chainhash"
	"github.com/bsv-blockchain/go-sdk/overlay"
	"github.com/bsv-blockchain/go-sdk/transaction"
	"github.com/bsv-blockchain/universal-test-vectors/pkg/testabilities"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/engine"
	"github.com/bsv-blockchain/go-overlay-services/pkg/storage/mongodb/internal/mongotest"
)

const admissionTestTopic = "tm_contract"

func TestAdmissionStorage(t *testing.T) {
	replica := mongotest.New(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	t.Run("AdvertisesV1", func(t *testing.T) {
		store := openAdmissionStore(ctx, t, replica, "reference-node")
		require.Equal(t, engine.AdmissionStorageProtocol, engine.GetAdmissionStorage(store).AdmissionProtocol())
	})

	t.Run("CommitsAtomicAdmissionAndLeavesExternalWorkPending", func(t *testing.T) {
		store := openAdmissionStore(ctx, t, replica, "reference-node")
		plan := admissionPlan(store.Scope(), "operation-1")
		seedAdmissionPlan(ctx, t, store, plan)
		result, commitErr := store.CommitAdmission(ctx, plan)
		require.NoError(t, commitErr)
		receipt := requireCommitted(t, result)
		require.Equal(t, plan.Steak, receipt.Steak)
		require.Equal(t, engine.AdmissionDurabilityAtomicLocal, receipt.Durability)
		require.Equal(t, []engine.AdmissionIndexStatus{{Target: "ls_contract", State: engine.AdmissionIndexPending}}, receipt.Indexes)
		require.Equal(t, engine.AdmissionPropagationPending, receipt.Propagation)
		require.EqualValues(t, 1, countDocs(ctx, t, store, edgeCollection))
		require.EqualValues(t, 1, countDocs(ctx, t, store, appliedCollection))
		require.EqualValues(t, 2, countDocs(ctx, t, store, outboxCollection))
		var spent outputDocument
		require.NoError(t, store.db.Collection(outputCollection).FindOne(ctx, bson.D{{Key: fieldSpentBy, Value: plan.Identity.TxID}}).Decode(&spent))
	})

	t.Run("ReplaySavedSteakBeforeStalePredicates", func(t *testing.T) {
		store := openAdmissionStore(ctx, t, replica, "reference-node")
		plan := admissionPlan(store.Scope(), "replay-1")
		seedAdmissionPlan(ctx, t, store, plan)
		first, firstErr := store.CommitAdmission(ctx, plan)
		require.NoError(t, firstErr)
		saved := requireCommitted(t, first)
		stale := plan
		stale.Steak = "not regenerated on retry"
		restarted, restartErr := New(ctx, replica.Client, admissionTestConfig(store.config.Database, "reference-node"))
		require.NoError(t, restartErr)
		replay, replayErr := restarted.CommitAdmission(ctx, stale)
		require.NoError(t, replayErr)
		require.Equal(t, saved, requireCommitted(t, replay))
	})

	t.Run("RejectsDigestMismatchWithoutEffects", func(t *testing.T) {
		store := openAdmissionStore(ctx, t, replica, "reference-node")
		plan := admissionPlan(store.Scope(), "mismatch-1")
		seedAdmissionPlan(ctx, t, store, plan)
		requireCommitted(t, mustCommit(ctx, t, store, plan))
		conflicting := admissionPlan(store.Scope(), "mismatch-1")
		conflicting.Identity.TxID = strings.Repeat("7", 64)
		conflicting.Key.SemanticDigest = mustDigest(t, conflicting.Identity)
		rejected, rejectErr := store.CommitAdmission(ctx, conflicting)
		require.NoError(t, rejectErr)
		require.Equal(t, engine.AdmissionCommitStateRejected, rejected.State)
		require.Equal(t, engine.AdmissionRejectionDigestMismatch, rejected.RejectionCode)
		wrong := admissionPlan(store.Scope(), "mismatch-supplied")
		seedAdmissionPlan(ctx, t, store, wrong)
		wrong.Key.SemanticDigest = strings.Repeat("0", 64)
		before := countDocs(ctx, t, store, appliedCollection)
		rejected, rejectErr = store.CommitAdmission(ctx, wrong)
		require.NoError(t, rejectErr)
		require.Equal(t, engine.AdmissionRejectionDigestMismatch, rejected.RejectionCode)
		require.Equal(t, before, countDocs(ctx, t, store, appliedCollection))
	})

	t.Run("RejectsMissingPayloadAndInvalidPlan", func(t *testing.T) {
		store := openAdmissionStore(ctx, t, replica, "reference-node")
		plan := admissionPlan(store.Scope(), "payload-missing")
		seedAdmissionPlan(ctx, t, store, plan, plan.Decisions[0].Applied.Proof.Digest)
		rejected, err := store.CommitAdmission(ctx, plan)
		require.NoError(t, err)
		require.Equal(t, engine.AdmissionRejectionPayloadNotReady, rejected.RejectionCode)

		wrongOutput := admissionPlan(store.Scope(), "wrong-output")
		seedAdmissionPlan(ctx, t, store, wrongOutput)
		wrongOutput.Decisions[0].Outputs[0].TxID = strings.Repeat("7", 64)
		rejected, err = store.CommitAdmission(ctx, wrongOutput)
		require.NoError(t, err)
		require.Equal(t, engine.AdmissionRejectionInvalidPlan, rejected.RejectionCode)

		historical := admissionPlan(store.Scope(), "historical-operation")
		historical.Identity.Mode = engine.AdmissionModeHistorical
		historical.Key.SemanticDigest = mustDigest(t, historical.Identity)
		seedAdmissionPlan(ctx, t, store, historical)
		rejected, err = store.CommitAdmission(ctx, historical)
		require.NoError(t, err)
		require.Equal(t, engine.AdmissionRejectionInvalidPlan, rejected.RejectionCode)

		overflow := admissionPlan(store.Scope(), "uint32-overflow")
		seedAdmissionPlan(ctx, t, store, overflow)
		overflow.Decisions[0].Outputs[0].OutputIndex = "4294967296"
		overflow.Decisions[0].Edges[0].Consumer.OutputIndex = "4294967296"
		rejected, err = store.CommitAdmission(ctx, overflow)
		require.NoError(t, err)
		require.Equal(t, engine.AdmissionRejectionInvalidPlan, rejected.RejectionCode)
	})

	t.Run("RejectsStaleReadAndSpend", func(t *testing.T) {
		store := openAdmissionStore(ctx, t, replica, "reference-node")
		staleRead := admissionPlan(store.Scope(), "stale-read")
		seedAdmissionPlan(ctx, t, store, staleRead)
		require.NoError(t, store.seedRead(ctx, admissionTestTopic, "selection:tm_contract", "r2"))
		rejected, err := store.CommitAdmission(ctx, staleRead)
		require.NoError(t, err)
		require.Equal(t, engine.AdmissionRejectionReadConflict, rejected.RejectionCode)

		staleSpend := admissionPlan(store.Scope(), "stale-spend")
		seedAdmissionPlan(ctx, t, store, staleSpend)
		require.NoError(t, store.seedSpendable(ctx, admissionTestTopic, engine.AdmissionOutpoint{TxID: strings.Repeat("9", 64), OutputIndex: "0"}, "spend-r2"))
		rejected, err = store.CommitAdmission(ctx, staleSpend)
		require.NoError(t, err)
		require.Equal(t, engine.AdmissionRejectionSpendConflict, rejected.RejectionCode)
	})

	t.Run("CompetingSpendPublishesOnlyWinner", func(t *testing.T) {
		store := openAdmissionStore(ctx, t, replica, "reference-node")
		first := admissionPlan(store.Scope(), "race-first")
		first.Identity.TxID = strings.Repeat("6", 64)
		first.Key.SemanticDigest = mustDigest(t, first.Identity)
		first.Decisions[0].Spends[0].Spender = first.Identity.TxID
		first.Decisions[0].Outputs[0].TxID = first.Identity.TxID
		first.Decisions[0].Applied.TxID = first.Identity.TxID
		first.Decisions[0].Edges[0].Consumer.TxID = first.Identity.TxID
		first.Steak = testSteakJSON()
		second := admissionPlan(store.Scope(), "race-second")
		second.Identity.TxID = strings.Repeat("7", 64)
		second.Key.SemanticDigest = mustDigest(t, second.Identity)
		second.Decisions[0].Spends[0].Spender = second.Identity.TxID
		second.Decisions[0].Outputs[0].TxID = second.Identity.TxID
		second.Decisions[0].Applied.TxID = second.Identity.TxID
		second.Decisions[0].Edges[0].Consumer.TxID = second.Identity.TxID
		second.Steak = testSteakJSON()
		seedAdmissionPlan(ctx, t, store, first)
		seedAdmissionPlan(ctx, t, store, second)
		var group sync.WaitGroup
		results := make([]engine.AdmissionCommitResult, 2)
		errs := make([]error, 2)
		group.Add(2)
		go func() { defer group.Done(); results[0], errs[0] = store.CommitAdmission(ctx, first) }()
		go func() { defer group.Done(); results[1], errs[1] = store.CommitAdmission(ctx, second) }()
		group.Wait()
		require.NoError(t, errs[0])
		require.NoError(t, errs[1])
		committed := 0
		conflicts := 0
		for _, result := range results {
			if result.State == engine.AdmissionCommitStateCommitted {
				committed++
			}
			if result.State == engine.AdmissionCommitStateRejected && result.RejectionCode == engine.AdmissionRejectionSpendConflict {
				conflicts++
			}
		}
		require.Equal(t, 1, committed)
		require.Equal(t, 1, conflicts)
	})

	t.Run("IsolatesScopeAndRejectsOutboxReuse", func(t *testing.T) {
		store := openAdmissionStore(ctx, t, replica, "reference-node")
		other, otherErr := New(ctx, replica.Client, admissionTestConfig(store.config.Database, "reference-node-b"))
		require.NoError(t, otherErr)
		first := admissionPlan(store.Scope(), "shared-operation")
		seedAdmissionPlan(ctx, t, store, first)
		requireCommitted(t, mustCommit(ctx, t, store, first))
		second := admissionPlan(other.Scope(), "shared-operation")
		seedAdmissionPlan(ctx, t, other, second)
		requireCommitted(t, mustCommit(ctx, t, other, second))
		require.EqualValues(t, 1, countScoped(ctx, t, store, outboxCollection, store.scopeID, "shared-operation:lookup"))
		require.EqualValues(t, 1, countScoped(ctx, t, other, outboxCollection, other.scopeID, "shared-operation:lookup"))

		conflict := admissionPlan(store.Scope(), "outbox-second")
		conflict.Identity.TxID = strings.Repeat("6", 64)
		conflict.Key.SemanticDigest = mustDigest(t, conflict.Identity)
		conflict.Decisions[0].Spends = nil
		conflict.Decisions[0].Outputs[0].TxID = conflict.Identity.TxID
		conflict.Decisions[0].Applied.TxID = conflict.Identity.TxID
		conflict.Decisions[0].Edges = nil
		conflict.Outbox = first.Outbox
		conflict.Steak = testSteakJSON()
		seedAdmissionPlan(ctx, t, store, conflict)
		rejected, err := store.CommitAdmission(ctx, conflict)
		require.NoError(t, err)
		require.Equal(t, engine.AdmissionRejectionInvalidPlan, rejected.RejectionCode)
	})

	t.Run("HistoryHandoffMovesFenceAndRejectsStaleWorker", func(t *testing.T) {
		store := openAdmissionStore(ctx, t, replica, "reference-node")
		plan := admissionPlan(store.Scope(), "history-1")
		seedAdmissionPlan(ctx, t, store, plan)
		lease := engine.RecoveryLease{
			HistoryFence: engine.HistoryFence{ChainEpoch: "7", TopicHistoryGeneration: "3"},
			Scope:        store.Scope(),
			Topic:        admissionTestTopic,
			PeerID:       "peer-a",
			JobID:        "repair-a",
			LeaseToken:   "9",
			ExpiresAtMS:  "100",
		}
		require.NoError(t, store.seedLease(ctx, lease))
		require.NoError(t, store.seedAdmissionClock(ctx, "50"))
		plan.Decisions[0].HistoryUpdate = &engine.AdmissionHistoryUpdate{
			NextTopicHistoryGeneration: "4",
			AffectedFromHeight:         "99",
			Handoff:                    &engine.HistoryRevisionHandoff{Expected: lease, Checkpoint: "repair-checkpoint"},
		}
		requireCommitted(t, mustCommit(ctx, t, store, plan))
		fence, fenceErr := store.CurrentHistoryFence(ctx, admissionTestTopic)
		require.NoError(t, fenceErr)
		require.Equal(t, engine.StorageUint64("4"), fence.TopicHistoryGeneration)

		stale := admissionPlan(store.Scope(), "history-2")
		stale.Identity.TxID = strings.Repeat("6", 64)
		stale.Key.SemanticDigest = mustDigest(t, stale.Identity)
		stale.Decisions[0].ExpectedHistory = engine.HistoryFence{ChainEpoch: "7", TopicHistoryGeneration: "4"}
		stale.Decisions[0].Spends = nil
		stale.Decisions[0].Outputs[0].TxID = stale.Identity.TxID
		stale.Decisions[0].Applied.TxID = stale.Identity.TxID
		stale.Decisions[0].Edges = nil
		stale.Decisions[0].HistoryUpdate = &engine.AdmissionHistoryUpdate{
			NextTopicHistoryGeneration: "5",
			AffectedFromHeight:         "99",
			Handoff:                    &engine.HistoryRevisionHandoff{Expected: lease, Checkpoint: "stale-checkpoint"},
		}
		stale.Steak = testSteakJSON()
		seedAdmissionPlan(ctx, t, store, stale)
		require.NoError(t, store.EnsureHistoryFence(ctx, admissionTestTopic, stale.Decisions[0].ExpectedHistory))
		rejected, err := store.CommitAdmission(ctx, stale)
		require.NoError(t, err)
		require.Equal(t, engine.AdmissionRejectionReadConflict, rejected.RejectionCode)
	})

	t.Run("UnknownCommitDoesNotRerunBody", func(t *testing.T) {
		store := openAdmissionStore(ctx, t, replica, "reference-node")
		plan := admissionPlan(store.Scope(), "unknown-commit")
		seedAdmissionPlan(ctx, t, store, plan)
		require.NoError(t, replica.FailCommand(ctx, 20, []string{"commitTransaction"}, 91, []string{"UnknownTransactionCommitResult", "TransientTransactionError"}))
		t.Cleanup(func() { _ = replica.DisableFailPoint(context.WithoutCancel(ctx)) })
		result, commitErr := store.CommitAdmission(ctx, plan)
		require.NoError(t, replica.DisableFailPoint(ctx))
		if result.State == engine.AdmissionCommitStateCommitted {
			require.NoError(t, commitErr)
			replay, replayErr := store.CommitAdmission(ctx, plan)
			require.NoError(t, replayErr)
			require.Equal(t, requireCommitted(t, result), requireCommitted(t, replay))
			return
		}
		require.Equal(t, engine.AdmissionCommitStatePending, result.State)
		require.NotEmpty(t, result.AttemptID)
		reconciled, recErr := store.ReconcileAdmission(ctx, plan.Key, &result.AttemptID)
		require.NoError(t, recErr)
		if reconciled.State == engine.AdmissionCommitStateCommitted {
			require.Equal(t, plan.Steak, reconciled.Receipt.Steak)
			return
		}
		require.Equal(t, engine.AdmissionCommitStatePending, reconciled.State)
		_, expireErr := store.db.Collection(operationCollection).UpdateOne(ctx, bson.D{{Key: fieldID, Value: store.operationID(plan.Key)}}, bson.D{{Key: fieldSet, Value: bson.D{{Key: fieldLeaseUntil, Value: time.Unix(1, 0)}}}})
		require.NoError(t, expireErr)
		after, afterErr := store.ReconcileAdmission(ctx, plan.Key, &result.AttemptID)
		require.NoError(t, afterErr)
		require.Contains(t, []engine.AdmissionCommitState{engine.AdmissionCommitStateAborted, engine.AdmissionCommitStateCommitted}, after.State)
	})

	t.Run("ReplicaStepDownStillCommits", func(t *testing.T) {
		store := openAdmissionStore(ctx, t, replica, "reference-node")
		plan := admissionPlan(store.Scope(), "stepdown-1")
		seedAdmissionPlan(ctx, t, store, plan)
		require.NoError(t, replica.StepDownPrimary(ctx))
		result, commitErr := store.CommitAdmission(ctx, plan)
		require.NoError(t, commitErr)
		require.Equal(t, engine.AdmissionCommitStateCommitted, result.State)
		require.Equal(t, plan.Steak, result.Receipt.Steak)
	})

	t.Run("EngineSubmitDurableSteakAndHistoricalHasNoPropagation", func(t *testing.T) {
		engineStore := openAdmissionStore(ctx, t, replica, "engine-node")
		eng := engine.NewEngine(&engine.Config{
			Managers: map[string]engine.TopicManager{
				admissionTestTopic: admitAllManager{},
			},
			Storage:      engineStore,
			ChainTracker: admitAllTracker{},
		})
		beef := dummyAtomicBeef(t)
		tagged := overlay.TaggedBEEF{Topics: []string{admissionTestTopic}, Beef: beef}
		steak, submitErr := eng.Submit(ctx, tagged, engine.SubmitModeCurrent, nil)
		require.NoError(t, submitErr)
		require.Contains(t, steak, admissionTestTopic)
		requireSubmittedOutputHydrated(ctx, t, engineStore, beef)
		replay, replayErr := eng.Submit(ctx, tagged, engine.SubmitModeCurrent, nil)
		require.NoError(t, replayErr)
		require.Equal(t, steak, replay)
		require.Positive(t, countDocs(ctx, t, engineStore, outboxCollection))

		historicalStore := openAdmissionStore(ctx, t, replica, "hist-node")
		histEngine := engine.NewEngine(&engine.Config{
			Managers:     map[string]engine.TopicManager{admissionTestTopic: admitAllManager{}},
			Storage:      historicalStore,
			ChainTracker: admitAllTracker{},
		})
		histBeef := dummyAtomicBeef(t)
		histSteak, histSubmitErr := histEngine.Submit(ctx, overlay.TaggedBEEF{Topics: []string{admissionTestTopic}, Beef: histBeef}, engine.SubmitModeHistorical, nil)
		require.NoError(t, histSubmitErr)
		require.Contains(t, histSteak, admissionTestTopic)
		requireSubmittedOutputHydrated(ctx, t, historicalStore, histBeef)
		cursor, cursorErr := historicalStore.db.Collection(outboxCollection).Find(ctx, bson.D{{Key: fieldKind, Value: engine.AdmissionOutboxPropagation}})
		require.NoError(t, cursorErr)
		var propagation []bson.M
		require.NoError(t, cursor.All(ctx, &propagation))
		require.Empty(t, propagation)
	})

	t.Run("EngineStorageHydratesOutputsAndUTXOs", func(t *testing.T) {
		engineStore := openAdmissionStore(ctx, t, replica, "crud-node")
		txid := mustHash(t, strings.Repeat("ab", 32))
		outpoint := &transaction.Outpoint{Txid: txid, Index: 0}
		require.NoError(t, engineStore.InsertOutputs(ctx, admissionTestTopic, &txid, []uint32{0}, nil, nil, nil))
		found, findErr := engineStore.FindOutput(ctx, outpoint, topicPtr(admissionTestTopic), boolPtr(false), false)
		require.NoError(t, findErr)
		require.NotNil(t, found)
		require.Equal(t, admissionTestTopic, found.Topic)
		require.NoError(t, engineStore.InsertAppliedTransaction(ctx, &overlay.AppliedTransaction{Txid: &txid, Topic: admissionTestTopic}))
		exists, existsErr := engineStore.DoesAppliedTransactionExist(ctx, &overlay.AppliedTransaction{Txid: &txid, Topic: admissionTestTopic})
		require.NoError(t, existsErr)
		require.True(t, exists)
		require.NoError(t, engineStore.UpdateLastInteraction(ctx, "peer.example", admissionTestTopic, 12))
		since, sinceErr := engineStore.GetLastInteraction(ctx, "peer.example", admissionTestTopic)
		require.NoError(t, sinceErr)
		require.InDelta(t, 12, since, 0)
		utxos, utxoErr := engineStore.FindUTXOsForTopic(ctx, admissionTestTopic, 0, 10, false)
		require.NoError(t, utxoErr)
		require.Len(t, utxos, 1)
	})
}

func openAdmissionStore(ctx context.Context, t *testing.T, replica *mongotest.ReplicaSet, node string) *Store {
	t.Helper()
	store, err := New(ctx, replica.Client, admissionTestConfig("adm_"+bson.NewObjectID().Hex(), node))
	require.NoError(t, err)
	return store
}

func admissionTestConfig(database, node string) Config {
	return Config{Database: database, Scope: engine.StorageScope{Network: "testnet", GenesisHash: strings.Repeat("1", 64), NodeID: node}}
}

func hashChar(value byte) string { return strings.Repeat(string(value), 64) }

func testSteakJSON() string {
	body, err := json.Marshal(map[string]steakTopicDocument{admissionTestTopic: {OutputsToAdmit: []uint32{0}, CoinsToRetain: []uint32{}, CoinsRemoved: []uint32{}}})
	if err != nil {
		return ""
	}
	return string(body)
}

func admissionPlan(scope engine.StorageScope, operationID string) engine.AdmissionCommit {
	identity := engine.AdmissionIdentity{
		Scope:         scope,
		TxID:          hashChar('8'),
		Mode:          engine.AdmissionModeLive,
		ContextDigest: hashChar('2'),
		Topics:        []engine.AdmissionTopic{{Topic: admissionTestTopic, PolicyID: "policy-1"}},
	}
	raw := engine.AdmissionPayloadRef{Digest: hashChar('a'), ByteLength: "100", Kind: engine.AdmissionPayloadRawTransaction}
	script := engine.AdmissionPayloadRef{Digest: hashChar('b'), ByteLength: "25", Kind: engine.AdmissionPayloadLockingScript}
	proof := engine.AdmissionPayloadRef{Digest: hashChar('c'), ByteLength: "40", Kind: engine.AdmissionPayloadMerklePath}
	outboxData := engine.AdmissionPayloadRef{Digest: hashChar('d'), ByteLength: "10", Kind: engine.AdmissionPayloadOutboxData}
	source := engine.AdmissionOutpoint{TxID: hashChar('9'), OutputIndex: "0"}
	digest, _ := engine.AdmissionSemanticDigest(identity)
	return engine.AdmissionCommit{
		Key:      engine.AdmissionOperationKey{Scope: scope, OperationID: operationID, SemanticDigest: digest},
		Identity: identity,
		Payloads: []engine.AdmissionPayloadRef{raw, script, proof, outboxData},
		Decisions: []engine.AdmissionTopicDecision{{
			Topic:           admissionTestTopic,
			ExpectedHistory: engine.HistoryFence{ChainEpoch: "7", TopicHistoryGeneration: "3"},
			Reads:           []engine.AdmissionReadPredicate{{Key: "selection:tm_contract", ExpectedVersion: strPtr("r1")}},
			Spends:          []engine.AdmissionSpend{{Outpoint: source, ExpectedVersion: "spend-r1", Spender: identity.TxID}},
			Outputs: []engine.AdmissionOutput{{
				AdmissionOutpoint: engine.AdmissionOutpoint{TxID: identity.TxID, OutputIndex: "0"},
				Satoshis:          "1",
				Score:             "10",
				Script:            engine.AdmissionScriptRange{Payload: script, Offset: "0", ByteLength: "25"},
			}},
			Edges:   []engine.AdmissionEdge{{Source: source, Consumer: engine.AdmissionOutpoint{TxID: identity.TxID, OutputIndex: "0"}}},
			Applied: engine.AdmissionAppliedTransaction{TxID: identity.TxID, FirstSeenHeight: storagePtr("100"), Proof: &proof, Block: &engine.AdmissionAppliedBlock{Height: "101", Hash: hashChar('3'), Index: "0", MerkleRoot: hashChar('4')}},
		}},
		Outbox: []engine.AdmissionOutboxIntent{
			{EventID: operationID + ":lookup", Kind: engine.AdmissionOutboxLookup, Target: "ls_contract", Payloads: []engine.AdmissionPayloadRef{outboxData}},
			{EventID: operationID + ":propagate", Kind: engine.AdmissionOutboxPropagation, Target: "peer-a", Payloads: []engine.AdmissionPayloadRef{raw}},
		},
		Steak: testSteakJSON(),
	}
}

func seedAdmissionPlan(ctx context.Context, t *testing.T, store *Store, plan engine.AdmissionCommit, omit ...string) {
	t.Helper()
	skipped := make(map[string]struct{}, len(omit))
	for _, digest := range omit {
		skipped[digest] = struct{}{}
	}
	for _, decision := range plan.Decisions {
		require.NoError(t, store.EnsureHistoryFence(ctx, decision.Topic, decision.ExpectedHistory))
		for _, read := range decision.Reads {
			if read.ExpectedVersion != nil {
				require.NoError(t, store.seedRead(ctx, decision.Topic, read.Key, *read.ExpectedVersion))
			}
		}
		for _, spend := range decision.Spends {
			require.NoError(t, store.seedSpendable(ctx, decision.Topic, spend.Outpoint, spend.ExpectedVersion))
		}
	}
	for _, payload := range plan.Payloads {
		if _, skip := skipped[payload.Digest]; skip {
			continue
		}
		require.NoError(t, store.seedReadyPayload(ctx, payload))
	}
}

func requireCommitted(t *testing.T, result engine.AdmissionCommitResult) engine.AdmissionReceipt {
	t.Helper()
	require.Equal(t, engine.AdmissionCommitStateCommitted, result.State)
	require.NotNil(t, result.Receipt)
	return *result.Receipt
}

func mustCommit(ctx context.Context, t *testing.T, store *Store, plan engine.AdmissionCommit) engine.AdmissionCommitResult {
	t.Helper()
	result, err := store.CommitAdmission(ctx, plan)
	require.NoError(t, err)
	return result
}

func mustDigest(t *testing.T, identity engine.AdmissionIdentity) string {
	t.Helper()
	digest, err := engine.AdmissionSemanticDigest(identity)
	require.NoError(t, err)
	return digest
}

func countDocs(ctx context.Context, t *testing.T, store *Store, collection string) int64 {
	t.Helper()
	count, err := store.db.Collection(collection).CountDocuments(ctx, bson.D{{Key: fieldScope, Value: store.scopeID}})
	require.NoError(t, err)
	return count
}

func countScoped(ctx context.Context, t *testing.T, store *Store, collection, scope, eventID string) int64 {
	t.Helper()
	count, err := store.db.Collection(collection).CountDocuments(ctx, bson.D{{Key: fieldScope, Value: scope}, {Key: fieldEventID, Value: eventID}})
	require.NoError(t, err)
	return count
}

func strPtr(value string) *string { return &value }

func storagePtr(value string) *engine.StorageUint64 {
	parsed := engine.StorageUint64(value)
	return &parsed
}

func topicPtr(value string) *string { return &value }

func boolPtr(value bool) *bool { return &value }

func mustHash(t *testing.T, hexValue string) chainhash.Hash {
	t.Helper()
	var hash chainhash.Hash
	require.NoError(t, hash.SetBytes(mustDecodeHex(t, hexValue)))
	return hash
}

func mustDecodeHex(t *testing.T, value string) []byte {
	t.Helper()
	decoded, err := hex.DecodeString(value)
	require.NoError(t, err)
	return decoded
}

type admitAllManager struct{}

func (admitAllManager) IdentifyAdmissibleOutputs(_ context.Context, _ *transaction.Beef, _ *chainhash.Hash, _ []uint32) (overlay.AdmittanceInstructions, error) {
	return overlay.AdmittanceInstructions{OutputsToAdmit: []uint32{0}}, nil
}

func (admitAllManager) IdentifyNeededInputs(_ context.Context, _ *transaction.Beef, _ *chainhash.Hash) ([]*transaction.Outpoint, error) {
	return nil, nil
}
func (admitAllManager) GetDocumentation() string { return "test" }
func (admitAllManager) GetMetaData() *overlay.MetaData {
	return &overlay.MetaData{Name: "test"}
}

type admitAllTracker struct{}

func (admitAllTracker) IsValidRootForHeight(_ context.Context, _ *chainhash.Hash, _ uint32) (bool, error) {
	return true, nil
}
func (admitAllTracker) CurrentHeight(_ context.Context) (uint32, error) { return 1, nil }

func requireSubmittedOutputHydrated(ctx context.Context, t *testing.T, store *Store, beef []byte) {
	t.Helper()
	_, _, txid, err := transaction.ParseBeef(beef)
	require.NoError(t, err)
	require.NotNil(t, txid)
	found, findErr := store.FindOutput(ctx, &transaction.Outpoint{Txid: *txid, Index: 0}, topicPtr(admissionTestTopic), boolPtr(false), true)
	require.NoError(t, findErr)
	require.NotNil(t, found)
	require.NotNil(t, found.Beef)
	utxos, utxoErr := store.FindUTXOsForTopic(ctx, admissionTestTopic, 0, 10, true)
	require.NoError(t, utxoErr)
	require.Len(t, utxos, 1)
	require.Positive(t, utxos[0].Score)
	require.NotNil(t, utxos[0].Beef)
}

func dummyAtomicBeef(t *testing.T) []byte {
	t.Helper()
	dummyTx := testabilities.GivenTX().WithInput(1000).WithP2PKHOutput(999).TX()
	beef, err := transaction.NewBeefFromTransaction(dummyTx)
	require.NoError(t, err)
	bytes, err := beef.AtomicBytes(dummyTx.TxID())
	require.NoError(t, err)
	return bytes
}
