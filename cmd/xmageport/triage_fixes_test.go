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
