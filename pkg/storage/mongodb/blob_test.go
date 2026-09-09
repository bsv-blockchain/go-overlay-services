package mongodb

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/bsv-blockchain/go-overlay-services/pkg/storage/mongodb/internal/mongotest"
)

func TestBlobStoreStreamsAndVerifiesContent(t *testing.T) {
	replica := mongotest.New(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	store := newBlobStore(replica.Client.Database("blob_test_" + bson.NewObjectID().Hex()))

	const largeLength = 17 << 20
	largeMetadata := newBlobMetadata(largeLength, 'a')
	largeID := bson.NewObjectID()
	require.NoError(t, store.upload(ctx, largeID, largeMetadata, &repeatedByteReader{remaining: largeLength, value: 'a'}))

	// Upload leaves no externally visible staged file after its exact identity
	// has been verified and its guarded GridFS metadata CAS succeeds.
	var file struct {
		Metadata blobMetadata `bson:"metadata"`
	}
	require.NoError(t, store.bucket.GetFilesCollection().FindOne(ctx, bson.D{{Key: fieldID, Value: largeID}}).Decode(&file))
	require.Equal(t, blobStatePublished, file.Metadata.State)
	stageBlob(ctx, t, store, largeID)
	require.ErrorIs(t, store.copy(ctx, largeID, largeMetadata, io.Discard), ErrBlobUnavailable)

	publishBlob(ctx, t, store, largeID)
	require.NoError(t, store.copy(ctx, largeID, largeMetadata, io.Discard))

	wrongDigest := largeMetadata
	wrongDigest.Digest = strings.Repeat("0", 64)
	require.ErrorIs(t, store.copy(ctx, largeID, wrongDigest, io.Discard), ErrBlobUnavailable)

	require.NoError(t, store.remove(ctx, largeID))
	require.NoError(t, store.remove(ctx, largeID))
	files, err := store.bucket.GetFilesCollection().CountDocuments(ctx, bson.D{{Key: fieldID, Value: largeID}})
	require.NoError(t, err)
	chunks, err := store.bucket.GetChunksCollection().CountDocuments(ctx, bson.D{{Key: fieldFilesID, Value: largeID}})
	require.NoError(t, err)
	require.Zero(t, files)
	require.Zero(t, chunks)
}

func TestBlobStoreRejectsMismatchesAndCorruptGridFS(t *testing.T) {
	replica := mongotest.New(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	store := newBlobStore(replica.Client.Database("blob_test_" + bson.NewObjectID().Hex()))

	data := bytes.Repeat([]byte{'b'}, int(blobChunkSize)*2+1)
	metadata := newBlobMetadata(len(data), 'b')

	badDigest := metadata
	badDigest.Digest = strings.Repeat("0", 64)
	badDigestID := bson.NewObjectID()
	require.ErrorIs(t, store.upload(ctx, badDigestID, badDigest, bytes.NewReader(data)), ErrBlobCorrupt)
	assertBlobRemoved(ctx, t, store, badDigestID)

	badLength := metadata
	badLength.ByteLength = strconv.Itoa(len(data) - 1)
	badLengthID := bson.NewObjectID()
	require.ErrorIs(t, store.upload(ctx, badLengthID, badLength, bytes.NewReader(data)), ErrBlobCorrupt)
	assertBlobRemoved(ctx, t, store, badLengthID)

	id := bson.NewObjectID()
	require.NoError(t, store.upload(ctx, id, metadata, bytes.NewReader(data)))
	publishBlob(ctx, t, store, id)
	_, err := store.bucket.GetChunksCollection().DeleteOne(ctx, bson.D{{Key: fieldFilesID, Value: id}, {Key: "n", Value: 1}})
	require.NoError(t, err)
	var received bytes.Buffer
	require.ErrorIs(t, store.copy(ctx, id, metadata, &received), ErrBlobCorrupt)
	require.NotEmpty(t, received.Bytes(), "a missing later GridFS chunk may leave a verified prefix in the writer")

	corruptID := bson.NewObjectID()
	require.NoError(t, store.upload(ctx, corruptID, metadata, bytes.NewReader(data)))
	publishBlob(ctx, t, store, corruptID)
	_, err = store.bucket.GetChunksCollection().UpdateOne(ctx, bson.D{{Key: fieldFilesID, Value: corruptID}, {Key: "n", Value: 0}}, bson.D{{Key: fieldSet, Value: bson.D{{Key: "data", Value: bson.Binary{Subtype: 0, Data: bytes.Repeat([]byte{'c'}, int(blobChunkSize))}}}}})
	require.NoError(t, err)
	require.ErrorIs(t, store.copy(ctx, corruptID, metadata, io.Discard), ErrBlobCorrupt)
}

func newBlobMetadata(length int, value byte) blobMetadata {
	return blobMetadata{
		ChainID:    "test-chain",
		Digest:     repeatedSHA256(length, value),
		ByteLength: strconv.Itoa(length),
		OwnerID:    "test-owner",
		Token:      "test-token",
	}
}

func publishBlob(ctx context.Context, t *testing.T, store *blobStore, id bson.ObjectID) {
	t.Helper()
	_, err := store.bucket.GetFilesCollection().UpdateOne(ctx, bson.D{{Key: fieldID, Value: id}}, bson.D{{Key: fieldSet, Value: bson.D{{Key: "metadata.state", Value: blobStatePublished}}}})
	require.NoError(t, err)
}

func stageBlob(ctx context.Context, t *testing.T, store *blobStore, id bson.ObjectID) {
	t.Helper()
	_, err := store.bucket.GetFilesCollection().UpdateOne(ctx, bson.D{{Key: fieldID, Value: id}}, bson.D{{Key: fieldSet, Value: bson.D{{Key: "metadata.state", Value: blobStateStaged}}}})
	require.NoError(t, err)
}

func assertBlobRemoved(ctx context.Context, t *testing.T, store *blobStore, id bson.ObjectID) {
	t.Helper()
	files, err := store.bucket.GetFilesCollection().CountDocuments(ctx, bson.D{{Key: fieldID, Value: id}})
	require.NoError(t, err)
	chunks, err := store.bucket.GetChunksCollection().CountDocuments(ctx, bson.D{{Key: fieldFilesID, Value: id}})
	require.NoError(t, err)
	require.Zero(t, files)
	require.Zero(t, chunks)
}

func repeatedSHA256(length int, value byte) string {
	hash := sha256.New()
	buffer := bytes.Repeat([]byte{value}, 32<<10)
	for length > 0 {
		n := len(buffer)
		if length < n {
			n = length
		}
		_, _ = hash.Write(buffer[:n])
		length -= n
	}
	return hex.EncodeToString(hash.Sum(nil))
}

type repeatedByteReader struct {
	remaining int
	value     byte
}

func (r *repeatedByteReader) Read(data []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	n := len(data)
	if r.remaining < n {
		n = r.remaining
	}
	for index := range data[:n] {
		data[index] = r.value
	}
	r.remaining -= n
	return n, nil
}
