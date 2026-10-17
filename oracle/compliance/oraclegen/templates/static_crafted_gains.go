package templates

import (
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/levelb"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
)

// Level-B observation of a crafted back face that has the activated abilities
// of the cards exiled to craft it (The Enigma Jewel's Locus of Enlightenment:
// "Locus of Enlightenment has each activated ability of the exiled cards used
// to craft it", GainsAbilitiesOfDefined$ ExiledWith, CR 702.167 craft, CR
// 613.1f). Setup cannot populate the exiled-with set (an exile placement is
// bound to no source, CR 406.6), so the front face is placed with the craft
// materials in the graveyard and its own Craft activation exiles them and
// returns the card transformed (craftPrelude, the shape Sunbird Standard's
// static#1.0 is served by). Then the back face activates a material's mana
// ability it gained: "{T}: Add {G}" from a Llanowar Elves material, so the
// pool shows G -- mana no printed ability of the back face makes. The control
// is the same craft with no gained-ability activation offered under that
// label before the craft (the front face prints no such ability).

// craftedGainedMana is the gained ability the observation activates: the
// runner's mana label and XMage's rule-text prefix.
const (
	craftedGainedManaLabel = "Add G"
	craftedGainedManaText  = "{T}: Add {G}"
)

// craftedGainsShape reports a back-face static granting the abilities of the
// cards exiled with the source.
func craftedGainsShape(c *cards.Card, req levelb.Requirement, st cards.Static) bool {
	if req.Face <= 0 || len(c.Faces) < 2 || craftAbilityIndex(c.Faces[0]) < 0 {
		return false
	}
	return strings.Contains(strings.ToLower(st.ParamStr(cards.PKGainsAbilitiesOfDefined)), "exiledwith")
}

// staticCraftedGainsItem serves the shape: craft the front face, then
// activate the gained material mana ability on the back face.
func staticCraftedGainsItem(reg *cards.Registry, c *cards.Card, f *cards.Face, name string, req levelb.Requirement, st cards.Static) (oraclegen.Item, bool) {
	if !craftedGainsShape(c, req, st) {
		return oraclegen.Item{}, false
	}
	craft, xab, materials, cost, ok := craftPrelude(c.Faces[0], name)
	if !ok {
		return oraclegen.Item{}, false
	}
	gains := false
	for _, m := range materials {
		gains = gains || m == "Llanowar Elves" || m == "Elvish Mystic"
	}
	if !gains {
		return oraclegen.Item{}, false
	}
	steps := append(append([]oraclegen.Step(nil), craft...),
		oraclegen.Step{Op: "activate", Seat: 0, Card: "p0:" + name, Ability: craftedGainedManaLabel})
	sc := staticScenario(f, name, []string{name}, nil, steps)
	p0 := sc.Setup["p0"]
	for _, m := range materials {
		p0.Graveyard = append(p0.Graveyard, m)
	}
	sc.Setup["p0"] = p0
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 || len(res.Snapshots) == 0 {
		return oraclegen.Item{}, false
	}
	if !poolHasG(res.Snapshots[len(res.Snapshots)-1]) {
		return oraclegen.Item{}, false
	}
	// Control: before the craft the front face gains nothing, so the same
	// activation is not offered.
	control := staticScenario(f, name, []string{name}, nil,
		[]oraclegen.Step{{Op: "activate", Seat: 0, Card: "p0:" + name, Ability: craftedGainedManaLabel}})
	control.Setup["p0"] = p0
	if cres, cok := runStatic(reg, control); cok && len(cres.Fails) == 0 {
		return oraclegen.Item{}, false
	}
	it := oraclegen.NewLevelBItem(name, req.Key, StaticObserved.Version, []string{"702.167", "613.1f", "605.3a"}, sc)
	it.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f), nil)
	it.XAnswers = scriptPreludeActivationCost(it.XAnswers, sc.Steps[:len(craft)], cost, len(sc.Steps), res.Decisions)
	// The gained ability is a fixed-colour mana ability XMage activates by
	// its rule text with no colour dialog; gorge's pick among the back
	// face's several gained mana abilities is a wheel choice, not a colour
	// the driver must script.
	if last := len(sc.Steps) - 1; last < len(it.XAnswers) {
		it.XAnswers[last] = nil
	}
	it.XAbility = append(append([]string(nil), xab...), craftedGainedManaText)
	return it, true
}

// poolHasG reports whether p0's pool holds green mana at snap.
func poolHasG(snap rules.OracleSnapshot) bool {
	for _, p := range snap.Players {
		if p.Seat == 0 {
			return strings.Contains(p.Pool, "G")
		}
	}
	return false
}
