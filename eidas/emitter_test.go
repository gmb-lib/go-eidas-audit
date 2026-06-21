package eidas_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"azugo.io/azugo"
	"github.com/go-quicktest/qt"
	"github.com/valyala/fasthttp"

	"github.com/gmb-lib/go-eidas-audit/eidas"
	"github.com/gmb-lib/go-platform-kit/broker"
)

// captureTransport records every published message for assertion.
type captureTransport struct {
	mu   sync.Mutex
	msgs []published
	err  error
}

type published struct {
	topic, key string
	payload    []byte
}

func (t *captureTransport) Publish(_ context.Context, topic, key string, payload []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.msgs = append(t.msgs, published{topic: topic, key: key, payload: append([]byte(nil), payload...)})

	return t.err
}

func (t *captureTransport) last() (published, *broker.Envelope) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if len(t.msgs) == 0 {
		return published{}, nil
	}

	m := t.msgs[len(t.msgs)-1]
	ev := &broker.Envelope{}
	_ = json.Unmarshal(m.payload, ev)

	return m, ev
}

// withCtx runs fn inside a real request handler so it receives a fully
// initialized *azugo.Context (mirrors go-platform-kit's broker test harness).
func withCtx(t *testing.T, fn func(ctx *azugo.Context)) {
	t.Helper()

	app := azugo.NewTestApp()
	app.Get("/t", func(ctx *azugo.Context) {
		fn(ctx)
		ctx.StatusCode(fasthttp.StatusNoContent)
	})
	app.Start(t)

	defer app.Stop()

	resp, err := app.TestClient().Get("/t")
	qt.Assert(t, qt.IsNil(err))
	fasthttp.ReleaseResponse(resp)
}

func newEmitter(tr *captureTransport, topic string) *eidas.Emitter {
	return eidas.NewEmitter(broker.NewPublisher(tr, "envelope-svc"), topic)
}

func TestDocumentUploaded(t *testing.T) {
	tr := &captureTransport{}
	em := newEmitter(tr, eidas.DefaultTopic)

	var err error

	withCtx(t, func(ctx *azugo.Context) {
		err = em.DocumentUploaded(ctx, eidas.DocumentUpload{
			Actor:        broker.Actor{ID: "user-1", Type: "user"},
			DataSubjects: []string{"user-1"},
			DocumentID:   "doc-1",
			ContentHash:  "sha256:abc",
			MIME:         "application/pdf",
			Size:         2048,
		})
	})

	qt.Assert(t, qt.IsNil(err))

	msg, ev := tr.last()
	qt.Assert(t, qt.IsNotNil(ev))
	qt.Check(t, qt.Equals(msg.topic, eidas.DefaultTopic))
	qt.Check(t, qt.Equals(msg.key, ev.EventID)) // partition key is the event id
	qt.Check(t, qt.Equals(ev.EventType, eidas.EventDocumentUploaded))
	qt.Check(t, qt.Equals(len(ev.Categories), 1))
	qt.Check(t, qt.Equals(ev.Categories[0], broker.CategorySigning))
	qt.Check(t, qt.Equals(ev.Operation, broker.OpCreate))
	qt.Check(t, qt.Equals(ev.Resource.Type, eidas.ResourceDocument))
	qt.Check(t, qt.Equals(ev.Resource.ID, "doc-1"))
	qt.Check(t, qt.Equals(str(ev.Attributes[eidas.AttrContentHash]), "sha256:abc"))
	qt.Check(t, qt.Equals(len(ev.DataSubjects), 1))
}

func TestSignatureApplied_KeepsBoolAndRefs(t *testing.T) {
	tr := &captureTransport{}
	em := newEmitter(tr, "")

	withCtx(t, func(ctx *azugo.Context) {
		_ = em.SignatureApplied(ctx, eidas.Signature{
			Actor:                 broker.Actor{ID: "user-1", Type: "user"},
			EnvelopeID:            "env-1",
			Slot:                  "slot-1",
			Format:                eidas.FormatPAdES,
			Level:                 eidas.LevelQES,
			SimpleSignRef:         "ss-ref-9",
			BaselineConfirmed:     true,
			QualifiedTimestampRef: "tsa-ref-3",
		})
	})

	msg, ev := tr.last()
	qt.Assert(t, qt.IsNotNil(ev))
	qt.Check(t, qt.Equals(msg.topic, eidas.DefaultTopic)) // empty topic → default
	qt.Check(t, qt.Equals(ev.EventType, eidas.EventSignatureApplied))
	qt.Check(t, qt.Equals(ev.Operation, broker.OpSign))
	qt.Check(t, qt.Equals(str(ev.Attributes[eidas.AttrSignatureFormat]), string(eidas.FormatPAdES)))

	confirmed, ok := ev.Attributes[eidas.AttrBaselineConfirmed].(bool)
	qt.Check(t, qt.IsTrue(ok))
	qt.Check(t, qt.IsTrue(confirmed))
	qt.Check(t, qt.Equals(str(ev.Attributes[eidas.AttrSimpleSignRef]), "ss-ref-9"))
}

func TestValidationPerformed_FailMapsOutcome(t *testing.T) {
	tr := &captureTransport{}
	em := newEmitter(tr, "")

	withCtx(t, func(ctx *azugo.Context) {
		_ = em.ValidationPerformed(ctx, eidas.Validation{
			EnvelopeID:  "env-1",
			Policy:      eidas.PolicyLV,
			Format:      "PAdES_BASELINE_LTA",
			Passed:      false,
			ReportS3Ref: "s3://reports/r1",
		})
	})

	_, ev := tr.last()
	qt.Assert(t, qt.IsNotNil(ev))
	qt.Check(t, qt.Equals(ev.Outcome, broker.OutcomeFailure))
	qt.Check(t, qt.Equals(str(ev.Attributes[eidas.AttrValidationPolicy]), string(eidas.PolicyLV)))
}

func TestCoSignerInvited_SetsDataSubject(t *testing.T) {
	tr := &captureTransport{}
	em := newEmitter(tr, "")

	withCtx(t, func(ctx *azugo.Context) {
		_ = em.CoSignerInvited(ctx, eidas.CoSigner{
			Actor:          broker.Actor{ID: "user-1", Type: "user"},
			EnvelopeID:     "env-1",
			Slot:           "slot-2",
			InvitedSubject: "subject-2",
		})
	})

	_, ev := tr.last()
	qt.Assert(t, qt.IsNotNil(ev))
	qt.Check(t, qt.Equals(ev.EventType, eidas.EventCoSignerInvited))
	qt.Check(t, qt.Equals(len(ev.DataSubjects), 1))
	qt.Check(t, qt.Equals(ev.DataSubjects[0], "subject-2"))
}

// TestEmit_StripsFatAndTokenAttributes proves the lean guard (fat crypto/PII
// keys) and the publisher's token guard both fire on the escape hatch.
func TestEmit_StripsFatAndTokenAttributes(t *testing.T) {
	tr := &captureTransport{}
	em := newEmitter(tr, "")

	withCtx(t, func(ctx *azugo.Context) {
		_ = em.Emit(ctx, &broker.Envelope{
			EventType: "signing.custom",
			Outcome:   broker.OutcomeSuccess,
			Attributes: map[string]any{
				"signer_certificate": "MIIB...",  // fat — must go
				"document_bytes":     "%PDF-1.7", // fat — must go
				"ocsp_response":      "AAA",      // fat — must go
				"authorization":      "Bearer x", // token — must go
				"envelope_id":        "env-1",    // safe ref — must stay
			},
		})
	})

	_, ev := tr.last()
	qt.Assert(t, qt.IsNotNil(ev))
	// Category defaulted to signing on the escape hatch.
	qt.Check(t, qt.Equals(ev.Categories[0], broker.CategorySigning))

	for _, gone := range []string{"signer_certificate", "document_bytes", "ocsp_response", "authorization"} {
		_, present := ev.Attributes[gone]
		qt.Check(t, qt.IsFalse(present), qt.Commentf("attribute %q must be stripped", gone))
	}

	_, kept := ev.Attributes["envelope_id"]
	qt.Check(t, qt.IsTrue(kept), qt.Commentf("safe reference must be kept"))
}

func TestEmit_NilGuards(t *testing.T) {
	var nilEmitter *eidas.Emitter

	withCtx(t, func(ctx *azugo.Context) {
		qt.Check(t, qt.IsNotNil(nilEmitter.Emit(ctx, &broker.Envelope{})))

		em := newEmitter(&captureTransport{}, "")
		qt.Check(t, qt.IsNotNil(em.Emit(ctx, nil)))
	})
}

func str(v any) string {
	s, _ := v.(string)

	return s
}
