package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/levelb"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
)

// TestLandPlayedZoneRowsGenerate is the class table for the origin-scoped
// LandPlayed trigger (wave-4 G5 LandPlayed rows): each row generates, plays
// through gorge, plays its land from a non-hand zone, and shows the row's own
// trigger on the stack.
func TestLandPlayedZoneRowsGenerate(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct{ name, key string }{
		{"Gwen Stacy", "trigger#1.0"},
		{"Shadow of the Goblin", "trigger#0.1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			it := triggerRequirement(t, reg, tc.name, tc.key, levelb.LandPlayedZoneSub)
			if res, ok := oraclegen.PlaysThrough(reg, it.Scenario); !ok || len(res.Fails) != 0 {
				t.Fatalf("does not play through gorge: ok=%v fails=%v", ok, res.Fails)
			}
			played := false
			for _, st := range it.Scenario.Steps {
				played = played || st.Op == "play"
			}
			if !played {
				t.Fatalf("no play step: %+v", it.Scenario.Steps)
			}
			slot := tc.key[strings.LastIndexByte(tc.key, '.')+1:]
			c, _ := reg.Lookup(tc.name)
			face := c.Faces[0]
			if strings.HasPrefix(tc.key, "trigger#1.") {
				face = c.Faces[1]
			}
			res, _ := oraclegen.PlaysThrough(reg, it.Scenario)
			if !abilityOnStack(res.Snapshots, stackSourceWants(reg, tc.name, face), slot) {
				t.Fatalf("no snapshot shows trigger %s on the stack", slot)
			}
		})
	}
}

// TestChosenCardLeavesRowGenerates: Zenos yae Galvus's "When the chosen
// creature leaves the battlefield" is served by casting Zenos (its ETB
// chooses p1's creature) and removing that creature; the row's trigger
// reaches the stack.
func TestChosenCardLeavesRowGenerates(t *testing.T) {
	reg := loadGenRegistry(t)
	it := triggerRequirement(t, reg, "Zenos yae Galvus", "trigger#0.1", "trigger.zone-change-residue")
	res, ok := oraclegen.PlaysThrough(reg, it.Scenario)
	if !ok || len(res.Fails) != 0 {
		t.Fatalf("does not play through gorge: ok=%v fails=%v", ok, res.Fails)
	}
	// The trigger's body transforms Zenos: the resolved scenario shows its
	// back face, which only the row's own trigger produces.
	for _, p := range res.Snapshots[len(res.Snapshots)-1].Permanents {
		if p.Name == "Shinryu, Transcendent Rival" {
			return
		}
	}
	t.Fatalf("Zenos did not transform: %+v", res.Snapshots[len(res.Snapshots)-1].Permanents)
}

// TestStateCounterDoublingRowGenerates: The Millennium Calendar's "When
// there are 1,000 or more time counters" state trigger is caused by its own
// doubling activation from 500 counters; the trigger sacrifices it.
func TestStateCounterDoublingRowGenerates(t *testing.T) {
	reg := loadGenRegistry(t)
	it := triggerRequirement(t, reg, "The Millennium Calendar", "trigger#0.1", "trigger.state-self-counters")
	res, ok := oraclegen.PlaysThrough(reg, it.Scenario)
	if !ok {
		t.Fatalf("does not play through gorge: fails=%v", res.Fails)
	}
	last := res.Snapshots[len(res.Snapshots)-1]
	for _, p := range last.Permanents {
		if p.Name == "The Millennium Calendar" {
			t.Fatalf("the Calendar is still on the battlefield: %+v", p)
		}
	}
}

// TestAttachedRelativeBearerRowGenerates: Eriette, the Beguiler's
// "Whenever an Aura you control becomes attached to a nonland permanent an
// opponent controls with mana value <= that Aura's" is caused by Pacifism on
// p1's Grizzly Bears; its gain-control effect takes the Bears.
func TestAttachedRelativeBearerRowGenerates(t *testing.T) {
	reg := loadGenRegistry(t)
	it := triggerRequirement(t, reg, "Eriette, the Beguiler", "trigger#0.0", "trigger.attached")
	res, ok := oraclegen.PlaysThrough(reg, it.Scenario)
	if !ok || len(res.Fails) != 0 {
		t.Fatalf("does not play through gorge: ok=%v fails=%v", ok, res.Fails)
	}
	for _, p := range res.Snapshots[len(res.Snapshots)-1].Permanents {
		if p.Name == "Grizzly Bears" && p.Owner == 1 && p.Controller == 0 {
			return
		}
	}
	t.Fatalf("p0 did not gain p1's Grizzly Bears: %+v", res.Snapshots[len(res.Snapshots)-1].Permanents)
}

// TestChosenCardLeavesAnswersAreTargets: XMage asks Zenos's ETB pick as a
// target, so the item scripts it as a target answer (a choice-queue answer is
// consumed by the wrong ask), while Shinryu's "choose an opponent" is
// player.choose over a TargetOpponent, which reads the choice queue: it stays
// a choice.
func TestChosenCardLeavesAnswersAreTargets(t *testing.T) {
	reg := loadGenRegistry(t)
	it := triggerRequirement(t, reg, "Zenos yae Galvus", "trigger#0.1", "trigger.zone-change-residue")
	var got []string
	for _, step := range it.XAnswers {
		for _, a := range step {
			got = append(got, a.Kind+" "+a.Value)
		}
	}
	if strings.Join(got, ",") != "target p1:Grizzly Bears,choice p1" {
		t.Fatalf("answers %v, want [target p1:Grizzly Bears, choice p1]", got)
	}
}
