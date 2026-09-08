package engine

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"reflect"
	"sort"
	"strconv"
	"unicode/utf8"
)

const (
	// AdmissionStorageProtocol identifies the optional v1 admission capability.
	AdmissionStorageProtocol = "overlay-admission-v1"
	// ReplaySafeProjectionProtocol identifies the optional v1 replay-safe projection capability.
	ReplaySafeProjectionProtocol = "overlay-projection-v1"
)

var (
	// ErrInvalidStorageUint64 is returned when a storage uint64 is not canonical unsigned decimal text.
	ErrInvalidStorageUint64 = errors.New("invalid storage uint64")
	// ErrInvalidStorageOutputIndex is returned when a canonical uint64 is outside the uint32 transaction outpoint index domain.
	ErrInvalidStorageOutputIndex = errors.New("invalid storage output index")
	// ErrInvalidAdmissionHash is returned when an admission identity has a non-canonical hash.
	ErrInvalidAdmissionHash = errors.New("invalid admission hash")
	// ErrInvalidAdmissionMode is returned when an admission identity has an unsupported mode.
	ErrInvalidAdmissionMode = errors.New("invalid admission mode")
	// ErrInvalidAdmissionTopics is returned when an admission identity has no topics or duplicate topics.
	ErrInvalidAdmissionTopics = errors.New("invalid admission topics")
	// ErrInvalidAdmissionIdentity is returned when a framed admission identity field is empty or invalid UTF-8.
	ErrInvalidAdmissionIdentity = errors.New("invalid admission identity")
)

// StorageUint64 is canonical unsigned decimal text in the range 0 through 2^64-1.
// Callers must not convert it through a floating-point value.
type StorageUint64 string

// StorageScope scopes durable data to one overlay network, genesis block, and node.
type StorageScope struct {
	Network     string
	GenesisHash string
	NodeID      string
}

// HistoryFence contains independently advancing chain and topic-history revisions.
type HistoryFence struct {
	ChainEpoch             StorageUint64
	TopicHistoryGeneration StorageUint64
}

// AdmissionIdentity contains immutable inputs that determine admission idempotency.
type AdmissionIdentity struct {
	Scope         StorageScope
	TxID          string
	Mode          AdmissionMode
	ContextDigest string
	Topics        []AdmissionTopic
}

// AdmissionMode identifies whether an admission is evaluated against live or historical state.
type AdmissionMode string

const (
	// AdmissionModeLive is admission against live state.
	AdmissionModeLive AdmissionMode = "live"
	// AdmissionModeHistorical is admission against historical state.
	AdmissionModeHistorical AdmissionMode = "historical"
)

// AdmissionTopic binds a topic to its immutable policy identifier.
type AdmissionTopic struct {
	Topic    string
	PolicyID string
}

// AdmissionOperationKey names one scope-local idempotent admission operation.
type AdmissionOperationKey struct {
	Scope          StorageScope
	OperationID    string
	SemanticDigest string
}

// AdmissionPayloadRef refers to content that was published outside the database transaction.
type AdmissionPayloadRef struct {
	Digest     string
	ByteLength StorageUint64
	Kind       AdmissionPayloadKind
}

// AdmissionPayloadKind identifies the content addressed by an AdmissionPayloadRef.
type AdmissionPayloadKind string

const (
	// AdmissionPayloadRawTransaction references raw transaction bytes.
	AdmissionPayloadRawTransaction AdmissionPayloadKind = "raw-transaction"
	// AdmissionPayloadMerklePath references a Merkle path.
	AdmissionPayloadMerklePath AdmissionPayloadKind = "merkle-path"
	// AdmissionPayloadBEEFManifest references a BEEF manifest.
	AdmissionPayloadBEEFManifest AdmissionPayloadKind = "beef-manifest"
	// AdmissionPayloadLockingScript references a locking script.
	AdmissionPayloadLockingScript AdmissionPayloadKind = "locking-script"
	// AdmissionPayloadOutboxData references outbox data.
	AdmissionPayloadOutboxData AdmissionPayloadKind = "outbox-data"
)

// AdmissionOutpoint identifies an output using a canonical transaction ID and a uint32 wire outpoint index.
type AdmissionOutpoint struct {
	TxID        string
	OutputIndex StorageUint64
}

// AdmissionReadPredicate captures a point, range, or projection read that influenced admission.
// ExpectedVersion is nil when absence is the required predicate.
type AdmissionReadPredicate struct {
	Key             string
	ExpectedVersion *string
}

// AdmissionScriptRange points to a byte range in a script payload.
type AdmissionScriptRange struct {
	Payload    AdmissionPayloadRef
	Offset     StorageUint64
	ByteLength StorageUint64
}

// AdmissionOutput contains the durable fields for one admitted output.
type AdmissionOutput struct {
	AdmissionOutpoint
	Satoshis StorageUint64
	Score    StorageUint64
	Script   AdmissionScriptRange
}

// AdmissionSpend conditionally marks an outpoint spent by a transaction.
type AdmissionSpend struct {
	Outpoint        AdmissionOutpoint
	ExpectedVersion string
	Spender         string
}

// AdmissionEdge associates an admitted output with one consumer output.
type AdmissionEdge struct {
	Source   AdmissionOutpoint
	Consumer AdmissionOutpoint
}

// AdmissionAppliedBlock records the block that supplied a retained applied transaction.
type AdmissionAppliedBlock struct {
	Height     StorageUint64
	Hash       string
	Index      StorageUint64
	MerkleRoot string
}

// AdmissionAppliedTransaction preserves history after an output is spent or evicted from serving state.
type AdmissionAppliedTransaction struct {
	TxID            string
	FirstSeenHeight *StorageUint64
	Proof           *AdmissionPayloadRef
	Block           *AdmissionAppliedBlock
}

// AdmissionTopicDecision contains all topic-local effects to validate and save atomically.
type AdmissionTopicDecision struct {
	Topic           string
	ExpectedHistory HistoryFence
	Reads           []AdmissionReadPredicate
	Spends          []AdmissionSpend
	Evictions       []AdmissionOutpoint
	Outputs         []AdmissionOutput
	Edges           []AdmissionEdge
	Applied         AdmissionAppliedTransaction
	HistoryUpdate   *AdmissionHistoryUpdate
}

// AdmissionHistoryUpdate invalidates dependent history anchors and comparisons.
// A handoff is present only when the recovery worker itself caused the revision.
type AdmissionHistoryUpdate struct {
	NextTopicHistoryGeneration StorageUint64
	AffectedFromHeight         StorageUint64
	Handoff                    *HistoryRevisionHandoff
}

// AdmissionOutboxIntent records an event whose payloads are ready before commit.
type AdmissionOutboxIntent struct {
	EventID  string
	Kind     AdmissionOutboxKind
	Target   string
	Payloads []AdmissionPayloadRef
}

// AdmissionOutboxKind identifies the intended recipient behavior for an outbox event.
type AdmissionOutboxKind string

const (
	// AdmissionOutboxLookup is an event for lookup projection.
	AdmissionOutboxLookup AdmissionOutboxKind = "lookup"
	// AdmissionOutboxPropagation is an event for propagation.
	AdmissionOutboxPropagation AdmissionOutboxKind = "propagation"
)

// AdmissionCommit is a complete immutable admission plan. Providers must revalidate its predicates atomically.
type AdmissionCommit struct {
	Key       AdmissionOperationKey
	Identity  AdmissionIdentity
	Payloads  []AdmissionPayloadRef
	Decisions []AdmissionTopicDecision
	Outbox    []AdmissionOutboxIntent
	Steak     string
}

// AdmissionReceipt is the durable result returned only for a committed local admission.
type AdmissionReceipt struct {
	OperationID    string
	SemanticDigest string
	Durability     AdmissionDurability
	Steak          string
	Indexes        []AdmissionIndexStatus
	Propagation    AdmissionPropagationState
}

// AdmissionDurability identifies the durability guarantee established by the selected adapter.
type AdmissionDurability string

const (
	// AdmissionDurabilityAtomicLocal means the provider atomically committed its local effects.
	AdmissionDurabilityAtomicLocal AdmissionDurability = "atomic-local"
)

// AdmissionIndexStatus records whether an index was visible at local commit time.
type AdmissionIndexStatus struct {
	Target string
	State  AdmissionIndexState
}

// AdmissionIndexState identifies index visibility at commit time.
type AdmissionIndexState string

const (
	// AdmissionIndexVisible means the enlisted index was visible at commit.
	AdmissionIndexVisible AdmissionIndexState = "visible"
	// AdmissionIndexPending means an external index is still pending.
	AdmissionIndexPending AdmissionIndexState = "pending"
)

// AdmissionPropagationState records whether propagation was requested at commit time.
type AdmissionPropagationState string

const (
	// AdmissionPropagationNotRequested means no propagation was requested.
	AdmissionPropagationNotRequested AdmissionPropagationState = "not-requested"
	// AdmissionPropagationPending means propagation is pending and is not an acknowledgement.
	AdmissionPropagationPending AdmissionPropagationState = "pending"
)

// AdmissionCommitState identifies a committed, pending, rejected, or reconciled-aborted admission attempt.
type AdmissionCommitState string

const (
	// AdmissionCommitStateCommitted contains a durable receipt.
	AdmissionCommitStateCommitted AdmissionCommitState = "committed"
	// AdmissionCommitStatePending retains an opaque attempt identifier for reconciliation.
	AdmissionCommitStatePending AdmissionCommitState = "pending"
	// AdmissionCommitStateRejected indicates a plan conflict or validation rejection.
	AdmissionCommitStateRejected AdmissionCommitState = "rejected"
	// AdmissionCommitStateAborted indicates reconciliation established that the attempt did not commit.
	AdmissionCommitStateAborted AdmissionCommitState = "aborted"
)

// AdmissionRejectionCode classifies a rejected admission plan.
type AdmissionRejectionCode string

const (
	// AdmissionRejectionDigestMismatch indicates a conflicting operation semantic digest.
	AdmissionRejectionDigestMismatch AdmissionRejectionCode = "digest-mismatch"
	// AdmissionRejectionReadConflict indicates an admission read predicate no longer holds.
	AdmissionRejectionReadConflict AdmissionRejectionCode = "read-conflict"
	// AdmissionRejectionSpendConflict indicates a conditional spend no longer holds.
	AdmissionRejectionSpendConflict AdmissionRejectionCode = "spend-conflict"
	// AdmissionRejectionPayloadNotReady indicates a payload pin was unavailable.
	AdmissionRejectionPayloadNotReady AdmissionRejectionCode = "payload-not-ready"
	// AdmissionRejectionUnsupportedProjection indicates a required projection cannot be supported.
	AdmissionRejectionUnsupportedProjection AdmissionRejectionCode = "unsupported-projection"
	// AdmissionRejectionInvalidPlan indicates the plan is invalid.
	AdmissionRejectionInvalidPlan AdmissionRejectionCode = "invalid-plan"
)

// AdmissionCommitResult describes one terminal committed or rejected result, or a pending unknown commit.
// Receipt is set only for committed results, AttemptID only for pending results, and RejectionCode only for rejected results. Aborted is legal only when this type is returned by ReconcileAdmission.
type AdmissionCommitResult struct {
	State         AdmissionCommitState
	Receipt       *AdmissionReceipt
	AttemptID     string
	RejectionCode AdmissionRejectionCode
}

// AdmissionReconcileResult describes reconciliation for the same opaque admission attempt.
type AdmissionReconcileResult = AdmissionCommitResult

// AdmissionStorage is an optional v1 persistence capability, independent of legacy Storage.
// A provider must atomically revalidate every predicate and fence, save all effects and its receipt, and recover a persisted unresolved attempt before a repeated commit starts a fresh body.
type AdmissionStorage interface {
	AdmissionProtocol() string
	CommitAdmission(ctx context.Context, plan AdmissionCommit) (AdmissionCommitResult, error)
	ReconcileAdmission(ctx context.Context, key AdmissionOperationKey, attemptID *string) (AdmissionReconcileResult, error)
}

// AdmissionStorageProvider explicitly advertises an optional AdmissionStorage capability.
// Its presence declares shape only and does not attest to adapter durability.
type AdmissionStorageProvider interface {
	AdmissionStorage() AdmissionStorage
}

// ReplaySafeProjection is an optional projection capability that can apply and reconcile scope-local outbox events.
type ReplaySafeProjection interface {
	ProjectionProtocol() string
	ApplyEvent(ctx context.Context, scope StorageScope, intent AdmissionOutboxIntent) (checkpoint string, err error)
	Reconcile(ctx context.Context, scope StorageScope, checkpoint *string) (nextCheckpoint string, err error)
}

// GetAdmissionStorage returns a declared v1 capability when provider explicitly advertises the matching protocol.
// It returns nil for legacy providers, absent capabilities, and capabilities with another protocol.
func GetAdmissionStorage(provider any) AdmissionStorage {
	if isNilCapability(provider) {
		return nil
	}

	candidate, ok := provider.(AdmissionStorageProvider)
	if !ok {
		return nil
	}

	admission := candidate.AdmissionStorage()
	if isNilCapability(admission) || admission.AdmissionProtocol() != AdmissionStorageProtocol {
		return nil
	}

	return admission
}

// GetReplaySafeProjection returns a declared replay-safe projection with the v1 protocol, or nil.
func GetReplaySafeProjection(candidate any) ReplaySafeProjection {
	if isNilCapability(candidate) {
		return nil
	}

	projection, ok := candidate.(ReplaySafeProjection)
	if !ok || projection.ProjectionProtocol() != ReplaySafeProjectionProtocol {
		return nil
	}

	return projection
}

// ParseStorageUint64 parses canonical unsigned decimal storage text without accepting signs, whitespace, or leading zeroes.
func ParseStorageUint64(value StorageUint64) (uint64, error) {
	if len(value) == 0 || len(value) > 20 || (len(value) > 1 && value[0] == '0') {
		return 0, ErrInvalidStorageUint64
	}
	for i := range len(value) {
		if value[i] < '0' || value[i] > '9' {
			return 0, ErrInvalidStorageUint64
		}
	}

	parsed, err := strconv.ParseUint(string(value), 10, 64)
	if err != nil {
		return 0, ErrInvalidStorageUint64
	}
	return parsed, nil
}

// ParseStorageOutputIndex parses a canonical uint64 and ensures it fits the uint32 transaction wire outpoint index domain.
func ParseStorageOutputIndex(value StorageUint64) (uint32, error) {
	parsed, err := ParseStorageUint64(value)
	if err != nil {
		return 0, err
	}
	if parsed > 1<<32-1 {
		return 0, ErrInvalidStorageOutputIndex
	}
	return uint32(parsed), nil
}

func isNilCapability(value any) bool {
	if value == nil {
		return true
	}

	reflectValue := reflect.ValueOf(value)
	switch reflectValue.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflectValue.IsNil()
	default:
		return false
	}
}

// AdmissionSemanticDigest returns the SHA-256 identity digest defined by persistence-v1.
// Mutable reads, decisions, and proof variants are deliberately excluded; providers still must validate their predicates atomically.
func AdmissionSemanticDigest(identity AdmissionIdentity) (string, error) {
	if !isLowerHexHash(identity.TxID) || !isLowerHexHash(identity.Scope.GenesisHash) || !isLowerHexHash(identity.ContextDigest) {
		return "", ErrInvalidAdmissionHash
	}
	if identity.Mode != AdmissionModeLive && identity.Mode != AdmissionModeHistorical {
		return "", ErrInvalidAdmissionMode
	}

	topics := append([]AdmissionTopic(nil), identity.Topics...)
	sort.Slice(topics, func(i, j int) bool {
		return bytes.Compare([]byte(topics[i].Topic), []byte(topics[j].Topic)) < 0
	})
	if len(topics) == 0 {
		return "", ErrInvalidAdmissionTopics
	}
	for i := 1; i < len(topics); i++ {
		if topics[i-1].Topic == topics[i].Topic {
			return "", ErrInvalidAdmissionTopics
		}
	}

	fields := make([]string, 0, 8+2*len(topics))
	fields = append(fields, AdmissionStorageProtocol, identity.Scope.Network, identity.Scope.GenesisHash,
		identity.Scope.NodeID, identity.TxID, string(identity.Mode), identity.ContextDigest, strconv.Itoa(len(topics)))
	for _, topic := range topics {
		fields = append(fields, topic.Topic, topic.PolicyID)
	}

	hash := sha256.New()
	for _, field := range fields {
		if field == "" || !utf8.ValidString(field) {
			return "", ErrInvalidAdmissionIdentity
		}
		bytes := []byte(field)
		_, _ = hash.Write([]byte(strconv.Itoa(len(bytes)) + ":"))
		_, _ = hash.Write(bytes)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func isLowerHexHash(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	for i := range len(value) {
		if (value[i] < '0' || value[i] > '9') && (value[i] < 'a' || value[i] > 'f') {
			return false
		}
	}
	return true
}
