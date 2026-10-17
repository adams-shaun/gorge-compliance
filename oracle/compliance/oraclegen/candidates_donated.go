package oraclegen

// donatedCandidates serves a "permanent you own but don't control" target
// (Permanent.YouOwn+YouDontCtrl, Coveted Falcon's "gain control of target
// permanent you own but don't control"). Setup places every card under its
// owner's control, so no placement makes the filter hold; a prelude does: p0
// activates Humble Defector ("{T}: Draw two cards. Target opponent gains
// control of Humble Defector. Activate only during your turn."), and the card
// under test then targets the p0-owned Defector p1 controls.
//
// The giver's one target is the opponent, so XMage's target queue holds
// exactly what the prelude consumes. The spell-shaped donors were measured
// against the XMage driver and fail: Donate's two-target cast leaves the
// driver's trailing cast skip at the head of the queue, where the card's own
// 1..1 target ask rejects it, and Akroan Horse's "an opponent gains control"
// is a chooseTarget(TargetOpponent) that gorge records as a choose, so the
// queued answer lands on the wrong queue. A base the Defector (a creature)
// does not satisfy fails closed.
func donatedCandidates(base string) []cand {
	switch base {
	case "permanent", "card", "creature":
	default:
		return nil
	}
	ability := 0
	return []cand{{seat: "p0", zone: "battlefield", card: donatedProbe, pre: []Step{
		{Op: "activate", Seat: 0, Card: "p0:" + donatedProbe, AbilityIndex: &ability, Targets: []string{"p1"}},
		{Op: "resolve"},
	}}}
}

// donatedProbe is the self-donating prelude permanent (a 2/1 creature in the
// corpus and in XMage's card database).
const donatedProbe = "Humble Defector"
