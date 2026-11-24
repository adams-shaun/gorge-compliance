package oraclegen

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
)

// TestSearchZoneAsksResolveEffectShape pins the source-derived ask shape: the
// standard SearchLibraryGraveyard* card is library-first and asks the
// graveyard chooseUse only when the library search found nothing, while
// Arachnus Spinner's custom graveyard-first implementation stays unscripted.
func TestSearchZoneAsksResolveEffectShape(t *testing.T) {
	reg := cards.NewRegistry()
	reg.Add(&cards.Card{Faces: []*cards.Face{{
		Name:   "Delivery Moogle",
		Oracle: "Flying\nWhen this creature enters, search your library and/or graveyard for an artifact card with mana value 2 or less, reveal it, and put it into your hand.",
		Triggers: []cards.Trigger{{
			Mode: "ChangesZone",
			Effect: &cards.SA{Kind: "DB", API: "ChangeZone", Params: map[string]string{
				"Origin": "Library", "OriginAlternative": "Graveyard", "Destination": "Hand",
			}},
		}},
	}}})
	reg.Add(&cards.Card{Faces: []*cards.Face{{
		Name:   "Arachnus Spinner",
		Oracle: "Reach\nTap an untapped Spider you control: Search your graveyard and/or library for a card named Arachnus Web and put it onto the battlefield attached to target creature.",
		Triggers: []cards.Trigger{{
			Mode: "ChangesZone",
			Effect: &cards.SA{Kind: "DB", API: "ChangeZone", Params: map[string]string{
				"Origin": "Library", "OriginAlternative": "Graveyard", "Destination": "Battlefield",
			}},
		}},
	}}})
	zone := func(picks ...string) rules.OracleDecision {
		kinds := make([]string, len(picks))
		for i := range kinds {
			kinds[i] = "zone"
		}
		return rules.OracleDecision{Step: 1, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
			Resume: "search_zones", Options: 2, Min: 1, Max: 2,
			Picks: picks, PickRefs: append([]string(nil), picks...), PickKinds: kinds}
	}
	pick := rules.OracleDecision{Step: 1, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
		Resume: "search", Options: 1, Min: 0, Max: 1,
		Picks: []string{"Ornithopter"}, PickRefs: []string{"p0:Ornithopter"},
		PickKinds: []string{"search"}}
	snap := func(library int, graveyard ...string) rules.OracleSnapshot {
		return rules.OracleSnapshot{
			Players: []rules.OracleSnapPlayer{{Seat: 0, LibraryCount: library, Graveyard: graveyard}},
			Stack:   []rules.OracleSnapStack{{Kind: "ability", Source: "p0:Delivery Moogle", Controller: 0, Trigger: "0"}},
		}
	}
	sc := Scenario{Steps: []Step{{Op: "pass"}, {Op: "pass"}}}
	for _, c := range []struct {
		name          string
		card          string
		dec           []rules.OracleDecision
		before, after rules.OracleSnapshot
		want          [][]XAnswer
	}{
		{
			name: "library hit", card: "p0:Delivery Moogle",
			dec:    []rules.OracleDecision{zone("Search the library"), pick},
			before: snap(40), after: snap(39),
			want: [][]XAnswer{nil, {{Seat: 0, Kind: "choice", Value: "yes"}, {Seat: 0, Kind: "target", Value: "Ornithopter"}}},
		},
		{
			name: "library miss", card: "p0:Delivery Moogle",
			dec:    []rules.OracleDecision{zone("Search the library"), pick},
			before: snap(40, "p0:Ornithopter"), after: snap(40),
			want: [][]XAnswer{nil, {{Seat: 0, Kind: "choice", Value: "yes"}, {Seat: 0, Kind: "choice", Value: "no"}, {Seat: 0, Kind: "target", Value: "Ornithopter"}}},
		},
		{
			name: "both zones library hit", card: "p0:Delivery Moogle",
			dec:    []rules.OracleDecision{zone("Search the library", "Search the graveyard"), pick},
			before: snap(40), after: snap(39),
			want: [][]XAnswer{nil, {{Seat: 0, Kind: "choice", Value: "yes"}, {Seat: 0, Kind: "target", Value: "Ornithopter"}}},
		},
		{
			name: "graveyard-first stays unscripted", card: "p0:Arachnus Spinner",
			dec:    []rules.OracleDecision{zone("Search the library"), pick},
			before: snap(40), after: snap(39),
			want: [][]XAnswer{nil, {{Seat: 0, Kind: "target", Value: "Ornithopter"}}},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			before, after := c.before, c.after
			before.Stack[0].Source = c.card
			after.Stack[0].Source = c.card
			res := rules.OracleResult{
				Decisions: c.dec,
				Snapshots: []rules.OracleSnapshot{{}, before, after},
			}
			got := xanswersForScenario(reg, res, sc, nil, nil)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("answers = %#v, want %#v", got, c.want)
			}
		})
	}
}

// TestSearchZoneTextOrder pins the wording gate: only the and/or pair is a
// mapped shape; a forced-both wording has no chooseUse to answer.
func TestSearchZoneTextOrder(t *testing.T) {
	for _, c := range []struct {
		text  string
		first bool
		ok    bool
	}{
		{"Search your library and/or graveyard for a card.", true, true},
		{"Search your graveyard and/or library for a card.", false, true},
		{"Search your library and graveyard for a card.", false, false},
		{"Search your library for a card.", false, false},
	} {
		if first, ok := searchZoneTextOrder(c.text); first != c.first || ok != c.ok {
			t.Errorf("%q = (%v,%v), want (%v,%v)", c.text, first, ok, c.first, c.ok)
		}
	}
}

// TestSearchZoneAsksResolveCastResolveSource pins the cast-resolve shape: the
// enters trigger has already left the stack at the resolve step, so the
// source is the preceding cast step's card. Without that fallback the
// generator would leave the library chooseUse unscripted again.
func TestSearchZoneAsksResolveCastResolveSource(t *testing.T) {
	reg := cards.NewRegistry()
	reg.Add(&cards.Card{Faces: []*cards.Face{{
		Name:   "Delivery Moogle",
		Oracle: "When this creature enters, search your library and/or graveyard for an artifact card, reveal it, and put it into your hand.",
		Triggers: []cards.Trigger{{
			Mode: "ChangesZone",
			Effect: &cards.SA{Kind: "DB", API: "ChangeZone", Params: map[string]string{
				"Origin": "Library", "OriginAlternative": "Graveyard", "Destination": "Hand",
			}},
		}},
	}}})
	zone := rules.OracleDecision{Step: 1, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
		Resume: "search_zones", Options: 2, Min: 1, Max: 2,
		Picks: []string{"Search the library"}, PickRefs: []string{"Search the library"}, PickKinds: []string{"zone"}}
	pick := rules.OracleDecision{Step: 1, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
		Resume: "search", Options: 1, Min: 0, Max: 1,
		Picks: []string{"Ornithopter"}, PickRefs: []string{"p0:Ornithopter"}, PickKinds: []string{"search"}}
	res := rules.OracleResult{
		Decisions: []rules.OracleDecision{zone, pick},
		Snapshots: []rules.OracleSnapshot{
			{},
			{Players: []rules.OracleSnapPlayer{{Seat: 0, LibraryCount: 40}}},
			{Players: []rules.OracleSnapPlayer{{Seat: 0, LibraryCount: 39}}},
		},
	}
	sc := Scenario{Steps: []Step{{Op: "cast", Seat: 0, Card: "p0:Delivery Moogle"}, {Op: "resolve", Seat: 0}}}
	want := [][]XAnswer{nil, {{Seat: 0, Kind: "choice", Value: "yes"}, {Seat: 0, Kind: "target", Value: "Ornithopter"}}}
	if got := xanswersForScenario(reg, res, sc, nil, nil); !reflect.DeepEqual(got, want) {
		t.Fatalf("answers = %#v, want %#v", got, want)
	}
}

// TestSearchZoneAsksFoldOptionalBooleans pins Fang-Druid Summoner: the
// XMage SearchLibraryGraveyard* effect folds the optional trigger and the
// Optional$ True confirmation into its own library chooseUse, so only that
// boolean and the card pick are scripted.
func TestSearchZoneAsksFoldOptionalBooleans(t *testing.T) {
	reg := cards.NewRegistry()
	reg.Add(&cards.Card{Faces: []*cards.Face{{
		Name:   "Fang-Druid Summoner",
		Oracle: "When this creature enters, you may search your library and/or graveyard for a creature card, reveal it, and put it into your hand.",
		Triggers: []cards.Trigger{{
			Mode: "ChangesZone",
			Effect: &cards.SA{Kind: "DB", API: "ChangeZone", Params: map[string]string{
				"Origin": "Library", "OriginAlternative": "Graveyard", "Destination": "Hand", "Optional": "True",
			}},
		}},
	}}})
	optional := rules.OracleDecision{Step: 1, Seat: 0, Kind: "yesno", GorgeKind: "trigger_optional",
		Resume: "optional", Options: 2, Min: 1, Max: 1, Picks: []string{"Yes"}, PickRefs: []string{"Yes"}, PickKinds: []string{"yes"}}
	confirm := rules.OracleDecision{Step: 1, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
		Resume: "search_confirm", Options: 2, Min: 1, Max: 1, Picks: []string{"Yes"}, PickRefs: []string{"Yes"}, PickKinds: []string{"yes"}}
	zone := rules.OracleDecision{Step: 1, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
		Resume: "search_zones", Options: 2, Min: 1, Max: 2,
		Picks: []string{"Search the library"}, PickRefs: []string{"Search the library"}, PickKinds: []string{"zone"}}
	pick := rules.OracleDecision{Step: 1, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
		Resume: "search", Options: 1, Min: 0, Max: 1,
		Picks: []string{"Grizzly Bears"}, PickRefs: []string{"p0:Grizzly Bears"}, PickKinds: []string{"search"}}
	res := rules.OracleResult{
		Decisions: []rules.OracleDecision{optional, confirm, zone, pick},
		Snapshots: []rules.OracleSnapshot{
			{},
			{Players: []rules.OracleSnapPlayer{{Seat: 0, LibraryCount: 40}}},
			{Players: []rules.OracleSnapPlayer{{Seat: 0, LibraryCount: 39}}},
		},
	}
	sc := Scenario{Steps: []Step{{Op: "cast", Seat: 0, Card: "p0:Fang-Druid Summoner"}, {Op: "resolve", Seat: 0}}}
	want := [][]XAnswer{nil, {{Seat: 0, Kind: "choice", Value: "yes"}, {Seat: 0, Kind: "target", Value: "Grizzly Bears"}}}
	if got := xanswersForScenario(reg, res, sc, nil, nil); !reflect.DeepEqual(got, want) {
		t.Fatalf("answers = %#v, want %#v", got, want)
	}
}
