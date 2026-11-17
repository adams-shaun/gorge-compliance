package main

import "testing"

// XMage's checkPlayableAbility is true when any cast of the card is payable
// with the lands: Willbender's face-down {3} with three Swamps, Cyclonic
// Rift's {6}{U} overload with seven Islands. The runner pays from the pool,
// so the check's mana must be what the lands can make, not only the plain
// cost (MorphTest, OverloadTest). Only the morph case is failing-first for the
// land pool: the overload case already passes without it (Cyclonic Rift's
// overload is seeded by its own alternative-cost path) and stays as a
// regression check.
func TestAlternateCastCheckSeedsWhatTheLandsCanPay(t *testing.T) {
	for name, body := range map[string]string{
		"morph": `addCard(Zone.HAND, playerA, "Willbender");
        addCard(Zone.BATTLEFIELD, playerA, "Swamp", 3);
        checkPlayableAbility("can", 1, PhaseStep.PRECOMBAT_MAIN, playerA, "Cast Willbender", true);
        checkPlayableAbility("can't", 2, PhaseStep.PRECOMBAT_MAIN, playerA, "Cast Willbender", false);
        setStrictChooseMode(true);
        setStopAt(2, PhaseStep.END_TURN);
        execute();`,
		"overload": `addCard(Zone.HAND, playerA, "Cyclonic Rift");
        addCard(Zone.BATTLEFIELD, playerA, "Island", 7);
        addCard(Zone.BATTLEFIELD, playerB, "Balduvian Bears", 1);
        checkPlayableAbility("before", 1, PhaseStep.PRECOMBAT_MAIN, playerA, "Cast Cyclonic Rift", true);
        checkPlayableAbility("before", 1, PhaseStep.PRECOMBAT_MAIN, playerA, "Cast Cyclonic Rift with overload", true);
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Cyclonic Rift");
        addTarget(playerA, "Balduvian Bears");
        setStrictChooseMode(true);
        setStopAt(1, PhaseStep.END_TURN);
        execute();
        assertGraveyardCount(playerA, "Cyclonic Rift", 1);`,
	} {
		t.Run(name, func(t *testing.T) { playsToPass(t, body) })
	}
}

// A prepared creature casts a copy of its prepare spell from exile: that copy,
// not the creature, is the cast option's object, so a check that the spell is
// castable names it. Before the creature is cast, or once it is gone, no copy
// exists and the check keeps the card's own ref (PrepareTest).
func TestPreparedCopyCheckNamesTheCopy(t *testing.T) {
	const castFirst = `addCard(Zone.HAND, playerA, "Goblin Glasswright");
        addCard(Zone.BATTLEFIELD, playerA, "Mountain", 3);
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Goblin Glasswright");
        checkPlayableAbility("copy", 3, PhaseStep.PRECOMBAT_MAIN, playerA, "Cast Craft with Pride", true);
        setStopAt(3, PhaseStep.BEGIN_COMBAT);
        execute();`
	playsToPass(t, castFirst)
	res := translateBody(t, castFirst)
	if res.Scenario == nil {
		t.Fatalf("refused %s: %s", res.Code, res.Detail)
	}
	var ref string
	for _, st := range res.Scenario.Steps {
		for _, e := range st.Expect {
			if e.Offered != nil {
				ref = e.Offered.Card
			}
		}
	}
	if ref != "p0:Craft with Pride" {
		t.Fatalf("offered ref %q, want the prepared copy p0:Craft with Pride", ref)
	}
	// No cast yet: the spell is not castable from hand, and the card's own
	// ref is the only object there is.
	playsToPass(t, `addCard(Zone.HAND, playerA, "Goblin Glasswright");
        addCard(Zone.BATTLEFIELD, playerA, "Mountain", 2);
        checkPlayableAbility("prepare spell isn't castable", 1, PhaseStep.PRECOMBAT_MAIN, playerA, "Cast Craft with Pride", false);
        setStopAt(1, PhaseStep.BEGIN_COMBAT);
        execute();`)
}
