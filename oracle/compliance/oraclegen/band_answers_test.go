package oraclegen

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
)

// TestBandAnswersScriptJoinThenDecline pins the measured xengine band decision
// shape (xe:SC15:s1000003:1000003:tape step 269): one KChoose per band answer,
// with the accepted member spelled as the object's name and the decline as
// "no". XMage's Combat.handleBanding asks a chooseUse ("Form a band ...?") and
// then a TargetControlledPermanent for the member. The member selection
// reaches TestPlayer.makeChoose on the CHOICE queue (Target.choose ->
// Player.choose), so the accepted pick needs yes + the member there, and a
// decline needs no. Scripting the gorge label (the generic choice route)
// leaves chooseUse unanswered and desyncs the rest of the step (CR 702.22c).
func TestBandAnswersScriptJoinThenDecline(t *testing.T) {
	join := rules.OracleDecision{
		Step: 2, Seat: 1, Kind: "choose_n", Options: 3, GorgeKind: "choose", Via: "answer",
		Picks: []string{"Band Copper Gnomes with Timber Wolves"}, PickIdx: []int{1},
		PickRefs: []string{"p1:Copper Gnomes"}, PickRefsInexact: []bool{true},
		OptionRefs:  []string{"p1:Copper Gnomes", "p1:Timber Wolves#2"},
		ObjectPicks: []string{"p1:Copper Gnomes"}, PickKinds: []string{"band"}, Min: 1, Max: 1,
	}
	decline := rules.OracleDecision{
		Step: 2, Seat: 1, Kind: "choose_n", Options: 2, GorgeKind: "choose", Via: "answer",
		Picks: []string{"No more creatures in Timber Wolves's band"}, PickIdx: []int{0},
		PickRefs:   []string{"No more creatures in Timber Wolves's band"},
		OptionRefs: []string{"p1:Timber Wolves#2"},
		PickKinds:  []string{"band"}, Min: 1, Max: 1,
	}
	got := routed(t, []rules.OracleDecision{join, decline}, 3)
	want := []XAnswer{
		{Seat: 1, Kind: "choice", Value: "yes"},
		{Seat: 1, Kind: "choice", Value: "Copper Gnomes"},
		{Seat: 1, Kind: "choice", Value: "no"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("band answers = %#v, want %#v", got, want)
	}
	// The routing is keyed on the engine's pick kind, not the label: an
	// ordinary permanent pick keeps its own target-queue route.
	generic := rules.OracleDecision{
		Step: 2, Seat: 1, Kind: "choose_n", Options: 3, GorgeKind: "choose", Via: "answer",
		Picks: []string{"Copper Gnomes"}, PickIdx: []int{1},
		PickRefs: []string{"p1:Copper Gnomes"}, ObjectPicks: []string{"p1:Copper Gnomes"},
		PickKinds: []string{"permanent"}, Min: 1, Max: 1,
	}
	if got := routed(t, []rules.OracleDecision{generic}, 3); len(got) != 1 || got[0].Kind != "target" {
		t.Fatalf("a permanent pick must keep its own route, got %#v", got)
	}
}

// TestBandAnswersSameNameMemberIsNotTheLeaderRef is the seed-2 SC15 certdeck
// shape that used to harness: attackers p0:Kjeldoran Warrior,
// p0:Kjeldoran Warrior#2, p0:Garrison Griffin; the second band ask is for
// leader #2 and accepts the first copy. The accepted ref is therefore a real
// setup ref that registerAliases can bind to the leader (the band target
// excludes the leader), so the answer must be the member's name, not the raw
// ref. XMage's target then has exactly one same-name candidate.
func TestBandAnswersSameNameMemberIsNotTheLeaderRef(t *testing.T) {
	// The exact decision recorded in xe:SC15:s2000013:2000013:w15 step 10.
	sameName := rules.OracleDecision{
		Step: 10, Seat: 0, Kind: "choose_n", Options: 3, GorgeKind: "choose", Via: "answer",
		Picks: []string{"Band Kjeldoran Warrior with Kjeldoran Warrior"}, PickIdx: []int{1},
		PickRefs: []string{"p0:Kjeldoran Warrior"}, PickRefsInexact: []bool{false},
		OptionRefs:  []string{"p0:Kjeldoran Warrior", "p0:Garrison Griffin"},
		ObjectPicks: []string{"p0:Kjeldoran Warrior"},
		PickKinds:   []string{"band"}, Min: 1, Max: 1,
	}
	got := routed(t, []rules.OracleDecision{sameName}, 11)
	want := []XAnswer{
		{Seat: 0, Kind: "choice", Value: "yes"},
		{Seat: 0, Kind: "choice", Value: "Kjeldoran Warrior"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("same-name band answer = %#v, want %#v (name, never the leader's ref)", got, want)
	}
}

// TestBandAnswersSameNameAmbiguousKeepsAlias keeps the one case where a name
// is not enough: two same-name members are offered and the ref is an exact
// setup identity, so the existing same-name policy scripts the @ref alias.
func TestBandAnswersSameNameAmbiguousKeepsAlias(t *testing.T) {
	ambiguous := rules.OracleDecision{
		Step: 3, Seat: 0, Kind: "choose_n", Options: 3, GorgeKind: "choose", Via: "answer",
		Picks: []string{"Band Timber Wolves with Timber Wolves"}, PickIdx: []int{1},
		PickRefs: []string{"p0:Timber Wolves#2"}, PickRefsInexact: []bool{false},
		OptionRefs:  []string{"p0:Timber Wolves#2", "p0:Timber Wolves#3"},
		ObjectPicks: []string{"p0:Timber Wolves#2"},
		PickKinds:   []string{"band"}, Min: 1, Max: 1,
	}
	got := routed(t, []rules.OracleDecision{ambiguous}, 4)
	want := []XAnswer{
		{Seat: 0, Kind: "choice", Value: "yes"},
		{Seat: 0, Kind: "choice", Value: "@p0:Timber Wolves#2"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ambiguous same-name band answer = %#v, want %#v", got, want)
	}
}

// TestBandAnswersTransformedMemberUsesLiveName is the Kessig Prowler //
// Sinuous Predator shape from xpt-e4827a43: the member ref keeps the setup
// front face, but it transformed mid-scenario (CR 712.8) and XMage's band
// target offers only the live back face. The pick label carries the live name
// ("Band Sinuous Predator with Timber Wolves"), so the scripted choice must
// name that, or the target selects nothing ("Found wrong choice command ...
// Kessig Prowler ... selected 0, more possible 2").
func TestBandAnswersTransformedMemberUsesLiveName(t *testing.T) {
	transformed := rules.OracleDecision{
		Step: 63, Seat: 0, Kind: "choose_n", Options: 3, GorgeKind: "choose", Via: "answer",
		Picks: []string{"Band Sinuous Predator with Timber Wolves"}, PickIdx: []int{1},
		PickRefs: []string{"p0:Kessig Prowler"}, PickRefsInexact: []bool{false},
		OptionRefs:  []string{"p0:Kessig Prowler", "p0:Timber Wolves", "p0:Hollowhenge Overlord"},
		First:       "No more creatures in Timber Wolves's band",
		ObjectPicks: []string{"p0:Kessig Prowler"},
		PickKinds:   []string{"band"}, Min: 1, Max: 1,
	}
	got := routed(t, []rules.OracleDecision{transformed}, 64)
	want := []XAnswer{
		{Seat: 0, Kind: "choice", Value: "yes"},
		{Seat: 0, Kind: "choice", Value: "Sinuous Predator"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("transformed band answer = %#v, want %#v (live back-face name)", got, want)
	}
	// The label alone still names the member when First is absent (a
	// synthetic decision); the leader is the text after the last " with ".
	transformed.First = ""
	if got := routed(t, []rules.OracleDecision{transformed}, 64); !reflect.DeepEqual(got, want) {
		t.Fatalf("transformed band answer without First = %#v, want %#v", got, want)
	}
}
