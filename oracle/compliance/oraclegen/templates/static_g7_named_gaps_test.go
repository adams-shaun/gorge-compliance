package templates_test

import (
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/levelb"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen/templates"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestStaticG7BackFaceGraveyardOfferServed: Emet-Selch, Unsundered
// static#1.0 (Hades, Sorcerer of Eld's "During your turn, you may play cards
// from your graveyard") was the G7 named gap "graveyard probe is exiled by
// the card's own Moved replacement when setup places it". The measured cause
// was the face, not the replacement: a fixture's setup graveyard placement
// does not run replacements, but the offer observation placed the card on
// its FRONT face, where the permission does not exist. The back-face retry
// (offerTry.backFace) serves it with the probe in the graveyard.
//
// (The Lunar Whale static#0.1 left this slice earlier, at lb-setupparity:
// static_offer_self_attack_test.go.)
func TestStaticG7BackFaceGraveyardOfferServed(t *testing.T) {
	const name, key = "Emet-Selch, Unsundered", "static#1.0"
	reg := testutil.CorpusRegistry(t)
	c, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("%s not in the corpus", name)
	}
	for _, r := range levelb.Requirements(c) {
		if r.Key != key {
			continue
		}
		it, skip := templates.GenerateB(reg, name, r)
		if skip != nil {
			t.Fatalf("%s %s skipped: %s", name, key, skip.Reason)
		}
		p0 := it.Scenario.Setup["p0"]
		if len(p0.BackFace) != 1 || p0.BackFace[0] != name || len(p0.Graveyard) == 0 {
			t.Fatalf("setup p0 = %+v, want %s on its back face and a graveyard probe", p0, name)
		}
		return
	}
	t.Fatalf("precondition: %s carries no requirement %s", name, key)
}

// TestStaticOfferMayhemServed: Norman Osborn static#1.1 (Green Goblin's
// "Each nonland card in your graveyard has mayhem") was "grants only keywords
// outside the compared evergreen set". Mayhem lets a card be cast from the
// graveyard only if it was discarded this turn, so the probe starts in hand,
// p0's Mind Rot at itself discards it, and the graveyard cast is offered with
// the "(mayhem)" label -- on the back face, through the back-face retry.
func TestStaticOfferMayhemServed(t *testing.T) {
	const name, key = "Norman Osborn", "static#1.1"
	reg := testutil.CorpusRegistry(t)
	c, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("%s not in the corpus", name)
	}
	for _, r := range levelb.Requirements(c) {
		if r.Key != key {
			continue
		}
		it, skip := templates.GenerateB(reg, name, r)
		if skip != nil {
			t.Fatalf("%s %s skipped: %s", name, key, skip.Reason)
		}
		last := it.Scenario.Steps[len(it.Scenario.Steps)-1]
		if len(last.Expect) != 1 || last.Expect[0].Offered == nil || last.Expect[0].Offered.Label != "(mayhem)" {
			t.Fatalf("last step %+v, want an offered (mayhem) cast", last)
		}
		if p0 := it.Scenario.Setup["p0"]; len(p0.BackFace) != 1 || len(p0.Graveyard) != 0 {
			t.Fatalf("setup p0 = %+v, want the source on its back face and an empty graveyard (the probe is discarded this turn)", p0)
		}
		return
	}
	t.Fatalf("precondition: %s carries no requirement %s", name, key)
}
