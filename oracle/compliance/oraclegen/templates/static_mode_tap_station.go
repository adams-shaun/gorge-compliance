// Level-B observation of the Station-scoped TapPowerValue static (Tapestry
// Warden: "Each creature you control with toughness greater than its power
// stations permanents using its toughness rather than its power"). The engine
// reads the value in engine/rules/statics_assignment.go's tapPowerValue, the ONE read
// the Station effect's counter amount goes through, so the observation is the
// CHARGE COUNTER COUNT: p0 stations a Spacecraft probe by tapping the card
// itself (CR 702.184a, "Put charge counters equal to its power"), and the
// probe ends with the card's toughness in charge counters rather than its
// power. The card is the only other creature p0 controls, so the tap choice
// is forced in both engines.
package templates

import (
	"fmt"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/levelb"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/cards"
)

// stationProbe is the Spacecraft the card stations. Its only activated
// ability is Station, and its enters trigger does not fire on a setup
// placement in either engine.
const stationProbe = "Wedgelight Rammer"

// tapPowerValueStationItem serves the station-counter observation for one
// Station-scoped TapPowerValue requirement.
func tapPowerValueStationItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	skip := func(why string) (oraclegen.Item, *oraclegen.Skip) {
		return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "static TapPowerValue station " + why}
	}
	power, tough, ok := parsePT(f.PT)
	if !ok || tough <= power {
		return skip("the card's printed toughness does not exceed its power")
	}
	if _, ok := reg.Lookup(stationProbe); !ok {
		return skip("probe not in corpus")
	}
	// Station is an activated ability that uses the stack (CR 702.184a); the
	// engine offers it under its own option kind, selected by label (no IR
	// ability index), and XMage selects it by its keyword line. The resolve
	// step lets the ability resolve before the counters are read.
	steps := []oraclegen.Step{
		{Op: "activate", Seat: 0, Card: "p0:" + stationProbe, Ability: "Station"},
		{Op: "resolve"},
	}
	sc := withBackFace(staticScenario(f, name, []string{name, stationProbe}, nil, steps), name, req)
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 {
		return skip(fmt.Sprintf("the station does not replay (ok=%v fails=%v)", ok, res.Fails))
	}
	p, ok := permByNameIn(res, stationProbe)
	if !ok || p.Counters["CHARGE"] != int32(tough) {
		return skip(fmt.Sprintf("the probe holds %d charge counters, want the card's toughness %d", p.Counters["CHARGE"], tough))
	}
	it := oraclegen.NewLevelBItem(name, req.Key, StaticObserved.Version, []string{"702.184a"}, sc)
	it.XAnswers = oraclegen.XAnswersForScenario(reg, res, sc, oraclegen.ModeNumbers(f), nil)
	it.XAbility = []string{"Station", ""}
	return it, nil
}
