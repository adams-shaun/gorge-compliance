package mtgish

import (
	"testing"

	"github.com/adams-shaun/gorge/engine/cards"
)

func TestP2HParamTokens(t *testing.T) {
	for _, tc := range []struct {
		name string
		sa   cards.SA
		want string
	}{
		{"explore twice", cards.SA{API: "Explore", Params: map[string]string{"Num": "2"}}, "explore"},
		{"explore once", cards.SA{API: "Explore", Params: map[string]string{}}, "-"},
		{"skip two turns", cards.SA{API: "SkipTurn", Params: map[string]string{"NumTurns": "2"}}, "skipturn"},
		{"landwalk of the sacrificed land", cards.SA{API: "Pump", Params: map[string]string{"DefinedLandwalk": "Sacrificed"}}, "grant:landwalk"},
	} {
		if got := joinTokens(p2hParamTokens(&tc.sa)); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestP2HAdjust(t *testing.T) {
	a := &cards.SA{API: "TaporUntapAll"}
	if got := joinTokens(p2hAdjust(a, []string{"tapuntap"})); got != "tap untap" {
		t.Errorf("tap or untap all: %q", got)
	}
	b := &cards.SA{API: "TapOrUntap"}
	if got := joinTokens(p2hAdjust(b, []string{"tapuntap"})); got != "tapuntap" {
		t.Errorf("tap or untap one: %q", got)
	}
}
