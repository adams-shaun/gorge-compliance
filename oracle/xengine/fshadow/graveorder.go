package fshadow

import (
	"slices"

	"github.com/adams-shaun/gorge/engine/events"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/engine/state"
)

// adoptGraveyardOrder takes Forge's order for cards put into a graveyard at
// the same time since the previous snapshot. CR 404.3 lets their owner
// arrange them in any order, a choice neither engine asks for and the log
// has no line for: gorge keeps the order its effect named them in (Fact or
// Fiction's unchosen pile in library order), Forge the order its pile list
// holds (its AI separator's pick order), so the two graveyards can hold the
// same cards differently ordered with neither engine wrong (farm cmdr4
// TLA-s137 pod 13703 line 607: gorge [Ray of Distortion, Stone Quarry,
// Plains, Fact or Fiction], Forge [Plains, Ray of Distortion, Stone Quarry,
// Fact or Fiction]).
//
// A batch is a run of consecutive move_zone events into one graveyard from
// one zone, with no other event between them: one zone change moving
// several cards at once (graveyardBatches). Only the batches' cards are
// reordered, and only among the graveyard positions their own batch holds;
// every other card must already be where Forge has it. Where that cannot
// make the graveyard Forge's -- different cards, a card out of place that no
// batch moved, an order spanning two batches -- nothing changes and the
// comparison reports the difference. An adopted order is written into
// gorge's graveyard, so later snapshots compare against the order the owner
// chose. It reports how many graveyards it reordered.
func (f *follower) adoptGraveyardOrder(x rules.OracleSnapshot) int {
	if f.e == nil || f.e.G == nil || f.e.L == nil {
		return 0
	}
	evs := f.e.L.Events
	from := f.graveMark
	if from > len(evs) {
		from = 0
	}
	batches := graveyardBatches(evs[from:])
	if len(batches) == 0 {
		return 0
	}
	n := 0
	for _, p := range x.Players {
		if p.Seat < 0 || p.Seat >= len(f.e.G.Players) {
			continue
		}
		gy := f.e.G.Zone(state.ZGraveyard, state.PlayerID(p.Seat))
		if order, ok := f.batchOrder(gy, p.Graveyard, batches); ok {
			copy(gy, order)
			n++
		}
	}
	return n
}

// graveyardBatches groups evs' move_zone events into a graveyard: a run of
// consecutive ones with the same origin forms one batch. Single-card runs
// are dropped: one card has no order to choose.
func graveyardBatches(evs []events.Event) [][]state.ObjID {
	var out [][]state.ObjID
	var cur []state.ObjID
	var curFrom state.Zone
	flush := func() {
		if len(cur) > 1 {
			out = append(out, cur)
		}
		cur = nil
	}
	for _, ev := range evs {
		if ev.Kind != events.MoveZone || ev.To != state.ZGraveyard {
			flush()
			continue
		}
		if len(cur) > 0 && ev.From != curFrom {
			flush()
		}
		cur, curFrom = append(cur, ev.Obj), ev.From
	}
	flush()
	return out
}

// batchOrder is gy rearranged to Forge's names want by permuting each
// batch's cards among the positions they hold, or ok false where no such
// rearrangement differs from gy and names want exactly.
func (f *follower) batchOrder(gy []state.ObjID, want []string, batches [][]state.ObjID) ([]state.ObjID, bool) {
	if len(gy) != len(want) || len(gy) < 2 {
		return nil, false
	}
	name := func(id state.ObjID) string { return objName(f.e.G.Obj(id)) }
	pos := map[state.ObjID]int{}
	for i, id := range gy {
		pos[id] = i
	}
	order := slices.Clone(gy)
	moved := map[int]bool{}
	for _, b := range batches {
		// The batch's cards still in this graveyard, and the positions they
		// hold there in order.
		var ids []state.ObjID
		var at []int
		for _, id := range b {
			if i, ok := pos[id]; ok && !moved[i] {
				ids = append(ids, id)
				at = append(at, i)
			}
		}
		if len(ids) < 2 {
			continue
		}
		slices.Sort(at)
		// Fill each held position with a batch card of the name Forge has
		// there, first come first served among cards of one name.
		left := slices.Clone(ids)
		for _, i := range at {
			k := slices.IndexFunc(left, func(id state.ObjID) bool { return name(id) == want[i] })
			if k < 0 {
				return nil, false
			}
			order[i] = left[k]
			left = slices.Delete(left, k, k+1)
			moved[i] = true
		}
	}
	for i, id := range order {
		if name(id) != want[i] {
			return nil, false
		}
	}
	if slices.Equal(order, gy) {
		return nil, false
	}
	return order, true
}
