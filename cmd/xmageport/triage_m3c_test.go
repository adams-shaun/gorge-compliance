package main

import "testing"

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
// targets were re-homed as answers of that payment choose (kind choose, not
// offered), never reaching the trigger's target asks at the resolve step; the
// payment ask is cost plumbing, so the targets ride on to the step that asks.
func TestUnusedTargetsSkipThePaymentChoose(t *testing.T) {
	playsToPass(t, `setStrictChooseMode(true);
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
// ignores its player argument, so the expectation belongs to the seat that
// owns the card (Assimilation Aegis exiles p1's Bears while the check names
// playerA); keyed to the named seat it read an empty exile.
func TestCheckExileCountIgnoresItsPlayer(t *testing.T) {
	res := translateBody(t, `addCard(Zone.HAND, playerA, "Lightning Bolt");
        addCard(Zone.BATTLEFIELD, playerA, "Mountain");
        addCard(Zone.BATTLEFIELD, playerB, "Grizzly Bears");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Lightning Bolt", "Grizzly Bears");
        checkExileCount("x", 1, PhaseStep.POSTCOMBAT_MAIN, playerA, "Grizzly Bears", 1);
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
