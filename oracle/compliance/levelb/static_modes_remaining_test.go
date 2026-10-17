package levelb_test

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/levelb"
	"github.com/adams-shaun/gorge/engine/cards"
)

// TestRemainingStaticModesClassify pins the classification of every row in
// the remaining-static-modes ticket's measured table: the served rows carry
// their sub-family with no gap (reqOf), the rows whose probe the engine or
// the fixture grammar cannot reach carry a named gap (gapOf).
func TestRemainingStaticModesClassify(t *testing.T) {
	for _, tc := range []struct{ card, key, sub string }{
		// Activations: a positive PowerUp ceiling (Wonder Man).
		{"Wonder Man, Hollywood Hero", "static#0.0", "static.activations-powerup"},
		// CantBlockUnless: the attacking self tax shape (the template keeps
		// it a named skip -- no paid-block checkpoint in the grammar).
		{"Archangel of Tithes", "static#0.1", "static.cant-block-unless-tax"},
		// ManaConvert: the Case-spell and activated-ability shapes.
		{"Case File Auditor", "static#0.0", "static.mana-convert-case-spells"},
		{"Agatha's Soul Cauldron", "static#0.0", "static.mana-convert-abilities"},
		// CantPreventDamage: unconditional and combat-only.
		{"Sunspine Lynx", "static#0.1", "static.cant-prevent-damage"},
		{"Frenzied Baloth", "static#0.0", "static.cant-prevent-damage-combat"},
		// Activations: Elvish Refueler's unbounded exhaust re-activation.
		{"Elvish Refueler", "static#0.0", "static.activations-exhaust-unbounded"},
		// AlternativeCost: a mana-only alternative for your spells, and the
		// enduring-story equip alternative.
		{"Leyline of Mutation", "static#0.0", "static.alternative-cost-spells"},
		{"Kíli the Resourceful", "static#0.0", "static.alternative-cost-equip"},
		// CantAttackUnless: the untapped self tax (Archangel of Tithes) and
		// the enduring-story gate (Dáin, set up by the Storied fixture).
		{"Archangel of Tithes", "static#0.0", "static.cant-attack-unless-tax"},
		{"Dáin, Lord of the Iron Hills", "static#0.0", "static.cant-attack-unless-tax"},
		// IgnoreHexproof, CantPutCounter, NoCleanupDamage, CantBeSuspected.
		{"Nowhere to Run", "static#0.0", "static.ignore-hexproof"},
		{"Blossombind", "static#0.0", "static.cant-put-counter"},
		{"Ancient Adamantoise", "static#0.0", "static.no-cleanup-damage"},
		{"Airtight Alibi", "static#0.1", "static.cant-be-suspected"},
		// ActivateAbilityAsIfHaste, PlotZone, CantBeCopied.
		{"Shang-Chi, Master of Kung Fu", "static#0.0", "static.activate-as-if-haste"},
		{"Fblthp, Lost on the Range", "static#0.2", "static.plot-zone"},
		{"Choreographed Sparks", "static#0.0", "static.cant-be-copied"},
		// UnspentMana and IgnoreLegendRule.
		{"Electro, Assaulting Battery", "static#0.0", "static.unspent-mana"},
		{"Spider-Verse", "static#0.0", "static.ignore-legend-rule"},
		// FlipCoinMod: Edgar's first-flip-each-turn heads shape, read by
		// effects.FlipCoinForced and observed by a coin-flip probe.
		{"Edgar, King of Figaro", "static#0.0", "static.flip-coin-mod"},
	} {
		reqOf(t, tc.card, tc.key, tc.sub)
	}
}

// TestFlipCoinModOtherShapeIsNamedGap: only Edgar's exact Two-Headed Coin
// shape is served (effects.FlipCoinForced reads nothing else); a FlipCoinMod
// with any other result keeps the mode's named gap so it never reaches the
// coin-flip template the engine would not honour.
func TestFlipCoinModOtherShapeIsNamedGap(t *testing.T) {
	c, d := cards.ParseBytes("t.txt", []byte("Name:Test Thumb\nTypes:Artifact\n"+
		"S:Mode$ FlipCoinMod | ValidPlayer$ You | CheckSVar$ Count$YouFlipThisTurn | SVarCompare$ EQ0 | Result$ False\nOracle:x\n"))
	if len(d) != 0 {
		t.Fatalf("diags: %v", d)
	}
	c.Link()
	for _, req := range levelb.Requirements(c) {
		if req.Key != "static#0.0" {
			continue
		}
		if req.Sub != "static.gap:FlipCoinMod" || !strings.Contains(req.Gap, "no coin-flip probe") {
			t.Fatalf("Result$ False FlipCoinMod classifies as %q gap %q, want the named FlipCoinMod gap", req.Sub, req.Gap)
		}
		return
	}
	t.Fatal("precondition: no static#0.0 requirement")
}
