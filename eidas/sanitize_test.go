package eidas_test

import (
	"strings"
	"testing"

	"azugo.io/azugo"
	"github.com/go-quicktest/qt"

	"github.com/gmb-lib/go-eidas-audit/eidas"
	"github.com/gmb-lib/go-platform-kit/broker"
)

func TestEmit_CapsLongValuesAndKeepsCallerMapIntact(t *testing.T) {
	tr := &captureTransport{}
	em := newEmitter(tr, "")

	long := strings.Repeat("x", 4*eidas.MaxAttrValueLen)
	caller := map[string]any{
		eidas.AttrReason: long,      // must be truncated on the event…
		"certificate":    "MIIC...", // fat key — must be stripped on the event…
		eidas.AttrSlot:   "slot-1",  // safe — must survive
	}

	withCtx(t, func(ctx *azugo.Context) {
		_ = em.Emit(ctx, &broker.Envelope{
			EventType:  eidas.EventEnvelopeTransition,
			Operation:  broker.OpUpdate,
			Outcome:    broker.OutcomeSuccess,
			Attributes: caller,
		})
	})

	_, ev := tr.last()
	qt.Assert(t, qt.IsNotNil(ev))

	reason, _ := ev.Attributes[eidas.AttrReason].(string)
	qt.Check(t, qt.Equals(len([]rune(reason)), eidas.MaxAttrValueLen))

	_, hasCert := ev.Attributes["certificate"]
	qt.Check(t, qt.IsFalse(hasCert))
	qt.Check(t, qt.Equals(ev.Attributes[eidas.AttrSlot], any("slot-1")))

	// …while the caller-owned map stays untouched (copy-on-write).
	qt.Check(t, qt.Equals(len(caller), 3))
	orig, _ := caller[eidas.AttrReason].(string)
	qt.Check(t, qt.Equals(len(orig), 4*eidas.MaxAttrValueLen))
}
