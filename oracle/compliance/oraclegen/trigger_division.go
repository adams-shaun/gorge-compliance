package oraclegen

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/effects"
	"github.com/adams-shaun/gorge/engine/rules"
)

// markPlacedTriggerDivisions marks, in the XMage view, the root target ask of a
// triggered ability that divides its amount among its targets
// (DividedAsYouChoose$ on a DealDamage, PutCounter or PreventDamage: Magmatic
// Core's "X damage divided as you choose among any number of target
// creatures"). XMage poses that slot as a TargetAmount and scripts it only as
// "<ref>^X=<share>", a lone target included; gorge (CR 603.3d applying 601.2d)
// asks nothing for a lone target, and records the ask's Divided total only when
// DividedAsYouChoose$ is a literal, so a total that is an SVar (X, a counter
// count) left the ask unmarked and its target scripted bare ("Must be target
// amount, but found unknown target").
//
// The total comes from what gorge recorded, never from the card's printed
// text: the "divide" ask that follows two or more targets carries the points
// above one per target (its Min), two or more targets with no such ask are
// exactly one point each, and for a lone target the source permanent's own
// snapshot at the checkpoint after the step is read through the SVar's
// Count$CardCounters.<TYPE> / Number$<n> form. Any other lone-target form
// stays unmarked (its bare answer is what XMage already rejects, so nothing
// regresses), and the cast and activation asks (their own routes) are left
// alone. Only decisions the Forge contract never sees are touched: the Forge
// callers pass xmage false and never reach this.
func markPlacedTriggerDivisions(reg *cards.Registry, sc Scenario, res rules.OracleResult, ds []rules.OracleDecision) []rules.OracleDecision {
	if reg == nil {
		return ds
	}
	var out []rules.OracleDecision
	for i, d := range ds {
		if d.Kind != "target" || d.Divided != 0 || d.Resume != "" || len(d.PickRefs) == 0 ||
			(d.Via != "target" && d.Via != "answer") {
			continue
		}
		if d.Step < 0 || d.Step >= len(sc.Steps) || d.Step+1 >= len(res.Snapshots) {
			continue
		}
		if op := sc.Steps[d.Step].Op; op == "cast" || op == "activate" {
			continue
		}
		total, ok := placedTriggerTotal(reg, res.Snapshots[d.Step+1], ds, i)
		if !ok || total < len(d.PickRefs) {
			continue
		}
		if out == nil {
			out = append([]rules.OracleDecision(nil), ds...)
		}
		out[i].Divided = total
	}
	if out == nil {
		return ds
	}
	return out
}

// placedTriggerTotal is the divided total of the trigger whose root target ask
// is ds[i], read against snap (the checkpoint after the ask's step, where the
// trigger sits on the stack). ok is false unless exactly one stack ability is
// a divided targeted trigger, so a second trigger cannot lend its total.
func placedTriggerTotal(reg *cards.Registry, snap rules.OracleSnapshot, ds []rules.OracleDecision, i int) (int, bool) {
	var sa *cards.SA
	var face *cards.Face
	var src string
	for _, o := range snap.Stack {
		if o.Kind != "ability" || o.Trigger == "" {
			continue
		}
		idx, err := strconv.Atoi(o.Trigger)
		if err != nil {
			continue
		}
		c, ok := reg.Lookup(oraclediffRefName(o.Source))
		if !ok {
			continue
		}
		for _, f := range digFaces(c, o.Source) {
			if f == nil || idx < 0 || idx >= len(f.Triggers) {
				continue
			}
			t := f.Triggers[idx].Effect
			if t == nil || !dividedTrigger(t) {
				continue
			}
			if sa != nil {
				return 0, false
			}
			sa, face, src = t, f, o.Source
		}
	}
	if sa == nil {
		return 0, false
	}
	n := len(ds[i].PickRefs)
	if j := divideAsk(ds, i); j >= 0 {
		return ds[j].Min + n, true
	}
	if n > 1 {
		// No division ask: the only legal division is one point each.
		return n, true
	}
	if lit := effects.DividedTotal(sa); lit > 0 {
		return lit, true
	}
	return svarTotal(face, strings.TrimSpace(sa.Params["DividedAsYouChoose"]), snap, src)
}

// dividedTrigger reports a trigger effect that divides among targets.
func dividedTrigger(sa *cards.SA) bool {
	switch sa.API {
	case "DealDamage", "PutCounter", "PreventDamage":
	default:
		return false
	}
	return strings.TrimSpace(sa.Params["DividedAsYouChoose"]) != "" && effects.TargetsOf(sa).Targeted()
}

// svarTotal resolves a DividedAsYouChoose$ SVar to a number from the source
// permanent's snapshot: Count$CardCounters.<TYPE> (its counters of that type)
// or Number$<n>. Any other form is unresolved.
func svarTotal(f *cards.Face, name string, snap rules.OracleSnapshot, src string) (int, bool) {
	body, ok := f.SVars[name]
	if !ok {
		return 0, false
	}
	body = strings.TrimSpace(body)
	if v, ok := strings.CutPrefix(body, "Number$"); ok {
		n, err := strconv.Atoi(strings.TrimSpace(v))
		return n, err == nil && n > 0
	}
	kind, ok := strings.CutPrefix(body, "Count$CardCounters.")
	if !ok || kind == "" || strings.ContainsAny(kind, "/$ ") {
		return 0, false
	}
	for _, p := range snap.Permanents {
		if p.Ref == src {
			n := int(p.Counters[kind])
			return n, n > 0
		}
	}
	return 0, false
}
