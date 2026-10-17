package templates

import (
	"testing"
)

// TestStaticPanharmoniconEquippedServed: an equipped-self Panharmonicon
// (Cloud, Midgar Mercenary: "if an ability of Cloud or an Equipment attached
// to it triggers, that ability triggers an additional time", CR 603.2d) is
// served by attaching the Equipment probe and attacking: TWO probe triggers at
// the attack checkpoint with the card as the bearer, ONE with a vanilla
// bearer and the card absent.
func TestStaticPanharmoniconEquippedServed(t *testing.T) {
	reg := loadGenRegistry(t)
	const card = "Cloud, Midgar Mercenary"
	req := panharmoniconReq(t, reg, card)
	it, skip := GenerateB(reg, card, req)
	if skip != nil {
		t.Fatalf("skipped: %s", skip.Reason)
	}
	probe := panharmoniconEquipProbes[0]
	with := runItemScenario(t, reg, it, true)
	if len(with.Snapshots) < 3 || !panharmoniconOnBattlefield(with.Snapshots[2], card) {
		t.Fatalf("precondition: %s is not on the battlefield at the attack checkpoint", card)
	}
	if n, others := listenerAbilities(with.Snapshots[2], "p0:"+probe); n != 2 || others != 0 {
		t.Errorf("with %s: %d %s triggers (+%d others), want 2", card, n, probe, others)
	}
	c, _ := reg.Lookup(card)
	f := c.Faces[0]
	control, ok := runStatic(reg, panharmoniconEquipScenario(f, card, probe, panharmoniconEquipControlBearer))
	if !ok || len(control.Fails) != 0 || len(control.Snapshots) < 3 {
		t.Fatalf("control does not replay: %v", control.Fails)
	}
	if n, others := listenerAbilities(control.Snapshots[2], "p0:"+probe); n != 1 || others != 0 {
		t.Errorf("control: %d %s triggers (+%d others), want 1", n, probe, others)
	}
}
