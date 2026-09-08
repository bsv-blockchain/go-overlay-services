package engine_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/engine"
)

type persistenceFixture struct {
	Identities []struct {
		Name     string                   `json:"name"`
		Identity engine.AdmissionIdentity `json:"identity"`
		Digest   string                   `json:"digest"`
	} `json:"identities"`
	Uint64 []struct {
		Value engine.StorageUint64 `json:"value"`
		Valid bool                 `json:"valid"`
	} `json:"uint64"`
	Cursors []struct {
		Name     string                    `json:"name"`
		Evidence engine.GASPCursorEvidence `json:"evidence"`
		Advance  bool                      `json:"advance"`
	} `json:"cursors"`
	Leases []struct {
		Name     string               `json:"name"`
		Expected engine.RecoveryLease `json:"expected"`
		Current  engine.RecoveryLease `json:"current"`
		NowMS    engine.StorageUint64 `json:"nowMs"`
		Valid    bool                 `json:"valid"`
	} `json:"leases"`
}

type admissionStorageStub struct {
	protocol string
}

func (s admissionStorageStub) AdmissionProtocol() string {
	return s.protocol
}

func (admissionStorageStub) CommitAdmission(_ context.Context, _ engine.AdmissionCommit) (engine.AdmissionCommitResult, error) {
	return engine.AdmissionCommitResult{}, nil
}

func (admissionStorageStub) ReconcileAdmission(_ context.Context, _ engine.AdmissionOperationKey, _ *string) (engine.AdmissionReconcileResult, error) {
	return engine.AdmissionReconcileResult{}, nil
}

type admissionStorageProviderStub struct {
	storage engine.AdmissionStorage
}

func (s admissionStorageProviderStub) AdmissionStorage() engine.AdmissionStorage {
	return s.storage
}

type replaySafeProjectionStub struct {
	protocol string
}

func (s replaySafeProjectionStub) ProjectionProtocol() string {
	return s.protocol
}

func (replaySafeProjectionStub) ApplyEvent(_ context.Context, _ engine.StorageScope, _ engine.AdmissionOutboxIntent) (string, error) {
	return "checkpoint", nil
}

func (replaySafeProjectionStub) Reconcile(_ context.Context, _ engine.StorageScope, _ *string) (string, error) {
	return "checkpoint", nil
}

var (
	_ engine.AdmissionStorage     = admissionStorageStub{}
	_ engine.ReplaySafeProjection = replaySafeProjectionStub{}
)

func TestAdmissionStorageCapabilityReturnsOnlyExplicitV1Advertisement(t *testing.T) {
	var nilProvider *admissionStorageProviderStub
	var nilStorage *admissionStorageStub
	tests := []struct {
		name      string
		provider  any
		wantFound bool
	}{
		{
			name:      "legacy provider",
			provider:  struct{}{},
			wantFound: false,
		},
		{
			name:      "typed nil provider",
			provider:  nilProvider,
			wantFound: false,
		},
		{
			name: "typed nil capability",
			provider: admissionStorageProviderStub{
				storage: nilStorage,
			},
			wantFound: false,
		},
		{
			name:      "nil capability",
			provider:  admissionStorageProviderStub{},
			wantFound: false,
		},
		{
			name: "different protocol",
			provider: admissionStorageProviderStub{
				storage: admissionStorageStub{protocol: "other"},
			},
			wantFound: false,
		},
		{
			name: "v1 capability",
			provider: admissionStorageProviderStub{
				storage: admissionStorageStub{protocol: engine.AdmissionStorageProtocol},
			},
			wantFound: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := engine.GetAdmissionStorage(tt.provider)
			if tt.wantFound {
				require.NotNil(t, actual)
				return
			}
			require.Nil(t, actual)
		})
	}
}

func TestReplaySafeProjectionCapabilityReturnsOnlyV1Advertisement(t *testing.T) {
	var nilProjection *replaySafeProjectionStub
	require.Nil(t, engine.GetReplaySafeProjection(struct{}{}))
	require.Nil(t, engine.GetReplaySafeProjection(nilProjection))
	require.Nil(t, engine.GetReplaySafeProjection(replaySafeProjectionStub{protocol: "other"}))
	require.NotNil(t, engine.GetReplaySafeProjection(replaySafeProjectionStub{protocol: engine.ReplaySafeProjectionProtocol}))
}

func TestAdmissionStorageReconcileSupportsOperationRecoveryWithoutAnAttempt(t *testing.T) {
	storage := admissionStorageStub{protocol: engine.AdmissionStorageProtocol}
	key := engine.AdmissionOperationKey{OperationID: "operation"}

	_, err := storage.ReconcileAdmission(context.Background(), key, nil)
	require.NoError(t, err)

	emptyAttemptID := ""
	_, err = storage.ReconcileAdmission(context.Background(), key, &emptyAttemptID)
	require.NoError(t, err)
}

func TestReplaySafeProjectionMethodsReceiveScope(t *testing.T) {
	projection := replaySafeProjectionStub{protocol: engine.ReplaySafeProjectionProtocol}
	scope := engine.StorageScope{Network: "test", GenesisHash: "genesis", NodeID: "node"}

	checkpoint, err := projection.ApplyEvent(context.Background(), scope, engine.AdmissionOutboxIntent{EventID: "event"})
	require.NoError(t, err)
	require.Equal(t, "checkpoint", checkpoint)

	nextCheckpoint, err := projection.Reconcile(context.Background(), scope, &checkpoint)
	require.NoError(t, err)
	require.Equal(t, "checkpoint", nextCheckpoint)
}

func TestAdmissionSemanticDigestMatchesFixture(t *testing.T) {
	fixture := loadPersistenceFixture(t)
	for _, tt := range fixture.Identities {
		t.Run(tt.Name, func(t *testing.T) {
			actual, err := engine.AdmissionSemanticDigest(tt.Identity)
			require.NoError(t, err)
			require.Equal(t, tt.Digest, actual)
		})
	}
}

func TestAdmissionSemanticDigestBindsScopeAndContextButExcludesPlanEvidence(t *testing.T) {
	identity := engine.AdmissionIdentity{
		Scope: engine.StorageScope{
			Network:     "test",
			GenesisHash: "0000000000000000000000000000000000000000000000000000000000000000",
			NodeID:      "node-a",
		},
		TxID:          "1111111111111111111111111111111111111111111111111111111111111111",
		Mode:          engine.AdmissionModeLive,
		ContextDigest: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		Topics: []engine.AdmissionTopic{
			{Topic: "tm_b", PolicyID: "policy-b"},
			{Topic: "tm_a", PolicyID: "policy-a"},
		},
	}

	digest, err := engine.AdmissionSemanticDigest(identity)
	require.NoError(t, err)

	reordered := identity
	reordered.Topics = []engine.AdmissionTopic{identity.Topics[1], identity.Topics[0]}
	reorderedDigest, err := engine.AdmissionSemanticDigest(reordered)
	require.NoError(t, err)
	require.Equal(t, digest, reorderedDigest)

	differentScope := identity
	differentScope.Scope.NodeID = "node-b"
	differentScopeDigest, err := engine.AdmissionSemanticDigest(differentScope)
	require.NoError(t, err)
	require.NotEqual(t, digest, differentScopeDigest)

	differentContext := identity
	differentContext.ContextDigest = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b856"
	differentContextDigest, err := engine.AdmissionSemanticDigest(differentContext)
	require.NoError(t, err)
	require.NotEqual(t, digest, differentContextDigest)

	commitWithProofA := engine.AdmissionCommit{Identity: identity, Decisions: []engine.AdmissionTopicDecision{{
		Applied: engine.AdmissionAppliedTransaction{Proof: &engine.AdmissionPayloadRef{Digest: "proof-a"}},
	}}}
	commitWithProofB := commitWithProofA
	commitWithProofB.Decisions = []engine.AdmissionTopicDecision{{
		Applied: engine.AdmissionAppliedTransaction{Proof: &engine.AdmissionPayloadRef{Digest: "proof-b"}},
	}}
	proofADigest, err := engine.AdmissionSemanticDigest(commitWithProofA.Identity)
	require.NoError(t, err)
	proofBDigest, err := engine.AdmissionSemanticDigest(commitWithProofB.Identity)
	require.NoError(t, err)
	require.Equal(t, proofADigest, proofBDigest)
}

func TestAdmissionSemanticDigestRejectsInvalidIdentity(t *testing.T) {
	valid := engine.AdmissionIdentity{
		Scope: engine.StorageScope{
			Network:     "test",
			GenesisHash: "0000000000000000000000000000000000000000000000000000000000000000",
			NodeID:      "node-a",
		},
		TxID:          "1111111111111111111111111111111111111111111111111111111111111111",
		Mode:          engine.AdmissionModeLive,
		ContextDigest: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		Topics:        []engine.AdmissionTopic{{Topic: "tm_a", PolicyID: "policy-a"}},
	}

	tests := []struct {
		name    string
		mutate  func(*engine.AdmissionIdentity)
		wantErr error
	}{
		{
			name: "uppercase hash",
			mutate: func(identity *engine.AdmissionIdentity) {
				identity.TxID = "A111111111111111111111111111111111111111111111111111111111111111"
			},
			wantErr: engine.ErrInvalidAdmissionHash,
		},
		{
			name: "unsupported mode",
			mutate: func(identity *engine.AdmissionIdentity) {
				identity.Mode = "future"
			},
			wantErr: engine.ErrInvalidAdmissionMode,
		},
		{
			name: "duplicate topic",
			mutate: func(identity *engine.AdmissionIdentity) {
				identity.Topics = append(identity.Topics, engine.AdmissionTopic{Topic: "tm_a", PolicyID: "another"})
			},
			wantErr: engine.ErrInvalidAdmissionTopics,
		},
		{
			name: "invalid utf8",
			mutate: func(identity *engine.AdmissionIdentity) {
				identity.Scope.Network = string([]byte{0xff})
			},
			wantErr: engine.ErrInvalidAdmissionIdentity,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			identity := valid
			identity.Topics = append([]engine.AdmissionTopic(nil), valid.Topics...)
			tt.mutate(&identity)
			_, err := engine.AdmissionSemanticDigest(identity)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestParseStorageUint64MatchesFixtureWithoutPrecisionLoss(t *testing.T) {
	fixture := loadPersistenceFixture(t)
	for _, tt := range fixture.Uint64 {
		t.Run(string(tt.Value), func(t *testing.T) {
			actual, err := engine.ParseStorageUint64(tt.Value)
			if !tt.Valid {
				require.ErrorIs(t, err, engine.ErrInvalidStorageUint64)
				return
			}
			require.NoError(t, err)
			if tt.Value == "9007199254740993" {
				require.Equal(t, uint64(9007199254740993), actual)
			}
		})
	}

	_, err := engine.ParseStorageUint64("100000000000000000000")
	require.ErrorIs(t, err, engine.ErrInvalidStorageUint64)
}

func TestRecoveryLeaseCurrentMatchesFixture(t *testing.T) {
	fixture := loadPersistenceFixture(t)
	for _, tt := range fixture.Leases {
		t.Run(tt.Name, func(t *testing.T) {
			actual, err := engine.IsRecoveryLeaseCurrent(tt.Expected, tt.Current, tt.NowMS)
			require.NoError(t, err)
			require.Equal(t, tt.Valid, actual)
		})
	}
}

func TestRecoveryLeaseCurrentRejectsInvalidFence(t *testing.T) {
	lease := engine.RecoveryLease{
		HistoryFence: engine.HistoryFence{ChainEpoch: "1", TopicHistoryGeneration: "1"},
		Scope:        engine.StorageScope{Network: "test", GenesisHash: "genesis", NodeID: "node"},
		Topic:        "topic",
		PeerID:       "peer",
		JobID:        "job",
		LeaseToken:   "01",
		ExpiresAtMS:  "100",
	}
	current, err := engine.IsRecoveryLeaseCurrent(lease, lease, "1")
	require.False(t, current)
	require.ErrorIs(t, err, engine.ErrInvalidStorageUint64)
}

func TestCanAdvanceGASPCursorMatchesFixture(t *testing.T) {
	fixture := loadPersistenceFixture(t)
	for _, tt := range fixture.Cursors {
		t.Run(tt.Name, func(t *testing.T) {
			require.Equal(t, tt.Advance, engine.CanAdvanceGASPCursor(tt.Evidence))
		})
	}
}

func loadPersistenceFixture(t *testing.T) persistenceFixture {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("testdata", "persistence-v1.json"))
	require.NoError(t, err)

	var fixture persistenceFixture
	require.NoError(t, json.Unmarshal(data, &fixture))
	return fixture
}
