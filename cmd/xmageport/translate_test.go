package main

import (
	"reflect"
	"testing"
)

func TestXChoice(t *testing.T) {
	for in, want := range map[string]int{"X=0": 0, "X=3": 3, "X=12": 12} {
		if n, ok := xChoice(in); !ok || n != want {
			t.Errorf("xChoice(%q) = %d, %v; want %d", in, n, ok, want)
		}
	}
	for _, in := range []string{"X", "X=", "X=a", "Y=1", "X=1^X=2", "X=10000"} {
		if _, ok := xChoice(in); ok {
			t.Errorf("xChoice(%q) accepted", in)
		}
	}
}

func TestManaFromCostX(t *testing.T) {
	cases := []struct {
		cost string
		x    int
		want string
		ref  bool
	}{
		{"X R", 3, "CCCR", false},
		{"X X", 2, "CCCC", false},
		{"X 2 G U", 0, "CCGU", false},
		{"X R", -1, "", true},
		{"XMin1 X", 1, "C", false},
	}
	for _, c := range cases {
		got, r := manaFromCostX(c.cost, c.x)
		if (r != nil) != c.ref || got != c.want {
			t.Errorf("manaFromCostX(%q, %d) = %q, %v; want %q refuse=%v", c.cost, c.x, got, r, c.want, c.ref)
		}
	}
}

func TestEvalExprPrecedence(t *testing.T) {
	for in, want := range map[string]int{"20 - 3 * 2": 14, "2 * 3 + 1": 7, "20 - 1 + 2": 21, "5 - 1": 4, "2 * 2 * 3 - 1": 11} {
		v := evalExpr(in, nil)
		if v.kind != vInt || v.n != want {
			t.Errorf("evalExpr(%q) = %v; want %d", in, v, want)
		}
	}
}

func TestInlineHelpers(t *testing.T) {
	src := `class FooTest extends CardTestPlayerBase {
    private static final String bolt = "Lightning Bolt";
    private void testBase(String card, int life) {
        addCard(Zone.HAND, playerA, card);
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, card, playerB);
        setStopAt(1, PhaseStep.END_TURN);
        execute();
        assertLife(playerB, life);
    }
    private void testChoices(String card, String... choices) {
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, card);
        for (String choice : choices) {
            setChoice(playerA, choice);
        }
    }
    @Test
    public void testOne() {
        testBase(bolt, 20 - 3);
        testChoices("Opt", "a", "b");
    }
}`
	jc, err := parseJava("x", "x", src)
	if err != nil {
		t.Fatal(err)
	}
	got := inlineHelpers(jc.Methods[0].Stmts, jc.Defs, 0)
	want := []string{
		"addCard(Zone.HAND, playerA, (bolt))",
		"castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, (bolt), playerB)",
		"setStopAt(1, PhaseStep.END_TURN)",
		"execute()",
		"assertLife(playerB, (20 - 3))",
		`castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, ("Opt"))`,
		`setChoice(playerA, ("a"))`,
		`setChoice(playerA, ("b"))`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("inlineHelpers:\n got %q\nwant %q", got, want)
	}
	c, ok := parseCall(got[4], jc.Consts)
	if !ok || c.Args[1].kind != vInt || c.Args[1].n != 17 {
		t.Fatalf("inlined arg did not evaluate: %+v", c)
	}
}

func TestSubstIdentsSkipsStringsAndFields(t *testing.T) {
	got := substIdents(`f(card, "card", x.card, card2)`, map[string]string{"card": "(y)"})
	if want := `f((y), "card", x.card, card2)`; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestManaFromCostLandsPaysFromLandColours(t *testing.T) {
	cases := []struct {
		cost, lands, want string
	}{
		{"1 WB", "WB", "BW"},      // hybrid takes the Plains' W, generic the Swamp's B
		{"1 WB", "BB", "BB"},      // no W land: hybrid falls to B
		{"2 B B", "GGBB", "GGBB"}, // coloured symbols reserve their lands first
		{"3 G", "", "CCCG"},       // no lands: colourless generic, as manaFromCostX
		{"1 WB", "", "CW"},        // no lands: first hybrid colour
		{"X R", "RR", "RR"},       // X counts as generic (x=1 below)
	}
	for _, c := range cases {
		x := -1
		if c.cost == "X R" {
			x = 1
		}
		got, r := manaFromCostLands(c.cost, x, c.lands)
		if r != nil || got != c.want {
			t.Errorf("manaFromCostLands(%q, %d, %q) = %q, %v; want %q", c.cost, x, c.lands, got, r, c.want)
		}
	}
}
