// Level-B observation of a continuous static that grants the activated
// abilities of the creature cards exiled with its source to the creatures
// you control with +1/+1 counters (Agatha's Soul Cauldron: "Creatures you
// control with +1/+1 counters on them have all activated abilities of all
// creature cards exiled with Agatha's Soul Cauldron", CR 613.1f). Setup can
// place neither half (an exile placement is bound to no source, CR 406.6), so
// the source's own "{T}: Exile target card from a graveyard. When a creature
// card is exiled this way, put a +1/+1 counter on target creature you
// control" makes both: it exiles a graveyard creature with a tap ability and
// counters the probe creature, which then activates the gained ability. The
// gained ping on p1 is the snapshot-visible effect.
package templates

import (
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/levelb"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/cards"
)

// gainsExiledWithCard is the exiled creature card whose activated ability is
// granted: Prodigal Pyromancer's "{T}: deal 1 damage to any target".
const gainsExiledWithCard = "Prodigal Pyromancer"

// gainsExiledWithHolder is the creature that gets the counter and the grant:
// vanilla, and named like no baseline permanent, so XMage binds it by name.
const gainsExiledWithHolder = "Hill Giant"

// gainsExiledWithItem serves the shape; ok is false for any other static or
// a face without the exile-and-counter activation.
func gainsExiledWithItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, st cards.Static) (oraclegen.Item, bool) {
	if !strings.EqualFold(st.ParamStr(cards.PKGainsAbilitiesOf), "Creature.ExiledWithSource") ||
		!strings.Contains(st.ParamStr(cards.PKAffected), "counters_GE1_P1P1") {
		return oraclegen.Item{}, false
	}
	idx := -1
	for i, sa := range f.Abilities {
		if sa.API == "ChangeZone" && strings.EqualFold(sa.Params["Origin"], "Graveyard") &&
			strings.EqualFold(sa.Params["Destination"], "Exile") && strings.EqualFold(sa.ParamStr(cards.PKCost), "T") {
			idx = i
		}
	}
	probe, ok := reg.Lookup(gainsExiledWithCard)
	if idx < 0 || !ok || len(probe.Faces) == 0 {
		return oraclegen.Item{}, false
	}
	srcPrefixes, why := oraclegen.XMageAbility(f)
	probePrefixes, why2 := oraclegen.XMageAbility(probe.Faces[0])
	if why != "" || why2 != "" || srcPrefixes[idx] == "" || probePrefixes[0] == "" {
		return oraclegen.Item{}, false
	}
	holder := "p0:" + gainsExiledWithHolder
	steps := []oraclegen.Step{
		{Op: "activate", Seat: 0, Card: "p0:" + name, AbilityIndex: intPtr(idx), Targets: []string{"p0:" + gainsExiledWithCard}},
		// The exile resolves and its reflexive "when a creature card is
		// exiled this way" trigger asks for the counter's target.
		{Op: "resolve", Answers: []oraclegen.Answer{{Kind: "target", Pick: []string{holder}}}},
		{Op: "resolve"},
		{Op: "activate", Seat: 0, Card: holder, Ability: "damage", Targets: []string{"p1"}},
		{Op: "resolve"},
	}
	sc := staticScenario(f, name, []string{name, gainsExiledWithHolder}, nil, steps)
	p0 := sc.Setup["p0"]
	p0.Graveyard = appendFixtureUnique(p0.Graveyard, gainsExiledWithCard)
	sc.Setup["p0"] = p0
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 || lifeOf(res, 1) != 19 {
		return oraclegen.Item{}, false
	}
	it := oraclegen.NewLevelBItem(name, req.Key, StaticObserved.Version, []string{"613.1f", "602.2"}, sc)
	// Both activate steps' targets travel on the step.
	it.XAnswers = oraclegen.XAnswersForScenario(reg, res, sc, oraclegen.ModeNumbers(f), map[int]bool{0: true, 3: true})
	it.XAbility = []string{srcPrefixes[idx], "", "", probePrefixes[0], ""}
	return it, true
}
