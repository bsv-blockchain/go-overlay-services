# BASM primitive and conformance foundation

The foundation record below describes commit `d99216814a4b9dca5f9f4d04a602ef2d48bdc4a7`.
The subsequent optional read-service, HTTP, and bounded wire slice is documented
in [READ_SERVICE.md](READ_SERVICE.md). Its additions supersede the foundation's
statements about those features being absent; recovery and activation remain
outside both slices.

This package implements the calculation and structural-validation portion of
B01/B03 (requirements B1 and X1; portions of verification cases T40/T44). It is
not a complete B03 delivery. No engine/storage methods, HTTP endpoints, binary
wire codec, scheduler, recovery jobs, or activation defaults are changed.

The normative source is [BRC-136 at file revision
2733cd2950a739b3c977b95d652ff63e3773c40b](https://github.com/bsv-blockchain/BRCs/blob/2733cd2950a739b3c977b95d652ff63e3773c40b/overlays/0136.md),
identical to the file at BRCs HEAD
`39a643ff148a8dcd23ec08986a8ddeb7d5713743` when inspected on 2026-09-08.
Frozen [language-neutral vectors](testdata/vectors.json) are independently
calculated with Python and cross-checked using OpenSSL; see
[fixture provenance](testdata/README.md). Synthetic block hashes are calculation
inputs, not asserted canonical BSV blocks or SPV proofs.

## Available operations

- `Hash` stores 32 internal-order bytes. `ParseHash` requires exactly 64
  lowercase display-order hex characters; `String`/text marshaling reverse
  back to display order. Conversion to/from SDK `chainhash.Hash` is a Go value
  conversion between the same internal-order array representation.
- `Root` computes zero for empty, the txid for singleton, and Bitcoin SHA256d
  pair hashing with odd-node duplication for larger lists. It preserves order
  and caller-owned values. It accepts illustrative duplicate primitive leaves.
- `ValidateAdmittedList` rejects actual duplicate txids, duplicate/decreasing
  block indices, indices outside the caller's block transaction count, and
  excessive counts. Indices remain original block positions with gaps allowed;
  no null leaves, sorting, deduplication, or renumbering occurs.
  `ValidateAnchorList` additionally matches the claimed count/root.
- `NewChain`/`Append` calculate an immutable, contiguous TAC from topic genesis,
  including every empty-admission height. Height zero needs no underflowing
  genesis-minus-one integer. No arbitrary remote-checkpoint constructor exists.
  `HashTACStep` is explicitly only three-hash arithmetic without continuity.
- `DecodeAnchorJSON`, `DecodeAdmittedListJSON` (bare reference array), and
  `DecodeRangeJSON` provide bounded parsing primitives, not HTTP response
  envelopes. Required fields, exact unsigned integers, lowercase hashes,
  UTF-8/paired Unicode surrogates, duplicate fields in parsed objects, and
  trailing values are checked. Unknown fields are ignored to permit additive
  extensions; their internal duplicate keys are not interpreted. The TS `tac`
  extension on an anchor is ignored, not authenticated. Ordinary `json.Unmarshal`
  into value structs is not a substitute for these strict decoders.

## Bounds and evidence limits

All calls use explicit positive `Limits` or a positive leaf/range limit.
`DefaultLimits()` proposes 100,000 admitted references, 1,024 heights, 16 MiB
JSON, and 256 UTF-8 topic bytes per call. Parsed objects additionally have a
fixed limit of 64 fields. Counts and block indices must fit the existing TS
JSON safe-integer domain, and heights fit the BRC binary format's uint32.
These are local acceptance limits, not consensus invalidity rules. Defaults
are inactive proposals pending workload selection before endpoint activation.

The representative limit test parses 100,000 unique references with original
even-numbered block positions: 9,544,446 serialized JSON bytes, below 16 MiB;
the same input fails with a 99,999-reference limit. This establishes a tested
fixture boundary, not an end-to-end memory or performance service commitment.
Hashing/list validation check context cancellation periodically. JSON data is
already buffered, so future transports must bound network reads, wall time,
concurrent requests, retained pages, raw transactions, and BUMP bytes separately.
No raw-transaction or BUMP parser is provided here.

Shape consistency does not prove the peer's block hash is canonical, a txid is
present at a claimed block position, raw bytes derive that txid, or a transaction
satisfies the local topic policy. Even a consistent count/root/TAC is only a
claim until those independent checks succeed. BASM concerns peer-relative
confirmed admission history through a stated comparison scope. It proves
neither global completeness, current lookup equality, unspentness, nor live
propagation. GASP remains necessary for live/unconfirmed transactions.

## Later integration seams

1. Preserve generic `engine.Storage` injection and current public HTTP/CORS
   semantics. Add six-message/binary and TS-envelope interoperability only
   after the S01/W02 capability review; do not infer BASM support from this
   package's presence. Policy/genesis/network compatibility needs an explicit
   negotiated or independently pinned basis.
2. Independently resolve canonical headers/block hashes and verify each claimed
   height, BUMP position, raw-tx identity, and local admission decision before
   a candidate affects persisted history. A peer anchor is not chain authority.
3. Store immutable anchor revisions with both `chainEpoch` (reorg) and
   `topicHistoryGeneration` (late/repaired admission even in the same block).
   Current pointers, leases, cursors, and comparisons must be fenced by both;
   do not freeze mutable uniqueness at `(topic,height,hash)`. Chain calculations
   here deliberately define no storage keys or restore-from-checkpoint API.
4. Recompute TAC contiguously from the affected history after repair. Keep
   persisted work and cursor advancement atomic/recoverable; compare TACs at
   one common canonical height within a stable remote/local snapshot. Different
   tip heights cannot directly establish equality. A matching old prefix is
   not caught-up completion, and local extra admissions must never be deleted
   to manufacture agreement.

Storage transactions/outboxes, crash/reorg/restart recovery, status/green
evidence, TS-to-Go network interop, deployments, and feature activation remain
unimplemented/unverified by this foundation. No production adapter is replaced.
