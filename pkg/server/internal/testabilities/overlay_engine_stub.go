package testabilities

import (
	"context"
	"errors"
	"testing"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/engine"
	"github.com/bsv-blockchain/go-overlay-services/pkg/core/gasp"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server/internal/app"
	"github.com/bsv-blockchain/go-sdk/chainhash"
	"github.com/bsv-blockchain/go-sdk/overlay"
	"github.com/bsv-blockchain/go-sdk/overlay/lookup"
	"github.com/bsv-blockchain/go-sdk/transaction"
)

// ProviderStateAsserter is an interface for asserting internal state after a test run.
type ProviderStateAsserter interface {
	AssertCalled()
}

// ARCIngestProvider extends app.ArcIngestProvider with the ability
// to assert whether it was called during a test.
type ARCIngestProvider interface {
	app.ARCIngestProvider
	ProviderStateAsserter
}

// LookupQuestionProvider extends app.LookupQuestionProvider with the ability
// to assert whether it was called during a test.
type LookupQuestionProvider interface {
	app.LookupQuestionProvider
	ProviderStateAsserter
}

// SyncAdvertisementsProvider extends app.SyncAdvertisementsProvider with the ability
// to assert whether it was called during a test.
type SyncAdvertisementsProvider interface {
	app.SyncAdvertisementsProvider
	ProviderStateAsserter
}

// SubmitTransactionProvider extends app.SubmitTransactionProvider with the ability
// to assert whether it was called during a test.
type SubmitTransactionProvider interface {
	app.SubmitTransactionProvider
	ProviderStateAsserter
}

// LookupListProvider extends app.LookupListProvider with the ability
// to assert whether it was called during a test.
type LookupListProvider interface {
	app.LookupListProvider
	ProviderStateAsserter
}

// TopicManagersListProvider extends app.TopicManagersListProvider with the ability
// to assert whether it was called during a test.
type TopicManagersListProvider interface {
	app.TopicManagersListProvider
	ProviderStateAsserter
}

// LookupServiceDocumentationProvider extends app.LookupServiceDocumentationProvider with the ability
// to assert whether it was called during a test.
type LookupServiceDocumentationProvider interface {
	app.LookupServiceDocumentationProvider
	ProviderStateAsserter
}

// StartGASPSyncProvider extends app.StartGASPSyncProvider with the ability
// to assert whether it was called during a test.
type StartGASPSyncProvider interface {
	app.StartGASPSyncProvider
	ProviderStateAsserter
}

// TopicManagerDocumentationProvider extends app.TopicManagerDocumentationProvider with the ability
// to assert whether it was called during a test.
type TopicManagerDocumentationProvider interface {
	app.TopicManagerDocumentationProvider
	ProviderStateAsserter
}

// RequestForeignGASPNodeProvider extends app.RequestForeignGASPNodeProvider with the ability
// to assert whether it was called during a test.
type RequestForeignGASPNodeProvider interface {
	app.RequestForeignGASPNodeProvider
	ProviderStateAsserter
}

// RequestSyncResponseProvider extends app.RequestSyncResponseProvider with the ability
// to assert whether it was called during a test.
type RequestSyncResponseProvider interface {
	app.RequestSyncResponseProvider
	ProviderStateAsserter
}

// TestOverlayEngineStubOption is a functional option type used to configure a TestOverlayEngineStub.
// It allows setting custom behaviors for different parts of the TestOverlayEngineStub.
type TestOverlayEngineStubOption func(*TestOverlayEngineStub)

// WithARCIngestProvider allows setting a custom ARCIngestProvider in a TestOverlayEngineStub.
// This can be used to mock arc ingest behavior during tests.
func WithARCIngestProvider(provider ARCIngestProvider) TestOverlayEngineStubOption {
	return func(stub *TestOverlayEngineStub) {
		stub.arcIngestProvider = provider
	}
}

// WithSubmitTransactionProvider allows setting a custom SubmitTransactionProvider in a TestOverlayEngineStub.
// This can be used to mock transaction submission behavior during tests.
func WithSubmitTransactionProvider(provider SubmitTransactionProvider) TestOverlayEngineStubOption {
	return func(stub *TestOverlayEngineStub) {
		stub.submitTransactionProvider = provider
	}
}

// WithTopicManagersListProvider allows setting a custom TopicManagersListProvider in a TestOverlayEngineStub.
// This can be used to mock topic managers list behavior during tests.
func WithTopicManagersListProvider(provider TopicManagersListProvider) TestOverlayEngineStubOption {
	return func(stub *TestOverlayEngineStub) {
		stub.topicManagersListProvider = provider
	}
}

// WithLookupQuestionProvider allows setting a custom LookupQuestionProvider in a TestOverlayEngineStub.
// This can be used to mock lookup question behavior during tests.
func WithLookupQuestionProvider(provider LookupQuestionProvider) TestOverlayEngineStubOption {
	return func(stub *TestOverlayEngineStub) {
		stub.lookupQuestionProvider = provider
	}
}

// WithLookupListProvider allows setting a custom LookupListProvider in a TestOverlayEngineStub.
// This can be used to mock lookup service provider list behavior during tests.
func WithLookupListProvider(provider LookupListProvider) TestOverlayEngineStubOption {
	return func(stub *TestOverlayEngineStub) {
		stub.lookupListProvider = provider
	}
}

// WithLookupDocumentationProvider allows setting a custom LookupServiceDocumentationProvider in a TestOverlayEngineStub.
// This can be used to mock lookup service documentation retrieval behavior during tests.
func WithLookupDocumentationProvider(provider LookupServiceDocumentationProvider) TestOverlayEngineStubOption {
	return func(stub *TestOverlayEngineStub) {
		stub.lookupDocumentationProvider = provider
	}
}

// WithSyncAdvertisementsProvider allows setting a custom SyncAdvertisementsProvider in a TestOverlayEngineStub.
// This can be used to mock advertisement synchronization behavior during tests.
func WithSyncAdvertisementsProvider(provider SyncAdvertisementsProvider) TestOverlayEngineStubOption {
	return func(stub *TestOverlayEngineStub) {
		stub.syncAdvertisementsProvider = provider
	}
}

// WithStartGASPSyncProvider allows setting a custom StartGASPSyncProvider in a TestOverlayEngineStub.
// This can be used to mock GASP synchronization behavior during tests.
func WithStartGASPSyncProvider(provider StartGASPSyncProvider) TestOverlayEngineStubOption {
	return func(stub *TestOverlayEngineStub) {
		stub.startGASPSyncProvider = provider
	}
}

// WithTopicManagerDocumentationProvider allows setting a custom TopicManagerDocumentationProvider in a TestOverlayEngineStub.
// This can be used to mock topic manager documentation retrieval behavior during tests.
func WithTopicManagerDocumentationProvider(provider TopicManagerDocumentationProvider) TestOverlayEngineStubOption {
	return func(stub *TestOverlayEngineStub) {
		stub.topicManagerDocumentationProvider = provider
	}
}

// WithRequestForeignGASPNodeProvider allows setting a custom RequestForeignGASPNodeProvider in a TestOverlayEngineStub.
// This can be used to mock foreign GASP node request behavior during tests.
func WithRequestForeignGASPNodeProvider(provider RequestForeignGASPNodeProvider) TestOverlayEngineStubOption {
	return func(stub *TestOverlayEngineStub) {
		stub.requestForeignGASPNodeProvider = provider
	}
}

// WithRequestSyncResponseProvider allows setting a custom RequestSyncResponseProvider in a TestOverlayEngineStub.
// This can be used to mock sync response behavior during tests.
func WithRequestSyncResponseProvider(provider RequestSyncResponseProvider) TestOverlayEngineStubOption {
	return func(stub *TestOverlayEngineStub) {
		stub.requestSyncResponseProvider = provider
	}
}

// TestOverlayEngineStub is a test implementation of the engine.OverlayEngineProvider interface.
// It is used to mock engine behavior in unit tests, allowing the simulation of various engine actions
// like submitting transactions and synchronizing advertisements.
type TestOverlayEngineStub struct {
	t                                 *testing.T
	lookupQuestionProvider            LookupQuestionProvider
	lookupListProvider                LookupListProvider
	topicManagersListProvider         TopicManagersListProvider
	lookupDocumentationProvider       LookupServiceDocumentationProvider
	topicManagerDocumentationProvider TopicManagerDocumentationProvider
	startGASPSyncProvider             StartGASPSyncProvider
	submitTransactionProvider         SubmitTransactionProvider
	syncAdvertisementsProvider        SyncAdvertisementsProvider
	requestForeignGASPNodeProvider    RequestForeignGASPNodeProvider
	requestSyncResponseProvider       RequestSyncResponseProvider
	arcIngestProvider                 ARCIngestProvider
}

// GetDocumentationForLookupServiceProvider returns documentation for a lookup service provider
// using the configured LookupServiceDocumentationProvider.
func (s *TestOverlayEngineStub) GetDocumentationForLookupServiceProvider(provider string) (string, error) {
	s.t.Helper()
	return s.lookupDocumentationProvider.GetDocumentationForLookupServiceProvider(provider)
}

// GetDocumentationForTopicManager returns documentation for a topic manager.
// It delegates to the configured topic manager documentation provider.
func (s *TestOverlayEngineStub) GetDocumentationForTopicManager(provider string) (string, error) {
	s.t.Helper()
	return s.topicManagerDocumentationProvider.GetDocumentationForTopicManager(provider)
}

// GetUTXOHistory retrieves UTXO history for the given output (unimplemented).
// This is a placeholder function meant to be overridden in actual implementations.
func (s *TestOverlayEngineStub) GetUTXOHistory(_ context.Context, _ *engine.Output, _ func(beef []byte, outputIndex, currentDepth uint32) bool, _ uint32) (*engine.Output, error) {
	panic("unimplemented")
}

// HandleNewMerkleProof processes a new Merkle proof for a transaction (unimplemented).
// This is a placeholder function meant to be overridden in actual implementations.
func (s *TestOverlayEngineStub) HandleNewMerkleProof(ctx context.Context, txid *chainhash.Hash, proof *transaction.MerklePath) error {
	s.t.Helper()
	return s.arcIngestProvider.HandleNewMerkleProof(ctx, txid, proof)
}

// ListLookupServiceProviders lists the available lookup service providers.
func (s *TestOverlayEngineStub) ListLookupServiceProviders() map[string]*overlay.MetaData {
	s.t.Helper()
	return s.lookupListProvider.ListLookupServiceProviders()
}

// ListTopicManagers lists the available topic managers.
func (s *TestOverlayEngineStub) ListTopicManagers() map[string]*overlay.MetaData {
	s.t.Helper()
	return s.topicManagersListProvider.ListTopicManagers()
}

// Lookup performs a lookup query based on the provided LookupQuestion (unimplemented).
// This is a placeholder function meant to be overridden in actual implementations.
func (s *TestOverlayEngineStub) Lookup(ctx context.Context, question *lookup.LookupQuestion) (*lookup.LookupAnswer, error) {
	s.t.Helper()
	return s.lookupQuestionProvider.Lookup(ctx, question)
}

// ProvideForeignGASPNode returns a foreign GASP node using the configured RequestForeignGASPNodeProvider.
func (s *TestOverlayEngineStub) ProvideForeignGASPNode(ctx context.Context, graphID, outpoints *transaction.Outpoint, topic string) (*gasp.Node, error) {
	s.t.Helper()
	return s.requestForeignGASPNodeProvider.ProvideForeignGASPNode(ctx, graphID, outpoints, topic)
}

// ProvideForeignSyncResponse returns a foreign sync response.
// It calls the ProvideForeignSyncResponse method of the configured RequestSyncResponseProvider.
func (s *TestOverlayEngineStub) ProvideForeignSyncResponse(ctx context.Context, initialRequess *gasp.InitialRequest, topic string) (*gasp.InitialResponse, error) {
	s.t.Helper()
	return s.requestSyncResponseProvider.ProvideForeignSyncResponse(ctx, initialRequess, topic)
}

// StartGASPSync starts the GASP synchronization process.
// It calls the StartGASPSync method of the configured StartGASPSyncProvider.
func (s *TestOverlayEngineStub) StartGASPSync(ctx context.Context) error {
	s.t.Helper()
	return s.startGASPSyncProvider.StartGASPSync(ctx)
}

// Submit processes a transaction submission and returns a steak or error based on the provided inputs.
// It calls the Submit method of the configured SubmitTransactionProvider and handles the steak callback.
func (s *TestOverlayEngineStub) Submit(ctx context.Context, taggedBEEF overlay.TaggedBEEF, mode engine.SumbitMode, onSteakReady engine.OnSteakReady) (overlay.Steak, error) {
	s.t.Helper()
	return s.submitTransactionProvider.Submit(ctx, taggedBEEF, mode, onSteakReady)
}

// SyncAdvertisements synchronizes advertisements using the configured SyncAdvertisementsProvider.
// It calls the SyncAdvertisements method of the provider and handles the result.
func (s *TestOverlayEngineStub) SyncAdvertisements(ctx context.Context) error {
	s.t.Helper()
	return s.syncAdvertisementsProvider.SyncAdvertisements(ctx)
}

// AssertProvidersState asserts that all configured providers were used as expected.
func (s *TestOverlayEngineStub) AssertProvidersState() {
	s.t.Helper()

	providers := []ProviderStateAsserter{
		s.lookupQuestionProvider,
		s.topicManagerDocumentationProvider,
		s.submitTransactionProvider,
		s.lookupListProvider,
		s.topicManagersListProvider,
		s.lookupDocumentationProvider,
		s.syncAdvertisementsProvider,
		s.startGASPSyncProvider,
		s.requestForeignGASPNodeProvider,
		s.requestSyncResponseProvider,
		s.arcIngestProvider,
	}
	for _, p := range providers {
		p.AssertCalled()
	}
}

// NewTestOverlayEngineStub creates and returns a new instance of TestOverlayEngineStub with the provided options.
// The options allow for configuring custom providers for transaction submission and advertisement synchronization.
func NewTestOverlayEngineStub(t *testing.T, opts ...TestOverlayEngineStubOption) *TestOverlayEngineStub {
	stub := TestOverlayEngineStub{
		t:                                 t,
		lookupQuestionProvider:            NewLookupQuestionProviderMock(t, LookupQuestionProviderMockExpectations{LookupQuestionCall: false}),
		lookupDocumentationProvider:       NewLookupServiceDocumentationProviderMock(t, LookupServiceDocumentationProviderMockExpectations{DocumentationCall: false}),
		topicManagerDocumentationProvider: NewTopicManagerDocumentationProviderMock(t, TopicManagerDocumentationProviderMockExpectations{DocumentationCall: false}),
		topicManagersListProvider:         NewTopicManagersListProviderMock(t, TopicManagersListProviderMockExpectations{ListTopicManagersCall: false}),
		startGASPSyncProvider:             NewStartGASPSyncProviderMock(t, StartGASPSyncProviderMockExpectations{StartGASPSyncCall: false}),
		submitTransactionProvider:         NewSubmitTransactionProviderMock(t, SubmitTransactionProviderMockExpectations{SubmitCall: false}),
		lookupListProvider:                NewLookupListProviderMock(t, LookupListProviderMockExpectations{ListLookupServiceProvidersCall: false}),
		syncAdvertisementsProvider:        NewSyncAdvertisementsProviderMock(t, SyncAdvertisementsProviderMockExpectations{SyncAdvertisementsCall: false}),
		requestForeignGASPNodeProvider:    NewRequestForeignGASPNodeProviderMock(t, RequestForeignGASPNodeProviderMockExpectations{ProvideForeignGASPNodeCall: false}),
		requestSyncResponseProvider:       NewRequestSyncResponseProviderMock(t, RequestSyncResponseProviderMockExpectations{ProvideForeignSyncResponseCall: false}),
		arcIngestProvider:                 NewARCIngestProviderMock(t, ARCIngestProviderMockExpectations{HandleNewMerkleProofCall: false}),
	}

	for _, opt := range opts {
		opt(&stub)
	}
	return &stub
}

// ErrTestNoopOpFailure represents a test-specific error used to simulate a no-op operation failure.
var ErrTestNoopOpFailure = errors.New("noop test error")
