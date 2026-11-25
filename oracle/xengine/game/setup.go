package game

import (
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/pincap"
	"github.com/adams-shaun/gorge/engine/events"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/engine/state"
)

// SeedPlayerCounters emits one events.PlayerCounterChange per counter into e,
// in seat then list order. It is the setup seed Play uses when the pinned
// rules.StartSeat has no PlayerCounters field (pincap.PlayerCounters is
// false); a pin that has the field puts the counters in the start spec and
// never calls this, so the two spellings cannot double.
//
// The engine's public Emit is the one event path every effect already uses, so
// the counters are in the log and in the first priority's digest. It is not
// the native setup field: a "whenever you get a counter" trigger it queues is
// resolved at the first priority rather than dropped with setup.
func SeedPlayerCounters(e *rules.Engine, cs [2][]pincap.Counter) {
	for seat, list := range cs {
		for _, c := range list {
			e.Emit(events.Event{Kind: events.PlayerCounterChange,
				Player: state.PlayerID(seat), Counter: c.Kind, Amount: c.N})
		}
	}
}
