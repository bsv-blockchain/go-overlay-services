package engine_test

import (
	"testing"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/engine"
	"github.com/stretchr/testify/require"
)

func TestEngine_NewEngine_ShouldInitializeFields_WhenNilProvided(t *testing.T) {
	// given:
	input := engine.Engine{}

	expected := &engine.Engine{
		Managers:          map[string]engine.TopicManager{},
		LookupServices:    map[string]engine.LookupService{},
		SyncConfiguration: map[string]engine.SyncConfiguration{},
		LookupResolver:    engine.NewLookupResolver(),
	}

	// when:
	actual := engine.NewEngine(input)

	// then:
	require.NotNil(t, actual)
	require.Equal(t, expected, actual)
}

func TestEngine_NewEngine_ShouldMergeTrackers_WhenManagerIsShipType(t *testing.T) {
	// given:
	input := engine.Engine{
		SHIPTrackers: []string{"http://tracker1.com"},
		Managers: map[string]engine.TopicManager{
			"tm_ship": fakeTopicManager{},
		},
		SyncConfiguration: map[string]engine.SyncConfiguration{
			"tm_ship": {Type: engine.SyncConfigurationPeers, Peers: []string{"http://peer1.com"}},
		},
	}

	expected := &engine.Engine{
		Managers: map[string]engine.TopicManager{
			"tm_ship": fakeTopicManager{},
		},
		LookupServices: map[string]engine.LookupService{},
		SyncConfiguration: map[string]engine.SyncConfiguration{
			"tm_ship": {
				Type:  engine.SyncConfigurationPeers,
				Peers: []string{"http://tracker1.com", "http://peer1.com"},
			},
		},
		SHIPTrackers: []string{"http://tracker1.com"},
	}

	// when:
	actual := engine.NewEngine(input)

	// then:
	require.NotNil(t, actual)
	require.Equal(t, expected.SHIPTrackers, actual.SHIPTrackers)
	require.Equal(t, expected.Managers, actual.Managers)
	require.Equal(t, expected.LookupServices, actual.LookupServices)

	require.ElementsMatch(t,
		expected.SyncConfiguration["tm_ship"].Peers,
		actual.SyncConfiguration["tm_ship"].Peers,
	)

	require.Equal(t,
		expected.SyncConfiguration["tm_ship"].Type,
		actual.SyncConfiguration["tm_ship"].Type,
	)
}

func TestEngine_NewEngine_ShouldMergeTrackers_WhenManagerIsSlapType(t *testing.T) {
	// given:
	input := engine.Engine{
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
	input := engine.Engine{
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
