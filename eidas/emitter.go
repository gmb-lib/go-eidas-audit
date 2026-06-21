package eidas

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"sync"
	"time"

	"azugo.io/azugo"
	"go.uber.org/zap"

	"github.com/gmb-lib/go-platform-kit/broker"
	"github.com/gmb-lib/go-platform-kit/observability"
)

// DefaultTopic is the broker topic the audit/evidence sink consumes eIDAS-audit
// signing-evidence events from.
const DefaultTopic = "audit.signing"

// Default resilience knobs for the background drainer, used when Options leaves
// them unset.
const (
	DefaultMaxRetries   = 5
	DefaultRetryBackoff = 500 * time.Millisecond
)

// maxBackoff caps the drainer's exponential backoff.
const maxBackoff = 30 * time.Second

// MetricEmitTotal counts signing-evidence events by emission-lifecycle outcome.
// Alert on the dropped outcome — it means legal evidence was lost.
const MetricEmitTotal = "eidas_audit_emit_total"

// Emission-lifecycle metric label values.
const (
	outcomePublished = "published" // delivered to the broker (sync, enqueue-fallback, or drained)
	outcomeBuffered  = "buffered"  // written to the durable outbox for background delivery
	outcomeDropped   = "dropped"   // not delivered and not buffered (dead-lettered when configured)
)

// DeadLetterFunc receives an event the Emitter is about to drop (outbox full and
// the synchronous fallback failed, drain retries exhausted, or a flush failure)
// so the service can persist it out-of-band instead of losing legal evidence. It
// must not block for long and must not panic.
type DeadLetterFunc func(rec *broker.Envelope)

// Emitter publishes eIDAS-audit signing-evidence events. Construct one per
// service with New (or NewEmitter for the legacy synchronous mode). With a
// durable Outbox it emits non-blocking + durable: Emit stamps and spools the
// event and returns, while Drain publishes it to the broker in the background
// and Close flushes on shutdown. It is safe for concurrent use.
type Emitter struct {
	pub   *broker.Publisher
	topic string

	// Durable-outbox emission. A nil outbox selects the legacy synchronous
	// publish (Emit publishes inline; Drain/Flush/Close are no-ops).
	outbox       Outbox
	log          *zap.Logger
	deadLetter   DeadLetterFunc
	maxRetries   int
	retryBackoff time.Duration

	// Drain lifecycle (owned by Drain / Close).
	lcMu      sync.Mutex
	draining  bool
	drainStop context.CancelFunc
	drainDone chan struct{}
}

// Options configures durable, non-blocking emission for New.
type Options struct {
	// Outbox makes emission durable + non-blocking: Emit spools the event and
	// returns, and a background Drain publishes it. The shipped FileOutbox is
	// crash-safe; the default (nil here) is the legacy synchronous publish, which
	// is NOT durable. Production signing services should supply a FileOutbox.
	Outbox Outbox
	// Logger is used for drop/lifecycle warnings. Defaults to a no-op logger.
	Logger *zap.Logger
	// DeadLetter, when set, receives every event the Emitter would otherwise drop
	// so it can be persisted out-of-band. Strongly recommended for evidence.
	DeadLetter DeadLetterFunc
	// MaxRetries bounds the drainer's per-event retry attempts. 0 selects
	// DefaultMaxRetries (evidence should retry); there is no "zero retries" mode.
	MaxRetries int
	// RetryBackoff is the initial drain backoff; it doubles up to an internal cap
	// with jitter. 0 selects DefaultRetryBackoff.
	RetryBackoff time.Duration
}

// NewEmitter returns a legacy synchronous Emitter that publishes to topic over
// pub (no outbox — events are NOT durable). Pass DefaultTopic unless the
// deployment overrides it. Prefer New with a durable Outbox for production.
func NewEmitter(pub *broker.Publisher, topic string) *Emitter {
	return New(pub, topic, Options{})
}

// New returns an Emitter publishing to topic over pub. With opts.Outbox set it
// emits non-blocking + durable (run Drain in a background goroutine and call
// Close on shutdown); without it, emission is synchronous and non-durable.
func New(pub *broker.Publisher, topic string, opts Options) *Emitter {
	if topic == "" {
		topic = DefaultTopic
	}

	log := opts.Logger
	if log == nil {
		log = zap.NewNop()
	}

	maxRetries := opts.MaxRetries
	if maxRetries <= 0 {
		maxRetries = DefaultMaxRetries
	}

	retryBackoff := opts.RetryBackoff
	if retryBackoff <= 0 {
		retryBackoff = DefaultRetryBackoff
	}

	if opts.Outbox == nil {
		log.Warn("eidas-audit: no outbox configured — signing-evidence events publish synchronously and are NOT durable; supply a durable Outbox (e.g. FileOutbox via EIDAS_AUDIT_OUTBOX_DIR) for production")
	}

	return &Emitter{
		pub:          pub,
		topic:        topic,
		outbox:       opts.Outbox,
		log:          log,
		deadLetter:   opts.DeadLetter,
		maxRetries:   maxRetries,
		retryBackoff: retryBackoff,
	}
}

// Emit is the escape hatch for events not covered by a typed helper. It tags the
// envelope as a signing event when no category is set, strips fat/PII attribute
// keys, then stamps the event id / occurrence time / correlation from ctx and
// validates. With a durable Outbox it spools the (now frozen) event and returns
// without touching the broker — so the request path never blocks on NATS and the
// event survives a crash; the background Drain publishes it. Without an outbox it
// publishes synchronously. Prefer the typed helpers — they fix the event_type and
// attribute shape so the chain stays consistent.
func (e *Emitter) Emit(ctx *azugo.Context, ev *broker.Envelope) error {
	if e == nil || e.pub == nil {
		return errors.New("eidas: emitter has no publisher")
	}

	if ev == nil {
		return errors.New("eidas: nil envelope")
	}

	if len(ev.Categories) == 0 {
		ev.Categories = []broker.Category{broker.CategorySigning}
	}

	ev.Attributes = sanitize(ev.Attributes)

	// Freeze identity + correlation NOW, on the request path, so a deferred
	// (drained) publish preserves the originating request's occurred_at and
	// trace ids rather than the drain's.
	broker.Stamp(ctx, ev)

	if err := ev.Validate(); err != nil {
		return err
	}

	// No outbox: legacy synchronous publish.
	if e.outbox == nil {
		return e.publish(ctx, ev)
	}

	// Durable-first: spool locally and return immediately; the drainer publishes.
	if err := e.outbox.Enqueue(ev); err != nil {
		// The spool is full or the disk is failing. Do NOT silently drop legal
		// evidence: attempt a synchronous publish (the event is already stamped
		// and validated). Only if that also fails do we dead-letter.
		if pErr := e.publish(ctx, ev); pErr != nil {
			cause := errors.Join(err, pErr)
			e.drop(ev, "enqueue failed and synchronous publish failed", cause)

			return fmt.Errorf("eidas: evidence not buffered and not published: %w", cause)
		}

		return nil
	}

	incEmit(outcomeBuffered)

	return nil
}

// publish delivers an already-stamped event over the broker and records the
// published outcome on success. ctx may be the request *azugo.Context (which
// satisfies context.Context) or a plain background context from the drainer.
func (e *Emitter) publish(ctx context.Context, ev *broker.Envelope) error {
	err := e.pub.PublishStamped(ctx, e.topic, ev)
	if err == nil {
		incEmit(outcomePublished)
	}

	return err
}

// signing builds a eIDAS-audit envelope skeleton with the given event type,
// operation and outcome already set.
func signing(eventType string, op broker.Operation, outcome broker.Outcome) *broker.Envelope {
	return &broker.Envelope{
		EventType:  eventType,
		Categories: []broker.Category{broker.CategorySigning},
		Operation:  op,
		Outcome:    outcome,
	}
}

// DocumentUpload is a document-uploaded material event.
type DocumentUpload struct {
	Actor        broker.Actor
	DataSubjects []string
	DocumentID   string
	ContentHash  string
	MIME         string
	Size         int64
}

// DocumentUploaded records a document entering the system with its content hash.
func (e *Emitter) DocumentUploaded(ctx *azugo.Context, d DocumentUpload) error {
	ev := signing(EventDocumentUploaded, broker.OpCreate, broker.OutcomeSuccess)
	ev.Actor = actor(d.Actor)
	ev.DataSubjects = d.DataSubjects
	ev.Resource = &broker.Resource{Type: ResourceDocument, ID: d.DocumentID}
	ev.Attributes = compact(map[string]any{
		AttrContentHash: d.ContentHash,
		AttrMIME:        d.MIME,
		AttrSize:        d.Size,
	})

	return e.Emit(ctx, ev)
}

// Preview is a document/envelope preview-opened event.
type Preview struct {
	Actor        broker.Actor
	DataSubjects []string
	DocumentID   string
	EnvelopeID   string
}

// DocumentPreviewed records a document/envelope being opened for review — the
// "what-you-saw-is-what-you-signed" anchor.
func (e *Emitter) DocumentPreviewed(ctx *azugo.Context, p Preview) error {
	ev := signing(EventDocumentPreviewed, broker.OpRead, broker.OutcomeSuccess)
	ev.Actor = actor(p.Actor)
	ev.DataSubjects = p.DataSubjects
	ev.Resource = resource(p.EnvelopeID, p.DocumentID)

	return e.Emit(ctx, ev)
}

// Consent is a consent/intent-to-sign capture.
type Consent struct {
	Actor        broker.Actor
	EnvelopeID   string
	Slot         string
	DocumentHash string
}

// ConsentCaptured records consent/intent to sign against the exact document hash
// shown to the signer.
func (e *Emitter) ConsentCaptured(ctx *azugo.Context, c Consent) error {
	ev := signing(EventConsentCaptured, broker.OpCreate, broker.OutcomeSuccess)
	ev.Actor = actor(c.Actor)
	ev.Resource = &broker.Resource{Type: ResourceEnvelope, ID: c.EnvelopeID}
	ev.Attributes = compact(map[string]any{
		AttrSlot:         c.Slot,
		AttrDocumentHash: c.DocumentHash,
	})

	return e.Emit(ctx, ev)
}

// Assurance is the authentication-assurance-at-signing event.
type Assurance struct {
	Actor            broker.Actor
	EnvelopeID       string
	Method           string // eID | eParaksts Mobile | Cloud eSeal
	LevelOfAssurance string // e.g. "high"
	BindingOutcome   string // login-method ↔ signing-identity binding result
	StepUp           string // step-up result
}

// AuthAssurance records the authentication assurance established at signing time
// (method, LoA, login↔signing-identity binding, step-up result).
func (e *Emitter) AuthAssurance(ctx *azugo.Context, a Assurance) error {
	ev := signing(EventAuthAssurance, "", broker.OutcomeSuccess)
	ac := actor(a.Actor)
	if ac != nil && ac.Assurance == "" {
		ac.Assurance = a.LevelOfAssurance
	}

	ev.Actor = ac
	ev.Resource = &broker.Resource{Type: ResourceEnvelope, ID: a.EnvelopeID}
	ev.Attributes = compact(map[string]any{
		AttrMethod:           a.Method,
		AttrLevelOfAssurance: a.LevelOfAssurance,
		AttrBindingOutcome:   a.BindingOutcome,
		AttrStepUp:           a.StepUp,
	})

	return e.Emit(ctx, ev)
}

// SigningInit is a signing-initiated event.
type SigningInit struct {
	Actor      broker.Actor
	EnvelopeID string
	Slot       string
	Method     string
	InputType  InputType
	SessionID  string // simpleSign sessionId (a reference, not a secret)
}

// SigningInitiated records that signing started for a slot via a chosen method.
func (e *Emitter) SigningInitiated(ctx *azugo.Context, s SigningInit) error {
	ev := signing(EventSigningInitiated, "", broker.OutcomeSuccess)
	ev.Actor = actor(s.Actor)
	ev.Resource = &broker.Resource{Type: ResourceEnvelope, ID: s.EnvelopeID}
	ev.Attributes = compact(map[string]any{
		AttrSlot:      s.Slot,
		AttrMethod:    s.Method,
		AttrInputType: string(s.InputType),
		AttrSessionID: s.SessionID,
	})

	return e.Emit(ctx, ev)
}

// Provider is a redirect-to / callback-from a remote trust service.
type Provider struct {
	Actor      broker.Actor
	EnvelopeID string
	Slot       string
	Provider   SigningProvider
	StateRef   string // state-token reference (not the token itself)
	Outcome    broker.Outcome
}

// ProviderRedirect records the signer being redirected to LVRTC / Entrust.
func (e *Emitter) ProviderRedirect(ctx *azugo.Context, p Provider) error {
	return e.provider(ctx, EventProviderRedirect, p)
}

// ProviderCallback records the LVRTC / Entrust callback returning.
func (e *Emitter) ProviderCallback(ctx *azugo.Context, p Provider) error {
	return e.provider(ctx, EventProviderCallback, p)
}

func (e *Emitter) provider(ctx *azugo.Context, eventType string, p Provider) error {
	ev := signing(eventType, "", outcomeOr(p.Outcome))
	ev.Actor = actor(p.Actor)
	ev.Resource = &broker.Resource{Type: ResourceEnvelope, ID: p.EnvelopeID}
	ev.Attributes = compact(map[string]any{
		AttrProvider: string(p.Provider),
		AttrSlot:     p.Slot,
		AttrStateRef: p.StateRef,
	})

	return e.Emit(ctx, ev)
}

// Signature is a signature-applied event.
type Signature struct {
	Actor         broker.Actor
	EnvelopeID    string
	Slot          string
	Format        SignatureFormat
	Level         SignatureLevel
	SimpleSignRef string
	// BaselineConfirmed records that the backend confirmed the expected AdES
	// baseline level (B-LT — qualified signature timestamp + embedded
	// revocation data; see AttrBaselineConfirmed).
	BaselineConfirmed     bool
	QualifiedTimestampRef string
}

// SignatureApplied records a signature being applied to a slot, with lean
// references to the LVRTC-held evidence (no certificates or digests).
func (e *Emitter) SignatureApplied(ctx *azugo.Context, s Signature) error {
	ev := signing(EventSignatureApplied, broker.OpSign, broker.OutcomeSuccess)
	ev.Actor = actor(s.Actor)
	ev.Resource = &broker.Resource{Type: ResourceEnvelope, ID: s.EnvelopeID}
	ev.Attributes = compact(map[string]any{
		AttrSlot:                  s.Slot,
		AttrSignatureFormat:       string(s.Format),
		AttrSignatureLevel:        string(s.Level),
		AttrSimpleSignRef:         s.SimpleSignRef,
		AttrBaselineConfirmed:     s.BaselineConfirmed,
		AttrQualifiedTimestampRef: s.QualifiedTimestampRef,
	})

	return e.Emit(ctx, ev)
}

// Validation is a validation-performed event.
type Validation struct {
	Actor       broker.Actor
	EnvelopeID  string
	DocumentID  string
	Policy      ValidationPolicy
	ReportLevel string // e.g. "basic" | "detailed"
	Format      string // e.g. "PAdES_BASELINE_LTA"
	Passed      bool
	ReportS3Ref string
}

// ValidationPerformed records a validation pass; Outcome is success when Passed,
// failure otherwise.
func (e *Emitter) ValidationPerformed(ctx *azugo.Context, v Validation) error {
	outcome := broker.OutcomeSuccess
	if !v.Passed {
		outcome = broker.OutcomeFailure
	}

	ev := signing(EventValidationPerformed, "", outcome)
	ev.Actor = actor(v.Actor)
	ev.Resource = resource(v.EnvelopeID, v.DocumentID)
	ev.Attributes = compact(map[string]any{
		AttrValidationPolicy:    string(v.Policy),
		AttrReportLevel:         v.ReportLevel,
		AttrFormat:              v.Format,
		AttrValidationReportRef: v.ReportS3Ref,
	})

	return e.Emit(ctx, ev)
}

// Transition is an envelope lifecycle state change.
type Transition struct {
	Actor      broker.Actor
	EnvelopeID string
	From       string
	To         string
	Reason     string // populated for declines/cancellations
}

// EnvelopeTransition records an envelope lifecycle transition
// (draft→sent→in_progress→completed|declined|expired|cancelled).
func (e *Emitter) EnvelopeTransition(ctx *azugo.Context, t Transition) error {
	ev := signing(EventEnvelopeTransition, broker.OpUpdate, broker.OutcomeSuccess)
	ev.Actor = actor(t.Actor)
	ev.Resource = &broker.Resource{Type: ResourceEnvelope, ID: t.EnvelopeID}
	ev.Attributes = compact(map[string]any{
		AttrFromState: t.From,
		AttrToState:   t.To,
		AttrReason:    t.Reason,
	})

	return e.Emit(ctx, ev)
}

// CoSigner is a co-signer-invited event.
type CoSigner struct {
	Actor          broker.Actor
	EnvelopeID     string
	Slot           string
	InvitedSubject string // the invited party — drives GDPR-audit indexing
}

// CoSignerInvited records the moment one party's action causes processing of
// another person's data.
func (e *Emitter) CoSignerInvited(ctx *azugo.Context, c CoSigner) error {
	ev := signing(EventCoSignerInvited, broker.OpCreate, broker.OutcomeSuccess)
	ev.Actor = actor(c.Actor)
	if c.InvitedSubject != "" {
		ev.DataSubjects = []string{c.InvitedSubject}
	}

	ev.Resource = &broker.Resource{Type: ResourceEnvelope, ID: c.EnvelopeID}
	ev.Attributes = compact(map[string]any{AttrSlot: c.Slot})

	return e.Emit(ctx, ev)
}

// Download is a signed-document / evidence-downloaded event.
type Download struct {
	Actor        broker.Actor
	DataSubjects []string
	DocumentID   string
	EnvelopeID   string
	What         string // e.g. "signed_document" | "evidence_package"
	FileRef      string
}

// DocumentDownloaded records a signed document or evidence artefact being
// downloaded.
func (e *Emitter) DocumentDownloaded(ctx *azugo.Context, d Download) error {
	ev := signing(EventDocumentDownloaded, broker.OpExport, broker.OutcomeSuccess)
	ev.Actor = actor(d.Actor)
	ev.DataSubjects = d.DataSubjects
	ev.Resource = resource(d.EnvelopeID, d.DocumentID)
	ev.Attributes = compact(map[string]any{
		AttrWhat:    d.What,
		AttrFileRef: d.FileRef,
	})

	return e.Emit(ctx, ev)
}

// Purge is a retention-sweep event.
type Purge struct {
	Actor             broker.Actor // usually a service identity
	ResourceType      string
	Count             int
	Basis             string
	RetainedUnderHold int
}

// RetentionPurge records a retention sweep deleting material; the fact of
// deletion is itself retained.
func (e *Emitter) RetentionPurge(ctx *azugo.Context, p Purge) error {
	ev := signing(EventRetentionPurge, broker.OpDelete, broker.OutcomeSuccess)
	ev.Actor = actor(p.Actor)
	if p.ResourceType != "" {
		ev.Resource = &broker.Resource{Type: p.ResourceType}
	}

	ev.Attributes = compact(map[string]any{
		AttrCount:             p.Count,
		AttrBasis:             p.Basis,
		AttrRetainedUnderHold: p.RetainedUnderHold,
	})

	return e.Emit(ctx, ev)
}

// actor returns a pointer to a copy of a when it carries any identity, else nil
// so the omitempty envelope field stays absent for system-less events.
func actor(a broker.Actor) *broker.Actor {
	if a.ID == "" && a.Type == "" && a.Assurance == "" {
		return nil
	}

	return &a
}

// resource builds a resource ref, preferring the envelope and falling back to
// the document, so an event scoped to either is labelled consistently.
func resource(envelopeID, documentID string) *broker.Resource {
	if envelopeID != "" {
		return &broker.Resource{Type: ResourceEnvelope, ID: envelopeID}
	}

	if documentID != "" {
		return &broker.Resource{Type: ResourceDocument, ID: documentID}
	}

	return nil
}

// outcomeOr defaults an unset outcome to success.
func outcomeOr(o broker.Outcome) broker.Outcome {
	if o == "" {
		return broker.OutcomeSuccess
	}

	return o
}

// MaxAttrValueLen is the maximum length (in runes) of a string attribute value;
// longer values are truncated by sanitize. Attributes are lean references and
// bounded operational metadata (e.g. a decline Reason), never narratives.
const MaxAttrValueLen = 256

// forbiddenAttrKeys are attribute-key fragments that signal "fat" cryptographic
// or document payloads the lean eIDAS-audit store must never hold. They are
// stripped defensively; typed helpers never produce them.
var forbiddenAttrKeys = []string{
	"certificate", "cert_", "ocsp", "crl", "digest",
	"document_bytes", "content_bytes", "file_bytes", "validation_blob",
	"archive", "private_key", "signature_bytes", "pdf_bytes",
}

// sanitize drops any attribute whose key names a forbidden fat/PII payload and
// truncates string values to MaxAttrValueLen runes. It never mutates the input
// map — a sanitized copy is returned when anything must change, so caller-owned
// maps stay intact. The publisher additionally strips bearer-token-shaped keys
// (broker.Stamp).
func sanitize(attrs map[string]any) map[string]any {
	if len(attrs) == 0 {
		return attrs
	}

	var out map[string]any // allocated only when something changes

	cow := func() {
		if out == nil {
			out = make(map[string]any, len(attrs))
			for ck, cv := range attrs {
				out[ck] = cv
			}
		}
	}

	for k, v := range attrs {
		lk := strings.ToLower(k)
		for _, f := range forbiddenAttrKeys {
			if strings.Contains(lk, f) {
				cow()
				delete(out, k)

				break
			}
		}

		if s, ok := v.(string); ok {
			if r := []rune(s); len(r) > MaxAttrValueLen {
				cow()
				if _, kept := out[k]; kept {
					out[k] = string(r[:MaxAttrValueLen])
				}
			}
		}
	}

	if out == nil {
		return attrs
	}

	return out
}

// compact removes nil and empty-string attribute values so events stay lean and
// the chain is not cluttered with absent fields. Booleans and numbers (incl.
// zero) are kept — false/0 can be meaningful.
func compact(attrs map[string]any) map[string]any {
	for k, v := range attrs {
		if v == nil {
			delete(attrs, k)

			continue
		}

		if s, ok := v.(string); ok && s == "" {
			delete(attrs, k)
		}
	}

	if len(attrs) == 0 {
		return nil
	}

	return attrs
}

// Drain publishes buffered events until ctx is cancelled. Run it once in a
// background goroutine with an application-lifetime context (go e.Drain(appCtx)),
// never a per-request context; prefer stopping it via Close, which also flushes.
// Each event is retried with jittered exponential backoff up to MaxRetries; an
// event that still cannot be published is dead-lettered (when a DeadLetter sink
// is configured) and dropped with a warning. A second concurrent Drain call is
// ignored. It is a no-op when the Emitter has no outbox (synchronous mode).
func (e *Emitter) Drain(ctx context.Context) {
	if e == nil || e.outbox == nil {
		return
	}

	e.lcMu.Lock()
	if e.draining {
		e.lcMu.Unlock()
		e.log.Warn("eidas-audit: Drain already running; ignoring duplicate call")

		return
	}

	dctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	e.draining = true
	e.drainStop = cancel
	e.drainDone = done
	e.lcMu.Unlock()

	defer func() {
		cancel()

		e.lcMu.Lock()
		e.draining = false
		e.drainStop = nil
		e.drainDone = nil
		e.lcMu.Unlock()

		close(done)
	}()

	for {
		rec, err := e.outbox.Dequeue(dctx)
		if err != nil {
			return
		}

		e.deliver(dctx, rec)
	}
}

// deliver retries a single buffered event with capped, jittered exponential
// backoff. If ctx is cancelled mid-retry the event is re-buffered so a
// subsequent Flush/Close can still publish it.
func (e *Emitter) deliver(ctx context.Context, rec *broker.Envelope) {
	backoff := e.retryBackoff

	for attempt := 0; ; attempt++ {
		if err := e.publish(ctx, rec); err == nil {
			return
		}

		if attempt >= e.maxRetries {
			e.drop(rec, "drain retries exhausted", nil)

			return
		}

		select {
		case <-ctx.Done():
			if err := e.outbox.Enqueue(rec); err != nil {
				e.drop(rec, "drain cancelled and outbox full", err)
			}

			return
		case <-time.After(jitter(backoff)):
		}

		if backoff < maxBackoff {
			backoff *= 2
		}
	}
}

// jitter spreads a backoff delay over [d/2, d] so recovering brokers are not hit
// by a thundering herd of synchronized retries.
func jitter(d time.Duration) time.Duration {
	if d <= 1 {
		return d
	}

	half := d / 2

	return half + rand.N(half+1)
}

// Flush synchronously publishes every currently-buffered event, best-effort, for
// graceful shutdown. Prefer Close, which stops the drainer first so the two do
// not both consume the Outbox. It is a no-op when the Emitter has no outbox.
func (e *Emitter) Flush(ctx context.Context) error {
	if e == nil || e.outbox == nil {
		return nil
	}

	for e.outbox.Len() > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		rec, err := e.outbox.Dequeue(ctx)
		if err != nil {
			return err
		}

		if err := e.publish(ctx, rec); err != nil {
			e.drop(rec, "flush failed", err)
		}
	}

	return nil
}

// Close stops the background drainer (if running), waits for it to exit, then
// flushes the outbox — the single shutdown call. It is bounded by ctx and is a
// no-op when the Emitter has no outbox.
func (e *Emitter) Close(ctx context.Context) error {
	if e == nil || e.outbox == nil {
		return nil
	}

	e.lcMu.Lock()
	stop, done := e.drainStop, e.drainDone
	e.lcMu.Unlock()

	if stop != nil {
		stop()

		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return e.Flush(ctx)
}

// drop counts, logs and (when configured) dead-letters an event that could not
// be published or buffered.
func (e *Emitter) drop(rec *broker.Envelope, reason string, err error) {
	incEmit(outcomeDropped)
	e.log.Warn("eidas-audit: dropping signing-evidence event",
		zap.String("reason", reason),
		zap.String("event_id", rec.EventID),
		zap.String("event_type", rec.EventType),
		zap.Error(err),
	)

	if e.deadLetter != nil {
		e.deadLetter(rec)
	}
}

func incEmit(outcome string) {
	observability.IncCounter(MetricEmitTotal, map[string]string{
		observability.LabelOutcome: outcome,
	})
}
