package oraclegen

import (
	"testing"

	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/state"
)

func fixtureCard(t *testing.T, src string) *cards.Card {
	t.Helper()
	c, diags := cards.ParseBytes(t.Name(), []byte(src))
	if len(diags) > 0 {
		t.Fatalf("parse fixture: %v", diags)
	}
	return c
}

func fixtureStatic(t *testing.T, c *cards.Card) cards.Static {
	t.Helper()
	if len(c.Faces) == 0 || len(c.Faces[0].Statics) == 0 {
		t.Fatalf("fixture has no static: %+v", c.Faces)
	}
	return c.Faces[0].Statics[0]
}

// TestStaticAffectsRecipientDefaultsToSelf: Forge omits Affected$ on a
// self-only static, and the resolver must read that default rather than
// treating the absent filter as "every card in the pool".
func TestStaticAffectsRecipientDefaultsToSelf(t *testing.T) {
	self := fixtureCard(t, "Name:X\nTypes:Creature\nS:Mode$ Continuous | AddAbility$ Pump\nSVar:Pump:AB$ Pump | Cost$ R\nOracle:x\n")
	other := fixtureCard(t, "Name:Other Card\nTypes:Creature\nOracle:x\n")
	st := fixtureStatic(t, self)
	src := &grantedPoolCard{card: self, zone: state.ZBattlefield}
	if !staticAffectsRecipient(st, src, grantRecipient{card: self, facts: cardFacts{card: self}}) {
		t.Errorf("absent Affected$ did not default to Card.Self for the source card")
	}
	if staticAffectsRecipient(st, src, grantRecipient{card: other, facts: cardFacts{card: other}}) {
		t.Errorf("absent Affected$ reached an unrelated card; it must default to Card.Self")
	}
}

// TestStaticAffectsRecipientFailsClosedOnUnknownPredicate: a predicate the
// evaluator cannot read must not be accepted, because a false candidate can
// make a resolvable grant ambiguous (or export the wrong line).
func TestStaticAffectsRecipientFailsClosedOnUnknownPredicate(t *testing.T) {
	self := fixtureCard(t, "Name:Remembered Grant\nTypes:Creature\nS:Mode$ Continuous | Affected$ Card.IsRemembered | AddAbility$ Pump\nSVar:Pump:AB$ Pump | Cost$ R\nOracle:x\n")
	notRemembered := fixtureCard(t, "Name:Not Remembered\nTypes:Creature\nS:Mode$ Continuous | Affected$ Card.!IsRemembered | AddAbility$ Pump\nSVar:Pump:AB$ Pump | Cost$ R\nOracle:x\n")
	src := &grantedPoolCard{card: self, zone: state.ZBattlefield}
	rec := grantRecipient{card: self, facts: cardFacts{card: self}}
	if staticAffectsRecipient(fixtureStatic(t, self), src, rec) {
		t.Errorf("unknown positive predicate IsRemembered was accepted")
	}
	if staticAffectsRecipient(fixtureStatic(t, notRemembered), src, rec) {
		t.Errorf("unknown negated predicate !IsRemembered was accepted")
	}
}
