package main

import (
	"strings"
	"testing"
)

// playsToPass translates a Java test body, plays it through the runner's
// placement loop and fails unless every XMage assert holds.
func playsToPass(t *testing.T, body string) {
	t.Helper()
	res := translateBody(t, body)
	if res.Scenario == nil {
		t.Fatalf("refused %s: %s", res.Code, res.Detail)
	}
	runResult(testutilRegistry(t), res)
	if res.Status != "pass" {
		t.Fatalf("status %s (%v), want pass", res.Status, res.Fails)
	}
}

// Omo's enter trigger names its targets with addTarget, but its hybrid
// {G/U} cost makes gorge pose a "Pay U" choose at the cast first. The unused
// target is re-homed as an answer of that payment choose, never reaching the
// trigger's target ask, so the counters are not put. This is one row, below
// the three-row bar for a shared translator fix: the shape is refused, not
// silently played differently.
func TestUnusedTargetPaymentChooseRefuses(t *testing.T) {
	res := translateBody(t, `setStrictChooseMode(true);
        addCard(Zone.BATTLEFIELD, playerA, "Island", 3);
        addCard(Zone.BATTLEFIELD, playerA, "Coiling Woodworm", 1);
        addCard(Zone.BATTLEFIELD, playerA, "Imperious Perfect", 1);
        addCard(Zone.HAND, playerA, "Omo, Queen of Vesuva");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Omo, Queen of Vesuva", true);
        addTarget(playerA, "Island");
        addTarget(playerA, "Coiling Woodworm");
        setStopAt(1, PhaseStep.POSTCOMBAT_MAIN);
        execute();
        assertCounterCount(playerA, "Coiling Woodworm", CounterType.EVERYTHING, 1);
        assertPowerToughness(playerA, "Coiling Woodworm", 2, 2);`)
	if res.Scenario == nil {
		t.Fatalf("refused early %s: %s", res.Code, res.Detail)
	}
	runResult(testutilRegistry(t), res)
	if res.Status != "refused" || res.Code != "unused-target-payment-choose" {
		t.Fatalf("status %s code %q (%v), want refused unused-target-payment-choose", res.Status, res.Code, res.Fails)
	}
}

// Beseech the Mirror's Bargain: XMage's first setChoice(true) elects the
// optional sacrifice and the next names the permanent. Gorge offers the
// election as the "bargained" cast mode (then asks what to sacrifice), so
// the boolean became an unconsumed answer and the spell was cast unbargained.
func TestBargainChoiceIsTheBargainedCastMode(t *testing.T) {
	playsToPass(t, `addCard(Zone.LIBRARY, playerA, "Gaea's Will");
        addCard(Zone.HAND, playerA, "Beseech the Mirror");
        addCard(Zone.BATTLEFIELD, playerA, "Sol Ring");
        addCard(Zone.BATTLEFIELD, playerA, "Swamp", 5);
        addCard(Zone.HAND, playerA, "Dark Ritual");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Beseech the Mirror");
        setChoice(playerA, true);
        setChoice(playerA, "Sol Ring");
        addTarget(playerA, "Gaea's Will");
        setChoice(playerA, true);
        castSpell(1, PhaseStep.POSTCOMBAT_MAIN, playerA, "Dark Ritual");
        setStrictChooseMode(true);
        setStopAt(1, PhaseStep.POSTCOMBAT_MAIN);
        execute();
        assertExileCount(playerA, "Dark Ritual", 1);`)
}

// Anhelo grants casualty to the first instant or sorcery each turn
// (AddKeyword$ Casualty:2 on a static), so the spell's own face carries no
// Casualty keyword: the setup permanent's grant makes the first setChoice the
// casualty election.
func TestGrantedCasualtyChoiceIsTheCasualtyCastMode(t *testing.T) {
	playsToPass(t, `addCard(Zone.BATTLEFIELD, playerA, "Anhelo, the Painter");
        addCard(Zone.BATTLEFIELD, playerA, "Mountain", 2);
        addCard(Zone.BATTLEFIELD, playerA, "Aetherwind Basker", 2);
        addCard(Zone.HAND, playerA, "Lightning Bolt", 2);
        setStrictChooseMode(true);
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Lightning Bolt", playerB);
        setChoice(playerA, "Yes");
        setChoice(playerA, "Aetherwind Basker");
        setChoice(playerA, "No");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Lightning Bolt", playerB);
        setStopAt(1, PhaseStep.END_TURN);
        execute();
        assertLife(playerB, 20 - 2*3 - 3);
        assertPermanentCount(playerA, "Aetherwind Basker", 1);`)
}

// XMage's checkExileCount counts the named card in every exile zone and
// ignores its player argument. When the named seat does not own the card
// (Assimilation Aegis exiles p1's Bears while the check names playerA), the
// two readings disagree and the shape is refused rather than counting the
// wrong seat.
func TestCheckExileCountPlayerMismatchRefuses(t *testing.T) {
	res := translateBody(t, `addCard(Zone.HAND, playerA, "Lightning Bolt");
        addCard(Zone.BATTLEFIELD, playerA, "Mountain");
        addCard(Zone.BATTLEFIELD, playerB, "Grizzly Bears");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Lightning Bolt", "Grizzly Bears");
        checkExileCount("x", 1, PhaseStep.POSTCOMBAT_MAIN, playerA, "Grizzly Bears", 1);
        setStopAt(1, PhaseStep.END_TURN);
        execute();`)
	if res.Status != "refused" || res.Code != "check-exile-count-player" {
		t.Fatalf("status %s code %q (%s), want refused check-exile-count-player", res.Status, res.Code, res.Detail)
	}
}

// A check that names the card's owner keeps the old mapping: the count lands
// on that seat.
func TestCheckExileCountKeepsMatchingPlayer(t *testing.T) {
	res := translateBody(t, `addCard(Zone.HAND, playerA, "Lightning Bolt");
        addCard(Zone.BATTLEFIELD, playerA, "Mountain");
        addCard(Zone.BATTLEFIELD, playerB, "Grizzly Bears");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Lightning Bolt", "Grizzly Bears");
        checkExileCount("x", 1, PhaseStep.POSTCOMBAT_MAIN, playerB, "Grizzly Bears", 1);
        setStopAt(1, PhaseStep.END_TURN);
        execute();`)
	if res.Scenario == nil {
		t.Fatalf("refused %s: %s", res.Code, res.Detail)
	}
	found := false
	for _, st := range res.Scenario.Steps {
		for _, e := range st.Expect {
			if e.Count != nil && e.Count.Zone == "exile" {
				found = true
				if e.Count.Seat != 1 {
					t.Errorf("exile count on seat %d, want 1 (the Bears' owner)", e.Count.Seat)
				}
			}
		}
	}
	if !found {
		t.Fatal("no exile count expectation")
	}
}

// Shapes whose verdict depends on which permanents paid for a cast: the
// runner seeds a step's mana into the pool, so nothing is tapped or
// sacrificed and the translator refuses them.
func TestManaSourceShapesRefuse(t *testing.T) {
	cases := []struct{ name, body, code string }{
		{"treasure mana", `addCard(Zone.HAND, playerA, "Rain of Riches");
        addCard(Zone.HAND, playerA, "Goblin Piker");
        addCard(Zone.BATTLEFIELD, playerA, "Mountain", 5);
        addCard(Zone.LIBRARY, playerA, "Elite Vanguard", 1);
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Rain of Riches", true);
        castSpell(1, PhaseStep.POSTCOMBAT_MAIN, playerA, "Goblin Piker");
        setStopAt(1, PhaseStep.END_TURN);
        execute();
        assertPermanentCount(playerA, "Elite Vanguard", 1);`, "mana-spent-by-source-type"},
		{"desert mana", `addCard(Zone.BATTLEFIELD, playerA, "Hostile Desert", 2);
        addCard(Zone.BATTLEFIELD, playerA, "Mountain", 5);
        addCard(Zone.HAND, playerA, "Cataclysmic Prospecting");
        setChoice(playerA, "X=5");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Cataclysmic Prospecting");
        setStopAt(1, PhaseStep.BEGIN_COMBAT);
        execute();
        assertPermanentCount(playerA, "Treasure Token", 2);`, "mana-spent-by-source-type"},
		{"tapped mana source targeted", `addCard(Zone.BATTLEFIELD, playerA, "The Spear of Bashenga");
        addCard(Zone.BATTLEFIELD, playerA, "Silvercoat Lion");
        addCard(Zone.BATTLEFIELD, playerA, "Plains", 2);
        addCard(Zone.HAND, playerB, "Crown-Hunter Hireling");
        addCard(Zone.BATTLEFIELD, playerB, "Mountain", 4);
        addCard(Zone.BATTLEFIELD, playerB, "Mind Stone");
        castSpell(2, PhaseStep.PRECOMBAT_MAIN, playerB, "Crown-Hunter Hireling");
        activateAbility(3, PhaseStep.PRECOMBAT_MAIN, playerA, "Equip", "Silvercoat Lion");
        attack(3, playerA, "Silvercoat Lion", playerB);
        addTarget(playerA, "Mind Stone");
        setStopAt(3, PhaseStep.END_COMBAT);
        execute();
        assertGraveyardCount(playerB, "Mind Stone", 1);`, "tapped-target-mana-source"},
	}
	for _, c := range cases {
		res := translateBody(t, c.body)
		if res.Scenario == nil {
			t.Errorf("%s: refused early %s: %s", c.name, res.Code, res.Detail)
			continue
		}
		if res.shape == nil || res.shape.Code != c.code {
			t.Errorf("%s: shape %v, want %s", c.name, res.shape, c.code)
			continue
		}
		runResult(testutilRegistry(t), res)
		if res.Status != "refused" || res.Code != c.code {
			t.Errorf("%s: status %s code %q (%v), want refused %s", c.name, res.Status, res.Code, res.Fails, c.code)
		}
	}
}

// A test of the same shape whose run passes stays covered: the refusal only
// replaces a failing verdict. Cataclysmic Prospecting with no Desert pays
// from Mountains alone, which the pool seed reproduces.
func TestManaSourceShapeKeepsAPassingRun(t *testing.T) {
	res := translateBody(t, `addCard(Zone.BATTLEFIELD, playerB, "Indomitable Ancients");
        addCard(Zone.BATTLEFIELD, playerA, "Mountain", 7);
        addCard(Zone.HAND, playerA, "Cataclysmic Prospecting");
        setChoice(playerA, "X=5");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Cataclysmic Prospecting");
        setStopAt(1, PhaseStep.BEGIN_COMBAT);
        execute();
        assertGraveyardCount(playerA, "Cataclysmic Prospecting", 1);
        assertDamageReceived(playerB, "Indomitable Ancients", 5);
        assertPermanentCount(playerA, "Treasure Token", 0);`)
	if res.Scenario == nil || res.shape == nil {
		t.Fatalf("scenario %v shape %v, want a scenario carrying the shape", res.Scenario != nil, res.shape)
	}
	runResult(testutilRegistry(t), res)
	if res.Status != "pass" {
		t.Fatalf("status %s (%v), want pass", res.Status, res.Fails)
	}
}

// Three same-text triggers whose order the test states and whose searches it
// answers with picks in that order: gorge keeps its own order, so the picks
// would land on other triggers' searches. The translator refuses it.
func TestStatedTriggerOrderWithPicksRefuses(t *testing.T) {
	res := translateBody(t, `addCard(Zone.BATTLEFIELD, playerA, "Verdant Succession");
        addCard(Zone.BATTLEFIELD, playerA, "Memnite");
        addCard(Zone.LIBRARY, playerA, "Memnite");
        addCard(Zone.BATTLEFIELD, playerA, "Birds of Paradise");
        addCard(Zone.LIBRARY, playerA, "Birds of Paradise");
        addCard(Zone.BATTLEFIELD, playerA, "Arbor Elf");
        addCard(Zone.LIBRARY, playerA, "Arbor Elf");
        addCard(Zone.BATTLEFIELD, playerA, "Ankle Biter");
        addCard(Zone.LIBRARY, playerA, "Ankle Biter");
        addCard(Zone.BATTLEFIELD, playerA, "Swamp", 4);
        addCard(Zone.HAND, playerA, "Damnation");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Damnation");
        setChoice(playerA, "Whenever");
        setChoice(playerA, "Whenever");
        setChoice(playerA, "Yes");
        setChoice(playerA, "Yes");
        setChoice(playerA, "Yes");
        addTarget(playerA, "Birds of Paradise");
        addTarget(playerA, "Arbor Elf");
        addTarget(playerA, "Ankle Biter");
        setStrictChooseMode(true);
        setStopAt(1, PhaseStep.BEGIN_COMBAT);
        execute();
        assertPermanentCount(playerA, "Birds of Paradise", 1);
        assertPermanentCount(playerA, "Arbor Elf", 1);
        assertPermanentCount(playerA, "Ankle Biter", 1);`)
	if res.Scenario == nil || res.shape == nil || res.shape.Code != "trigger-order-search" {
		t.Fatalf("scenario %v shape %v, want trigger-order-search", res.Scenario != nil, res.shape)
	}
	runResult(testutilRegistry(t), res)
	if res.Status != "refused" || res.Code != "trigger-order-search" {
		t.Fatalf("status %s code %q (%v), want refused trigger-order-search", res.Status, res.Code, res.Fails)
	}
}

// Galvanoth's upkeep trigger fires during turn 1's upkeep, before the first
// scenario step: gorge asks the may-look and may-cast there and the runner's
// setup answers them by fallback, so the test's two setChoice(true) have no
// step to ride on. The row is refused instead of failing its asserts.
func TestSetupUpkeepDecisionRefuses(t *testing.T) {
	res := translateBody(t, `setStrictChooseMode(true);
        skipInitShuffling();
        addCard(Zone.LIBRARY, playerA, "Divination");
        addCard(Zone.BATTLEFIELD, playerA, "Galvanoth");
        setChoice(playerA, true);
        setChoice(playerA, true);
        setStopAt(1, PhaseStep.PRECOMBAT_MAIN);
        execute();
        assertGraveyardCount(playerA, "Divination", 1);
        assertHandCount(playerA, 2);`)
	if res.Scenario == nil {
		t.Fatalf("refused early %s: %s", res.Code, res.Detail)
	}
	runResult(testutilRegistry(t), res)
	if res.Status != "refused" || res.Code != "setup-decision" {
		t.Fatalf("status %s code %q (%v), want refused setup-decision", res.Status, res.Code, res.Fails)
	}
}

// Birthing Ritual looks at the top seven cards: gorge poses a lone "Continue"
// acknowledgement ahead of the sacrifice and the pick, which XMage never asks,
// so the stated picks are offered to the acknowledgement and cannot be
// placed. The translator refuses the row instead.
func TestContinueAcknowledgementRefuses(t *testing.T) {
	res := translateBody(t, `setStrictChooseMode(true);
        skipInitShuffling();
        addCard(Zone.BATTLEFIELD, playerA, "Birthing Ritual");
        addCard(Zone.BATTLEFIELD, playerA, "Grizzly Bears");
        addCard(Zone.LIBRARY, playerA, "Memnite", 8);
        addCard(Zone.LIBRARY, playerA, "Centaur Courser", 8);
        setChoice(playerA, "Grizzly Bears");
        setChoice(playerA, "Centaur Courser");
        checkPermanentCount("3: Courser in play", 3, PhaseStep.PRECOMBAT_MAIN, playerA, "Centaur Courser", 1);
        checkGraveyardCount("3: Bears in graveyard", 3, PhaseStep.PRECOMBAT_MAIN, playerA, "Grizzly Bears", 1);
        setChoice(playerA, "Centaur Courser");
        setChoice(playerA, "Memnite");
        setStopAt(4, PhaseStep.UPKEEP);
        execute();
        assertPermanentCount(playerA, "Memnite", 1);
        assertHandCount(playerA, "Centaur Courser", 1);`)
	if res.Scenario == nil {
		t.Fatalf("refused early %s: %s", res.Code, res.Detail)
	}
	runResult(testutilRegistry(t), res)
	if res.Status != "refused" || res.Code != "continue-acknowledgement" {
		t.Fatalf("status %s code %q (%v), want refused continue-acknowledgement", res.Status, res.Code, res.Fails)
	}
}

// castModeOf returns the cast_mode of the first cast step whose card ref names
// card, or "" when the scenario has no such step.
func castModeOf(t *testing.T, res *result, card string) string {
	t.Helper()
	if res.Scenario == nil {
		t.Fatalf("refused %s: %s", res.Code, res.Detail)
	}
	for _, st := range res.Scenario.Steps {
		if st.Op == "cast" && sameName(strings.TrimPrefix(st.Card[strings.Index(st.Card, ":")+1:], "p0:"), card) {
			return st.CastMode
		}
	}
	t.Fatalf("no cast step for %s", card)
	return ""
}

// Anhelo grants casualty only to instant and sorcery spells its controller
// casts. A creature spell must not receive the cast mode even when the test's
// first setChoice is true.
func TestGrantedCasualtySkipsCreatureSpells(t *testing.T) {
	res := translateBody(t, `addCard(Zone.BATTLEFIELD, playerA, "Anhelo, the Painter");
        addCard(Zone.BATTLEFIELD, playerA, "Mountain", 2);
        addCard(Zone.HAND, playerA, "Grizzly Bears");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Grizzly Bears");
        setChoice(playerA, true);
        setStopAt(1, PhaseStep.END_TURN);
        execute();
        assertPermanentCount(playerA, "Grizzly Bears", 1);`)
	if mode := castModeOf(t, res, "Grizzly Bears"); mode == "casualty" {
		t.Fatal("Anhelo's instant/sorcery grant was applied to a creature spell")
	}
}

// The granting permanent must belong to the caster: Anhelo on playerA's
// battlefield does not grant casualty to playerB's spell.
func TestGrantedCasualtySkipsOpponentSpells(t *testing.T) {
	res := translateBody(t, `addCard(Zone.BATTLEFIELD, playerA, "Anhelo, the Painter");
        addCard(Zone.BATTLEFIELD, playerB, "Mountain");
        addCard(Zone.HAND, playerB, "Lightning Bolt");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerB, "Lightning Bolt", playerA);
        setChoice(playerB, true);
        setStopAt(1, PhaseStep.END_TURN);
        execute();
        assertLife(playerA, 17);`)
	if mode := castModeOf(t, res, "Lightning Bolt"); mode == "casualty" {
		t.Fatal("Anhelo granted casualty to an opponent's spell")
	}
}

// Anhelo's static is the first instant or sorcery spell each turn: the
// second bolt must not receive the casualty cast mode.
func TestGrantedCasualtyOnlyTheFirstSpellEachTurn(t *testing.T) {
	res := translateBody(t, `addCard(Zone.BATTLEFIELD, playerA, "Anhelo, the Painter");
        addCard(Zone.BATTLEFIELD, playerA, "Mountain", 2);
        addCard(Zone.BATTLEFIELD, playerA, "Aetherwind Basker", 1);
        addCard(Zone.HAND, playerA, "Lightning Bolt", 2);
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Lightning Bolt", playerB);
        setChoice(playerA, "Yes");
        setChoice(playerA, "Aetherwind Basker");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Lightning Bolt", playerB);
        setChoice(playerA, "Yes");
        setStopAt(1, PhaseStep.END_TURN);
        execute();
        assertLife(playerB, 11);`)
	if res.Scenario == nil {
		t.Fatalf("refused %s: %s", res.Code, res.Detail)
	}
	var modes []string
	for _, st := range res.Scenario.Steps {
		if st.Op == "cast" {
			modes = append(modes, st.CastMode)
		}
	}
	if len(modes) != 2 || modes[0] != "casualty" || modes[1] != "" {
		t.Fatalf("cast modes %v, want [casualty \"\"]", modes)
	}
}

// A shape refusal must never hide a replay diff or a harness failure: those
// are the audit gate's verdict and decideStatus ranks them above fail-expect.
func TestShapeRefusalWaitsForHardFailures(t *testing.T) {
	r := &result{shape: refuse("trigger-order-search", "x"), Status: "fail-expect",
		Fails: []string{"harness: answer \"Island\" not offered"}}
	if shapeRefusalApplies(r) {
		t.Fatal("harness failure was refused away")
	}
	r.Fails = []string{"p0:Coiling Woodworm: EVERYTHING counters 0, want 1"}
	if !shapeRefusalApplies(r) {
		t.Fatal("a plain fail-expect shape was not refused")
	}
	r.ReplayDiff = "live != replay"
	if shapeRefusalApplies(r) {
		t.Fatal("replay diff was refused away")
	}
}

// A "target tapped" card in hand must not arm the tapped-target shape: only
// the exact "target tapped nonland permanent" battlefield ability does.
func TestManaSourceShapeIgnoresUnrelatedTappedTarget(t *testing.T) {
	res := translateBody(t, `addCard(Zone.HAND, playerA, "Assassinate");
        addCard(Zone.HAND, playerA, "Manic Vandal");
        addCard(Zone.BATTLEFIELD, playerA, "Mountain", 3);
        addCard(Zone.BATTLEFIELD, playerB, "Mind Stone");
        addCard(Zone.HAND, playerB, "Lightning Bolt");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Manic Vandal");
        addTarget(playerA, "Mind Stone");
        castSpell(2, PhaseStep.PRECOMBAT_MAIN, playerB, "Lightning Bolt", playerA);
        setStopAt(2, PhaseStep.END_TURN);
        execute();
        assertGraveyardCount(playerB, "Mind Stone", 1);`)
	if res.shape != nil {
		t.Fatalf("unrelated tapped-target card armed shape %v", res.shape)
	}
}

// A card that reads mana from a source type only arms the shape when it is
// live: on the battlefield, or itself cast. A copy left in hand does not.
func TestManaSpentShapeIgnoresUncastCard(t *testing.T) {
	res := translateBody(t, `addCard(Zone.HAND, playerA, "Rain of Riches");
        addCard(Zone.HAND, playerA, "Goblin Piker");
        addCard(Zone.BATTLEFIELD, playerA, "Mountain", 3);
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Goblin Piker");
        setStopAt(1, PhaseStep.END_TURN);
        execute();
        assertPermanentCount(playerA, "Goblin Piker", 1);`)
	if res.shape != nil {
		t.Fatalf("uncast mana-source card armed shape %v", res.shape)
	}
}

// The trigger-order refusal needs a same-text repeated trigger: one dropped
// order, or orders of different abilities, cannot line card picks up with a
// repeated search and must not mask a real assert failure.
func TestTriggerOrderRefusalNeedsSameTextOrder(t *testing.T) {
	one := &translator{droppedOrder: 1, droppedOrderTexts: []string{"Whenever a creature dies"},
		choiceList: []choice{{target: true}, {target: true}}}
	if r := one.orderedPicksRefusal(); r != nil {
		t.Fatalf("a single dropped order refused: %v", r)
	}
	different := &translator{droppedOrder: 2,
		droppedOrderTexts: []string{"Whenever a creature dies", "At the beginning of combat"},
		choiceList:        []choice{{target: true}, {target: true}}}
	if r := different.orderedPicksRefusal(); r != nil {
		t.Fatalf("orders of different abilities refused: %v", r)
	}
	same := &translator{droppedOrder: 2,
		droppedOrderTexts: []string{"Whenever a creature dies", "Whenever a creature dies"},
		choiceList:        []choice{{target: true}, {target: true}}}
	if r := same.orderedPicksRefusal(); r == nil || r.Code != "trigger-order-search" {
		t.Fatalf("same-text orders: refusal %v, want trigger-order-search", r)
	}
}

// Vraska sets the artifact subtype set to Treasure while removing card types;
// the XMage test expects the old artifact subtype Clue retained. The two
// readings are contested, so the failing row is refused, not recorded as a
// gorge bug.
func TestRemoveCardTypesSubtypeContestRefuses(t *testing.T) {
	res := translateBody(t, `addCard(Zone.BATTLEFIELD, playerA, "Vraska, the Silencer");
        addCard(Zone.BATTLEFIELD, playerA, "Swamp", 3);
        addCard(Zone.HAND, playerA, "Doom Blade");
        addCard(Zone.BATTLEFIELD, playerB, "Red Herring");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Doom Blade", "Red Herring");
        setChoice(playerA, true);
        setStopAt(1, PhaseStep.BEGIN_COMBAT);
        execute();
        assertSubtype("Red Herring", SubType.CLUE);`)
	if res.Scenario == nil {
		t.Fatalf("refused early %s: %s", res.Code, res.Detail)
	}
	runResult(testutilRegistry(t), res)
	if res.Status != "refused" || res.Code != "removecardtypes-subtype-contested" {
		t.Fatalf("status %s code %q (%v), want refused removecardtypes-subtype-contested", res.Status, res.Code, res.Fails)
	}
}

// The grant's Affected$ can name the type after "Card." (Ashad's
// Card.Artifact+nonLegendary+YouCtrl); the matcher must read Artifact as a
// type word, not as an unknown qualifier.
func TestAffectedMatchesCardArtifactForm(t *testing.T) {
	reg := testutilRegistry(t)
	c, ok := reg.Lookup("Mind Stone")
	if !ok || len(c.Faces) == 0 {
		t.Fatal("Mind Stone missing from the corpus")
	}
	f := c.Faces[0]
	if !affectedMatchesFace(f, "Card.Artifact+nonLegendary+YouCtrl") {
		t.Fatal("Card.Artifact+nonLegendary+YouCtrl did not match Mind Stone")
	}
	if affectedMatchesFace(f, "Creature.YouCtrl") {
		t.Fatal("Creature.YouCtrl matched an artifact")
	}
}
