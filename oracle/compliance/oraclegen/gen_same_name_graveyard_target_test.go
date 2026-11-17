package oraclegen

import (
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
)

// TestSameNameGraveyardTargetCarriesOwner pins the ECL Rooftop Percher
// lowering (xe:RAND:1:13:w22): two same-named graveyard cards owned by
// different seats are an Unanswerable name-only pick, but the owner prefix is
// exact, so the target answer carries it. A bare name lets XMage bind the
// first same-named graveyard card it visits (p0's) instead of gorge's pick
// (p1's), contrary to CR 601.2c's announced object.
func TestSameNameGraveyardTargetCarriesOwner(t *testing.T) {
	d := rules.OracleDecision{
		Step: 16, Seat: 0, Kind: "target", GorgeKind: "target", Options: 2, Min: 0, Max: 1,
		Picks: []string{"Dawn-Blessed Pennant"}, PickRefs: []string{"p1:Dawn-Blessed Pennant"},
		PickIdx: []int{1}, PickKinds: []string{"card"},
		OptionRefs:      []string{"p0:Dawn-Blessed Pennant", "p1:Dawn-Blessed Pennant"},
		PickRefsInexact: []bool{true},
	}
	// Precondition: the ref is rank-derived, so the name-only classifier
	// still refuses it; the owner prefix is the remaining exact identity.
	if p := ClassifySameName(d, 0); !p.Ambiguous || !p.Unanswerable || p.Alias != "" {
		t.Fatalf("fixture must be an unanswerable same-name name-only pick: %+v", p)
	}
	as := targetDecisionAnswers(newAnswerRouting([]rules.OracleDecision{d}), 0)
	if len(as) != 1 || as[0].Kind != "target" {
		t.Fatalf("answers = %+v, want one target", as)
	}
	if as[0].Value == "Dawn-Blessed Pennant" {
		t.Fatalf("target answer = %q, want the owner-qualified ref (a bare name matches both graveyards)", as[0].Value)
	}
	if as[0].Value != "p1:Dawn-Blessed Pennant" {
		t.Fatalf("target answer = %q, want %q", as[0].Value, "p1:Dawn-Blessed Pennant")
	}

	// The other target spelling home (a decision routed from the pick queue to
	// the target queue) must carry the same owner form.
	if got := targetPickValue(d, 0, "Dawn-Blessed Pennant"); got != "p1:Dawn-Blessed Pennant" {
		t.Fatalf("targetPickValue = %q, want %q", got, "p1:Dawn-Blessed Pennant")
	}
	// The choice-queue spelling shares the rule (a same-name pick on
	// makeChoose), so the owner form cannot drift between queues.
	if got := disambiguatedObjectChoice(d, 0, "Dawn-Blessed Pennant"); got != "@p1:Dawn-Blessed Pennant" {
		t.Fatalf("disambiguatedObjectChoice = %q, want %q", got, "@p1:Dawn-Blessed Pennant")
	}
	// A joined multi-pick answer mixing an owner-qualified pick with an exact
	// alias pick keeps every segment in "@<ref>" form, so the driver binds
	// each one; a bare "p1:<name>" segment would match no alias and no card.
	mixed := rules.OracleDecision{
		Step: 3, Seat: 0, Kind: "choose", GorgeKind: "choose", Options: 4, Min: 2, Max: 2,
		Picks:           []string{"Dawn-Blessed Pennant", "Grizzly Bears"},
		PickRefs:        []string{"p1:Dawn-Blessed Pennant", "p0:Grizzly Bears#2"},
		PickKinds:       []string{"card", "card"},
		OptionRefs:      []string{"p0:Dawn-Blessed Pennant", "p1:Dawn-Blessed Pennant", "p0:Grizzly Bears", "p0:Grizzly Bears#2"},
		PickRefsInexact: []bool{true, false},
	}
	if got, want := JoinedChoice(mixed), "@p1:Dawn-Blessed Pennant^@p0:Grizzly Bears#2"; got != want {
		t.Fatalf("JoinedChoice = %q, want %q", got, want)
	}
	mixed.Picks[0], mixed.Picks[1] = mixed.Picks[1], mixed.Picks[0]
	mixed.PickRefs[0], mixed.PickRefs[1] = mixed.PickRefs[1], mixed.PickRefs[0]
	mixed.PickRefsInexact[0], mixed.PickRefsInexact[1] = mixed.PickRefsInexact[1], mixed.PickRefsInexact[0]
	if got, want := JoinedChoice(mixed), "@p0:Grizzly Bears#2^@p1:Dawn-Blessed Pennant"; got != want {
		t.Fatalf("reversed JoinedChoice = %q, want %q", got, want)
	}
	// Batch cost paths that script one answer per pick go through the same
	// exact-ref helper.
	if got := ExactRefAlias(d, 0); got != "@p1:Dawn-Blessed Pennant" {
		t.Fatalf("ExactRefAlias = %q, want %q", got, "@p1:Dawn-Blessed Pennant")
	}

	// Control: when the owner does not separate the candidates (a ranked ref
	// with a same-name sibling on the same seat), the conservative bare name
	// is kept rather than emitting a rank the driver's zone order cannot bind.
	sameSeat := d
	sameSeat.PickRefs = []string{"p1:Dawn-Blessed Pennant#2"}
	sameSeat.OptionRefs = []string{"p1:Dawn-Blessed Pennant", "p1:Dawn-Blessed Pennant#2"}
	if got := targetDecisionAnswers(newAnswerRouting([]rules.OracleDecision{sameSeat}), 0); len(got) != 1 || got[0].Value != "Dawn-Blessed Pennant" {
		t.Fatalf("owner-ambiguous answer = %+v, want the bare name", got)
	}
}
