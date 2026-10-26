package templates

import (
	"fmt"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/levelb"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/cards"
)

// activationsExhaustUnboundedItem serves Elvish Refueler's Activations static
// (MinLimit$ -1 over exhaust abilities, gated on no exhaust activation this
// turn): the card's own exhaust ability is activated on turn 1, then again in
// p0's next upkeep -- an activation exhaust alone forbids (CR 702.177a,
// "activate each exhaust ability only once") and the static re-allows. The
// second activation's effect is the compared trace; a gorge that withholds it
// fails the step.
func activationsExhaustUnboundedItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	const mode = "Activations"
	skip := func(why string) (oraclegen.Item, *oraclegen.Skip) {
		return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "static " + mode + " " + why}
	}
	idx := -1
	for i, sa := range f.Abilities {
		if strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKExhaust)), "True") && len(oraclegen.AbilitySlotSpecs(f, sa)) == 0 {
			idx = i
			break
		}
	}
	if idx < 0 {
		return skip("the card carries no untargeted exhaust ability to re-activate")
	}
	pool, why := oraclegen.PoolFor(f.Abilities[idx].ParamStr(cards.PKCost))
	if why != "" {
		return skip("exhaust cost unpayable: " + why)
	}
	exhaust := []oraclegen.Step{
		{Op: "activate", Seat: 0, Card: "p0:" + name, Mana: pool, AbilityIndex: intPtr(idx)},
		{Op: "resolve"},
	}
	steps := append([]oraclegen.Step(nil), exhaust...)
	// The exhaust ability has no timing restriction, so it is re-activated
	// in p0's next upkeep: one pass_to, which both engines stop at on turn 3
	// (a second pass_to to main1 ran XMage a full round further).
	steps = append(steps, oraclegen.Step{Op: "pass_to", Seat: 0, Step: "upkeep", Active: "p0"})
	steps = append(steps, exhaust...)
	sc := withBackFace(staticScenario(f, name, []string{name}, nil, steps), name, req)
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 {
		return skip(fmt.Sprintf("the exhaust ability is not re-activated on the next turn (ok=%v fails=%v)", ok, res.Fails))
	}
	it := oraclegen.NewLevelBItem(name, req.Key, StaticObserved.Version, []string{"702.177a", "602.5b"}, sc)
	it.XAnswers = oraclegen.XAnswersForScenario(reg, res, sc, oraclegen.ModeNumbers(f), nil)
	return it, nil
}
