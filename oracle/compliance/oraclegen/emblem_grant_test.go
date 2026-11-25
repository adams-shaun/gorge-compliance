package oraclegen

import (
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestResolveActivatedAbilityEmblemGrant: Koth of the Hammer's -5 is an
// `AB$ Effect | StaticAbilities$ STDamage`; the emblem's static (an SVar, not
// a printed S: line) gives Mountains its controller controls the {T}: deal 1
// ability. A step activating that granted ability on a Mountain must resolve
// to ABDealDamage's line, not ActivatedGrantedUnresolved.
func TestResolveActivatedAbilityEmblemGrant(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	const label = "Mountain: CARDNAME deals 1 damage to any target."
	k := 0
	it := Item{Scenario: Scenario{
		Setup: map[string]Seat{"p0": {Battlefield: []string{"Koth of the Hammer", "Mountain"}}},
		Steps: []Step{{Op: "activate", Card: "p0:Mountain", AbilityIndex: &k, Ability: label}},
	}}
	g := rules.OracleResult{Snapshots: []rules.OracleSnapshot{{Permanents: []rules.OracleSnapPerm{
		{Ref: "p0:Koth of the Hammer", Name: "Koth of the Hammer", Controller: 0, Owner: 0, Types: []string{"Legendary", "Planeswalker"}},
		{Ref: "p0:Mountain", Name: "Mountain", Controller: 0, Owner: 0, Types: []string{"Land", "Mountain"}},
	}}}}
	sa, kind := ResolveActivatedAbility(reg, it, 0, it.Steps[0], g)
	if kind != ActivatedGranted || sa == nil {
		t.Fatalf("kind = %v, sa = %v; want ActivatedGranted", kind, sa)
	}
	const want = "AB$ DealDamage | Cost$ T | ValidTgts$ Any | NumDmg$ 1 | SpellDescription$ CARDNAME deals 1 damage to any target."
	if sa.Line != want {
		t.Errorf("line = %q, want %q", sa.Line, want)
	}

	// The emblem belongs to Koth's controller: an opponent's Mountain is not
	// reached by Mountain.YouCtrl.
	g.Snapshots[0].Permanents[1].Controller = 1
	g.Snapshots[0].Permanents[1].Ref = "p1:Mountain"
	it.Steps[0].Card = "p1:Mountain"
	if _, kind := ResolveActivatedAbility(reg, it, 0, it.Steps[0], g); kind != ActivatedGrantedUnresolved {
		t.Errorf("opponent's Mountain kind = %v, want ActivatedGrantedUnresolved", kind)
	}
}

// TestResolveActivatedAbilityPumpKeywordGrant: Cursecloth Wrappings' Pump
// grants Embalm:CardManaCost to a creature card in the graveyard. A later step
// activating that granted Embalm on Memnite resolves to the Embalm keyword
// ability; without the earlier Pump step the same pick stays unresolved.
func TestResolveActivatedAbilityPumpKeywordGrant(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	const label = "Memnite: Embalm 0"
	k0, k1 := 0, -1
	it := Item{Scenario: Scenario{
		Setup: map[string]Seat{"p0": {Battlefield: []string{"Cursecloth Wrappings"}, Graveyard: []string{"Memnite"}}},
		Steps: []Step{
			{Op: "activate", Card: "p0:Cursecloth Wrappings", AbilityIndex: &k0},
			{Op: "activate", Card: "p0:Memnite", AbilityIndex: &k1, Ability: label},
		},
	}}
	g := rules.OracleResult{Snapshots: []rules.OracleSnapshot{
		{Permanents: []rules.OracleSnapPerm{{Ref: "p0:Cursecloth Wrappings", Name: "Cursecloth Wrappings", Controller: 0, Owner: 0, Types: []string{"Artifact"}}}},
		{Permanents: []rules.OracleSnapPerm{{Ref: "p0:Cursecloth Wrappings", Name: "Cursecloth Wrappings", Controller: 0, Owner: 0, Types: []string{"Artifact"}}}},
	}}
	sa, kind := ResolveActivatedAbility(reg, it, 1, it.Steps[1], g)
	if kind != ActivatedGranted || sa == nil {
		t.Fatalf("kind = %v, sa = %v; want ActivatedGranted", kind, sa)
	}
	if got := GrantedAbilityLabel("Memnite", sa); got != label {
		t.Errorf("label = %q, want %q", got, label)
	}
	if sa.Params["KeywordLine"] != "Embalm:CardManaCost" {
		t.Errorf("KeywordLine = %q", sa.Params["KeywordLine"])
	}

	// No earlier use of the Pump source: nothing granted the keyword.
	it.Steps = it.Steps[1:]
	if _, kind := ResolveActivatedAbility(reg, it, 0, it.Steps[0], g); kind != ActivatedGrantedUnresolved {
		t.Errorf("unused source kind = %v, want ActivatedGrantedUnresolved", kind)
	}
}
