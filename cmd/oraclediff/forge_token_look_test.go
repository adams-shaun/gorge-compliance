package main

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// arrangeOf is the one arrange decision of a Forge request.
func arrangeOf(t *testing.T, ds []rules.OracleDecision) rules.OracleDecision {
	t.Helper()
	var got []rules.OracleDecision
	for _, d := range ds {
		if d.GorgeKind == "arrange" {
			got = append(got, d)
		}
	}
	if len(got) != 1 {
		t.Fatalf("want one arrange decision, got %+v", ds)
	}
	return got[0]
}

// TestForgeTokenTopLook: Vanish from Sight puts a token on top of its owner's
// library and surveils 1 (xe:RAND:4:16:w8). Gorge looks at the token and bins or
// keeps it (CR 111.8: nothing moves), while Forge drops the token and looks at
// the Plains under it. gorge's empty pick would bin that Plains, and a pick
// naming the token matches nothing there, so the export names the card now on
// top of the library; a look at the Plains itself (token put on the bottom)
// stays gorge's own answer.
func TestForgeTokenTopLook(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	window := func(where string, arrange []string) rules.OracleDecision {
		t.Helper()
		it := oraclegen.Item{ID: "xe:test:forge-token-top-look", Scenario: oraclegen.Scenario{
			Name: "vanish from sight " + where, Why: "a one-card surveil's look is a token put on top",
			Setup: map[string]oraclegen.Seat{
				"p0": {Hand: []string{"Vanish from Sight", "Raise the Alarm"}, LibraryTop: []string{"Plains", "Mountain"}},
			},
			Steps: []oraclegen.Step{
				{Op: "cast", Seat: 0, Card: "p0:Raise the Alarm", Mana: "CW"},
				{Op: "resolve"},
				{Op: "cast", Seat: 0, Card: "p0:Vanish from Sight", Mana: "CCCU", Targets: []string{"p0:token:Soldier"}},
				{Op: "resolve", Answers: []oraclegen.Answer{{Kind: "any", Pick: []string{where}}, {Kind: "arrange", Pick: arrange}}},
			},
		}}
		rq, g := forgeExportItem(reg, it, true)
		if len(g.Fails) > 0 {
			t.Fatalf("gorge rejects the window: %v", g.Fails)
		}
		return arrangeOf(t, rq.GorgeDecisions)
	}
	for _, c := range []struct {
		name    string
		where   string
		arrange []string
	}{
		{"token on top, binned", "top", []string{}},
		{"token on top, kept", "top", []string{"Soldier"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			d := window(c.where, c.arrange)
			if want := []string{"Plains"}; !reflect.DeepEqual(d.Picks, want) {
				t.Fatalf("picks = %q, want %q (the card Forge looks at stays on top)", d.Picks, want)
			}
			if want := []string{"p0:Plains"}; !reflect.DeepEqual(d.PickRefs, want) {
				t.Fatalf("pick refs = %q, want %q", d.PickRefs, want)
			}
		})
	}
	t.Run("token on the bottom, Plains binned", func(t *testing.T) {
		d := window("bottom", []string{})
		if len(d.Picks) != 0 {
			t.Fatalf("picks = %q, want gorge's empty pick (bin the Plains)", d.Picks)
		}
	})
}

// TestForgeTokenLooksGuards: a decision the rewrite cannot place is left as
// gorge answered it, and the input is never mutated.
func TestForgeTokenLooksGuards(t *testing.T) {
	look := rules.OracleDecision{
		Step: 0, Seat: 0, Kind: "order", GorgeKind: "arrange", ArrangeKind: "graveyard",
		Options: 1, OptionRefs: []string{"p0:token:Soldier"}, ArrangeLabels: []string{"Soldier"},
	}
	snap := func(top ...string) rules.OracleResult {
		return rules.OracleResult{Snapshots: []rules.OracleSnapshot{
			{}, {Players: []rules.OracleSnapPlayer{{Seat: 0, LibraryTop: top}}},
		}}
	}
	ds := []rules.OracleDecision{look}
	for name, g := range map[string]rules.OracleResult{
		"no checkpoint after the step": {},
		"library top too short":        snap(),
	} {
		if got := forgeTokenLooks(g, ds); !reflect.DeepEqual(got, ds) {
			t.Fatalf("%s: rewrote %+v", name, got)
		}
	}
	got := forgeTokenLooks(snap("Plains"), ds)
	if !reflect.DeepEqual(got[0].Picks, []string{"Plains"}) {
		t.Fatalf("picks = %q", got[0].Picks)
	}
	if len(ds[0].Picks) != 0 || len(ds[0].PickRefs) != 0 {
		t.Fatalf("input mutated: %+v", ds[0])
	}
}
