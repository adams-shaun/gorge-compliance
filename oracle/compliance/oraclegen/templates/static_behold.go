package templates

import (
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/cards"
)

// staticBeholdFixtures is the fixture for a static on a card whose own cast
// carries an additional "behold and exile" cost (Champion of the Clachan),
// observed on a probe that is itself a legal behold choice. The hand holds a
// spare card of the beholden type and the cast's behold pick is scripted to
// it: left to the default the cast exiles the first option, the battlefield
// probe, before the checkpoint can read the static on it. reanswers carries
// the scripted pick to XMage's cost dialog.
func staticBeholdFixtures(f *cards.Face) []staticFixture {
	typ := requiredBeholdType(f)
	if typ == "" || beholdFixture[typ] == "" {
		return nil
	}
	spare := beholdFixture[typ]
	return []staticFixture{{
		conditionPrelude: conditionPrelude{hand: []string{spare}},
		beholdPick:       spare,
		reanswers:        true,
	}}
}

// answerBeholdCost scripts the behold pick of the card's own cast step.
func answerBeholdCost(base *oraclegen.Item, name, want string) bool {
	return scriptSingleChoose(base, func(st oraclegen.Step) bool {
		return st.Op == "cast" && st.Card == "p0:"+name
	}, want)
}

// beholdFromHand answers XMage's behold location ask ahead of the scripted
// hand pick. With a beholdable permanent on the battlefield AND one in hand,
// BeholdType.beholdOne first asks chooseUse "Choose controlled" / "Reveal from
// hand"; unscripted, the pick's card name reaches the battlefield
// TargetPermanent instead and is rejected (Champion of the Clachan). The
// fixture always has both: the battlefield probe and the spare in hand.
func beholdFromHand(answers [][]oraclegen.XAnswer, steps []oraclegen.Step, name, pick string) {
	for i, st := range steps {
		if st.Op != "cast" || st.Card != "p0:"+name || i >= len(answers) {
			continue
		}
		for k, a := range answers[i] {
			if a.Kind == "choice" && a.Value == pick {
				answers[i] = append(answers[i][:k:k], append([]oraclegen.XAnswer{{Seat: a.Seat, Kind: "choice", Value: "no"}}, answers[i][k:]...)...)
				return
			}
		}
	}
}
