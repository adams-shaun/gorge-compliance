// Level-B observation of a mana-only AlternativeCost static granted to every
// spell its controller casts (Leyline of Mutation: "You may pay
// {W}{U}{B}{R}{G} rather than pay the mana cost for spells that you cast",
// CR 118.9). p0 casts a probe creature electing the alternative cost (the
// runner's cast_mode "alternative", the engine's AltCostIndex-1 cast option)
// from exactly the alternative's mana; the control without the card offers
// no such option, so the elected cast fails there.
package templates

import (
	"fmt"
	"strconv"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/levelb"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/cards"
)

// alternativeCostCastMode is the runner's cast_mode electing the first
// alternative-cost cast option (engine/rules/oracle_run_faces.go oracleAltCostMode).
const alternativeCostCastMode = "alternative"

// alternativeCostProbe is the spell cast for the alternative cost.
const alternativeCostProbe = "Grizzly Bears"

// alternativeCostSpellsItem serves static.alternative-cost-spells.
func alternativeCostSpellsItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	const mode = "AlternativeCost"
	i, err := strconv.Atoi(req.Slot)
	if err != nil || i < 0 || i >= len(f.Statics) {
		return staticSkip(name, mode, "requirement slot names no static")
	}
	pool, why := oraclegen.PoolFor(f.Statics[i].ParamStr(cards.PKCost))
	if why != "" {
		return staticSkip(name, mode, "alternative cost unpayable: "+why)
	}
	steps := []oraclegen.Step{
		{Op: "cast", Seat: 0, Card: "p0:" + alternativeCostProbe, Mana: pool, CastMode: alternativeCostCastMode},
		{Op: "resolve"},
	}
	control := staticScenario(f, name, nil, []string{alternativeCostProbe}, steps)
	if cres, ok := runStatic(reg, control); ok && len(cres.Fails) == 0 {
		return staticSkip(name, mode, "control elects an alternative cost without the card")
	}
	sc := withBackFace(staticScenario(f, name, []string{name}, []string{alternativeCostProbe}, steps), name, req)
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 || !onBattlefield(res, "p0:"+alternativeCostProbe) {
		return staticSkip(name, mode, fmt.Sprintf("the probe is not cast for the alternative cost (ok=%v fails=%v)", ok, res.Fails))
	}
	if pool := res.Snapshots[len(res.Snapshots)-1].Players[0].Pool; pool != "" {
		return staticSkip(name, mode, "the alternative cost left "+pool+" floating")
	}
	it, skip := finishLegalityItem(reg, f, name, mode, req, sc, []string{"118.9", "601.2b"})
	if skip != nil {
		return it, skip
	}
	// XMage poses the alternative cost as a "use the alternative cost?"
	// chooseUse that gorge's elected cast option answers implicitly, so the
	// derived stream needs the yes lead (the optional-cost cast's shape).
	if len(it.XAnswers) < len(it.Steps) {
		grown := make([][]oraclegen.XAnswer, len(it.Steps))
		copy(grown, it.XAnswers)
		it.XAnswers = grown
	}
	it.XAnswers[0] = append([]oraclegen.XAnswer{{Seat: 0, Kind: "choice", Value: "yes"}}, it.XAnswers[0]...)
	return it, nil
}

// alternativeCostEquipProbe is the Equipment whose equip ability the
// alternative prices: an artifact, so with the card (a legendary) and the
// Storied fixture's Sol Ring it completes the enduring story's three.
const alternativeCostEquipProbe = "Bonesplitter"

// alternativeCostEquipItem serves static.alternative-cost-equip (Kíli the
// Resourceful): with an enduring story p0 equips the probe onto the card
// electing the alternate-cost option and paying no mana (CR 118.9). The
// control drops the Sol Ring, so no enduring story starts (CR 702.175a) and
// the alternate option is not offered.
func alternativeCostEquipItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	const mode = "AlternativeCost"
	steps := []oraclegen.Step{
		{Op: "activate", Seat: 0, Card: "p0:" + alternativeCostEquipProbe, Ability: "(alternate cost)",
			Targets: []string{"p0:" + name}},
		{Op: "resolve"},
	}
	control := staticScenario(f, name, []string{name, alternativeCostEquipProbe}, nil, steps)
	if cres, ok := runStatic(reg, control); ok && len(cres.Fails) == 0 {
		return staticSkip(name, mode, "control equips for the alternative cost without an enduring story")
	}
	sc := staticScenario(f, name, []string{name, "Sol Ring", alternativeCostEquipProbe}, nil, steps)
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 {
		return staticSkip(name, mode, fmt.Sprintf("the equip is not activated for the alternative cost (ok=%v fails=%v)", ok, res.Fails))
	}
	it, skip := finishLegalityItem(reg, f, name, mode, req, sc, []string{"118.9", "702.6a"})
	if skip != nil {
		return it, skip
	}
	// The equip target travels on the activate step (the driver queues a
	// step's targets), so it is not scripted a second time as an answer.
	it.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f), map[int]bool{0: true})
	if len(it.XAnswers) < len(it.Steps) {
		grown := make([][]oraclegen.XAnswer, len(it.Steps))
		copy(grown, it.XAnswers)
		it.XAnswers = grown
	}
	it.XAnswers[0] = append([]oraclegen.XAnswer{{Seat: 0, Kind: "choice", Value: "yes"}}, it.XAnswers[0]...)
	if probe, ok := reg.Lookup(alternativeCostEquipProbe); ok && len(probe.Faces) > 0 {
		if prefixes, why := oraclegen.XMageAbility(probe.Faces[0]); why == "" {
			if prefix := prefixes[0]; prefix != "" {
				it.XAbility = []string{prefix, ""}
			}
		}
	}
	return it, nil
}
