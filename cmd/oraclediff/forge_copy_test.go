package main

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
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

// copyDonorFixture is the step-56 view of Absorbing Man after it copied an
// artifact: live types [Artifact Creature Human Legendary Villain], Vigilance,
// and the other permanents the test puts on the table.
func copyDonorFixture(t *testing.T, others ...rules.OracleSnapPerm) (*cards.Registry, oraclegen.Item, rules.OracleResult) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	zero := 0
	it := oraclegen.Item{Scenario: oraclegen.Scenario{Steps: []oraclegen.Step{{Op: "activate", Seat: 0, Card: "p0:Absorbing Man", AbilityIndex: &zero}}}}
	am := rules.OracleSnapPerm{Ref: "p0:Absorbing Man", Name: "Absorbing Man", Controller: 0,
		Types: []string{"Artifact", "Creature", "Human", "Legendary", "Villain"}, Keywords: []string{"Vigilance"}}
	g := rules.OracleResult{Snapshots: []rules.OracleSnapshot{{Permanents: append([]rules.OracleSnapPerm{am}, others...)}}}
	return reg, it, g
}

// A token donor is a candidate like any other permanent: Absorbing Man copying
// a Banana exports the Banana's line, not the line of another artifact that
// merely has the same shape (Mind Stone).
func TestForgeAbilityLinesCopyEffectTokenDonor(t *testing.T) {
	reg, it, g := copyDonorFixture(t,
		rules.OracleSnapPerm{Ref: "p1:token:Banana", Name: "Banana", Controller: 1, Token: true, Types: []string{"Artifact"}},
		rules.OracleSnapPerm{Ref: "p0:Mind Stone", Name: "Mind Stone", Controller: 0, Types: []string{"Artifact"}})
	const want = "AB$ Mana | Cost$ T Sac<1/CARDNAME/this artifact> | Produced$ Combo R G | Amount$ 1 | SubAbility$ DBGainLife | SpellDescription$ Add {R} or {G}. You gain 2 life."
	// Banana and Mind Stone both fit the copy's shape with different lines:
	// a tie, so the step is omitted. It must never be Mind Stone's line.
	if got, ok := forgeAbilityLines(reg, it, g)["0"]; ok {
		t.Errorf("Banana donor tied with Mind Stone resolved to %q, want the step omitted", got)
	}
	// Without the other artifact the Banana alone is the donor.
	g.Snapshots[0].Permanents = g.Snapshots[0].Permanents[:2]
	if got := forgeAbilityLines(reg, it, g)["0"]; got != want {
		t.Errorf("Banana-only donor line = %q, want %q", got, want)
	}
}

// Two donors that carry the ability with different lines are a tie, and a tie
// is left out rather than guessed; either alone resolves.
func TestForgeAbilityLinesCopyEffectTieOmitted(t *testing.T) {
	reg, it, g := copyDonorFixture(t,
		rules.OracleSnapPerm{Ref: "p1:Sol Ring", Name: "Sol Ring", Controller: 1, Types: []string{"Artifact"}},
		rules.OracleSnapPerm{Ref: "p0:Mind Stone", Name: "Mind Stone", Controller: 0, Types: []string{"Artifact"}})
	if got, ok := forgeAbilityLines(reg, it, g)["0"]; ok {
		t.Errorf("two donors with different ability lines resolved to %q, want the step omitted", got)
	}
	g.Snapshots[0].Permanents = g.Snapshots[0].Permanents[:2]
	if got := forgeAbilityLines(reg, it, g)["0"]; got == "" {
		t.Error("a single Sol Ring donor did not resolve")
	}
}

// A token permanent whose script cannot be named, but whose snapshot shape
// could be the donor, blocks the step instead of letting another artifact's
// line stand in for it.
func TestForgeAbilityLinesCopyEffectUnnamedTokenBlocks(t *testing.T) {
	reg, it, g := copyDonorFixture(t,
		rules.OracleSnapPerm{Ref: "p1:token:No Such Token", Name: "No Such Token", Controller: 1, Token: true, Types: []string{"Artifact"}},
		rules.OracleSnapPerm{Ref: "p0:Mind Stone", Name: "Mind Stone", Controller: 0, Types: []string{"Artifact"}})
	if got, ok := forgeAbilityLines(reg, it, g)["0"]; ok {
		t.Errorf("unresolvable token donor still let %q through, want the step omitted", got)
	}
}
