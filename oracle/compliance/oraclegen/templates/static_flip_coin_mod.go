package templates

import (
	"fmt"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/levelb"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
)

// flipCoinProbe is the coin-flip source the FlipCoinMod observation
// activates: "{1}, Sacrifice Bottle of Suleiman: Flip a coin. If you win the
// flip, create a 5/5 colorless Djinn artifact creature token with flying. If
// you lose the flip, Bottle of Suleiman deals 5 damage to you." Each outcome
// leaves its own compared snapshot field (a Djinn permanent, or 15 life), and
// neither needs an answer.
const flipCoinProbe = "Bottle of Suleiman"

// flipCoinProbeWinToken is the permanent the probe's won flip creates.
const flipCoinProbeWinToken = "Djinn Token"

// flipCoinModItem serves FlipCoinMod (Edgar, King of Figaro's Two-Headed
// Coin: "The first time you flip one or more coins each turn, those coins
// come up heads and you win those flips"). With the card on p0's
// battlefield, p0's first flip of the turn -- the probe's -- is won in both
// engines (gorge: effects.FlipCoinForced; XMage: a FLIP_COINS replacement
// that sets heads-and-won, so the driver is asked no heads/tails call), so
// the scenario's outcome is independent of either engine's RNG: the Djinn is
// on the battlefield and p0 is still at 20 life. Only the first flip is
// forced; a later flip would be random again and is never part of the item.
//
// The control (no card) only proves the probe flips and resolves to one of
// its two outcomes at the same checkpoint: its side is gorge's seeded RNG's
// and is not compared anywhere.
func flipCoinModItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	const mode = "FlipCoinMod"
	skip := func(why string) (oraclegen.Item, *oraclegen.Skip) {
		return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "static " + mode + " " + why}
	}
	if name == flipCoinProbe {
		return skip("the card is the coin-flip probe")
	}
	probe := lookupFrontFace(reg, flipCoinProbe)
	if probe == nil || len(probe.Abilities) == 0 || probe.Abilities[0].API != "FlipCoin" {
		return skip("coin-flip probe is not in the corpus")
	}
	pool, why := activationCostIn(probe.Abilities[0].ParamStr(cards.PKCost), "battlefield", flipCoinProbe)
	if why != "" {
		return skip("probe cost unpayable: " + why)
	}
	steps := []oraclegen.Step{
		{Op: "activate", Seat: 0, Card: cardAt(0, flipCoinProbe), Mana: pool, AbilityIndex: intPtr(0)},
		{Op: "resolve"},
	}
	life := func(res rules.OracleResult) int32 {
		if len(res.Snapshots) == 0 || len(res.Snapshots[len(res.Snapshots)-1].Players) == 0 {
			return -1
		}
		return res.Snapshots[len(res.Snapshots)-1].Players[0].Life
	}
	won := func(res rules.OracleResult) bool {
		return permCount(res, flipCoinProbeWinToken) == 1 && life(res) == 20
	}
	lost := func(res rules.OracleResult) bool {
		return permCount(res, flipCoinProbeWinToken) == 0 && life(res) == 15
	}
	control := staticScenario(f, name, []string{flipCoinProbe}, nil, steps)
	cres, ok := runStatic(reg, control)
	if !ok || len(cres.Fails) != 0 || !(won(cres) || lost(cres)) || permCount(cres, flipCoinProbe) != 0 {
		return skip(fmt.Sprintf("control probe does not flip to one of its outcomes (ok=%v fails=%v life=%d)", ok, cres.Fails, life(cres)))
	}
	sc := withBackFace(staticScenario(f, name, []string{name, flipCoinProbe}, nil, steps), name, req)
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 {
		return skip(fmt.Sprintf("observation does not replay (ok=%v fails=%v)", ok, res.Fails))
	}
	if !won(res) {
		return skip(fmt.Sprintf("the first flip of the turn was not won with the card on the battlefield (djinn=%d life=%d)",
			permCount(res, flipCoinProbeWinToken), life(res)))
	}
	if !onBattlefield(res, "p0:"+name) {
		return skip("card is not on the battlefield at the checkpoint")
	}
	it := oraclegen.NewLevelBItem(name, req.Key, StaticObserved.Version, []string{"705.2", "705.3"}, sc)
	it.XAnswers = oraclegen.XAnswersForScenario(reg, res, sc, oraclegen.ModeNumbers(f), nil)
	return it, nil
}
