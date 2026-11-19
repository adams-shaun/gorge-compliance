package lower

import (
	"fmt"
	"slices"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/cov"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestLowerFundsResolveTimePayment: gorge's resolve-time planner taps the
// payer's lands for a "you may pay {1}" trigger and logs the taps on the
// election's own entry. Forge and XMage pay from the floating pool only, so
// the lowering must float them: an explicit tap step, with no live checkpoint,
// ahead of the payer's pass, and the lowered scenario must still self-check.
func TestLowerFundsResolveTimePayment(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	names := []string{"Oreplate Pangolin", "Squire's Lightblade", "Ornithopter", "Mind Stone", "Arcane Signet",
		"Fellwar Stone", "Darksteel Relic", "Thran Dynamo", "Mountain", "Plains", "Sol Ring", "Pilgrim's Eye",
		"Wayfarer's Bauble", "Skullclamp", "Bonesplitter", "Short Sword", "Prismatic Lens", "Millikin", "Hedron Archive",
		"Swiftfoot Boots", "Walking Ballista", "Lightning Bolt", "Shock", "Grizzly Bears", "Goblin Guide", "Savannah Lions",
		"Pyrite Spellbomb", "Chromatic Star", "Burnished Hart", "Wurmcoil Engine", "Solemn Simulacrum", "Juggernaut",
		"Lightning Helix", "Boros Charm", "Raise the Alarm", "Serra Angel", "Shivan Dragon"}
	cands, _ := decks.CandidatesNamed(reg, names, decks.Options{})
	if len(cands) == 0 {
		t.Fatal("no candidates")
	}
	var funded, unfunded int
	var gaps []string
	for g := 0; g < 60; g++ {
		pair := podOf(decks.Pair(cands, cov.New(), "EOE", 0, g, 5))
		ls, _, _, err := Windows(reg, pair, uint64(7000+g), game.Options{Set: "EOE"}, false)
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range ls {
			limit := len(l.Against.Entries)
			if l.Truncated != "" {
				limit = l.TruncatedAt
			}
			m := seatMap{swap: l.Swap}
			for k := 0; k < limit; k++ {
				e := l.Against.Entries[k]
				if !isResolvePay(e) || len(e.Mana) == 0 {
					continue
				}
				for _, mr := range e.Mana {
					ok := false
					for si, st := range l.Item.Steps {
						// A funding tap is followed by the payer's pass, which is
						// not checked against the tape (the pool holds the mana).
						if st.Op == "activate" && st.Card == m.ref(mr.Ref) && l.StepEntry[si] < 0 &&
							si+1 < len(l.Item.Steps) && l.Item.Steps[si+1].Op == "pass" && l.StepEntry[si+1] < 0 {
							ok = true
						}
					}
					if ok {
						funded++
					} else {
						unfunded++
						gaps = append(gaps, fmt.Sprintf("%s entry %d %s", l.Item.ID, k, mr.Ref))
					}
				}
				if c := CheckWindow(reg, l); c.Status != StatusOK {
					t.Errorf("%s: %s at step %d, %s: gorge %s, tape %s", l.Item.ID, c.Status, c.Step, c.Field, c.Gorge, c.Tape)
				}
			}
		}
	}
	if funded == 0 {
		t.Fatal("no funded resolve-time payment in 60 games: nothing was checked")
	}
	if unfunded > 0 {
		t.Errorf("%d funding taps were not lowered ahead of the payer's pass: %v", unfunded, gaps)
	}
}

// TestLowerFundingTapCarriesSpend: the funding taps of a resolve-time payment
// replay the activations gorge's planner fired to fund it, and each
// activation's own cost is pool mana gorge spent doing so. The recorder puts
// the election entry's whole spend — the funding activations' own costs (an
// Azorius Signet's {1}, paid here with a floating U), in activation order,
// followed by the resolve-time charge itself — on the trigger_cost_pay entry,
// so the funding path must split it the way lowerGroup splits a mana-window
// ask's: each activation's own Cost$ units ride the tap step that activation
// is lowered as (the only step kind any follower reads a payment from —
// oraclegen's Payment is "the pool mana a cast or activate step must spend"),
// and the rest is the resolve-time charge, which the follower engine pays from
// its floating pool at the resolve step, so spending it early would leave the
// pool short of exactly those units at the resolve's checkpoint. Dropping the
// whole spend instead (the old empty-pay pushFunding) replayed a pool the live
// game never had: the follower paid the Signet's {1} with its own split.
//
// An election's commit can fund the payment with several activations, so the
// per-record split must follow activation order: one entry's whole spend is
// not one activation's.
func TestLowerFundingTapCarriesSpend(t *testing.T) {
	reg := cards.NewRegistry()
	for _, name := range []string{"Azorius Signet", "Dimir Signet"} {
		reg.Add(&cards.Card{Faces: []*cards.Face{{
			Name:      name,
			Abilities: []*cards.SA{{Params: map[string]string{"Cost": "1 T"}}},
		}}})
	}
	reg.Add(&cards.Card{Faces: []*cards.Face{{
		Name:      "Island",
		Abilities: []*cards.SA{{Params: map[string]string{"Cost": "T"}}},
	}}})
	pass := game.Entry{Kind: "priority", Seat: 0, Picks: []game.Pick{{Kind: "pass"}}, StackTop: 1}
	sig := game.ManaRec{Obj: 9, Name: "Azorius Signet", Ability: 0, Ref: "p0:Azorius Signet", Player: 0}
	dimir := game.ManaRec{Obj: 10, Name: "Dimir Signet", Ability: 0, Ref: "p0:Dimir Signet", Player: 0}
	island := game.ManaRec{Obj: 11, Name: "Island", Ability: 0, Ref: "p0:Island", Player: 0}
	ask := func(mana []game.ManaRec, spent string) game.Entry {
		return game.Entry{
			Kind: "choose_n", Seat: 0,
			Picks: []game.Pick{{Kind: "trigger_cost_pay", Obj: 7, Ref: "p0:Glorious Attendant"}},
			Mana:  mana, Spent: spent,
		}
	}
	for _, tc := range []struct {
		name  string
		mana  []game.ManaRec
		spent string
		want  []string
	}{
		{"the activation's own cost is not dropped", []game.ManaRec{sig}, "U", []string{"U"}},
		{"the resolve-time charge stays with the resolve", []game.ManaRec{sig}, "UU", []string{"U"}},
		{"a free or unspent activation pays nothing", []game.ManaRec{sig}, "", []string{""}},
		{
			"the paid activation keeps its cost when a free land follows",
			[]game.ManaRec{sig, island}, "UU", []string{"U", ""},
		},
		{
			"a free activation before a paid one does not take its cost",
			[]game.ManaRec{island, sig}, "U", []string{"", "U"},
		},
		{
			"each paid activation gets its own cost",
			[]game.ManaRec{sig, dimir}, "UU", []string{"U", "U"},
		},
		{
			"a charge after two paid activations stays with the resolve",
			[]game.ManaRec{sig, dimir}, "UUU", []string{"U", "U"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var l Lowered
			l.StepEntry = []int{}
			fund := l.fundingTaps(reg, seatMap{}, []game.Entry{pass, ask(tc.mana, tc.spent)}, 0, 2, nil, 0, false, false)
			if fund.refused {
				t.Fatal("funding refused")
			}
			if len(fund.taps) != len(tc.mana) {
				t.Fatalf("taps = %+v, want one per mana record", fund.taps)
			}
			for k, mr := range tc.mana {
				if fund.taps[k].Card != mr.Ref {
					t.Errorf("tap %d = %q, want %q (activation order)", k, fund.taps[k].Card, mr.Ref)
				}
			}
			l.pushFunding(fund)
			if !slices.Equal(l.Item.Payment, tc.want) {
				t.Errorf("funding tap payments = %v, want %v (each activation's own spend)", l.Item.Payment, tc.want)
			}
		})
	}
}
