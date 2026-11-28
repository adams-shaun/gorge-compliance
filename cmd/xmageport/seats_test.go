package main

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/testkit/testutil"
)

func TestXMageBaseSeats(t *testing.T) {
	want := map[string]xmageBase{
		"CardTestPlayerBase":                   {Seats: 2, Order: []string{"playerA", "playerB"}},
		"":                                     {Seats: 2, Order: []string{"playerA", "playerB"}},
		"CardTestCommander4Players":            {Seats: 4, Order: []string{"playerA", "playerD", "playerC", "playerB"}},
		"CardTestMultiPlayerBaseWithRangeAll":  {Seats: 4, Order: []string{"playerA", "playerD", "playerC", "playerB"}},
		"CardTestMultiPlayerBase":              {Refuse: "range-one"},
		"MultiplayerTriggerTest":               {Refuse: "range-one"},
		"CardTestCommander3PlayersFFA":         {Refuse: "commander-deck"},
		"CardTestOathbreaker3PlayersFFA":       {Refuse: "commander-deck"},
		"CardTestCommanderDuelBase":            {Refuse: "commander-deck"},
		"CardTestCommander4PlayersWithAIHelps": {Refuse: "ai"},
	}
	if !reflect.DeepEqual(xmageBases, want) {
		t.Fatalf("xmageBases:\n got %+v\nwant %+v", xmageBases, want)
	}
	if _, ok := xmageBases["CardTestFooBase"]; ok {
		t.Fatal("CardTestFooBase is in the table")
	}
	method := func(base string) *result {
		src := `class FooTest extends ` + base + ` {
    @Test
    public void testOne() {
        addCard(Zone.HAND, playerA, "Shock");
        setStopAt(1, PhaseStep.END_TURN);
        execute();
        assertLife(playerB, 20);
    }
}`
		jc, err := parseJava("x", "x", src)
		if err != nil {
			t.Fatal(err)
		}
		// a refusal by base class is decided before any card is looked up
		return translate(nil, jc, &jc.Methods[0], "rules")
	}
	for base, code := range map[string]string{
		"CardTestFooBase":                      "base-class",
		"CardTestMultiPlayerBase":              "range-one",
		"CardTestCommanderDuelBase":            "commander-deck",
		"CardTestCommander3PlayersFFA":         "commander-deck",
		"CardTestCommander4PlayersWithAIHelps": "ai",
		"CardTestPlayerBaseWithAIHelps":        "ai",
		"CardTestFooMultiBase":                 "multiplayer",
	} {
		if res := method(base); res.Code != code {
			t.Errorf("extends %s: refusal %q (%s), want %q", base, res.Code, res.Detail, code)
		}
	}
}

func TestXMageSeatOfFourPlayers(t *testing.T) {
	ident := func(s string) value { return value{kind: vIdent, s: s} }
	four := &translator{base: xmageBases["CardTestCommander4Players"]}
	for name, want := range map[string]int{"playerA": 0, "playerD": 1, "playerC": 2, "playerB": 3} {
		if p, ok := four.seatOf(ident(name)); !ok || p != want {
			t.Errorf("four seats: seatOf(%s) = %d, %v; want %d", name, p, ok, want)
		}
	}
	if four.seats() != 4 {
		t.Errorf("four seats: seats() = %d", four.seats())
	}
	two := &translator{base: xmageBases["CardTestPlayerBase"]}
	for name, want := range map[string]int{"playerA": 0, "playerB": 1} {
		if p, ok := two.seatOf(ident(name)); !ok || p != want {
			t.Errorf("two seats: seatOf(%s) = %d, %v; want %d", name, p, ok, want)
		}
	}
	if _, ok := two.seatOf(ident("playerC")); ok {
		t.Error("two seats: seatOf(playerC) ok")
	}
	// a translator built without a base (tests, helpers) is the two-seat game
	if p, ok := (&translator{}).seatOf(ident("playerB")); !ok || p != 1 {
		t.Errorf("no base: seatOf(playerB) = %d, %v", p, ok)
	}
	if _, ok := (&translator{}).seatOf(ident("playerD")); ok {
		t.Error("no base: seatOf(playerD) ok")
	}
}

const fourPlayerShock = `class FooTest extends CardTestCommander4Players {
    @Test
    public void testShock() {
        addCard(Zone.HAND, playerA, "Shock");
        addCard(Zone.BATTLEFIELD, playerC, "Memnite");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Shock", playerC);
        setStopAt(1, PhaseStep.END_TURN);
        setStrictChooseMode(true);
        execute();
        assertLife(playerC, 18);
        assertLife(playerB, 20);
    }
}`

func TestTranslateFourPlayerMethod(t *testing.T) {
	jc, err := parseJava("x", "x", fourPlayerShock)
	if err != nil {
		t.Fatal(err)
	}
	res := translate(testutil.CorpusRegistry(t), jc, &jc.Methods[0], "rules")
	if res.Status != "generated" {
		t.Fatalf("status %s: %s %s", res.Status, res.Code, res.Detail)
	}
	sc := res.Scenario
	if sc.Players != 4 {
		t.Errorf("players %d, want 4", sc.Players)
	}
	if s := sc.Setup["p2"]; s == nil || !reflect.DeepEqual(s.Battlefield, []string{"Memnite"}) {
		t.Errorf("p2 setup %+v, want Memnite on the battlefield", s)
	}
	var cast *step
	for _, s := range sc.Steps {
		if s.Op == "cast" {
			cast = s
		}
	}
	if cast == nil || cast.Seat != 0 || !reflect.DeepEqual(cast.Targets, []string{"p2"}) {
		t.Errorf("cast step %+v, want p0 casting Shock at p2", cast)
	}
	life := map[string]int32{}
	for _, e := range sc.Expect {
		for k, v := range e.Life {
			life[k] = v
		}
	}
	if want := map[string]int32{"p2": 18, "p3": 20}; !reflect.DeepEqual(life, want) {
		t.Errorf("life expects %v, want %v", life, want)
	}
}

func TestTranslateFourPlayerSplitAttackRefused(t *testing.T) {
	src := `class FooTest extends CardTestCommander4Players {
    @Test
    public void testSplit() {
        addCard(Zone.BATTLEFIELD, playerA, "Memnite");
        addCard(Zone.BATTLEFIELD, playerA, "Ornithopter");
        attack(1, playerA, "Memnite", playerB);
        attack(1, playerA, "Ornithopter", playerC);
        setStopAt(1, PhaseStep.END_TURN);
        execute();
        assertLife(playerB, 19);
        assertLife(playerC, 20);
    }
}`
	jc, err := parseJava("x", "x", src)
	if err != nil {
		t.Fatal(err)
	}
	res := translate(testutil.CorpusRegistry(t), jc, &jc.Methods[0], "rules")
	if res.Code != "multi-defender" {
		t.Fatalf("refusal %q (%s), want multi-defender", res.Code, res.Detail)
	}
}

// The two-seat path keeps its bytes: no players field, and an attack with no
// defending player still goes at the other seat.
func TestTranslateTwoPlayerUnchanged(t *testing.T) {
	src := `class FooTest extends CardTestPlayerBase {
    @Test
    public void testAttack() {
        addCard(Zone.BATTLEFIELD, playerA, "Memnite");
        attack(1, playerA, "Memnite");
        setStopAt(1, PhaseStep.END_TURN);
        execute();
        assertLife(playerB, 19);
    }
}`
	jc, err := parseJava("x", "x", src)
	if err != nil {
		t.Fatal(err)
	}
	res := translate(testutil.CorpusRegistry(t), jc, &jc.Methods[0], "rules")
	if res.Status != "generated" {
		t.Fatalf("status %s: %s %s", res.Status, res.Code, res.Detail)
	}
	if res.Scenario.Players != 0 {
		t.Errorf("players %d, want 0 (absent)", res.Scenario.Players)
	}
	for _, s := range res.Scenario.Steps {
		if s.Op == "attack" && s.Defender != "p1" {
			t.Errorf("attack defender %q, want p1", s.Defender)
		}
	}
}
