package templates

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/levelb"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
)

// mayPlayRiderFires serves a levelb.MayPlayRiderSub requirement: the Static$
// True SpellCast rider of a play-permission static ("If you cast a spell this
// way, that creature enters with a finality counter on it"). The rider never
// uses the stack, so the trigger template's abilityOnStack probe cannot see
// it. The cause is the cast the face's own MayPlay static permits: the
// static template's offered scenario (staticOfferItem, the scenario that
// already agrees for that static's row) has its final offered assertion
// replaced by the cast itself, and the stack is settled. The row is served
// only when gorge shows the cast permanent entering with a counter or tapped
// -- the rider's visible consequence, which the comparator's permanent key
// (counters, tapped) then holds XMage to.
func mayPlayRiderFires(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement) (oraclegen.Item, *oraclegen.Skip) {
	skip := func(why string) (oraclegen.Item, *oraclegen.Skip) {
		return oraclegen.Item{}, &oraclegen.Skip{Card: name, Reason: "trigger mayplay rider: " + why}
	}
	c, ok := reg.Lookup(name)
	if !ok {
		return skip("card not in the registry")
	}
	si, ok := levelb.MayPlayRiderStatic(f)
	if !ok {
		return skip("no MayPlay static")
	}
	sreq := levelb.Requirement{
		Key: "static#" + strconv.Itoa(req.Face) + "." + strconv.Itoa(si), Family: "static",
		Face: req.Face, Slot: strconv.Itoa(si), Sub: "static.continuous",
	}
	base, ok := staticOfferItem(reg, c, f, name, sreq, f.Statics[si])
	if !ok {
		return skip("the permission static has no offered scenario")
	}
	sc, probe, ok := mayPlayRiderCast(base.Scenario)
	if !ok {
		// The static's own row may be served by a land play (Mikey & Don's
		// "play lands and cast Mutant, Ninja, or Turtle spells"), which the
		// rider never reads: retry with a creature of a subtype the
		// permission names.
		if alt, altOK := mayPlayRiderSubtypeOffer(reg, c, f, name, sreq, f.Statics[si]); altOK {
			base = alt
			sc, probe, ok = mayPlayRiderCast(base.Scenario)
		}
	}
	if !ok {
		return skip("the permission scenario ends in no offered cast")
	}
	n, res, ok := oraclegen.Settle(reg, sc)
	if !ok {
		return skip("the permitted cast does not play through")
	}
	for i := 0; i < n; i++ {
		sc.Steps = append(sc.Steps, oraclegen.Step{Op: "resolve"})
	}
	sc, castSteps := oraclegen.ChooseTargets(sc, res.Decisions)
	res, ok = oraclegen.PlaysThrough(reg, sc)
	if !ok || len(res.Fails) != 0 {
		return skip("the permitted cast does not play through")
	}
	if !mayPlayRiderObserved(res, probe) {
		return skip("the cast permanent entered unmodified")
	}
	it := oraclegen.NewLevelBItem(name, req.Key, TriggerFires.Version, []string{"603.2"}, sc)
	if len(base.XAbility) > 0 {
		it.XAbility = make([]string, len(sc.Steps))
		copy(it.XAbility, base.XAbility)
	}
	it.XAnswers = oraclegen.XAnswersForScenario(res, sc, oraclegen.ModeNumbers(f), castSteps)
	return it, nil
}

// mayPlayRiderCast turns the static template's offered scenario into the
// cast: its last step, the pass_to asserting the probe is offered as a cast,
// becomes the cast of that probe (the step before it already added the mana
// the cast spends). probe is the cast card's ref.
func mayPlayRiderCast(base oraclegen.Scenario) (oraclegen.Scenario, string, bool) {
	sc := base
	if len(sc.Steps) == 0 {
		return sc, "", false
	}
	last := sc.Steps[len(sc.Steps)-1]
	if last.Op != "pass_to" || len(last.Expect) != 1 || last.Expect[0].Offered == nil {
		return sc, "", false
	}
	off := last.Expect[0].Offered
	if off.Kind != offerKindCast || off.Label != "" || off.Seat != 0 {
		return sc, "", false
	}
	steps := append([]oraclegen.Step(nil), sc.Steps[:len(sc.Steps)-1]...)
	sc.Steps = append(steps, oraclegen.Step{Op: "cast", Seat: 0, Card: off.Card})
	return sc, off.Card, true
}

// mayPlayRiderObserved reports that the last snapshot shows p0's permanent
// for probe (a ref "p0:<name>") with a counter on it or tapped: the probes
// the offered scenario casts enter with neither on their own.
func mayPlayRiderObserved(res rules.OracleResult, probe string) bool {
	if len(res.Snapshots) == 0 {
		return false
	}
	want := strings.TrimPrefix(probe, "p0:")
	for _, p := range res.Snapshots[len(res.Snapshots)-1].Permanents {
		if p.Controller != 0 || p.Name != want {
			continue
		}
		if p.Tapped {
			return true
		}
		for _, n := range p.Counters {
			if n > 0 {
				return true
			}
		}
	}
	return false
}

// mayPlayRiderSubtypeOffer is the offered-cast scenario for a MayPlay static
// whose Affected$ alternatives name creature SUBTYPES (Mutant.TopLibrary,
// Ninja.TopLibrary, Turtle.TopLibrary) no generic offer probe carries: a
// quiet creature of the first subtype the registry holds is the probe, in
// each zone the static affects, tried exactly as staticOfferItem tries its
// own (with and without the source's cast base). A rider row is about the
// creature cast the permission allows, so only a cast is served.
func mayPlayRiderSubtypeOffer(reg *cards.Registry, c *cards.Card, f *cards.Face, name string, req levelb.Requirement, st cards.Static) (oraclegen.Item, bool) {
	var cast *oraclegen.Item
	if base, why := staticBase(reg, c, f, name, req, staticProbePlan{}, nil); why == "" {
		cast = &base
	}
	zones := offerZones(st.ParamStr(cards.PKAffectedZone))
	for _, alt := range strings.Split(st.ParamStr(cards.PKAffected), ",") {
		word := strings.TrimSpace(alt)
		if i := strings.IndexAny(word, ".+"); i >= 0 {
			word = word[:i]
		}
		if word == "" || isCardTypeWord(word) {
			continue
		}
		probe, ok := oraclegen.SubtypeCard(reg, word)
		if !ok {
			continue
		}
		pc, ok := reg.Lookup(probe)
		if !ok || len(pc.Faces) == 0 || !pc.Faces[0].IsCreature() {
			continue
		}
		for _, z := range zones {
			t, ok := offerTryFor(reg, probe, z, false, false)
			if !ok || t.kind != offerKindCast {
				continue
			}
			for _, base := range []*oraclegen.Item{cast, nil} {
				if it, ok := offerItem(reg, f, name, req, t, base); ok {
					return it, true
				}
			}
		}
	}
	return oraclegen.Item{}, false
}

// isCardTypeWord reports a Forge card-type or catch-all head ("Card",
// "Permanent", "Land", "Creature", ...): not a subtype a probe is found by.
func isCardTypeWord(word string) bool {
	switch strings.ToLower(word) {
	case "card", "permanent", "spell", "land", "creature", "artifact", "enchantment",
		"planeswalker", "instant", "sorcery", "battle", "kindred", "tribal":
		return true
	}
	return false
}
