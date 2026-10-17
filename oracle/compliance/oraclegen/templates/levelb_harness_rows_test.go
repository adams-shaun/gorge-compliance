package templates

import (
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/rules"
)

// TestLevelBHarnessRowsWave4 pins the generator side of the wave-4 Level B
// harness rows (lb4-harness): scenarios XMage could not execute because the
// generated script asked the wrong question, not because either engine
// disagreed. Each case names the XMage failure the row showed and asserts
// the generated item now carries what XMage needs (measured by replaying the
// regenerated scenario in XMage at XMAGE_REF 6b602a1c85e8). The cases are the
// class members of each root cause, so a regression in any class fails here.
func TestLevelBHarnessRowsWave4(t *testing.T) {
	reg := loadGenRegistry(t)
	type check func(t *testing.T, it oraclegen.Item)
	cases := []struct {
		name, key string
		check     check
	}{
		// H1 "{X}": a bare-X cost whose target compares mana value to X
		// announces the fixture target's mana value; X = 0 left the ability
		// with no legal target in either engine.
		{"Likeness Looter", "activate#0.1", func(t *testing.T, it oraclegen.Item) {
			st := it.Steps[activateStepIndex(it.Steps)]
			if len(st.Targets) == 0 || !stepPicks(st, "X = 2") {
				t.Errorf("activate step %+v: want a target and the X = 2 announce", st)
			}
			wantXAnswer(t, it, "choice", "X=2")
		}},
		{"Rydia, Summoner of Mist", "activate#0.0", func(t *testing.T, it oraclegen.Item) {
			st := it.Steps[activateStepIndex(it.Steps)]
			if len(st.Targets) != 1 {
				t.Fatalf("activate step %+v: want the Saga target", st)
			}
			c, ok := reg.Lookup(strings.TrimPrefix(st.Targets[0], "p0:"))
			if !ok {
				t.Fatalf("precondition: target %q not in corpus", st.Targets[0])
			}
			mv := c.Faces[0].ManaValue()
			if mv == 0 || !stepPicks(st, "X = "+strconv.Itoa(int(mv))) {
				t.Errorf("activate step %+v: want X = the target's mana value %d", st, mv)
			}
		}},
		// H1 cost spelling of another permanent's counter: XMage's
		// RemoveCounterCost picks the permanent on the choice queue.
		{"Sunstar Chaplain", "activate#0.0", func(t *testing.T, it oraclegen.Item) {
			wantXAnswer(t, it, "choice", "Grizzly Bears")
		}},
		// H1 self-named grave exile: no catalogue choice ahead of the named
		// target (it was consumed by TargetCardInYourGraveyard and rejected).
		{"Say Its Name", "activate#0.1", func(t *testing.T, it oraclegen.Item) {
			for _, a := range it.XAnswers[activateStepIndex(it.Steps)] {
				if a.Kind == "choice" {
					t.Errorf("activate bucket scripts choice %q; want only the named targets", a.Value)
				}
			}
			wantXAnswer(t, it, "target", "Say Its Name")
		}},
		// H4 tapXType<X/...>: the tap election is answered (X = 1 tapped
		// artifact) and XMage's TapVariableTargetCost announce precedes it.
		{"Secluded Starforge", "activate#0.1", func(t *testing.T, it oraclegen.Item) {
			i := activateStepIndex(it.Steps)
			if !stepPicks(it.Steps[i], "Ornithopter") {
				t.Errorf("activate step %+v: want the Ornithopter tap election", it.Steps[i])
			}
			wantOrder(t, it.XAnswers[i], "X=1", "Ornithopter")
			for _, a := range it.XAnswers[i] {
				if a.Value == "[target_skip]" || a.Value == "no" {
					t.Errorf("activate bucket %+v still scripts the declined tap election", it.XAnswers[i])
				}
			}
		}},
		// H1 activated charm: the fixture gives the chosen mode a legal
		// target (XMage chooses mode and target on activation).
		{"Lost Jitte", "activate#0.0", wantCharmTarget},
		{"Insidious Fungus", "activate#0.0", wantCharmTarget},
		{"Phoenix Down", "activate#0.0", wantCharmTarget},
		// H7 copy new targets: XMage asks "Change this target?" (chooseUse)
		// before any new target (copy_target_answers.go).
		{"Kitsa, Otterball Elite", "activate#0.1", func(t *testing.T, it oraclegen.Item) {
			last := it.XAnswers[len(it.XAnswers)-1]
			if len(last) == 0 || last[0].Kind != "choice" || (last[0].Value != "yes" && last[0].Value != "no") {
				t.Errorf("resolve bucket %+v: want the change-target yes/no first", last)
			}
		}},
		// H7 a permanent exiled and returned transformed: its current face's
		// name selects it (the setup alias binds the old object).
		{"Jill, Shiva's Dominant", "activate#0.0", func(t *testing.T, it oraclegen.Item) {
			wantXAnswer(t, it, "target", "Shiva, Warden of Ice")
		}},
		// H7 a step's own Targets are not scripted a second time.
		{"Meltstrider's Resolve", "static#0.1", wantNoDuplicateTargets},
		{"Choreographed Sparks", "static#0.0", wantNoDuplicateTargets},
		// H7 attached-Aura cost: the Aura is cast onto the source (XMage bins
		// an unattached battlefield Aura before an attach step).
		{"Faunsbane Troll", "activate#0.0", func(t *testing.T, it oraclegen.Item) {
			p0 := it.Scenario.Setup["p0"]
			if !containsString(p0.Hand, "Unholy Strength") || containsString(p0.Battlefield, "Unholy Strength") {
				t.Errorf("p0 setup %+v: want Unholy Strength in hand only", p0)
			}
			if it.Steps[0].Op != "cast" || it.Steps[0].Card != "p0:Unholy Strength" ||
				len(it.Steps[0].Targets) != 1 || it.Steps[0].Targets[0] != "p0:Faunsbane Troll" {
				t.Errorf("first step %+v: want Unholy Strength cast on the Troll", it.Steps[0])
			}
		}},
		// H7 the attacking token is named as both engines name it.
		{"Old Hob, Alleycat Blues", "activate#0.0", func(t *testing.T, it oraclegen.Item) {
			for _, st := range it.Steps {
				if st.Op == "attack" && (len(st.Attackers) != 1 || st.Attackers[0] != "p0:token:Goblin Token") {
					t.Errorf("attack step %+v: want p0:token:Goblin Token", st)
				}
			}
		}},
		// H7 back face kept through the setup drive's upkeep (and the
		// opponent's upkeep before a block): "you may transform" declined.
		{"Avatar Aang", "combat#1.attack", wantFaceKept},
		{"Avatar Aang", "combat#1.block", wantFaceKept},
		{"Avatar Aang", "static#1.0", wantFaceKept},
		// H6 behold with a Kithkin on the battlefield and one in hand:
		// "Reveal from hand" (no) precedes the hand pick.
		{"Champion of the Clachan", "static#0.1", func(t *testing.T, it oraclegen.Item) {
			wantOrder(t, it.XAnswers[0], "no", "Kithkin Greatheart")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name+"/"+tc.key, func(t *testing.T) {
			it, _ := activateRequirement(t, reg, tc.name, tc.key)
			if res, err := rules.RunOracleScenarioJSON(reg, it.Raw()); err != nil || len(res.Fails) != 0 || len(res.Snapshots) == 0 {
				t.Fatalf("precondition: the item must replay in gorge (err=%v fails=%v)", err, res.Fails)
			}
			tc.check(t, it)
		})
	}
}

func stepPicks(st oraclegen.Step, pick string) bool {
	for _, a := range st.Answers {
		for _, p := range a.Pick {
			if p == pick {
				return true
			}
		}
	}
	return false
}

func wantXAnswer(t *testing.T, it oraclegen.Item, kind, value string) {
	t.Helper()
	for _, b := range it.XAnswers {
		for _, a := range b {
			if a.Kind == kind && a.Value == value {
				return
			}
		}
	}
	t.Errorf("no %s answer %q in %+v", kind, value, it.XAnswers)
}

// wantOrder asserts first and then second appear in bucket, in that order.
func wantOrder(t *testing.T, bucket []oraclegen.XAnswer, first, second string) {
	t.Helper()
	i, j := -1, -1
	for k, a := range bucket {
		if a.Value == first && i < 0 {
			i = k
		}
		if a.Value == second && j < 0 {
			j = k
		}
	}
	if i < 0 || j < 0 || i > j {
		t.Errorf("bucket %+v: want %q before %q", bucket, first, second)
	}
}

func wantCharmTarget(t *testing.T, it oraclegen.Item) {
	res, _ := oraclegen.PlaysThrough(loadGenRegistry(t), it.Scenario)
	if charmModeTargetUnchosen(res.Decisions) {
		t.Errorf("the chosen charm mode has no target: %+v", res.Decisions)
	}
}

func wantNoDuplicateTargets(t *testing.T, it oraclegen.Item) {
	for i, st := range it.Steps {
		if (st.Op != "cast" && st.Op != "activate") || len(st.Targets) == 0 || i >= len(it.XAnswers) {
			continue
		}
		for _, a := range it.XAnswers[i] {
			for _, tg := range st.Targets {
				if a.Kind == "target" && (a.Value == tg || "p0:"+a.Value == tg || "p1:"+a.Value == tg) {
					t.Errorf("step %d (%s) scripts its own target %q again: %+v", i, st.Card, tg, it.XAnswers[i])
				}
			}
		}
	}
}

func wantFaceKept(t *testing.T, it oraclegen.Item) {
	declined := false
	for _, a := range it.Scenario.SetupAnswers {
		if a.Kind == "any" && len(a.Pick) == 1 && a.Pick[0] == "no" {
			declined = true
		}
	}
	if !declined {
		t.Errorf("setup answers %+v: want the upkeep self-transform declined", it.Scenario.SetupAnswers)
	}
	wantXAnswer(t, it, "setup_choice", "no")
}
