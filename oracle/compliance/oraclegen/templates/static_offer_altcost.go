// Level-B observation of a MayPlay static that prices the cast it permits
// (wave-5 group X, CR 118.9 / 601.2b,f). Two shapes:
//
//   - a GRANTING static over cards exiled with the source whose exile is the
//     source's own graveyard replacement (Valgavoth, Terror Eater: "If a card
//     you didn't control would be put into an opponent's graveyard from
//     anywhere, exile it instead. During your turn, you may play cards exiled
//     with Valgavoth. If you cast a spell this way, pay life equal to its mana
//     value rather than pay its mana cost"): p0 Shocks p1's creature, the
//     replacement exiles it with the source, and p0 is offered its cast and
//     casts it for life alone -- the final checkpoint shows the price in p0's
//     life total and the card under p0's control;
//   - a NON-granting static that reprices a cast from exile another
//     permission makes (Warped Space: "Once each turn, you may pay {0} rather
//     than pay the mana cost for a spell you cast from exile"): with the door
//     unlocked, p0 casts Misthollow Griffin (its own "You may cast this card
//     from exile") electing the alternative with no mana in pool.
//
// Each has a control without the source in which the observed cast is not
// offered, so the offer is the static's doing.
package templates

import (
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/levelb"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
)

// altCostExileVictim is p1's creature the Shock kills: Baseline already seats
// it on p1's battlefield, and its mana value (2) is the life the cast pays.
const altCostExileVictim = "Grizzly Bears"

// altCostExileVictimMV is altCostExileVictim's mana value.
const altCostExileVictimMV = 2

// altCostExileCastProbe is the card with its own exile cast permission the
// non-granting alternative reprices.
const altCostExileCastProbe = "Misthollow Griffin"

// staticMayPlayAltCostItem serves either shape; ok is false for any other
// static.
func staticMayPlayAltCostItem(reg *cards.Registry, c *cards.Card, f *cards.Face, name string, req levelb.Requirement, st cards.Static) (oraclegen.Item, bool) {
	if !strings.EqualFold(strings.TrimSpace(st.ParamStr(cards.PKMayPlay)), "True") || !st.HasParam(cards.PKMayPlayAltManaCost) {
		return oraclegen.Item{}, false
	}
	alt := strings.TrimSpace(st.ParamStr(cards.PKMayPlayAltManaCost))
	if strings.EqualFold(strings.TrimSpace(st.Params["MayPlayDontGrantZonePermissions"]), "True") {
		return altCostExileRepriceItem(reg, c, f, name, req, st, alt)
	}
	return altCostExiledWithLifeItem(reg, f, name, req, st, alt)
}

// altCostExiledWithLifeItem is the granting shape (Valgavoth).
func altCostExiledWithLifeItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, st cards.Static, alt string) (oraclegen.Item, bool) {
	if !strings.EqualFold(alt, "PayLife<ConvertedManaCost>") ||
		!strings.Contains(st.ParamStr(cards.PKAffected), "ExiledWithSource") ||
		!strings.Contains(st.ParamStr(cards.PKAffectedZone), "Exile") || !graveyardExileReplacement(f) {
		return oraclegen.Item{}, false
	}
	victim := "p1:" + altCostExileVictim
	kill := []oraclegen.Step{
		{Op: "cast", Seat: 0, Card: "p0:Shock", Mana: "R", Targets: []string{victim}},
		{Op: "resolve"},
	}
	offered := func(want bool) oraclegen.Step {
		return oraclegen.Step{Op: "pass_to", Seat: 0, Decision: "priority",
			Expect: []oraclegen.Expect{{Offered: &oraclegen.Offered{Seat: 0, Kind: "cast", Card: victim}, Want: boolPtr(want)}}}
	}
	steps := append(append([]oraclegen.Step(nil), kill...), offered(true),
		oraclegen.Step{Op: "cast", Seat: 0, Card: victim},
		oraclegen.Step{Op: "resolve"})
	sc := staticScenario(f, name, []string{name}, []string{"Shock"}, steps)
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 || len(res.Snapshots) == 0 {
		return oraclegen.Item{}, false
	}
	final := res.Snapshots[len(res.Snapshots)-1]
	if len(final.Players) == 0 || final.Players[0].Life != 20-altCostExileVictimMV || final.Players[0].Pool != "" {
		return oraclegen.Item{}, false
	}
	if !permanentControlledBy(final.Permanents, altCostExileVictim, 1, 0) {
		return oraclegen.Item{}, false
	}
	control := staticScenario(f, name, nil, []string{"Shock"}, append(append([]oraclegen.Step(nil), kill...), offered(false)))
	if cres, cok := runStatic(reg, control); !cok || len(cres.Fails) != 0 {
		return oraclegen.Item{}, false
	}
	it := oraclegen.NewLevelBItem(name, req.Key, StaticApplies.Version, []string{"118.9", "601.2b", "601.2f", "614.1"}, sc)
	it.XAnswers = oraclegen.XAnswersForScenario(reg, res, sc, oraclegen.ModeNumbers(f), castStepIndices(sc.Steps))
	return it, true
}

// altCostExileRepriceItem is the non-granting shape (Warped Space), served on
// a Room's door by casting the door first.
func altCostExileRepriceItem(reg *cards.Registry, c *cards.Card, f *cards.Face, name string, req levelb.Requirement, st cards.Static, alt string) (oraclegen.Item, bool) {
	if alt != "0" || !strings.Contains(st.ParamStr(cards.PKAffectedZone), "Exile") {
		return oraclegen.Item{}, false
	}
	var pre []oraclegen.Step
	var hand, bf []string
	if req.Face > 0 && levelb.IsRoomCard(c) {
		pool, why := oraclegen.PoolFor(f.ManaCost)
		if why != "" {
			return oraclegen.Item{}, false
		}
		pre = []oraclegen.Step{{Op: "cast", Seat: 0, Card: "p0:" + f.Name, Mana: pool}, {Op: "resolve"}}
		hand = []string{name}
	} else {
		bf = []string{name}
	}
	probe := "p0:" + altCostExileCastProbe
	steps := append(append([]oraclegen.Step(nil), pre...),
		oraclegen.Step{Op: "pass_to", Seat: 0, Decision: "priority", Expect: []oraclegen.Expect{{
			Offered: &oraclegen.Offered{Seat: 0, Kind: "cast", Card: probe, Label: "alternative cost"}, Want: boolPtr(true)}}},
		oraclegen.Step{Op: "cast", Seat: 0, Card: probe, CastMode: alternativeCostCastMode},
		oraclegen.Step{Op: "resolve"})
	castAt := len(steps) - 2
	sc := staticScenario(f, name, bf, hand, steps)
	withExile(&sc, altCostExileCastProbe)
	res, ok := runStatic(reg, sc)
	if !ok || len(res.Fails) != 0 || !onBattlefield(res, probe) {
		return oraclegen.Item{}, false
	}
	control := staticScenario(f, name, nil, nil, []oraclegen.Step{{Op: "pass_to", Seat: 0, Decision: "priority",
		Expect: []oraclegen.Expect{{Offered: &oraclegen.Offered{Seat: 0, Kind: "cast", Card: probe}, Want: boolPtr(false)}}}})
	withExile(&control, altCostExileCastProbe)
	if cres, cok := runStatic(reg, control); !cok || len(cres.Fails) != 0 {
		return oraclegen.Item{}, false
	}
	it := oraclegen.NewLevelBItem(name, req.Key, StaticApplies.Version, []string{"118.9", "601.2f"}, sc)
	it.XAnswers = oraclegen.XAnswersForScenario(reg, res, sc, oraclegen.ModeNumbers(f), castStepIndices(sc.Steps))
	// XMage poses the alternative cost as a "use the alternative cost?" ask
	// that gorge's elected cast option answers implicitly (the Leyline of
	// Mutation shape, static_alternative_cost.go): script the yes lead at
	// the probe's cast step.
	if len(it.XAnswers) < len(it.Steps) {
		grown := make([][]oraclegen.XAnswer, len(it.Steps))
		copy(grown, it.XAnswers)
		it.XAnswers = grown
	}
	it.XAnswers[castAt] = append([]oraclegen.XAnswer{{Seat: 0, Kind: "choice", Value: "yes"}}, it.XAnswers[castAt]...)
	return it, true
}

// withExile adds card to p0's setup exile.
func withExile(sc *oraclegen.Scenario, card string) {
	p0 := sc.Setup["p0"]
	p0.Exile = appendFixtureUnique(p0.Exile, card)
	sc.Setup["p0"] = p0
}

// graveyardExileReplacement reports whether the face carries the
// "would be put into a graveyard ... exile it instead" replacement: a Moved
// event to the graveyard whose replacement is a ChangeZone to exile.
func graveyardExileReplacement(f *cards.Face) bool {
	for i := range f.Repls {
		r := &f.Repls[i]
		if r.Event != "Moved" || r.With == nil || r.With.API != "ChangeZone" {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(r.Params["Destination"]), "Graveyard") &&
			strings.EqualFold(strings.TrimSpace(r.With.Params["Destination"]), "Exile") {
			return true
		}
	}
	return false
}

// permanentControlledBy reports whether a permanent named name, owned by
// seat owner, sits on the battlefield under seat ctrl.
func permanentControlledBy(perms []rules.OracleSnapPerm, name string, owner, ctrl int) bool {
	for i := range perms {
		if perms[i].Name == name && perms[i].Owner == owner && perms[i].Controller == ctrl {
			return true
		}
	}
	return false
}
