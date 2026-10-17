package templates_test

// static_exile_opponent_offer_test.go pins the opponent-owned exiled-with
// may-play row (ticket lb-mayplayopp): Azula, Cunning Usurper's ETB makes
// the target opponent exile a creature, and "you may cast cards exiled with
// Azula" must offer that OPPONENT-owned card to Azula's controller. The
// engine's may-play walk used to visit only the caster's own cards, so the
// row was skipped with a named gap; it is now served by running Azula's own
// cast ETB.

import (
	"slices"
	"testing"
)

func TestStaticExileOpponentOwnedRowIsServed(t *testing.T) {
	it, skip := zoneItem(t, "Azula, Cunning Usurper", "static#0.0")
	if skip != nil {
		t.Fatalf("skipped (%s), want served", skip.Reason)
	}
	// The served scenario offers p1's card, exiled by Azula's ETB from p1's
	// battlefield -- not a setup placement in exile.
	if p1 := it.Scenario.Setup["p1"]; len(p1.Exile) != 0 || len(p1.Battlefield) == 0 || p1.Battlefield[len(p1.Battlefield)-1] != "Hill Giant" {
		t.Fatalf("p1 setup = %+v, want the victim on p1's battlefield and nothing in exile", p1)
	}
	var offered bool
	for _, st := range it.Scenario.Steps {
		for _, ex := range st.Expect {
			if ex.Offered != nil && ex.Offered.Card == "p1:"+"Hill Giant" && ex.Want != nil && *ex.Want {
				offered = true
			}
		}
	}
	if !offered {
		t.Fatalf("scenario never asserts the opponent-owned cast offer: %+v", it.Scenario.Steps)
	}
}

// Null Summoner is the same opponent-owned class behind a Threshold gate: its
// ETB exiles from the opponent's HAND, and the condition prelude's seven-card
// graveyard turns the grant on.
func TestStaticExileOpponentOwnedGatedRowIsServed(t *testing.T) {
	it, skip := zoneItem(t, "Null Summoner", "static#0.0")
	if skip != nil {
		t.Fatalf("skipped (%s), want served", skip.Reason)
	}
	if p1 := it.Scenario.Setup["p1"]; len(p1.Exile) != 0 || !slices.Contains(p1.Hand, "Hill Giant") {
		t.Fatalf("p1 setup = %+v, want the victim in p1's hand and nothing in exile", p1)
	}
	if p0 := it.Scenario.Setup["p0"]; len(p0.Graveyard) < 7 {
		t.Fatalf("p0 graveyard = %v, want the threshold's seven cards", p0.Graveyard)
	}
}

// Maralen, Fae Ascendant is the class's Dig-exile shape: its enters trigger
// exiles the top two cards of target opponent's library, and "once each
// turn, you may cast a spell with mana value less than or equal to the
// number of Elves and Faeries you control from among cards exiled with
// Maralen this turn without paying its mana cost" must offer the OPPONENT's
// card. The engine already offered it (the Dig records the exiled-with
// association and the may-play walk covers opponent-owned cards); the row
// was skipped because no fixture exiled from a library. The cheap victim
// (mana value 1) meets the cmcLEX cap Maralen alone sets.
func TestStaticExileOpponentOwnedDigRowIsServed(t *testing.T) {
	it, skip := zoneItem(t, "Maralen, Fae Ascendant", "static#0.0")
	if skip != nil {
		t.Fatalf("skipped (%s), want served", skip.Reason)
	}
	if p1 := it.Scenario.Setup["p1"]; len(p1.Exile) != 0 || !slices.Contains(p1.LibraryTop, "Savannah Lions") {
		t.Fatalf("p1 setup = %+v, want the victim on top of p1's library and nothing in exile", p1)
	}
	var offered bool
	for _, st := range it.Scenario.Steps {
		for _, ex := range st.Expect {
			if ex.Offered != nil && ex.Offered.Card == "p1:Savannah Lions" && ex.Want != nil && *ex.Want {
				offered = true
			}
		}
	}
	if !offered {
		t.Fatalf("scenario never asserts the opponent-owned cast offer: %+v", it.Scenario.Steps)
	}
}
