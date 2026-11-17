package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// activations lists the cards a scenario's activate steps name, in order.
func activations(res *result) []string {
	var out []string
	for _, st := range res.Scenario.Steps {
		if st.Op == "activate" {
			out = append(out, st.Card)
		}
	}
	return out
}

func wantActivations(t *testing.T, res *result, want ...string) {
	t.Helper()
	if got := strings.Join(activations(res), ","); got != strings.Join(want, ",") {
		t.Fatalf("activate steps %q, want %q", got, strings.Join(want, ","))
	}
}

// Wall of Roots pays a cast with a -0/-1 counter. The pool-seeded cast never
// spent one, so the translator activates each Wall the cost needs and seeds
// only the rest: no lands pay {1}{G}, so both Walls are activated.
func TestCounterCostSourcePaysByActivation(t *testing.T) {
	res := runBody(t, `addCard(Zone.HAND, playerA, "Runeclaw Bear");
        addCard(Zone.BATTLEFIELD, playerA, "Wall of Roots", 2);
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Runeclaw Bear");
        setStopAt(1, PhaseStep.BEGIN_COMBAT);
        execute();
        assertPowerToughness(playerA, "Wall of Roots", 0, 4);
        assertPermanentCount(playerA, "Runeclaw Bear", 1);`)
	wantStatus(t, res, "pass", "")
	wantActivations(t, res, "p0:Wall of Roots", "p0:Wall of Roots#2")
	for _, st := range res.Scenario.Steps {
		if st.Op == "cast" && st.Mana != "" {
			t.Fatalf("cast seeds %q though both Walls pay", st.Mana)
		}
	}
}

// One Wall is needed for {G}; Momentary Blink makes the Wall a new object, so
// the second {G} cast activates it again (CR 400.7), where without the blink
// the activation limit would forbid it.
func TestFlickeredSourcePaysAgain(t *testing.T) {
	res := runBody(t, `addCard(Zone.HAND, playerA, "Wall of Wood", 2);
        addCard(Zone.HAND, playerA, "Momentary Blink");
        addCard(Zone.BATTLEFIELD, playerA, "Plains", 2);
        addCard(Zone.BATTLEFIELD, playerA, "Wall of Roots");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Wall of Wood");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Momentary Blink", "Wall of Roots", "Cast Wall of Wood");
        waitStackResolved(1, PhaseStep.PRECOMBAT_MAIN);
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Wall of Wood");
        setStopAt(1, PhaseStep.BEGIN_COMBAT);
        execute();
        assertPowerToughness(playerA, "Wall of Roots", 0, 4);
        assertPermanentCount(playerA, "Wall of Wood", 2);`)
	wantStatus(t, res, "pass", "")
	wantActivations(t, res, "p0:Wall of Roots", "p0:Wall of Roots")
}

// Hall of the Bandit Lord's haste rides the mana its own ability made. The
// cast needs all three sources, so the Hall is activated and the Mountains
// are seeded.
func TestRiderSourcePaysByActivation(t *testing.T) {
	res := runBody(t, `addCard(Zone.BATTLEFIELD, playerA, "Mountain", 2);
        addCard(Zone.BATTLEFIELD, playerA, "Hall of the Bandit Lord");
        addCard(Zone.HAND, playerA, "Goblin Roughrider");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Goblin Roughrider");
        setStrictChooseMode(true);
        setStopAt(1, PhaseStep.BEGIN_COMBAT);
        execute();
        assertAbility(playerA, "Goblin Roughrider", HasteAbility.getInstance(), true);`)
	wantStatus(t, res, "pass", "")
	wantActivations(t, res, "p0:Hall of the Bandit Lord")
}

// Boseiju's "can't be countered" rides its own mana the same way.
func TestRiderSourceOfALandPaysByActivation(t *testing.T) {
	res := runBody(t, `addCard(Zone.BATTLEFIELD, playerA, "Island", 4);
        addCard(Zone.BATTLEFIELD, playerA, "Boseiju, Who Shelters All");
        addCard(Zone.BATTLEFIELD, playerB, "Island", 2);
        addCard(Zone.HAND, playerA, "Brilliant Plan");
        addCard(Zone.HAND, playerB, "Counterspell");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Brilliant Plan");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerB, "Counterspell", "Brilliant Plan");
        setStopAt(1, PhaseStep.BEGIN_COMBAT);
        execute();
        assertHandCount(playerA, 3);
        assertGraveyardCount(playerB, "Counterspell", 1);`)
	wantStatus(t, res, "pass", "")
	wantActivations(t, res, "p0:Boseiju, Who Shelters All")
}

// A land that entered as a copy of a rider source (Vesuva as Hall) is a second
// payer: the Hall is tapped by the first cast, so the second activates Vesuva.
func TestClonedRiderSourceIsASecondPayer(t *testing.T) {
	res := runBody(t, `addCard(Zone.BATTLEFIELD, playerA, "Mountain", 1);
        addCard(Zone.HAND, playerA, "Mountain", 1);
        addCard(Zone.BATTLEFIELD, playerA, "Hall of the Bandit Lord");
        addCard(Zone.BATTLEFIELD, playerA, "Mirror Gallery");
        addCard(Zone.HAND, playerA, "Vesuva");
        addCard(Zone.HAND, playerA, "Goblin Piker");
        addCard(Zone.HAND, playerA, "Feral Maaka");
        playLand(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Vesuva");
        setChoice(playerA, true);
        setChoice(playerA, "Hall of the Bandit Lord");
        castSpell(3, PhaseStep.PRECOMBAT_MAIN, playerA, "Goblin Piker");
        playLand(3, PhaseStep.POSTCOMBAT_MAIN, playerA, "Mountain");
        castSpell(3, PhaseStep.POSTCOMBAT_MAIN, playerA, "Feral Maaka");
        setStrictChooseMode(true);
        setStopAt(3, PhaseStep.END_TURN);
        execute();
        assertAbility(playerA, "Goblin Piker", HasteAbility.getInstance(), true);
        assertAbility(playerA, "Feral Maaka", HasteAbility.getInstance(), true);`)
	wantStatus(t, res, "pass", "")
	wantActivations(t, res, "p0:Hall of the Bandit Lord", "p0:Vesuva")
}

// When the other sources pay the whole cost, which payer XMage used is not
// decided by the cost: the cast activates nothing, and a haste assert that
// fails is refused rather than blamed on gorge.
func TestUndeterminedRiderPayerStaysRefused(t *testing.T) {
	res := runBody(t, `addCard(Zone.BATTLEFIELD, playerA, "Mountain", 3);
        addCard(Zone.BATTLEFIELD, playerA, "Hall of the Bandit Lord");
        addCard(Zone.HAND, playerA, "Ember Hauler");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Ember Hauler");
        setStrictChooseMode(true);
        setStopAt(1, PhaseStep.BEGIN_COMBAT);
        execute();
        assertAbility(playerA, "Ember Hauler", HasteAbility.getInstance(), true);`)
	wantStatus(t, res, "refused", "mana-spent-by-source-type")
}

// The activation is gorge-only: XMage pays by itself, so the driver line
// drops it with the gorge-only passes.
func TestPaymentActivationIsNotSentToTheXMageDriver(t *testing.T) {
	res := runBody(t, `addCard(Zone.HAND, playerA, "Runeclaw Bear");
        addCard(Zone.BATTLEFIELD, playerA, "Wall of Roots", 2);
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Runeclaw Bear");
        setStopAt(1, PhaseStep.BEGIN_COMBAT);
        execute();
        assertPowerToughness(playerA, "Wall of Roots", 0, 4);`)
	wantStatus(t, res, "pass", "")
	line, err := driverLine(res)
	if err != nil {
		t.Fatal(err)
	}
	var d struct {
		Steps []struct{ Op string }
		Abil  []string `json:"xmage_ability"`
	}
	if err := json.Unmarshal(line, &d); err != nil {
		t.Fatal(err)
	}
	for _, s := range d.Steps {
		if s.Op == "activate" {
			t.Fatalf("driver line carries a payment activation: %s", line)
		}
	}
	if len(d.Abil) != len(d.Steps) {
		t.Fatalf("xmage_ability has %d entries for %d steps", len(d.Abil), len(d.Steps))
	}
}

// Villainous Wealth's "Play Amount$ All" is one modes ask in gorge but a pick
// and a yes/no per card in XMage: two casts cannot be mapped answer by answer.
func TestPlayAllAtOnceWithSeveralCastsIsRefused(t *testing.T) {
	res := translateBody(t, `addCard(Zone.HAND, playerA, "Villainous Wealth");
        addCard(Zone.BATTLEFIELD, playerA, "Swamp", 2);
        addCard(Zone.BATTLEFIELD, playerA, "Forest", 2);
        addCard(Zone.BATTLEFIELD, playerA, "Island", 2);
        addCard(Zone.LIBRARY, playerB, "Mox Emerald");
        addCard(Zone.LIBRARY, playerB, "Quicken");
        addCard(Zone.LIBRARY, playerB, "Mox Sapphire");
        skipInitShuffling();
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Villainous Wealth", playerB);
        setChoice(playerA, "X=3");
        setChoice(playerA, "Mox Emerald");
        setChoice(playerA, "Yes");
        setChoice(playerA, "Mox Sapphire");
        setChoice(playerA, "Yes");
        setChoice(playerA, "Yes");
        setStopAt(1, PhaseStep.PRECOMBAT_MAIN);
        execute();
        assertPermanentCount(playerA, "Mox Emerald", 1);`)
	if res.Scenario != nil || res.Code != "play-all-one-ask" {
		t.Fatalf("status %s code %q, want refused play-all-one-ask", res.Status, res.Code)
	}
}
