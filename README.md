# go-eidas-audit

The **eIDAS-audit** (signing & evidence) audit emitter for eIDAS signing services. A thin
library that gives every signing-event service **one correct way** to publish
structured, schema-correct material-signing events to the broker, so the hash-chained
trail consumed by the audit/evidence sink is consistent regardless of producer.

**Scope:** this library targets [Azugo](https://azugo.io) services — its entrypoints take
`*azugo.Context` by design, and it is versioned in lockstep with the Azugo-based platform
kit. `DataSubjects` values must be **pseudonymous internal identity references**, never
national identifiers, names, or e-mail addresses.

It builds on [`go-platform-kit`](https://github.com/gmb-lib/go-platform-kit)'s `broker`: every event is the
**frozen `broker.Envelope`**, tagged `signing`, stamped with a ULID id, a
high-precision occurrence time, and the request's correlation/trace ids, then published to
the signing topic. Async is fine here — **integrity, not latency**, is the priority.

> **Lean / reference-only.** The portal is *not* a certified Qualified
> validation service. The cryptographic evidence lives with LVRTC and inside the
> self-contained B-LT file (authoritative within its validity horizon); this stream stores only the **minimum to find and trust** that
> evidence — material events + references. It must **never** carry certificates, digests,
> OCSP/CRL, archive material, full validation blobs, or document bytes. The emitter strips
> such "fat" attribute keys defensively, and the publisher strips bearer-token-shaped keys.

## Install

```sh
go get github.com/gmb-lib/go-eidas-audit
```

Pinned in lockstep to `github.com/gmb-lib/go-platform-kit` (which pins the `azugo.io/*` line; see `go.mod` for the current version).

## Usage

Construct one `Emitter` per service over the service's `broker.Publisher` (the publisher
carries the injected transport — TLS + per-topic ACLs — and stamps every event):

```go
import (
    "github.com/gmb-lib/go-eidas-audit/eidas"
    "github.com/gmb-lib/go-platform-kit/broker"
)

pub := broker.NewPublisher(transport, cfg.ServiceName) // transport wired by the service
audit := eidas.NewEmitter(pub, eidas.DefaultTopic)      // or a configured topic
```

Then emit material signing events with the typed helpers — the event type, category,
operation and lean attribute shape are fixed for you:

```go
func (r *router) upload(ctx *azugo.Context) {
    // …store the document, compute its hash…
    err := audit.DocumentUploaded(ctx, eidas.DocumentUpload{
        Actor:        broker.Actor{ID: ctx.User().ID(), Type: "user"},
        DataSubjects: []string{ctx.User().ID()},
        DocumentID:   doc.ID,
        ContentHash:  doc.SHA256,
        MIME:         doc.MIME,
        Size:         doc.Size,
    })
    // …
}

audit.SignatureApplied(ctx, eidas.Signature{
    Actor:                 broker.Actor{ID: signer, Type: "user", Assurance: "high"},
    EnvelopeID:            env.ID,
    Slot:                  slot.ID,
    Format:                eidas.FormatPAdES,
    Level:                 eidas.LevelQES,
    SimpleSignRef:         resp.Reference, // a reference, not crypto material
    BaselineConfirmed:     true,
    QualifiedTimestampRef: resp.TSARef,
})
```

For events not covered by a helper, use the `Emit` escape hatch — it defaults the category
to `signing` and runs the same fat/PII + token sanitization before publishing.

## Events

One typed helper per eIDAS-audit material event:

| Helper | `event_type` |
|---|---|
| `DocumentUploaded` | `document.uploaded` |
| `DocumentPreviewed` | `document.previewed` |
| `ConsentCaptured` | `signing.consent` |
| `AuthAssurance` | `signing.assurance` |
| `SigningInitiated` | `signing.initiated` |
| `ProviderRedirect` / `ProviderCallback` | `signing.redirect` / `signing.callback` |
| `SignatureApplied` | `signing.applied` |
| `ValidationPerformed` | `signing.validation` |
| `EnvelopeTransition` | `envelope.transition` |
| `CoSignerInvited` | `envelope.cosigner_invited` |
| `DocumentDownloaded` | `document.downloaded` |
| `RetentionPurge` | `retention.purge` |

GDPR-audit (GDPR access) and NIS2-audit (NIS2 security) are **separate mechanisms** with their
own libraries — [`go-gdpr-audit`](https://github.com/gmb-lib/go-gdpr-audit) and [`go-sec-events`](https://github.com/gmb-lib/go-sec-events).
A service that uploads a document emits the eIDAS-audit event here *and* the corresponding
access record there; this library does not route across regimes.

## Develop

```sh
go build ./...
go test ./...
go vet ./...
```

## Contributing

Bug reports and pull requests are welcome. [CONTRIBUTING.md](CONTRIBUTING.md) names the gate a
change has to pass, what a change to this library needs, and the sign-off every commit carries.

Suspected vulnerabilities go through the private route in [SECURITY.md](SECURITY.md) — never a
public issue.

## License

MIT — see [LICENSE](./LICENSE).
