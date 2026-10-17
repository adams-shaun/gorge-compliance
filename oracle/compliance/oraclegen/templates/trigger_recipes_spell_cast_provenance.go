package templates

import (
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/cards"
)

// The cast-provenance spell-cast triggers name a predicate the ordinary probe
// cast from p0's hand can never satisfy: a cast from exile
// (Card.wasCastFromExile, and the negated Card.!wasCastFromYourHand) or an
// Adventure spell face (Card.Adventure). The engine's trigger matcher already
// evaluates both predicates (engine/rules/cast_provenance.go, engine/effects/filter.go);
// only the CAUSE was missing, so the row skipped with "spell-cast unsupported
// <x> provenance" (spellCastNarrowSkip). Each cause below produces the cast
// the predicate needs, and gorge's own matcher decides whether it fires.
//
// The ownership family (Card.YouDontOwn, Spell.YouDontOwn) is served by one
// generic exile-and-grant cause: Nita, Forum Conciliator's own activated
// ability exiles a p1-owned instant from p1's graveyard and grants p0 the
// right to cast it this turn, so the cause's cast is a spell p0 does not
// own. The engine evaluates the ownership predicate rules-side
// (engine/effects/filter.go) and models Gonti's `ValidSAonCard$ Spell.YouDontOwn`
// head (trigmatch.spellValidSAonCardMatches), so the same cast fires every
// manifest row whose trigger names it.
func spellCastProvenanceCauses(reg *cards.Registry, f *cards.Face, name string, t *cards.Trigger) []triggerCause {
	filter := strings.ToLower(strings.ReplaceAll(
		t.ParamStr(cards.PKValidCard)+","+t.ParamStr(cards.PKValidSAonCard), " ", ""))
	prelude := setupResolvePrelude(f)
	var out []triggerCause
	if strings.Contains(filter, "wascastfromexile") || strings.Contains(filter, "!wascastfromyourhand") {
		if c, ok := spellCastFromExileCause(reg, name); ok {
			c.prelude = prelude
			out = append(out, c)
		}
	}
	if strings.Contains(filter, "adventure") && filterNamesAdventure(filter) {
		if c, ok := spellCastAdventureCause(reg, name); ok {
			c.prelude = prelude
			out = append(out, c)
		}
	}
	if strings.Contains(filter, "youdontown") {
		if c, ok := spellCastYouDontOwnCause(reg); ok {
			c.prelude = prelude
			out = append(out, c)
		}
	}
	if strings.Contains(strings.ToLower(t.ParamStr(cards.PKValidSA)), "manafromcard") {
		if c, ok := spellCastSourceManaCause(reg, f, name); ok {
			c.prelude = prelude
			out = append(out, c)
		}
	}
	if strings.Contains(strings.ToLower(t.ParamStr(cards.PKValidSA)), "manafromtreasure") {
		if c, ok := spellCastTreasureManaCause(reg); ok {
			c.prelude = prelude
			out = append(out, c)
		}
	}
	return out
}

// spellCastTreasureManaCause is the cast a "mana from a Treasure was spent to
// cast it" provenance (ValidSA$ Spell.ManaFromTreasure, Smaug, Wicked Worm)
// needs: p0 taps and sacrifices a Treasure for mana, then casts a probe that
// spends that pool mana. The Treasure is Buried Treasure, a NONTOKEN Artifact
// -- Treasure card setup can place: a Treasure token would need a maker's
// cast first, and the XMage driver selects a mana ability through the
// card database, which holds no token (its activate step then finds no
// ability). The probe is Sol Ring ({1}), payable by whichever colour the
// Treasure's any-colour choice takes. The pool is empty otherwise, so the
// cast spends the Treasure's mana in both engines.
func spellCastTreasureManaCause(reg *cards.Registry) (triggerCause, bool) {
	const treasure, probe = "Buried Treasure", "Sol Ring"
	tc, ok := reg.Lookup(treasure)
	if !ok || len(tc.Faces) == 0 || !oraclegen.XMageKnown(treasure) || !oraclegen.XMageKnown(probe) {
		return triggerCause{}, false
	}
	if _, ok := reg.Lookup(probe); !ok {
		return triggerCause{}, false
	}
	idx := -1
	for i, sa := range tc.Faces[0].Abilities {
		if sa.API == "Mana" && strings.Contains(sa.ParamStr(cards.PKCost), "Sac<1/CARDNAME>") {
			idx = i
			break
		}
	}
	if idx < 0 {
		return triggerCause{}, false
	}
	return triggerCause{
		battlefield: []string{treasure},
		hand:        []string{probe},
		steps: []oraclegen.Step{
			{Op: "activate", Seat: 0, Card: "p0:" + treasure, AbilityIndex: &idx},
			{Op: "cast", Seat: 0, Card: "p0:" + probe},
		},
	}, true
}

// spellCastSourceManaCause is the cast a producing-source provenance
// (ValidSA$ Spell.ManaFromCard.StrictlySelf: "whenever you cast a permanent
// spell using mana produced by CARDNAME" -- Tecutlan, the Searing Rift; The
// Myriad Pools; Barracks of the Thousand) needs: p0 taps the card under test
// for mana with its own {T} mana ability, then casts a probe that spends that
// pool mana. The probe is Sol Ring ({1}, an artifact: a permanent spell and
// an artifact-or-creature spell, so every carrier's ValidCard$ accepts it),
// payable by whichever single colour the card adds. The pool is empty
// otherwise, so the cast spends the card's mana in both engines. The
// land-paid control (the trigger must stay silent) is pinned by
// TestSpellCastSourceManaCauseServesAndControls.
func spellCastSourceManaCause(reg *cards.Registry, f *cards.Face, name string) (triggerCause, bool) {
	idx := sourceManaAbilityIndex(f)
	if idx < 0 || !oraclegen.XMageKnown(sourceManaProbe) {
		return triggerCause{}, false
	}
	if _, ok := reg.Lookup(sourceManaProbe); !ok {
		return triggerCause{}, false
	}
	// The XMage driver selects the activated ability by its rule text. The
	// mana ability lives on the face under test (a back face for every
	// corpus carrier), which the front-face xmage_ability fill cannot see,
	// so the cause names it itself.
	prefixes, why := oraclegen.XMageAbility(f)
	if why != "" || prefixes[idx] == "" {
		return triggerCause{}, false
	}
	return triggerCause{
		hand: []string{sourceManaProbe},
		steps: []oraclegen.Step{
			{Op: "activate", Seat: 0, Card: "p0:" + name, AbilityIndex: &idx},
			{Op: "cast", Seat: 0, Card: "p0:" + sourceManaProbe},
		},
		xability: []string{prefixes[idx], ""},
	}, true
}

// sourceManaProbe is the permanent spell the producing-source cause casts.
const sourceManaProbe = "Sol Ring"

// sourceManaAbilityIndex is the face's own bare-{T} mana ability (the one
// whose mana the provenance reads), or -1.
func sourceManaAbilityIndex(f *cards.Face) int {
	for i, sa := range f.Abilities {
		if sa.API == "Mana" && strings.TrimSpace(sa.ParamStr(cards.PKCost)) == "T" {
			return i
		}
	}
	return -1
}

// spellCastYouDontOwnCause builds the exile-and-grant cast for the ownership
// family (Card.YouDontOwn, Spell.YouDontOwn). Nita, Forum Conciliator's own
// activated ability exiles a p1-owned instant from p1's graveyard, then its
// Effect grants p0 permission to cast that card this turn; the cast step
// names the card by its OWNER (p1), so gorge casts a spell p0 does not own.
// Nita is a generic granter the way Misthollow Griffin is the generic
// from-exile probe: the predicate reads the cast spell's owner, so the same
// cast serves any card whose trigger names it. The sacrifice cost's pick is
// scripted to the deterministic activation-cost fixture, so the trigger
// source on p0's battlefield is never the permanent sacrificed.
func spellCastYouDontOwnCause(reg *cards.Registry) (triggerCause, bool) {
	const granter = "Nita, Forum Conciliator"
	granterCard, ok := reg.Lookup(granter)
	if !ok || len(granterCard.Faces) == 0 {
		return triggerCause{}, false
	}
	pf := granterCard.Faces[0]
	prefixes, why := oraclegen.XMageAbility(pf)
	if why != "" {
		return triggerCause{}, false
	}
	probe := "Opt"
	probeCard, ok := reg.Lookup(probe)
	if !ok || len(probeCard.Faces) == 0 {
		return triggerCause{}, false
	}
	pool, why := oraclegen.PoolFor(probeCard.Faces[0].ManaCost)
	if why != "" {
		return triggerCause{}, false
	}
	for i, sa := range pf.Abilities {
		if !sa.IsActivated() || !strings.Contains(sa.ParamStr(cards.PKValidTgts), "Instant") {
			continue
		}
		cost := sa.ParamStr(cards.PKCost)
		mana, gap := activationCostIn(cost, "battlefield", granter)
		if gap != "" {
			continue
		}
		prefix, exists := prefixes[i]
		if !exists {
			continue
		}
		idx := i
		setup := oraclegen.Seat{}
		addActivationCostFixtures(&setup, granter, cost)
		answers := activationXAnswers(cost)
		if len(setup.Battlefield) > 0 {
			// The cost's own fixture table names the sacrifice; scripting it
			// by ref keeps the pick deterministic and the XMage answer
			// (recorded from the observed decision) identical to it.
			answers = append(answers, oraclegen.Answer{Kind: "choose", Pick: []string{"p0:" + setup.Battlefield[0]}})
		}
		return triggerCause{
			battlefield:       append([]string{granter}, setup.Battlefield...),
			opponentGraveyard: []string{probe},
			steps: []oraclegen.Step{
				{Op: "activate", Seat: 0, Card: "p0:" + granter, Mana: mana, AbilityIndex: &idx, Targets: []string{"p1:" + probe}, Answers: answers},
				{Op: "resolve"},
				{Op: "cast", Seat: 0, Card: "p1:" + probe, Mana: pool},
			},
			xability:     []string{prefix, "", ""},
			activateCost: cost,
		}, true
	}
	return triggerCause{}, false
}

// filterNamesAdventure reports whether the filter carries the `Adventure`
// provenance predicate as its own dot/plus/comma-separated token. It keeps
// the distinct `AdventureCard` predicate (the card IS an Adventure card, a
// characteristic, not a cast provenance) from being mistaken for it.
func filterNamesAdventure(filter string) bool {
	for _, seg := range strings.FieldsFunc(filter, func(r rune) bool {
		return r == '.' || r == '+' || r == ','
	}) {
		if seg == "adventure" {
			return true
		}
	}
	return false
}

// setupResolvePrelude returns the two passes that clear a trigger queued at
// the start of p0's first main phase. A generated (xmageFixture) scenario
// deliberately stops at the FIRST priority in turn 1's main1 even with a
// non-empty stack (engine/rules/oracle_run.go's build), so a beginning-of-first-
// main-phase trigger (Shadow of the Goblin's "Unreliable Visions") sits on the
// stack at the setup checkpoint and a sorcery-speed cast cannot be offered
// until it resolves. It returns nil when the face has no such trigger, so
// every other card's item is unchanged.
func setupResolvePrelude(f *cards.Face) []oraclegen.Step {
	for i := range f.Triggers {
		t := &f.Triggers[i]
		if t.Mode != "Phase" || !strings.EqualFold(strings.TrimSpace(t.ParamStr(cards.PKPhase)), "Main1") {
			continue
		}
		if vp := strings.TrimSpace(t.ParamStr(cards.PKValidPlayer)); vp != "" &&
			!strings.EqualFold(vp, "You") && !strings.EqualFold(vp, "Player") {
			continue
		}
		return []oraclegen.Step{{Op: "pass", Seat: 0}, {Op: "pass", Seat: 1}}
	}
	return nil
}

// spellCastFromExileCause casts a probe card out of p0's exile. The probe's
// OWN Continuous static grants the cast from exile (Misthollow Griffin,
// Eternal Scourge), which the engine's may-play walk offers; the trigger's
// wasCastFromExile / !wasCastFromYourHand provenance then holds. The probe is
// a creature, so the sorcery-speed turn-1 main phase offers it.
func spellCastFromExileCause(reg *cards.Registry, name string) (triggerCause, bool) {
	for _, probe := range []string{"Misthollow Griffin", "Eternal Scourge"} {
		if probe == name {
			continue
		}
		card, ok := reg.Lookup(probe)
		if !ok || len(card.Faces) == 0 || !card.Faces[0].IsCreature() {
			continue
		}
		pool, why := oraclegen.PoolFor(card.Faces[0].ManaCost)
		if why != "" {
			continue
		}
		return triggerCause{
			exile: []string{probe},
			steps: []oraclegen.Step{{Op: "cast", Seat: 0, Card: "p0:" + probe, Mana: pool}},
		}, true
	}
	return triggerCause{}, false
}

// spellCastAdventureCause casts an Adventure spell face from p0's hand. Setup
// deals the PHYSICAL card under its front name; the cast step names the
// Adventure face, which the runner binds to the engine's "adventure_alt"
// offer (engine/rules/oracle_run_faces.go). The trigger's Card.Adventure filter then
// matches the spell on the stack.
func spellCastAdventureCause(reg *cards.Registry, name string) (triggerCause, bool) {
	for _, parent := range []string{"Beanstalk Wurm", "Bonecrusher Giant", "Brazen Borrower", "Curious Pair"} {
		if parent == name {
			continue
		}
		card, ok := reg.Lookup(parent)
		if !ok || card.AlternateMode != "Adventure" || len(card.Faces) < 2 {
			continue
		}
		face := requestedFace(card, card.Faces[1].Name)
		if face == nil || face == card.Faces[0] {
			continue
		}
		pool, why := oraclegen.PoolFor(face.ManaCost)
		if why != "" {
			continue
		}
		return triggerCause{
			hand:  []string{parent},
			steps: []oraclegen.Step{{Op: "cast", Seat: 0, Card: "p0:" + face.Name, Mana: pool}},
		}, true
	}
	return triggerCause{}, false
}
