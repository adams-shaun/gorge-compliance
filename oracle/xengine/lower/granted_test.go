package lower

import (
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
)

// TestLowerGrantedAbilityIdentity: a granted, gained or keyword-granted
// activated ability's pick is not the card's printed Face.Abilities[Ability]
// (an SVar grant's Ability aliases printed index 0, a keyword grant's is -1),
// so its step must carry the engine's offered label in Step.Ability beside
// the index — the runner matches the label beside the index, and the follower
// exporters resolve the ability by it (Dalakos's {T} mana ability must not be
// exported for Dragon Mantle's granted {R} pump, xe:RAND:1:4:w14/w22). A
// printed ability pick carries no grant identity and lowers to the same
// index-only step as before.
func TestLowerGrantedAbilityIdentity(t *testing.T) {
	const ref = "p0:Dalakos, Crafter of Wonders"
	const label = "Dalakos, Crafter of Wonders: CARDNAME gets +1/+0 until end of turn."
	d := game.Digest{Turn: 1, Step: "main1", Active: 0, Priority: 0}
	prio := func(p game.Pick) game.Entry {
		return game.Entry{Kind: "priority", Seat: 0, Turn: 1, Step: "main1",
			Digest: d, Picks: []game.Pick{p}}
	}
	cases := []struct {
		name    string
		pick    game.Pick
		wantAb  string
		wantIdx int
	}{
		{"svar_granted", game.Pick{Kind: "ability", Obj: stateID(9), Ref: ref, Ability: 0,
			Label: label, SVar: "Pump", GrantSource: stateID(4)}, label, 0},
		{"gained", game.Pick{Kind: "ability", Obj: stateID(9), Ref: ref, Ability: 0,
			Label: label, GainedSource: stateID(6), GainedIdx: 3}, label, 0},
		{"keyword_granted", game.Pick{Kind: "ability", Obj: stateID(9), Ref: ref, Ability: -1,
			Label: label, Keyword: "Cycling:1 U"}, label, -1},
		{"printed", game.Pick{Kind: "ability", Obj: stateID(9), Ref: ref, Ability: 0,
			Label: ref + ": Add {C}{C}."}, "", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			steps, why := lowerPriority(nil, seatMap{}, prio(tc.pick), nil)
			if why != nil {
				t.Fatalf("lowerPriority: %s", why.reason)
			}
			if len(steps) != 1 {
				t.Fatalf("steps = %+v, want one", steps)
			}
			st := steps[0]
			if st.Op != "activate" || st.Card != ref {
				t.Fatalf("step = %+v, want an activate of %s", st, ref)
			}
			if st.AbilityIndex == nil || *st.AbilityIndex != tc.wantIdx {
				t.Errorf("ability_index = %v, want %d", st.AbilityIndex, tc.wantIdx)
			}
			if st.Ability != tc.wantAb {
				t.Errorf("ability = %q, want %q", st.Ability, tc.wantAb)
			}
		})
	}
}
