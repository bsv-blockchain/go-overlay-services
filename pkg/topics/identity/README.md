# Identity companion service

This opt-in package supplies `engine.TopicManager` for `tm_identity` and
`engine.LookupService` for `ls_identity`. It does not register either service,
start HTTP, or supply a database adapter. The generic engine must independently
verify transactions, scripts and chain proofs before topic admission.

`NewTopicManager` evaluates outputs independently. It verifies the exact
concatenation of PushDrop fields using the subject's `[1, "identity"]`, key
ID `"1"` signature, then verifies the certificate and decrypts public fields.
The certificate subject need not equal the derived script locking key.
Retention is always empty. Admission alone does not establish SPV validity,
unspentness, certificate revocation status, freshness or trusted-certifier policy.

`NewLookupService(projection)` indexes admitted outputs and removes them on
spend or legal eviction. History-retention and block-height notifications do
not change the current-output index. Lookup results are actual outpoint
formulas for engine BEEF hydration. `ProjectOutput` exposes the same validated
public record derivation without a storage write for future atomic admission
and projection-intent integration.

## Projection contract

Implement `Projection` with a durable adapter and explicitly inject the topic
and lookup service into the engine's configuration. `Upsert` must use the
outpoint as a unique key, and repeated `Delete` calls must succeed. Public
decrypted fields remain in `Record.Certificate.Fields`; searchable text excludes
`profilePhoto` and `icon`. Its concatenation preserves TS keyring property
enumeration, including numeric field names.

`Find` must honor `Query.Kind`, including an empty-string identity key, all
selected exact filters, attribute predicates, offset and limit. Attribute
predicates combine with AND. Empty optional certifier filters are unrestricted;
an empty certifier-only query is empty. No new ordering guarantee is implied.
`Field == ""` selects `SearchableAttributes`; otherwise it names a literal
certificate field. Adapters must use server-owned operators and must not splice
query values into database operators or regular expressions.

Preserved TS query semantics:

- Precedence: serial number; attributes; identity key with certificate types;
  identity key; certifiers. Serial number ignores lower-priority filters.
- Attribute whitespace follows ECMAScript trimming/collapsing. `userName` is an
  exact, case-sensitive normalized match; other attributes use escaped tokens
  joined by `.*` with case-insensitive matching.
- `attributes.any` takes precedence within attributes. After normalization,
  fewer than two UTF-16 code units yields empty; two uses a fuzzy text regex;
  more than two requires the native Mongo text-search semantics/index used by
  the TS service. It must not be approximated by substring or fuzzy matching.
- Empty/blank attribute searches are empty. Native Mongo text language,
  stemming, stop words, phrases and index behavior need real adapter tests.

The adapter and host own admission/projection atomicity, ordered replay,
tombstones or equivalent fencing, outbox retries, read-your-write boundaries,
rebuilds, migration and readiness. An old admission replay must not resurrect
an output after spend/eviction. Callback idempotency alone cannot guarantee
this. The legacy engine callbacks are not an atomic transaction merely because
this interface exists. Use the separately reviewed engine persistence/outbox
capability when integrating; this package adds no competing operation ledger.

## Budgets and narrow compatibility limits

`DefaultAdmissionPolicy()` limits one output script to 1 MiB, certificate and
PushDrop fields to 128, selected outputs to 10,000, total selected transaction
locking-script bytes to 32 MiB, and engine notification BEEF to 64 MiB.
`NewTopicManagerWithPolicy` and `NewLookupServiceWithPolicies` accept explicit
positive policy values. These are application budgets; the host separately
needs bounded transport and BEEF/transaction/proof parsing. Notification BEEF
is an already admitted engine payload, not an independent network ingress API.

`DefaultQueryPolicy()` permits at most 10,000 results and offset 100,000.
The policy may lower those operational ceilings. Explicit positive limits
within the cap retain limit/offset behavior. Omitted or zero legacy limits
fetch `cap + 1`: results within the cap succeed; overflow returns `ErrQueryBudget`
and requires explicit bounded pagination. No truncated success or new wire
metadata is invented. Large offsets remain workload-dependent scans, not a
promise of cheap access.

Queries are capped at 64 KiB, 4 KiB per string, 64 attributes and 128 certifiers
or certificate types. Pagination accepts finite integral JSON numbers and
rejects negative, fractional, quoted or over-budget values before projection
work. Attribute selectors must be nonempty and exclude `.`, `$` and NUL to
prevent unsafe Mongo path construction; certificate data is not normalized or
renamed. Those selectors and resource ceilings are explicit restrictions on
the otherwise permissive TS input shape.

## Certificate serialization checkpoint

This component deliberately uses the existing Go SDK v1.4.1 certificate
verification profile. Its serializer orders UTF-8 field names with Go byte
ordering. Pinned TS `2bc799a8d8e535242e6de2d305f426ce3975ea7b` reconstructs
the certificate preimage with `localeCompare`; even mixed ASCII case can
differ. Collation ties preserve JSON insertion order. The PushDrop envelope
carries JSON and the subject signature, not a separate signed binary preimage.

The common-compatible real TS fixtures pass. Other legitimate TS certificates
may not verify under this profile. `ErrCertificateVerification` describes a
failure to verify under the Go profile, not a diagnosis of forgery. There is
no alternate-order retry, arbitrary permutation search, ambient locale
dependency or normalization of signed data. Full TS certificate interoperability
remains open pending explicit deterministic profile/migration review. The
[portable fixture provenance](testdata/data/provenance.md) records mixed ASCII,
accent/combining ties, astral/BMP and searchable-field cases with exact bytes.

## Verification and integration gates

Package tests consume actual TS signed certificate/PushDrop/transaction/BEEF
bytes. They exercise independently admitted/rejected outputs, exact signature
and decryption checks, replay/upsert and deletion seams, query behavior and
budgets, and malformed scripts. In-memory projections exist only in tests;
they do not claim Mongo text-search or persistent-server acceptance.

Run package tests with `GOTOOLCHAIN=go1.26.8 go test ./pkg/topics/identity`.
The fixture generator requires the pinned TS sources and built artifacts;
committed fixture bytes make ordinary Go checks independent of Node or a
database. Regeneration uses SDK randomness, producing new signed bytes with
the same tested behavior, and records their SHA-256 digests.

Remaining acceptance includes the durable Mongo adapter/indexes, atomic engine
admission/projection/outbox integration, actual engine+HTTP lookup consumed by
the real TS IdentityClient/wallet, restart/spend/eviction, independent chain
validation and identity-history integration. SDK v1.4.1 also misserializes
parsed V1 BEEF through `AtomicBytes`; the generic SDK repair is a separate
primary-profile dependency. Tests here consume exact TS `toAtomicBEEF()` bytes
and do not conceal that unresolved engine interoperability dependency.
