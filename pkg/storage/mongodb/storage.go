package mongodb

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/bsv-blockchain/go-sdk/chainhash"
	"github.com/bsv-blockchain/go-sdk/overlay"
	"github.com/bsv-blockchain/go-sdk/transaction"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/engine"
)

type transactionDocument struct {
	ID          string    `bson:"_id"`
	Version     int32     `bson:"version"`
	Chain       string    `bson:"chain"`
	TxID        string    `bson:"txid"`
	BeefDigest  string    `bson:"beefDigest,omitempty"`
	BeefLength  string    `bson:"beefLength,omitempty"`
	BeefKind    string    `bson:"beefKind,omitempty"`
	BlockHeight string    `bson:"blockHeight,omitempty"`
	BlockHash   string    `bson:"blockHash,omitempty"`
	BlockIndex  string    `bson:"blockIndex,omitempty"`
	MerkleRoot  string    `bson:"merkleRoot,omitempty"`
	Ancillary   []string  `bson:"ancillaryTxids,omitempty"`
	CreatedAt   time.Time `bson:"createdAt"`
	UpdatedAt   time.Time `bson:"updatedAt"`
}

type cursorDocument struct {
	ID        string    `bson:"_id"`
	Version   int32     `bson:"version"`
	Scope     string    `bson:"scope"`
	Host      string    `bson:"host"`
	Topic     string    `bson:"topic"`
	Since     float64   `bson:"since"`
	CreatedAt time.Time `bson:"createdAt"`
	UpdatedAt time.Time `bson:"updatedAt"`
}

// InsertOutputs persists admitted topic outputs and their BEEF payload outside any admission transaction.
func (s *Store) InsertOutputs(ctx context.Context, topic string, txid *chainhash.Hash, outputs []uint32, outpointsConsumed []*transaction.Outpoint, beef *transaction.Beef, ancillaryTxids []*chainhash.Hash) error {
	if !validText(topic) || txid == nil {
		return ErrInvalidConfig
	}
	txID := canonicalHash(txid)
	var beefRef *engine.AdmissionPayloadRef
	if beef != nil {
		beefBytes, err := beef.AtomicBytes(txid)
		if err != nil {
			return err
		}
		ref := payloadRefFromBytes(beefBytes, engine.AdmissionPayloadBEEFManifest)
		if err = s.PublishPayload(ctx, ref, bytes.NewReader(beefBytes)); err != nil {
			return err
		}
		beefRef = &ref
	}
	ancillary := hashStrings(ancillaryTxids)
	now := time.Now().UTC()
	_, err := s.runTransaction(ctx, func(sessionCtx context.Context) error {
		if beefRef != nil {
			if pinErr := s.pinPayload(sessionCtx, *beefRef, ReferenceOwner{Kind: ReferenceTransaction, ID: txID}); pinErr != nil {
				return pinErr
			}
			if upsertErr := s.upsertTransaction(sessionCtx, txID, beefRef, ancillary, now); upsertErr != nil {
				return upsertErr
			}
		}
		for _, vout := range outputs {
			outpoint := engine.AdmissionOutpoint{TxID: txID, OutputIndex: engine.StorageUint64(strconv.FormatUint(uint64(vout), 10))}
			if insertErr := s.insertEngineOutput(sessionCtx, topic, outpoint, ancillary, now); insertErr != nil {
				return insertErr
			}
			for _, consumed := range outpointsConsumed {
				if consumed == nil {
					continue
				}
				edge := engine.AdmissionEdge{
					Source:   engine.AdmissionOutpoint{TxID: canonicalHash(&consumed.Txid), OutputIndex: engine.StorageUint64(strconv.FormatUint(uint64(consumed.Index), 10))},
					Consumer: outpoint,
				}
				if edgeErr := s.insertAdmissionEdge(sessionCtx, topic, edge, now); edgeErr != nil {
					return edgeErr
				}
			}
		}
		return nil
	})
	return err
}

func (s *Store) insertEngineOutput(ctx context.Context, topic string, outpoint engine.AdmissionOutpoint, ancillary []string, now time.Time) error {
	index, err := encodeOutpointIndex(outpoint.OutputIndex)
	if err != nil {
		return err
	}
	zero, err := EncodeUint64("0")
	if err != nil {
		return err
	}
	doc := outputDocument{
		ID: s.outputID(topic, outpoint), Version: schemaVersion, Scope: s.scopeID, Topic: topic, TxID: outpoint.TxID,
		OutputIndex: index, Satoshis: zero, Score: zero, EngineScore: float64(now.UnixMilli()), Spent: false, Serving: true,
		SpendVersion: spendVersionInitial, MerkleState: merkleStateUnmined, Ancillary: ancillary, CreatedAt: now, UpdatedAt: now,
	}
	_, err = s.db.Collection(outputCollection).UpdateOne(ctx, bson.D{{Key: fieldID, Value: doc.ID}}, bson.D{{Key: "$setOnInsert", Value: doc}}, options.UpdateOne().SetUpsert(true))
	return err
}

func (s *Store) upsertTransaction(ctx context.Context, txID string, beefRef *engine.AdmissionPayloadRef, ancillary []string, now time.Time) error {
	doc := transactionDocument{ID: s.transactionID(txID), Version: schemaVersion, Chain: s.chainID, TxID: txID, Ancillary: ancillary, CreatedAt: now, UpdatedAt: now}
	if beefRef != nil {
		length, err := EncodeUint64(beefRef.ByteLength)
		if err != nil {
			return err
		}
		doc.BeefDigest = beefRef.Digest
		doc.BeefLength = length
		doc.BeefKind = string(beefRef.Kind)
	}
	_, err := s.db.Collection(transactionCollection).UpdateOne(ctx, bson.D{{Key: fieldID, Value: doc.ID}}, mongo.Pipeline{bson.D{{Key: "$replaceWith", Value: bson.D{{Key: "$mergeObjects", Value: bson.A{bson.D{{Key: "$literal", Value: doc}}, bson.D{{Key: fieldCreatedAt, Value: bson.D{{Key: "$ifNull", Value: bson.A{"$createdAt", now}}}}}}}}}}}, options.UpdateOne().SetUpsert(true))
	return err
}

// FindOutput returns one scoped output, or (nil, nil) when it is absent.
func (s *Store) FindOutput(ctx context.Context, outpoint *transaction.Outpoint, topic *string, spent *bool, includeBEEF bool) (*engine.Output, error) {
	if outpoint == nil {
		return nil, nil //nolint:nilnil // missing output is represented as a nil result
	}
	filter := s.outpointFilter(outpoint, topic, spent)
	var doc outputDocument
	err := s.db.Collection(outputCollection).FindOne(ctx, filter).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil //nolint:nilnil // missing output is represented as a nil result
	}
	if err != nil {
		return nil, err
	}
	return s.outputFromDocument(ctx, doc, includeBEEF)
}

// FindOutputs returns a slot-aligned result for each requested outpoint.
func (s *Store) FindOutputs(ctx context.Context, outpoints []*transaction.Outpoint, topic string, spent *bool, includeBEEF bool) ([]*engine.Output, error) {
	results := make([]*engine.Output, len(outpoints))
	for i, outpoint := range outpoints {
		output, err := s.FindOutput(ctx, outpoint, &topic, spent, includeBEEF)
		if err != nil {
			return nil, err
		}
		results[i] = output
	}
	return results, nil
}

// FindOutputsForTransaction returns every stored output for a transaction.
func (s *Store) FindOutputsForTransaction(ctx context.Context, txid *chainhash.Hash, includeBEEF bool) ([]*engine.Output, error) {
	if txid == nil {
		return nil, nil
	}
	cursor, err := s.db.Collection(outputCollection).Find(ctx, bson.D{{Key: fieldScope, Value: s.scopeID}, {Key: fieldTxID, Value: canonicalHash(txid)}})
	if err != nil {
		return nil, err
	}
	defer schemaCloseCursor(ctx, cursor)
	var docs []outputDocument
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	outputs := make([]*engine.Output, 0, len(docs))
	for _, doc := range docs {
		output, outErr := s.outputFromDocument(ctx, doc, includeBEEF)
		if outErr != nil {
			return nil, outErr
		}
		outputs = append(outputs, output)
	}
	return outputs, nil
}

// FindUTXOsForTopic returns unspent serving outputs with engineScore greater than since.
func (s *Store) FindUTXOsForTopic(ctx context.Context, topic string, since float64, limit uint32, includeBEEF bool) ([]*engine.Output, error) {
	filter := bson.D{{Key: fieldScope, Value: s.scopeID}, {Key: fieldTopic, Value: topic}, {Key: fieldSpent, Value: false}, {Key: fieldServing, Value: true}, {Key: fieldEngineScore, Value: bson.D{{Key: "$gt", Value: since}}}}
	opts := options.Find().SetSort(bson.D{{Key: fieldEngineScore, Value: 1}, {Key: fieldID, Value: 1}})
	if limit > 0 {
		opts.SetLimit(int64(limit))
	}
	cursor, err := s.db.Collection(outputCollection).Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer schemaCloseCursor(ctx, cursor)
	var docs []outputDocument
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	outputs := make([]*engine.Output, 0, len(docs))
	for _, doc := range docs {
		output, outErr := s.outputFromDocument(ctx, doc, includeBEEF)
		if outErr != nil {
			return nil, outErr
		}
		outputs = append(outputs, output)
	}
	return outputs, nil
}

// DeleteOutput removes a serving output. Applied history is retained.
func (s *Store) DeleteOutput(ctx context.Context, outpoint *transaction.Outpoint, topic string) error {
	if outpoint == nil {
		return nil
	}
	_, err := s.db.Collection(outputCollection).DeleteOne(ctx, bson.D{{Key: fieldID, Value: s.outputID(topic, admissionOutpoint(outpoint))}})
	return err
}

// MarkUTXOsAsSpent marks the requested outputs spent by spendTxid.
func (s *Store) MarkUTXOsAsSpent(ctx context.Context, outpoints []*transaction.Outpoint, topic string, spendTxid *chainhash.Hash) error {
	if spendTxid == nil {
		return ErrInvalidConfig
	}
	spender := canonicalHash(spendTxid)
	now := time.Now().UTC()
	for _, outpoint := range outpoints {
		if outpoint == nil {
			continue
		}
		_, err := s.db.Collection(outputCollection).UpdateOne(ctx, bson.D{{Key: fieldID, Value: s.outputID(topic, admissionOutpoint(outpoint))}}, bson.D{{Key: fieldSet, Value: bson.D{{Key: fieldSpent, Value: true}, {Key: fieldSpentBy, Value: spender}, {Key: fieldUpdatedAt, Value: now}}}})
		if err != nil {
			return err
		}
	}
	return nil
}

// UpdateConsumedBy replaces consumption edges for one source outpoint.
func (s *Store) UpdateConsumedBy(ctx context.Context, outpoint *transaction.Outpoint, topic string, consumedBy []*transaction.Outpoint) error {
	if outpoint == nil {
		return nil
	}
	now := time.Now().UTC()
	source := admissionOutpoint(outpoint)
	sourceIndex, err := encodeOutpointIndex(source.OutputIndex)
	if err != nil {
		return err
	}
	_, err = s.runTransaction(ctx, func(sessionCtx context.Context) error {
		if _, delErr := s.db.Collection(edgeCollection).DeleteMany(sessionCtx, bson.D{{Key: fieldScope, Value: s.scopeID}, {Key: fieldTopic, Value: topic}, {Key: fieldSourceTxID, Value: source.TxID}, {Key: fieldSourceIndex, Value: sourceIndex}}); delErr != nil {
			return delErr
		}
		for _, consumer := range consumedBy {
			if consumer == nil {
				continue
			}
			edge := engine.AdmissionEdge{Source: source, Consumer: admissionOutpoint(consumer)}
			if edgeErr := s.insertAdmissionEdge(sessionCtx, topic, edge, now); edgeErr != nil {
				return edgeErr
			}
		}
		return nil
	})
	return err
}

// UpdateTransactionBEEF publishes and stores a replacement BEEF for one transaction.
func (s *Store) UpdateTransactionBEEF(ctx context.Context, txid *chainhash.Hash, beef *transaction.Beef) error {
	if txid == nil || beef == nil {
		return ErrInvalidConfig
	}
	beefBytes, err := beef.AtomicBytes(txid)
	if err != nil {
		return err
	}
	ref := payloadRefFromBytes(beefBytes, engine.AdmissionPayloadBEEFManifest)
	if err = s.PublishPayload(ctx, ref, bytes.NewReader(beefBytes)); err != nil {
		return err
	}
	now := time.Now().UTC()
	_, err = s.runTransaction(ctx, func(sessionCtx context.Context) error {
		if pinErr := s.pinPayload(sessionCtx, ref, ReferenceOwner{Kind: ReferenceTransaction, ID: canonicalHash(txid)}); pinErr != nil {
			return pinErr
		}
		return s.upsertTransaction(sessionCtx, canonicalHash(txid), &ref, nil, now)
	})
	return err
}

// UpdateOutputBlockHeight stores canonical block coordinates on one output.
func (s *Store) UpdateOutputBlockHeight(ctx context.Context, outpoint *transaction.Outpoint, topic string, blockHeight uint32, blockIndex uint64) error {
	if outpoint == nil {
		return nil
	}
	height, err := EncodeUint64(engine.StorageUint64(strconv.FormatUint(uint64(blockHeight), 10)))
	if err != nil {
		return err
	}
	index, err := EncodeUint64(engine.StorageUint64(strconv.FormatUint(blockIndex, 10)))
	if err != nil {
		return err
	}
	_, err = s.db.Collection(outputCollection).UpdateOne(ctx, bson.D{{Key: fieldID, Value: s.outputID(topic, admissionOutpoint(outpoint))}}, bson.D{{Key: fieldSet, Value: bson.D{{Key: fieldBlockHeight, Value: height}, {Key: fieldBlockIndex, Value: index}, {Key: fieldUpdatedAt, Value: time.Now().UTC()}}}})
	return err
}

// InsertAppliedTransaction records that a topic has applied a transaction.
func (s *Store) InsertAppliedTransaction(ctx context.Context, tx *overlay.AppliedTransaction) error {
	if tx == nil || tx.Txid == nil || !validText(tx.Topic) {
		return ErrInvalidConfig
	}
	now := time.Now().UTC()
	doc := appliedDocument{ID: s.appliedID(tx.Topic, canonicalHash(tx.Txid)), Version: schemaVersion, Scope: s.scopeID, Topic: tx.Topic, TxID: canonicalHash(tx.Txid), Proven: false, CreatedAt: now, UpdatedAt: now}
	_, err := s.db.Collection(appliedCollection).UpdateOne(ctx, bson.D{{Key: fieldID, Value: doc.ID}}, bson.D{{Key: "$setOnInsert", Value: doc}}, options.UpdateOne().SetUpsert(true))
	return err
}

// DoesAppliedTransactionExist reports whether the topic already applied tx.
func (s *Store) DoesAppliedTransactionExist(ctx context.Context, tx *overlay.AppliedTransaction) (bool, error) {
	if tx == nil || tx.Txid == nil {
		return false, nil
	}
	err := s.db.Collection(appliedCollection).FindOne(ctx, bson.D{{Key: fieldID, Value: s.appliedID(tx.Topic, canonicalHash(tx.Txid))}}).Err()
	if errors.Is(err, mongo.ErrNoDocuments) {
		return false, nil
	}
	return err == nil, err
}

// UpdateLastInteraction stores the GASP cursor score for a host and topic.
func (s *Store) UpdateLastInteraction(ctx context.Context, host, topic string, since float64) error {
	if !validText(host) || !validText(topic) {
		return ErrInvalidConfig
	}
	now := time.Now().UTC()
	id := s.cursorID(host, topic)
	_, err := s.db.Collection(cursorCollection).UpdateOne(ctx, bson.D{{Key: fieldID, Value: id}}, bson.D{
		{Key: fieldSet, Value: bson.D{{Key: fieldSince, Value: since}, {Key: fieldUpdatedAt, Value: now}}},
		{Key: "$setOnInsert", Value: bson.D{{Key: fieldVersion, Value: schemaVersion}, {Key: fieldScope, Value: s.scopeID}, {Key: fieldHost, Value: host}, {Key: fieldTopic, Value: topic}, {Key: fieldCreatedAt, Value: now}}},
	}, options.UpdateOne().SetUpsert(true))
	return err
}

// GetLastInteraction returns the stored GASP cursor, or 0 when absent.
func (s *Store) GetLastInteraction(ctx context.Context, host, topic string) (float64, error) {
	var doc cursorDocument
	err := s.db.Collection(cursorCollection).FindOne(ctx, bson.D{{Key: fieldID, Value: s.cursorID(host, topic)}}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return doc.Since, nil
}

// FindOutpointsByMerkleState lists outpoints in one merkle validation state.
func (s *Store) FindOutpointsByMerkleState(ctx context.Context, topic string, state engine.MerkleState, limit uint32) ([]*transaction.Outpoint, error) {
	filter := bson.D{{Key: fieldScope, Value: s.scopeID}, {Key: fieldTopic, Value: topic}, {Key: fieldMerkleState, Value: merkleStateString(state)}}
	opts := options.Find().SetProjection(bson.D{{Key: fieldTxID, Value: 1}, {Key: fieldOutputIndex, Value: 1}})
	if limit > 0 {
		opts.SetLimit(int64(limit))
	}
	cursor, err := s.db.Collection(outputCollection).Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer schemaCloseCursor(ctx, cursor)
	var docs []outputDocument
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	outpoints := make([]*transaction.Outpoint, 0, len(docs))
	for _, doc := range docs {
		outpoint, parseErr := outpointFromDoc(doc)
		if parseErr != nil {
			return nil, parseErr
		}
		outpoints = append(outpoints, outpoint)
	}
	return outpoints, nil
}

// ReconcileMerkleRoot updates merkle validation state for outputs at one height.
func (s *Store) ReconcileMerkleRoot(ctx context.Context, topic string, blockHeight uint32, merkleRoot *chainhash.Hash) error {
	if merkleRoot == nil {
		return ErrInvalidConfig
	}
	height, err := EncodeUint64(engine.StorageUint64(strconv.FormatUint(uint64(blockHeight), 10)))
	if err != nil {
		return err
	}
	root := canonicalHash(merkleRoot)
	cursor, err := s.db.Collection(outputCollection).Find(ctx, bson.D{{Key: fieldScope, Value: s.scopeID}, {Key: fieldTopic, Value: topic}, {Key: fieldBlockHeight, Value: height}})
	if err != nil {
		return err
	}
	defer schemaCloseCursor(ctx, cursor)
	var docs []outputDocument
	if err = cursor.All(ctx, &docs); err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, doc := range docs {
		var next string
		switch doc.MerkleRoot {
		case "":
			next = merkleStateUnmined
		case root:
			next = merkleStateValidated
		default:
			next = merkleStateInvalidated
		}
		if _, updateErr := s.db.Collection(outputCollection).UpdateOne(ctx, bson.D{{Key: fieldID, Value: doc.ID}}, bson.D{{Key: fieldSet, Value: bson.D{{Key: fieldMerkleState, Value: next}, {Key: fieldUpdatedAt, Value: now}}}}); updateErr != nil {
			return updateErr
		}
	}
	return nil
}

// LoadAncillaryBeef merges stored ancillary transaction BEEF into output.Beef.
func (s *Store) LoadAncillaryBeef(ctx context.Context, output *engine.Output) error {
	if output == nil || output.Beef == nil {
		return nil
	}
	for _, txid := range output.AncillaryTxids {
		if txid == nil {
			continue
		}
		beef, err := s.loadTransactionBeef(ctx, canonicalHash(txid))
		if err != nil || beef == nil {
			continue
		}
		if mergeErr := output.Beef.MergeBeef(beef); mergeErr != nil {
			return mergeErr
		}
	}
	return nil
}

func (s *Store) outputFromDocument(ctx context.Context, doc outputDocument, includeBEEF bool) (*engine.Output, error) {
	outpoint, err := outpointFromDoc(doc)
	if err != nil {
		return nil, err
	}
	score, err := DecodeUint64(doc.Score)
	if err != nil {
		return nil, err
	}
	output := &engine.Output{
		Outpoint:    *outpoint,
		Topic:       doc.Topic,
		Spent:       doc.Spent,
		Score:       engineScoreFromUint(score),
		MerkleState: merkleStateFromString(doc.MerkleState),
	}
	if doc.EngineScore != 0 {
		output.Score = doc.EngineScore
	}
	if doc.BlockHeight != "" {
		height, heightErr := DecodeUint64(doc.BlockHeight)
		if heightErr != nil {
			return nil, heightErr
		}
		parsed, parseErr := engine.ParseStorageUint64(height)
		if parseErr != nil {
			return nil, parseErr
		}
		if parsed > uint64(^uint32(0)) {
			return nil, engine.ErrInvalidStorageOutputIndex
		}
		output.BlockHeight = uint32(parsed)
	}
	if doc.BlockIndex != "" {
		index, indexErr := DecodeUint64(doc.BlockIndex)
		if indexErr != nil {
			return nil, indexErr
		}
		parsed, parseErr := engine.ParseStorageUint64(index)
		if parseErr != nil {
			return nil, parseErr
		}
		output.BlockIdx = parsed
	}
	if doc.MerkleRoot != "" {
		root, rootErr := parseCanonicalHash(doc.MerkleRoot)
		if rootErr != nil {
			return nil, rootErr
		}
		output.MerkleRoot = root
	}
	output.AncillaryTxids = parseHashList(doc.Ancillary)
	consumed, consumers, edgeErr := s.edgesForOutput(ctx, doc)
	if edgeErr != nil {
		return nil, edgeErr
	}
	output.OutputsConsumed = consumed
	output.ConsumedBy = consumers
	if includeBEEF {
		output.Beef, err = s.loadTransactionBeef(ctx, doc.TxID)
		if err != nil {
			return nil, err
		}
	}
	return output, nil
}

func (s *Store) edgesForOutput(ctx context.Context, doc outputDocument) ([]*transaction.Outpoint, []*transaction.Outpoint, error) {
	consumed, err := s.lookupEdges(ctx, bson.D{{Key: fieldScope, Value: s.scopeID}, {Key: fieldTopic, Value: doc.Topic}, {Key: fieldConsumerTxID, Value: doc.TxID}, {Key: fieldConsumerIndex, Value: doc.OutputIndex}}, true)
	if err != nil {
		return nil, nil, err
	}
	consumers, err := s.lookupEdges(ctx, bson.D{{Key: fieldScope, Value: s.scopeID}, {Key: fieldTopic, Value: doc.Topic}, {Key: fieldSourceTxID, Value: doc.TxID}, {Key: fieldSourceIndex, Value: doc.OutputIndex}}, false)
	return consumed, consumers, err
}

func (s *Store) lookupEdges(ctx context.Context, filter bson.D, sources bool) ([]*transaction.Outpoint, error) {
	cursor, err := s.db.Collection(edgeCollection).Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer schemaCloseCursor(ctx, cursor)
	var docs []edgeDocument
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	outpoints := make([]*transaction.Outpoint, 0, len(docs))
	for _, doc := range docs {
		txid, index := doc.ConsumerTxID, doc.ConsumerIndex
		if sources {
			txid, index = doc.SourceTxID, doc.SourceIndex
		}
		outpoint, parseErr := outpointFromParts(txid, index)
		if parseErr != nil {
			return nil, parseErr
		}
		outpoints = append(outpoints, outpoint)
	}
	return outpoints, nil
}

func (s *Store) loadTransactionBeef(ctx context.Context, txID string) (*transaction.Beef, error) {
	var doc transactionDocument
	err := s.db.Collection(transactionCollection).FindOne(ctx, bson.D{{Key: fieldID, Value: s.transactionID(txID)}}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) || doc.BeefDigest == "" {
		return nil, nil //nolint:nilnil // absent BEEF is a valid hydration miss
	}
	if err != nil {
		return nil, err
	}
	length, err := DecodeUint64(doc.BeefLength)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err = s.CopyPayload(ctx, engine.AdmissionPayloadRef{Digest: doc.BeefDigest, ByteLength: length, Kind: engine.AdmissionPayloadKind(doc.BeefKind)}, &buf); err != nil {
		return nil, err
	}
	beef, _, _, err := transaction.ParseBeef(buf.Bytes())
	return beef, err
}

func (s *Store) outpointFilter(outpoint *transaction.Outpoint, topic *string, spent *bool) bson.D {
	filter := bson.D{{Key: fieldScope, Value: s.scopeID}, {Key: fieldTxID, Value: canonicalHash(&outpoint.Txid)}, {Key: fieldOutputIndex, Value: mustEncodeUint64(engine.StorageUint64(strconv.FormatUint(uint64(outpoint.Index), 10)))}}
	if topic != nil {
		filter = append(filter, bson.E{Key: fieldTopic, Value: *topic})
	}
	if spent != nil {
		filter = append(filter, bson.E{Key: fieldSpent, Value: *spent})
	}
	return filter
}

func admissionOutpoint(outpoint *transaction.Outpoint) engine.AdmissionOutpoint {
	return engine.AdmissionOutpoint{TxID: canonicalHash(&outpoint.Txid), OutputIndex: engine.StorageUint64(strconv.FormatUint(uint64(outpoint.Index), 10))}
}

func canonicalHash(hash *chainhash.Hash) string {
	return hex.EncodeToString(hash[:])
}

func parseCanonicalHash(value string) (*chainhash.Hash, error) {
	bytes, err := hex.DecodeString(value)
	if err != nil {
		return nil, err
	}
	var hash chainhash.Hash
	if err = hash.SetBytes(bytes); err != nil {
		return nil, err
	}
	return &hash, nil
}

func hashStrings(hashes []*chainhash.Hash) []string {
	out := make([]string, 0, len(hashes))
	for _, hash := range hashes {
		if hash == nil {
			continue
		}
		out = append(out, canonicalHash(hash))
	}
	return out
}

func parseHashList(values []string) []*chainhash.Hash {
	out := make([]*chainhash.Hash, 0, len(values))
	for _, value := range values {
		hash, err := parseCanonicalHash(value)
		if err != nil {
			continue
		}
		out = append(out, hash)
	}
	return out
}

func outpointFromDoc(doc outputDocument) (*transaction.Outpoint, error) {
	return outpointFromParts(doc.TxID, doc.OutputIndex)
}

func outpointFromParts(txid, index string) (*transaction.Outpoint, error) {
	hash, err := parseCanonicalHash(txid)
	if err != nil {
		return nil, err
	}
	decoded, err := DecodeUint64(index)
	if err != nil {
		return nil, err
	}
	parsed, err := engine.ParseStorageOutputIndex(decoded)
	if err != nil {
		return nil, err
	}
	return &transaction.Outpoint{Txid: *hash, Index: parsed}, nil
}

func payloadRefFromBytes(data []byte, kind engine.AdmissionPayloadKind) engine.AdmissionPayloadRef {
	return engine.AdmissionPayloadRef{Digest: sha256Hex(data), ByteLength: engine.StorageUint64(strconv.Itoa(len(data))), Kind: kind}
}

func merkleStateString(state engine.MerkleState) string {
	switch state {
	case engine.MerkleStateUnmined:
		return merkleStateUnmined
	case engine.MerkleStateValidated:
		return merkleStateValidated
	case engine.MerkleStateInvalidated:
		return merkleStateInvalidated
	case engine.MerkleStateImmutable:
		return merkleStateImmutable
	default:
		return merkleStateUnmined
	}
}

func merkleStateFromString(value string) engine.MerkleState {
	switch value {
	case merkleStateValidated:
		return engine.MerkleStateValidated
	case merkleStateInvalidated:
		return engine.MerkleStateInvalidated
	case merkleStateImmutable:
		return engine.MerkleStateImmutable
	default:
		return engine.MerkleStateUnmined
	}
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
