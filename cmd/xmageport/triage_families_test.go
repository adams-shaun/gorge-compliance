package main

import (
	"strings"
	"testing"
)

// runBody translates body and plays it; the scenario must not be refused at
// translation.
func runBody(t *testing.T, body string) *result {
	t.Helper()
	res := translateBody(t, body)
	if res.Scenario == nil {
		t.Fatalf("refused %s: %s", res.Code, res.Detail)
	}
	runResult(testutilRegistry(t), res)
	return res
}

func wantStatus(t *testing.T, res *result, status, code string) {
	t.Helper()
	if res.Status != status || res.Code != code {
		t.Fatalf("status %s code %q (%v), want %s %q", res.Status, res.Code, res.Fails, status, code)
	}
}

// XMage's test player drops the attack() calls an attack restriction forbids
// (Crawlspace: "no more than two creatures can attack you each combat");
// gorge refuses the whole declaration and reverses it to the empty one. The
// runner drops the refused attacker the transcript names and plays again.
func TestRestrictedAttackerIsTrimmedNotReversed(t *testing.T) {
	res := runBody(t, `addCard(Zone.BATTLEFIELD, playerB, "Crawlspace");
        addCard(Zone.BATTLEFIELD, playerA, "Grizzly Bears");
        addCard(Zone.BATTLEFIELD, playerA, "Silvercoat Lion");
        addCard(Zone.BATTLEFIELD, playerA, "Elite Vanguard");
        attack(1, playerA, "Grizzly Bears", playerB);
        attack(1, playerA, "Silvercoat Lion", playerB);
        attack(1, playerA, "Elite Vanguard", playerB); // can't attack
        setStrictChooseMode(true);
        setStopAt(1, PhaseStep.END_COMBAT);
        execute();
        assertLife(playerB, 20 - 2 - 2);`)
	wantStatus(t, res, "pass", "")
	for _, st := range res.Scenario.Steps {
		if st.Op == "attack" && len(st.Attackers) != 2 {
			t.Fatalf("attackers %v, want the two the restriction allows", st.Attackers)
		}
	}
}

func TestTrimRestrictedAttackReadsTheRefusedOption(t *testing.T) {
	sc := &portScenario{Steps: []*step{{Op: "resolve"}, {Op: "attack", Attackers: []string{"p0:Grizzly Bears", "p0:Silvercoat Lion#2", "p0:Elite Vanguard"}}}}
	tr := []string{
		"step resolve ",
		"step attack ",
		`  [attack] refused, reversed to the empty declaration (CR 733.1): harness: submit attackers [0 1]: choices 0 and 2 are mutually exclusive (group "attack-restrict:1") (options [0 attacker "Attack with Grizzly Bears at b" obj=1 pl=1 att=0 mode=] [1 attacker "Attack with Silvercoat Lion at b" obj=2 pl=1 att=0 mode=] [2 attacker "Attack with Elite Vanguard at b" obj=3 pl=1 att=0 mode=] )`,
	}
	if !trimRestrictedAttack(sc, tr) {
		t.Fatal("nothing trimmed")
	}
	if got := strings.Join(sc.Steps[1].Attackers, ","); got != "p0:Grizzly Bears,p0:Silvercoat Lion#2" {
		t.Fatalf("attackers %s, want the later of the exclusive pair (Elite Vanguard) dropped", got)
	}
	if trimRestrictedAttack(sc, []string{"step resolve ", "step attack ", "  [attack] p0 attackers -> []"}) {
		t.Fatal("trimmed a declaration that was not refused")
	}
}

// A Role token is named "Cursed"; the runner counts every object whose name
// CONTAINS the asserted one, so the card "Cursed Courtier" counted as a
// second "Cursed". A name no card bears and a token script does is a token's.
func TestTokenOnlyNameCountsTokensOnly(t *testing.T) {
	res := runBody(t, `addCard(Zone.BATTLEFIELD, playerA, "Plains", 3);
        addCard(Zone.HAND, playerA, "Cursed Courtier");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Cursed Courtier");
        setStrictChooseMode(true);
        setStopAt(1, PhaseStep.END_TURN);
        execute();
        assertPermanentCount(playerA, "Cursed Courtier", 1);
        assertPermanentCount(playerA, "Cursed", 1);`)
	wantStatus(t, res, "pass", "")
	var tok, card bool
	for _, e := range res.Scenario.Expect {
		if e.Count != nil && e.Count.Name == "Cursed" && e.Count.Token != nil && *e.Count.Token {
			tok = true
		}
		if e.Count != nil && e.Count.Name == "Cursed Courtier" && e.Count.Token == nil {
			card = true
		}
	}
	if !tok || !card {
		t.Fatalf("token count %v, card count %v; want the token name restricted to tokens and the card name left alone", tok, card)
	}
}

// XMage states read ahead's chapter as "X=n"; gorge offers the chapters as
// readahead options labelled "Chapter I".."Chapter III".
func TestReadAheadChapterAnswerBecomesTheLabel(t *testing.T) {
	opts := []dumpOption{{Kind: "readahead", Label: "Chapter I"}, {Kind: "readahead", Label: "Chapter II"}, {Kind: "readahead", Label: "Chapter III"}}
	g := &group{answers: []answer{{Kind: "choose", Pick: []string{"3"}}}, kinds: []int{0}}
	if !rewriteOfferedLabel(g, 0, "choose", opts) || g.answers[0].Pick[0] != "Chapter III" {
		t.Fatalf("answers %+v, want Chapter III", g.answers)
	}
	g = &group{answers: []answer{{Kind: "choose", Pick: []string{"4"}}}, kinds: []int{0}}
	if rewriteOfferedLabel(g, 0, "choose", opts) {
		t.Fatal("chapter 4 of 3 was rewritten")
	}
	res := runBody(t, `addCard(Zone.HAND, playerA, "The Elder Dragon War");
        addCard(Zone.BATTLEFIELD, playerA, "Mountain", 4);
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "The Elder Dragon War");
        setChoice(playerA, "X=3");
        setStopAt(1, PhaseStep.END_TURN);
        setStrictChooseMode(true);
        execute();
        assertPermanentCount(playerA, "Dragon", 1);`)
	wantStatus(t, res, "pass", "")
}

// An optional trigger's cost ask is "Sacrifice 1 creature" / "Do not pay";
// XMage states only the creature, so the pay option precedes it.
func TestOptionalTriggerCostPickGetsThePayOption(t *testing.T) {
	opts := []dumpOption{{Kind: "trigger_cost_pay", Label: "Sacrifice 1 creature"}, {Kind: "trigger_cost_decline", Label: "Do not pay"}}
	g := &group{answers: []answer{{Kind: "choose", Pick: []string{"p0:Guardian Automaton"}}}, kinds: []int{0}}
	if !rewriteOfferedLabel(g, 0, "choose", opts) || len(g.answers) != 2 || g.answers[0].Pick[0] != "Sacrifice 1 creature" {
		t.Fatalf("answers %+v, want the pay option inserted first", g.answers)
	}
}

// assertType(name, type, null) states no subtype; it was read as the
// subtype "Null".
func TestAssertTypeWithNullSubtype(t *testing.T) {
	res := runBody(t, `addCard(Zone.BATTLEFIELD, playerA, "Crucible of Worlds");
        addCard(Zone.BATTLEFIELD, playerA, "Island", 4);
        addCard(Zone.HAND, playerA, "Animate Artifact", 1);
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Animate Artifact", "Crucible of Worlds");
        setStopAt(1, PhaseStep.BEGIN_COMBAT);
        execute();
        assertType("Crucible of Worlds", CardType.CREATURE, null);`)
	wantStatus(t, res, "pass", "")
	for _, e := range res.Scenario.Expect {
		for _, ty := range e.Types {
			if ty == "Null" {
				t.Fatalf("types %v carry the null argument", e.Types)
			}
		}
	}
}

// Gilded Drake's ETB asks for a copy election, then for the exchange target:
// the stated target waits for the election instead of answering it.
func TestCloneElectionIsNotAnsweredByTheNextTarget(t *testing.T) {
	res := runBody(t, `addCard(Zone.BATTLEFIELD, playerA, "Island", 2);
        addCard(Zone.HAND, playerA, "Gilded Drake", 1);
        addCard(Zone.BATTLEFIELD, playerA, "Pillarfield Ox", 1);
        addCard(Zone.HAND, playerB, "Clever Impersonator", 1);
        addCard(Zone.BATTLEFIELD, playerB, "Island", 4);
        addCard(Zone.BATTLEFIELD, playerB, "Silvercoat Lion", 1);
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Gilded Drake");
        addTarget(playerA, "Silvercoat Lion");
        castSpell(2, PhaseStep.PRECOMBAT_MAIN, playerB, "Clever Impersonator");
        setChoice(playerB, "Gilded Drake"); // copy the drake
        addTarget(playerB, "Pillarfield Ox"); // exchange control with Ox
        setStopAt(2, PhaseStep.BEGIN_COMBAT);
        execute();
        assertPermanentCount(playerA, "Gilded Drake", 1);
        assertPermanentCount(playerB, "Pillarfield Ox", 1);`)
	wantStatus(t, res, "pass", "")
}

// Wall of Roots pays for a cast with a -0/-1 counter; the runner pays from
// the pool, so no counter is ever put and its P/T cannot move.
func TestManaAbilityCounterShapeCoversPowerToughness(t *testing.T) {
	res := runBody(t, `addCard(Zone.HAND, playerA, "Runeclaw Bear");
        addCard(Zone.BATTLEFIELD, playerA, "Wall of Roots", 2);
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Runeclaw Bear");
        setStopAt(1, PhaseStep.BEGIN_COMBAT);
        execute();
        assertPowerToughness(playerA, "Wall of Roots", 0, 4);`)
	wantStatus(t, res, "refused", "mana-ability-counter")
}

// Hall of the Bandit Lord and Boseiju mark the mana they make: "If that mana
// is spent on ..." The pool-seeded cast carries no such mana.
func TestManaSpentRiderIsAManaSourceShape(t *testing.T) {
	res := runBody(t, `addCard(Zone.BATTLEFIELD, playerA, "Mountain", 2);
        addCard(Zone.BATTLEFIELD, playerA, "Hall of the Bandit Lord");
        addCard(Zone.HAND, playerA, "Goblin Roughrider");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Goblin Roughrider");
        setStrictChooseMode(true);
        setStopAt(1, PhaseStep.BEGIN_COMBAT);
        execute();
        assertAbility(playerA, "Goblin Roughrider", HasteAbility.getInstance(), true);`)
	wantStatus(t, res, "refused", "mana-spent-by-source-type")
}

func TestSetupEntryCounterReplacementIsAShape(t *testing.T) {
	res := runBody(t, `addCard(Zone.BATTLEFIELD, playerA, "Doubling Season");
        addCard(Zone.BATTLEFIELD, playerA, "Tibalt, the Fiend-Blooded");
        setStrictChooseMode(true);
        setStopAt(1, PhaseStep.PRECOMBAT_MAIN);
        execute();
        assertCounterCount("Tibalt, the Fiend-Blooded", CounterType.LOYALTY, 4);`)
	wantStatus(t, res, "refused", "setup-etb-counter-replacement")
}

func TestTokenCapIsAShape(t *testing.T) {
	res := translateBody(t, `addCard(Zone.BATTLEFIELD, playerA, "Anointed Procession", 4);
        addCard(Zone.BATTLEFIELD, playerA, "Plains", 34);
        addCard(Zone.HAND, playerA, "Secure the Wastes", 2);
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Secure the Wastes");
        setChoice(playerA, "X=16");
        castSpell(1, PhaseStep.POSTCOMBAT_MAIN, playerA, "Secure the Wastes");
        setChoice(playerA, "X=16");
        setStopAt(1, PhaseStep.END_TURN);
        execute();
        assertPermanentCount(playerA, "Warrior Token", 500);`)
	if res.Scenario == nil {
		t.Fatalf("refused %s: %s", res.Code, res.Detail)
	}
	if res.shape == nil || res.shape.Code != "xmage-token-cap" {
		t.Fatalf("shape %v, want xmage-token-cap", res.shape)
	}
}

func TestSearchOfAnUnstockedLibraryIsAShape(t *testing.T) {
	res := translateBody(t, `addCard(Zone.BATTLEFIELD, playerB, "Tunnel Ignus");
        addCard(Zone.HAND, playerA, "Scalding Tarn");
        playLand(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Scalding Tarn");
        activateAbility(1, PhaseStep.POSTCOMBAT_MAIN, playerA, "{T}, ");
        setStopAt(1, PhaseStep.END_TURN);
        execute();
        assertLife(playerA, 16);`)
	if res.shape == nil || res.shape.Code != "search-default-library" {
		t.Fatalf("shape %v, want search-default-library", res.shape)
	}
	stocked := translateBody(t, `addCard(Zone.BATTLEFIELD, playerB, "Tunnel Ignus");
        addCard(Zone.HAND, playerA, "Scalding Tarn");
        addCard(Zone.LIBRARY, playerA, "Mountain");
        playLand(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Scalding Tarn");
        activateAbility(1, PhaseStep.POSTCOMBAT_MAIN, playerA, "{T}, ");
        setStopAt(1, PhaseStep.END_TURN);
        execute();
        assertLife(playerA, 16);`)
	if stocked.shape != nil && stocked.shape.Code == "search-default-library" {
		t.Fatal("a stocked library was still refused")
	}
}

func TestBooleanShapes(t *testing.T) {
	res := translateBody(t, `addCard(Zone.BATTLEFIELD, playerA, "Syr Carah, the Bold", 1);
        addCard(Zone.BATTLEFIELD, playerA, "Illusionist's Bracers", 1);
        addCard(Zone.BATTLEFIELD, playerA, "Island", 3);
        activateAbility(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Equip {3}", "Syr Carah, the Bold");
        waitStackResolved(1, PhaseStep.PRECOMBAT_MAIN);
        activateAbility(1, PhaseStep.PRECOMBAT_MAIN, playerA, "{T}: {this} deals", playerB);
        setChoice(playerA, false); // no new target for copy
        setStopAt(1, PhaseStep.END_TURN);
        execute();
        assertLife(playerB, 18);`)
	if res.shape == nil || res.shape.Code != "new-target-copy-boolean" {
		t.Fatalf("shape %v, want new-target-copy-boolean", res.shape)
	}
	res = translateBody(t, `addCard(Zone.BATTLEFIELD, playerA, "Battletide Alchemist");
        addCard(Zone.HAND, playerA, "Boltwave");
        addCard(Zone.BATTLEFIELD, playerA, "Mystic Monastery", 2);
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Boltwave");
        setChoice(playerA, true);
        setStopAt(1, PhaseStep.BEGIN_COMBAT);
        execute();
        assertLife(playerB, 18);`)
	if res.shape == nil || res.shape.Code != "optional-prevention-boolean" {
		t.Fatalf("shape %v, want optional-prevention-boolean", res.shape)
	}
}

// An Aura moved by a trigger gorge poses no ask for (one candidate) leaves
// the stated creature pick to the next decision that takes it, here the
// extra combat's attack declaration.
func TestChoiceTakenByADeclarationIsAShape(t *testing.T) {
	r := &result{Status: "fail-expect", Fails: []string{"p1 life 14, want 16"}}
	if declarationAnswer([]string{"  [answer] p0 choose -> [\"Pillarfield Ox\"]"}) {
		t.Fatal("a choose answer is not a declaration")
	}
	if !declarationAnswer([]string{"step pass_to ", `  [answer] p0 attackers -> ["Attack with Pillarfield Ox at b"]`}) {
		t.Fatal("an attackers answer was not found")
	}
	r.runShape = refuse("choice-consumed-by-declaration", "x")
	if !shapeRefusalApplies(r) {
		t.Fatal("a plain fail-expect was not refused")
	}
}
