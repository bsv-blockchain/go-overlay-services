// Package mongodb provides an opt-in MongoDB persistence foundation for an
// overlay node. It requires an unsharded replica set and uses primary reads and
// majority journaled writes. It does not yet implement engine.Storage or
// advertise the optional engine.AdmissionStorage capability.
package mongodb
