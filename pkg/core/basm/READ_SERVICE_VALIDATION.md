# B03 serving-slice validation record

This follow-on is based on foundation commit
`d99216814a4b9dca5f9f4d04a602ef2d48bdc4a7` in the isolated
`codex/go-basm-foundation` worktree. The foundation and its original validation
record remain in history. [READ_SERVICE.md](READ_SERVICE.md) defines this
slice's behavior and explicit integration obligations. The handoff identifies
the resulting commit. Validation date: 2026-09-08.

## Environment and commands

All Go checks use `GOTOOLCHAIN=go1.26.8` on darwin/arm64; the module remains at
`go 1.26.0` with `go-sdk v1.4.1`, without dependency or toolchain-directive edits.
The foundation record documents why the installed 1.26.0 toolchain was not
used for final validation. Tools below use the repository's pinned versions.

| Check | Result |
| --- | --- |
| `go test ./... -failfast -vet=all -count=1` | Pass across the repository. |
| `BASM_TS_STACK=/Users/personal/git/ts-stack go test -race ./pkg/core/basm ./pkg/core/engine ./pkg/server/... -count=1 -coverprofile=/tmp/go-basm-read-final-coverage.out` | Pass, including the real TS network test. BASM package coverage 89.4%; legacy engine package 19.6%, server package 30.3%, ports package 79.5%. These package totals are not claims of complete engine/server coverage. |
| `BASM_TS_STACK=/Users/personal/git/ts-stack go test ./pkg/server -run TestBASMRemoteInterop -v` | Pass with installed `tsx`, actual TS BASMRemote, and TS SDK decoding/root/txid checks. Default runs skip this opt-in test. |
| `go vet ./...` | Pass. |
| `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2 run --config .golangci.json ./...` | Pass, 0 issues. |
| `go run honnef.co/go/tools/cmd/staticcheck@2026.2.1 ./pkg/core/basm/... ./pkg/core/engine/... ./pkg/server/internal/ports` | Pass. |
| `go run honnef.co/go/tools/cmd/staticcheck@2026.2.1 ./...` | Only pre-existing generated API deprecation SA1019 (`runtime.BindQueryParameter`), now at generated lines 420 and 457 after regeneration. No new BASM finding. |
| `go run golang.org/x/vuln/cmd/govulncheck@v1.7.0 -show verbose ./...` | 0 called-code vulnerabilities and 0 imported-package vulnerabilities. The unchanged unused required-module advisory GO-2026-5932 remains for x/crypto v0.56.0's unmaintained openpgp package. |
| `go test ./pkg/core/basm -run '^$' -fuzz FuzzBoundedWireDecoders -fuzztime=15s -parallel=2` | Pass; 75,849 executions and 37 additional interesting inputs. This finite fuzz sample is not exhaustive. |
| `python3 pkg/core/basm/testdata/verify_vectors.py` | Pass; all 14 independent OpenSSL foundation cross-checks. |
| `go generate ./pkg/server` | Pass using module-selected oapi-codegen v2.7.1. API output and three generated-file version headers reflect normal generation; generated files were not hand edited. |
| `goimports -l` on new/modified handwritten Go files and foundation package | No files reported. |
| `gitleaks dir` on a temporary copy of all changed/new files, with `--no-banner --redact` | Pass, no leaks. |
| `git diff --check` | Pass. |

Early lint findings in new code and test helpers were corrected before the
final pinned lint pass. During parallel editing, an intermediate build saw
incomplete test helpers and one unused import; completed suites passed. These
intermediate failures are not represented as passed checks. Standalone full
Staticcheck still reports its generated-code baseline findings above.

## Exercised behavior

Service tests use immutable in-memory fixtures and an independently configured
canonical-header fixture. They exercise all five methods; initialized empty
tips; unsupported/no-header readiness boundaries; complete ranges including
empty heights; TAC gaps/mismatch; admitted count/root/uniqueness/original-index
checks; stale header and storage-currency rejection; close failures and
cancellation; malformed, missing, mismatched, and oversized raw transactions;
and response budgets for every response shape. A valid standard raw transaction
above 16 MiB passes the real service and produces its bounded hex response.

Proof tests cover singleton and 2/3/4/5-leaf roots, membership, wrong original
position, wrong height/root, missing siblings, excess depth, invalid odd-node
duplication, conflicting paths, aggregate input budgets, mixed unions, and
pruned internal levels. Wire tests additionally use the official BRC-74
synthetic explanatory vector, a Python-serialized binary-anchor golden value,
trailing/truncated/noncanonical encodings, excessive compact counts, offsets,
flags, the raw genesis transaction, and a 17 MiB script. The explanatory BUMP
vector is not claimed as independent canonical-chain evidence.

HTTP tests use Fiber and optional provider doubles to cover public success
shapes, malformed/missing/duplicate headers, strict JSON/ranges/txids, stable
redacted failures, prefix handling, cancellation/deadlines, conservative
pre-encoding limits, malformed injected-provider outputs, optional tip fields,
invalid configuration, a hex response above 16 MiB, credential-free CORS, and
unchanged admin authorization. These doubles validate HTTP adaptation; raw
identity and proof/root checks run through the actual service in separate
service and interoperability tests.

`TestBASMRemoteInterop` starts actual localhost Fiber listeners with the real
`engine.BASMReadService` over a fixed genesis fixture, a nil provider, and a
nil-header service. It runs the actual client at TS commit
`2bc799a8d8e535242e6de2d305f426ce3975ea7b` through that checkout's installed
`node_modules/.bin/tsx`; `@bsv/sdk` resolves from its overlay package.
All five client methods succeed, the SDK parses the returned BUMP and computes
its singleton genesis root, and it parses/derives the raw transaction ID.
Missing raw IDs and 501/503 failures are also checked. The TS checkout stays
clean. This is a real wire/transport fixture test, not a live chain or peer
recovery test. It uses root routes because current BASMRemote discards endpoint
path prefixes. No tests create credentials, contact remote peers, or mutate a
production database.

An agent that did not author the service/wire code independently audited the
bounded parsers, proof validation/merge, read/recheck/close sequencing, and raw
response accounting and found no remaining concrete issue.

The subsequent external review of serving commit `e8ff6dd` identified typed-nil
interfaces passing ordinary nil guards. A separate follow-up normalizes optional
providers, rejects nilable storage implementations, normalizes headers, and
checks returned views before invocation or deferred close. Regressions cover
`*BASMReadService(nil)` through server options, route configuration, and direct
handlers; pointer/function-nil storage; pointer-nil headers/views; and direct
nil service calls. Unsupported/not-ready distinctions remain unchanged.
The full repository suite and pinned linter pass for this correction, as do
race tests selected by `(TypedNil|NilReceiver)` in engine, server, and ports.

## Acceptance boundary

This slice provides optional serving interfaces, a validating read service,
current TS HTTP interoperability, and BRC binary primitives. It supplies no
production storage/header adapter, S01 persistence implementation, automatic
Engine activation, recovery client/scheduler, durable outbox, lease/cursor
updates, reorg/restart/crash recovery, peer comparison, or UI agreement claim.
`CheckCurrent`'s two-fence guarantee is an adapter contract; a fake returning
not-ready does not prove a real database implements either fence correctly.
Initial TAC/prefix readiness remains a storage obligation. Range/txid splitting
is client-managed; oversized single admitted lists have no pagination fallback.
No deployment, push, merge, or TS source change was performed. B03 remains
partial until those separately reviewed integration and recovery layers exist.
