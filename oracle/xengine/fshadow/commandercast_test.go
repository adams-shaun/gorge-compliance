package fshadow

import "testing"

// TestCommanderCastPaidThroughAFilterLand: a commander cast from the command
// zone whose mana needs a filter land's, which gorge neither plans nor offers,
// follows to the end of the log. The line lists the one cast with no ability,
// as Forge's recorder does, and the closing snapshot holds gorge to the board
// with the commander resolved.
func TestCommanderCastPaidThroughAFilterLand(t *testing.T) {
	for name, game := range map[string]func(*testing.T) *Log{
		// Farm cmdr4 BIG-s142 pod 14201, line 417.
		"ferrous lake": shikoGame,
		// The colour to back out of is another source's, not the one the
		// filter's mana leaves short.
		"skycloud expanse": func(t *testing.T) *Log {
			return handGame(t, corpus(t), shiko, []string{"Command Tower", "Skycloud Expanse", "Sol Ring"}, []handStep{
				{land: "Command Tower"},
				{land: "Skycloud Expanse"},
				{plan: "Sol Ring"},
				{use: shiko, taps: []string{"Sol Ring", "Skycloud Expanse", "Command Tower:R"}},
			})
		},
	} {
		t.Run(name, func(t *testing.T) {
			l := game(t)
			a, _ := lastAsk(t, l, "cast")
			seat := l.Header.Seats[0]
			if want := seat.Cards[seat.Commanders[0]].ID; a.Answer.Card != want {
				t.Fatalf("the paid cast is of card %d, want the commander %d", a.Answer.Card, want)
			}
			a.Options = []Option{{Action: "cast", Card: a.Answer.Card}}
			if res := followClean(t, l, 1); res.End != "log" {
				t.Errorf("end %q, want the whole log followed", res.End)
			}
		})
	}
}
