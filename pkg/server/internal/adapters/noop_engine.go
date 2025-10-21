// Package adapters provides adapter implementations for interfacing with external services and systems.
package adapters

import (
	"context"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/engine"
	"github.com/bsv-blockchain/go-overlay-services/pkg/core/gasp"
	"github.com/bsv-blockchain/go-sdk/chainhash"
	"github.com/bsv-blockchain/go-sdk/overlay"
	"github.com/bsv-blockchain/go-sdk/overlay/lookup"
	"github.com/bsv-blockchain/go-sdk/transaction"
)

// NoopEngineProvider is a custom test overlay engine implementation. This is only a temporary solution and will be removed
// after migrating the engine code. Currently, it functions as mock for the overlay HTTP server.
type NoopEngineProvider struct{}

// HandleNewMerkleProof implements engine.OverlayEngineProvider.
func (n *NoopEngineProvider) HandleNewMerkleProof(_ context.Context, _ *chainhash.Hash, _ *transaction.MerklePath) error {
	panic("unimplemented")
}

// Submit is a no-op call that always returns an empty STEAK with nil error.
func (*NoopEngineProvider) Submit(_ context.Context, _ overlay.TaggedBEEF, _ engine.SumbitMode, onSteakReady engine.OnSteakReady) (overlay.Steak, error) {
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
		},
	})
	return overlay.Steak{}, nil
}

// SyncAdvertisements is a no-op call that always returns a nil error.
func (*NoopEngineProvider) SyncAdvertisements(_ context.Context) error { return nil }

// GetTopicManagerDocumentation is a no-op call that always returns a nil error.
func (*NoopEngineProvider) GetTopicManagerDocumentation(_ context.Context) error { return nil }

// Lookup is a no-op call that always returns an empty lookup answer with nil error.
func (*NoopEngineProvider) Lookup(_ context.Context, _ *lookup.LookupQuestion) (*lookup.LookupAnswer, error) {
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
				Outpoint: &transaction.Outpoint{
					Txid:  chainhash.Hash{},
					Index: 0,
				},
			},
		},
		Result: nil,
	}, nil
}

// GetUTXOHistory is a no-op call that always returns an empty engine output with nil error.
func (*NoopEngineProvider) GetUTXOHistory(_ context.Context, _ *engine.Output, _ func(beef []byte, outputIndex, currentDepth uint32) bool, _ uint32) (*engine.Output, error) {
	return &engine.Output{}, nil
}

// StartGASPSync is a no-op call that always returns a nil error.
func (*NoopEngineProvider) StartGASPSync(_ context.Context) error { return nil }

// ProvideForeignSyncResponse is a no-op call that always returns an empty initial GASP response with nil error.
func (*NoopEngineProvider) ProvideForeignSyncResponse(_ context.Context, _ *gasp.InitialRequest, _ string) (*gasp.InitialResponse, error) {
	return &gasp.InitialResponse{
		UTXOList: []*gasp.Output{},
		Since:    0,
	}, nil
}

// ProvideForeignGASPNode is a no-op call that always returns an empty GASP node with nil error.
func (*NoopEngineProvider) ProvideForeignGASPNode(_ context.Context, _, _ *transaction.Outpoint, _ string) (*gasp.Node, error) {
	return &gasp.Node{}, nil
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
func (*NoopEngineProvider) GetDocumentationForLookupServiceProvider(_ string) (string, error) {
	return "noop_engine_lookuo_service_provider_doc", nil
}

// GetDocumentationForTopicManager is a no-op call that always returns an empty string with nil error.
func (*NoopEngineProvider) GetDocumentationForTopicManager(_ string) (string, error) {
	return "noop_engine_topic_manager_doc", nil
}

// NewNoopEngineProvider returns an OverlayEngineProvider implementation
// and checks whether the engine contract matches the implemented method set.
func NewNoopEngineProvider() engine.OverlayEngineProvider {
	return &NoopEngineProvider{}
}
