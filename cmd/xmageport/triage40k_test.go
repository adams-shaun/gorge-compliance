package main

import (
	"fmt"
	"testing"

	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// gorge lists a cast option only while the pool holds its cost and the runner
// has no autopay, so XMage's checkPlayableAbility("Cast X", true) (the lands
// pay) is expressible only with the cost floated on the step that carries the
// offered expect (Outwit, Kess, Uba Mask, Emrakul).
func TestOfferedCastCheckFloatsTheCost(t *testing.T) {
	res := translateTapped(t, `addCard(Zone.BATTLEFIELD, playerA, "Mountain");
        addCard(Zone.HAND, playerA, "Lightning Bolt");
        checkPlayableAbility("bolt", 1, PhaseStep.PRECOMBAT_MAIN, playerA, "Cast Lightning Bolt", true);`)
	if res.Scenario == nil {
		t.Fatalf("refused %s: %s", res.Code, res.Detail)
	}
	var carrier *step
	for _, st := range res.Scenario.Steps {
		for _, x := range st.Expect {
			if x.Offered != nil {
				carrier = st
			}
		}
	}
	if carrier == nil || carrier.Op != "mana" || carrier.Mana != "R" {
		t.Fatalf("offered expect rides %+v, want a mana step floating R", carrier)
	}
	// want=false needs nothing floated: no mana, no offer
	res = translateTapped(t, `addCard(Zone.BATTLEFIELD, playerA, "Mountain");
        addCard(Zone.HAND, playerA, "Lightning Bolt");
        checkPlayableAbility("bolt", 1, PhaseStep.PRECOMBAT_MAIN, playerA, "Cast Lightning Bolt", false);`)
	if res.Scenario == nil {
		t.Fatalf("refused %s: %s", res.Code, res.Detail)
	}
	for _, st := range res.Scenario.Steps {
		if st.Op == "mana" {
			t.Fatalf("want=false floated mana: %+v", st)
		}
	}
}

// Sigurd's "put another counter", Indomitable Might's "assign as though not
// blocked" and Emeria Shepherd's "battlefield" are the SECOND option of a
// two-option ask: a boolean maps to the first option by convention, so a run
// must be able to invert each mapped boolean (runResult replays a failing run
// with every inversion mask).
func TestBoolMappingInvertsPerMask(t *testing.T) {
	opts := []dumpOption{{Index: 0, Kind: "aor_remove", Label: "Remove a LORE counter"}, {Index: 1, Kind: "aor_put", Label: "Put another LORE counter on it"}}
	label, other, ok := mapBoolPair("yes", opts)
	if !ok || label != "Remove a LORE counter" || other != "Put another LORE counter on it" {
		t.Fatalf("mapBoolPair(yes) = %q, %q, %v", label, other, ok)
	}
	label, other, _ = mapBoolPair("no", opts)
	if label != "Put another LORE counter on it" || other != "Remove a LORE counter" {
		t.Fatalf("mapBoolPair(no) = %q, %q", label, other)
	}
	f := &boolFlips{mask: 0b10}
	if f.next() || !f.next() || f.next() || f.seq != 3 {
		t.Fatalf("mask 0b10 must invert only the second mapped boolean (seq %d)", f.seq)
	}
}

// A replay starts from the placement the first run started from: the groups
// the first run mutated must not leak into it.
func TestCloneGroupsIsIndependent(t *testing.T) {
	g := &group{answers: []answer{{Kind: "any", Pick: []string{"yes"}}}, kinds: []int{0}, targets: []string{"p0:A"}}
	c := cloneGroups([]*group{g})
	c[0].answers[0].Kind = "choose"
	c[0].targets[0] = "p1:B"
	c[0].kinds[0] = 3
	if g.answers[0].Kind != "any" || g.targets[0] != "p0:A" || g.kinds[0] != 0 {
		t.Fatalf("clone aliases the original: %+v", g)
	}
}

// XMage names a card where the ask labels it bare ("choose a card name"), and
// states only the card where gorge opens an optional with a Yes/No pair.
func TestRewriteOfferedLabel(t *testing.T) {
	names := []dumpOption{{0, "name", "Cranial Extraction", ""}, {1, "name", "Llanowar Elves", ""}}
	g := &group{answers: []answer{{Kind: "any", Pick: []string{"p0:Llanowar Elves"}}}, kinds: []int{0}}
	if !rewriteOfferedLabel(g, 0, "choose", names) || g.answers[0].Kind != "choose" || g.answers[0].Pick[0] != "Llanowar Elves" {
		t.Fatalf("ref against bare-name options: %+v", g.answers)
	}
	yn := []dumpOption{{0, "choose", "Yes", ""}, {1, "choose", "No", ""}}
	g = &group{answers: []answer{{Kind: "any", Pick: []string{"p0:Goblin Piker"}}}, kinds: []int{0}}
	if !rewriteOfferedLabel(g, 0, "choose", yn) || len(g.answers) != 2 || g.answers[0].Pick[0] != "Yes" || g.answers[1].Pick[0] != "p0:Goblin Piker" || len(g.kinds) != 2 {
		t.Fatalf("card pick against Yes/No: %+v kinds %v", g.answers, g.kinds)
	}
	g = &group{answers: []answer{{Kind: "any", Pick: []string{"yes"}}}, kinds: []int{0}}
	if rewriteOfferedLabel(g, 0, "choose", names) {
		t.Fatal("a boolean is not a ref")
	}
}

// answers before the first one gorge does not ask were consumed: only the
// unasked tail moves to the next step.
func TestSplitUnaskedTailKeepsConsumedAnswers(t *testing.T) {
	r := &result{}
	g := &group{answers: []answer{{Pick: []string{"a"}}, {Pick: []string{"b"}}, {Pick: []string{"c"}}}, kinds: []int{0, 0, 0}}
	if !splitUnaskedTail(r, g, 1, 2, 5) {
		t.Fatal("did not split")
	}
	if len(g.answers) != 1 || len(r.Groups) != 1 || len(r.Groups[0].answers) != 2 || r.Groups[0].at != 3 {
		t.Fatalf("g %+v, groups %+v", g.answers, r.Groups)
	}
	if splitUnaskedTail(r, g, 0, 2, 5) {
		t.Fatal("index 0 means nothing was consumed: the whole group moves")
	}
	r2 := &result{}
	g2 := &group{answers: []answer{{Pick: []string{"a"}}, {Pick: []string{"b"}}}, kinds: []int{0, 0}}
	splitUnaskedTail(r2, g2, 1, 4, 5)
	if r2.Dropped != 1 || len(r2.Groups) != 0 {
		t.Fatalf("last step: dropped %d groups %d", r2.Dropped, len(r2.Groups))
	}
}

// Starseer Mentor's "loses 3 life unless they sacrifice a nonland permanent or
// discard a card" is two modes asks in gorge (which cost, then pay or not)
// where XMage asks booleans (pay? then sacrifice?). Each case is the XMage
// method's booleans, played end to end.
func TestUnlessChainBooleansPlayAsTwoModesAnswers(t *testing.T) {
	const setup = `setStrictChooseMode(true);
        addCard(Zone.BATTLEFIELD, playerA, "Starseer Mentor");
        addCard(Zone.HAND, playerA, "Angel of Mercy");
        addCard(Zone.BATTLEFIELD, playerA, "Plains", 5);
        addCard(Zone.BATTLEFIELD, playerB, "Elite Vanguard");
        addCard(Zone.HAND, playerB, "Abandon Hope");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Angel of Mercy");
        addTarget(playerA, playerB);
`
	cases := []struct{ name, choices, asserts string }{
		{"decline", `setChoice(playerB, false);`, `assertLife(playerB, 17); assertHandCount(playerB, "Abandon Hope", 1);`},
		{"pay by discarding", `setChoice(playerB, true); setChoice(playerB, false); setChoice(playerB, "Abandon Hope");`,
			`assertLife(playerB, 20); assertGraveyardCount(playerB, "Abandon Hope", 1); assertPermanentCount(playerB, "Elite Vanguard", 1);`},
	}
	for _, c := range cases {
		res := translateTapped(t, setup+c.choices+"\n"+c.asserts)
		if res.Scenario == nil {
			t.Fatalf("%s: refused %s: %s", c.name, res.Code, res.Detail)
		}
		runResult(testutilRegistry(t), res)
		if res.Status != "pass" {
			t.Errorf("%s: status %s, fails %v", c.name, res.Status, res.Fails)
		}
	}
}

func testutilRegistry(t *testing.T) *cards.Registry {
	t.Helper()
	return testutil.CorpusRegistry(t)
}

// translateAsserting is translateTapped for a body with no assert of its own.
func translateAsserting(t *testing.T, body string) *result {
	t.Helper()
	return translateTapped(t, body+"\n        assertLife(playerA, 20);")
}

func castStep(t *testing.T, res *result) *step {
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

// XMage's pick of a MayPlay alternative cost is gorge's own cast option, and
// the step pays no mana (Demon of Fates' Design: pay life for an enchantment).
func TestAlternativeCostChoiceBecomesCastMode(t *testing.T) {
	st := castStep(t, translateAsserting(t, `addCard(Zone.BATTLEFIELD, playerA, "Demon of Fates' Design");
        addCard(Zone.HAND, playerA, "Anthem of Rakdos");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Anthem of Rakdos");
        setChoice(playerA, "Cast with alternative cost: Pay 3 life");`))
	if st.CastMode != "alternative" || st.Mana != "" {
		t.Fatalf("cast step %+v, want cast_mode alternative and no mana", st)
	}
}

// "Offspring?" declined is the plain cast; paying for offspring is a gorge
// cast option the translator does not select.
func TestOffspringChoice(t *testing.T) {
	const cast = `addCard(Zone.BATTLEFIELD, playerA, "Mountain", 5);
        addCard(Zone.HAND, playerA, "Coruscation Mage");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Coruscation Mage", true);
`
	res := translateAsserting(t, cast+`setChoice(playerA, "No");`)
	if res.Scenario == nil {
		t.Fatalf("declined offspring refused %s: %s", res.Code, res.Detail)
	}
	for _, g := range res.Groups {
		if len(g.answers) != 0 {
			t.Fatalf("the Offspring answer must be consumed by the cast, left %+v", g.answers)
		}
	}
	if res := translateAsserting(t, cast+`setChoice(playerA, "Yes");`); res.Code != "offspring-choice" {
		t.Fatalf("paid offspring: %q, want refused offspring-choice", res.Code)
	}
}

func TestOptionalCostPaidWithACardRefuses(t *testing.T) {
	res := translateAsserting(t, `addCard(Zone.HAND, playerA, "Redemptor Dreadnought");
        addCard(Zone.GRAVEYARD, playerA, "Warpath Ghoul");
        addCard(Zone.BATTLEFIELD, playerA, "Wastes", 5);
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Redemptor Dreadnought");
        setChoice(playerA, "Warpath Ghoul");`)
	if res.Code != "optional-cost-pick" {
		t.Fatalf("code %q (%s), want optional-cost-pick", res.Code, res.Detail)
	}
}

// XMage serves a spell's and its triggers' targets from one addTarget queue in
// the order it asks them; the runner places answers by step.
func TestTargetQueueOrderRefusals(t *testing.T) {
	const cast = `addCard(Zone.BATTLEFIELD, playerA, "Island", 3);
        addCard(Zone.BATTLEFIELD, playerB, "Grizzly Bears");
        addCard(Zone.HAND, playerA, "Ancestral Recall");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Ancestral Recall");
`
	cases := []struct {
		name, choices, want string
	}{
		{"two seats", `addTarget(playerA, playerB); addTarget(playerA, playerA); addTarget(playerB, playerA);`, "target-queue-order"},
		{"interleaved with a boolean", `addTarget(playerA, playerB); setChoice(playerA, true); addTarget(playerA, playerA);`, "target-queue-order"},
		{"one seat, back to back", `addTarget(playerA, playerB); addTarget(playerA, playerA);`, ""},
		{"one target", `addTarget(playerA, playerA);`, ""},
	}
	for _, c := range cases {
		if res := translateAsserting(t, cast+c.choices); res.Code != c.want {
			t.Errorf("%s: code %q (%s), want %q", c.name, res.Code, res.Detail, c.want)
		}
	}
}

func TestMultipleModeChoicesRefuse(t *testing.T) {
	res := translateAsserting(t, `addCard(Zone.BATTLEFIELD, playerA, "Swamp", 5);
        addCard(Zone.HAND, playerA, "Season of Loss");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Season of Loss");
        setModeChoice(playerA, "2");
        setModeChoice(playerA, "1");`)
	if res.Code != "multi-mode-choice" {
		t.Fatalf("code %q (%s), want multi-mode-choice", res.Code, res.Detail)
	}
}

func TestAlternativeCostActivationChoiceRefuses(t *testing.T) {
	res := translateAsserting(t, `addCard(Zone.BATTLEFIELD, playerA, "Bruenor Battlehammer");
        addCard(Zone.BATTLEFIELD, playerA, "Silvercoat Lion");
        addCard(Zone.BATTLEFIELD, playerA, "Gorgon Flail");
        setChoice(playerA, true);
        activateAbility(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Equip", "Silvercoat Lion");`)
	if res.Code != "alt-cost-activation-choice" {
		t.Fatalf("code %q (%s), want alt-cost-activation-choice", res.Code, res.Detail)
	}
}

// A seeded pool skips what paying with a source does: Thran Portal's life,
// Gilanra's "when you spend this mana", K'rrik's life-for-{B} choice.
func TestPoolPaymentRefusesSourcesThatActOnPayment(t *testing.T) {
	for _, c := range []struct{ card, code string }{
		{"Thran Portal", "mana-ability-cost"},
		{"Gilanra, Caller of Wirewood", "mana-payment"},
		{"K'rrik, Son of Yawgmoth", "pay-life-instead-of-mana"},
	} {
		res := translateAsserting(t, `addCard(Zone.BATTLEFIELD, playerA, "`+c.card+`");
        addCard(Zone.BATTLEFIELD, playerA, "Mountain");
        addCard(Zone.HAND, playerA, "Lightning Bolt");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Lightning Bolt", playerB);`)
		if res.Code != c.code {
			t.Errorf("%s: code %q (%s), want %q", c.card, res.Code, res.Detail, c.code)
		}
	}
}

// Two copies cast on different turns diverge: an assert on "the" copy cannot
// be bound to one ref. Identical copies (nothing cast on two turns) are fine.
func TestDuplicateNameAssertRefusesCopiesCastOnDifferentTurns(t *testing.T) {
	const setup = `addCard(Zone.HAND, playerA, "Memnite", 2);
        addCard(Zone.BATTLEFIELD, playerA, "Mountain", 4);
`
	res := translateBody(t, setup+`castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Memnite");
        castSpell(3, PhaseStep.PRECOMBAT_MAIN, playerA, "Memnite");
        setStopAt(3, PhaseStep.END_TURN);
        execute();
        assertPowerToughness(playerA, "Memnite", 1, 1);`)
	if res.Code != "duplicate-name-assert" {
		t.Fatalf("code %q (%s), want duplicate-name-assert", res.Code, res.Detail)
	}
	res = translateAsserting(t, setup+`castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Memnite");
        castSpell(1, PhaseStep.POSTCOMBAT_MAIN, playerA, "Memnite");
        assertPowerToughness(playerA, "Memnite", 1, 1);`)
	if res.Code == "duplicate-name-assert" {
		t.Fatal("copies cast on one turn are not ambiguous")
	}
}

// Only battlefield permanents have active statics: a K'rrik or Thran Portal
// merely in hand must not refuse a scenario that pays from the pool.
func TestPoolPaymentStaticRequiresBattlefield(t *testing.T) {
	const cast = `addCard(Zone.BATTLEFIELD, playerA, "Mountain");
        addCard(Zone.HAND, playerA, "%s");
        addCard(Zone.HAND, playerA, "Lightning Bolt");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Lightning Bolt", playerB);`
	for _, c := range []struct{ card, code string }{
		{"K'rrik, Son of Yawgmoth", "pay-life-instead-of-mana"},
		{"Thran Portal", "mana-ability-cost"},
	} {
		res := translateAsserting(t, fmt.Sprintf(cast, c.card))
		if res.Code != "" {
			t.Errorf("%s in hand: refused %q (%s), want a scenario", c.card, res.Code, res.Detail)
		}
		res = translateAsserting(t, `addCard(Zone.BATTLEFIELD, playerA, "`+c.card+`");
        addCard(Zone.BATTLEFIELD, playerA, "Mountain");
        addCard(Zone.HAND, playerA, "Lightning Bolt");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Lightning Bolt", playerB);`)
		if res.Code != c.code {
			t.Errorf("%s on battlefield: code %q (%s), want %q", c.card, res.Code, res.Detail, c.code)
		}
	}
	// a land played during the method is on the battlefield when the later
	// pool-paid cast runs, so its mana tax still applies
	res := translateAsserting(t, `addCard(Zone.BATTLEFIELD, playerA, "Mountain");
        addCard(Zone.HAND, playerA, "Thran Portal");
        addCard(Zone.HAND, playerA, "Lightning Bolt");
        playLand(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Thran Portal");
        castSpell(1, PhaseStep.POSTCOMBAT_MAIN, playerA, "Lightning Bolt", playerB);`)
	if res.Code != "mana-ability-cost" {
		t.Fatalf("Thran Portal played then paid from pool: code %q (%s), want mana-ability-cost", res.Code, res.Detail)
	}
}

// XMage's checkPlayableAbility counts Alena's {T}: Add {R}; seeding that mana
// into the pool would make the check pass without the ability ever producing
// it, so a check only a non-land mana source can pay is refused.
func TestOfferedCheckRefusesNonlandManaSource(t *testing.T) {
	res := translateAsserting(t, `addCard(Zone.BATTLEFIELD, playerA, "Alena, Kessig Trapper");
        addCard(Zone.BATTLEFIELD, playerA, "Island", 2);
        addCard(Zone.HAND, playerA, "Barbarian Horde");
        checkPlayableAbility("horde", 3, PhaseStep.PRECOMBAT_MAIN, playerA, "Cast Barbarian Horde", true);`)
	if res.Code != "offered-check-mana-source" {
		t.Fatalf("code %q (%s), want offered-check-mana-source", res.Code, res.Detail)
	}
	// the same check paid by lands alone still floats and stays a scenario
	res = translateAsserting(t, `addCard(Zone.BATTLEFIELD, playerA, "Mountain", 5);
        addCard(Zone.HAND, playerA, "Lightning Bolt");
        checkPlayableAbility("bolt", 1, PhaseStep.PRECOMBAT_MAIN, playerA, "Cast Lightning Bolt", true);`)
	if res.Code != "" {
		t.Fatalf("land-paid check refused %q (%s)", res.Code, res.Detail)
	}
}

// Anrakyr's DB$ Play casts an artifact for life; gorge's effect-play path has
// no kicked answer, so a multikicker count cannot be stated (the multikicker
// 1 and no-mana rows would otherwise be dropped choices, and cannot go stale).
func TestPlayEffectKickerRefuses(t *testing.T) {
	res := translateAsserting(t, `addCard(Zone.BATTLEFIELD, playerA, "Anrakyr the Traveller");
        addCard(Zone.HAND, playerA, "Everflowing Chalice");
        attack(1, playerA, "Anrakyr the Traveller", playerB);
        setChoice(playerA, true);
        setChoice(playerA, "Everflowing Chalice");
        setChoice(playerA, true);
        setChoice(playerA, true);
        setChoice(playerA, false);`)
	if res.Code != "play-effect-kicker" {
		t.Fatalf("code %q (%s), want play-effect-kicker", res.Code, res.Detail)
	}
}

// XMage's setChoice of a planeswalker after an attack says which planeswalker
// the effect's creature attacks; gorge never poses that defender choice, so
// the stated choice would be dropped and could not go stale.
func TestAttackPlaneswalkerChoiceRefuses(t *testing.T) {
	res := translateAsserting(t, `addCard(Zone.BATTLEFIELD, playerA, "Hans Eriksson");
        addCard(Zone.LIBRARY, playerA, "Grizzly Bears");
        addCard(Zone.BATTLEFIELD, playerB, "Chandra, Flame's Fury");
        attack(1, playerA, "Hans Eriksson", playerB);
        setChoice(playerA, "Chandra, Flame's Fury");`)
	if res.Code != "attack-planeswalker-choice" {
		t.Fatalf("code %q (%s), want attack-planeswalker-choice", res.Code, res.Detail)
	}
}

// The boolean-flip replay must be exercised end to end: Sigurd's "put another
// LORE counter" is the second option of a two-option ask, so the mask-0 run
// fails and runResult's replay is what makes the scenario pass.
func TestBooleanReplayEndToEnd(t *testing.T) {
	const body = `setStrictChooseMode(true);
        addCard(Zone.BATTLEFIELD, playerA, "Sigurd, Jarl of Ravensthorpe");
        addCard(Zone.BATTLEFIELD, playerA, "Bear Cub");
        addCard(Zone.HAND, playerA, "Urza's Saga");
        addCard(Zone.BATTLEFIELD, playerA, "Forest");
        playLand(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Urza's Saga");
        addTarget(playerA, "Bear Cub");
        setChoice(playerA, "I - {this} gains");
        waitStackResolved(1, PhaseStep.PRECOMBAT_MAIN);
        checkPermanentCounters("Saga entering counter", 1, PhaseStep.PRECOMBAT_MAIN, playerA, "Urza's Saga", CounterType.LORE, 1);
        checkPermanentCounters("Bear cub single counter", 1, PhaseStep.PRECOMBAT_MAIN, playerA, "Bear Cub", CounterType.P1P1, 1);
        attack(1, playerA, "Sigurd, Jarl of Ravensthorpe");
        activateAbility(1, PhaseStep.POSTCOMBAT_MAIN, playerA, "Boast &mdash; {1}: ", "Urza's Saga");
        setChoice(playerA, true);
        setChoice(playerA, "II - {this} gains");
        addTarget(playerA, "Bear Cub");
        waitStackResolved(1, PhaseStep.POSTCOMBAT_MAIN);
        checkPermanentCounters("Saga boast counter", 1, PhaseStep.POSTCOMBAT_MAIN, playerA, "Urza's Saga", CounterType.LORE, 2);
        checkPermanentCounters("Bear cub two counters", 1, PhaseStep.POSTCOMBAT_MAIN, playerA, "Bear Cub", CounterType.P1P1, 2);`
	src := `class SigurdReplayTest extends CardTestPlayerBase {
    @Test
    public void testSigurd() {
` + body + `
        setStopAt(1, PhaseStep.END_TURN);
        execute();
    }
}`
	jc, err := parseJava("x", "x", src)
	if err != nil {
		t.Fatal(err)
	}
	if len(jc.Methods) != 1 {
		t.Fatalf("parsed %d methods, want 1", len(jc.Methods))
	}
	reg := testutilRegistry(t)
	res := translate(reg, jc, &jc.Methods[0], "rules")
	if res.Scenario == nil {
		t.Fatalf("refused %s: %s", res.Code, res.Detail)
	}
	without := *res
	without.Groups = cloneGroups(res.Groups)
	runResultMask(reg, &without, 0)
	if without.Status != "fail-expect" {
		t.Fatalf("mask 0 status %s (%v), want fail-expect", without.Status, without.Fails)
	}
	runResult(reg, res)
	if res.Status != "pass" {
		t.Fatalf("replay status %s (%v), want pass", res.Status, res.Fails)
	}
}

// A land played during the method taxes only the pool-paid steps that run
// after it. Thran Portal played after the method's only pool-paid cast, then an
// unrelated no-mana activation, never takes part in a mana payment: refusing
// it would hide a scenario that proves nothing about mana.
func TestPoolPaymentLandPlayedAfterLastPaidStepIsFine(t *testing.T) {
	res := translateAsserting(t, `addCard(Zone.BATTLEFIELD, playerA, "Mountain");
        addCard(Zone.BATTLEFIELD, playerA, "Prodigal Sorcerer");
        addCard(Zone.HAND, playerA, "Thran Portal");
        addCard(Zone.HAND, playerA, "Lightning Bolt");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Lightning Bolt", playerB);
        playLand(1, PhaseStep.POSTCOMBAT_MAIN, playerA, "Thran Portal");
        activateAbility(1, PhaseStep.POSTCOMBAT_MAIN, playerA, "{T}: ", playerB);`)
	if res.Code == "mana-ability-cost" {
		t.Fatalf("refused %q (%s): the Portal enters after the only pool-paid step", res.Code, res.Detail)
	}
}

// XMage answers each additional Twinning Staff copy's "choose new targets"
// with its own addTarget. Gorge asks that only for copies it makes, and a
// copy that is not yet red (Fork) or a doubled replacement it does not apply
// (Zada twice) offers other targets; the runner's first-option fallback would
// then pick a different target, so the scenario could never go stale.
func TestCopyReplacementTargetChoicesRefuse(t *testing.T) {
	res := translateAsserting(t, `addCard(Zone.BATTLEFIELD, playerA, "Badlands", 3);
        addCard(Zone.BATTLEFIELD, playerA, "Twinning Staff");
        addCard(Zone.BATTLEFIELD, playerA, "Grizzly Bears");
        addCard(Zone.BATTLEFIELD, playerA, "Scalebane's Elite");
        addCard(Zone.HAND, playerA, "Disfigure");
        addCard(Zone.HAND, playerA, "Fork");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Disfigure", "Grizzly Bears");
        addTarget(playerA, "Scalebane's Elite", 2);
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Fork", "Disfigure", "Disfigure");`)
	if res.Code != "copy-target-choice" {
		t.Fatalf("code %q (%s), want copy-target-choice", res.Code, res.Detail)
	}
	// one new-target answer for one extra copy is asked by gorge and stays
	res = translateAsserting(t, `addCard(Zone.BATTLEFIELD, playerA, "Forest");
        addCard(Zone.BATTLEFIELD, playerA, "Twinning Staff");
        addCard(Zone.BATTLEFIELD, playerA, "Zada, Hedron Grinder");
        addCard(Zone.BATTLEFIELD, playerA, "Grizzly Bears");
        addCard(Zone.HAND, playerA, "Giant Growth");
        addTarget(playerA, "Grizzly Bears");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Giant Growth", "Zada, Hedron Grinder");`)
	if res.Code == "copy-target-choice" {
		t.Fatalf("a single new-target answer was refused: %s", res.Detail)
	}
}
