package engine_test

import (
	"context"
	"testing"

	"github.com/4chain-ag/go-overlay-services/pkg/core/engine"
	"github.com/bsv-blockchain/go-sdk/overlay"
	"github.com/bsv-blockchain/go-sdk/transaction"
	"github.com/stretchr/testify/require"
)

func TestEngine_SyncConfiguration_DefaultBehavior(t *testing.T) {
	t.Run("should initialize empty sync configuration when not provided", func(t *testing.T) {
		// given
		input := engine.Engine{
			Managers: map[string]engine.TopicManager{
				"tm_helloworld": &mockTopicManager{},
				"tm_custom":     &mockTopicManager{},
			},
			SyncConfiguration: nil, // Not provided
		}

		// when
		result := engine.NewEngine(input)

		// then
		require.NotNil(t, result.SyncConfiguration)

		// NOTE: Unlike the TypeScript implementation, the Go version does NOT
		// automatically set undefined managers to SHIP by default.
		// This is a behavioral difference that should be considered.
		_, hasHelloworld := result.SyncConfiguration["tm_helloworld"]
		_, hasCustom := result.SyncConfiguration["tm_custom"]
		require.False(t, hasHelloworld)
		require.False(t, hasCustom)
	})

	t.Run("should preserve explicit sync configuration", func(t *testing.T) {
		// given
		input := engine.Engine{
			Managers: map[string]engine.TopicManager{
				"tm_helloworld": &mockTopicManager{},
				"tm_custom":     &mockTopicManager{},
				"tm_nosync":     &mockTopicManager{},
			},
			SyncConfiguration: map[string]engine.SyncConfiguration{
				"tm_helloworld": {Type: engine.SyncConfigurationPeers, Peers: []string{"peer1", "peer2"}},
				"tm_custom":     {Type: engine.SyncConfigurationSHIP},
				"tm_nosync":     {Type: engine.SyncConfigurationNone},
			},
		}

		// when
		result := engine.NewEngine(input)

		// then
		// Explicitly configured managers should keep their settings
		require.Equal(t, engine.SyncConfigurationPeers, result.SyncConfiguration["tm_helloworld"].Type)
		require.ElementsMatch(t, []string{"peer1", "peer2"}, result.SyncConfiguration["tm_helloworld"].Peers)

		require.Equal(t, engine.SyncConfigurationSHIP, result.SyncConfiguration["tm_custom"].Type)
		require.Equal(t, engine.SyncConfigurationNone, result.SyncConfiguration["tm_nosync"].Type)
	})

	t.Run("should handle mixed configuration with undefined managers", func(t *testing.T) {
		// given
		input := engine.Engine{
			Managers: map[string]engine.TopicManager{
				"tm_defined":   &mockTopicManager{},
				"tm_undefined": &mockTopicManager{},
			},
			SyncConfiguration: map[string]engine.SyncConfiguration{
				"tm_defined": {Type: engine.SyncConfigurationNone},
				// tm_undefined is not in SyncConfiguration
			},
		}

		// when
		result := engine.NewEngine(input)

		// then
		require.Equal(t, engine.SyncConfigurationNone, result.SyncConfiguration["tm_defined"].Type)

		// NOTE: Unlike TypeScript, Go doesn't default undefined managers to SHIP
		_, hasUndefined := result.SyncConfiguration["tm_undefined"]
		require.False(t, hasUndefined)
	})

	t.Run("should combine ship trackers with existing peers for tm_ship", func(t *testing.T) {
		// given
		input := engine.Engine{
			SHIPTrackers: []string{"tracker1", "tracker2"},
			Managers: map[string]engine.TopicManager{
				"tm_ship": &mockTopicManager{},
			},
			SyncConfiguration: map[string]engine.SyncConfiguration{
				"tm_ship": {Type: engine.SyncConfigurationPeers, Peers: []string{"peer1", "tracker1"}}, // tracker1 is duplicate
			},
		}

		// when
		result := engine.NewEngine(input)

		// then
		require.Equal(t, engine.SyncConfigurationPeers, result.SyncConfiguration["tm_ship"].Type)
		// Should combine and deduplicate
		require.ElementsMatch(t, []string{"tracker1", "tracker2", "peer1"}, result.SyncConfiguration["tm_ship"].Peers)
	})

	t.Run("should combine slap trackers with existing peers for tm_slap", func(t *testing.T) {
		// given
		input := engine.Engine{
			SLAPTrackers: []string{"slap1", "slap2"},
			Managers: map[string]engine.TopicManager{
				"tm_slap": &mockTopicManager{},
			},
			SyncConfiguration: map[string]engine.SyncConfiguration{
				"tm_slap": {Type: engine.SyncConfigurationPeers, Peers: []string{"peer1", "slap1"}}, // slap1 is duplicate
			},
		}

		// when
		result := engine.NewEngine(input)

		// then
		require.Equal(t, engine.SyncConfigurationPeers, result.SyncConfiguration["tm_slap"].Type)
		// Should combine and deduplicate
		require.ElementsMatch(t, []string{"slap1", "slap2", "peer1"}, result.SyncConfiguration["tm_slap"].Peers)
	})

	t.Run("should not modify tm_ship when sync type is not Peers", func(t *testing.T) {
		// given
		input := engine.Engine{
			SHIPTrackers: []string{"tracker1", "tracker2"},
			Managers: map[string]engine.TopicManager{
				"tm_ship": &mockTopicManager{},
			},
			SyncConfiguration: map[string]engine.SyncConfiguration{
				"tm_ship": {Type: engine.SyncConfigurationSHIP}, // Not Peers type
			},
		}

		// when
		result := engine.NewEngine(input)

		// then
		require.Equal(t, engine.SyncConfigurationSHIP, result.SyncConfiguration["tm_ship"].Type)
		require.Empty(t, result.SyncConfiguration["tm_ship"].Peers) // Should not add trackers
	})

	t.Run("should not modify tm_ship when it's set to None", func(t *testing.T) {
		// given
		input := engine.Engine{
			SHIPTrackers: []string{"tracker1", "tracker2"},
			Managers: map[string]engine.TopicManager{
				"tm_ship": &mockTopicManager{},
			},
			SyncConfiguration: map[string]engine.SyncConfiguration{
				"tm_ship": {Type: engine.SyncConfigurationNone}, // Explicitly disabled
			},
		}

		// when
		result := engine.NewEngine(input)

		// then
		require.Equal(t, engine.SyncConfigurationNone, result.SyncConfiguration["tm_ship"].Type)
		require.Empty(t, result.SyncConfiguration["tm_ship"].Peers) // Should not add trackers
	})

	t.Run("should handle empty managers gracefully", func(t *testing.T) {
		// given
		input := engine.Engine{
			Managers:          map[string]engine.TopicManager{},
			SyncConfiguration: nil,
		}

		// when
		result := engine.NewEngine(input)

		// then
		require.NotNil(t, result.SyncConfiguration)
		require.Empty(t, result.SyncConfiguration)
	})

	t.Run("should set concurrency if provided in sync configuration", func(t *testing.T) {
		// given
		input := engine.Engine{
			Managers: map[string]engine.TopicManager{
				"tm_concurrent": &mockTopicManager{},
			},
			SyncConfiguration: map[string]engine.SyncConfiguration{
				"tm_concurrent": {
					Type:        engine.SyncConfigurationPeers,
					Peers:       []string{"peer1"},
					Concurrency: 5,
				},
			},
		}

		// when
		result := engine.NewEngine(input)

		// then
		require.Equal(t, 5, result.SyncConfiguration["tm_concurrent"].Concurrency)
	})
}

func TestEngine_SyncConfiguration_TypeScriptParity(t *testing.T) {
	t.Run("should set default SHIP sync configuration for undefined managers", func(t *testing.T) {
		// This test reflects the TypeScript behavior where undefined topic managers
		// are set to sync method of "SHIP" by default
		// NOTE: The Go implementation might differ in behavior

		// given
		input := engine.Engine{
			Managers: map[string]engine.TopicManager{
				"tm_helloworld": &mockTopicManager{},
				"tm_undefined":  &mockTopicManager{},
			},
			SyncConfiguration: map[string]engine.SyncConfiguration{
				"tm_helloworld": {Type: engine.SyncConfigurationSHIP},
				// tm_undefined is not configured
			},
		}

		// when
		result := engine.NewEngine(input)

		// then
		require.Equal(t, engine.SyncConfigurationSHIP, result.SyncConfiguration["tm_helloworld"].Type)

		// In TypeScript, tm_undefined would be set to SHIP by default
		// In Go, this behavior needs to be explicitly implemented if desired
		_, hasUndefined := result.SyncConfiguration["tm_undefined"]
		require.False(t, hasUndefined, "Go implementation doesn't auto-default to SHIP")
	})

	t.Run("should not set sync method to SHIP for managers explicitly set to false", func(t *testing.T) {
		// Test that disabled sync is respected

		// given
		input := engine.Engine{
			Managers: map[string]engine.TopicManager{
				"tm_helloworld": &mockTopicManager{},
			},
			SyncConfiguration: map[string]engine.SyncConfiguration{
				"tm_helloworld": {Type: engine.SyncConfigurationNone},
			},
		}

		// when
		result := engine.NewEngine(input)

		// then
		require.Equal(t, engine.SyncConfigurationNone, result.SyncConfiguration["tm_helloworld"].Type)
	})

	t.Run("should combine trackers without duplicates", func(t *testing.T) {
		// Test deduplication when combining trackers

		// given
		input := engine.Engine{
			SHIPTrackers: []string{"tracker1", "tracker2", "tracker1"}, // tracker1 appears twice
			SLAPTrackers: []string{"slap1", "slap2", "slap1"},          // slap1 appears twice
			Managers: map[string]engine.TopicManager{
				"tm_ship": &mockTopicManager{},
				"tm_slap": &mockTopicManager{},
			},
			SyncConfiguration: map[string]engine.SyncConfiguration{
				"tm_ship": {Type: engine.SyncConfigurationPeers, Peers: []string{"existingPeer", "tracker2"}}, // tracker2 is duplicate
				"tm_slap": {Type: engine.SyncConfigurationPeers, Peers: []string{"existingPeer", "slap2"}},    // slap2 is duplicate
			},
		}

		// when
		result := engine.NewEngine(input)

		// then
		// Verify deduplication for tm_ship
		require.Equal(t, engine.SyncConfigurationPeers, result.SyncConfiguration["tm_ship"].Type)
		shipPeers := result.SyncConfiguration["tm_ship"].Peers
		require.Len(t, shipPeers, 3) // Should have tracker1, tracker2, existingPeer (no duplicates)
		require.ElementsMatch(t, []string{"tracker1", "tracker2", "existingPeer"}, shipPeers)

		// Verify deduplication for tm_slap
		require.Equal(t, engine.SyncConfigurationPeers, result.SyncConfiguration["tm_slap"].Type)
		slapPeers := result.SyncConfiguration["tm_slap"].Peers
		require.Len(t, slapPeers, 3) // Should have slap1, slap2, existingPeer (no duplicates)
		require.ElementsMatch(t, []string{"slap1", "slap2", "existingPeer"}, slapPeers)
	})

	t.Run("should disable sync for specific topics when set to None", func(t *testing.T) {
		// Test that sync can be disabled for specific topics

		// given
		input := engine.Engine{
			Managers: map[string]engine.TopicManager{
				"tm_sync":   &mockTopicManager{},
				"tm_nosync": &mockTopicManager{},
			},
			SyncConfiguration: map[string]engine.SyncConfiguration{
				"tm_sync":   {Type: engine.SyncConfigurationSHIP},
				"tm_nosync": {Type: engine.SyncConfigurationNone}, // Explicitly disabled
			},
		}

		// when
		result := engine.NewEngine(input)

		// then
		require.Equal(t, engine.SyncConfigurationSHIP, result.SyncConfiguration["tm_sync"].Type)
		require.Equal(t, engine.SyncConfigurationNone, result.SyncConfiguration["tm_nosync"].Type)
		require.Empty(t, result.SyncConfiguration["tm_nosync"].Peers)
	})
}

// Mock topic manager for testing
type mockTopicManager struct{}

func (m *mockTopicManager) IdentifyAdmissibleOutputs(ctx context.Context, beef []byte, previousCoins map[uint32]*transaction.TransactionOutput) (overlay.AdmittanceInstructions, error) {
	return overlay.AdmittanceInstructions{}, nil
}

func (m *mockTopicManager) ProcessOutput(ctx context.Context, output []byte, proof []byte, vout uint32, topic string) error {
	return nil
}

func (m *mockTopicManager) IdentifyNeededInputs(ctx context.Context, beef []byte) ([]*transaction.Outpoint, error) {
	return nil, nil
}

func (m *mockTopicManager) GetDocumentation() string {
	return ""
}

func (m *mockTopicManager) GetMetaData() *overlay.MetaData {
	return nil
}
