package mongotest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestReplicaSetSmokeMajorityTransaction(t *testing.T) {
	replica := New(t)
	require.Equal(t, memberTotal, replica.MemberCount())

	ctx, cancel := context.WithTimeout(context.Background(), startupTimeout)
	defer cancel()
	for index := 0; index < replica.MemberCount(); index++ {
		client, err := replica.MemberClient(ctx, index)
		require.NoError(t, err)
		require.NoError(t, client.Disconnect(ctx))
	}

	session, err := replica.Client.StartSession()
	require.NoError(t, err)
	defer session.EndSession(ctx)
	collection := replica.Client.Database("mongotest_smoke").Collection("majority_transactions")
	_, err = session.WithTransaction(ctx, func(transactionContext context.Context) (any, error) {
		_, insertErr := collection.InsertOne(transactionContext, bson.D{{Key: "_id", Value: "smoke"}, {Key: "value", Value: "majority"}})
		return nil, insertErr
	})
	require.NoError(t, err)

	count, err := collection.CountDocuments(ctx, bson.D{{Key: "_id", Value: "smoke"}})
	require.NoError(t, err)
	assert.Equalf(t, int64(1), count, "transaction must commit through %s", replica.URI)
}
