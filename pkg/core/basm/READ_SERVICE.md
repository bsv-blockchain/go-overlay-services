# Optional BASM read service and current TS wire

This follow-on to the primitive foundation implements a bounded serving slice
of B03. It adds five public JSON routes, an optional historical read service,
strict request decoding, the BRC-136 anchor binary codec, and bounded BUMP/raw
transaction parsing. It does not complete B03 or activate BASM on an existing
engine. A deployment with no BASM provider returns HTTP 501 for valid BASM
requests. Existing `engine.Storage`, `OverlayEngineProvider`, and Noop providers
remain compatible.

The current wire reference is the maintained TS checkout at
`bsv-blockchain/ts-stack` revision `2bc799a8d8e535242e6de2d305f426ce3975ea7b`,
particularly `packages/overlays/overlay/src/BASMRemote.ts`, `BASM.ts`, `Engine.ts`,
and the OverlayExpress BASM routes. BRC-136 remains pinned to the revision in
the foundation README. Its six named messages are three request/response
pairs, not six HTTP routes. The additional proof/raw routes follow TS.

| POST route | JSON request | JSON response |
| --- | --- | --- |
| `/requestTopicAnchorTip` | `{}` | `{topic, blockHeight, tac, blockHash?, basmRoot?, admittedCount?}` |
| `/requestTopicAnchorRange` | `{fromHeight, toHeight}` | `{topic, anchors: [{topic, blockHeight, blockHash, basmRoot, admittedCount, tac}]}` |
| `/requestAdmittedList` | `{blockHeight, blockHash?}` | `{topic, blockHeight, blockHash?, admitted: [{txid, blockIndex}]}` |
| `/requestCompoundMerklePath` | `{blockHeight, txids}` | `{topic, blockHeight, txids, merklePath}` |
| `/requestRawTransactions` | `{txids}` | `{transactions: [{txid, rawTx}], missing: [txid]}` |

The first four require exactly one nonempty UTF-8 `x-bsv-topic` header. Raw
lookup is global and does not require that header. Hashes are 64 lowercase
display-order hex characters; proof/raw payloads are lowercase hex. Heights
are uint32 and indices/counts must fit the TS safe integer range. Empty raw
requests succeed; empty proof requests fail. Duplicate requested txids fail.
An initialized supported topic without anchors has the TS tip sentinel
`blockHeight: -1` and zero TAC. Unsupported or incomplete state is an error,
never that sentinel.

`server.WithBASMProvider` or `RegisterRoutesConfig.BASMProvider` configures the
optional capability explicitly. As a convenience, an injected existing engine
that also implements `engine.BASMProvider` is discovered when no explicit
provider is supplied. The default Go route prefix remains `/api/v1`. Current
TS `BASMRemote` constructs root-relative URLs and discards an endpoint path
prefix: configure an empty Go `BaseURL` or a matching external proxy rewrite
for that client. No prefix fallback or client mutation is introduced here.
Nil and typed-nil optional providers are treated as absent. Typed-nil storage
is rejected as unsupported; typed-nil headers preserve raw-only availability,
and a typed-nil returned read view is rejected as not-ready before use or close.
Public routes retain credential-free CORS; admin bearer authorization remains
in place. OpenAPI source and its scoped header-validation template are
regenerated with `go generate ./pkg/server`, not manually edited output.

## Storage and chain obligations

Construct `engine.NewBASMReadService(storage, headers, limits)` and supply it as
the optional provider. `BASMReadOpener` is a new, separate capability.
`OpenBASMRead` must return an immutable coherent per-request view, enforce
passed count/byte limits before allocating, retain historical spent/banned
admissions, and return errors for incomplete intervals. `CheckCurrent` must
validate both S01 `chainEpoch` and `topicHistoryGeneration`. `Close` releases
resources even after cancellation. The interfaces intentionally define no
alternative scope/fence/revision representation. A durable adapter must bind
these obligations to the reviewed S01 contract during integration; this slice
contains test fixtures, not a production storage adapter.

The configured header resolver supplies canonical block hashes, Merkle roots,
and independently obtained full-block transaction counts. A nil resolver
permits raw reads only; confirmed reads return not-ready. The service checks
headers initially and again before returning, then checks storage currency.
The adapter and resolver must expose a coordinated canonical view for their
deployment. These end-of-read checks reject observed changes but do not create
a distributed transaction or promise the chain cannot change after return.

Anchor ranges must be complete, ordered and internally TAC-contiguous. The
first returned TAC and the tip's contiguous prefix come from the initialized
storage contract; serving does not reconstruct all earlier history or
authenticate an arbitrary remote checkpoint. Lists match their anchor's
count/root and preserve unique txids and strictly increasing original block
positions bounded by the independent block transaction count.

Proof serving verifies every requested admitted txid at its original position
against the canonical header root. It bounds binary counts before SDK parsing
and derives available parents iteratively, avoiding recursive exploration of
missing subtrees. It accepts pruned internal levels, validates legitimate
odd-width duplication, rejects conflicting or illegal internal offsets, and
requires every supplied base hash to connect to the same root before merging
bounded paths. Tip and range anchors cannot claim more admissions than the
independently known full-block transaction count.
Raw serving accepts one standard non-EF/non-BEEF transaction per record and
checks SHA256d of the original fully consumed bytes against the requested ID.
Neither check executes scripts, reruns local topic policy, or asserts that an
output remains unspent.

`EncodeAnchorBinary`/`DecodeAnchorBinary` implement the BRC-136 binary anchor:
canonical CompactSize UTF-8 topic length/topic, uint32 little-endian height,
internal-order block hash/root, and canonical CompactSize count. There is no
binary HTTP negotiation in the current TS client, so the five HTTP routes
serve JSON only.

## Resource and failure behavior

Defaults are 1 MiB request JSON, 1,000 requested txids, 1,024 range heights,
100,000 admitted references, 256 topic bytes, 8 MiB aggregate fetched proof
bytes, 32 MiB per raw transaction, 96 MiB response JSON, and 10 seconds per
request. These are local configurable resource budgets, not consensus rules.
The service and handler use conservative JSON size estimates before encoding;
an estimate may reject a payload whose exact encoding would be slightly
smaller. Fiber's existing global request-body limit still applies first.
Existing all-zero server configuration selects defaults; explicitly invalid
nonzero limits fail closed. Service construction requires valid positive limits.

There are no continuation tokens or admitted-list pagination in the current
TS contract. Callers can split ranges and txid batches after a limit error.
A single admitted list exceeding the configured cap cannot be served by this
slice; it is not silently truncated and is not reported as missing. Future
pagination needs an explicit protocol extension and snapshot semantics.

Errors use `{status: "error", code, message}` with redacted messages: 400 for
invalid input, 413 for resource limits, 404 for unknown records/topics, 501 for
unsupported capability, 503 for incomplete/stale state, 500 for inconsistent
provider output, 504 for deadline, and 408 for cancellation. Cancellation is
cooperative; no orphan goroutine is created to force a timeout. Fiber does not
automatically turn a disconnected client into `UserContext` cancellation;
the handler's finite deadline still applies. Deployment-level concurrency and
rate limits remain necessary for aggregate resource control.

No recovery scheduling, durable writes, Mongo adapter, restart/crash protocol,
common-height peer agreement, status/green evidence, GASP redesign, TS client
changes, default activation, or deployment is included. Successful serving is
not proof of recovery completion or global overlay completeness.
