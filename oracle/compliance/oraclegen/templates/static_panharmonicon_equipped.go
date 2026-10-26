package templates

import (
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/levelb"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/cards"
)

// panharmoniconEquipProbes are the Equipment probes for an equipped-self
// Panharmonicon ("if an ability of NICKNAME or an Equipment attached to it
// triggers, that ability triggers an additional time": Cloud, Midgar
// Mercenary). Armory of Iroas's only feature besides Equip is "Whenever
// equipped creature attacks, put a +1/+1 counter on it": untargeted,
// non-optional and unconditional, so the attack poses no ask but the trigger
// order. Checked in XMage's card database by oraclegen.XMageKnown.
var panharmoniconEquipProbes = []string{"Armory of Iroas"}

// panharmoniconEquipControlBearer carries the probe in the control, where
// the card under test is absent: a vanilla creature no baseline places, so
// its ref is unambiguous.
const panharmoniconEquipControlBearer = "Savannah Lions"

// panharmoniconEquippedItem serves an equipped-self Panharmonicon whose card
// is a creature: the probe Equipment is attached to the card at setup and the
// card attacks, so the Equipment's attack trigger fires TWICE (CR 603.2d via
// the static); in the control the same Equipment on a vanilla attacker fires
// ONCE. As in panharmoniconItem, gorge's own matcher decides: the item is
// kept only for exactly 2 vs 1 stack entries from the probe at the attack
// checkpoint, so the static's filter is never parsed here.
func panharmoniconEquippedItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, bool) {
	if !f.IsCreature() || req.Face != 0 {
		return oraclegen.Item{}, false
	}
	for _, probe := range panharmoniconEquipProbes {
		if !oraclegen.XMageKnown(probe) {
			continue
		}
		if _, ok := reg.Lookup(probe); !ok {
			continue
		}
		sc := panharmoniconEquipScenario(f, name, probe, name)
		res, ok := runStatic(reg, sc)
		if !ok || len(res.Fails) != 0 || len(res.Snapshots) < 3 {
			continue
		}
		// snaps: setup, attach, attack, resolve -- the attack checkpoint
		// holds the doubled trigger.
		if n, others := listenerAbilities(res.Snapshots[2], "p0:"+probe); n != 2 || others != 0 {
			continue
		}
		control, cok := runStatic(reg, panharmoniconEquipScenario(f, name, probe, panharmoniconEquipControlBearer))
		if !cok || len(control.Fails) != 0 || len(control.Snapshots) < 3 {
			continue
		}
		if n, others := listenerAbilities(control.Snapshots[2], "p0:"+probe); n != 1 || others != 0 {
			continue
		}
		it := oraclegen.NewLevelBItem(name, req.Key, StaticObserved.Version, []string{"603.2d", "301.5"}, sc)
		it.XAnswers = oraclegen.XAnswersForScenario(reg, res, sc, oraclegen.ModeNumbers(f), nil)
		return it, true
	}
	return oraclegen.Item{}, false
}

// panharmoniconEquipScenario attaches probe to bearer, attacks with bearer
// and resolves the triggers. With bearer = the card under test it is the
// served scenario; with a vanilla bearer it is the control, the card absent.
func panharmoniconEquipScenario(f *cards.Face, name, probe, bearer string) oraclegen.Scenario {
	steps := []oraclegen.Step{
		{Op: "attach", Seat: 0, Card: "p0:" + probe, AttachedTo: "p0:" + bearer},
		{Op: "attack", Seat: 0, Attackers: []string{"p0:" + bearer}, Defender: "p1"},
		{Op: "resolve", Seat: 0},
	}
	return staticScenario(f, name, []string{bearer, probe}, nil, steps)
}
