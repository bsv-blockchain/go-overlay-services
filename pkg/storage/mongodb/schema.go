package mongodb

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const (
	hex64Pattern = "^[0-9a-f]{64}$"
	textPattern  = `^[^\x00]{1,1024}$`
)

// bootstrap creates and verifies only the version-one persistence foundation.
// Existing collections are never altered: a mismatch is an operator action.
func (s *Store) bootstrap(ctx context.Context) error {
	if s == nil || s.client == nil || s.db == nil || ctx == nil {
		return ErrInvalidConfig
	}
	if err := schemaTopology(ctx, s.db); err != nil {
		return err
	}
	if err := s.schemaEnsureCollection(ctx, schemaCollection, schemaValidator()); err != nil {
		return err
	}
	ledger := schemaLedger(s)
	if err := s.schemaCheckLedger(ctx, ledger); err != nil {
		return err
	}
	for _, spec := range []schemaCollectionSpec{
		{name: payloadCollection, validator: schemaPayloadValidator(), indexes: schemaPayloadIndexes()},
		{name: referenceCollection, validator: schemaReferenceValidator(), indexes: schemaReferenceIndexes()},
		{name: operationCollection, validator: schemaOperationValidator(), indexes: schemaOperationIndexes()},
		{name: blobBucketName + ".files", validator: schemaGridFSFilesValidator(), indexes: schemaGridFSFilesIndexes()},
		{name: blobBucketName + ".chunks", validator: schemaGridFSChunksValidator(), indexes: schemaGridFSChunksIndexes()},
	} {
		if err := s.schemaEnsureCollection(ctx, spec.name, spec.validator); err != nil {
			return err
		}
		if err := s.schemaEnsureIndexes(ctx, spec.name, spec.indexes); err != nil {
			return err
		}
	}
	if err := s.schemaProbeTransaction(ctx); err != nil {
		return err
	}
	if err := s.schemaWriteLedger(ctx, ledger); err != nil {
		return err
	}
	return nil
}

type schemaCollectionSpec struct {
	name      string
	validator bson.D
	indexes   []mongo.IndexModel
}

func schemaTopology(ctx context.Context, db *mongo.Database) error {
	var hello struct {
		Writable bool   `bson:"isWritablePrimary"`
		SetName  string `bson:"setName"`
		Msg      string `bson:"msg"`
	}
	if err := db.RunCommand(ctx, bson.D{{Key: "hello", Value: 1}}).Decode(&hello); err != nil {
		return fmt.Errorf("%w: hello: %w", ErrIncompatibleSchema, err)
	}
	if !hello.Writable || hello.SetName == "" || hello.Msg == "isdbgrid" {
		return fmt.Errorf("%w: writable unsharded replica set required", ErrIncompatibleSchema)
	}
	return nil
}

func (s *Store) schemaEnsureCollection(ctx context.Context, name string, validator bson.D) error {
	var docs []bson.M
	cursor, err := s.db.ListCollections(ctx, bson.D{{Key: "name", Value: name}})
	if err != nil {
		return err
	}
	defer schemaCloseCursor(ctx, cursor)
	if err = cursor.All(ctx, &docs); err != nil {
		return err
	}
	if len(docs) == 0 {
		err = s.db.CreateCollection(ctx, name, options.CreateCollection().SetCollation(&options.Collation{Locale: "simple"}).SetValidator(validator).SetValidationLevel("strict").SetValidationAction("error"))
		if err == nil {
			return nil
		}
		// A concurrent bootstrap may have created it; inspect rather than overwrite.
		cursor, err = s.db.ListCollections(ctx, bson.D{{Key: "name", Value: name}})
		if err != nil {
			return fmt.Errorf("create collection %s: %w", name, err)
		}
		defer schemaCloseCursor(ctx, cursor)
		docs = nil
		if err = cursor.All(ctx, &docs); err != nil || len(docs) == 0 {
			return fmt.Errorf("create collection %s: %w", name, err)
		}
	}
	if len(docs) != 1 || !schemaCollectionCompatible(docs[0], validator) {
		return fmt.Errorf("%w: collection %s options", ErrIncompatibleSchema, name)
	}
	return nil
}

func schemaCollectionCompatible(doc bson.M, validator bson.D) bool {
	if doc["type"] != "collection" {
		return false
	}
	optionsDoc, ok := schemaMap(doc["options"])
	if !ok {
		return false
	}
	for key := range optionsDoc {
		switch key {
		case "validator", "validationLevel", "validationAction", "collation":
		default:
			return false
		}
	}
	collation, hasCollation := schemaMap(optionsDoc["collation"])
	if hasCollation && collation["locale"] != "simple" {
		return false
	}
	if level, exists := optionsDoc["validationLevel"]; exists && level != "strict" {
		return false
	}
	if action, exists := optionsDoc["validationAction"]; exists && action != "error" {
		return false
	}
	actual, ok := schemaMap(optionsDoc["validator"])
	if !ok {
		return false
	}
	var expected bson.M
	bytes, err := bson.Marshal(validator)
	if err != nil || bson.Unmarshal(bytes, &expected) != nil {
		return false
	}
	return reflect.DeepEqual(actual, expected)
}

func schemaMap(value any) (bson.M, bool) {
	encoded, err := bson.Marshal(value)
	if err != nil {
		return nil, false
	}
	var result bson.M
	if bson.Unmarshal(encoded, &result) != nil {
		return nil, false
	}
	return result, true
}

func (s *Store) schemaEnsureIndexes(ctx context.Context, name string, wanted []mongo.IndexModel) error {
	view := s.db.Collection(name).Indexes()
	existing, err := schemaListIndexes(ctx, view)
	if err != nil {
		return err
	}
	if err = schemaRejectUnexpectedIndexes(existing, wanted, name); err != nil {
		return err
	}
	return schemaCreateMissingIndexes(ctx, view, existing, wanted, name)
}

func schemaListIndexes(ctx context.Context, view mongo.IndexView) (map[string]bson.M, error) {
	cursor, err := view.List(ctx)
	if err != nil {
		return nil, err
	}
	defer schemaCloseCursor(ctx, cursor)
	var documents []bson.D
	if err = cursor.All(ctx, &documents); err != nil {
		return nil, err
	}
	existing := make(map[string]bson.M, len(documents))
	for _, document := range documents {
		current := make(bson.M, len(document))
		for _, element := range document {
			current[element.Key] = element.Value
		}
		indexName, ok := current["name"].(string)
		if !ok {
			return nil, ErrIncompatibleSchema
		}
		existing[indexName] = current
	}
	return existing, nil
}

func schemaRejectUnexpectedIndexes(existing map[string]bson.M, wanted []mongo.IndexModel, name string) error {
	required := make(map[string]mongo.IndexModel, len(wanted))
	for _, model := range wanted {
		required[schemaIndexName(model)] = model
	}
	// Inspect before creating anything, including unexpected restrictive indexes.
	for indexName, current := range existing {
		if indexName == "_id_" {
			continue
		}
		model, ok := required[indexName]
		if !ok || !schemaIndexCompatible(current, model) {
			return fmt.Errorf("%w: index %s on %s", ErrIncompatibleSchema, indexName, name)
		}
	}
	return nil
}

func schemaCreateMissingIndexes(ctx context.Context, view mongo.IndexView, existing map[string]bson.M, wanted []mongo.IndexModel, name string) error {
	for _, model := range wanted {
		if _, exists := existing[schemaIndexName(model)]; exists {
			continue
		}
		if _, err := view.CreateOne(ctx, model, options.CreateIndexes().SetCommitQuorumMajority()); err != nil {
			return fmt.Errorf("%w: create index on %s: %w", ErrIncompatibleSchema, name, err)
		}
	}
	return nil
}

func schemaIndexOptions(model mongo.IndexModel) *options.IndexOptions {
	opts := &options.IndexOptions{}
	for _, apply := range model.Options.List() {
		_ = apply(opts)
	}
	return opts
}
func schemaIndexName(model mongo.IndexModel) string { return *schemaIndexOptions(model).Name }
func schemaIndexCompatible(current bson.M, model mongo.IndexModel) bool {
	want, err := bson.Marshal(model.Keys)
	if err != nil {
		return false
	}
	actual, err := bson.Marshal(current["key"])
	if err != nil || !bytes.Equal(want, actual) {
		return false
	}
	opts := schemaIndexOptions(model)
	for _, field := range []struct {
		name     string
		expected *bool
	}{{"unique", opts.Unique}, {"sparse", opts.Sparse}, {"hidden", opts.Hidden}} {
		wantValue := field.expected != nil && *field.expected
		actualValue, exists := current[field.name]
		if exists && actualValue != wantValue {
			return false
		}
		if !exists && wantValue {
			return false
		}
	}
	// Version one uses only complete, non-TTL indexes under simple collation.
	for _, key := range []string{"expireAfterSeconds", "partialFilterExpression", "collation", "wildcardProjection", "weights", "default_language", "language_override", "prepareUnique"} {
		if _, exists := current[key]; exists {
			return false
		}
	}
	return true
}

func schemaCloseCursor(ctx context.Context, cursor *mongo.Cursor) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	_ = cursor.Close(cleanupCtx)
}

func schemaLedger(s *Store) bson.D {
	fingerprint := s.schemaFingerprint()
	return bson.D{{Key: fieldID, Value: tupleID("schema", s.scopeID)}, {Key: "type", Value: "schema"}, {Key: "schemaVersion", Value: schemaVersion}, {Key: "scopeID", Value: s.scopeID}, {Key: "fingerprint", Value: fingerprint}, {Key: "network", Value: s.config.Scope.Network}, {Key: "genesisHash", Value: s.config.Scope.GenesisHash}, {Key: "nodeID", Value: s.config.Scope.NodeID}, {Key: "ready", Value: true}, {Key: fieldCreatedAt, Value: time.Now().UTC()}}
}

func (s *Store) schemaFingerprint() string {
	parts := []string{"mongo-schema", fmt.Sprint(schemaVersion)}
	for _, spec := range []schemaCollectionSpec{
		{name: schemaCollection, validator: schemaValidator()},
		{name: payloadCollection, validator: schemaPayloadValidator(), indexes: schemaPayloadIndexes()},
		{name: referenceCollection, validator: schemaReferenceValidator(), indexes: schemaReferenceIndexes()},
		{name: operationCollection, validator: schemaOperationValidator(), indexes: schemaOperationIndexes()},
		{name: blobBucketName + ".files", validator: schemaGridFSFilesValidator(), indexes: schemaGridFSFilesIndexes()},
		{name: blobBucketName + ".chunks", validator: schemaGridFSChunksValidator(), indexes: schemaGridFSChunksIndexes()},
	} {
		encoded, err := bson.Marshal(bson.D{{Key: "name", Value: spec.name}, {Key: "validator", Value: spec.validator}})
		if err != nil {
			return ""
		}
		parts = append(parts, hex.EncodeToString(encoded))
		for _, index := range spec.indexes {
			parts = append(parts, schemaIndexFingerprint(index))
		}
	}
	return tupleID(parts...)
}

func schemaIndexFingerprint(model mongo.IndexModel) string {
	key, err := bson.Marshal(model.Keys)
	if err != nil {
		return ""
	}
	opts := &options.IndexOptions{}
	for _, apply := range model.Options.List() {
		_ = apply(opts)
	}
	name, unique, sparse, hidden := "", false, false, false
	if opts.Name != nil {
		name = *opts.Name
	}
	if opts.Unique != nil {
		unique = *opts.Unique
	}
	if opts.Sparse != nil {
		sparse = *opts.Sparse
	}
	if opts.Hidden != nil {
		hidden = *opts.Hidden
	}
	partial := ""
	if opts.PartialFilterExpression != nil {
		encoded, marshalErr := bson.Marshal(opts.PartialFilterExpression)
		if marshalErr != nil {
			return ""
		}
		partial = hex.EncodeToString(encoded)
	}
	return tupleID("index", hex.EncodeToString(key), name, fmt.Sprint(unique), fmt.Sprint(sparse), fmt.Sprint(hidden), partial)
}

func schemaValue(document bson.D, key string) any {
	for _, element := range document {
		if element.Key == key {
			return element.Value
		}
	}
	return nil
}

func (s *Store) schemaCheckLedger(ctx context.Context, ledger bson.D) error {
	var doc bson.M
	err := s.db.Collection(schemaCollection).FindOne(ctx, bson.D{{Key: fieldID, Value: tupleID("schema", s.scopeID)}}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, key := range []string{"schemaVersion", "scopeID", "fingerprint", "network", "genesisHash", "nodeID", "ready"} {
		want := schemaValue(ledger, key)
		if !reflect.DeepEqual(doc[key], want) {
			return fmt.Errorf("%w: schema ledger %s", ErrIncompatibleSchema, key)
		}
	}
	return nil
}

func (s *Store) schemaWriteLedger(ctx context.Context, ledger bson.D) error {
	_, err := s.db.Collection(schemaCollection).UpdateOne(ctx, bson.D{{Key: fieldID, Value: schemaValue(ledger, "_id")}}, bson.D{{Key: "$setOnInsert", Value: ledger}}, options.UpdateOne().SetUpsert(true))
	if err != nil {
		return err
	}
	return s.schemaCheckLedger(ctx, ledger)
}

func (s *Store) schemaProbeTransaction(ctx context.Context) error {
	id := tupleID("probe", s.scopeID, s.ownerID)
	coll := s.db.Collection(schemaCollection)
	outcome, err := s.runTransaction(ctx, func(sessionCtx context.Context) error {
		doc := bson.D{{Key: fieldID, Value: id}, {Key: "type", Value: "probe"}, {Key: "scopeID", Value: s.scopeID}, {Key: fieldCreatedAt, Value: time.Now().UTC()}}
		if _, insertErr := coll.InsertOne(sessionCtx, doc); insertErr != nil {
			return insertErr
		}
		if readErr := coll.FindOne(sessionCtx, bson.D{{Key: fieldID, Value: id}}).Err(); readErr != nil {
			return readErr
		}
		_, deleteErr := coll.DeleteOne(sessionCtx, bson.D{{Key: fieldID, Value: id}})
		return deleteErr
	})
	if outcome != transactionCommitted || err != nil {
		return fmt.Errorf("%w: majority snapshot probe: %w", ErrIncompatibleSchema, err)
	}
	return nil
}

func schemaString(pattern string, minimum, maximum int32) bson.D {
	return bson.D{{Key: "bsonType", Value: "string"}, {Key: "minLength", Value: minimum}, {Key: "maxLength", Value: maximum}, {Key: "pattern", Value: pattern}}
}

func schemaHash() bson.D {
	return schemaString(hex64Pattern, 64, 64)
}

func schemaText() bson.D {
	return schemaString(textPattern, 1, 1024)
}

func schemaBase(required []string, properties bson.D) bson.D {
	return bson.D{{Key: "$jsonSchema", Value: bson.D{{Key: "bsonType", Value: "object"}, {Key: "additionalProperties", Value: false}, {Key: "required", Value: required}, {Key: "properties", Value: properties}}}}
}

func schemaPayloadValidator() bson.D {
	return schemaDocument([]string{fieldID, fieldVersion, fieldChain, "digest", fieldLength, fieldState, fieldOwner, fieldToken, fieldLeaseUntil, "guard", "createdAt", fieldUpdatedAt}, bson.D{{Key: fieldID, Value: schemaHash()}, {Key: fieldVersion, Value: schemaIntEnum()}, {Key: fieldChain, Value: schemaHash()}, {Key: fieldDigest, Value: schemaHash()}, {Key: fieldLength, Value: schemaUint64()}, {Key: fieldState, Value: schemaEnum("uploading", "ready", "deleting", "deleted")}, {Key: fieldOwner, Value: schemaText()}, {Key: fieldToken, Value: schemaUint64()}, {Key: fieldLeaseUntil, Value: schemaType("date")}, {Key: fieldGuard, Value: schemaType("objectId")}, {Key: fieldCreatedAt, Value: schemaType("date")}, {Key: fieldUpdatedAt, Value: schemaType("date")}, {Key: "fileId", Value: schemaType("objectId")}, {Key: "inlineData", Value: schemaType("binData")}, {Key: "blobOwner", Value: schemaText()}, {Key: "blobToken", Value: schemaUint64()}})
}

func schemaReferenceValidator() bson.D {
	return schemaDocument([]string{fieldID, fieldVersion, fieldChain, fieldScope, "digest", fieldLength, "kind", "ownerKind", "ownerID", "createdAt"}, bson.D{{Key: fieldID, Value: schemaHash()}, {Key: fieldVersion, Value: schemaIntEnum()}, {Key: fieldChain, Value: schemaHash()}, {Key: fieldScope, Value: schemaHash()}, {Key: fieldDigest, Value: schemaHash()}, {Key: fieldLength, Value: schemaUint64()}, {Key: "kind", Value: schemaEnum("raw-transaction", "merkle-path", "beef-manifest", "locking-script", "outbox-data")}, {Key: "ownerKind", Value: schemaEnum("transaction", "applied-history", "output", "gasp-graph", "gasp-node", "basm-job", "lookup-outbox", "propagation-outbox", "manifest", "pin")}, {Key: "ownerID", Value: schemaText()}, {Key: fieldCreatedAt, Value: schemaType("date")}})
}

func schemaOperationValidator() bson.D {
	return schemaDocument([]string{fieldID, fieldVersion, fieldScope, "operationID", "digest", fieldState, fieldAttempt, fieldOwner, fieldToken, fieldLeaseUntil, "guard", "createdAt", fieldUpdatedAt}, bson.D{{Key: fieldID, Value: schemaHash()}, {Key: fieldVersion, Value: schemaIntEnum()}, {Key: fieldScope, Value: schemaHash()}, {Key: "operationID", Value: schemaText()}, {Key: fieldDigest, Value: schemaHash()}, {Key: fieldState, Value: schemaEnum("pending", "committed", "aborted")}, {Key: fieldAttempt, Value: schemaText()}, {Key: fieldOwner, Value: schemaText()}, {Key: fieldToken, Value: schemaUint64()}, {Key: fieldLeaseUntil, Value: schemaType("date")}, {Key: fieldGuard, Value: schemaType("objectId")}, {Key: fieldCreatedAt, Value: schemaType("date")}, {Key: fieldUpdatedAt, Value: schemaType("date")}, {Key: "receipt", Value: schemaType("binData")}})
}

func schemaValidator() bson.D {
	return schemaDocument([]string{fieldID, "type", "scopeID", "createdAt"}, bson.D{{Key: fieldID, Value: schemaHash()}, {Key: "type", Value: schemaEnum("schema", "probe")}, {Key: "scopeID", Value: schemaHash()}, {Key: fieldCreatedAt, Value: schemaType("date")}, {Key: "schemaVersion", Value: schemaIntEnum()}, {Key: "fingerprint", Value: schemaHash()}, {Key: "network", Value: schemaText()}, {Key: "genesisHash", Value: schemaHash()}, {Key: "nodeID", Value: schemaText()}, {Key: "ready", Value: schemaType("bool")}})
}

func schemaGridFSFilesValidator() bson.D {
	return schemaDocument([]string{fieldID, fieldLength, "chunkSize", "uploadDate", "filename", "metadata"}, bson.D{{Key: fieldID, Value: schemaType("objectId")}, {Key: fieldLength, Value: schemaType("long")}, {Key: "chunkSize", Value: schemaType("int")}, {Key: "uploadDate", Value: schemaType("date")}, {Key: "filename", Value: schemaText()}, {Key: "metadata", Value: schemaValue(schemaBase([]string{fieldChain, "digest", "byteLength", fieldOwner, fieldToken, fieldState}, bson.D{{Key: fieldChain, Value: schemaHash()}, {Key: fieldDigest, Value: schemaHash()}, {Key: "byteLength", Value: schemaString("^(0|[1-9][0-9]{0,12})$", 1, 13)}, {Key: fieldOwner, Value: schemaText()}, {Key: fieldToken, Value: schemaUint64()}, {Key: fieldState, Value: schemaEnum("staged", "published")}}), "$jsonSchema")}})
}

func schemaGridFSChunksValidator() bson.D {
	return schemaDocument([]string{fieldID, fieldFilesID, "n", "data"}, bson.D{{Key: fieldID, Value: schemaType("objectId")}, {Key: fieldFilesID, Value: schemaType("objectId")}, {Key: "n", Value: schemaType("int")}, {Key: "data", Value: schemaType("binData")}})
}
func schemaType(value string) bson.D { return bson.D{{Key: "bsonType", Value: value}} }
func schemaEnum(values ...string) bson.D {
	array := make(bson.A, len(values))
	for i, v := range values {
		array[i] = v
	}
	return bson.D{{Key: "enum", Value: array}}
}

func schemaIntEnum() bson.D {
	return bson.D{{Key: "bsonType", Value: "int"}, {Key: "enum", Value: bson.A{schemaVersion}}}
}

func schemaDocument(required []string, properties bson.D) bson.D {
	return schemaBase(required, properties)
}

func schemaPayloadIndexes() []mongo.IndexModel {
	return []mongo.IndexModel{{Keys: bson.D{{Key: fieldChain, Value: 1}, {Key: fieldState, Value: 1}, {Key: fieldUpdatedAt, Value: 1}}, Options: options.Index().SetName("chain_state_updated")}}
}

func schemaReferenceIndexes() []mongo.IndexModel {
	return []mongo.IndexModel{{Keys: bson.D{{Key: fieldChain, Value: 1}, {Key: fieldDigest, Value: 1}}, Options: options.Index().SetName("chain_digest")}, {Keys: bson.D{{Key: fieldScope, Value: 1}, {Key: "ownerKind", Value: 1}, {Key: "ownerID", Value: 1}, {Key: fieldDigest, Value: 1}, {Key: "kind", Value: 1}}, Options: options.Index().SetName("scope_owner_digest_kind").SetUnique(true)}}
}

func schemaOperationIndexes() []mongo.IndexModel {
	return []mongo.IndexModel{{Keys: bson.D{{Key: fieldScope, Value: 1}, {Key: fieldState, Value: 1}, {Key: fieldLeaseUntil, Value: 1}}, Options: options.Index().SetName("scope_state_lease")}, {Keys: bson.D{{Key: fieldScope, Value: 1}, {Key: "operationID", Value: 1}}, Options: options.Index().SetName("scope_operation").SetUnique(true)}}
}

func schemaGridFSFilesIndexes() []mongo.IndexModel {
	return []mongo.IndexModel{{Keys: bson.D{{Key: "filename", Value: 1}, {Key: "uploadDate", Value: 1}}, Options: options.Index().SetName("filename_uploadDate")}}
}

func schemaGridFSChunksIndexes() []mongo.IndexModel {
	return []mongo.IndexModel{{Keys: bson.D{{Key: fieldFilesID, Value: 1}, {Key: "n", Value: 1}}, Options: options.Index().SetName("files_id_n").SetUnique(true)}}
}

func schemaUint64() bson.D {
	const maximum = "18446744073709551615"
	alternatives := []string{maximum}
	for i, digit := range maximum {
		if digit == '0' {
			continue
		}
		alternatives = append(alternatives, maximum[:i]+"[0-"+string(digit-1)+"][0-9]{"+strconv.Itoa(len(maximum)-i-1)+"}")
	}
	return schemaString("^(?:"+strings.Join(alternatives, "|")+")$", 20, 20)
}
