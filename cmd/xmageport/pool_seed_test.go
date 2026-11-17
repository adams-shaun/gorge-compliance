package main

import (
	"testing"

	"github.com/adams-shaun/gorge/testkit/testutil"
)

func translateBody(t *testing.T, body string) *result {
	t.Helper()
	jc, err := parseJava("x", "x", "class PoolSeedTest extends CardTestPlayerBase {\n    @Test\n    public void testOne() {\n"+body+"\n    }\n}")
	if err != nil {
		t.Fatal(err)
	}
	if len(jc.Methods) != 1 {
		t.Fatalf("parsed %d methods, want 1", len(jc.Methods))
	}
	return translate(testutil.CorpusRegistry(t), jc, &jc.Methods[0], "mana")
}

func castMana(t *testing.T, res *result) (string, bool) {
	t.Helper()
	if res.Scenario == nil {
		return "", false
	}
	for _, st := range res.Scenario.Steps {
		if st.Op == "cast" {
			return st.Mana, true
		}
	}
	return "", false
}

// TestCastManaSeedsOnlyWhatTheSeatCanPay: a cast's pool holds only the mana
// the seat's battlefield can make, so gorge pays the rest the way the game
// would (K'rrik's life), and a seat holding a self-sacrificing mana source
// the cast may have spent refuses.
func TestCastManaSeedsOnlyWhatTheSeatCanPay(t *testing.T) {
	const tail = `
        setStopAt(1, PhaseStep.END_TURN);
        execute();
        assertPermanentCount(playerA, "Banehound", 1);`
	t.Run("no source for the symbol", func(t *testing.T) {
		res := translateBody(t, `addCard(Zone.BATTLEFIELD, playerA, "K'rrik, Son of Yawgmoth");
        addCard(Zone.HAND, playerA, "Banehound");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Banehound");`+tail)
		if res.Status == "refused" {
			t.Fatalf("refused %s: %s", res.Code, res.Detail)
		}
		if m, ok := castMana(t, res); !ok || m != "" {
			t.Fatalf("cast mana %q (cast step found %v), want empty", m, ok)
		}
	})
	t.Run("land still seeds", func(t *testing.T) {
		res := translateBody(t, `addCard(Zone.BATTLEFIELD, playerA, "Swamp");
        addCard(Zone.HAND, playerA, "Banehound");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Banehound");`+tail)
		if m, ok := castMana(t, res); !ok || m != "B" {
			t.Fatalf("cast mana %q (found %v, status %s %s), want B", m, ok, res.Status, res.Code)
		}
	})
	t.Run("nonbasic land and rock seed", func(t *testing.T) {
		res := translateBody(t, `addCard(Zone.BATTLEFIELD, playerA, "Watery Grave");
        addCard(Zone.BATTLEFIELD, playerA, "Sol Ring");
        addCard(Zone.HAND, playerA, "Hill Giant");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Hill Giant");`+tail)
		if m, ok := castMana(t, res); !ok || len(m) != 3 {
			t.Fatalf("cast mana %q (found %v, status %s %s), want 3 mana", m, ok, res.Status, res.Code)
		}
	})
	t.Run("self-sacrificing source", func(t *testing.T) {
		res := translateBody(t, `addCard(Zone.BATTLEFIELD, playerA, "Black Lotus");
        addCard(Zone.HAND, playerA, "Armory Mice");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Armory Mice");`+tail)
		if res.Status != "refused" || res.Code != "mana-source-payment" {
			t.Fatalf("status %s code %q, want refused mana-source-payment", res.Status, res.Code)
		}
	})
	t.Run("self-sacrificing source cast from hand", func(t *testing.T) {
		res := translateBody(t, `addCard(Zone.HAND, playerA, "Armory Mice");
        addCard(Zone.HAND, playerA, "Black Lotus");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Black Lotus", true);
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Armory Mice");`+tail)
		if res.Status != "refused" || res.Code != "mana-source-payment" {
			t.Fatalf("status %s code %q, want refused mana-source-payment", res.Status, res.Code)
		}
	})
	t.Run("lands cover the cost", func(t *testing.T) {
		res := translateBody(t, `addCard(Zone.BATTLEFIELD, playerA, "Black Lotus");
        addCard(Zone.BATTLEFIELD, playerA, "Plains", 2);
        addCard(Zone.HAND, playerA, "Armory Mice");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Armory Mice");`+tail)
		if res.Status == "refused" && res.Code == "mana-source-payment" {
			t.Fatalf("refused though two basic lands pay Armory Mice")
		}
	})
}
