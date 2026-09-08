# Persistence v1 contract

`pkg/core/engine` exposes types for the optional `overlay-admission-v1` capability. The normative shared S01 contract is maintained at `ts-stack/specs/overlay/persistence-v1.md`; this document mirrors its Go boundary without claiming an unpublished external URL. The capability is separate from the legacy `engine.Storage` interface, and no engine runtime path selects it. An adapter advertises the capability through `engine.AdmissionStorageProvider`; `engine.GetAdmissionStorage` accepts only an explicitly advertised `overlay-admission-v1` implementation. That detection declares API shape and does not establish durability.

An admission provider must atomically revalidate admission read predicates, history fences, conditional spends, and ready payload pins. It recomputes the semantic digest and binds the operation key scope, topics, transaction IDs, and exact UTF-8 STEAK to the plan before saving all plan effects and the local receipt together. A matching retry returns its original receipt before stale predicate checks; a conflicting semantic digest is rejected. Historical admissions have no propagation intents, and a retry receipt remains its original delivery observation even when delivery later advances.

A pending result is not an acknowledgement. Before a repeated commit starts a fresh body, the provider recovers any persisted unresolved attempt for that operation, including after a lost response or restart. Reconcile by operation key with an optional attempt ID; Go uses a `*string`, so `nil` asks the provider to recover the unresolved attempt by key and an empty token remains representable. A missing or wrong attempt, or a missing operation, does not prove abort. `TransientTransactionError` permits a fresh body only after the prior body aborts. `UnknownTransactionCommitResult` continues reconciliation of the same attempt. Any nonnil adapter error, including context cancellation or timeout, remains an error and never becomes rejection, abort, or a successful acknowledgement. In Go, `AdmissionReconcileResult` aliases the broader result shape, but its `aborted` state is legal only from reconciliation. Do not run verification, payload upload, network requests, or arbitrary plug-ins inside the commit transaction.

`StorageUint64` values are canonical decimal unsigned integers from `0` through `18446744073709551615`. They have no sign, whitespace, exponent, decimal point, or leading zeroes. `ParseStorageUint64` preserves the complete integer value.

`AdmissionSemanticDigest` hashes these UTF-8 length-framed fields with SHA-256, in order:

1. `overlay-admission-v1`
2. scope network, genesis hash, and node ID
3. transaction ID, mode, and context digest
4. decimal topic count
5. each topic and policy ID, ordered by UTF-8 topic bytes

Every framed field must be nonempty valid UTF-8. Transaction ID, genesis hash, and context digest must each be 64 lowercase hexadecimal characters. Duplicate topics are rejected. Read predicates, decisions, and proof variants are outside this identity; a provider still validates them atomically. The shared synthetic vectors live at `pkg/core/engine/testdata/persistence-v1.json`; they do not claim database durability or protocol activation.

Payload publication happens outside admission. A `ready -> deleting` garbage-collection claim serializes with every new pin or reference and rejects new references after the claim. Physical cleanup is a recoverable later operation.

Recovery leases compare the whole scope, topic, peer, job, both history fences, and lease token, and require `expiresAtMs` to be later than database time. Check that predicate inside the same compare-and-swap as checkpoint or publication. A history update advances the topic history generation and records its earliest affected height; a recovery handoff is attached only when the recovery worker caused the revision. Current topic anchors compare-and-swap both history revisions and their header.

`ReplaySafeProjection` receives `StorageScope` for both event application and checkpoint reconciliation because event IDs and external checkpoints are scope-local. `CanAdvanceGASPCursor` is only a gate for a durable page-finalization and cursor transaction. It never advances a cursor itself. Negotiated tuples require negotiation, inclusive-since requires proven inclusive semantics and drained equal scores, full resync requires proven no-skip semantics and completed resync, and unsupported modes never advance.
