// Package basm provides bounded BRC-136 hash calculations and structural
// validation of confirmed topic-admission claims.
//
// Successful validation does not establish canonical block identity, SPV,
// transaction identity, local admission policy, or peer agreement. Callers must
// independently verify those facts before using claims to drive recovery.
// BASM describes peer-relative confirmed admission history; GASP remains the
// live/unconfirmed propagation mechanism. This package performs no I/O and
// changes no engine, storage, HTTP, or activation defaults.
package basm
