package templates

import (
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclediff"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// staticAt returns the cards.Static at name's requirement key.
func staticAt(t *testing.T, reg *cards.Registry, name, key string) cards.Static {
	t.Helper()
	req := counterReq(t, reg, name, key)
	c, _ := reg.Lookup(name)
	st, _ := staticSlotOf(c.Faces[0], req)
	return st
}

// TestStaticGatedGrantFallsThroughToGenericPath pins the rows the gated
// self-grant helper must NOT take ownership of. A gated grant that also
// carries an observable effect (a level creature's SetPower/SetToughness --
// Echo Mage, Joraga Treespeaker) is served by the generic probe path; the
// helper's failure must fall through to it instead of returning a skip. Echo
// Mage / Joraga Treespeaker live in ROE, which no census pins, so the
// regression this test guards was invisible to every other test in the
// package.
//
// Joraga Treespeaker static#0.0 used to be in this list. It stopped being a
// fall-through row at gorge b2a42a24d8, which made the mana-grant scan honour
// an IsPresent$ gate (engine/rules/mana_grant_gate.go): the gate-off control
// no longer leaks the {T}: Add {G}{G} offer, so the helper observes the
// LEVEL 1-4 grant itself (TestStaticGatedManaGrantServedByHelper).
func TestStaticGatedGrantFallsThroughToGenericPath(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct{ card, key string }{
		{"Echo Mage", "static#0.0"},
		{"Echo Mage", "static#0.1"},
	} {
		card, key := tc.card, tc.key
		t.Run(card+"/"+key, func(t *testing.T) {
			st := staticAt(t, reg, card, key)
			// The static in question carries an AddAbility$ grant beside its
			// P/T set, which the generic path observes. Assert that shape so a
			// corpus change cannot make this test vacuous.
			if !st.HasParam(cards.PKAddAbility) {
				t.Fatalf("precondition: %s %s carries no AddAbility$", card, key)
			}
			if !staticSelfGrantedAbility(&st) {
				t.Fatalf("precondition: %s %s is not a self grant", card, key)
			}
			req := counterReq(t, reg, card, key)
			item, skip := GenerateB(reg, card, req)
			if skip != nil {
				t.Fatalf("GenerateB skip = %v; want the generic path to serve it", skip)
			}
			// The generic observation compares the keyword line; the gated
			// self-grant helper instead asserts an offered activation on the
			// card. Require the generic shape so a helper that wrongly claims
			// the row is caught even if it happens to serve something.
			hasKeywords := false
			for _, cm := range item.Compare {
				if cm == oraclediff.CompareKeywords {
					hasKeywords = true
				}
			}
			if !hasKeywords {
				t.Fatalf("%s %s served without the generic keyword comparison: %+v", card, key, item.Compare)
			}
			for _, step := range item.Scenario.Steps {
				for _, ex := range step.Expect {
					if ex.Offered != nil && ex.Offered.Card == "p0:"+card {
						t.Fatalf("%s %s: generic serving must not assert an offered option on the card: %+v", card, key, ex.Offered)
					}
				}
			}
		})
	}
}

// TestStaticGatedManaGrantServedByHelper pins Joraga Treespeaker's LEVEL 1-4
// band as served by the gated self-grant helper: the card carries a level
// counter, so gorge offers "Add GG" on it, and the helper's gate-off control
// (counter removed) must not offer it. The item asserts that offer, not the
// generic keyword comparison.
func TestStaticGatedManaGrantServedByHelper(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	const card, key = "Joraga Treespeaker", "static#0.0"
	st := staticAt(t, reg, card, key)
	if !st.HasParam(cards.PKAddAbility) || !staticSelfGrantedAbility(&st) {
		t.Fatalf("precondition: %s %s is not a self grant", card, key)
	}
	item, skip := GenerateB(reg, card, counterReq(t, reg, card, key))
	if skip != nil {
		t.Fatalf("GenerateB skip = %v; want the helper to serve it", skip)
	}
	offered := false
	for _, step := range item.Scenario.Steps {
		for _, ex := range step.Expect {
			if ex.Offered != nil && ex.Offered.Card == "p0:"+card && ex.Offered.Kind == "activate" && ex.Want != nil && *ex.Want {
				offered = true
			}
		}
	}
	if !offered {
		t.Fatalf("%s %s: want an offered activate assertion on the card: %+v", card, key, item.Scenario.Steps)
	}
}

// TestStaticSelfGrantedAbilityRecipient pins the recipient predicate the
// gated self-grant helper consults: a grant is a self grant only when its
// Affected$ names Self (or is absent, the engine's Card.Self default). Joraga
// Treespeaker's LEVEL 5+ static grants to Elf.YouCtrl, so it is not; Squadron
// Carrier grants to Spacecraft.YouCtrl.
func TestStaticSelfGrantedAbilityRecipient(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		card, key string
		want      bool
	}{
		{"Echo Mage", "static#0.0", true},
		{"Muraganda Raceway", "static#0.0", true},
		{"Joraga Treespeaker", "static#0.0", true},
		{"Joraga Treespeaker", "static#0.2", false},
		{"Squadron Carrier", "static#0.0", false},
	} {
		st := staticAt(t, reg, tc.card, tc.key)
		if got := staticSelfGrantedAbility(&st); got != tc.want {
			t.Errorf("staticSelfGrantedAbility(%s %s) = %v, want %v (Affected=%q)",
				tc.card, tc.key, got, tc.want, st.ParamStr(cards.PKAffected))
		}
	}
}
