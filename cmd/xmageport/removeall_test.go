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
	// cards added before the removal are gone with it
	if _, r := readWithCorpus(t, `addCard(Zone.LIBRARY, playerA, "Grizzly Bears");
        removeAllCardsFromLibrary(playerA);
        setStopAt(3, PhaseStep.BEGIN_COMBAT);
        execute();
        assertLife(playerA, 20);`); r == nil || r.Code != "library-size" {
		t.Fatalf("library cleared after the add, then a draw: refusal %v, want library-size", r)
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
	// the library count is XMage's alone
	if _, r := readWithCorpus(t, `removeAllCardsFromLibrary(playerA);
        addCard(Zone.LIBRARY, playerA, "Grizzly Bears", 3);
        setStopAt(1, PhaseStep.BEGIN_COMBAT);
        execute();
        assertLibraryCount(playerA, 3);`); r == nil || r.Code != "library-size" {
		t.Fatalf("assertLibraryCount after a clear: refusal %v, want library-size", r)
	}
}
