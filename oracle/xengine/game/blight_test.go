package game

import (
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge/engine/decision"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// A triggered "you may blight N. If you do, ..." is declined by every seat: the
// followers cannot answer a paid blight (see blightDecline), and the decline is
// the only way the ask leaves no unanswered cost on either of them.
func TestBlightDecline(t *testing.T) {
	pay := decision.Option{Index: 0, Kind: "trigger_cost_pay", Label: "Blight 2"}
	no := decision.Option{Index: 1, Kind: "trigger_cost_decline", Label: "Do not pay"}
	for _, c := range []struct {
		name string
		d    decision.Decision
		want int
	}{
		{"blight", decision.Decision{Kind: decision.KChoose, Options: []decision.Option{pay, no}}, 1},
		{"decline listed first", decision.Decision{Kind: decision.KChoose, Options: []decision.Option{
			{Index: 0, Kind: "trigger_cost_decline"}, {Index: 1, Kind: "trigger_cost_pay", Label: "Blight 1"}}}, 0},
		{"mana cost", decision.Decision{Kind: decision.KChoose, Options: []decision.Option{
			{Index: 0, Kind: "trigger_cost_pay", Label: "Pay 2"}, no}}, -1},
		{"another kind", decision.Decision{Kind: decision.KPriority, Options: []decision.Option{pay, no}}, -1},
		{"no decline offered", decision.Decision{Kind: decision.KChoose, Options: []decision.Option{pay}}, -1},
	} {
		if got := blightDecline(c.d); got != c.want {
			t.Errorf("%s: blightDecline = %d, want %d", c.name, got, c.want)
		}
	}
}

// A game whose seat holds the blight creatures meets the ask, and every
// recorded answer to it is the decline.
func TestBlightAskIsDeclinedInPlay(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	blight := decks.Deck{Name: "blight", Colours: "B"}
	for i := 0; i < 20; i++ {
		blight.Cards = append(blight.Cards, "Blighted Blackthorn", "Swamp")
	}
	other := decks.Deck{Name: "other", Colours: "B"}
	for i := 0; i < 40; i++ {
		other.Cards = append(other.Cards, "Swamp")
	}
	pair := [2]decks.Deck{blight, other}
	asks := 0
	for seed := uint64(1); seed <= 4; seed++ {
		for _, e := range play(t, reg, pair, seed, Options{Set: "XP"}).Entries {
			for _, p := range e.Picks {
				if p.Kind == "trigger_cost_pay" && len(p.Label) > 6 && p.Label[:6] == "Blight" {
					t.Errorf("seed %d: the blight was paid (%q)", seed, p.Label)
				}
				if p.Kind == "trigger_cost_decline" && p.Ref == "p0:Blighted Blackthorn" {
					asks++
				}
			}
		}
	}
	if asks == 0 {
		t.Fatal("no game met the blight ask: the test pins nothing")
	}
}
