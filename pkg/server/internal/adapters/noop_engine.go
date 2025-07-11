package adapters

import (
	"context"

	"github.com/4chain-ag/go-overlay-services/pkg/core/engine"
	"github.com/4chain-ag/go-overlay-services/pkg/core/gasp/core"
	"github.com/bsv-blockchain/go-sdk/chainhash"
	"github.com/bsv-blockchain/go-sdk/overlay"
	"github.com/bsv-blockchain/go-sdk/overlay/lookup"
	"github.com/bsv-blockchain/go-sdk/transaction"
)

// NoopEngineProvider is a custom test overlay engine implementation. This is only a temporary solution and will be removed
// after migrating the engine code. Currently, it functions as mock for the overlay HTTP server.
type NoopEngineProvider struct{}

// HandleNewMerkleProof implements engine.OverlayEngineProvider.
func (n *NoopEngineProvider) HandleNewMerkleProof(ctx context.Context, txid *chainhash.Hash, proof *transaction.MerklePath) error {
	panic("unimplemented")
}

// Submit is a no-op call that always returns an empty STEAK with nil error.
func (*NoopEngineProvider) Submit(ctx context.Context, taggedBEEF overlay.TaggedBEEF, mode engine.SumbitMode, onSteakReady engine.OnSteakReady) (overlay.Steak, error) {
	hex1, _ := chainhash.NewHashFromHex("03895fb984362a4196bc9931629318fcbb2aeba7c6293638119ea653fa31d119")
	hex2, _ := chainhash.NewHashFromHex("03815fb984362a4196bc9931629318fcbb2aeba7c6293638119ea653fa31d119")

	onSteakReady(&overlay.Steak{
		"noop_engine_provider": &overlay.AdmittanceInstructions{
			AncillaryTxids: []*chainhash.Hash{
				hex1, hex2,
			},
			OutputsToAdmit: []uint32{1000},
			CoinsToRetain:  []uint32{1000},
			CoinsRemoved:   []uint32{1000},
		}})
	return overlay.Steak{}, nil
}

// SyncAdvertisements is a no-op call that always returns a nil error.
func (*NoopEngineProvider) SyncAdvertisements(ctx context.Context) error { return nil }

// GetTopicManagerDocumentation is a no-op call that always returns a nil error.
func (*NoopEngineProvider) GetTopicManagerDocumentation(ctx context.Context) error { return nil }

// Lookup is a no-op call that always returns an empty lookup answer with nil error.
func (*NoopEngineProvider) Lookup(ctx context.Context, question *lookup.LookupQuestion) (*lookup.LookupAnswer, error) {
	return &lookup.LookupAnswer{
		Type: "noop_engine_provider",
		Outputs: []*lookup.OutputListItem{
			{
				Beef:        []byte{},
				OutputIndex: 0,
			},
		},
		Formulas: []lookup.LookupFormula{
			{
				Outpoint: &overlay.Outpoint{
					Txid:        chainhash.Hash{},
					OutputIndex: 0,
				},
			},
		},
		Result: nil,
	}, nil
}

// GetUTXOHistory is a no-op call that always returns an empty engine output with nil error.
func (*NoopEngineProvider) GetUTXOHistory(ctx context.Context, output *engine.Output, historySelector func(beef []byte, outputIndex uint32, currentDepth uint32) bool, currentDepth uint32) (*engine.Output, error) {
	return &engine.Output{}, nil
}

// StartGASPSync is a no-op call that always returns a nil error.
func (*NoopEngineProvider) StartGASPSync(ctx context.Context) error { return nil }

// ProvideForeignSyncResponse is a no-op call that always returns an empty initial GASP response with nil error.
func (*NoopEngineProvider) ProvideForeignSyncResponse(ctx context.Context, initialRequest *core.GASPInitialRequest, topic string) (*core.GASPInitialResponse, error) {
	return &core.GASPInitialResponse{
		UTXOList: []*overlay.Outpoint{
			{},
			{},
		},
		Since: 0,
	}, nil
}

// ProvideForeignGASPNode is a no-op call that always returns an empty GASP node with nil error.
func (*NoopEngineProvider) ProvideForeignGASPNode(ctx context.Context, graphId, outpoint *overlay.Outpoint, topic string) (*core.GASPNode, error) {
	return &core.GASPNode{}, nil
}

// ListTopicManagers is a no-op call that always returns an empty topic managers map with nil error.
func (*NoopEngineProvider) ListTopicManagers() map[string]*overlay.MetaData {
	return map[string]*overlay.MetaData{
		"noop_engine_topic_manager_1": {
			Name:        "example_name_1",
			Description: "example_desc_1",
			Icon:        "example_icon_1",
			Version:     "0.0.0",
			InfoUrl:     "example_info",
		},
		"noop_engine_topic_manager_2": {
			Name:        "example_name_2",
			Description: "example_desc_2",
			Icon:        "example_icon_2",
			Version:     "0.0.0",
			InfoUrl:     "example_info",
		},
	}
}

// ListLookupServiceProviders is a no-op call that always returns an empty lookup service providers map with nil error.
func (*NoopEngineProvider) ListLookupServiceProviders() map[string]*overlay.MetaData {
	return map[string]*overlay.MetaData{
		"noop_engine_lookup_service_provider_1": {
			Name:        "example_name_1",
			Description: "example_desc_1",
			Icon:        "example_icon_1",
			Version:     "0.0.0",
			InfoUrl:     "example_info",
		},
		"noop_engine_lookup_service_provider_2": {
			Name:        "example_name_2",
			Description: "example_desc_2",
			Icon:        "example_icon_2",
			Version:     "0.0.0",
			InfoUrl:     "example_info",
		},
	}
}

// GetDocumentationForLookupServiceProvider is a no-op call that always returns an empty string with nil error.
func (*NoopEngineProvider) GetDocumentationForLookupServiceProvider(provider string) (string, error) {
	return "noop_engine_lookuo_service_provider_doc", nil
}

// GetDocumentationForTopicManager is a no-op call that always returns an empty string with nil error.
func (*NoopEngineProvider) GetDocumentationForTopicManager(provider string) (string, error) {
	return "noop_engine_topic_manager_doc", nil
}

// NewNoopEngineProvider returns an OverlayEngineProvider implementation
// and checks whether the engine contract matches the implemented method set.
func NewNoopEngineProvider() engine.OverlayEngineProvider {
	return &NoopEngineProvider{}
}
