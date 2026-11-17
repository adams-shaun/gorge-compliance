package main

import (
	"testing"

	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestCheckPlayableAbilityEmitsOffered: XMage's checkPlayableAbility asserts
// whether "Cast X" (or an ability text) is on offer at a turn/step for a
// player. The runner has the inline offered expect for exactly that, so a
// method using it must translate instead of refusing playable-check.
func TestCheckPlayableAbilityEmitsOffered(t *testing.T) {
	src := `class CheckPlayableTest extends CardTestPlayerBase {
    @Test
    public void testOffered() {
        addCard(Zone.BATTLEFIELD, playerA, "Mountain", 2);
        addCard(Zone.HAND, playerA, "Lightning Bolt", 1);
        addCard(Zone.HAND, playerA, "Karn, Silver Golem", 1);
        checkPlayableAbility("before", 1, PhaseStep.PRECOMBAT_MAIN, playerA, "Cast Lightning Bolt", true);
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Lightning Bolt", playerB);
        checkPlayableAbility("second is locked", 1, PhaseStep.POSTCOMBAT_MAIN, playerA, "Cast Karn, Silver Golem", false);
        setStopAt(1, PhaseStep.END_TURN);
        execute();
    }
}`
	jc, err := parseJava("CheckPlayableTest.java", "CheckPlayableTest.java", src)
	if err != nil {
		t.Fatal(err)
	}
	if len(jc.Methods) != 1 {
		t.Fatalf("parsed %d methods, want 1", len(jc.Methods))
	}
	res := translate(testutil.CorpusRegistry(t), jc, &jc.Methods[0], "rules")
	if res.Status == "refused" {
		t.Fatalf("method refused %s: %s", res.Code, res.Detail)
	}
	if res.Scenario == nil {
		t.Fatalf("no scenario: %s", res.Status)
	}
	var got []*offered
	for _, st := range res.Scenario.Steps {
		for _, x := range st.Expect {
			if x.Offered != nil {
				got = append(got, x.Offered)
			}
		}
	}
	if len(got) != 2 {
		t.Fatalf("got %d inline offered expects, want 2: %+v", len(got), got)
	}
	first, second := got[0], got[1]
	if first.Seat != 0 || first.Kind != "cast" || first.Label != "Lightning Bolt" || first.Card != "p0:Lightning Bolt" {
		t.Errorf("first offered = %+v, want p0 cast Lightning Bolt (p0:Lightning Bolt)", first)
	}
	if second.Seat != 0 || second.Kind != "cast" || second.Label != "Karn, Silver Golem" || second.Card != "p0:Karn, Silver Golem" {
		t.Errorf("second offered = %+v, want p0 cast Karn, Silver Golem (p0:Karn, Silver Golem)", second)
	}
	// wants ride the owning expect, not the offered struct: collect them.
	var wants []bool
	for _, st := range res.Scenario.Steps {
		for _, x := range st.Expect {
			if x.Offered != nil {
				wants = append(wants, x.Want != nil && *x.Want)
			}
		}
	}
	if len(wants) != 2 || !wants[0] || wants[1] {
		t.Errorf("offered wants = %v, want [true false]", wants)
	}
}

// TestPlayableOfferResolvesSiblingSpellings covers the rest of the class the
// first test's full "Cast <name>" spelling misses: XMage abbreviations
// ("Silvercoat" for Silvercoat Lion), alternative-cast qualifiers ("using
// Morph"), back-face names of a card whose setup name is the front face
// ("Craft with Pride" on Goblin Glasswright), and bare card-name ability
// checks. A fix that only matched exact full names would pass the first test
// and miss every one of these.
func TestPlayableOfferResolvesSiblingSpellings(t *testing.T) {
	src := `class PlayableSpellingsTest extends CardTestPlayerBase {
    @Test
    public void testSpellings() {
        addCard(Zone.BATTLEFIELD, playerA, "Island", 2);
        addCard(Zone.HAND, playerA, "Silvercoat Lion", 1);
        addCard(Zone.HAND, playerA, "Willbender", 1);
        addCard(Zone.HAND, playerA, "Goblin Glasswright", 1);
        addCard(Zone.BATTLEFIELD, playerA, "Serra Avenger", 1);
        checkPlayableAbility("short", 1, PhaseStep.PRECOMBAT_MAIN, playerA, "Cast Silvercoat", true);
        checkPlayableAbility("morph", 1, PhaseStep.PRECOMBAT_MAIN, playerA, "Cast Willbender using Morph", false);
        checkPlayableAbility("back face", 1, PhaseStep.PRECOMBAT_MAIN, playerA, "Cast Craft with Pride", false);
        checkPlayableAbility("bare card", 1, PhaseStep.PRECOMBAT_MAIN, playerA, "Serra Avenger", false);
        setStopAt(1, PhaseStep.END_TURN);
        execute();
    }
}`
	jc, err := parseJava("PlayableSpellingsTest.java", "PlayableSpellingsTest.java", src)
	if err != nil {
		t.Fatal(err)
	}
	res := translate(testutil.CorpusRegistry(t), jc, &jc.Methods[0], "rules")
	if res.Status == "refused" {
		t.Fatalf("method refused %s: %s", res.Code, res.Detail)
	}
	var got []*offered
	for _, st := range res.Scenario.Steps {
		for _, x := range st.Expect {
			if x.Offered != nil {
				got = append(got, x.Offered)
			}
		}
	}
	want := []offered{
		{Seat: 0, Kind: "cast", Card: "p0:Silvercoat Lion", Label: "Silvercoat Lion"},
		{Seat: 0, Kind: "cast", Card: "p0:Willbender", Label: "Willbender (face down)"},
		{Seat: 0, Kind: "cast", Card: "p0:Goblin Glasswright", Label: "Craft with Pride"},
		{Seat: 0, Kind: "activate", Card: "p0:Serra Avenger", Label: "Serra Avenger"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d offered expects, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if *got[i] != want[i] {
			t.Errorf("offered[%d] = %+v, want %+v", i, *got[i], want[i])
		}
	}
}
