package lower

import "github.com/adams-shaun/gorge/engine/rules"

// cipherResume is the ResumeKind of gorge's cipher encode ask (CR 702.99a,
// engine/effects/cipher.go): one KModes decision, Min 0 Max 1, over the
// controller's creatures. An empty pick declines; one pick encodes the card
// on that creature.
const cipherResume = "cipher"

// CipherAsYesNo is decisions with each cipher encode answer reshaped into the
// two asks Forge poses: the "Do you want to exile <card> and encode it onto a
// creature you control?" confirm, then (only when the card is encoded) the
// creature choice. gorge's single KModes answer matches neither ask, so left
// as is the confirm is a strict miss and the pick an unconsumed leftover.
//
// A decline (no pick) becomes the yes/no decision answered "no". An encode
// becomes that decision answered "yes" followed by the original decision,
// unchanged, which is the creature choice. The yes/no decision has the shape
// of the explore rewrite: a choose_n whose single pick has kind yes or no and
// no object refs, on the same step and seat. The input slice is not modified;
// a list with no cipher answer is returned as is.
func CipherAsYesNo(decisions []rules.OracleDecision) []rules.OracleDecision {
	has := false
	for _, d := range decisions {
		if isCipherAsk(d) {
			has = true
			break
		}
	}
	if !has {
		return decisions
	}
	out := make([]rules.OracleDecision, 0, len(decisions)+1)
	for _, d := range decisions {
		if !isCipherAsk(d) {
			out = append(out, d)
			continue
		}
		yn := rules.OracleDecision{
			Step: d.Step, Seat: d.Seat, Kind: "choose_n", Options: 2, Min: 1, Max: 1,
			GorgeKind: d.GorgeKind, Resume: d.Resume, Via: d.Via, First: "Yes",
			Picks: []string{"No"}, PickIdx: []int{1}, PickKinds: []string{"no"},
		}
		if len(d.Picks) == 0 && len(d.PickRefs) == 0 {
			out = append(out, yn)
			continue
		}
		yn.Picks, yn.PickIdx, yn.PickKinds = []string{"Yes"}, []int{0}, []string{"yes"}
		out = append(out, yn, d)
	}
	return out
}

// isCipherAsk reports gorge's cipher encode decision.
func isCipherAsk(d rules.OracleDecision) bool { return d.Kind == "mode" && d.Resume == cipherResume }
