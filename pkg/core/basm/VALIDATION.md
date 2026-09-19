# Foundation validation record

Reviewed source base: `f5d8074be085c1970e113bc8ee55fecbab251fcf` on maintained
`bsv-blockchain/go-overlay-services/master`, fetched 2026-09-08. Work branch:
`codex/go-basm-foundation`. Only `pkg/core/basm/` is added; module dependencies,
engine/storage injection, HTTP/CORS, and defaults are unchanged. The parent
review report supplies the resulting commit identity.

## Environments

Initial installed toolchain: `go1.26.0 darwin/arm64`, matching the module's
minimum `go 1.26.0`; SDK dependency is unchanged `go-sdk v1.4.1`.
The initial full suite (`go test ./... -failfast -vet=all -count=1`) and
`go vet ./...` passed. The initial repository vulnerability scan with
`go run golang.org/x/vuln/cmd/govulncheck@v1.7.0 ./...` failed with 17 called
standard-library advisories in existing HTTP/TLS/XML/network paths.

Final verification uses `GOTOOLCHAIN=go1.26.8`, downloaded through Go's standard
toolchain mechanism. [Go's official release feed](https://go.dev/dl/?mode=json)
listed `go1.26.8` alongside `go1.27.1` on 2026-09-08. No module or system-default
toolchain change was made. The module still permits 1.26.0; release/build
environments must select a patched toolchain. Passing on 1.26.8 does not make
the old 1.26.0 vulnerability result disappear.

## Final checks

Commands ran from the repository root. Each `go` command below used
`GOTOOLCHAIN=go1.26.8` unless an environment is explicitly stated.

| Command | Result |
| --- | --- |
| `go test ./... -failfast -vet=all -count=1` | Pass across the repository. |
| `go test -race ./pkg/core/basm -count=1 -coverprofile=/tmp/go-basm-foundation-coverage.out` | Pass; 91.0% statements. |
| `go vet ./...` | Pass. |
| `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2 run --config .golangci.json ./...` | Pass; 0 issues, using repository-pinned version/configuration. |
| `go run honnef.co/go/tools/cmd/staticcheck@2026.2.1 ./pkg/core/basm/...` | Pass, using repository-pinned version. |
| `go run honnef.co/go/tools/cmd/staticcheck@2026.2.1 ./...` | Fails only SA1019 at unchanged generated `pkg/server/internal/ports/openapi/openapi_api.gen.go:222,257`: deprecated `runtime.BindQueryParameter`. No BASM finding. |
| `go run golang.org/x/vuln/cmd/govulncheck@v1.7.0 -show verbose ./...` | Pass; 0 called/imported-package vulnerabilities. Reports one unused required-module advisory, GO-2026-5932 for unmaintained `golang.org/x/crypto/openpgp` in x/crypto v0.56.0. |
| `go test ./pkg/core/basm -run '^$' -fuzz FuzzBoundedJSONDecoders -fuzztime=15s -parallel=2` | Pass; 380,468 executions, 115 additional interesting inputs. |
| `python3 pkg/core/basm/testdata/verify_vectors.py` | Pass; 14 OpenSSL cross-checks (8 multi-leaf roots, 6 TAC anchors), independent of production Go/TS. |
| `gofmt -l pkg/core/basm` and `/Users/personal/go/bin/goimports -l pkg/core/basm` | No files reported. |
| `gitleaks dir pkg/core/basm --no-banner --redact` | Pass; no leaks. |
| `git diff --check` | Pass. |

The initial locally installed Staticcheck 2025.1.1 could not analyze Go 1.26;
it was replaced for these commands with the repository's pinned 2026.2.1 via
`go run`, without changing the shared executable. Early lint findings in new
code were corrected and the final pinned checks rerun. One early fuzz build
overlapped a test-file rename and failed to build; the stable-source run above
passed. Neither failed attempt is represented as a passed check.

The bounded benchmark
`go test ./pkg/core/basm -run '^$' -bench BenchmarkValidateAdmittedListProposedLimit -benchtime=3x -benchmem`
passed on Go 1.26.8 / Apple M3 Pro: 17,771,458 ns/op, 11,683,808 B/op,
275 allocations/op for 100,000 references. This three-iteration local sample
is not a throughput or memory commitment. The representative JSON test passes
9,544,446 bytes / 100,000 references and rejects one fewer allowed reference.

An independent read-only reviewer also ran the package suite and 68,626
external Unicode/escaping/number/delimiter cases with no blocker. Parent review
caught the repository-wide `testdata` ignore rule: the three required fixture
files are explicitly force-added, without changing unrelated ignore behavior.

## Acceptance scope

This is B01/B03 primitive groundwork for B1/X1 and portions of T40/T44.
Committed fixtures pin the current ordered admitted-subset construction, empty
and singleton roots, odd/even trees, original-index gaps, duplicate-list
rejection, byte order, genesis/empty heights, JSON/count/range/overflow bounds,
and contiguous TAC calculation. See [integration seams](README.md).

B03 endpoints/storage and six-message/binary interop remain incomplete.
Canonical block/BUMP position/raw-tx/local-policy verification, common-height
peer comparison, durable repair/reorg/restart/lease tests, Mongo adapters,
status evidence, live HTTP/CORS behavior, deployment, and automatic activation
were not exercised or implemented. This commit does not complete B01/B03 or
authorize a BASM agreement/independent-SPV claim.
