package oraclegen

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
)

// The xproof decision_shape shapes (xp-xmage-choice-defs), each pinned on a
// synthetic decision so the rule, not a card, is what is tested. The corpus
// windows they came from are oracle/xengine/xprep's TestXProofChoiceDefs.

// TestCastStepForcedXAnnounceIsScripted: XMage announces a spell's X over
// 0..any, so a forced X = 0 on a cast step is scripted; the same forced
// announce on an activate step keeps the old drop (announcesX's measured
// single-X activations).
func TestCastStepForcedXAnnounceIsScripted(t *testing.T) {
	x0 := func(step int) rules.OracleDecision {
		return rules.OracleDecision{Step: step, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Options: 1, Min: 1, Max: 1,
			Picks: []string{"X = 0"}, PickIdx: []int{0}, PickRefs: []string{"X = 0"}, PickKinds: []string{"x"}}
	}
	sc := Scenario{Steps: []Step{{Op: "activate", Card: "p0:Forest"}, {Op: "cast", Card: "p0:The Goose Mother"}}}
	got := xanswersForScenario(nil, rules.OracleResult{Decisions: []rules.OracleDecision{x0(1)}}, sc, nil, nil)
	want := [][]XAnswer{nil, {{Seat: 0, Kind: "choice", Value: "X=0"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cast X = %#v, want %#v", got, want)
	}
	if got := xanswersForScenario(nil, rules.OracleResult{Decisions: []rules.OracleDecision{x0(0)}}, sc, nil, nil); got != nil {
		t.Fatalf("activate-step forced X = %#v, want unscripted", got)
	}
	// An X that also sizes a non-mana cost part (discard X) is bounded by
	// XMage, which then announces the fixed X unasked.
	reg := cards.NewRegistry()
	reg.Add(&cards.Card{Faces: []*cards.Face{{Name: "Abandon Hope", ManaCost: "X 1 B",
		Abilities: []*cards.SA{{Kind: "SP", API: "Discard", Params: map[string]string{"Cost": "X 1 B Discard<X/Card/card>"}}}}}})
	bounded := Scenario{Steps: []Step{{Op: "cast", Card: "p0:Abandon Hope"}}}
	if got := xanswersForScenario(reg, rules.OracleResult{Decisions: []rules.OracleDecision{x0(0)}}, bounded, nil, nil); got != nil {
		t.Fatalf("discard-X forced X = %#v, want unscripted", got)
	}
}

// TestDigBottomOrderScriptsAllButLast: XMage's putCardsOnBottomOfLibrary asks
// while more than one card is left and places the last unasked.
func TestDigBottomOrderScriptsAllButLast(t *testing.T) {
	d := rules.OracleDecision{Step: 0, Seat: 0, Kind: "order", GorgeKind: "arrange", Resume: "dig_arrange", ArrangeKind: "dig_bottom",
		Options: 3, Min: 3, Max: 3, Picks: []string{"Forest", "Island", "Swamp"}, PickIdx: []int{0, 1, 2},
		ArrangeLabels: []string{"Forest", "Island", "Swamp"}, PickKinds: []string{"dig_bottom", "dig_bottom", "dig_bottom"}}
	want := [][]XAnswer{{{Seat: 0, Kind: "choice", Value: "Forest"}, {Seat: 0, Kind: "choice", Value: "Island"}}}
	if got := xanswers([]rules.OracleDecision{d}, 1, nil, nil); !reflect.DeepEqual(got, want) {
		t.Fatalf("dig bottom = %#v, want %#v", got, want)
	}
}

// TestEmptyPoolConfirmScriptsNothing: gorge's Optional$ confirmation over an
// empty pool has no pick after it and XMage asks nothing; with a pick after
// it the confirmation is still XMage's chooseUse. A hand-move confirmation
// counts only over a hand the step targeted another player for: over the
// asker's own hand XMage's chooseUse is posed regardless (PuPu UFO).
func TestEmptyPoolConfirmScriptsNothing(t *testing.T) {
	confirm := func(resume string) rules.OracleDecision {
		return rules.OracleDecision{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Resume: resume, Options: 2, Min: 1, Max: 1,
			Picks: []string{"Yes"}, PickIdx: []int{0}, PickRefs: []string{"Yes"}, PickKinds: []string{"yes"}}
	}
	opponent := rules.OracleDecision{Step: 0, Seat: 0, Kind: "target", GorgeKind: "target", Options: 1, Min: 1, Max: 1,
		Picks: []string{"b"}, PickIdx: []int{0}, PickRefs: []string{"p1"}, PickKinds: []string{"player"}}
	pick := rules.OracleDecision{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Resume: "hand_move", Options: 2, Min: 1, Max: 1,
		Picks: []string{"Grizzly Bears"}, PickIdx: []int{0}, PickRefs: []string{"p1:Grizzly Bears"}, PickKinds: []string{"card"}}
	yes := XAnswer{Seat: 0, Kind: "choice", Value: "yes"}
	target := XAnswer{Seat: 0, Kind: "target", Value: "p1"}
	for name, tc := range map[string]struct {
		ds   []rules.OracleDecision
		want [][]XAnswer
	}{
		"hidden pick, empty pool":         {[]rules.OracleDecision{confirm("hidden_pick_confirm")}, nil},
		"opponent's hand, empty":          {[]rules.OracleDecision{opponent, confirm("hand_move_confirm")}, [][]XAnswer{{target}}},
		"own hand, empty: still asked":    {[]rules.OracleDecision{confirm("hand_move_confirm")}, [][]XAnswer{{yes}}},
		"hidden pick, a pick follows":     {[]rules.OracleDecision{confirm("hidden_pick_confirm"), pick}, nil},
		"opponent's hand, a pick follows": {[]rules.OracleDecision{opponent, confirm("hand_move_confirm"), pick}, nil},
	} {
		got := xanswers(tc.ds, 1, nil, nil)
		if tc.want != nil && !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%s = %#v, want %#v", name, got, tc.want)
		}
		if tc.want == nil && len(tc.ds) == 1 && got != nil {
			t.Fatalf("%s = %#v, want nothing", name, got)
		}
		if len(tc.ds) > 1 && tc.ds[len(tc.ds)-1].Resume == "hand_move" {
			// The pick follows: the confirmation keeps its "yes" ahead of it.
			found := false
			for _, a := range got[0] {
				found = found || a == yes
			}
			if !found {
				t.Fatalf("%s = %#v, want the confirmation's yes", name, got)
			}
		}
	}
}

// TestCollectEvidenceDeclineIsNotPosed: XMage's CollectEvidenceCost has a real
// canPay, so a decline-only collect evidence trigger poses no chooseUse; a
// mana trigger cost's decline is still answered no.
func TestCollectEvidenceDeclineIsNotPosed(t *testing.T) {
	reg := cards.NewRegistry()
	for name, cost := range map[string]string{"Surveillance Monitor": "CollectEvidence<4>", "Pay Two Probe": "2"} {
		reg.Add(&cards.Card{Faces: []*cards.Face{{Name: name, Triggers: []cards.Trigger{{
			Mode:   "ChangesZone",
			Effect: &cards.SA{Kind: "AB", API: "Nothing", Params: map[string]string{"Cost": cost}},
		}}}}})
	}
	decline := func(ref string) rules.OracleDecision {
		return rules.OracleDecision{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Options: 1, Min: 1, Max: 1,
			Picks: []string{"Do not pay"}, PickIdx: []int{0}, PickRefs: []string{ref}, ObjectPicks: []string{ref},
			PickKinds: []string{"trigger_cost_decline"}, CostPosed: true}
	}
	sc := Scenario{Steps: []Step{{Op: "pass"}}}
	if got := xanswersForScenario(reg, rules.OracleResult{Decisions: []rules.OracleDecision{decline("p0:Surveillance Monitor")}}, sc, nil, nil); got != nil {
		t.Fatalf("collect evidence decline = %#v, want nothing", got)
	}
	want := [][]XAnswer{{{Seat: 0, Kind: "choice", Value: "no"}}}
	if got := xanswersForScenario(reg, rules.OracleResult{Decisions: []rules.OracleDecision{decline("p0:Pay Two Probe")}}, sc, nil, nil); !reflect.DeepEqual(got, want) {
		t.Fatalf("mana cost decline = %#v, want %#v", got, want)
	}
}

// TestDigGraveyardTakeUsesTargetQueue: a fixed look of three or more whose
// rest goes to the graveyard is LookLibraryAndPickControllerEffect's
// chooseTarget; several picks are one "^"-joined target.
func TestDigGraveyardTakeUsesTargetQueue(t *testing.T) {
	reg := cards.NewRegistry()
	reg.Add(&cards.Card{Faces: []*cards.Face{{Name: "Polygraph Orb", Triggers: []cards.Trigger{{
		Mode: "ChangesZone",
		Effect: &cards.SA{Kind: "DB", API: "Dig", Params: map[string]string{
			"DigNum": "4", "ChangeNum": "2", "DestinationZone2": "Graveyard"}},
	}}}}})
	d := rules.OracleDecision{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Resume: "dig", Options: 4, Min: 2, Max: 2,
		Picks: []string{"Swamp", "Island"}, PickIdx: []int{0, 2}, PickRefs: []string{"p0:Swamp#3", "p0:Island#2"},
		PickKinds: []string{"dig", "dig"}, OptionRefs: []string{"p0:Swamp#3", "p0:Swamp#4", "p0:Island#2", "p0:Swamp#5"}}
	sc := Scenario{Steps: []Step{{Op: "cast", Card: "p0:Polygraph Orb"}}}
	want := [][]XAnswer{{{Seat: 0, Kind: "target", Value: "Swamp^Island"}}}
	if got := xanswersForScenario(reg, rules.OracleResult{Decisions: []rules.OracleDecision{d}}, sc, nil, nil); !reflect.DeepEqual(got, want) {
		t.Fatalf("dig graveyard take = %#v, want %#v", got, want)
	}
}

// TestSharedStepTriggerOrderDemotesToRuleKeys: a distinct-source trigger
// order on a step that also scripts a target demotes to XMage's rule-text
// keys, not the verbatim TriggerDescription$ text the order ask cannot match.
func TestSharedStepTriggerOrderDemotesToRuleKeys(t *testing.T) {
	order := rules.OracleDecision{Step: 0, Seat: 0, Kind: "order", GorgeKind: "trigger_order", Options: 2, Min: 2, Max: 2,
		Picks: []string{
			"Clement, the Worrywort: Whenever CARDNAME or another creature you control enters, return up to one target creature you control with lesser mana value to its owner's hand.",
			"Valley Mightcaller: Whenever another Frog, Rabbit, Raccoon, or Squirrel you control enters, put a +1/+1 counter on CARDNAME.",
		},
		PickIdx: []int{1, 0}, PickRefs: []string{"p0:Clement, the Worrywort", "p0:Valley Mightcaller"},
		PickKinds: []string{"trigger", "trigger"}}
	target := rules.OracleDecision{Step: 0, Seat: 0, Kind: "target", GorgeKind: "target", Options: 1, Min: 0, Max: 1,
		Picks: []string{"Valley Mightcaller (a)"}, PickIdx: []int{0}, PickRefs: []string{"p0:Valley Mightcaller"}, PickKinds: []string{"permanent"}}
	got := xanswers([]rules.OracleDecision{order, target}, 1, nil, nil)
	want := [][]XAnswer{{
		{Seat: 0, Kind: "choice", Value: "Whenever {this} or another creature you control enters, return up to one target creature you control with lesser mana value to its owner's hand."},
		{Seat: 0, Kind: "target", Value: "Valley Mightcaller"},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("shared-step trigger order = %#v, want %#v", got, want)
	}
}

// TestForageExileArmNamesTheExiledCards: gorge's forage exiles three graveyard
// cards without a pick, so the accepted election is followed by the three
// names the checkpoint moved to exile; any other count is left unscripted.
func TestForageExileArmNamesTheExiledCards(t *testing.T) {
	d := rules.OracleDecision{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Options: 2, Min: 1, Max: 1,
		Picks: []string{"Forage"}, PickIdx: []int{0}, PickRefs: []string{"p0:Bushy Bodyguard"}, PickKinds: []string{"trigger_cost_pay"}}
	snaps := func(gy, ex []string) []rules.OracleSnapshot {
		return []rules.OracleSnapshot{
			{Players: []rules.OracleSnapPlayer{{Seat: 0, Graveyard: []string{"Forest", "Island", "Forest", "Swamp"}}}},
			{Players: []rules.OracleSnapPlayer{{Seat: 0, Graveyard: gy, Exile: ex}}},
		}
	}
	sc := Scenario{Steps: []Step{{Op: "pass"}}}
	res := rules.OracleResult{Decisions: []rules.OracleDecision{d}, Snapshots: snaps([]string{"Forest"}, []string{"Island", "Forest", "Swamp"})}
	want := [][]XAnswer{{{Seat: 0, Kind: "choice", Value: "yes"}, {Seat: 0, Kind: "choice", Value: "Island^Forest^Swamp"}}}
	if got := xanswersForScenario(nil, res, sc, nil, nil); !reflect.DeepEqual(got, want) {
		t.Fatalf("forage exile arm = %#v, want %#v", got, want)
	}
	res.Snapshots = snaps([]string{"Forest", "Island", "Forest", "Swamp"}, nil)
	want = [][]XAnswer{{{Seat: 0, Kind: "choice", Value: "yes"}}}
	if got := xanswersForScenario(nil, res, sc, nil, nil); !reflect.DeepEqual(got, want) {
		t.Fatalf("forage with no exile = %#v, want %#v", got, want)
	}
}
