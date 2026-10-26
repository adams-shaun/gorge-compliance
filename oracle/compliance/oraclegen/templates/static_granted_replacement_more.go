// Two more Level-B observations for AddReplacementEffect$ statics (ticket
// agent-20261010T160945Z-e004cc3c), served from staticGrantedReplacementItem:
//
//   - a granted DamageDone PREVENTION whose body is DB$ ReplaceDamage (the
//     Level-up Hedron-Field Purists' two LEVEL statics): the card sits on the
//     battlefield holding the LEVEL counters its own gate needs, p0 casts
//     Shock at p0, and p0 ends at 20 - (2 - Amount). The control is the same
//     scenario with the counters absent: the grant is off, p0 ends at 18.
//   - a granted Untap replacement whose body is DB$ RemoveCounter (Bewitching
//     Leechcraft): the Aura is cast on a creature holding a +1/+1 counter, its
//     ETB trigger taps it, and p0's next untap step removes the counter and
//     untaps it. The control is the same creature, tapped, with no Aura: it
//     untaps and keeps its counter. A third generator-side run with no counter
//     proves the "otherwise it doesn't untap" arm (the creature stays tapped).
//
// Not served here: Master Chef and Scion of Halaster grant to a COMMANDER, and
// the XMage driver rejects command-zone setup (ScenarioReplay "command zone
// setup unsupported"), so no scenario both engines can run exists for them.
package templates

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/levelb"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
)

// staticGrantedPreventItem tries only the ReplaceDamage prevention arm over
// st's granted replacements; the other bodies stay with
// staticGrantedReplacementItem's late call.
func staticGrantedPreventItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, st cards.Static) (oraclegen.Item, bool) {
	for _, nm := range cards.SplitGrantNames(strings.TrimSpace(st.ParamStr(cards.PKAddReplacementEffect))) {
		r, ok := cards.ParseReplacementLine(f.SVars[nm])
		if !ok || r.EventKind() != cards.ReplDamageDone {
			continue
		}
		if it, ok := grantedReplacementPreventItem(reg, f, name, req, st, r); ok {
			return it, true
		}
	}
	return oraclegen.Item{}, false
}

// grantedReplacementPreventItem serves a counter-gated granted DamageDone
// replacement whose body is DB$ ReplaceDamage | Amount$ N.
func grantedReplacementPreventItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, st cards.Static, r cards.Repl) (oraclegen.Item, bool) {
	with := cards.ResolveSVar(f.SVars, strings.TrimSpace(r.ParamStr(cards.PKReplaceWith)))
	if with == nil || with.API != "ReplaceDamage" {
		return oraclegen.Item{}, false
	}
	amt, err := strconv.Atoi(strings.TrimSpace(with.ParamStr(cards.PKAmount)))
	if err != nil || amt <= 0 || amt > 2 {
		return oraclegen.Item{}, false
	}
	// The control needs a gate to switch off.
	kind, n, gated := staticCounterGate(&st)
	if !gated {
		return oraclegen.Item{}, false
	}
	shock, ok := castProbe(reg, "Shock", "p0")
	if !ok || !oraclegen.XMageKnown("Shock") {
		return oraclegen.Item{}, false
	}
	steps := []oraclegen.Step{shock, {Op: "resolve"}}
	sc := staticScenario(f, name, []string{name}, []string{"Shock"}, steps)
	sc.Setup["p0"] = oraclegen.WithCounters(sc.Setup["p0"], name, kind, n)
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 || len(res.Snapshots) == 0 {
		return oraclegen.Item{}, false
	}
	// The grant prevented Amount of Shock's 2.
	want := int32(20 - (2 - amt))
	if life, ok := snapshotLife(res.Snapshots[len(res.Snapshots)-1], 0); !ok || life != want {
		return oraclegen.Item{}, false
	}
	// Gate-off control: no LEVEL counters, so Shock lands its printed 2.
	ctl := sc
	ctl.Setup = cloneSeats(sc.Setup)
	cp0 := ctl.Setup["p0"]
	delete(cp0.Counters, name)
	ctl.Setup["p0"] = cp0
	cres, cok := runStatic(reg, ctl)
	if !cok || len(cres.Fails) != 0 || len(cres.Snapshots) == 0 {
		return oraclegen.Item{}, false
	}
	if life, ok := snapshotLife(cres.Snapshots[len(cres.Snapshots)-1], 0); !ok || life != 18 {
		return oraclegen.Item{}, false
	}
	it := oraclegen.NewLevelBItem(name, req.Key, StaticApplies.Version, []string{"614.1a", "615.1"}, sc)
	it.XAnswers = oraclegen.XAnswersForScenario(reg, res, sc, oraclegen.ModeNumbers(f), nil)
	return it, true
}

// grantedReplacementUntapItem serves an Aura-granted Untap replacement whose
// body is DB$ RemoveCounter.
func grantedReplacementUntapItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, st cards.Static, r cards.Repl) (oraclegen.Item, bool) {
	with := cards.ResolveSVar(f.SVars, strings.TrimSpace(r.ParamStr(cards.PKReplaceWith)))
	if with == nil || with.API != "RemoveCounter" || !oraclegen.HasType(f, "Aura") {
		return oraclegen.Item{}, false
	}
	pool, why := oraclegen.PoolFor(f.ManaCost)
	if why != "" {
		return oraclegen.Item{}, false
	}
	host := staticProbe
	hostRef := "p0:" + host
	untap := []oraclegen.Step{
		{Op: "pass_to", Step: "main2"},
		{Op: "pass_to", Step: "main1", Active: "p0"},
	}
	build := func(counters int32, aura bool) oraclegen.Scenario {
		var steps []oraclegen.Step
		if aura {
			// Resolve the Aura spell, then its ETB "tap enchanted creature".
			steps = append(steps,
				oraclegen.Step{Op: "cast", Seat: 0, Card: "p0:" + name, Mana: pool, Targets: []string{hostRef}},
				oraclegen.Step{Op: "resolve"}, oraclegen.Step{Op: "resolve"})
		}
		steps = append(steps, untap...)
		var hand []string
		if aura {
			hand = []string{name}
		}
		sc := staticScenario(f, name, []string{host}, hand, steps)
		p0 := sc.Setup["p0"]
		if counters > 0 {
			p0 = oraclegen.WithCounters(p0, host, "P1P1", counters)
		}
		if !aura {
			p0.Tapped = []string{host}
		}
		sc.Setup["p0"] = p0
		return sc
	}
	bears := func(res rules.OracleResult) (rules.OracleSnapPerm, bool) {
		if len(res.Snapshots) == 0 {
			return rules.OracleSnapPerm{}, false
		}
		for _, p := range res.Snapshots[len(res.Snapshots)-1].Permanents {
			if p.Name == host && p.Controller == 0 {
				return p, true
			}
		}
		return rules.OracleSnapPerm{}, false
	}
	run := func(sc oraclegen.Scenario) (rules.OracleSnapPerm, rules.OracleResult, bool) {
		res, ok := runStatic(reg, sc)
		if !ok || len(res.Fails) != 0 {
			return rules.OracleSnapPerm{}, res, false
		}
		p, ok := bears(res)
		return p, res, ok
	}
	// Aura on a creature holding one counter: the untap removes it and untaps.
	sc := build(1, true)
	got, res, ok := run(sc)
	if !ok || got.Tapped || got.Counters["P1P1"] != 0 {
		return oraclegen.Item{}, false
	}
	// Control (no Aura, creature tapped): it untaps and keeps its counter.
	cgot, _, ok := run(build(1, false))
	if !ok || cgot.Tapped || cgot.Counters["P1P1"] != 1 {
		return oraclegen.Item{}, false
	}
	// "Otherwise it doesn't untap": with no counter the creature stays tapped.
	zgot, _, ok := run(build(0, true))
	if !ok || !zgot.Tapped {
		return oraclegen.Item{}, false
	}
	it := oraclegen.NewLevelBItem(name, req.Key, StaticApplies.Version, []string{"614.1a", "616.1"}, sc)
	it.XAnswers = oraclegen.XAnswersForScenario(reg, res, sc, oraclegen.ModeNumbers(f), nil)
	return it, true
}
