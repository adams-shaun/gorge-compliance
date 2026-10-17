package templates

import (
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/levelb"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/cards"
)

// webSlingingProbe is the legendary coloured spell a web-slinging grant
// reaches: its printed {2}{R}{R} is out of reach of the grant's pool, so only
// the web-slinging cost makes it castable.
const webSlingingProbe = "Krenko, Mob Boss"

// webSlingingTapper is the Vehicle whose crew taps the creature web-slinging
// returns.
const webSlingingTapper = "Smuggler's Copter"

// staticWebSlingingGrantItem serves a stack-scoped Web-slinging grant (the
// Amazing Spider-Man face's "Each legendary spell you cast that's one or more
// colors has web-slinging {G}{W}{U}"). Web-slinging (CR 702.188a) is an
// alternative cost that also returns a tapped creature you control to its
// owner's hand, so the scenario taps a creature by crewing a Vehicle with it,
// floats exactly the web-slinging mana and asserts the probe is offered. The control without the card asserts it is not.
func staticWebSlingingGrantItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, st cards.Static) (oraclegen.Item, bool) {
	kw := strings.TrimSpace(st.ParamStr(cards.PKAddKeyword))
	rest, ok := strings.CutPrefix(kw, "Web-slinging:")
	if !ok || strings.Contains(kw, " & ") {
		return oraclegen.Item{}, false
	}
	cost, _, _ := strings.Cut(rest, ":")
	pool, why := oraclegen.PoolFor(cost)
	if why != "" || pool == "" {
		return oraclegen.Item{}, false
	}
	if _, ok := reg.Lookup(webSlingingProbe); !ok {
		return oraclegen.Item{}, false
	}
	// The tapped creature is a crew tap (Smuggler's Copter's Crew 1): an
	// attack would tap one too, but the XMage driver hoists an offered
	// assertion after combat ahead of the attack checkpoint (the harness row
	// The Lunar Whale shares), so the scenario stays in main 1.
	copter, ok := reg.Lookup(webSlingingTapper)
	if !ok || len(copter.Faces) == 0 {
		return oraclegen.Item{}, false
	}
	crew, crewXab, taps, _, ok := crewPrelude(copter.Faces[0], webSlingingTapper)
	if !ok {
		return oraclegen.Item{}, false
	}
	crew = append([]oraclegen.Step(nil), crew...)
	for i := range crew {
		if crew[i].Op == "activate" {
			crew[i].Answers = append(append([]oraclegen.Answer(nil), crew[i].Answers...), oraclegen.Answer{Kind: "choose", Pick: taps})
		}
	}
	steps := func(want bool) []oraclegen.Step {
		out := append([]oraclegen.Step{{Op: "pass_to", Seat: 0, Step: "main1", Decision: "priority"}}, crew...)
		return append(out,
			oraclegen.Step{Op: "mana", Seat: 0, Mana: pool},
			oraclegen.Step{Op: "pass_to", Seat: 0, Decision: "priority", Expect: []oraclegen.Expect{
				{Offered: &oraclegen.Offered{Seat: 0, Kind: offerKindCast, Card: "p0:" + webSlingingProbe}, Want: boolPtr(want)},
			}})
	}
	board := append([]string{webSlingingTapper}, taps...)
	control := staticScenario(f, name, board, []string{webSlingingProbe}, steps(false))
	if res, ok := runStatic(reg, control); !ok || len(res.Fails) != 0 {
		return oraclegen.Item{}, false
	}
	sc := withBackFace(staticScenario(f, name, append([]string{name}, board...), []string{webSlingingProbe}, steps(true)), name, req)
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 {
		return oraclegen.Item{}, false
	}
	it := oraclegen.NewLevelBItem(name, req.Key, StaticApplies.Version, []string{"611.3", "601.2b"}, sc)
	it.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f), nil)
	it.XAbility = make([]string, len(sc.Steps))
	copy(it.XAbility[1:], crewXab)
	return it, true
}
