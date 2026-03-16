package engine_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/engine"
)

func TestEngine_NewEngine_ShouldInitializeFields_WhenNilProvided(t *testing.T) {
	// given:
	input := &engine.Config{}

	// when:
	actual := engine.NewEngine(input)

	// then:
	require.NotNil(t, actual)
	require.NotNil(t, actual.SyncConfiguration)
	require.NotNil(t, actual.LookupResolver)
	// Managers and LookupServices are now private; verify via ListTopicManagers/ListLookupServiceProviders
	require.Empty(t, actual.ListTopicManagers())
	require.Empty(t, actual.ListLookupServiceProviders())
}

func TestEngine_NewEngine_ShouldMergeTrackers_WhenManagerIsShipType(t *testing.T) {
	// given:
	input := &engine.Config{
		SHIPTrackers: []string{"http://tracker1.com"},
		Managers: map[string]engine.TopicManager{
			"tm_ship": fakeTopicManager{},
		},
		SyncConfiguration: map[string]engine.SyncConfiguration{
			"tm_ship": {Type: engine.SyncConfigurationPeers, Peers: []string{"http://peer1.com"}},
		},
	}

	expectedPeers := []string{"http://tracker1.com", "http://peer1.com"}

	// when:
	actual := engine.NewEngine(input)

	// then:
	require.NotNil(t, actual)
	require.Equal(t, input.SHIPTrackers, actual.SHIPTrackers)

	// Verify the topic manager was registered
	managers := actual.ListTopicManagers()
	require.Len(t, managers, 1)
	require.Contains(t, managers, "tm_ship")

	// Verify lookup services are empty
	require.Empty(t, actual.ListLookupServiceProviders())

	require.ElementsMatch(t,
		expectedPeers,
		actual.SyncConfiguration["tm_ship"].Peers,
	)

	require.Equal(t,
		engine.SyncConfigurationPeers,
		actual.SyncConfiguration["tm_ship"].Type,
	)
}

func TestEngine_NewEngine_ShouldMergeTrackers_WhenManagerIsSlapType(t *testing.T) {
	// given:
	input := &engine.Config{
		SLAPTrackers: []string{"http://slaptracker.com"},
		Managers: map[string]engine.TopicManager{
			"tm_slap": fakeTopicManager{},
		},
		SyncConfiguration: map[string]engine.SyncConfiguration{
			"tm_slap": {Type: engine.SyncConfigurationPeers, Peers: []string{"http://peer2.com"}},
		},
	}

	// when:
	result := engine.NewEngine(input)

	// then:
	require.NotNil(t, result)

	expectedPeers := []string{"http://slaptracker.com", "http://peer2.com"}
	require.ElementsMatch(t, result.SyncConfiguration["tm_slap"].Peers, expectedPeers)
}

func TestEngine_NewEngine_ShouldNotMergeTrackers_WhenTypeIsNotPeers(t *testing.T) {
	// given:
	input := &engine.Config{
		SHIPTrackers: []string{"http://tracker-should-not-merge.com"},
		Managers: map[string]engine.TopicManager{
			"tm_ship": fakeTopicManager{},
		},
		SyncConfiguration: map[string]engine.SyncConfiguration{
			"tm_ship": {Type: engine.SyncConfigurationSHIP, Peers: []string{"http://peer1.com"}},
		},
	}

	// when:
	result := engine.NewEngine(input)

	// then:
	require.NotNil(t, result)

	expectedPeers := []string{"http://peer1.com"}
	require.ElementsMatch(t, result.SyncConfiguration["tm_ship"].Peers, expectedPeers, "Trackers should not be merged if type != Peers")
}
