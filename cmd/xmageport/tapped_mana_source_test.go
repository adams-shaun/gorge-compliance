package main

import (
	"testing"

	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestAssertTappedOnManaSourceRefuses: XMage taps a land or a mana rock to
// pay for a spell or ability, but the runner injects a step's mana from the
// pool, so the source stays untapped. A generated assertTapped(source, true)
// can only fail (CR 502.3, Mana Vapors), exactly like assertTappedCount on a
// land, so the translator must refuse it.
func TestAssertTappedOnManaSourceRefuses(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	// Mountain is a land whose mana ability is intrinsic (no printed AB$ Mana);
	// Jade Orb is a non-land artifact with a printed AB$ Mana. Both are sources
	// XMage can tap for a cost the runner pays from the pool. Grizzly Bears is
	// the negative control: a creature with no mana ability that a test may tap
	// for convoke/crew is not this class and must still generate.
	cases := []struct{ card, wantCode string }{
		{"Mountain", "tapped-land-count"},
		{"Jade Orb of Dragonkind", "tapped-land-count"},
		{"Grizzly Bears", ""},
	}
	for _, tc := range cases {
		card := tc.card
		t.Run(card, func(t *testing.T) {
			src := `class TappedManaSourceTest extends CardTestPlayerBase {
    @Test
    public void testTappedManaSource() {
        addCard(Zone.BATTLEFIELD, playerA, "` + card + `");
        addCard(Zone.HAND, playerA, "Lightning Bolt");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Lightning Bolt", playerB);
        setStopAt(1, PhaseStep.END_TURN);
        execute();
        assertTapped("` + card + `", true);
    }
}`
			jc, err := parseJava("x", "x", src)
			if err != nil {
				t.Fatal(err)
			}
			res := translate(reg, jc, &jc.Methods[0], "rules")
			if res.Code != tc.wantCode {
				t.Fatalf("assertTapped(%q, true): status %q code %q detail %q; want code %q",
					card, res.Status, res.Code, res.Detail, tc.wantCode)
			}
		})
	}
}

// TestTappedCountSharesManaSourceGuard: assertTappedCount and assertTapped use
// the same mana-source predicate, so a non-land mana rock refuses in both and
// the two guards cannot drift.
func TestTappedCountSharesManaSourceGuard(t *testing.T) {
	tr := &translator{reg: testutil.CorpusRegistry(t)}
	if _, r := tr.tappedCount("Jade Orb of Dragonkind", true, 1); r == nil || r.Code != "tapped-land-count" {
		t.Fatalf("tappedCount on a mana rock: refusal %v, want tapped-land-count", r)
	}
}
