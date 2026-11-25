package oraclegen

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

func reliveDecision(step int, options []string, pick string) rules.OracleDecision {
	return rules.OracleDecision{Step: step, Kind: "target", Via: "target", Min: 0, Max: 1,
		OptionRefs: options, PickRefs: []string{pick}}
}

// TestLoweredTargetSkipsLaterSlot: a Relive the Past cast whose only ask was
// the land object (gorge poses no ask for an object with no candidate) is
// matched to slot 1 by the slots' own filters, so the plan closes slot 0
// before the target and slot 2 after it; a Forest could not have been the
// artifact or the enchantment object's ask. A Plains-style non-card ref, an
// ambiguous fit and a cast that filled every slot derive nothing.
func TestLoweredTargetSkipsLaterSlot(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	c, ok := reg.Lookup("Relive the Past")
	if !ok {
		t.Fatal("Relive the Past is not in the corpus")
	}
	f := c.Faces[0]
	sc := Scenario{Steps: []Step{{Op: "cast", Seat: 0, Card: "p0:Relive the Past", Targets: []string{"p0:Forest#7"}}, {Op: "resolve"}}}
	forests := []string{"p0:Forest#7", "p0:Forest#8"}

	got := LoweredTargetSkips(reg, f, sc, []rules.OracleDecision{reliveDecision(0, forests, "p0:Forest#7")}, 0)
	want := []XTargetSkip{{At: 0, Slot: 0}, {At: 1, Slot: 2}}
	if !slices.Equal(got, want) {
		t.Fatalf("land-only cast plan = %v, want %v", got, want)
	}
	if err := CheckTargetSkips(SlotSpecs(f), sc.Steps, [][]XTargetSkip{got, nil}); err != nil {
		t.Fatalf("plan fails validation: %v", err)
	}

	// The ask's candidates carry no card the filters can rule out (a token
	// ref): any of the three objects could have posed it, so nothing is guessed.
	tok := sc
	tok.Steps = []Step{{Op: "cast", Seat: 0, Card: "p0:Relive the Past", Targets: []string{"p0:token:Soldier"}}, {Op: "resolve"}}
	if got := LoweredTargetSkips(reg, f, tok, []rules.OracleDecision{reliveDecision(0, []string{"p0:token:Soldier"}, "p0:token:Soldier")}, 0); got != nil {
		t.Errorf("an ambiguous fit derived %v", got)
	}
	// Every object asked: nothing was omitted.
	all := []rules.OracleDecision{
		reliveDecision(0, []string{"p0:Forest#7"}, "p0:Forest#7"),
		reliveDecision(0, forests, "p0:Forest#8"),
		reliveDecision(0, forests, "p0:Forest#9"),
	}
	full := Scenario{Steps: []Step{{Op: "cast", Seat: 0, Card: "p0:Relive the Past", Targets: []string{"p0:Forest#7", "p0:Forest#8", "p0:Forest#9"}}}}
	if got := LoweredTargetSkips(reg, f, full, all, 0); got != nil {
		t.Errorf("a cast that asked every object derived %v", got)
	}
}

// TestFilterRejects: only a certain rejection prunes a slot.
func TestFilterRejects(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, c := range []struct {
		filter, ref string
		want        bool
	}{
		{"Artifact.YouOwn@Graveyard", "p0:Forest#7", true},
		{"Land.YouOwn@Graveyard", "p0:Forest#7", false},
		{"Enchantment.nonAura+YouOwn@Graveyard", "p0:Forest#7", true},
		{"Creature,Land", "p0:Forest#7", false},
		{"Artifact,Enchantment", "p0:Forest#7", true},
		{"Land.nonBasic", "p0:Forest#7", true},
		{"Artifact.YouOwn", "p0:token:Soldier", false},
		{"Artifact.YouOwn", "p1", false},
		{"Artifact.YouOwn", "p0:No Such Card#2", false},
	} {
		if got := filterRejects(reg, c.filter, c.ref); got != c.want {
			t.Errorf("filterRejects(%q, %q) = %v, want %v", c.filter, c.ref, got, c.want)
		}
	}
}
