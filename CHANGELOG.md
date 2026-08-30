# Changelog

Notable changes to this library, newest first. Versions are git tags; this file is written
for whoever bumps the dependency — what changed, and what it means for code that already
uses it.

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
