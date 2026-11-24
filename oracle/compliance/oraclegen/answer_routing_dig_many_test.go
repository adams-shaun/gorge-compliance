package oraclegen

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
)

// symbioteDigDecision is Symbiote Spider-Man's "look at that many cards ...
// put one of them into your hand and the rest into your graveyard" take after
// a 3-damage hit: three distinct offered cards, gorge picking the second.
func symbioteDigDecision(step int) rules.OracleDecision {
	return rules.OracleDecision{Step: step, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Resume: "dig",
		Options: 3, Min: 1, Max: 1, Picks: []string{"Wraith, Vicious Vigilante"}, PickIdx: []int{1},
		PickRefs: []string{"p0:Wraith, Vicious Vigilante"}, PickKinds: []string{"dig"},
		OptionRefs: []string{"p0:Prowler, Clawed Thief", "p0:Wraith, Vicious Vigilante", "p0:Venom, Evil Unleashed"}}
}

// TestDigManyCardLookScriptsPickOnTargetQueue pins the SPM p0.hand farm rows
// (Symbiote Spider-Man, class GF|X): XMage's look-and-pick dialog
// (LookLibraryAndPickControllerEffect.actionWithLookedCards) is a chooseTarget
// over a TargetCard, the TARGET queue, however many cards the look offers. The
// router scripted it only for a two-card look (digExileComplement's
// Options == 2 gate), so a "that many" look of three or more cards fell to the
// generic choice-queue answer, which that dialog never reads: the pick was left
// unused and XMage's AI took the card it liked (measured on the
// ScenarioReplay driver: gorge Wraith vs XMage Prowler, leftover "Choices of
// PlayerA count not equal"). The target answer must be gorge's own pick, for
// the graveyard-second family, whatever the look's size.
func TestDigManyCardLookScriptsPickOnTargetQueue(t *testing.T) {
	reg := cards.NewRegistry()
	reg.Add(&cards.Card{Faces: []*cards.Face{{
		Name: "Symbiote Spider-Man",
		Triggers: []cards.Trigger{{
			Mode: "DamageDone",
			Effect: &cards.SA{Kind: "DB", API: "Dig", Params: map[string]string{
				"DigNum": "X", "ChangeNum": "1",
				"DestinationZone": "Hand", "DestinationZone2": "Graveyard",
			}},
		}},
	}}})
	res := rules.OracleResult{
		Snapshots: []rules.OracleSnapshot{
			{},
			{Stack: []rules.OracleSnapStack{{Kind: "ability", Source: "p0:Symbiote Spider-Man", Controller: 0, Trigger: "0"}}},
			{},
		},
		Decisions: []rules.OracleDecision{symbioteDigDecision(1)},
	}
	sc := Scenario{Steps: []Step{{Op: "pass"}, {Op: "pass"}}}
	want := [][]XAnswer{nil, {{Seat: 0, Kind: "target", Value: "Wraith, Vicious Vigilante"}}}
	if got := xanswersForScenario(reg, res, sc, nil, nil); !reflect.DeepEqual(got, want) {
		t.Fatalf("routed = %#v, want %#v (a three-card look is the same TARGET-queue dialog as a two-card one)", got, want)
	}
}

// TestDigManyCardLookWithoutSourceStaysOnChoiceQueue is the unthreaded caller
// (no registry, so no resolved source): the IR cannot prove the dialog is the
// look-and-pick chooseTarget, so a look of more than two cards keeps the
// choice-queue answer it had before the many-card fix.
func TestDigManyCardLookWithoutSourceStaysOnChoiceQueue(t *testing.T) {
	got := routed(t, []rules.OracleDecision{symbioteDigDecision(1)}, 2)
	for _, a := range got {
		if a.Kind == "target" {
			t.Fatalf("unresolved-source many-card look scripted a target answer %#v", a)
		}
	}
}

// memoriesReturningRouted routes one Dig decision of Memories Returning's
// chained spell (DigNum 5 hand/Library, then the opponent's and the caster's
// narrowing digs) through xanswersForScenario with the card's real IR shape:
// the Dig abilities disagree on DestinationZone2 (Library, none, Library, Hand).
func memoriesReturningRouted(t *testing.T, d rules.OracleDecision) []XAnswer {
	t.Helper()
	dig := func(second string, sub *cards.SA) *cards.SA {
		params := map[string]string{"ChangeNum": "1", "DestinationZone": "Hand"}
		if second != "" {
			params["DestinationZone2"] = second
		}
		return &cards.SA{Kind: "DB", API: "Dig", Params: params, Sub: sub}
	}
	chain := dig("Library", dig("", dig("Library", dig("Hand", nil))))
	chain.Kind = "SP"
	reg := cards.NewRegistry()
	reg.Add(&cards.Card{Faces: []*cards.Face{{Name: "Memories Returning", Abilities: []*cards.SA{chain}}}})
	res := rules.OracleResult{
		Snapshots: []rules.OracleSnapshot{
			{},
			{Stack: []rules.OracleSnapStack{{Kind: "spell", Source: "p0:Memories Returning", Controller: 0}}},
			{},
		},
		Decisions: []rules.OracleDecision{d},
	}
	sc := Scenario{Steps: []Step{{Op: "pass"}, {Op: "pass"}}}
	out := xanswersForScenario(reg, res, sc, nil, nil)
	if len(out) < 2 {
		t.Fatalf("routed %d steps, want at least 2: %#v", len(out), out)
	}
	return out[1]
}

// TestDigMemoriesReturningKeepsChoiceQueueAnswer pins Memories Returning's
// answer routing to its pre-fddc5e6c21 output. The spell chains four Digs whose
// second destinations disagree, so its picks are XMage's own multi-step effect
// reading the CHOICE queue; the broad many-card rule scripted them on the TARGET
// queue and the cast-resolve scenario turned from agree to a HARNESS failure
// ("wrong choice command"). A five-card and a three-card look both stay on the
// choice queue, by name.
func TestDigMemoriesReturningKeepsChoiceQueueAnswer(t *testing.T) {
	five := rules.OracleDecision{Step: 1, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Resume: "dig",
		Options: 5, Min: 1, Max: 1, Picks: []string{"Island"}, PickIdx: []int{0},
		PickRefs: []string{"p0:Island"}, PickKinds: []string{"dig"},
		OptionRefs: []string{"p0:Island", "p0:Swamp", "p0:Forest", "p0:Mountain", "p0:Plains"}}
	three := rules.OracleDecision{Step: 1, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Resume: "dig",
		Options: 3, Min: 1, Max: 1, Picks: []string{"Forest"}, PickIdx: []int{0},
		PickRefs: []string{"p0:Forest"}, PickKinds: []string{"dig"},
		OptionRefs: []string{"p0:Forest", "p0:Mountain", "p0:Plains"}}
	for _, d := range []rules.OracleDecision{five, three} {
		for _, a := range memoriesReturningRouted(t, d) {
			if a.Kind == "target" {
				t.Fatalf("%d-card Memories Returning look scripted a target answer %#v; XMage reads the choice queue", d.Options, a)
			}
		}
	}
}

// TestDigManyCardExileFamilyIsNotRescripted guards the exile-second family: its
// XMage dialog selects the card to EXILE, so gorge's pick is the wrong card to
// script, and the complement of a look of three or more is not one card. That
// shape stays with the generic path, as before.
func TestDigManyCardExileFamilyIsNotRescripted(t *testing.T) {
	reg := cards.NewRegistry()
	reg.Add(&cards.Card{Faces: []*cards.Face{{
		Name: "Ashiok, Wicked Manipulator",
		Triggers: []cards.Trigger{{
			Mode: "ChangesZone",
			Effect: &cards.SA{Kind: "DB", API: "Dig", Params: map[string]string{
				"DigNum": "3", "ChangeNum": "1", "DestinationZone2": "Exile",
			}},
		}},
	}}})
	res := rules.OracleResult{
		Snapshots: []rules.OracleSnapshot{
			{},
			{Stack: []rules.OracleSnapStack{{Kind: "ability", Source: "p0:Ashiok, Wicked Manipulator", Controller: 0, Trigger: "0"}}},
			{},
		},
		Decisions: []rules.OracleDecision{symbioteDigDecision(1)},
	}
	sc := Scenario{Steps: []Step{{Op: "pass"}, {Op: "pass"}}}
	for _, as := range xanswersForScenario(reg, res, sc, nil, nil) {
		for _, a := range as {
			if a.Kind == "target" {
				t.Fatalf("exile-second family scripted a target answer %#v for a three-card look", a)
			}
		}
	}
}
