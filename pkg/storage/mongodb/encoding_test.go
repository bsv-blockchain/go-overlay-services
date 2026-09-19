package mongodb

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/engine"
)

func TestUint64EncodingUsesSharedS01Vectors(t *testing.T) {
	data, err := os.ReadFile("../../core/engine/testdata/persistence-v1.json")
	require.NoError(t, err)
	var fixture struct {
		Integers []struct {
			Value engine.StorageUint64 `json:"value"`
			Valid bool                 `json:"valid"`
		} `json:"uint64"`
	}
	require.NoError(t, json.Unmarshal(data, &fixture))
	require.NotEmpty(t, fixture.Integers)
	for _, vector := range fixture.Integers {
		t.Run(string(vector.Value), func(t *testing.T) {
			encoded, encodeErr := EncodeUint64(vector.Value)
			if !vector.Valid {
				require.Error(t, encodeErr)
				return
			}
			require.NoError(t, encodeErr)
			document, marshalErr := bson.Marshal(bson.D{{Key: "value", Value: encoded}})
			require.NoError(t, marshalErr)
			raw := bson.Raw(document).Lookup("value")
			require.Equal(t, bson.TypeString, raw.Type)
			decoded, decodeErr := DecodeUint64(raw.StringValue())
			require.NoError(t, decodeErr)
			require.Equal(t, vector.Value, decoded)
		})
	}
	for _, invalid := range []string{"99999999999999999999", "18446744073709551616", "0000000000000000000x", "1"} {
		_, err = DecodeUint64(invalid)
		require.Error(t, err)
	}
	values := []engine.StorageUint64{"0", "1", "9007199254740993", "9223372036854775808", "18446744073709551615"}
	previous := ""
	for _, value := range values {
		encoded, encodeErr := EncodeUint64(value)
		require.NoError(t, encodeErr)
		require.Greater(t, encoded, previous)
		previous = encoded
	}
	require.NotEqual(t, tupleID("a:b", "c"), tupleID("a", "b:c"))
	require.NotEqual(t, tupleID("𐀀", "x"), tupleID("𐀀x"))
}

func TestStoreRejectsUnboundedConfigurationWithoutConnection(t *testing.T) {
	config := Config{Database: "overlay", Scope: engine.StorageScope{Network: "test", GenesisHash: "0000000000000000000000000000000000000000000000000000000000000000", NodeID: "node"}}
	_, err := normalizeConfig(config)
	require.NoError(t, err)
	_, err = New(t.Context(), nil, config)
	require.ErrorIs(t, err, ErrInvalidConfig)
	for _, database := range []string{"", "has.dot", "admin.$cmd", "name\x00suffix", "bad/name"} {
		copyConfig := config
		copyConfig.Database = database
		_, err = normalizeConfig(copyConfig)
		require.ErrorIs(t, err, ErrInvalidConfig)
	}
	config.MaxPayloadBytes = 1<<40 + 1
	_, err = normalizeConfig(config)
	require.ErrorIs(t, err, ErrInvalidConfig)
}
