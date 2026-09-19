// Package mongodb provides an opt-in MongoDB engine.Storage adapter and the
// overlay-admission-v1 commit/ack capability. It requires an unsharded replica
// set and uses primary reads and majority journaled writes. Callers that do not
// inject this store keep their existing storage behavior. Mixed TS/Go writers of
// one node database are unsupported.
package mongodb
