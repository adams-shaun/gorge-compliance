package main

import (
	"strings"
	"testing"
)

// castStepOf returns the first cast step of a translated scenario.
func castStepOf(t *testing.T, res *result) *step {
	t.Helper()
	if res.Scenario == nil {
		t.Fatalf("refused %s: %s", res.Code, res.Detail)
	}
	for _, st := range res.Scenario.Steps {
		if st.Op == "cast" {
			return st
		}
	}
	t.Fatal("no cast step")
	return nil
}

// groupPicks lists every answer pick the translator placed, in order.
func groupPicks(res *result) []string {
	var out []string
	for _, g := range res.Groups {
		for _, a := range g.answers {
			out = append(out, a.Pick...)
		}
	}
	return out
}

// XMage answers a counted keyword cost (Multikicker, Replicate, Squad) with one
// boolean per payment and a closing false; gorge offers the payment as its own
// cast option ("multikicked", "replicated", "squadded") and asks the count in
// the cast flow. The booleans were never consumed, so the spell was cast
// plain and the interaction went unplayed (KickerTest, ReplicateTest,
// SquadTest).
func TestCountedKeywordCostBooleansAreACastModeAndACount(t *testing.T) {
	for _, tc := range []struct {
		name, card, mode, label, mana, body string
	}{
		{"multikicker", "Apex Hawks", "multikicked", "Pay multikicker 2 times", "WWWWWWW",
			`addCard(Zone.BATTLEFIELD, playerA, "Plains", 7);
        addCard(Zone.HAND, playerA, "Apex Hawks");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Apex Hawks");
        setChoice(playerA, true);
        setChoice(playerA, true);
        setChoice(playerA, false);
        setStrictChooseMode(true);
        setStopAt(1, PhaseStep.BEGIN_COMBAT);
        execute();
        assertCounterCount("Apex Hawks", CounterType.P1P1, 2);
        assertPowerToughness(playerA, "Apex Hawks", 4, 4);`},
		{"replicate", "Train of Thought", "replicated", "Pay replicate 2 times", "UUUUUU",
			`addCard(Zone.BATTLEFIELD, playerA, "Island", 6);
        addCard(Zone.HAND, playerA, "Train of Thought");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Train of Thought");
        setChoice(playerA, true);
        setChoice(playerA, true);
        setChoice(playerA, false);
        setStopAt(1, PhaseStep.BEGIN_COMBAT);
        execute();
        assertHandCount(playerA, 3);`},
		{"squad", "Arco-Flagellant", "squadded", "Pay squad once", "BBBBB",
			`addCard(Zone.BATTLEFIELD, playerA, "Swamp", 7);
        addCard(Zone.HAND, playerA, "Arco-Flagellant");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Arco-Flagellant");
        setChoice(playerA, true);
        setChoice(playerA, false);
        setStrictChooseMode(true);
        setStopAt(1, PhaseStep.BEGIN_COMBAT);
        execute();
        assertPermanentCount(playerA, "Arco-Flagellant", 2);`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := translateBody(t, tc.body)
			cast := castStepOf(t, res)
			if cast.CastMode != tc.mode {
				t.Errorf("cast_mode %q, want %q", cast.CastMode, tc.mode)
			}
			if len(cast.Mana) != len(tc.mana) {
				t.Errorf("mana %q, want %d mana (base cost plus each payment)", cast.Mana, len(tc.mana))
			}
			if picks := groupPicks(res); len(picks) != 1 || picks[0] != tc.label {
				t.Errorf("answers %v, want only %q", picks, tc.label)
			}
			playsToPass(t, tc.body)
		})
	}
}

// Buyback is one yes/no in XMage and the "buyback" cast option in gorge, paid
// with the buyback cost on top of the spell's (BuybackTest).
func TestBuybackElectionIsTheBuybackCastMode(t *testing.T) {
	const body = `addCard(Zone.BATTLEFIELD, playerA, "Forest", 5);
        addCard(Zone.BATTLEFIELD, playerA, "Silvercoat Lion", 1);
        addCard(Zone.HAND, playerA, "Elvish Fury", 1);
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Elvish Fury", "Silvercoat Lion");
        setChoice(playerA, true);
        setStrictChooseMode(true);
        setStopAt(1, PhaseStep.BEGIN_COMBAT);
        execute();
        assertPowerToughness(playerA, "Silvercoat Lion", 4, 4);
        assertHandCount(playerA, "Elvish Fury", 1);`
	cast := castStepOf(t, translateBody(t, body))
	if cast.CastMode != "buyback" || len(cast.Mana) != 5 {
		t.Fatalf("cast_mode %q mana %q, want buyback and 5 mana", cast.CastMode, cast.Mana)
	}
	playsToPass(t, body)
}

// Buyback is a single yes/no: a second yes after it answers something else (a
// "you may" on resolution) and must stay a queued answer, not be swallowed as
// a second election.
func TestBuybackTakesOnlyTheFirstBoolean(t *testing.T) {
	const body = `addCard(Zone.BATTLEFIELD, playerA, "Forest", 5);
        addCard(Zone.BATTLEFIELD, playerA, "Silvercoat Lion", 1);
        addCard(Zone.HAND, playerA, "Elvish Fury", 1);
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Elvish Fury", "Silvercoat Lion");
        setChoice(playerA, true);
        setChoice(playerA, true);
        setStrictChooseMode(true);
        setStopAt(1, PhaseStep.BEGIN_COMBAT);
        execute();
        assertHandCount(playerA, "Elvish Fury", 1);`
	res := translateBody(t, body)
	if cast := castStepOf(t, res); cast.CastMode != "buyback" {
		t.Fatalf("cast_mode %q, want buyback", cast.CastMode)
	}
	var yes int
	for _, p := range groupPicks(res) {
		if p == "yes" {
			yes++
		}
	}
	if yes != 1 {
		t.Fatalf("%d yes answers left for the later ask, want 1 (picks %v)", yes, groupPicks(res))
	}
}

const wortBody = `addCard(Zone.BATTLEFIELD, playerA, "Mountain", 7);
        addCard(Zone.HAND, playerA, "Wort, the Raidmother");
        addCard(Zone.HAND, playerA, "Lightning Bolt");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Wort, the Raidmother");
        castSpell(1, PhaseStep.POSTCOMBAT_MAIN, playerA, "Lightning Bolt", playerB);
        setChoice(playerA, true);
        setChoice(playerA, "Goblin Warrior Token");
        setChoice(playerA, "Goblin Warrior Token");
        setChoice(playerA, false);
        setStrictChooseMode(true);
        setStopAt(1, PhaseStep.END_TURN);
        execute();
        assertLife(playerB, 20 - 3 - 3);`

// Conspire, here granted by Wort, is one yes/no and the two taps in XMage; gorge
// offers the "conspired" cast option and then the tap election as one choose of
// two creatures (ConspireTest).
func TestConspireElectionIsTheConspiredCastMode(t *testing.T) {
	res := translateBody(t, wortBody)
	var cast *step
	for _, st := range res.Scenario.Steps {
		if st.Op == "cast" && st.Card == "p0:Lightning Bolt" {
			cast = st
		}
	}
	if cast == nil || cast.CastMode != "conspired" {
		t.Fatalf("Lightning Bolt cast %+v, want cast_mode conspired", cast)
	}
	playsToPass(t, wortBody)
}

// Two instances of conspire are paid separately (CR 702.78b). Gorge poses one
// election per cast, which is its defect, so the translator must not refuse the
// shape: it states the conspired cast mode and the scenario fails on gorge
// (one copy, XMage two) until gorge asks each instance.
func TestConspireSecondPaymentIsAKnownBugNotARefusal(t *testing.T) {
	body := strings.Replace(wortBody, "setChoice(playerA, true);\n", "setChoice(playerA, true);\n        setChoice(playerA, true);\n", 1)
	res := translateBody(t, body)
	if res.Scenario == nil {
		t.Fatalf("refused %s: %s", res.Code, res.Detail)
	}
	var cast *step
	for _, st := range res.Scenario.Steps {
		if st.Op == "cast" && st.Card == "p0:Lightning Bolt" {
			cast = st
		}
	}
	if cast == nil || cast.CastMode != "conspired" {
		t.Fatalf("Lightning Bolt cast %+v, want cast_mode conspired", cast)
	}
}

// A test that never states the conspire election leaves it to XMage's AI. The
// runner plays the plain cast, so a failing run says nothing about gorge: it
// refuses, while the same test that passes stays covered.
func TestUnstatedOptionalCostElectionIsAShape(t *testing.T) {
	const body = `addCard(Zone.HAND, playerA, "Burn Trail", 1);
        addCard(Zone.BATTLEFIELD, playerA, "Mountain", 4);
        addCard(Zone.BATTLEFIELD, playerA, "Goblin Assailant", 2);
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Burn Trail");
        addTarget(playerA, playerB);
        setStrictChooseMode(false);
        setStopAt(1, PhaseStep.BEGIN_COMBAT);
        execute();
        assertLife(playerB, 20 - 3 - 3);`
	res := translateBody(t, body)
	if res.Scenario == nil {
		t.Fatalf("refused %s: %s", res.Code, res.Detail)
	}
	runResult(testutilRegistry(t), res)
	if res.Status != "refused" || res.Code != "optional-cost-ai-election" {
		t.Fatalf("status %s code %q (%v), want refused optional-cost-ai-election", res.Status, res.Code, res.Fails)
	}
}
