package main

import (
	"testing"

	"github.com/adams-shaun/gorge/testkit/testutil"
)

// readWithCorpus is readMethod with the corpus registry, for bodies that
// addCard.
func readWithCorpus(t *testing.T, body string) (*translator, *refusal) {
	t.Helper()
	jc, err := parseJava("x", "x", "class FooTest extends CardTestPlayerBase {\n    @Test\n    public void testOne() {\n"+body+"\n    }\n}")
	if err != nil {
		t.Fatal(err)
	}
	if len(jc.Methods) != 1 {
		t.Fatalf("parsed %d methods, want 1", len(jc.Methods))
	}
	tr := &translator{reg: testutil.CorpusRegistry(t), jc: jc, m: &jc.Methods[0], consts: map[string]value{},
		allNames: map[string]bool{}, copied: map[string]bool{}, copyRef: map[string]string{}}
	return tr, tr.read()
}

func TestRemoveAllCardsTranslates(t *testing.T) {
	// the runner deals no opening hand: clearing the hand is a no-op
	if _, r := readWithCorpus(t, `removeAllCardsFromHand(playerA);
        addCard(Zone.HAND, playerA, "Grizzly Bears");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Grizzly Bears");
        setStopAt(1, PhaseStep.BEGIN_COMBAT);
        execute();
        assertPermanentCount(playerA, "Grizzly Bears", 1);`); r != nil {
		t.Fatalf("removeAllCardsFromHand before the hand is built: refused %v", r)
	}
	// clearing the hand AFTER it was built would drop setup cards
	if _, r := readWithCorpus(t, `addCard(Zone.HAND, playerA, "Grizzly Bears");
        removeAllCardsFromHand(playerA);
        setStopAt(1, PhaseStep.BEGIN_COMBAT);
        execute();`); r == nil || r.Code != "library-manipulation" {
		t.Fatalf("removeAllCardsFromHand after addCard HAND: refusal %v, want library-manipulation", r)
	}
	// no draw reaches past the library the test adds
	if _, r := readWithCorpus(t, `removeAllCardsFromLibrary(playerA);
        addCard(Zone.LIBRARY, playerA, "Grizzly Bears");
        setStopAt(1, PhaseStep.BEGIN_COMBAT);
        execute();
        assertLife(playerA, 20);`); r != nil {
		t.Fatalf("removeAllCardsFromLibrary with no draw: refused %v", r)
	}
	// turn 3 and turn 5 are playerA's draws: two draws, one added card
	if _, r := readWithCorpus(t, `removeAllCardsFromLibrary(playerA);
        addCard(Zone.LIBRARY, playerA, "Grizzly Bears");
        setStopAt(5, PhaseStep.BEGIN_COMBAT);
        execute();
        assertLife(playerA, 20);`); r == nil || r.Code != "library-size" {
		t.Fatalf("a draw past the added cards: refusal %v, want library-size", r)
	}
	// the other seat's library is the runner's filler: not marked
	if _, r := readWithCorpus(t, `removeAllCardsFromLibrary(playerA);
        addCard(Zone.LIBRARY, playerA, "Grizzly Bears");
        setStopAt(4, PhaseStep.BEGIN_COMBAT);
        execute();
        assertLife(playerA, 20);`); r != nil {
		// turn 3 is playerA's only draw by turn 4: one card is enough
		t.Fatalf("one draw, one added card: refused %v", r)
	}
	// XMage applies the queued clear before it adds the cheated cards, so a
	// card added before the call survives; one card covers the turn 3 draw
	if _, r := readWithCorpus(t, `addCard(Zone.LIBRARY, playerA, "Grizzly Bears");
        removeAllCardsFromLibrary(playerA);
        setStopAt(3, PhaseStep.BEGIN_COMBAT);
        execute();
        assertLife(playerA, 20);`); r != nil {
		t.Fatalf("addCard LIBRARY before the clear: refused %v", r)
	}
	// a card queued between two clears survives both: the clear is applied
	// once, before the cheats, not at its source position
	tr, r := readWithCorpus(t, `removeAllCardsFromLibrary(playerA);
        addCard(Zone.LIBRARY, playerA, "Grizzly Bears");
        removeAllCardsFromLibrary(playerA);
        setStopAt(1, PhaseStep.BEGIN_COMBAT);
        execute();
        assertLife(playerA, 20);`)
	if r != nil {
		t.Fatalf("addCard between two clears: refused %v", r)
	}
	added := 0
	for _, ac := range tr.setup[0] {
		if ac.zone == "library_top" {
			added += ac.count
		}
	}
	if added != 1 {
		t.Fatalf("addCard between two clears: %d library card(s) in the setup, want 1", added)
	}
	// a card the test plays that draws is a visible draw effect
	if _, r := readWithCorpus(t, `removeAllCardsFromLibrary(playerA);
        addCard(Zone.LIBRARY, playerA, "Grizzly Bears");
        addCard(Zone.HAND, playerA, "Divination");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Divination");
        setStopAt(1, PhaseStep.BEGIN_COMBAT);
        execute();
        assertLife(playerA, 20);`); r == nil || r.Code != "library-size" {
		t.Fatalf("Divination draws two from one card: refusal %v, want library-size", r)
	}
	// the same draw spell twice draws twice: 1 draw step + 2 + 2 > 3 added
	if _, r := readWithCorpus(t, `removeAllCardsFromLibrary(playerA);
        addCard(Zone.LIBRARY, playerA, "Grizzly Bears", 3);
        addCard(Zone.BATTLEFIELD, playerA, "Island", 5);
        addCard(Zone.HAND, playerA, "Divination", 2);
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Divination");
        castSpell(3, PhaseStep.PRECOMBAT_MAIN, playerA, "Divination");
        setStopAt(3, PhaseStep.END_TURN);
        execute();
        assertLife(playerA, 20);`); r == nil || r.Code != "library-size" {
		t.Fatalf("Divination twice from three added cards: refusal %v, want library-size", r)
	}
	// ... and once, 1 draw step + 2 = 3 added, is covered
	if _, r := readWithCorpus(t, `removeAllCardsFromLibrary(playerA);
        addCard(Zone.LIBRARY, playerA, "Grizzly Bears", 3);
        addCard(Zone.BATTLEFIELD, playerA, "Island", 5);
        addCard(Zone.HAND, playerA, "Divination");
        castSpell(3, PhaseStep.PRECOMBAT_MAIN, playerA, "Divination");
        setStopAt(3, PhaseStep.END_TURN);
        execute();
        assertLife(playerA, 20);`); r != nil {
		t.Fatalf("Divination once from three added cards: refused %v", r)
	}
	// a search looks at every card of the library: the runner's filler
	// still holds Wastes, XMage's cleared library holds nothing
	if _, r := readWithCorpus(t, `removeAllCardsFromLibrary(playerA);
        addCard(Zone.BATTLEFIELD, playerA, "Forest", 2);
        addCard(Zone.HAND, playerA, "Rampant Growth");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Rampant Growth");
        addTarget(playerA, "Forest");
        setStopAt(1, PhaseStep.END_TURN);
        execute();
        assertLife(playerA, 20);`); r == nil || r.Code != "library-size" {
		t.Fatalf("Rampant Growth searches the cleared library: refusal %v, want library-size", r)
	}
	// a mill past the added cards reads the runner's filler
	if _, r := readWithCorpus(t, `removeAllCardsFromLibrary(playerA);
        addCard(Zone.LIBRARY, playerA, "Grizzly Bears", 3);
        addCard(Zone.HAND, playerA, "Glimpse the Unthinkable");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Glimpse the Unthinkable", playerA);
        setStopAt(1, PhaseStep.BEGIN_COMBAT);
        execute();
        assertLife(playerA, 20);`); r == nil || r.Code != "library-size" {
		t.Fatalf("mill ten from three added cards: refusal %v, want library-size", r)
	}
	// "draw a card for each" is not one fixed draw
	if _, r := readWithCorpus(t, `removeAllCardsFromLibrary(playerA);
        addCard(Zone.LIBRARY, playerA, "Grizzly Bears");
        addCard(Zone.HAND, playerA, "Collective Unconscious");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Collective Unconscious");
        setStopAt(1, PhaseStep.BEGIN_COMBAT);
        execute();
        assertLife(playerA, 20);`); r == nil || r.Code != "library-size" {
		t.Fatalf("Collective Unconscious draws for each creature: refusal %v, want library-size", r)
	}
	// amounts above seven were dropped by the draw scan
	if _, r := readWithCorpus(t, `removeAllCardsFromLibrary(playerA);
        addCard(Zone.LIBRARY, playerA, "Grizzly Bears");
        addCard(Zone.HAND, playerA, "Blood for the Blood God!");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Blood for the Blood God!");
        setStopAt(1, PhaseStep.BEGIN_COMBAT);
        execute();
        assertLife(playerA, 20);`); r == nil || r.Code != "library-size" {
		t.Fatalf("Blood for the Blood God! draws eight: refusal %v, want library-size", r)
	}
	// the library count is XMage's alone
	if _, r := readWithCorpus(t, `removeAllCardsFromLibrary(playerA);
        addCard(Zone.LIBRARY, playerA, "Grizzly Bears", 3);
        setStopAt(1, PhaseStep.BEGIN_COMBAT);
        execute();
        assertLibraryCount(playerA, 3);`); r == nil || r.Code != "library-size" {
		t.Fatalf("assertLibraryCount after a clear: refusal %v, want library-size", r)
	}
}

// TestOracleDrawDepths pins the Oracle-text scan: a fixed high amount still
// counts, an amount fixed per permanent or per trigger does not, and a draw
// named in a trigger's condition is not an effect.
func TestOracleDrawDepths(t *testing.T) {
	tr, r := readWithCorpus(t, `setStopAt(1, PhaseStep.BEGIN_COMBAT);
        execute();
        assertLife(playerA, 20);`)
	if r != nil {
		t.Fatalf("plain method: refused %v", r)
	}
	for _, tc := range []struct {
		name string
		want int
	}{
		{"Blood for the Blood God!", 8},
		{"Collective Unconscious", unknownDraws},
		{"Ripjaw Raptor", unknownDraws},
		{"Chasm Skulker", 1},
		{"Rampant Growth", unknownDraws},
	} {
		if got := tr.oracleDraws(tc.name); got != tc.want {
			t.Errorf("oracleDraws(%q) = %d, want %d", tc.name, got, tc.want)
		}
	}
}
