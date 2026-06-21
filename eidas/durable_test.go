package eidas_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"azugo.io/azugo"
	"github.com/go-quicktest/qt"

	"github.com/gmb-lib/go-eidas-audit/eidas"
	"github.com/gmb-lib/go-platform-kit/broker"
)

// flakyTransport fails the first failFor publishes, then succeeds — for the
// drain retry path.
type flakyTransport struct {
	mu       sync.Mutex
	failFor  int
	attempts int
	msgs     []published
}

func (t *flakyTransport) Publish(_ context.Context, topic, key string, payload []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.attempts++
	if t.attempts <= t.failFor {
		return errors.New("eidas-test: transport down")
	}

	t.msgs = append(t.msgs, published{topic: topic, key: key, payload: append([]byte(nil), payload...)})

	return nil
}

func (t *flakyTransport) delivered() int {
	t.mu.Lock()
	defer t.mu.Unlock()

	return len(t.msgs)
}

// count reports how many messages the capture transport has recorded.
func (t *captureTransport) count() int {
	t.mu.Lock()
	defer t.mu.Unlock()

	return len(t.msgs)
}

// waitFor polls cond until it holds or the deadline elapses.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}

		time.Sleep(time.Millisecond)
	}

	t.Fatal("eidas-test: condition not met within timeout")
}

func TestFileOutbox_FIFOAndDurability(t *testing.T) {
	dir := t.TempDir()

	ob, err := eidas.NewFileOutbox(dir, 16)
	qt.Assert(t, qt.IsNil(err))

	for _, id := range []string{"a", "b", "c"} {
		qt.Assert(t, qt.IsNil(ob.Enqueue(&broker.Envelope{EventID: id, EventType: "t", Outcome: broker.OutcomeSuccess, Categories: []broker.Category{broker.CategorySigning}})))
	}
	qt.Check(t, qt.Equals(ob.Len(), 3))

	// Reopen over the same dir: buffered events survive (crash/redeploy).
	reopened, err := eidas.NewFileOutbox(dir, 16)
	qt.Assert(t, qt.IsNil(err))
	qt.Check(t, qt.Equals(reopened.Len(), 3))

	ctx := context.Background()
	for _, want := range []string{"a", "b", "c"} { // FIFO by enqueue time
		rec, err := reopened.Dequeue(ctx)
		qt.Assert(t, qt.IsNil(err))
		qt.Check(t, qt.Equals(rec.EventID, want))
	}
	qt.Check(t, qt.Equals(reopened.Len(), 0))
}

func TestFileOutbox_Full(t *testing.T) {
	ob, err := eidas.NewFileOutbox(t.TempDir(), 1)
	qt.Assert(t, qt.IsNil(err))

	qt.Assert(t, qt.IsNil(ob.Enqueue(&broker.Envelope{EventID: "1"})))
	qt.Check(t, qt.ErrorIs(ob.Enqueue(&broker.Envelope{EventID: "2"}), eidas.ErrOutboxFull))
}

func TestEmit_BuffersWhenOutboxSet(t *testing.T) {
	tr := &captureTransport{}
	ob := eidas.NewMemoryOutbox(16)
	em := eidas.New(broker.NewPublisher(tr, "svc"), "", eidas.Options{Outbox: ob})

	withCtx(t, func(ctx *azugo.Context) {
		qt.Check(t, qt.IsNil(em.SignatureApplied(ctx, eidas.Signature{EnvelopeID: "env-1", Slot: "s1"})))
	})

	// Durable-first: spooled, not yet published (request path never blocked).
	qt.Check(t, qt.Equals(ob.Len(), 1))
	qt.Check(t, qt.Equals(tr.count(), 0))
}

func TestDrain_PublishesBuffered(t *testing.T) {
	tr := &captureTransport{}
	ob := eidas.NewMemoryOutbox(16)
	em := eidas.New(broker.NewPublisher(tr, "svc"), "", eidas.Options{Outbox: ob})

	withCtx(t, func(ctx *azugo.Context) {
		_ = em.SignatureApplied(ctx, eidas.Signature{EnvelopeID: "env-1", Slot: "s1"})
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go em.Drain(ctx)
	waitFor(t, func() bool { return tr.count() == 1 })

	msg, ev := tr.last()
	qt.Assert(t, qt.IsNotNil(ev))
	qt.Check(t, qt.Equals(msg.topic, eidas.DefaultTopic))
	qt.Check(t, qt.Equals(msg.key, ev.EventID)) // partition key is the (frozen) event id
	qt.Check(t, qt.IsFalse(ev.OccurredAt.IsZero()))
}

func TestDrain_RetriesThenSucceeds(t *testing.T) {
	tr := &flakyTransport{failFor: 2}
	ob := eidas.NewMemoryOutbox(16)
	em := eidas.New(broker.NewPublisher(tr, "svc"), "", eidas.Options{
		Outbox:       ob,
		MaxRetries:   5,
		RetryBackoff: time.Millisecond,
	})

	withCtx(t, func(ctx *azugo.Context) {
		_ = em.SignatureApplied(ctx, eidas.Signature{EnvelopeID: "env-1", Slot: "s1"})
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go em.Drain(ctx)
	waitFor(t, func() bool { return tr.delivered() == 1 })

	tr.mu.Lock()
	defer tr.mu.Unlock()
	qt.Check(t, qt.Equals(tr.attempts, 3)) // 2 failures + 1 success
}

func TestClose_FlushesBuffered(t *testing.T) {
	tr := &captureTransport{}
	ob := eidas.NewMemoryOutbox(16)
	em := eidas.New(broker.NewPublisher(tr, "svc"), "", eidas.Options{Outbox: ob})

	withCtx(t, func(ctx *azugo.Context) {
		_ = em.SignatureApplied(ctx, eidas.Signature{EnvelopeID: "env-1", Slot: "s1"})
		_ = em.SignatureApplied(ctx, eidas.Signature{EnvelopeID: "env-2", Slot: "s2"})
	})
	qt.Check(t, qt.Equals(ob.Len(), 2))

	// No drainer running: Close flushes the backlog synchronously.
	qt.Assert(t, qt.IsNil(em.Close(context.Background())))
	qt.Check(t, qt.Equals(ob.Len(), 0))
	qt.Check(t, qt.Equals(tr.count(), 2))
}

// TestEmit_EnqueueFullFallsBackToSyncPublish proves the durable path never
// silently drops evidence: a full spool triggers a synchronous publish instead.
func TestEmit_EnqueueFullFallsBackToSyncPublish(t *testing.T) {
	tr := &captureTransport{}
	ob, err := eidas.NewFileOutbox(t.TempDir(), 1) // capacity 1
	qt.Assert(t, qt.IsNil(err))
	em := eidas.New(broker.NewPublisher(tr, "svc"), "", eidas.Options{Outbox: ob})

	withCtx(t, func(ctx *azugo.Context) {
		qt.Check(t, qt.IsNil(em.SignatureApplied(ctx, eidas.Signature{EnvelopeID: "env-1"}))) // buffered
		qt.Check(t, qt.IsNil(em.SignatureApplied(ctx, eidas.Signature{EnvelopeID: "env-2"}))) // outbox full → sync publish
	})

	qt.Check(t, qt.Equals(ob.Len(), 1))    // first still buffered
	qt.Check(t, qt.Equals(tr.count(), 1))  // second published synchronously
}

// TestEmit_EnqueueFullAndPublishFailsDrops proves the last-resort: when both the
// spool and the synchronous publish fail, the event is dead-lettered and the
// error surfaces (the caller logs it).
func TestEmit_EnqueueFullAndPublishFailsDrops(t *testing.T) {
	tr := &captureTransport{err: errors.New("eidas-test: broker down")}
	ob, err := eidas.NewFileOutbox(t.TempDir(), 1)
	qt.Assert(t, qt.IsNil(err))

	var dead []*broker.Envelope
	em := eidas.New(broker.NewPublisher(tr, "svc"), "", eidas.Options{
		Outbox:     ob,
		DeadLetter: func(rec *broker.Envelope) { dead = append(dead, rec) },
	})

	var emitErr error
	withCtx(t, func(ctx *azugo.Context) {
		_ = em.SignatureApplied(ctx, eidas.Signature{EnvelopeID: "env-1"}) // buffered (fills capacity)
		emitErr = em.SignatureApplied(ctx, eidas.Signature{EnvelopeID: "env-2"})
	})

	qt.Check(t, qt.IsNotNil(emitErr))     // surfaced as non-fatal error to the caller
	qt.Check(t, qt.Equals(len(dead), 1))  // dead-lettered, not silently lost
}

func TestDrainCloseFlush_NoOpWithoutOutbox(t *testing.T) {
	// Legacy synchronous emitter (no outbox): lifecycle calls are safe no-ops.
	em := eidas.NewEmitter(broker.NewPublisher(&captureTransport{}, "svc"), "")

	go em.Drain(context.Background()) // must return immediately, not block on a nil outbox
	qt.Check(t, qt.IsNil(em.Flush(context.Background())))
	qt.Check(t, qt.IsNil(em.Close(context.Background())))
}
