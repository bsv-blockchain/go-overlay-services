package mongodb

import (
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/engine"
)

func schemaHashField() bson.D {
	return schemaString("^[0-9a-f]{64}$", 64, 64)
}

func schemaTextField() bson.D {
	return schemaString("^[^\\x00]{1,1024}$", 1, 1024)
}

func schemaPayloadKindEnum() bson.D {
	return schemaEnum("raw-transaction", "merkle-path", "beef-manifest", "locking-script", "outbox-data")
}

func schemaPayloadRef() bson.D {
	return bson.D{
		{Key: "bsonType", Value: "object"},
		{Key: "additionalProperties", Value: false},
		{Key: "required", Value: bson.A{fieldDigest, fieldByteLength, fieldKind}},
		{Key: "properties", Value: bson.D{
			{Key: fieldDigest, Value: schemaHashField()},
			{Key: fieldByteLength, Value: schemaUint64()},
			{Key: fieldKind, Value: schemaPayloadKindEnum()},
		}},
	}
}

func schemaHashArray() bson.D {
	return bson.D{{Key: "bsonType", Value: "array"}, {Key: "maxItems", Value: int32(maxAdmissionArrayLength)}, {Key: "items", Value: schemaHashField()}}
}

func schemaOutputValidator() bson.D {
	return schemaDocument([]string{fieldID, fieldVersion, fieldScope, fieldTopic, fieldTxID, fieldOutputIndex, fieldSatoshis, fieldScore, fieldEngineScore, fieldSpent, fieldServing, fieldSpendVersion, fieldMerkleState, fieldCreatedAt, fieldUpdatedAt}, bson.D{
		{Key: fieldID, Value: schemaHashField()},
		{Key: fieldVersion, Value: schemaIntEnum()},
		{Key: fieldScope, Value: schemaHashField()},
		{Key: fieldTopic, Value: schemaTextField()},
		{Key: fieldTxID, Value: schemaHashField()},
		{Key: fieldOutputIndex, Value: schemaUint64()},
		{Key: fieldSatoshis, Value: schemaUint64()},
		{Key: fieldScore, Value: schemaUint64()},
		{Key: fieldEngineScore, Value: schemaType("double")},
		{Key: fieldSpent, Value: schemaType("bool")},
		{Key: fieldServing, Value: schemaType("bool")},
		{Key: fieldSpendVersion, Value: schemaTextField()},
		{Key: fieldSpentBy, Value: schemaHashField()},
		{Key: fieldMerkleState, Value: schemaEnum(merkleStateUnmined, merkleStateValidated, merkleStateInvalidated, merkleStateImmutable)},
		{Key: fieldCreatedAt, Value: schemaType("date")},
		{Key: fieldUpdatedAt, Value: schemaType("date")},
		{Key: fieldScriptDigest, Value: schemaHashField()},
		{Key: fieldScriptKind, Value: schemaPayloadKindEnum()},
		{Key: fieldScriptOffset, Value: schemaUint64()},
		{Key: fieldScriptLength, Value: schemaUint64()},
		{Key: fieldBlockHeight, Value: schemaUint64()},
		{Key: fieldBlockIndex, Value: schemaUint64()},
		{Key: fieldMerkleRoot, Value: schemaHashField()},
		{Key: fieldAncillary, Value: schemaHashArray()},
	})
}

func schemaEdgeValidator() bson.D {
	return schemaDocument([]string{fieldID, fieldVersion, fieldScope, fieldTopic, fieldSourceTxID, fieldSourceIndex, fieldConsumerTxID, fieldConsumerIndex, fieldCreatedAt}, bson.D{
		{Key: fieldID, Value: schemaHashField()},
		{Key: fieldVersion, Value: schemaIntEnum()},
		{Key: fieldScope, Value: schemaHashField()},
		{Key: fieldTopic, Value: schemaTextField()},
		{Key: fieldSourceTxID, Value: schemaHashField()},
		{Key: fieldSourceIndex, Value: schemaUint64()},
		{Key: fieldConsumerTxID, Value: schemaHashField()},
		{Key: fieldConsumerIndex, Value: schemaUint64()},
		{Key: fieldCreatedAt, Value: schemaType("date")},
	})
}

func schemaAppliedValidator() bson.D {
	return schemaDocument([]string{fieldID, fieldVersion, fieldScope, fieldTopic, fieldTxID, fieldProven, fieldCreatedAt, fieldUpdatedAt}, bson.D{
		{Key: fieldID, Value: schemaHashField()},
		{Key: fieldVersion, Value: schemaIntEnum()},
		{Key: fieldScope, Value: schemaHashField()},
		{Key: fieldTopic, Value: schemaTextField()},
		{Key: fieldTxID, Value: schemaHashField()},
		{Key: fieldProven, Value: schemaType("bool")},
		{Key: fieldCreatedAt, Value: schemaType("date")},
		{Key: fieldUpdatedAt, Value: schemaType("date")},
		{Key: fieldFirstSeenHeight, Value: schemaUint64()},
		{Key: fieldProofDigest, Value: schemaHashField()},
		{Key: fieldProofKind, Value: schemaPayloadKindEnum()},
		{Key: fieldProofLength, Value: schemaUint64()},
		{Key: fieldBlockHeight, Value: schemaUint64()},
		{Key: fieldBlockHash, Value: schemaHashField()},
		{Key: fieldBlockIndex, Value: schemaUint64()},
		{Key: fieldMerkleRoot, Value: schemaHashField()},
	})
}

func schemaTransactionValidator() bson.D {
	return schemaDocument([]string{fieldID, fieldVersion, fieldChain, fieldTxID, fieldCreatedAt, fieldUpdatedAt}, bson.D{
		{Key: fieldID, Value: schemaHashField()},
		{Key: fieldVersion, Value: schemaIntEnum()},
		{Key: fieldChain, Value: schemaHashField()},
		{Key: fieldTxID, Value: schemaHashField()},
		{Key: fieldCreatedAt, Value: schemaType("date")},
		{Key: fieldUpdatedAt, Value: schemaType("date")},
		{Key: fieldBeefDigest, Value: schemaHashField()},
		{Key: fieldBeefLength, Value: schemaUint64()},
		{Key: fieldBeefKind, Value: schemaPayloadKindEnum()},
		{Key: fieldBlockHeight, Value: schemaUint64()},
		{Key: fieldBlockHash, Value: schemaHashField()},
		{Key: fieldBlockIndex, Value: schemaUint64()},
		{Key: fieldMerkleRoot, Value: schemaHashField()},
		{Key: fieldAncillary, Value: schemaHashArray()},
	})
}

func schemaOutboxValidator() bson.D {
	return schemaDocument([]string{fieldID, fieldVersion, fieldScope, fieldEventID, fieldKind, fieldTarget, fieldState, fieldOperationID, fieldCreatedAt, fieldUpdatedAt}, bson.D{
		{Key: fieldID, Value: schemaHashField()},
		{Key: fieldVersion, Value: schemaIntEnum()},
		{Key: fieldScope, Value: schemaHashField()},
		{Key: fieldEventID, Value: schemaTextField()},
		{Key: fieldKind, Value: schemaEnum(string(engine.AdmissionOutboxLookup), string(engine.AdmissionOutboxPropagation))},
		{Key: fieldTarget, Value: schemaTextField()},
		{Key: fieldState, Value: schemaEnum(outboxStatePending)},
		{Key: fieldOperationID, Value: schemaTextField()},
		{Key: fieldCreatedAt, Value: schemaType("date")},
		{Key: fieldUpdatedAt, Value: schemaType("date")},
		{Key: fieldPayloads, Value: bson.D{{Key: "bsonType", Value: "array"}, {Key: "maxItems", Value: int32(maxAdmissionArrayLength)}, {Key: "items", Value: schemaPayloadRef()}}},
	})
}

func schemaReadValidator() bson.D {
	return schemaDocument([]string{fieldID, fieldVersion, fieldScope, fieldTopic, fieldKey, fieldReadVersion, fieldCreatedAt, fieldUpdatedAt}, bson.D{
		{Key: fieldID, Value: schemaHashField()},
		{Key: fieldVersion, Value: schemaIntEnum()},
		{Key: fieldScope, Value: schemaHashField()},
		{Key: fieldTopic, Value: schemaTextField()},
		{Key: fieldKey, Value: schemaTextField()},
		{Key: fieldReadVersion, Value: schemaTextField()},
		{Key: fieldCreatedAt, Value: schemaType("date")},
		{Key: fieldUpdatedAt, Value: schemaType("date")},
	})
}

func schemaFenceValidator() bson.D {
	return schemaDocument([]string{fieldID, fieldVersion, fieldScope, fieldTopic, fieldChainEpoch, fieldTopicHistoryGeneration, fieldCreatedAt, fieldUpdatedAt}, bson.D{
		{Key: fieldID, Value: schemaHashField()},
		{Key: fieldVersion, Value: schemaIntEnum()},
		{Key: fieldScope, Value: schemaHashField()},
		{Key: fieldTopic, Value: schemaTextField()},
		{Key: fieldChainEpoch, Value: schemaUint64()},
		{Key: fieldTopicHistoryGeneration, Value: schemaUint64()},
		{Key: fieldCreatedAt, Value: schemaType("date")},
		{Key: fieldUpdatedAt, Value: schemaType("date")},
		{Key: fieldAffectedFromHeight, Value: schemaUint64()},
		{Key: fieldCheckpoint, Value: schemaTextField()},
	})
}

func schemaLeaseValidator() bson.D {
	return schemaDocument([]string{fieldID, fieldVersion, fieldScope, fieldTopic, fieldPeerID, fieldJobID, fieldChainEpoch, fieldTopicHistoryGeneration, fieldToken, fieldExpiresAtMS, fieldCreatedAt, fieldUpdatedAt}, bson.D{
		{Key: fieldID, Value: schemaHashField()},
		{Key: fieldVersion, Value: schemaIntEnum()},
		{Key: fieldScope, Value: schemaHashField()},
		{Key: fieldTopic, Value: schemaTextField()},
		{Key: fieldPeerID, Value: schemaTextField()},
		{Key: fieldJobID, Value: schemaTextField()},
		{Key: fieldChainEpoch, Value: schemaUint64()},
		{Key: fieldTopicHistoryGeneration, Value: schemaUint64()},
		{Key: fieldToken, Value: schemaUint64()},
		{Key: fieldExpiresAtMS, Value: schemaUint64()},
		{Key: fieldCreatedAt, Value: schemaType("date")},
		{Key: fieldUpdatedAt, Value: schemaType("date")},
	})
}

func schemaCursorValidator() bson.D {
	return schemaDocument([]string{fieldID, fieldVersion, fieldScope, fieldHost, fieldTopic, fieldSince, fieldCreatedAt, fieldUpdatedAt}, bson.D{
		{Key: fieldID, Value: schemaHashField()},
		{Key: fieldVersion, Value: schemaIntEnum()},
		{Key: fieldScope, Value: schemaHashField()},
		{Key: fieldHost, Value: schemaTextField()},
		{Key: fieldTopic, Value: schemaTextField()},
		{Key: fieldSince, Value: schemaType("double")},
		{Key: fieldCreatedAt, Value: schemaType("date")},
		{Key: fieldUpdatedAt, Value: schemaType("date")},
	})
}

func schemaOutputIndexes() []mongo.IndexModel {
	return []mongo.IndexModel{
		{Keys: bson.D{{Key: fieldScope, Value: 1}, {Key: fieldTopic, Value: 1}, {Key: fieldSpent, Value: 1}, {Key: fieldServing, Value: 1}, {Key: fieldEngineScore, Value: 1}}, Options: options.Index().SetName("scope_topic_utxo_score")},
		{Keys: bson.D{{Key: fieldScope, Value: 1}, {Key: fieldTxID, Value: 1}}, Options: options.Index().SetName("scope_txid")},
		{Keys: bson.D{{Key: fieldScope, Value: 1}, {Key: fieldTopic, Value: 1}, {Key: fieldMerkleState, Value: 1}}, Options: options.Index().SetName("scope_topic_merkle")},
	}
}

func schemaEdgeIndexes() []mongo.IndexModel {
	return []mongo.IndexModel{
		{Keys: bson.D{{Key: fieldScope, Value: 1}, {Key: fieldTopic, Value: 1}, {Key: fieldSourceTxID, Value: 1}, {Key: fieldSourceIndex, Value: 1}, {Key: fieldConsumerTxID, Value: 1}, {Key: fieldConsumerIndex, Value: 1}}, Options: options.Index().SetName("scope_edge_identity").SetUnique(true)},
		{Keys: bson.D{{Key: fieldScope, Value: 1}, {Key: fieldTopic, Value: 1}, {Key: fieldConsumerTxID, Value: 1}, {Key: fieldConsumerIndex, Value: 1}}, Options: options.Index().SetName("scope_edge_consumer")},
	}
}

func schemaAppliedIndexes() []mongo.IndexModel {
	return []mongo.IndexModel{
		{Keys: bson.D{{Key: fieldScope, Value: 1}, {Key: fieldTopic, Value: 1}, {Key: fieldTxID, Value: 1}}, Options: options.Index().SetName("scope_topic_txid").SetUnique(true)},
	}
}

func schemaTransactionIndexes() []mongo.IndexModel {
	return []mongo.IndexModel{
		{Keys: bson.D{{Key: fieldChain, Value: 1}, {Key: fieldBlockHash, Value: 1}, {Key: fieldBlockHeight, Value: 1}}, Options: options.Index().SetName("chain_block")},
	}
}

func schemaOutboxIndexes() []mongo.IndexModel {
	return []mongo.IndexModel{
		{Keys: bson.D{{Key: fieldScope, Value: 1}, {Key: fieldState, Value: 1}, {Key: fieldUpdatedAt, Value: 1}}, Options: options.Index().SetName("scope_state_updated")},
	}
}

func schemaReadIndexes() []mongo.IndexModel {
	return []mongo.IndexModel{
		{Keys: bson.D{{Key: fieldScope, Value: 1}, {Key: fieldTopic, Value: 1}, {Key: fieldKey, Value: 1}}, Options: options.Index().SetName("scope_topic_key").SetUnique(true)},
	}
}

func schemaFenceIndexes() []mongo.IndexModel {
	return []mongo.IndexModel{
		{Keys: bson.D{{Key: fieldScope, Value: 1}, {Key: fieldTopic, Value: 1}}, Options: options.Index().SetName("scope_topic").SetUnique(true)},
	}
}

func schemaLeaseIndexes() []mongo.IndexModel {
	return []mongo.IndexModel{
		{Keys: bson.D{{Key: fieldScope, Value: 1}, {Key: fieldTopic, Value: 1}, {Key: fieldPeerID, Value: 1}, {Key: fieldJobID, Value: 1}}, Options: options.Index().SetName("scope_lease_identity").SetUnique(true)},
	}
}

func schemaCursorIndexes() []mongo.IndexModel {
	return []mongo.IndexModel{
		{Keys: bson.D{{Key: fieldScope, Value: 1}, {Key: fieldHost, Value: 1}, {Key: fieldTopic, Value: 1}}, Options: options.Index().SetName("scope_host_topic").SetUnique(true)},
	}
}
