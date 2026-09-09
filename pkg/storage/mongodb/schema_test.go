package mongodb

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/engine"
	"github.com/bsv-blockchain/go-overlay-services/pkg/storage/mongodb/internal/mongotest"
)

func TestSchemaBootstrap(t *testing.T) {
	replica := mongotest.New(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	t.Run("idempotent and concurrent scopes", func(t *testing.T) {
		database := schemaTestDatabase("concurrent")
		first := schemaTestConfig(database, "same")
		second := schemaTestConfig(database, "other")
		configs := []Config{first, first, second, second}
		start := make(chan struct{})
		errs := make(chan error, len(configs))
		var group sync.WaitGroup
		for _, config := range configs {
			group.Add(1)
			go func(config Config) {
				defer group.Done()
				<-start
				_, err := New(ctx, replica.Client, config)
				errs <- err
			}(config)
		}
		close(start)
		group.Wait()
		close(errs)
		for err := range errs {
			require.NoError(t, err)
		}

		firstStore, err := New(ctx, replica.Client, first)
		require.NoError(t, err)
		secondStore, err := New(ctx, replica.Client, second)
		require.NoError(t, err)
		require.NotEqual(t, firstStore.scopeID, secondStore.scopeID)
		require.Equal(t, int64(2), schemaLedgerCount(ctx, t, firstStore.db))

		_, err = firstStore.db.Collection(payloadCollection).InsertOne(ctx, bson.D{{Key: fieldID, Value: "not-a-payload"}})
		require.Error(t, err)
	})

	t.Run("incompatible ledger is preserved", func(t *testing.T) {
		database := schemaTestDatabase("ledger")
		config := schemaTestConfig(database, "ledger")
		db := replica.Client.Database(database)
		require.NoError(t, createCompatibleCollection(ctx, db, schemaCollection, schemaValidator()))
		store := &Store{config: config, scopeID: schemaScopeID(config)}
		ledger := schemaLedger(store)
		for index := range ledger {
			if ledger[index].Key == "schemaVersion" {
				ledger[index].Value = schemaVersion + 1
			}
		}
		_, err := db.Collection(schemaCollection).InsertOne(ctx, ledger, options.InsertOne().SetBypassDocumentValidation(true))
		require.NoError(t, err)
		before := schemaCollectionRaw(ctx, t, db, schemaCollection)

		_, err = New(ctx, replica.Client, config)
		require.ErrorIs(t, err, ErrIncompatibleSchema)
		require.Equal(t, before, schemaCollectionRaw(ctx, t, db, schemaCollection))
		require.Equal(t, int64(1), schemaLedgerCount(ctx, t, db))
	})

	for _, scenario := range []struct {
		name    string
		prepare func(context.Context, *mongo.Database) error
	}{
		{
			name: "wrong collection collation",
			prepare: func(ctx context.Context, db *mongo.Database) error {
				return db.CreateCollection(ctx, payloadCollection, options.CreateCollection().SetCollation(&options.Collation{Locale: "en"}).SetValidator(schemaPayloadValidator()).SetValidationLevel("strict").SetValidationAction("error"))
			},
		},
		{
			name: "wrong collection validator",
			prepare: func(ctx context.Context, db *mongo.Database) error {
				return db.CreateCollection(ctx, payloadCollection, options.CreateCollection().SetCollation(&options.Collation{Locale: "simple"}).SetValidator(bson.D{{Key: "$jsonSchema", Value: bson.D{{Key: "bsonType", Value: "object"}}}}).SetValidationLevel("strict").SetValidationAction("error"))
			},
		},
		{
			name: "view in place of collection",
			prepare: func(ctx context.Context, db *mongo.Database) error {
				if err := db.CreateCollection(ctx, "payload_source"); err != nil {
					return err
				}
				return db.CreateView(ctx, payloadCollection, "payload_source", mongo.Pipeline{})
			},
		},
		{
			name: "capped collection",
			prepare: func(ctx context.Context, db *mongo.Database) error {
				return db.CreateCollection(ctx, payloadCollection, options.CreateCollection().SetCapped(true).SetSizeInBytes(4096).SetCollation(&options.Collation{Locale: "simple"}).SetValidator(schemaPayloadValidator()).SetValidationLevel("strict").SetValidationAction("error"))
			},
		},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			database := schemaTestDatabase("collection")
			db := replica.Client.Database(database)
			require.NoError(t, scenario.prepare(ctx, db))
			before := schemaCollectionRaw(ctx, t, db, payloadCollection)

			_, err := New(ctx, replica.Client, schemaTestConfig(database, "collection"))
			require.ErrorIs(t, err, ErrIncompatibleSchema)
			require.Equal(t, before, schemaCollectionRaw(ctx, t, db, payloadCollection))
			require.Zero(t, schemaLedgerCount(ctx, t, db))
		})
	}

	for _, scenario := range []struct {
		name  string
		model mongo.IndexModel
	}{
		{
			name:  "wrong index key order",
			model: mongo.IndexModel{Keys: bson.D{{Key: fieldState, Value: 1}, {Key: fieldChain, Value: 1}, {Key: fieldUpdatedAt, Value: 1}}, Options: options.Index().SetName("chain_state_updated")},
		},
		{
			name:  "wrong index unique",
			model: mongo.IndexModel{Keys: bson.D{{Key: fieldChain, Value: 1}, {Key: fieldState, Value: 1}, {Key: fieldUpdatedAt, Value: 1}}, Options: options.Index().SetName("chain_state_updated").SetUnique(true)},
		},
		{
			name:  "wrong index sparse",
			model: mongo.IndexModel{Keys: bson.D{{Key: fieldChain, Value: 1}, {Key: fieldState, Value: 1}, {Key: fieldUpdatedAt, Value: 1}}, Options: options.Index().SetName("chain_state_updated").SetSparse(true)},
		},
		{
			name:  "wrong index partial filter",
			model: mongo.IndexModel{Keys: bson.D{{Key: fieldChain, Value: 1}, {Key: fieldState, Value: 1}, {Key: fieldUpdatedAt, Value: 1}}, Options: options.Index().SetName("chain_state_updated").SetPartialFilterExpression(bson.D{{Key: fieldState, Value: "ready"}})},
		},
		{
			name:  "wrong index collation",
			model: mongo.IndexModel{Keys: bson.D{{Key: fieldChain, Value: 1}, {Key: fieldState, Value: 1}, {Key: fieldUpdatedAt, Value: 1}}, Options: options.Index().SetName("chain_state_updated").SetCollation(&options.Collation{Locale: "en"})},
		},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			database := schemaTestDatabase("index")
			db := replica.Client.Database(database)
			require.NoError(t, createCompatibleCollection(ctx, db, payloadCollection, schemaPayloadValidator()))
			_, err := db.Collection(payloadCollection).Indexes().CreateOne(ctx, scenario.model)
			require.NoError(t, err)
			before := schemaIndexRaw(ctx, t, db, payloadCollection, "chain_state_updated")

			_, err = New(ctx, replica.Client, schemaTestConfig(database, "index"))
			require.ErrorIs(t, err, ErrIncompatibleSchema)
			require.Equal(t, before, schemaIndexRaw(ctx, t, db, payloadCollection, "chain_state_updated"))
			require.Zero(t, schemaLedgerCount(ctx, t, db))
		})
	}

	t.Run("index compatibility rejects TTL", func(t *testing.T) {
		model := schemaPayloadIndexes()[0]
		current := bson.M{
			"name":               "chain_state_updated",
			"key":                model.Keys,
			"expireAfterSeconds": int64(60),
		}
		require.False(t, schemaIndexCompatible(current, model))
	})
}

func schemaTestConfig(database, nodeID string) Config {
	return Config{Database: database, Scope: engine.StorageScope{Network: "testnet", GenesisHash: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", NodeID: nodeID}}
}

func schemaTestDatabase(prefix string) string {
	return "schema_" + prefix + "_" + bson.NewObjectID().Hex()
}

func schemaScopeID(config Config) string {
	return tupleID("scope", config.Scope.Network, config.Scope.GenesisHash, config.Scope.NodeID)
}

func createCompatibleCollection(ctx context.Context, db *mongo.Database, name string, validator bson.D) error {
	return db.CreateCollection(ctx, name, options.CreateCollection().SetCollation(&options.Collation{Locale: "simple"}).SetValidator(validator).SetValidationLevel("strict").SetValidationAction("error"))
}

func schemaLedgerCount(ctx context.Context, t *testing.T, db *mongo.Database) int64 {
	t.Helper()
	count, err := db.Collection(schemaCollection).CountDocuments(ctx, bson.D{{Key: "type", Value: "schema"}})
	require.NoError(t, err)
	return count
}

func schemaCollectionRaw(ctx context.Context, t *testing.T, db *mongo.Database, name string) []byte {
	t.Helper()
	cursor, err := db.ListCollections(ctx, bson.D{{Key: "name", Value: name}})
	require.NoError(t, err)
	defer func() { _ = cursor.Close(context.WithoutCancel(ctx)) }()
	require.True(t, cursor.Next(ctx))
	return bytes.Clone(cursor.Current)
}

func schemaIndexRaw(ctx context.Context, t *testing.T, db *mongo.Database, collection, name string) []byte {
	t.Helper()
	cursor, err := db.Collection(collection).Indexes().List(ctx)
	require.NoError(t, err)
	defer func() { _ = cursor.Close(context.WithoutCancel(ctx)) }()
	for cursor.Next(ctx) {
		var index bson.M
		require.NoError(t, bson.Unmarshal(cursor.Current, &index))
		if index["name"] == name {
			return bytes.Clone(cursor.Current)
		}
	}
	require.NoError(t, cursor.Err())
	t.Fatalf("index %q not found", name)
	return nil
}
