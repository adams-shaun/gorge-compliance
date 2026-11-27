package templates

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// A Delve counterspell's level-A item pays short of its generic mana with a
// graveyard of Swamps to delve, so gorge's delve ask (CR 702.66) is part of the
// counter-spell scenario instead of a full-pool cast that never reaches it.
// The counter's own step carries the short pool and the exile answer; the
// precast spell it counters still pays its own mana in full. The item's bytes
// moved, so it carries counter-spell generation 2, while a non-Delve
// counterspell keeps the v1 id and name its committed verdict row recorded.
func TestCounterSpellDelveSeedsGraveyard(t *testing.T) {
	if CounterSpell.Version != 1 {
		t.Fatalf("CounterSpell.Version = %d, want 1: bumping it re-names every counter-spell scenario, changed or not", CounterSpell.Version)
	}
	if counterSpellDelve.ID != CounterSpell.ID || counterSpellDelve.Version != 2 {
		t.Fatalf("counterSpellDelve = %+v, want the counter-spell id at version 2", counterSpellDelve)
	}

	reg := testutil.CorpusRegistry(t)
	const name = "Logic Knot"
	f := faceOf(t, reg, name)
	picks := delvePicks(f)
	if picks == 0 {
		t.Fatalf("precondition: %s has Delve and generic mana, delvePicks = 0", name)
	}
	pool, why := oraclegen.PoolFor(f.ManaCost)
	if why != "" {
		t.Fatalf("precondition: no mana pool: %s", why)
	}

	it, skip := Generate(reg, name)
	if skip != nil {
		t.Fatalf("%s skipped: %+v", name, skip)
	}
	if want := fmt.Sprintf("%s/%s/v%d", name, CounterSpell.ID, counterSpellDelve.Version); it.Template != CounterSpell.ID || it.ID != want {
		t.Fatalf("%s: template %q id %q, want %q", name, it.Template, it.ID, want)
	}
	if want := fmt.Sprintf("gen%d-%s", counterSpellDelve.Version, CounterSpell.ID); it.Scenario.Name != want {
		t.Fatalf("%s: scenario name %q, want %q", name, it.Scenario.Name, want)
	}

	sc := it.Scenario
	grave := sc.Setup["p0"].Graveyard
	if len(grave) != picks {
		t.Fatalf("%s: p0 graveyard has %d cards, want %d Swamps", name, len(grave), picks)
	}
	for i, card := range grave {
		if card != delveGrave {
			t.Fatalf("%s: p0 graveyard[%d] = %q, want %s", name, i, card, delveGrave)
		}
	}

	// The counter's own step pays the pool with every delved generic symbol
	// removed and appends the exile answer after the X announcement.
	cast := castStep(t, it, name)
	if want := strings.Replace(pool, "C", "", picks); cast.Mana != want {
		t.Fatalf("%s: cast pays %q, want the short pool %q (full %q, %d delved)", name, cast.Mana, want, pool, picks)
	}
	if len(cast.Answers) == 0 {
		t.Fatalf("%s: cast step has no answers, want the X answer and the Swamp exile", name)
	}
	last := cast.Answers[len(cast.Answers)-1]
	want := oraclegen.Repeat(delveGrave, picks)
	if last.Kind != "choose" || !reflect.DeepEqual(last.Pick, want) {
		t.Fatalf("%s: last cast answer = %+v, want choose of %d Swamps", name, last, picks)
	}

	// The precast spell is not the Delve spell: its own step pays its full
	// mana and scripts no graveyard answer.
	var pre *oraclegen.Step
	for i := range sc.Steps {
		if sc.Steps[i].Op == "cast" && sc.Steps[i].Card != "p0:"+name {
			pre = &sc.Steps[i]
			break
		}
	}
	if pre == nil {
		t.Fatalf("%s: scenario has no precast step: %+v", name, sc.Steps)
	}
	if pre.Card != "p0:Disfigure" || pre.Mana != "B" {
		t.Fatalf("%s: precast = %+v, want p0:Disfigure paying B in full", name, *pre)
	}
	if len(pre.Answers) != 0 {
		t.Fatalf("%s: precast answers = %+v, want none (it is not the Delve spell)", name, pre.Answers)
	}

	// A Delve X spell scripted below the default X must not over-offer
	// Swamps: the seed and the short pool both follow the X actually named.
	if got, short, ok := delvePlan(f, pool, []oraclegen.Answer{{Kind: "choose", Pick: []string{"X = 1"}}}); !ok || got != picks-1 || short != "UU" {
		t.Fatalf("%s at X = 1: delvePlan = (%d, %q, %v), want (%d, %q, true)", name, got, short, ok, picks-1, "UU")
	}

	// The per-item generation is derived from the Delve predicate: a
	// counterspell the arm never touches keeps v1.
	other, skip := Generate(reg, "Counterspell")
	if skip != nil {
		t.Fatalf("Counterspell skipped: %+v", skip)
	}
	if other.ID != "Counterspell/counter-spell/v1" || other.Scenario.Name != "gen1-counter-spell" {
		t.Fatalf("Counterspell = %q %q, want Counterspell/counter-spell/v1 gen1-counter-spell", other.ID, other.Scenario.Name)
	}
}
