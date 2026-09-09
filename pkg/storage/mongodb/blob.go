package mongodb

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"math"
	"strconv"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/engine"
)

const (
	blobBucketName           = "go_overlay_v1_blobs"
	blobChunkSize      int32 = 255 * 1024
	blobMaxBytes             = uint64(1) << 40
	blobStateStaged          = "staged"
	blobStatePublished       = "published"
)

var (
	// ErrBlobContext prevents GridFS I/O from being accidentally included in a metadata transaction.
	ErrBlobContext = errors.New("MongoDB blob operation requires a context without a session")
	// ErrBlobMetadata marks a malformed, untrusted GridFS metadata document.
	ErrBlobMetadata = errors.New("invalid MongoDB blob metadata")
	// ErrBlobCorrupt marks content which cannot be proven to match its metadata.
	ErrBlobCorrupt = errors.New("corrupt MongoDB blob content")
	// ErrBlobUnavailable marks missing or unpublished content.
	ErrBlobUnavailable = errors.New("MongoDB blob unavailable")
)

// blobMetadata is stored in the GridFS file metadata document. ByteLength is
// S01 canonical unsigned decimal text, distinct from fixed-width BSON counters.
type blobMetadata struct {
	ChainID    string `bson:"chain"`
	Digest     string `bson:"digest"`
	ByteLength string `bson:"byteLength"`
	OwnerID    string `bson:"owner"`
	Token      string `bson:"token"`
	State      string `bson:"state"`
}

// blobStore streams content to a dedicated versioned GridFS bucket. It publishes verified GridFS metadata; the caller owns
// the separate ready/reference/deletion policy.
type blobStore struct {
	db     *mongo.Database
	bucket *mongo.GridFSBucket
}

func newBlobStore(db *mongo.Database) *blobStore {
	if db == nil {
		return nil
	}
	return &blobStore{db: db, bucket: db.GridFSBucket(options.GridFSBucket().SetName(blobBucketName).SetChunkSizeBytes(blobChunkSize))}
}

// upload writes exactly metadata.ByteLength bytes from reader, verifies their
// SHA-256 digest, then atomically promotes matching staged metadata to
// published. It never buffers the full payload. Any failed write is aborted
// and then best-effort cleaned up.
func (s *blobStore) upload(ctx context.Context, id bson.ObjectID, metadata blobMetadata, reader io.Reader) (err error) {
	if err = s.checkContext(ctx); err != nil {
		return err
	}
	if reader == nil {
		return ErrBlobMetadata
	}
	expected, err := validateBlobMetadata(metadata)
	if err != nil {
		return err
	}
	metadata.State = blobStateStaged
	stream, err := s.bucket.OpenUploadStreamWithID(ctx, id, id.Hex(), options.GridFSUpload().SetChunkSizeBytes(blobChunkSize).SetMetadata(metadata))
	if err != nil {
		return fmt.Errorf("open GridFS upload: %w", err)
	}
	closed := false
	defer func() {
		if err == nil || closed {
			return
		}
		cleanupErr := stream.Abort()
		cleanupErr = errors.Join(cleanupErr, s.cleanup(ctx, id))
		if cleanupErr != nil {
			err = errors.Join(err, cleanupErr)
		}
	}()

	digest, length, err := streamBlob(ctx, reader, expected, stream)
	if err != nil {
		return err
	}
	if length != expected || digest != metadata.Digest {
		return fmt.Errorf("%w: uploaded bytes do not match declared digest or length", ErrBlobCorrupt)
	}
	if err = stream.Close(); err != nil {
		return fmt.Errorf("close verified GridFS upload: %w", err)
	}
	closed = true
	return s.publish(ctx, id, metadata)
}

// publish is the only GridFS metadata transition. Its full identity guard
// prevents a delayed upload from publishing a different owner or payload.
func (s *blobStore) publish(ctx context.Context, id bson.ObjectID, metadata blobMetadata) error {
	filter := bson.D{
		{Key: fieldID, Value: id},
		{Key: fieldLength, Value: blobLengthBSON(metadata.ByteLength)},
		{Key: "chunkSize", Value: blobChunkSize},
		{Key: "metadata.chain", Value: metadata.ChainID},
		{Key: "metadata.digest", Value: metadata.Digest},
		{Key: "metadata.byteLength", Value: metadata.ByteLength},
		{Key: "metadata.owner", Value: metadata.OwnerID},
		{Key: "metadata.token", Value: metadata.Token},
		{Key: "metadata.state", Value: blobStateStaged},
	}
	result, err := s.bucket.GetFilesCollection().UpdateOne(ctx, filter, bson.D{{Key: fieldSet, Value: bson.D{{Key: "metadata.state", Value: blobStatePublished}}}})
	if err != nil {
		return fmt.Errorf("publish verified GridFS upload: %w", err)
	}
	if result.MatchedCount != 1 {
		return ErrBlobUnavailable
	}
	return nil
}

// copy streams one published blob to writer while verifying the exact stored
// identity. A corrupt or truncated GridFS stream may have already written a
// prefix to writer; in that case this method returns ErrBlobCorrupt and callers
// must discard that partial output.
func (s *blobStore) copy(ctx context.Context, id bson.ObjectID, metadata blobMetadata, writer io.Writer) (err error) {
	if err = s.checkContext(ctx); err != nil {
		return err
	}
	if writer == nil {
		return ErrBlobMetadata
	}
	expected, err := validateBlobMetadata(metadata)
	if err != nil {
		return err
	}
	stored, err := s.loadPublishedBlob(ctx, id, metadata, expected)
	if err != nil {
		return err
	}
	stream, err := s.openDownloadStream(ctx, id)
	if err != nil {
		return err
	}
	defer func() {
		closeErr := stream.Close()
		if closeErr != nil && err == nil {
			err = fmt.Errorf("close GridFS download: %w", closeErr)
		}
	}()
	if err = verifyStreamedBlobFile(stream.GetFile(), stored, metadata, expected); err != nil {
		return err
	}
	return copyVerifiedBlob(ctx, stream, expected, metadata.Digest, writer)
}

func (s *blobStore) openDownloadStream(ctx context.Context, id bson.ObjectID) (*mongo.GridFSDownloadStream, error) {
	stream, err := s.bucket.OpenDownloadStream(ctx, id)
	if err == nil {
		return stream, nil
	}
	if errors.Is(err, mongo.ErrFileNotFound) {
		return nil, ErrBlobUnavailable
	}
	return nil, fmt.Errorf("open GridFS download: %w", err)
}

func verifyStreamedBlobFile(file *mongo.GridFSFile, stored storedBlobFile, metadata blobMetadata, expected uint64) error {
	if file == nil || file.Length != stored.Length || file.ChunkSize != blobChunkSize || file.Length < 0 || uint64(file.Length) != expected {
		return ErrBlobUnavailable
	}
	var streamedMetadata blobMetadata
	if len(file.Metadata) == 0 || bson.Unmarshal(file.Metadata, &streamedMetadata) != nil || streamedMetadata.State != blobStatePublished || !sameBlobIdentity(streamedMetadata, metadata) {
		return ErrBlobUnavailable
	}
	return nil
}

func copyVerifiedBlob(ctx context.Context, stream io.Reader, expected uint64, digest string, writer io.Writer) error {
	gotDigest, length, copyErr := streamBlob(ctx, stream, expected, writer)
	if copyErr != nil {
		var writerErr *blobWriterError
		if errors.As(copyErr, &writerErr) || errors.Is(copyErr, context.Canceled) || errors.Is(copyErr, context.DeadlineExceeded) {
			return copyErr
		}
		return fmt.Errorf("%w: %w", ErrBlobCorrupt, copyErr)
	}
	if length != expected || gotDigest != digest {
		return fmt.Errorf("%w: downloaded bytes do not match metadata", ErrBlobCorrupt)
	}
	return nil
}

type storedBlobFile struct {
	Length    int64        `bson:"length"`
	ChunkSize int32        `bson:"chunkSize"`
	Metadata  blobMetadata `bson:"metadata"`
}

// loadPublishedBlob checks the file record before GridFS constructs its
// reader, whose chunkSize field otherwise controls an allocation.
func (s *blobStore) loadPublishedBlob(ctx context.Context, id bson.ObjectID, metadata blobMetadata, expected uint64) (storedBlobFile, error) {
	var stored storedBlobFile
	err := s.bucket.GetFilesCollection().FindOne(ctx, bson.D{{Key: fieldID, Value: id}}).Decode(&stored)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return storedBlobFile{}, ErrBlobUnavailable
	}
	if err != nil {
		return storedBlobFile{}, fmt.Errorf("read GridFS file metadata: %w", err)
	}
	if stored.Length < 0 || uint64(stored.Length) != expected || stored.ChunkSize != blobChunkSize || stored.Metadata.State != blobStatePublished || !sameBlobIdentity(stored.Metadata, metadata) {
		return storedBlobFile{}, ErrBlobUnavailable
	}
	return stored, nil
}

// remove deletes GridFS metadata and all matching chunks. It is idempotent,
// including an interrupted staged upload where chunks exist without a file.
func (s *blobStore) remove(ctx context.Context, id bson.ObjectID) error {
	if err := s.checkContext(ctx); err != nil {
		return err
	}
	_, fileErr := s.bucket.GetFilesCollection().DeleteOne(ctx, bson.D{{Key: fieldID, Value: id}})
	_, chunkErr := s.bucket.GetChunksCollection().DeleteMany(ctx, bson.D{{Key: fieldFilesID, Value: id}})
	if fileErr != nil || chunkErr != nil {
		return errors.Join(fileErr, chunkErr)
	}
	return nil
}

func (s *blobStore) checkContext(ctx context.Context) error {
	if s == nil || s.db == nil || s.bucket == nil || ctx == nil {
		return ErrBlobMetadata
	}
	if mongo.SessionFromContext(ctx) != nil {
		return ErrBlobContext
	}
	return ctx.Err()
}

func (s *blobStore) cleanup(ctx context.Context, id bson.ObjectID) error {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	// A failed Close can be an unknown-result error after MongoDB accepted the
	// files document. Deleting that document could erase a valid concurrent
	// upload using the same ID, so failure cleanup removes only uncommitted
	// chunks. Successful staged files are reclaimed by the metadata owner.
	_, err := s.bucket.GetChunksCollection().DeleteMany(cleanupCtx, bson.D{{Key: fieldFilesID, Value: id}})
	return err
}

func validateBlobMetadata(metadata blobMetadata) (uint64, error) {
	if !validText(metadata.ChainID) || !validHash(metadata.Digest) || !validText(metadata.OwnerID) || !validText(metadata.Token) {
		return 0, ErrBlobMetadata
	}
	length, err := engine.ParseStorageUint64(engine.StorageUint64(metadata.ByteLength))
	if err != nil || length > blobMaxBytes || length > math.MaxInt64 {
		return 0, ErrBlobMetadata
	}
	return length, nil
}

func sameBlobIdentity(left, right blobMetadata) bool {
	return left.ChainID == right.ChainID && left.Digest == right.Digest && left.ByteLength == right.ByteLength && left.OwnerID == right.OwnerID && left.Token == right.Token
}

func blobLengthBSON(value string) int64 {
	length, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return -1
	}
	return length
}

func streamBlob(ctx context.Context, reader io.Reader, expected uint64, writer io.Writer) (string, uint64, error) {
	digest := sha256.New()
	buffer := make([]byte, 32<<10)
	written, err := copyBlobBytes(ctx, reader, expected, writer, digest, buffer)
	if err != nil {
		return "", written, err
	}
	return finishBlobStream(ctx, reader, digest, written)
}

func copyBlobBytes(ctx context.Context, reader io.Reader, expected uint64, writer io.Writer, digest hash.Hash, buffer []byte) (uint64, error) {
	var written uint64
	for written < expected {
		n, readErr, err := readBlobChunk(ctx, reader, buffer, expected, written)
		if err != nil {
			return written, err
		}
		written, stop, err := applyBlobChunk(writer, digest, buffer[:n], n, readErr, written, expected)
		if err != nil || stop {
			return written, err
		}
	}
	return written, nil
}

func readBlobChunk(ctx context.Context, reader io.Reader, buffer []byte, expected, written uint64) (int, error, error) {
	if err := ctx.Err(); err != nil {
		return 0, nil, err
	}
	wanted := uint64(len(buffer))
	if remaining := expected - written; remaining < wanted {
		wanted = remaining
	}
	n, readErr := reader.Read(buffer[:wanted])
	if n < 0 || uint64(n) > wanted {
		return 0, readErr, ErrBlobCorrupt
	}
	if n > 0 && uint64(n) > expected-written {
		return 0, readErr, ErrBlobCorrupt
	}
	return n, readErr, nil
}

func applyBlobChunk(writer io.Writer, digest hash.Hash, data []byte, n int, readErr error, written, expected uint64) (uint64, bool, error) {
	if n > 0 {
		if err := commitBlobChunk(writer, digest, data); err != nil {
			return written, true, err
		}
		written += uint64(n)
	}
	if readErr != nil {
		if readErr == io.EOF && written == expected {
			return written, true, nil
		}
		return written, true, readErr
	}
	if n == 0 {
		return written, true, io.ErrNoProgress
	}
	return written, false, nil
}

func commitBlobChunk(writer io.Writer, digest hash.Hash, data []byte) error {
	if err := writeBlobBytes(writer, data); err != nil {
		return &blobWriterError{err: err}
	}
	_, err := digest.Write(data)
	return err
}

func finishBlobStream(ctx context.Context, reader io.Reader, digest hash.Hash, written uint64) (string, uint64, error) {
	if err := ctx.Err(); err != nil {
		return "", written, err
	}
	var extra [1]byte
	n, readErr := reader.Read(extra[:])
	if n > 0 {
		return "", written, ErrBlobCorrupt
	}
	if readErr != nil && readErr != io.EOF {
		return "", written, readErr
	}
	return hex.EncodeToString(digest.Sum(nil)), written, nil
}

type blobWriterError struct{ err error }

func (e *blobWriterError) Error() string { return e.err.Error() }

func (e *blobWriterError) Unwrap() error { return e.err }

func writeBlobBytes(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := writer.Write(data)
		if n < 0 || n > len(data) {
			return io.ErrShortWrite
		}
		if n > 0 {
			data = data[n:]
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}
