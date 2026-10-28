# Changelog

Notable changes to this library, newest first. Versions are git tags; this file is written
for whoever bumps the dependency — what changed, and what it means for code that already
uses it.

## v1.2.7

This release requires **Go 1.27.3** (was 1.27.2). Raise your own module's `go` directive before upgrading.
No library source or public API changed.

### Changed

- `azugo.io/azugo` → v0.40.2, `azugo.io/core` → v0.40.1, `go-platform-kit` → v1.11.5,
  and `fasthttp` → v1.77.0. Their transitive dependencies were refreshed.

## v1.2.6

A security and dependency release with two things to act on: **this library now needs Go 1.27.2**,
and it **requires azugo v0.40.0 and go-platform-kit v1.11.4**, which your service inherits when it
takes this version. No source changed here and nothing it does behaves differently.

### Changed

- **The module declares `go 1.27.2`** (was `1.27.0`). Go 1.27.2 and `golang.org/x/net` v0.61.0 fix
  vulnerabilities that this library's code reaches under the old set. `govulncheck` found six it
  calls before the move and none after it: GO-2026-6603, -6608, -6611, -6612, -6613 and -6617, in
  `net/http`, `net/textproto`, `mime/multipart` and `golang.org/x/net`. Raise your own module's `go`
  directive to `1.27.2`; from there the go command downloads and uses that toolchain by itself. CI
  that reads `go-version-file: go.mod` follows with no workflow edit.

- **`azugo.io/azugo` and `azugo.io/core` → v0.40.0** (were v0.38.1), with **`go-platform-kit` →
  v1.11.4**. Nothing in this library's API changed with them. What azugo v0.40.0 changes for a
  service (outbound calls stop at the request deadline, cookies found by their bare name, `303`
  after a non-GET redirect, a wider cache interface, one more cache at start) is listed in the
  `go-platform-kit` v1.11.4 notes.

### Notes

- **Also moved:** `fasthttp` → **v1.75.0**, `golang.org/x/net` → **v0.61.0**, and the indirect
  modules that came with the platform kit, OpenTelemetry → v1.47.0 and gRPC → v1.84.0 among them.

- **CI's linter moved to golangci-lint v2.14.0** (was v2.13.1). The earlier release cannot read Go
  1.27.2's compiled standard library and stops before linting anything. CI's pinned GitHub Actions
  also moved to their current commits. No code changed with either.

- The gate is green on Go 1.27.2: `go mod verify`, `go mod tidy -diff`, build, vet, `gofmt`,
  golangci-lint v2.14.0, `go test -race` with **0 races**; `govulncheck` reports nothing this library
  calls.

## v1.2.5

Dependency maintenance with one thing to act on: **this library now needs Go 1.27**. No source
changed here and nothing it does behaves differently — but the platform kit moves two patch
releases in one step, and one upstream behaviour change rides along with it. See *Notes*.

### Changed

- **The module declares `go 1.27.0`** (was `1.26.6`), so your own module has to be on Go 1.27
  before it can build against this one. A dependency's `go` line does **not** make the go command
  fetch a newer toolchain for you — measured both ways: a consumer whose own `go` directive is
  lower stops with a `requires go >= 1.27.0 (running go 1.26.6)` error, and it stops there with
  `GOTOOLCHAIN` on its `auto` default just as it does under `local`. Raise your own `go` directive
  to `1.27.0` first; from there the go command downloads and uses the 1.27 toolchain by itself, so
  nobody has to install Go by hand. CI that reads `go-version-file: go.mod` follows the bump with
  no workflow edit — a workflow naming a Go version in the YAML needs that line changed.

### Notes

- **`github.com/gmb-lib/go-platform-kit` → v1.11.3** (was v1.11.1), and with it the framework this
  library pins in lockstep: `azugo.io/azugo`, `azugo.io/core` and `azugo.io/opentelemetry` →
  **v0.38.1**, `github.com/valyala/fasthttp` → **v1.74.0**. Nothing this library takes from any of
  them moved — it has no metrics code and touches fasthttp only in a test. The hash-chained event
  envelope is untouched, as it has to be.

  **One thing in that framework release is visible to your monitoring, not to your code, and it
  arrives with nothing to opt into.** From azugo v0.38.1 the metrics endpoint no longer negotiates
  OpenMetrics: a scraper sending `Accept: application/openmetrics-text` is answered
  `Content-Type: text/plain; version=0.0.4; charset=utf-8` with no `# EOF` terminator, where it used
  to get the OpenMetrics format. **The metric names, labels and values are unchanged.** It reaches a
  service through the platform kit, which binds azugo's metrics configuration — so if your scrape
  configuration demands the OpenMetrics content type, or treats a missing `# EOF` as a truncated
  scrape, **check it before you deploy**.

- **One module leaves the dependency graph and another joins it**, both transitively through
  fasthttp: `github.com/andybalholm/brotli` is gone and `github.com/molecule-man/go-brrr` v1.1.0
  provides the brotli implementation in its place. Also moved indirectly:
  `go-playground/validator/v10` → v10.30.4, `klauspost/compress` → v1.20.0, `golang.org/x/crypto` →
  v0.57.0, `x/net` → v0.59.0, `x/sys` → v0.48.0, `x/text` → v0.42.0, and the
  `google.golang.org/genproto/googleapis/{api,rpc}` snapshots → 20260911204522. Nothing in this
  library calls any of them directly.

- The repository gained a code of conduct, and the advisory DCO workflow was removed now that the
  sign-off is enforced by the organisation's app together with a branch ruleset. What a contribution
  has to carry is unchanged.

- The gate is green on the new set: `go mod verify`, `go mod tidy -diff`, build, vet, `gofmt`, and
  `go test -race` with **0 races**; `govulncheck` finds nothing.

## v1.2.4

### Changed

- **`azugo.io/azugo` and `azugo.io/core` → v0.38.0, `github.com/gmb-lib/go-platform-kit` →
  v1.10.0.** No source change here: the platform-kit release is additive — a size cap on a
  JetStream stream, which this library does not configure — and nothing else reaches this code.

  One thing in the framework release is worth knowing if you use azugo directly: `user.Basic`'s
  `MarshalJSON` **moved to a pointer receiver**, so marshalling a `Basic` *value* silently produces
  default field JSON instead of the custom form — no compile error.

### Notes

- The repository gained the open-source kit it was missing — `SECURITY.md`, `CONTRIBUTING.md`,
  a secret-scan configuration and the README sections pointing at them — plus this file.

---

The entries below were **reconstructed from git history** rather than written at the time, so they
say what each tag contains, not why it was decided.

## v1.2.3 · v1.2.2 · v1.2.1

- Dependency updates only.

## v1.2.0

- No library change: continuous-integration and linter configuration, dependency updates, and a
  README correction.

## v1.1.0

- **Emission can be made durable and non-blocking.** `New` takes an `Options` carrying an `Outbox`:
  `Emit` then spools the event and returns, and a background drainer publishes it. The shipped
  `FileOutbox` writes each buffered signing-evidence event as one JSON file in a spool directory, so
  events written on the request path **survive a crash or redeploy** and are re-published after
  restart from the same directory.

  Also on `Options`: `DeadLetter` receives every event the emitter would otherwise drop, so evidence
  can be persisted out of band; `MaxRetries` and `RetryBackoff` bound the drainer's retries (backoff
  doubles with jitter up to an internal cap). New lifecycle methods `Drain`, `Flush` and `Close`.

  **The default is unchanged and is not durable.** With no `Outbox`, `New` keeps the previous
  synchronous publish. Existing code compiles and behaves as before; a signing service that wants
  the guarantee has to supply the outbox.

## v1.0.3 and earlier

- Not reconstructed. See the git history and the tag list.
