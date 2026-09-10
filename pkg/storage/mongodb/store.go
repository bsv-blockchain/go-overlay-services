package mongodb

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readconcern"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
	"go.mongodb.org/mongo-driver/v2/mongo/writeconcern"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/engine"
)

var (
	// ErrInvalidConfig indicates an unsupported or unbounded store configuration.
	ErrInvalidConfig = errors.New("invalid MongoDB store configuration")
	// ErrIncompatibleSchema indicates bootstrap found an incompatible schema or topology.
	ErrIncompatibleSchema = errors.New("incompatible MongoDB schema or topology")
	// ErrConflict indicates a conditional write no longer holds.
	ErrConflict = errors.New("MongoDB persistence conflict")
	// ErrInvalidPayload indicates a malformed content address or payload reference.
	ErrInvalidPayload = errors.New("invalid MongoDB payload")
	// ErrPayloadUnavailable indicates content is absent, unpublished, or being deleted.
	ErrPayloadUnavailable = errors.New("MongoDB payload unavailable")
	// ErrNestedTransaction prevents helpers from accidentally opening a second session.
	ErrNestedTransaction = errors.New("MongoDB helper requires a context without a session")
)

const (
	schemaVersion           = int32(1)
	payloadCollection       = "go_overlay_v1_payloads"
	referenceCollection     = "go_overlay_v1_references"
	operationCollection     = "go_overlay_v1_operations"
	schemaCollection        = "go_overlay_v1_schema"
	outputCollection        = "go_overlay_v1_outputs"
	edgeCollection          = "go_overlay_v1_edges"
	appliedCollection       = "go_overlay_v1_applied"
	transactionCollection   = "go_overlay_v1_transactions"
	outboxCollection        = "go_overlay_v1_outbox"
	readCollection          = "go_overlay_v1_reads"
	fenceCollection         = "go_overlay_v1_fences"
	leaseCollection         = "go_overlay_v1_leases"
	cursorCollection        = "go_overlay_v1_cursors"
	merkleStateUnmined      = "unmined"
	merkleStateValidated    = "validated"
	merkleStateInvalidated  = "invalidated"
	merkleStateImmutable    = "immutable"
	outboxStatePending      = "pending"
	payloadStateReady       = "ready"
	spendVersionInitial     = "1"
	maxAdmissionArrayLength = 1024
)

// Config binds a store to an explicit database and overlay scope. Zero limits
// select conservative defaults. Callers supply a dedicated unsharded replica
// set database; factories never select a backend or activate an engine.
type Config struct {
	Database           string
	Scope              engine.StorageScope
	InlineLimit        uint64
	MaxPayloadBytes    uint64
	TransactionTimeout time.Duration
	CommitTimeout      time.Duration
	LeaseDuration      time.Duration
	MaxBodyAttempts    int
	MaxCommitAttempts  int
}

// Store owns scoped persistence helpers. A Store created by New does not own the
// injected client. A Store created by Connect disconnects its client on Close.
type Store struct {
	client     *mongo.Client
	db         *mongo.Database
	config     Config
	chainID    string
	scopeID    string
	ownerID    string
	ownsClient bool
	blobs      *blobStore
	projector  any
}

func normalizeConfig(config Config) (Config, error) {
	if validateScope(config.Scope) != nil || config.Database == "" || len(config.Database) > 63 || strings.ContainsAny(config.Database, " /\\.\"$*<>:|?\x00") {
		return Config{}, ErrInvalidConfig
	}
	if config.InlineLimit == 0 {
		config.InlineLimit = 256 << 10
	}
	if config.MaxPayloadBytes == 0 {
		config.MaxPayloadBytes = 64 << 20
	}
	if config.TransactionTimeout == 0 {
		config.TransactionTimeout = 20 * time.Second
	}
	if config.CommitTimeout == 0 {
		config.CommitTimeout = 10 * time.Second
	}
	if config.LeaseDuration == 0 {
		config.LeaseDuration = 2 * time.Minute
	}
	if config.MaxBodyAttempts == 0 {
		config.MaxBodyAttempts = 3
	}
	if config.MaxCommitAttempts == 0 {
		config.MaxCommitAttempts = 3
	}
	if config.InlineLimit < 1 || config.InlineLimit > 1<<20 || config.MaxPayloadBytes < config.InlineLimit || config.MaxPayloadBytes > 1<<40 || config.TransactionTimeout < time.Millisecond || config.TransactionTimeout > 30*time.Second || config.CommitTimeout < time.Millisecond || config.CommitTimeout > 30*time.Second || config.LeaseDuration <= config.TransactionTimeout+config.CommitTimeout || config.LeaseDuration > 24*time.Hour || config.MaxBodyAttempts < 1 || config.MaxBodyAttempts > 10 || config.MaxCommitAttempts < 1 || config.MaxCommitAttempts > 10 {
		return Config{}, ErrInvalidConfig
	}
	return config, nil
}

// New validates and bootstraps the explicit scope using an injected client.
// It requires permission to inspect/create the package's versioned collections
// and indexes; incompatible existing structures are rejected without alteration.
func New(ctx context.Context, client *mongo.Client, config Config) (*Store, error) {
	if client == nil || mongo.SessionFromContext(ctx) != nil {
		return nil, ErrInvalidConfig
	}
	normalized, err := normalizeConfig(config)
	if err != nil {
		return nil, err
	}
	journal := true
	db := client.Database(normalized.Database, options.Database().SetReadConcern(readconcern.Majority()).SetReadPreference(readpref.Primary()).SetWriteConcern(&writeconcern.WriteConcern{W: "majority", Journal: &journal}))
	store := &Store{client: client, db: db, config: normalized, chainID: tupleID("chain", normalized.Scope.Network, normalized.Scope.GenesisHash), scopeID: tupleID("scope", normalized.Scope.Network, normalized.Scope.GenesisHash, normalized.Scope.NodeID), ownerID: uuid.NewString()}
	store.blobs = newBlobStore(db)
	bootstrapCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	if err = store.bootstrap(bootstrapCtx); err != nil {
		return nil, fmt.Errorf("bootstrap MongoDB store: %w", err)
	}
	return store, nil
}

// Connect creates an owned driver client and bootstraps an explicit store. URI
// credentials remain in the driver; no URI is retained in schema documents.
func Connect(ctx context.Context, uri string, config Config) (*Store, error) {
	if _, err := normalizeConfig(config); err != nil {
		return nil, err
	}
	client, err := mongo.Connect(options.Client().ApplyURI(uri).SetServerSelectionTimeout(10 * time.Second).SetConnectTimeout(10 * time.Second).SetTimeout(30 * time.Second).SetMaxPoolSize(32))
	if err != nil {
		return nil, fmt.Errorf("connect MongoDB: %w", err)
	}
	store, err := New(ctx, client, config)
	if err != nil {
		closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		return nil, errors.Join(err, client.Disconnect(closeCtx))
	}
	store.ownsClient = true
	return store, nil
}

// Close disconnects only a client owned by Connect. Injected clients remain the
// caller's responsibility. All helpers must have finished before Close.
func (s *Store) Close(ctx context.Context) error {
	if !s.ownsClient {
		return nil
	}
	return s.client.Disconnect(ctx)
}

// Scope returns the immutable node authority namespace of the store.
func (s *Store) Scope() engine.StorageScope { return s.config.Scope }

// AdmissionProtocol identifies the implemented overlay-admission-v1 capability.
func (s *Store) AdmissionProtocol() string {
	return engine.AdmissionStorageProtocol
}

// AdmissionStorage advertises this store as the v1 admission capability.
// A nil store does not advertise a protocol.
func (s *Store) AdmissionStorage() engine.AdmissionStorage {
	if s == nil {
		return nil
	}
	return s
}

func (s *Store) transactionOptions() *options.TransactionOptionsBuilder {
	return options.Transaction().SetReadConcern(readconcern.Snapshot()).SetReadPreference(readpref.Primary()).SetWriteConcern(majorityWriteConcern())
}

func serverLease(duration time.Duration) bson.D {
	return bson.D{{Key: "$dateAdd", Value: bson.D{{Key: "startDate", Value: serverNow}, {Key: "unit", Value: "millisecond"}, {Key: "amount", Value: duration.Milliseconds()}}}}
}

func leaseCurrent() bson.D {
	return bson.D{{Key: "$expr", Value: bson.D{{Key: "$gt", Value: bson.A{"$leaseUntil", "$$NOW"}}}}}
}

func leaseExpired() bson.D {
	return bson.D{{Key: "$expr", Value: bson.D{{Key: "$lte", Value: bson.A{"$leaseUntil", "$$NOW"}}}}}
}

func majorityWriteConcern() *writeconcern.WriteConcern {
	journal := true
	return &writeconcern.WriteConcern{W: "majority", Journal: &journal}
}

const (
	fieldID                     = "_id"
	fieldGuard                  = "guard"
	fieldDigest                 = "digest"
	fieldCreatedAt              = "createdAt"
	fieldUpdatedAt              = "updatedAt"
	fieldState                  = "state"
	fieldOwner                  = "owner"
	fieldChain                  = "chain"
	fieldLeaseUntil             = "leaseUntil"
	fieldToken                  = "token"
	fieldLength                 = "length"
	fieldScope                  = "scope"
	fieldVersion                = "version"
	fieldAttempt                = "attempt"
	fieldFilesID                = "files_id"
	fieldSet                    = "$set"
	fieldSetOnInsert            = "$setOnInsert"
	fieldLiteral                = "$literal"
	fieldTopic                  = "topic"
	fieldTxID                   = "txid"
	fieldOutputIndex            = "outputIndex"
	fieldSatoshis               = "satoshis"
	fieldScore                  = "score"
	fieldEngineScore            = "engineScore"
	fieldSpent                  = "spent"
	fieldSpentBy                = "spentBy"
	fieldSpendVersion           = "spendVersion"
	fieldServing                = "serving"
	fieldMerkleState            = "merkleState"
	fieldKind                   = "kind"
	fieldTarget                 = "target"
	fieldEventID                = "eventId"
	fieldPayloads               = "payloads"
	fieldReadVersion            = "readVersion"
	fieldKey                    = "key"
	fieldPeerID                 = "peerId"
	fieldJobID                  = "jobId"
	fieldChainEpoch             = "chainEpoch"
	fieldTopicHistoryGeneration = "topicHistoryGeneration"
	fieldExpiresAtMS            = "expiresAtMs"
	fieldAffectedFromHeight     = "affectedFromHeight"
	fieldCheckpoint             = "checkpoint"
	fieldHost                   = "host"
	fieldSince                  = "since"
	fieldNowMS                  = "nowMs"
	fieldSourceTxID             = "sourceTxid"
	fieldSourceIndex            = "sourceIndex"
	fieldConsumerTxID           = "consumerTxid"
	fieldConsumerIndex          = "consumerIndex"
	fieldAncillary              = "ancillaryTxids"
	fieldBeefDigest             = "beefDigest"
	fieldBeefLength             = "beefLength"
	fieldBeefKind               = "beefKind"
	fieldScriptDigest           = "scriptDigest"
	fieldScriptKind             = "scriptKind"
	fieldScriptOffset           = "scriptOffset"
	fieldScriptLength           = "scriptLength"
	fieldBlockHeight            = "blockHeight"
	fieldBlockIndex             = "blockIndex"
	fieldBlockHash              = "blockHash"
	fieldMerkleRoot             = "merkleRoot"
	fieldProven                 = "proven"
	fieldFirstSeenHeight        = "firstSeenHeight"
	fieldProofDigest            = "proofDigest"
	fieldProofKind              = "proofKind"
	fieldProofLength            = "proofLength"
	fieldOperationID            = "operationID"
	fieldByteLength             = "byteLength"
	serverNow                   = "$$NOW"
)

var (
	_ engine.Storage                  = (*Store)(nil)
	_ engine.AdmissionStorage         = (*Store)(nil)
	_ engine.AdmissionStorageProvider = (*Store)(nil)
)
