// Class G12 (wave 4, "trigger did not fire"): the recipe's causes fire the
// right trigger MODE but the trigger's own filter or condition rejects them,
// so no scenario is emitted. tailTriggerCauses builds, per root cause, the
// cause that satisfies the filter. triggerFires tries them only after every
// ordinary cause failed, so no item an earlier cause serves changes bytes.
//
// Buckets (one function each):
//   - setup consumes the trigger: the source sits on the battlefield through
//     setup's turn-1 upkeep, where a "your upkeep" trigger already fires and
//     spends its target (Undergrowth Recon, Virtue of Persistence) or its
//     source (Mistbreath Elder); an unattached Aura is swept by CR 704.5m
//     (Chorale of the Void). Cast the source on turn 1 instead.
//   - a source that setup leaves 0/0 (Wildgrowth Archaic's Converge
//     etbCounter): a +1/+1 counter keeps it alive (CR 704.5f).
//   - a damage threshold (DamageAmount$ GEn): Lightning Bolt instead of
//     Shock, or +1/+1 counters until the source's combat damage reaches n.
//   - a crime condition (You.committedCrimeThisTurn, CR 700.13): Shock an
//     opponent first.
//   - spell-cast filters: a modal spell, a mana value equal to the source's
//     power, less mana spent than the mana value, a targeted artifact, an
//     Equipment or a spell targeting your creature.
//   - an origin zone: a land returned from the graveyard, a permanent that
//     enters from exile.
//   - a graveyard-source FullyUnlock trigger whose sibling "may return this
//     card" trigger takes the card out of the graveyard first.
package templates

import (
	"sort"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/levelb"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/cards"
)

// Tail probes, each XMage-known (oracle/compliance/manifests).
const (
	// auraTailBearer is the p0 creature a cast-self Aura is cast onto.
	auraTailBearer = "Hill Giant"
	// boltProbe ({R}, 3 damage to any target) serves a "dealt 3 or more
	// damage" threshold Shock's 2 misses.
	boltProbe = "Lightning Bolt"
	// affinityProbe ({4} Frogmite, affinity for artifacts) is cast for no
	// mana beside four artifacts: less mana spent than its mana value.
	affinityProbe = "Frogmite"
	// landReturnProbe ({3}{G} Splendid Reclamation) returns every land card
	// from p0's graveyard to the battlefield, untargeted.
	landReturnProbe = "Splendid Reclamation"
	// flickerProbe ({W} Cloudshift) exiles a creature you control and
	// returns it: it enters from exile.
	flickerProbe = "Cloudshift"
	// flickerTarget is the p0 creature the flicker probe blinks.
	flickerTarget = "Llanowar Elves"
)

// modalProbes are the modal spells a Spell.Modal cast trigger watches
// (Healing Salve's first mode gains p0 3 life; Boros Charm's deals 4 to p1).
var modalProbes = []struct{ name, target string }{
	{"Healing Salve", "p0"}, {"Boros Charm", "p1"},
}

func tailTriggerCauses(reg *cards.Registry, f *cards.Face, name string, t *cards.Trigger, req levelb.Requirement, causes []triggerCause) []triggerCause {
	grave := triggerFromGraveyard(f, req)
	var out []triggerCause
	if !grave {
		out = append(out, tailCastSelf(f, req, causes)...)
		out = append(out, tailZeroToughness(f, causes)...)
	}
	switch req.Sub {
	case "trigger.noncombat-damage":
		out = append(out, tailDamageBolt(reg, name, t, causes)...)
	case levelb.DamageSub:
		out = append(out, tailDamagePower(f, t, causes)...)
	case "trigger.combat-damage":
		out = append(out, tailCrime(reg, t, causes)...)
	case "trigger.spell-cast":
		out = append(out, tailSpellCast(reg, f, name, t)...)
	case "trigger.etb-land":
		out = append(out, tailLandFromGraveyard(reg, name, t)...)
	case "trigger.etb-other":
		out = append(out, tailEntersFromExile(reg, name, t)...)
	case levelb.FullyUnlockSub:
		if grave {
			out = append(out, tailDeclineSiblingReturn(causes)...)
		}
	}
	return out
}

// tailCastSelf casts the source on turn 1 for a phase trigger, and casts an
// Aura source onto a bearer for any trigger. Setup places the source before
// turn 1, so its own upkeep trigger fires during the setup drive (both
// engines: engine/rules/oracle_run.go's xmageFixture drive), spending a target or
// bouncing the source before the scenario's upkeep. An Aura placed by setup
// is unattached and swept (CR 704.5m).
func tailCastSelf(f *cards.Face, req levelb.Requirement, causes []triggerCause) []triggerCause {
	aura := oraclegen.HasType(f, "Aura")
	if req.Sub != "trigger.phase" && !aura {
		return nil
	}
	if _, why := oraclegen.PoolFor(f.ManaCost); why != "" {
		return nil
	}
	var out []triggerCause
	for _, c := range causes {
		if c.selfInHand || c.castSelfX {
			continue
		}
		v := c
		v.castSelfX = true
		if aura {
			v.auraBearer = auraTailBearer
		}
		out = append(out, v)
	}
	return out
}

// tailZeroToughness keeps a printed x/0 creature alive with a +1/+1 counter:
// a Converge or etbCounter body enters with counters only when cast, so a
// setup placement is put into the graveyard (CR 704.5f) before it triggers.
func tailZeroToughness(f *cards.Face, causes []triggerCause) []triggerCause {
	if !f.IsCreature() || strings.Contains(f.PT, "*") {
		return nil
	}
	if _, tough, ok := strings.Cut(f.PT, "/"); !ok || strings.TrimSpace(tough) != "0" {
		return nil
	}
	var out []triggerCause
	for _, c := range causes {
		if c.selfInHand || c.castSelfX {
			continue
		}
		v := c
		v.counters = mergeCounters(c.counters, map[string]map[string]int{"__SOURCE__": {"P1P1": 1}})
		out = append(out, v)
	}
	return out
}

// damageThreshold reads DamageAmount$ GEn, 0 when the trigger has none.
func damageThreshold(t *cards.Trigger) int {
	amt := strings.TrimSpace(t.ParamStr(cards.PKDamageAmount))
	n, err := strconv.Atoi(strings.TrimPrefix(strings.ToUpper(amt), "GE"))
	if !strings.HasPrefix(strings.ToUpper(amt), "GE") || err != nil {
		return 0
	}
	return n
}

// tailDamageBolt re-aims each Shock cause with Lightning Bolt when the
// trigger needs 3 damage at once (Innocent Bystander).
func tailDamageBolt(reg *cards.Registry, name string, t *cards.Trigger, causes []triggerCause) []triggerCause {
	if n := damageThreshold(t); n < 3 || n > 3 {
		return nil
	}
	var out []triggerCause
	for _, c := range causes {
		for _, st := range c.steps {
			if st.Op != "cast" || st.Card != "p0:"+shockProbe {
				continue
			}
			if v, ok := castCause(reg, name, boltProbe, st.Targets...); ok {
				out = append(out, v)
			}
		}
	}
	return out
}

// tailDamagePower grows a self source's power to the trigger's damage
// threshold with +1/+1 counters, for every cause that attacks with it
// (Spinneret and Spiderling's "deals 4 or more damage").
func tailDamagePower(f *cards.Face, t *cards.Trigger, causes []triggerCause) []triggerCause {
	n := damageThreshold(t)
	if n == 0 || !namesSelfFold(t.ParamStr(cards.PKValidSource)) || !f.IsCreature() {
		return nil
	}
	power, _, ok := parsePT(f.PT)
	if !ok || power >= n {
		return nil
	}
	var out []triggerCause
	for _, c := range causes {
		attacks := false
		for _, st := range append(append([]oraclegen.Step(nil), c.prelude...), c.steps...) {
			attacks = attacks || st.Op == "attack"
		}
		if !attacks {
			continue
		}
		v := c
		v.counters = mergeCounters(c.counters, map[string]map[string]int{"__SOURCE__": {"P1P1": n - power}})
		out = append(out, v)
	}
	return out
}

// tailCrime commits a crime first (CR 700.13: targeting an opponent) for a
// trigger gated on "if you've committed a crime this turn" (Servant of the
// Stinger's CheckDefinedPlayer$ You.committedCrimeThisTurn).
func tailCrime(reg *cards.Registry, t *cards.Trigger, causes []triggerCause) []triggerCause {
	if !strings.Contains(strings.ToLower(t.ParamStr(cards.PKCheckDefinedPlayer)), "committedcrime") {
		return nil
	}
	shock, ok := castProbe(reg, shockProbe, "p1")
	if !ok {
		return nil
	}
	crime := conditionPrelude{hand: []string{shockProbe}, steps: []oraclegen.Step{shock, {Op: "resolve"}}}
	var out []triggerCause
	for _, c := range causes {
		out = append(out, applyPrelude(c, crime))
	}
	return out
}

// tailSpellCast builds the cast causes whose spell the trigger's filter
// demands and the ordinary probe list never offers.
func tailSpellCast(reg *cards.Registry, f *cards.Face, name string, t *cards.Trigger) []triggerCause {
	var out []triggerCause
	add := func(c triggerCause, ok bool) {
		if ok {
			out = append(out, c)
		}
	}
	validSA := strings.ToLower(t.ParamStr(cards.PKValidSA))
	validCard := strings.ToLower(t.ParamStr(cards.PKValidCard))
	onCard := strings.ToLower(t.ParamStr(cards.PKValidSAonCard))
	// "Whenever you cast a modal spell" (Riku of Many Paths).
	if strings.Contains(validSA, "modal") {
		for _, p := range modalProbes {
			add(castCause(reg, name, p.name, p.target))
		}
	}
	// "A creature spell with mana value equal to CARDNAME's power" (Jackal,
	// Genius Geneticist: Creature.cmcEQX, X = Count$CardPower).
	if strings.Contains(validCard, "cmceqx") && strings.EqualFold(strings.TrimSpace(f.SVars["X"]), "Count$CardPower") {
		if power, _, ok := parsePT(f.PT); ok {
			if probe, found := creatureWithManaValue(reg, power, name); found {
				add(castCause(reg, name, probe))
			}
		}
	}
	// "If the amount of mana spent to cast it was less than its mana value"
	// (Tokka & Rahzar: Spell.ManaSpent LTX, X = Count$CardManaCost): Frogmite
	// cast for nothing beside four artifacts (affinity, CR 702.41).
	if strings.Contains(onCard+validSA, "manaspent lt") {
		if c, ok := castCause(reg, name, affinityProbe); ok {
			c.steps[0].Mana = ""
			c.battlefield = append(c.battlefield, "Ornithopter", "Ornithopter", "Ornithopter", "Ornithopter")
			out = append(out, c)
		}
	}
	// "An instant or sorcery spell that targets an artifact or land" (Fin
	// Fang Foom's TargetsValid$): Shock at an artifact creature the setup
	// places. The ordinary probes aim a target only for a creature filter.
	if tv := strings.ToLower(t.ParamStr(cards.PKTargetsValid)); strings.Contains(tv, "artifact") || strings.Contains(tv, "land") {
		if perm, targets := spellCastTriggerTarget(reg, name, t); perm != "" {
			if c, ok := castCause(reg, name, shockProbe, targets...); ok {
				c.battlefield = append(c.battlefield, perm)
				out = append(out, c)
			}
		}
	}
	// "An Equipment spell or a spell that targets a creature you control"
	// (Danitha, Sword of Hope's ValidSA$ Spell.Equipment,Spell.IsTargeting).
	if strings.Contains(validSA, "istargeting") && f.IsCreature() {
		add(castCause(reg, name, growthProbe, "p0:"+name))
	}
	if strings.Contains(validSA, "equipment") {
		add(castCause(reg, name, oraclegenQuietEquipment))
	}
	return out
}

// oraclegenQuietEquipment is the Equipment the Equipment-spell cause casts:
// the same quiet Bonesplitter the "equipped creature" fixture attaches.
const oraclegenQuietEquipment = "Bonesplitter"

// creatureWithManaValue picks a creature card of mana value mv to cast: the
// fixed probes first, then the first vanilla creature in name order.
func creatureWithManaValue(reg *cards.Registry, mv int, avoid string) (string, bool) {
	for _, n := range []string{"Llanowar Elves", bearsProbe, "Hill Giant", "Air Elemental"} {
		if c, ok := reg.Lookup(n); ok && len(c.Faces) > 0 && int(c.Faces[0].Cmc()) == mv && n != avoid && oraclegen.XMageKnown(n) {
			return n, true
		}
	}
	var names []string
	for _, c := range reg.AllCards() {
		if len(c.Faces) != 1 {
			continue
		}
		face := c.Faces[0]
		if !face.IsCreature() || int(face.Cmc()) != mv || len(face.Abilities)+len(face.Triggers)+len(face.Statics) != 0 || face.Name == avoid || !oraclegen.XMageKnown(face.Name) {
			continue
		}
		if _, why := oraclegen.PoolFor(face.ManaCost); why != "" {
			continue
		}
		names = append(names, face.Name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return "", false
	}
	return names[0], true
}

// tailLandFromGraveyard returns the land from p0's graveyard for "when this
// land enters from a graveyard" (Oscorp Industries: Origin$ Graveyard). A
// land played from hand never satisfies the origin.
func tailLandFromGraveyard(reg *cards.Registry, name string, t *cards.Trigger) []triggerCause {
	if !strings.EqualFold(strings.TrimSpace(t.ParamStr(cards.PKOrigin)), "Graveyard") {
		return nil
	}
	c, ok := castCause(reg, name, landReturnProbe)
	if !ok {
		return nil
	}
	c.selfInHand = true // off the battlefield: the cause puts it there
	c.graveyard = []string{name}
	return []triggerCause{c}
}

// tailEntersFromExile blinks a creature p0 controls for "whenever a
// permanent you control enters from exile" (Fire Lord Zuko: Origin$ Exile).
func tailEntersFromExile(reg *cards.Registry, name string, t *cards.Trigger) []triggerCause {
	if !strings.EqualFold(strings.TrimSpace(t.ParamStr(cards.PKOrigin)), "Exile") {
		return nil
	}
	c, ok := castCause(reg, name, flickerProbe, "p0:"+flickerTarget)
	if !ok {
		return nil
	}
	c.battlefield = append(c.battlefield, flickerTarget)
	return []triggerCause{c}
}

// tailDeclineSiblingReturn declines every optional trigger the cause's
// resolve steps pose. A graveyard card with a second "you may return this
// card" trigger on the same cause (Fear of Infinity's Eerie enchantment
// clause, which the Room's own entry fires) would otherwise leave the
// graveyard under the runner's accepting fallback before the unlock.
func tailDeclineSiblingReturn(causes []triggerCause) []triggerCause {
	var out []triggerCause
	for _, c := range causes {
		v := c
		v.steps = append([]oraclegen.Step(nil), c.steps...)
		changed := false
		for i := range v.steps {
			if v.steps[i].Op != "resolve" {
				continue
			}
			v.steps[i].Answers = append(append([]oraclegen.Answer(nil), v.steps[i].Answers...), oraclegen.Answer{Kind: "trigger_optional", Pick: []string{"No"}})
			changed = true
		}
		if changed {
			out = append(out, v)
		}
	}
	return out
}
