# Identity TopicManager fixture provenance

`identity-topic-fixtures.json` is generated from the built TypeScript SDK and
the TypeScript wallet toolbox fixture pinned at:

- ts-stack reference: `2bc799a8d8e535242e6de2d305f426ce3975ea7b`
- C01 fixture source: `c58f81787e7c2a985a167c15db9fd9ca5fed6c35`
- C01 source files: `packages/wallet/wallet-toolbox/src/utility/__tests__/identityVerification.fixtures.ts` and `packages/wallet/wallet-toolbox/src/utility/__tests__/fixtures/identity-verification.json`

Regenerate with both pinned worktree roots:

```sh
TS_STACK_ROOT=/path/to/ts-stack \
C01_TS_STACK_ROOT=/path/to/c01-source \
  node pkg/topics/identity/testdata/generator/generate.mjs
```

The roots are required; the generator has no machine-specific default path.
It checks the TypeScript stack at `2bc799a8d8e535242e6de2d305f426ce3975ea7b`
and the C01 source worktree at
`c58f81787e7c2a985a167c15db9fd9ca5fed6c35`.

The generator loads `packages/sdk/dist/esm/mod.js` and
`packages/overlays/topics/dist/identity/IdentityTopicManager.js`. It uses the
SDK's `Certificate.sign`, `MasterCertificate.createCertificateFields`,
`MasterCertificate.createKeyringForVerifier`, `PushDrop.lock`, transaction
signing, and `MerklePath` implementations. The output stores BEEF as base64,
along with SHA-256, txid, Merkle root, certificate objects, labels for every
output, and the admission indices returned by the TypeScript TopicManager.

The C01 certificate is reused byte-for-byte after checking its pinned SHA-256
digest. The positive fixtures spend a confirmed P2PKH ancestor with a signed
P2PKH unlocking script and carry a two-leaf confirmation proof. The
`c01-confirmed-signed-spend` fixture therefore exercises an actual signed spend
with ancestry; the TopicManager expected indices only describe output admission
and do not assert graph or SPV verification. The mixed transaction has valid
C01 outputs at indices 0 and 6 plus the mixed-field certificate at index 5, so
the common C01 path can be tested independently of field-order support.

The mixed fixture has valid C01 outputs at indices 0 and 6 plus the
mixed-case/accent field-name output at index 5. Indices 1 through 4 independently exercise a malformed
PushDrop envelope, a tampered subject envelope signature, an invalid
certifier signature, and an empty keyring that makes decryption fail. The
mixed certificate has fields `Name`, `name`, `éclair`, `zebra`, and `userName`,
all encrypted and revealed to the SDK `anyone` public keyring. Its encrypted
field and envelope bytes use the SDK's random symmetric keys, so regenerating
the corpus creates new valid BEEF bytes and hashes while preserving the schema
and admission expectations.

The committed BEEF values are test data only. No network, chain tracker, or
database is required to parse or exercise the output admission cases.

The corpus records SHA-256 values for the pinned source blobs used by the
generator, each loaded SDK certificate/wallet/transaction primitive and
TopicManager ESM artifact, and the Node/V8/ICU runtime plus `Intl.Collator`
options that produced the collation observations. The generator verifies both
git worktree heads and uses `git show` at the pinned revisions before loading
those artifacts. These hashes bind the recorded source and loaded dist files,
but do not claim a hermetic rebuild of every transitive dependency; a consumer
can independently verify the recorded files. Every transaction fixture also
carries the exact TS `toAtomicBEEF()` bytes and hash alongside its raw BEEF
bytes.

## Field-order profiles

`orderingProfiles` contains four deliberately small certificates:

- `ascii-case`: `Name`, `name`, `userName`, `zebra`.
- `accent-combining-forward`: `é`, `e\u0301`, `alpha` in precomposed-first JSON order.
- `accent-combining-reverse`: the same canonical-equivalent names in combining-first JSON order.
- `astral-versus-bmp`: an astral character (`𐀀`) beside BMP private-use `\uE000` and `alpha`.

Each profile records the input JSON field order, the order produced by
`localeCompare`, JavaScript's default UTF-16 `.sort()`, and UTF-8 byte order
using `Buffer.compare` (the byte-order reference used for Go characterization).
It also emits the exact TS `Certificate.toBinary(false)` bytes and a separate
certificate signature made over the UTF-8 comparator bytes. The TS-signed
envelope is admitted by the built TypeScript TopicManager. The UTF-8-signed
envelope is then passed through that same TopicManager to record whether the
two signing preimages happen to agree. The combining-first tie profile agrees
because `localeCompare` returns zero for `é` and `e\u0301` and the stable sort
retains that JSON order; the precomposed-first profile does not.

PushDrop transports only the UTF-8 JSON envelope field. The certificate
signature preimage is reconstructed by `Certificate.toBinary(false)` during
verification, so a Go implementation parsing a JSON object must preserve or
explicitly define field ordering before reproducing the signature preimage.
The profile's `envelopeJSONBase64` gives the exact transported JSON bytes, and
the two preimage fields are diagnostic references rather than protocol fields.

The `searchableProfile` is a complete signed BEEF fixture with publicly
revealed fields `{name, profilePhoto, icon, userName, "2", "10", bom}`. It records
the actual TypeScript decrypted property iteration order (`2`, `10`, `name`,
`profilePhoto`, `icon`, `userName`), excludes `profilePhoto` and `icon`, and
emits searchable attributes and the exact concatenation
`second tenth Alice Smith Alice Alice BOM`. The raw source field `bom` begins
with U+FEFF; TypeScript's `TextDecoder` strips that leading BOM during
decryption, so `expectedFields.bom` is `Alice BOM` while `sourceFields.bom`
retains the original BOM-prefixed text.

## Exact envelope-property regressions

`envelope-cases.json` is a separate regression corpus generated by
`generator/generate-envelope-cases.mjs` with `TS_STACK_ROOT` at the same pin.
It reuses the frozen C01 certificate and the existing unconfirmed fixture's
confirmed P2PKH ancestor, then signs real spends with fixture key 13.
Each subject-signed PushDrop records exact envelope JSON, script, BEEF/atomic
BEEF, hashes and actual TS TopicManager admission. It does not regenerate the
original certificate corpus or claim independent SPV verification.

The seven cases cover normal properties, uppercase-only SUBJECT/FIELDS,
unknown uppercase TYPE/KEYRING shadows, and duplicate subject properties whose
last value is null or the correct string. These preserve TS exact-name and
last-property-value behavior. The generator checks the existing direct module
hashes before import; the transitive/hermetic limitations above still apply.
