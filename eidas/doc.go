// Package eidas is the eIDAS-audit (signing & evidence) audit emitter for eIDAS
// signing services. It gives every signing-event service one correct way to
// publish structured, schema-correct material-signing events to the broker, so
// the hash-chained trail consumed by the audit/evidence sink is consistent
// regardless of which service produced the event.
//
// It is a thin layer over go-platform-kit's broker: every event is the frozen
// broker.Envelope, tagged broker.CategorySigning, stamped with a ULID event
// id, a high-precision occurrence time, and the request's correlation/trace ids
// (broker.Publish handles stamping + validation), then published to the signing
// topic over the broker's TLS + per-topic ACLs. Async is fine here — integrity,
// not latency, is the priority.
//
// # Lean / reference-only
//
// The portal is not a certified Qualified validation service. The cryptographic
// evidence lives with LVRTC and inside the self-contained B-LT file
// (authoritative within its validity horizon — the TSA/CA chain lifetime; LTA
// augmentation is the preservation service's job); this
// stream stores only the minimum needed to find and trust that evidence —
// material-event records plus references (envelope/slot, session id, signature
// type, at-signing validation pass/fail, qualified-timestamp presence, the S3
// ref + content hash of the signed file). It must never carry certificates,
// digests, OCSP/CRL, archive material, full validation blobs, or document bytes;
// the emitter strips such "fat" attribute keys defensively (see sanitize).
//
// GDPR-audit (GDPR personal-data access) and NIS2-audit (NIS2 security telemetry) are
// separate mechanisms with their own libraries (go-gdpr-audit, go-sec-events) —
// a service that uploads a document emits this eIDAS-audit event *and* the
// corresponding access record, rather than overloading one event.
package eidas
