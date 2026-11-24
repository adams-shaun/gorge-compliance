package main

import (
	"fmt"
	"slices"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/xprep"
	"github.com/adams-shaun/gorge/engine/rules"
)

// forgeTokenLooks rewrites a surveil/scry arrange whose looked-at cards are all
// tokens (xprep.TokenLook, the predicate XMage's keep-all lowering shares) so
// Forge keeps the cards it actually looks at.
//
// Gorge looks at the token put on top of the library (Vanish from Sight, CR
// 111.8: a token that left the battlefield cannot move on, so the pick moves
// nothing) and the card under it never enters the look. Forge drops the token
// when it arrives, so its look is that card. StepMachine.arrange matches the
// picks by name against the cards looked at: gorge's empty pick bins the card
// under the token (the xe:RAND:4:16:w8 divergence), and a pick naming the
// token matches nothing, so the controller falls back to Forge's own choice.
// What does hold in both readings is "keep what you look at", and the driver
// has no keep-all answer, so the pick names the cards: the top Options cards of
// the seat's library in the checkpoint after the step, where the tokens have
// ceased to exist (CR 704.5d), in top-first order.
//
// A decision whose step has no such checkpoint, or whose library top lists
// fewer than Options cards, is left as gorge answered it. The input is not
// mutated.
func forgeTokenLooks(g rules.OracleResult, ds []rules.OracleDecision) []rules.OracleDecision {
	out := ds
	cloned := false
	for i, d := range ds {
		if !xprep.TokenLook(d) || d.Step < 0 || d.Step+1 >= len(g.Snapshots) {
			continue
		}
		ps := g.Snapshots[d.Step+1].Players
		if d.Seat < 0 || d.Seat >= len(ps) {
			continue
		}
		top := ps[d.Seat].LibraryTop
		if len(top) < d.Options {
			continue
		}
		names := slices.Clone(top[:d.Options])
		refs := make([]string, len(names))
		idx := make([]int, len(names))
		for k, n := range names {
			refs[k] = fmt.Sprintf("p%d:%s", d.Seat, n)
			idx[k] = k
		}
		if !cloned {
			out = append([]rules.OracleDecision(nil), ds...)
			cloned = true
		}
		d.Picks, d.PickRefs, d.PickIdx = names, refs, idx
		d.ObjectPicks = nil
		out[i] = d
	}
	return out
}
