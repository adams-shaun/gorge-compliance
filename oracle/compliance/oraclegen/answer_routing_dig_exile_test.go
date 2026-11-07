package oraclegen

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
)

// TestDigExileFamilyScriptsComplement pins the Ashiok, Wicked Manipulator
// shape: a one-card Dig take over two named cards whose second destination is
// exile. XMage's Dig dialog for that family (a chooseTarget, the target
// queue) selects the card to EXILE, the opposite of gorge's pick (the card to
// hand), so the scripted answer must be the OTHER offered card.
func TestDigExileFamilyScriptsComplement(t *testing.T) {
	d := rules.OracleDecision{Step: 2, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Resume: "dig",
		Options: 2, Min: 1, Max: 1, Picks: []string{"Jace Beleren"}, PickIdx: []int{0},
		PickRefs: []string{"p0:Jace Beleren"}, PickKinds: []string{"dig"},
		OptionRefs: []string{"p0:Jace Beleren", "p0:Grizzly Bears"}}
	// Precondition: the two offered names differ and one is gorge's pick, so
	// the complement is computable and non-empty -- a same-named pair would
	// make this test vacuous.
	if len(d.OptionRefs) != 2 || oraclediffRefName(d.OptionRefs[0]) == oraclediffRefName(d.OptionRefs[1]) {
		t.Fatalf("precondition: options must be two distinct named cards, got %v", d.OptionRefs)
	}
	if got := digExileComplement(d); got != "Grizzly Bears" {
		t.Fatalf("digExileComplement = %q, want %q", got, "Grizzly Bears")
	}
	got := routed(t, []rules.OracleDecision{d}, 3)
	want := []XAnswer{{0, "target", "Grizzly Bears"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("routed = %#v, want %#v (the exile family scripts the complement on the target queue)", got, want)
	}
}

// TestDigFiveCardLookKeepsPick pins the Hit It! shape: a one-card take over
// two named cards whose second destination is the GRAVEYARD, from a five-card
// look. XMage's look-and-pick dialog for this family selects the card going to
// the FIRST destination (the battlefield here), the same card gorge picked, so
// the scripted target must be gorge's own pick -- never the exile family's
// complement. The source is resolved from the trigger on the stack, as
// xanswersForScenario does from gorge's snapshots.
func TestDigFiveCardLookKeepsPick(t *testing.T) {
	reg := cards.NewRegistry()
	reg.Add(&cards.Card{Faces: []*cards.Face{{
		Name: "Hit It!",
		Triggers: []cards.Trigger{{
			Mode: "ChangesZone",
			Effect: &cards.SA{Kind: "DB", API: "Dig", Params: map[string]string{
				"DigNum": "5", "ChangeNum": "1",
				"DestinationZone": "Battlefield", "DestinationZone2": "Graveyard",
			}},
		}},
	}}})
	d := rules.OracleDecision{Step: 1, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Resume: "dig",
		Options: 2, Min: 1, Max: 1, Picks: []string{"Mountain"}, PickIdx: []int{0},
		PickRefs: []string{"p0:Mountain#3"}, PickKinds: []string{"dig"},
		OptionRefs: []string{"p0:Mountain#3", "p0:Forest#5"}}
	res := rules.OracleResult{
		Snapshots: []rules.OracleSnapshot{
			{},
			{Stack: []rules.OracleSnapStack{{Kind: "ability", Source: "p0:Hit It!", Controller: 0, Trigger: "0"}}},
			{},
		},
		Decisions: []rules.OracleDecision{d},
	}
	sc := Scenario{Steps: []Step{{Op: "pass"}, {Op: "pass"}}}
	want := [][]XAnswer{nil, {{Seat: 0, Kind: "target", Value: "Mountain"}}}
	if got := xanswersForScenario(reg, res, sc, nil, nil); !reflect.DeepEqual(got, want) {
		t.Fatalf("routed = %#v, want %#v (the graveyard-second family keeps gorge's pick)", got, want)
	}
}

// TestDigBottomLabelStaysUninverted pins the unchanged bottom-label form (Jace,
// the Mind Sculptor): its pick IS the card XMage selects, so it must script the
// label's name and never the complement.
func TestDigBottomLabelStaysUninverted(t *testing.T) {
	d := rules.OracleDecision{Step: 2, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Resume: "dig",
		Options: 2, Min: 1, Max: 1, Picks: []string{"Put Wastes on bottom"}, PickIdx: []int{0},
		PickRefs: []string{"p0:Wastes"}, PickKinds: []string{"dig"},
		OptionRefs: []string{"p0:Wastes", "p0:Island"}}
	// Precondition: this is the bottom-label form (digBottomName non-empty),
	// the shape that measures AGREE and must be excluded from the complement.
	if digBottomName(d.Picks[0]) == "" {
		t.Fatalf("precondition: %q must be a bottom label", d.Picks[0])
	}
	if got := digExileComplement(d); got != "" {
		t.Fatalf("digExileComplement = %q, want empty for the bottom-label form", got)
	}
	got := routed(t, []rules.OracleDecision{d}, 3)
	want := []XAnswer{{0, "choice", "Wastes"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("routed = %#v, want %#v (the bottom label scripts its own name)", got, want)
	}
}

// TestDigMultiTakeStaysUninverted pins a non-family dig: a two-card take from
// a four-card window keeps gorge's own picks, so the complement never fires on
// an option count other than two.
func TestDigMultiTakeStaysUninverted(t *testing.T) {
	d := rules.OracleDecision{Step: 1, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Resume: "dig",
		Options: 4, Min: 2, Max: 2, Picks: []string{"Wastes", "Wastes"}, PickIdx: []int{0, 1},
		PickRefs: []string{"p0:Wastes#27", "p0:Wastes#39"}, PickKinds: []string{"dig", "dig"},
		OptionRefs: []string{"p0:Wastes#27", "p0:Wastes#39", "p0:Wastes#4", "p0:Wastes#2"}}
	// Precondition: this is NOT the two-option one-card take the family
	// requires, so the complement must decline.
	if d.Options == 2 && d.Min == 1 && d.Max == 1 {
		t.Fatalf("precondition: %+v must not match the family shape", d)
	}
	if got := digExileComplement(d); got != "" {
		t.Fatalf("digExileComplement = %q, want empty for a multi-take dig", got)
	}
}
