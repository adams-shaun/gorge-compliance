package main

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/testkit/testutil"
)

// translateTapped translates a one-method class whose body is setup+actions+
// asserts (the readMethod pattern, plus the corpus translate uses).
func translateTapped(t *testing.T, body string) *result {
	t.Helper()
	// execute() goes before the body's first assert: XMage runs an assert
	// written earlier against the game before setup, and the translator drops it.
	lines := strings.Split(body, "\n")
	at := len(lines)
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "assert") {
			at = i
			break
		}
	}
	body = strings.Join(lines[:at], "\n") + "\n        setStopAt(2, PhaseStep.END_TURN);\n        execute();\n" + strings.Join(lines[at:], "\n")
	src := `class TappedManaSourceTest extends CardTestPlayerBase {
    @Test
    public void testTappedManaSource() {
` + body + `
    }
}`
	jc, err := parseJava("x", "x", src)
	if err != nil {
		t.Fatal(err)
	}
	if len(jc.Methods) != 1 {
		t.Fatalf("parsed %d methods, want 1", len(jc.Methods))
	}
	return translate(testutil.CorpusRegistry(t), jc, &jc.Methods[0], "rules")
}

// TestAssertTappedOnManaSourceRefuses: XMage taps a land or a mana rock to pay
// for a spell, but the runner injects a step's mana from the pool, so the
// source stays untapped. A generated assertTapped(source, true) can only fail
// (CR 502.3, Mana Vapors), exactly like assertTappedCount on a land, so the
// translator must refuse it.
//
// The other direction is not this class: the runner leaves sources untapped,
// which matches assertTapped(source, false) — Magewright's Stone untaps
// Rosheen Meanderer, Razorverge Thicket enters untapped — so refusing those
// would only drop coverage.
func TestAssertTappedOnManaSourceRefuses(t *testing.T) {
	const bolt = `addCard(Zone.HAND, playerA, "Lightning Bolt");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Lightning Bolt", playerB);`
	cases := []struct {
		name     string
		body     string
		wantCode string
	}{
		{
			name: "land tapped to pay",
			body: `addCard(Zone.BATTLEFIELD, playerA, "Mountain");
        ` + bolt + `
        assertTapped("Mountain", true);`,
			wantCode: "tapped-land-count",
		},
		{
			name: "mana rock tapped to pay",
			body: `addCard(Zone.BATTLEFIELD, playerA, "Jade Orb of Dragonkind");
        ` + bolt + `
        assertTapped("Jade Orb of Dragonkind", true);`,
			wantCode: "tapped-land-count",
		},
		{
			// The runner leaves the source untapped, so this matches XMage:
			// Rosheen is setup-tapped and Magewright's Stone untaps it.
			name: "setup-tapped mana dork asserted untapped",
			body: `addCard(Zone.BATTLEFIELD, playerA, "Rosheen Meanderer", 1, true);
        ` + bolt + `
        assertTapped("Rosheen Meanderer", false);`,
		},
		{
			name: "land asserted untapped",
			body: `addCard(Zone.BATTLEFIELD, playerA, "Mountain");
        ` + bolt + `
        assertTapped("Mountain", false);`,
		},
		{
			// No cast or activation can have paid this source: the tapped
			// state came from a non-mana ability, crew or convoke (brief 10).
			name: "non-land mana source tapped without paying mana",
			body: `addCard(Zone.BATTLEFIELD, playerA, "Jade Orb of Dragonkind", 1, true);
        assertTapped("Jade Orb of Dragonkind", true);`,
		},
		{
			name: "non-mana permanent tapped for convoke",
			body: `addCard(Zone.BATTLEFIELD, playerA, "Runeclaw Bear");
        ` + bolt + `
        assertTapped("Runeclaw Bear", true);`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := translateTapped(t, tc.body)
			if res.Code != tc.wantCode {
				t.Fatalf("status %q code %q detail %q; want code %q",
					res.Status, res.Code, res.Detail, tc.wantCode)
			}
		})
	}
}

// TestTappedCountSharesManaSourceGuard: assertTappedCount uses the same guard
// as assertTapped, so the two cannot drift. It also owns the same direction
// rule: n==0 inverts the predicate, so "0 tapped" expects all untapped.
func TestTappedCountSharesManaSourceGuard(t *testing.T) {
	const bolt = `addCard(Zone.HAND, playerA, "Lightning Bolt");
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Lightning Bolt", playerB);`
	cases := []struct {
		name     string
		body     string
		wantCode string
	}{
		{
			name: "land tapped",
			body: `addCard(Zone.BATTLEFIELD, playerA, "Mountain");
        ` + bolt + `
        assertTappedCount("Mountain", true, 1);`,
			wantCode: "tapped-land-count",
		},
		{
			name: "land untapped",
			body: `addCard(Zone.BATTLEFIELD, playerA, "Mountain");
        ` + bolt + `
        assertTappedCount("Mountain", false, 1);`,
		},
		{
			name: "zero tapped expects untapped",
			body: `addCard(Zone.BATTLEFIELD, playerA, "Mountain");
        ` + bolt + `
        assertTappedCount("Mountain", true, 0);`,
		},
		{
			name: "zero untapped expects tapped",
			body: `addCard(Zone.BATTLEFIELD, playerA, "Mountain");
        ` + bolt + `
        assertTappedCount("Mountain", false, 0);`,
			wantCode: "tapped-land-count",
		},
		{
			name: "mana rock tapped to pay",
			body: `addCard(Zone.BATTLEFIELD, playerA, "Jade Orb of Dragonkind");
        ` + bolt + `
        assertTappedCount("Jade Orb of Dragonkind", true, 1);`,
			wantCode: "tapped-land-count",
		},
		{
			name: "mana rock tapped without paying mana",
			body: `addCard(Zone.BATTLEFIELD, playerA, "Jade Orb of Dragonkind", 1, true);
        assertTappedCount("Jade Orb of Dragonkind", true, 1);`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := translateTapped(t, tc.body)
			if res.Code != tc.wantCode {
				t.Fatalf("status %q code %q detail %q; want code %q",
					res.Status, res.Code, res.Detail, tc.wantCode)
			}
		})
	}
}
