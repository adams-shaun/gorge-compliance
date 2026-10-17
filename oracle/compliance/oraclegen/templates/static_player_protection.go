package templates

import (
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/levelb"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/cards"
)

// staticPlayerProtectionItem serves a grant of protection from opponents to
// the card's controller (Absolute Virtue's "You have protection from each of
// your opponents"): the keyword lands on a player, whose keywords no snapshot
// carries, so it is observed through what it does. CR 702.16b/702.16j: a
// player with protection from a player can't be dealt damage by sources that
// player controls. The card sits on p1's side and p0 attacks p1 with an
// unblocked Grizzly Bears: p1 takes no combat damage with the card and 2
// without it (the control).
func staticPlayerProtectionItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, st cards.Static) (oraclegen.Item, bool) {
	if !strings.EqualFold(strings.TrimSpace(st.ParamStr(cards.PKAffected)), "You") ||
		!strings.HasPrefix(strings.TrimSpace(st.ParamStr(cards.PKAddKeyword)), "Protection:Player.Opponent") {
		return oraclegen.Item{}, false
	}
	steps := []oraclegen.Step{
		{Op: "attack", Seat: 0, Attackers: []string{"p0:" + staticProbe}, Defender: "p1"},
		{Op: "pass_to", Seat: 0, Step: "main2"},
	}
	build := func(card bool) oraclegen.Scenario {
		sc := staticScenario(f, name, []string{staticProbe}, nil, steps)
		if card {
			p1 := sc.Setup["p1"]
			p1.Battlefield = appendFixtureUnique(p1.Battlefield, name)
			sc.Setup["p1"] = p1
		}
		return sc
	}
	control, ok := runStatic(reg, build(false))
	if !ok || len(control.Fails) != 0 || len(control.Snapshots) == 0 || lifeOf(control, 1) >= 20 {
		return oraclegen.Item{}, false
	}
	sc := build(true)
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 || len(res.Snapshots) == 0 || lifeOf(res, 1) <= lifeOf(control, 1) {
		return oraclegen.Item{}, false
	}
	it := oraclegen.NewLevelBItem(name, req.Key, StaticApplies.Version, []string{"611.3", "702.16j"}, sc)
	it.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f), nil)
	return it, true
}
