package templates

import (
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/levelb"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/cards"
)

// A DisableTriggers static scoped to ward (Nowhere to Run's "Ward abilities
// of those creatures don't trigger", ValidTrigger$ Triggered.Ward) suppresses
// no enters trigger, so the Elvish Visionary probe disableTriggersItem uses
// observes nothing. The ward observation targets an opposing ward creature
// with a Shock: without the card the ward trigger is on the stack above the
// Shock (CR 702.21a), with it the trigger never fires.

// wardProbe is p1's creature whose only ability is ward; wardProbeSpell the
// targeting spell p0 casts at it.
const (
	wardProbe          = "Tomakul Honor Guard"
	wardProbeSpell     = "Shock"
	wardProbeSpellMana = "R"
)

// disablesWard reports whether a DisableTriggers static names ward triggers.
func disablesWard(st cards.Static) bool {
	return strings.Contains(st.Params["ValidTrigger"], "Ward")
}

// disableWardItem serves the ward-scoped DisableTriggers static.
func disableWardItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, bool) {
	if _, ok := reg.Lookup(wardProbe); !ok {
		return oraclegen.Item{}, false
	}
	if _, ok := reg.Lookup(wardProbeSpell); !ok {
		return oraclegen.Item{}, false
	}
	ref := "p1:" + wardProbe
	build := func(card bool, want bool) oraclegen.Scenario {
		var bf []string
		if card {
			bf = []string{name}
		}
		steps := []oraclegen.Step{
			{Op: "pass_to", Seat: 0, Step: "main1", Decision: "priority"},
			{Op: "cast", Seat: 0, Card: "p0:" + wardProbeSpell, Mana: wardProbeSpellMana, Targets: []string{ref},
				Expect: []oraclegen.Expect{{TriggerOnStack: ref, Want: boolPtr(want)}}},
		}
		sc := staticScenario(f, name, bf, []string{wardProbeSpell}, steps)
		p1 := sc.Setup["p1"]
		p1.Battlefield = appendFixtureUnique(p1.Battlefield, wardProbe)
		sc.Setup["p1"] = p1
		if card {
			p0 := sc.Setup["p0"]
			setupBackFace(&p0, name, req)
			sc.Setup["p0"] = p0
		}
		return sc
	}
	if control, ok := runStatic(reg, build(false, true)); !ok || len(control.Fails) != 0 {
		return oraclegen.Item{}, false
	}
	sc := build(true, false)
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 {
		return oraclegen.Item{}, false
	}
	it := oraclegen.NewLevelBItem(name, req.Key, StaticObserved.Version, []string{"614.1", "702.21a"}, sc)
	it.XAnswers = oraclegen.XAnswersForScenario(reg, res, sc, oraclegen.ModeNumbers(f), nil)
	return it, true
}
