package main

import (
	"reflect"
	"slices"
	"testing"
)

// readMethod parses one @Test body (no addCard, so no corpus is needed)
// and runs the translator's statement reader over it.
func readMethod(t *testing.T, body string) (*translator, *refusal) {
	t.Helper()
	jc, err := parseJava("x", "x", "class FooTest extends CardTestPlayerBase {\n    @Test\n    public void testOne() {\n"+body+"\n    }\n}")
	if err != nil {
		t.Fatal(err)
	}
	if len(jc.Methods) != 1 {
		t.Fatalf("parsed %d methods, want 1", len(jc.Methods))
	}
	tr := &translator{jc: jc, m: &jc.Methods[0], consts: map[string]value{}, allNames: map[string]bool{},
		copied: map[string]bool{}, copyRef: map[string]string{}}
	return tr, tr.read()
}

func TestReadRefusesSecondExecute(t *testing.T) {
	_, r := readMethod(t, `setStopAt(1, PhaseStep.END_TURN);
        execute();
        assertLife(playerB, 20);
        setStopAt(2, PhaseStep.END_TURN);
        execute();
        assertLife(playerB, 18);`)
	if r == nil || r.Code != "multi-execute" {
		t.Fatalf("two execute() calls: refusal %v, want multi-execute", r)
	}
	if _, r := readMethod(t, `setStopAt(1, PhaseStep.END_TURN);
        execute();
        assertLife(playerB, 20);`); r != nil {
		t.Fatalf("one execute(): refused %v", r)
	}
}

func TestReadRefusesCastWaitingForATrigger(t *testing.T) {
	_, r := readMethod(t, `castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerB, "Lightning Bolt", "Silvercoat Lion", "When {this} enters");
        setStopAt(1, PhaseStep.END_TURN);
        execute();`)
	if r == nil || r.Code != "stack-clause" {
		t.Fatalf("castSpell waiting for a trigger: refusal %v, want stack-clause", r)
	}
	// waiting for a SPELL is the generated order already: no refusal
	tr, r := readMethod(t, `castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerB, "Lightning Bolt", "Silvercoat Lion", "Giant Growth");
        setStopAt(1, PhaseStep.END_TURN);
        execute();`)
	if r != nil || len(tr.actions) != 1 || !reflect.DeepEqual(tr.actions[0].targets, []string{"Silvercoat Lion"}) {
		t.Fatalf("castSpell waiting for a spell: refusal %v, actions %+v", r, tr.actions)
	}
}

func TestReadKeepsChoicesOfAMethodWithNoAction(t *testing.T) {
	tr, r := readMethod(t, `setStrictChooseMode(true);
        setChoice(playerA, "Guardian Automaton");
        setStopAt(2, PhaseStep.PRECOMBAT_MAIN);
        execute();`)
	if r != nil {
		t.Fatal(r)
	}
	if len(tr.actions) != 0 {
		t.Fatalf("precondition: the method has actions %+v", tr.actions)
	}
	if len(tr.tailChoices) != 1 || tr.tailChoices[0].pick != "name:Guardian Automaton" {
		t.Fatalf("tailChoices = %+v, want the one setChoice", tr.tailChoices)
	}
	if !tr.strict {
		t.Fatal("setStrictChooseMode(true) not recorded")
	}
}

func TestTriggerOrderChoiceRecognisedWithoutTrailingText(t *testing.T) {
	for _, s := range []string{"Whenever", "Whenever ", "When {this} dies", "At the beginning of"} {
		if !isTriggerOrderText(s) {
			t.Errorf("isTriggerOrderText(%q) = false", s)
		}
	}
	if isTriggerOrderText("Whenever-Bound Lion") || isTriggerOrderText("Wheneverland") {
		t.Error("a card name starting with Whenever is not a trigger-order choice")
	}
}

func TestManaFromCostRefusesPhyrexianAndSkipsNonManaTokens(t *testing.T) {
	if _, r := manaFromCost("1 U/P"); r == nil || r.Code != "phyrexian-mana" {
		t.Fatalf("U/P: refusal %v, want phyrexian-mana", r)
	}
	if _, r := manaFromCost("UP"); r == nil || r.Code != "phyrexian-mana" {
		t.Fatalf("UP: refusal %v, want phyrexian-mana", r)
	}
	for cost, want := range map[string]string{"1 GW GW": "CGG", "2 W/U": "CCW", "Reveal<X/Card.White> W": "W", "RG": "R"} {
		got, r := manaFromCost(cost)
		if r != nil || got != want {
			t.Errorf("manaFromCost(%q) = %q, %v; want %q", cost, got, r, want)
		}
	}
}

func TestCountExpectRefusesLibrarySizedSeatCount(t *testing.T) {
	if _, r := countExpect(1, "exile", "", 71); r == nil || r.Code != "library-size" {
		t.Fatalf("seat-wide exile count 71: refusal %v, want library-size", r)
	}
	if _, r := countExpect(1, "battlefield", "", 737); r != nil {
		t.Fatalf("battlefield count does not depend on the library: refused %v", r)
	}
	if _, r := countExpect(1, "exile", "Lightning Bolt", 41); r != nil {
		t.Fatalf("named count: refused %v", r)
	}
}

func TestPTChoiceText(t *testing.T) {
	if got := rePTChoice.FindString("a 3/3 artifact creature"); got != "3/3" {
		t.Fatalf("got %q", got)
	}
	if got := rePTChoice.FindString("Craw Wurm"); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestMergePicksFoldsConsecutiveChoices(t *testing.T) {
	g := &group{answers: []answer{
		{Kind: "any", Pick: []string{"yes"}},
		{Kind: "any", Pick: []string{"p0:Grizzly Bears"}},
		{Kind: "any", Pick: []string{"p0:Elite Vanguard"}},
	}, kinds: []int{0, 0, 0}}
	if !mergePicks(g, 2) {
		t.Fatal("did not merge")
	}
	want := []answer{{Kind: "any", Pick: []string{"yes"}}, {Kind: "any", Pick: []string{"p0:Grizzly Bears", "p0:Elite Vanguard"}}}
	if !reflect.DeepEqual(g.answers, want) || len(g.kinds) != 2 {
		t.Fatalf("answers %+v kinds %v", g.answers, g.kinds)
	}
	if mergePicks(&group{answers: []answer{{Kind: "any", Pick: []string{"p0:A"}}}, kinds: []int{0}}, 2) {
		t.Fatal("merged with too few answers")
	}
}

func TestSkipEmptyDeclaration(t *testing.T) {
	s := &step{Op: "block", Seat: 1}
	if !skipEmptyDeclaration(s, "step 3 (block): harness: block: no blockers decision pending") {
		t.Fatal("empty block not rewritten")
	}
	if s.Op != "pass_to" || s.Step != "declare-blockers" || s.Active != "p0" {
		t.Fatalf("got %+v", s)
	}
	a := &step{Op: "attack", Seat: 0, Defender: "p1"}
	if !skipEmptyDeclaration(a, "harness: attack: passed declare-attackers without being asked") || a.Op != "pass_to" || a.Active != "p0" {
		t.Fatalf("empty attack: %+v", a)
	}
	b := &step{Op: "block", Seat: 1, Blocks: [][2]string{{"p1:Bear", "p0:Lion"}}}
	if skipEmptyDeclaration(b, "harness: block: no blockers decision pending") {
		t.Fatal("a stated block is never dropped")
	}
}

func TestAIChoiceFallback(t *testing.T) {
	if k := aiChoiceFallback([]string{"  [pass_to fallback] p0 attackers -> []", "  [pass_to fallback] p1 blockers -> []"}); k != "" {
		t.Fatalf("declarations are not AI choices: %q", k)
	}
	if k := aiChoiceFallback([]string{"  [pass_to fallback] p0 attackers -> []", "  [resolve fallback] p1 target -> [\"a\"]"}); k != "target" {
		t.Fatalf("got %q, want target", k)
	}
}

// A merged multi-pick answer stays a wildcard so the kind retry can hand it
// to a modes ask (Ancient Excavation's "discard two" is posed as modes).
func TestMergedPickRetriesKinds(t *testing.T) {
	g := &group{answers: []answer{
		{Kind: "any", Pick: []string{"p0:Shock"}},
		{Kind: "any", Pick: []string{"p0:Healing Salve"}},
	}, kinds: []int{0, 0}}
	if !mergePicks(g, 2) {
		t.Fatal("did not merge")
	}
	ks := answerKinds(g.answers[0])
	if !slices.Contains(ks, "modes") {
		t.Fatalf("merged answer kinds %v lack modes", ks)
	}
}

// Anrakyr's tests answer "use the ability" and "pay life instead of mana" with
// two setChoice(true); gorge never asks the second. The unconsumed answer is
// the LATER yes (the runner consumes a group in order): moving the first one
// instead put the later name picks in front of the trigger's own ask.
func TestTakeLeftoversMovesTheLaterDuplicatePick(t *testing.T) {
	yes := answer{Kind: "any", Pick: []string{"yes"}}
	g := &group{answers: []answer{yes, {Kind: "any", Pick: []string{"p0:Cryptek"}}, yes, {Kind: "any", Pick: []string{"p0:Lesser Masticore"}}},
		kinds: []int{0, 0, 0, 0}}
	left := takeLeftovers(g, `step 1 (pass_to): unconsumed answer(s) for this step: [{"kind":"any","pick":["yes"],"seat":0}]`)
	if left == nil || len(left.answers) != 1 || left.answers[0].Pick[0] != "yes" {
		t.Fatalf("left = %+v", left)
	}
	var picks []string
	for _, a := range g.answers {
		picks = append(picks, a.Pick[0])
	}
	if want := []string{"yes", "p0:Cryptek", "p0:Lesser Masticore"}; !reflect.DeepEqual(picks, want) || len(g.kinds) != 3 {
		t.Fatalf("group keeps %v (kinds %v), want %v", picks, g.kinds, want)
	}
}

// A checkpoint assert is evaluated where the XMage method states it. The
// active-seat ordering that puts simultaneous casts in priority order must
// not move the active seat's check ahead of the other seat's later cast:
// ThousandYearStorm's end-of-turn checkLife ran on the second Bolt's cast
// step and saw 20 life where three cast Bolts had not yet resolved.
func TestCheckpointAssertsFollowSourceOrder(t *testing.T) {
	res := translateBody(t, `addCard(Zone.BATTLEFIELD, playerB, "Mountain", 5);
        addCard(Zone.HAND, playerB, "Lightning Bolt", 3);
        addCard(Zone.BATTLEFIELD, playerA, "Mountain");
        addCard(Zone.HAND, playerA, "Lightning Bolt");
        castSpell(2, PhaseStep.END_TURN, playerB, "Lightning Bolt", playerA);
        castSpell(2, PhaseStep.END_TURN, playerB, "Lightning Bolt", playerA);
        castSpell(2, PhaseStep.END_TURN, playerA, "Lightning Bolt", playerB);
        waitStackResolved(2, PhaseStep.END_TURN);
        checkLife("A must get damage", 2, PhaseStep.END_TURN, playerA, 20 - 3 - 3);
        checkLife("B must get damage", 2, PhaseStep.END_TURN, playerB, 20 - 3);
        setStopAt(4, PhaseStep.END_TURN);
        execute();`)
	if res.Scenario == nil {
		t.Fatalf("refused %s: %s", res.Code, res.Detail)
	}
	castP0 := -1
	for i, st := range res.Scenario.Steps {
		if st.Op == "cast" && st.Card == "p0:Lightning Bolt" {
			castP0 = i
		}
	}
	if castP0 < 0 {
		t.Fatal("no p0 Lightning Bolt cast step")
	}
	for i, st := range res.Scenario.Steps {
		for _, x := range st.Expect {
			if x.Life != nil && x.Life["p1"] == 17 && i <= castP0 {
				t.Fatalf("check p1=17 rides step %d, at or before p0's cast step %d", i, castP0)
			}
		}
	}
}

// XMage's addTarget queue also states card choices (a library search), which
// gorge poses as a choose ask. An unused step target must be re-homed as that
// ask's answer instead of being moved to a later step where no target ask
// consumes it. Alpine Houndmaster's "you may search for Alpine Watchdog and/or
// Igneous Cur" is exactly that shape.
func TestUnusedTargetAnswersChooseAsk(t *testing.T) {
	res := translateBody(t, `addCard(Zone.HAND, playerA, "Alpine Houndmaster");
        addCard(Zone.BATTLEFIELD, playerA, "Mountain");
        addCard(Zone.BATTLEFIELD, playerA, "Plains");
        addCard(Zone.LIBRARY, playerA, "Alpine Watchdog");
        addCard(Zone.LIBRARY, playerA, "Igneous Cur");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Alpine Houndmaster");
        setChoice(playerA, true);
        addTarget(playerA, "Igneous Cur");
        addTarget(playerA, "Alpine Watchdog");
        setStrictChooseMode(true);
        setStopAt(1, PhaseStep.POSTCOMBAT_MAIN);
        execute();
        assertHandCount(playerA, 2);`)
	if res.Scenario == nil {
		t.Fatalf("refused %s: %s", res.Code, res.Detail)
	}
	runResult(testutilRegistry(t), res)
	if res.Status != "pass" {
		t.Fatalf("status %s (%v), want pass", res.Status, res.Fails)
	}
}

// gorge asks Juxtapose's tied-permanent choices in a different seat order
// than XMage states them (the defender first), so the pick for the asked
// seat is not the first answer in the group. The runner must reorder to the
// asked seat instead of reporting the first pick as not offered.
func TestRepeatedSeatChoicesReorderToTheAsk(t *testing.T) {
	g := &group{answers: []answer{
		{Kind: "any", Pick: []string{"p0:Watchwolf"}, Seat: ip(0)},
		{Kind: "any", Pick: []string{"p1:Gray Ogre"}, Seat: ip(1)},
		{Kind: "any", Pick: []string{"p0:Millstone"}, Seat: ip(0)},
		{Kind: "any", Pick: []string{"p1:Jayemdae Tome"}, Seat: ip(1)},
	}, kinds: []int{0, 0, 0, 0}}
	if !reorderAnswerForSeat(g, 0, 1) || g.answers[0].Pick[0] != "p1:Gray Ogre" || g.answers[1].Pick[0] != "p0:Watchwolf" {
		t.Fatalf("reorder = %+v", g.answers)
	}
	line := `step 2 (pass_to): harness: answer "p0:Watchwolf" not offered for choose: [0 card "" obj=41 pl=1 att=0 mode=] [1 card "" obj=42 pl=1 att=0 mode=]`
	if s, ok := optionSeat(line); !ok || s != 1 {
		t.Fatalf("optionSeat = %d, %v, want 1", s, ok)
	}
}

func TestJuxtaposeSeatOrderPlays(t *testing.T) {
	res := translateBody(t, `addCard(Zone.HAND, playerA, "Juxtapose");
        addCard(Zone.BATTLEFIELD, playerA, "Island", 4);
        addCard(Zone.BATTLEFIELD, playerA, "Grizzly Bears");
        addCard(Zone.BATTLEFIELD, playerA, "Watchwolf");
        addCard(Zone.BATTLEFIELD, playerA, "Millstone");
        addCard(Zone.BATTLEFIELD, playerA, "Ankh of Mishra");
        addCard(Zone.BATTLEFIELD, playerB, "Gray Ogre");
        addCard(Zone.BATTLEFIELD, playerB, "Centaur Courser");
        addCard(Zone.BATTLEFIELD, playerB, "Llanowar Elves");
        addCard(Zone.BATTLEFIELD, playerB, "Icy Manipulator");
        addCard(Zone.BATTLEFIELD, playerB, "Jayemdae Tome");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Juxtapose", playerB);
        setChoice(playerA, "Watchwolf");
        setChoice(playerB, "Gray Ogre");
        setChoice(playerA, "Millstone");
        setChoice(playerB, "Jayemdae Tome");
        setStrictChooseMode(true);
        setStopAt(1, PhaseStep.END_TURN);
        execute();
        assertPermanentCount(playerA, "Gray Ogre", 1);
        assertPermanentCount(playerB, "Watchwolf", 1);
        assertPermanentCount(playerA, "Jayemdae Tome", 1);
        assertPermanentCount(playerB, "Millstone", 1);
        assertPermanentCount(playerA, "Grizzly Bears", 1);
        assertPermanentCount(playerA, "Ankh of Mishra", 1);
        assertPermanentCount(playerB, "Centaur Courser", 1);
        assertPermanentCount(playerB, "Llanowar Elves", 1);
        assertPermanentCount(playerB, "Icy Manipulator", 1);`)
	if res.Scenario == nil {
		t.Fatalf("refused %s: %s", res.Code, res.Detail)
	}
	runResult(testutilRegistry(t), res)
	if res.Status != "pass" {
		t.Fatalf("status %s (%v), want pass", res.Status, res.Fails)
	}
}

// Djinn Illuminatus grants replicate to the seat's instants and sorceries.
// XMage answers the replicate election with a boolean (yes, replicate once;
// no, only once), but gorge poses the count ask (0..N), so the booleans are
// never consumed and the spell is cast once. The shape is refused rather
// than shipped with the interaction unplayed.
func TestReplicateBooleanChoiceRefuses(t *testing.T) {
	res := translateBody(t, `addCard(Zone.BATTLEFIELD, playerA, "Djinn Illuminatus");
        addCard(Zone.BATTLEFIELD, playerA, "Mountain", 2);
        addCard(Zone.HAND, playerA, "Lightning Bolt");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Lightning Bolt", playerB);
        setChoice(playerA, "Yes");
        setChoice(playerA, "No");
        setChoice(playerA, "No");
        setStopAt(1, PhaseStep.PRECOMBAT_MAIN);
        execute();
        assertLife(playerB, 14);`)
	if res.Code != "replicate-choice" {
		t.Fatalf("code %q (%s), want replicate-choice", res.Code, res.Detail)
	}
}

// Garnet states the same Saga twice ("remove a counter from each of any
// number"); the first card pick is consumed by a multi-pick choose and the
// second is left over. Folding the leftover into the consumed answer is what
// makes gorge remove both lore counters, so it must accept abstract as well
// as converted addTarget picks.
func TestRepeatedCardPickMergesIntoOneAnswer(t *testing.T) {
	g := &group{answers: []answer{
		{Kind: "any", Pick: []string{"p0:Summon: Choco/Mog"}, Seat: ip(0)},
		{Kind: "any", Pick: []string{"p0:Summon: Choco/Mog#2"}, Seat: ip(0)},
	}, kinds: []int{0, 0}}
	line := `step 8 (pass_to): unconsumed answer(s) for this step: [{"kind":"any","pick":["p0:Summon: Choco/Mog#2"],"seat":0}]`
	if !mergeUnconsumedCardPick(g, line) {
		t.Fatal("did not merge the repeated card pick")
	}
	if len(g.answers) != 1 || len(g.answers[0].Pick) != 2 || g.answers[0].Kind != "choose" || len(g.kinds) != 1 {
		t.Fatalf("answers %+v kinds %v", g.answers, g.kinds)
	}
}

// Bolg's Company's "{T}, Sacrifice another Goblin: Add {B}{R}" is a mana
// ability with a non-tap cost. Paying the spell from the pool never
// activates it, so the stated Goblin sacrifice would be silently dropped:
// the shape is refused with the mana-ability-cost code.
func TestSacrificingManaAbilityRefuses(t *testing.T) {
	res := translateAsserting(t, `addCard(Zone.BATTLEFIELD, playerA, "Bolg's Company");
        addCard(Zone.BATTLEFIELD, playerA, "Goblin Piker");
        addCard(Zone.BATTLEFIELD, playerB, "Grizzly Bears");
        addCard(Zone.HAND, playerA, "Terminate");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Terminate", "Grizzly Bears");
        setChoice(playerA, "Goblin Piker");`)
	if res.Code != "mana-ability-cost" {
		t.Fatalf("code %q (%s), want mana-ability-cost", res.Code, res.Detail)
	}
}

// "Each player sacrifices a creature" poses one choose per seat; the two
// picks are separate answers, not two picks of one multi-pick ask.
func TestCrossSeatCardPicksDoNotMerge(t *testing.T) {
	g := &group{answers: []answer{
		{Kind: "any", Pick: []string{"p0:Ornithopter"}, Seat: ip(0)},
		{Kind: "any", Pick: []string{"p1:Grizzly Bears"}, Seat: ip(1)},
	}, kinds: []int{0, 0}}
	line := `step 2 (pass_to): unconsumed answer(s) for this step: [{"kind":"any","pick":["p1:Grizzly Bears"],"seat":1}]`
	if mergeUnconsumedCardPick(g, line) {
		t.Fatalf("cross-seat picks merged into %+v", g.answers)
	}
}
