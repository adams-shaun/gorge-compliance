package main

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestForgeAbilityLinesCopyEffect: Absorbing Man's main-phase trigger makes it
// a copy of p1's Sol Talisman (CR 707.2); step 56 then activates the copied
// {T}: Add {C}{C}. The printed Absorbing Man has no abilities, so the line is
// the donor's (found from the snapshot's live types/keywords), not omitted.
// The fixture is the xproof find xs_XP_absorbing-man_f0t0_s1.
func TestForgeAbilityLinesCopyEffect(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	b, err := os.ReadFile("testdata/absorbing_man_copy_sol_talisman.json")
	if err != nil {
		t.Fatal(err)
	}
	var it oraclegen.Item
	if err := json.Unmarshal(b, &it); err != nil {
		t.Fatal(err)
	}
	g := replayItem(t, reg, it)
	const want = "AB$ Mana | Cost$ T | Produced$ C | Amount$ 2 | SpellDescription$ Add {C}{C}."
	if st := it.Steps[56]; st.Card != "p0:Absorbing Man" {
		t.Fatalf("fixture step 56 = %+v, want an activate on p0:Absorbing Man", st)
	}
	lines := forgeAbilityLines(reg, it, g)
	if got := lines["56"]; got != want {
		t.Errorf("step 56 line = %q, want Sol Talisman's %q", got, want)
	}
}

// TestForgeAbilityLinesEmblemGrant: Koth's -5 emblem grants Mountains "{T}:
// deal 1 damage to any target"; step 3 activates it. The fixture is the
// xproof find xs_XP_koth-of-the-hammer_f0a2_s1.
func TestForgeAbilityLinesEmblemGrant(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	b, err := os.ReadFile("testdata/koth_emblem_mountain_ping.json")
	if err != nil {
		t.Fatal(err)
	}
	var it oraclegen.Item
	if err := json.Unmarshal(b, &it); err != nil {
		t.Fatal(err)
	}
	g := replayItem(t, reg, it)
	const want = "AB$ DealDamage | Cost$ T | ValidTgts$ Any | NumDmg$ 1 | SpellDescription$ CARDNAME deals 1 damage to any target."
	if got := forgeAbilityLines(reg, it, g)["3"]; got != want {
		t.Errorf("step 3 line = %q, want the emblem-granted %q", got, want)
	}
}
