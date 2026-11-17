package main

import (
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
