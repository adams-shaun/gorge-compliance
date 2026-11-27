package scen

import (
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/certdeck"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/effects"
)

// A carrier is the card a forced start moves to make a trigger's event happen:
// the creature that enters for "whenever another creature you control enters",
// the spell cast for "whenever you cast a noncreature spell". It is the most
// inert card the corpus holds that matches the trigger's filter, so the one
// event the start offers is the one the key reads, and the three engines face
// the same small decision tree.

// preferredCarriers are tried first, in order, before the ranked corpus scan:
// Memnite ({0} vanilla artifact creature: a creature, artifact and permanent
// spell no land pays for), Reach Through Mists ({U} instant, draw a card: an
// instant, noncreature spell with no target), Chaplain's Blessing ({W}
// sorcery, gain 5 life: a sorcery with no target), Nyxborn Courser (a vanilla
// enchantment creature: the cheapest vanilla enchantment).
var preferredCarriers = []string{"Memnite", "Reach Through Mists", "Chaplain's Blessing", "Nyxborn Courser"}

// carrierPools holds each registry's ranked carrier candidates, built once.
var carrierPools sync.Map // *cards.Registry -> []*cards.Face

// carrierPool is every card that may carry a start's event, ranked: the
// preferred carriers, then fewest keywords, then by mana value, nonlegendary
// first, then name.
func carrierPool(reg *cards.Registry, b *certdeck.Builder) []*cards.Face {
	if v, ok := carrierPools.Load(reg); ok {
		return v.([]*cards.Face)
	}
	var out []*cards.Face
	for _, c := range reg.AllCards() {
		if len(c.Faces) != 1 || !b.Playable(c) {
			continue
		}
		if f := c.Faces[0]; inertCarrier(f) {
			out = append(out, f)
		}
	}
	pref := func(f *cards.Face) int {
		if i := slices.Index(preferredCarriers, f.Name); i >= 0 {
			return i
		}
		return len(preferredCarriers)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, c := out[i], out[j]
		if pa, pc := pref(a), pref(c); pa != pc {
			return pa < pc
		}
		if ka, kc := len(a.Keywords), len(c.Keywords); ka != kc {
			return ka < kc
		}
		if a.Cmc() != c.Cmc() {
			return a.Cmc() < c.Cmc()
		}
		if a.IsLegendary() != c.IsLegendary() {
			return !a.IsLegendary()
		}
		return a.Name < c.Name
	})
	carrierPools.Store(reg, out)
	return out
}

// inertCarrier is a card a start can cast and that does nothing else: a
// vanilla permanent (no ability, trigger, static or replacement, and no
// keyword but an evergreen one: evergreenKeywords) or
// an instant or sorcery whose one ability gains a fixed amount of life or
// draws a fixed number of cards, with no target and no extra cost. Its cost
// is one basic land per symbol (no X, snow or colourless-only symbol), and it
// is no Aura and no land (a land carrier is a basic, landCarrier).
func inertCarrier(f *cards.Face) bool {
	if f.IsLand() || f.ManaCost == "" || f.ManaCost == "no cost" {
		return false
	}
	for _, tok := range splitCost(f.ManaCost) {
		if !manaTokenRE.MatchString(tok) || tok == "X" || tok == "S" || tok == "C" {
			return false
		}
	}
	if FieldBlock(f) == "aura-unattached" || len(f.Triggers)+len(f.Statics)+len(f.Repls) > 0 || !evergreenOnly(f) {
		return false
	}
	if f.IsPermanent() {
		return len(f.Abilities) == 0
	}
	if len(f.Keywords) > 0 {
		return false
	}
	if len(f.Abilities) != 1 {
		return false
	}
	sa := f.Abilities[0]
	if sa.Sub != nil || sa.API != "GainLife" && sa.API != "Draw" {
		return false
	}
	for k, v := range sa.Params {
		switch k {
		case "ValidTgts", "TgtPrompt", "UnlessCost", "Condition", "ConditionCheckSVar":
			return false
		case "Defined":
			if v != "You" {
				return false
			}
		case "LifeAmount", "NumCards":
			if _, err := strconv.Atoi(v); err != nil {
				return false
			}
		case "Cost":
			if _, why := costTokens(v); why != "" {
				return false
			}
			for _, tok := range splitCost(v) {
				if !manaTokenRE.MatchString(tok) {
					return false
				}
			}
		}
	}
	return true
}

// evergreenKeywords are the keywords a carrier permanent may have: they change
// how it fights, not what the start offers (a vanilla Dragon has flying).
var evergreenKeywords = map[string]bool{"Flying": true, "Reach": true, "Vigilance": true, "Trample": true,
	"Defender": true, "Menace": true, "First Strike": true, "Lifelink": true, "Deathtouch": true}

// evergreenOnly reports whether every keyword of f is an evergreen one.
func evergreenOnly(f *cards.Face) bool {
	for _, k := range f.Keywords {
		if !evergreenKeywords[k] {
			return false
		}
	}
	return true
}

// Controller requirements of a filter alternative.
const (
	ctrlAny = -1
	ctrlYou = 0
	ctrlOpp = 1
)

// filterAlt is one comma alternative of a Forge ValidCard filter
// ("Creature.Other+YouCtrl"), read as far as a start needs it: who controls
// the card, whether it is the card itself, whether it is a token, and a test
// of a carrier's face. Known is false when some property is not understood:
// no carrier is picked for it.
type filterAlt struct {
	ctrl  int
	self  bool
	other bool
	// token is 1 when the alternative requires a token, -1 when it requires a
	// non-token card (a carrier is a card), 0 when it does not say.
	token int
	// tapped is 1 when the alternative requires a tapped permanent, -1 when it
	// requires an untapped one, 0 when it says nothing. Tapped is the state of
	// a permanent on the battlefield, not a property of a face, so only a
	// reader that knows the placed object's state enables it.
	tapped int
	// topLibrary is true when the alternative requires the object to be the
	// top card of its owner's library. Only the static start places a card
	// there, so a lane that casts or places its fixture cannot meet it.
	topLibrary bool
	known      bool
	// history is the first game-history property ("wasDealtDamageThisTurn")
	// the alternative carries: the engine reads it, and nothing has happened
	// yet at the start's step, so no placed object can meet it.
	history string
	// counters are the counters every object the alternative matches must
	// carry ("counters_GE1_P1P1"): a gate the start meets by placing them.
	counters []counterGate
	// commander is true when the alternative reads the positive IsCommander
	// property. The inert carrier pool can never hold a card the seat would
	// designate as its commander (Commander is no evergreen keyword, and a
	// commander-shaped creature is no inert stand-in), so the attacks lane
	// reaches the fixture picker on this flag instead of refusing a pool it
	// cannot fill; only a lane that records the designation (commanderOK)
	// reads the alternative as known at all.
	commander bool
	test      func(f *cards.Face) bool
}

// counterGate is a "counters_<OP><N>_<KIND>" filter property: the object must
// carry at least N counters of KIND.
type counterGate struct {
	kind string
	n    int32
}

// countersAny is the KIND of the generic "HasCounters" gate: the object must
// carry at least one counter of any kind. A placement cannot name "any", so
// placedKind turns it into one P1P1 counter (every kind a start places).
const countersAny = "*"

// placedKind is the counter kind to place for this gate.
func (g counterGate) placedKind() string {
	if g.kind == countersAny {
		return "P1P1"
	}
	return g.kind
}

// parseCounterGate reads "GE1_P1P1" (the part after "counters_") into a gate.
// A comparison a counter placement cannot meet (LE/LT) is not understood.
func parseCounterGate(s string) (counterGate, bool) {
	op, rest, ok := strings.Cut(s, "_")
	if !ok || rest == "" {
		return counterGate{}, false
	}
	letters := 0
	for letters < len(op) && (op[letters] < '0' || op[letters] > '9') {
		letters++
	}
	n, err := strconv.Atoi(op[letters:])
	if err != nil || n < 0 {
		return counterGate{}, false
	}
	switch op[:letters] {
	case "GE":
	case "GT":
		n++
	case "EQ":
	default:
		return counterGate{}, false
	}
	return counterGate{kind: rest, n: int32(n)}, true
}

// matches is test plus the token requirement: a token maker's token passes
// isToken true, every placed card passes false. A tapped/untapped requirement
// is battlefield state a face alone cannot answer, so it does not match here;
// matchesState is for a caller that has placed the object.
func (a filterAlt) matches(f *cards.Face, isToken bool) bool {
	return a.matchesState(f, isToken, false) && a.tapped == 0
}

// matchesState is matches with the placed object's tapped state.
func (a filterAlt) matchesState(f *cards.Face, isToken, tapped bool) bool {
	if a.tapped > 0 && !tapped || a.tapped < 0 && tapped {
		return false
	}
	return (a.token <= 0 || isToken) && (a.token >= 0 || !isToken) && a.test(f)
}

var colourWord = map[string]byte{"White": 'W', "Blue": 'U', "Black": 'B', "Red": 'R', "Green": 'G'}

// faceColours is the colour letters of a face's mana cost (hybrid: both).
func faceColours(f *cards.Face) string {
	var out string
	for _, l := range "WUBRG" {
		if hasShard(f.ManaCost, byte(l)) {
			out += string(l)
		}
	}
	return out
}

func hasShard(cost string, l byte) bool {
	for _, shard := range strings.Fields(cost) {
		for _, part := range shardParts(shard) {
			if len(part) == 1 && part[0] == l {
				return true
			}
		}
	}
	return false
}

// compare reads a "<stat><OP><n>" property (cmcGE5, powerLE2): the stat and
// a test of a value, or ok false.
func compare(prop string) (stat string, test func(int) bool, ok bool) {
	for _, s := range []string{"cmc", "power", "toughness"} {
		rest, found := strings.CutPrefix(prop, s)
		if !found || len(rest) < 3 {
			continue
		}
		n, err := strconv.Atoi(rest[2:])
		if err != nil {
			return "", nil, false
		}
		switch rest[:2] {
		case "GE":
			return s, func(v int) bool { return v >= n }, true
		case "GT":
			return s, func(v int) bool { return v > n }, true
		case "LE":
			return s, func(v int) bool { return v <= n }, true
		case "LT":
			return s, func(v int) bool { return v < n }, true
		case "EQ":
			return s, func(v int) bool { return v == n }, true
		}
	}
	return "", nil, false
}

// historyProp reports whether a filter property words a this-turn event
// ("wasDealtDamageThisTurn", "dealtDamageToYouThisTurn", "blockedThisTurn"):
// the start's step is turn 1's precombat main with nothing yet happened, so the
// engine's reading of it is false for every object. ThisTurnEntered is the
// same for a start: setup permanents and tokens are placed before turn 1 and
// do not count as entered this turn (gorge's
// TestOracleSetupPermanentsDoNotCountAsEnteredThisTurn), so no placement meets
// it.
func historyProp(p string) bool {
	return p == "ThisTurnEntered" || strings.HasSuffix(p, "ThisTurn")
}

// typeWord reports whether w reads as a type word (Creature, Dragon, Legendary):
// capitalised letters and hyphens only.
func typeWord(w string) bool {
	if w == "" || w[0] < 'A' || w[0] > 'Z' {
		return false
	}
	for _, r := range w {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '-') {
			return false
		}
	}
	return true
}

// typeWordTest is the filter test for one type word under a filter head. A
// creature subtype is also met by a changeling or an every-creature-types CDA
// (CR 702.73a); a card type, supertype or noncreature subtype is not.
//
// "Is a creature subtype" is the engine's positive CR 205.3m vocabulary
// (effects.CreatureTypeWords), not "any capitalised word a filter spells that
// is not a card type". Forge filters also spell predicates as capitalised words
// (Commander's Plate's Creature.IsCommander, Seasinger's
// Creature.ControllerControlsIsland), and those are not subtypes: a changeling
// must not stand in for one, or the start places a target the engine rejects.
func typeWordTest(head, w string) func(*cards.Face) bool {
	subtype := strings.EqualFold(head, "Creature") && effects.CreatureTypeWords(w)
	return func(f *cards.Face) bool {
		if f.HasTypeWord(w) {
			return true
		}
		return subtype && (f.HasKeyword("Changeling") || f.AllCreatureTypesCDA())
	}
}

// parseFilter reads a ValidCard filter into its alternatives. A counter gate
// ("counters_GE1_P1P1"), a library-top marker ("TopLibrary") and a battlefield
// state ("tapped"/"untapped") are not understood here: a caller that places no
// counters, no card on top of the library and no permanent of known state
// would pick a carrier that does not meet the filter, so the alternative stays
// unknown. The static start reads the first two with parseFilterStatic, and a
// focus target the start places reads its state with parseFilterPlaced.
func parseFilter(s string) []filterAlt { return parseFilterCounters(s, false, false, false, false) }

// parseFilterStatic is parseFilter for the static start: it places counters and
// may put the affected card on top of the library, so both markers are
// understood.
func parseFilterStatic(s string) []filterAlt { return parseFilterCounters(s, true, true, false, false) }

// parseFilterPlaced is parseFilter for a target the start places on the
// battlefield: whether the placed permanent is tapped is then known from
// StartSeat.Tapped, so the property is read rather than left unknown.
func parseFilterPlaced(s string) []filterAlt {
	return parseFilterCounters(s, false, false, true, false)
}

// parseFilterCommander is parseFilter for a lane that designates its carrier as
// the seat's commander (StartSeat.Commanders): the IsCommander alternative is
// read rather than left unknown, because the lane records the designation the
// engine reads (Game.CommanderOf). Every other lane keeps the alternative
// unknown: an undesignated permanent reads false there whatever its face says.
func parseFilterCommander(s string) []filterAlt {
	return parseFilterCounters(s, false, false, false, true)
}

// parseFilterStaticCommander is parseFilterStatic with the commander
// alternative read, for a lane that designates its fixture.
func parseFilterStaticCommander(s string) []filterAlt {
	return parseFilterCounters(s, true, true, false, true)
}

// typeWordAny is a filter test any of the given type words (or creature
// types) satisfies.
func typeWordAny(words ...string) func(*cards.Face) bool {
	return func(f *cards.Face) bool {
		for _, w := range words {
			if f.HasTypeWord(w) {
				return true
			}
		}
		return false
	}
}

// filterProps are the named card properties a Forge filter tests that are
// neither a type word nor a keyword, keyed by the spelling the scripts use.
// The property and its "non" negation both read this map, so one row covers
// both forms; an unknown property still refuses rather than matching too
// little (a start is a claim about one exact event).
var filterProps = map[string]func(*cards.Face) bool{
	"Colorless":  func(f *cards.Face) bool { return faceColours(f) == "" },
	"Multicolor": func(f *cards.Face) bool { return len(faceColours(f)) > 1 },
	"MultiColor": func(f *cards.Face) bool { return len(faceColours(f)) > 1 }, // Forge's spelling
	// CR 105.2: a monocolored object is exactly one colour.
	"MonoColor": func(f *cards.Face) bool { return len(faceColours(f)) == 1 },
	// CR 700.6: a card is historic if it is legendary, an artifact, or a Saga.
	"Historic": func(f *cards.Face) bool {
		return f.IsLegendary() || f.IsArtifact() || f.HasTypeWord("Saga")
	},
	// CR 700.12: an outlaw has one of these creature types.
	"Outlaw": typeWordAny("Assassin", "Mercenary", "Pirate", "Rogue", "Warlock"),
	// CR 700.8: a party card is a Cleric, Rogue, Warrior, or Wizard.
	"Party": typeWordAny("Cleric", "Rogue", "Warrior", "Wizard"),
	// CR 903.3: each deck designates a legendary card as its commander, and
	// the designated card must be a creature. The designation is an attribute
	// of the card, not a printed characteristic (CR 903.3), which is why the
	// engine reads Game.CommanderOf list membership rather than a keyword:
	// the face test here only picks a carrier that could legally be
	// designated, and the lane that reads this row must record the
	// designation too (parseFilterCounters' commanderOK and
	// designateCommander), or the trigger never fires. The Vehicle,
	// Spacecraft and says-it-can-be variants of CR 903.3/903.3a stay
	// unmatched: refusing those is the existing unknown-property behaviour.
	"IsCommander": func(f *cards.Face) bool { return f.IsLegendary() && f.IsCreature() },
}

// pinMatches reports whether the pinned gorge can evaluate the filter
// predicate p. The engine fails an unrecognised predicate closed, so a start
// built around one (Card.Party, Card.nonHistoric at the current pin) would be
// a trigger that can never fire: the alternative stays unknown and the key
// refuses instead. The gate reads the engine's own census, so a pin bump that
// learns a predicate turns its row on without an edit here.
func pinMatches(p string) bool {
	return len(effects.UnknownPredicates("Card."+p)) == 0
}

// parseFilterCounters reads a ValidCard filter into its alternatives, with the
// same opt-in gates its wrappers set: commanderOK is only true for a lane that
// records the designation on its seat (designateCommander) — the engine answers
// Card.IsCommander from Game.CommanderOf list membership, not from the face, so
// a carrier a lane never designated reads false there and the alternative stays
// unknown elsewhere, like the counter, library-top and tapped gates.
func parseFilterCounters(s string, countersOK, topLibraryOK, tappedOK, commanderOK bool) []filterAlt {
	var out []filterAlt
	for _, raw := range strings.Split(s, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		head, props, _ := strings.Cut(raw, ".")
		a := filterAlt{ctrl: ctrlAny, known: true}
		var tests []func(*cards.Face) bool
		switch head {
		case "Card", "Spell":
		case "Permanent":
			tests = append(tests, (*cards.Face).IsPermanent)
		default:
			if !typeWord(head) {
				a.known = false
			}
			tests = append(tests, typeWordTest(head, head))
		}
		if props != "" {
			for _, p := range strings.Split(props, "+") {
				switch p {
				case "YouCtrl", "YouOwn":
					a.ctrl = ctrlYou
					continue
				case "OppCtrl", "OppOwn", "YouDontCtrl":
					a.ctrl = ctrlOpp
					continue
				case "Self":
					a.self = true
					continue
				case "Other":
					a.other = true
					continue
				case "inZoneBattlefield", "inRealZoneBattlefield":
					continue // a placed carrier is on the battlefield
				case "tapped":
					if !tappedOK {
						a.known = false
					} else {
						a.tapped = 1
					}
					continue
				case "untapped":
					if !tappedOK {
						a.known = false
					} else {
						a.tapped = -1
					}
					continue
				case "TopLibrary":
					// only the static start places the affected card there
					if !topLibraryOK {
						a.known = false
					} else {
						a.topLibrary = true
					}
					continue
				case "!token", "nonToken":
					a.token = -1
					continue
				case "token":
					a.token = 1
					continue
				}
				if test, ok := filterProps[p]; ok {
					if !pinMatches(p) {
						a.known = false // never read a named property as a type word
					}
					if p == "IsCommander" {
						a.commander = true
						if !commanderOK {
							a.known = false // only a lane that designates reads this row
						}
					}
					tests = append(tests, test)
					continue
				}
				if p == "HasCounters" {
					// "a creature with a counter on it": the generic gate. A
					// reader that places no counters cannot meet it, so the
					// alternative stays unknown outside the countersOK lanes
					// (like counters_, which it joins below).
					if !countersOK {
						a.known = false
						continue
					}
					a.counters = append(a.counters, counterGate{kind: countersAny, n: 1})
					continue
				}
				if c, ok := strings.CutPrefix(p, "counters_"); ok {
					g, good := parseCounterGate(c)
					if !good || !countersOK {
						a.known = false
						continue
					}
					a.counters = append(a.counters, g)
					continue
				}
				if name, ok := strings.CutPrefix(p, "named"); ok && name != "" {
					tests = append(tests, func(f *cards.Face) bool { return f.Name == name })
					continue
				}
				if kw, ok := strings.CutPrefix(p, "with"); ok && evergreenKeywords[kw] {
					// withFlying, withDefender: carriers hold only evergreen keywords
					tests = append(tests, func(f *cards.Face) bool { return f.HasKeyword(kw) })
					continue
				}
				if kw, ok := strings.CutPrefix(p, "without"); ok && typeWord(kw) {
					// withoutFlying: a carrier holds no keyword but an evergreen one
					tests = append(tests, func(f *cards.Face) bool { return !f.HasKeyword(kw) })
					continue
				}
				if l, ok := colourWord[p]; ok {
					tests = append(tests, func(f *cards.Face) bool { return hasShard(f.ManaCost, l) })
					continue
				}
				if w, ok := strings.CutPrefix(p, "non"); ok {
					if l, ok := colourWord[w]; ok {
						tests = append(tests, func(f *cards.Face) bool { return !hasShard(f.ManaCost, l) })
						continue
					}
					if test, ok := filterProps[w]; ok {
						if !pinMatches(p) {
							a.known = false
						}
						tests = append(tests, func(f *cards.Face) bool { return !test(f) })
						continue
					}
					if typeWord(w) {
						neg := typeWordTest(head, w)
						tests = append(tests, func(f *cards.Face) bool { return !neg(f) })
						continue
					}
				}
				if stat, cmp, ok := compare(p); ok {
					tests = append(tests, func(f *cards.Face) bool {
						switch stat {
						case "cmc":
							return cmp(int(f.Cmc()))
						case "power":
							return f.IsCreature() && cmp(f.Power())
						}
						return f.IsCreature() && cmp(f.Toughness())
					})
					continue
				}
				if historyProp(p) {
					// a this-turn event no setup placement has undergone: the
					// alternative is not placeable, and refusing by name beats
					// a blind pick the engine would reject
					a.known = false
					if a.history == "" {
						a.history = p
					}
					continue
				}
				if typeWord(p) {
					tests = append(tests, typeWordTest(head, p))
					continue
				}
				a.known = false
			}
		}
		a.test = func(f *cards.Face) bool {
			for _, t := range tests {
				if !t(f) {
					return false
				}
			}
			return true
		}
		out = append(out, a)
	}
	return out
}

// pickCarrier is the first carrier of the pool (carrierPool) that keep
// accepts and that is not the focus card, or "".
func pickCarrier(reg *cards.Registry, b *certdeck.Builder, card string, keep func(*cards.Face) bool) string {
	for _, f := range carrierPool(reg, b) {
		if f.Name != card && keep(f) {
			return f.Name
		}
	}
	return ""
}

// filterCarrier picks, for the first understood, non-Self alternative of the
// filter that some carrier matches, that carrier and the seat that must
// control it (an alternative with no controller: seat 0). extra further
// narrows the carriers (a permanent for an enters trigger).
func filterCarrier(reg *cards.Registry, b *certdeck.Builder, card, filter string, extra func(*cards.Face) bool) (name string, seat int) {
	return filterCarrierMode(reg, b, card, filter, extra, false)
}

// filterCarrierCommander is filterCarrier for a lane that designates the
// carrier it picks as the seat's commander (StartSeat.Commanders,
// designateCommander): the IsCommander alternative is read rather than left
// unknown, because the lane records the designation the engine reads.
func filterCarrierCommander(reg *cards.Registry, b *certdeck.Builder, card, filter string, extra func(*cards.Face) bool) (name string, seat int) {
	return filterCarrierMode(reg, b, card, filter, extra, true)
}

func filterCarrierMode(reg *cards.Registry, b *certdeck.Builder, card, filter string, extra func(*cards.Face) bool, commanderOK bool) (name string, seat int) {
	parse := parseFilter
	if commanderOK {
		parse = parseFilterCommander
	}
	for _, a := range parse(filter) {
		if !a.known || a.self {
			continue
		}
		if n := pickCarrier(reg, b, card, func(f *cards.Face) bool { return extra(f) && a.matches(f, false) }); n != "" {
			if a.ctrl == ctrlOpp {
				return n, 1
			}
			return n, 0
		}
	}
	return "", 0
}

// landCarrier is, for a filter some alternative of which a basic land meets
// ("Land.YouCtrl", "Forest.YouCtrl"), the basic and the seat that plays it,
// or "".
func landCarrier(reg *cards.Registry, filter, generic string) (name string, seat int) {
	for _, a := range parseFilter(filter) {
		if !a.known || a.self {
			continue
		}
		for _, basic := range append([]string{generic}, "Plains", "Island", "Swamp", "Mountain", "Forest") {
			c, ok := reg.Lookup(basic)
			if !ok || len(c.Faces) == 0 || !a.matches(c.Faces[0], false) {
				continue
			}
			if a.ctrl == ctrlOpp {
				return basic, 1
			}
			return basic, 0
		}
	}
	return "", 0
}

// fixtureCarrier is filterCarrier for a static's placements on the battlefield:
// when no inert carrier meets the filter, the simplest playable card that does
// stands in. The inert pool keeps a start's decision tree small, but a static's
// Affected filter often names a permanent class no vanilla card has (Equipment,
// an Aura-bearing host, a counter-gated creature), and refusing it left the key
// unserved. The fallback is still deterministic: fewest abilities, then
// keywords, mana value, legendary last, then name.
func fixtureCarrier(reg *cards.Registry, b *certdeck.Builder, card, filter string, extra func(*cards.Face) bool) (name string, seat int) {
	return fixtureCarrierIn(reg, b, card, filter, extra, true)
}

// zoneFixtureCarrier is fixtureCarrier for a card placed in a zone other than
// the battlefield (a play permission's hand, library, graveyard or exile): an
// Aura or a spell is a legal choice there, so only the battlefield refuses a
// FieldBlock card.
func zoneFixtureCarrier(reg *cards.Registry, b *certdeck.Builder, card, filter string, extra func(*cards.Face) bool) (name string, seat int) {
	return fixtureCarrierIn(reg, b, card, filter, extra, false)
}

func fixtureCarrierIn(reg *cards.Registry, b *certdeck.Builder, card, filter string, extra func(*cards.Face) bool, onBattlefield bool) (name string, seat int) {
	return fixtureCarrierMode(reg, b, card, filter, extra, onBattlefield, nil, false)
}

// castableAlt reports whether a start can establish alternative a by casting
// its carrier from hand rather than placing it: a counter gate
// ("counters_GE1_OIL", "HasCounters") cannot be met, because a setup counter
// has nothing to sit on while the card is still in hand, a token requirement
// names an object a cast card is not, and a top-of-library requirement names a
// card the cast does not put there. A caller that casts the fixture must skip
// such an alternative instead of letting its test alone stand for the gate; a
// caller that places the object on the battlefield meets the counter gate with
// filterGateCounters and the library gate by placing it there.
func castableAlt(a filterAlt) bool {
	return len(a.counters) == 0 && a.token <= 0 && !a.topLibrary
}

// castFixtureCarrierIn is fixtureCarrierIn for a fixture the start casts from
// hand (entersFallback): an alternative the cast cannot establish
// (castableAlt) is not answered, so the ":filter" refusal follows rather than
// a start whose entering object never meets the trigger's filter.
func castFixtureCarrierIn(reg *cards.Registry, b *certdeck.Builder, card, filter string, extra func(*cards.Face) bool, onBattlefield, commanderOK bool) (name string, seat int) {
	return fixtureCarrierMode(reg, b, card, filter, extra, onBattlefield, castableAlt, commanderOK)
}

// fixtureCarrierMode is fixtureCarrierIn with an optional accept gate on the
// fixture fallback: nil takes every understood, non-Self alternative, and
// castFixtureCarrierIn passes castableAlt to drop the ones its cast cannot
// establish.
func fixtureCarrierMode(reg *cards.Registry, b *certdeck.Builder, card, filter string, extra func(*cards.Face) bool, onBattlefield bool, accept func(filterAlt) bool, commanderOK bool) (name string, seat int) {
	// Both passes refuse a FieldBlock card on the battlefield: the inert
	// carrier pool is not filtered for it, and an unattached Aura or a
	// non-permanent listed there aborts the whole start file.
	first := func(f *cards.Face) bool {
		return (!onBattlefield || FieldBlock(f) == "") && (extra == nil || extra(f))
	}
	if n, s := filterCarrierMode(reg, b, card, filter, first, commanderOK); n != "" {
		return n, s
	}
	parse := parseFilterStatic
	if commanderOK {
		parse = parseFilterStaticCommander
	}
	for _, a := range parse(filter) {
		if !a.known || a.self || accept != nil && !accept(a) {
			continue
		}
		var n string
		if onBattlefield {
			n = pickFixture(reg, b, card, a.test, extra, true)
		} else {
			n = pickZoneFixture(reg, b, card, a.test, extra, true)
		}
		if n != "" {
			if a.ctrl == ctrlOpp {
				return n, 1
			}
			return n, 0
		}
	}
	return "", 0
}

// countFixture is the object a computed amount counts ("Count$Valid
// Equipment.YouCtrl"): the same fixture scan, but another copy of the card
// itself is allowed, because a count may name it ("other creatures named
// Relentless Rats"). The amount only has to be non-zero for the effect to
// show, so one fixture is placed. zone is where the counted object will be
// placed; a battlefield count may not choose a FieldBlock card (an unattached
// Aura would make the whole start file unplayable).
//
// gates are the counters the chosen alternative requires ("counters_GE1_OIL"):
// the fixture must carry them for the count to read it.
func countFixture(reg *cards.Registry, b *certdeck.Builder, card, filter, zone string) (name string, seat int, gates []counterGate) {
	onBattlefield := zone == "" || zone == "Battlefield"
	for _, a := range parseFilterStatic(filter) {
		if !a.known || a.self {
			continue
		}
		var n string
		if onBattlefield {
			n = pickFixture(reg, b, card, a.test, nil, false)
		} else {
			n = pickZoneFixture(reg, b, card, a.test, nil, false)
		}
		if n != "" {
			if a.ctrl == ctrlOpp {
				return n, 1, a.counters
			}
			return n, 0, a.counters
		}
	}
	if n, g := landFixtureGated(reg, b, filter); n != "" {
		return n, 0, g
	}
	return "", 0, nil
}

// landFixture is a land some non-Self, non-opponent alternative of filter
// matches, preferring a basic (the mana base already holds every basic) and
// falling back to the least busy nonbasic land for a subtype a basic does not
// have (a Gate, a Desert).
func landFixture(reg *cards.Registry, b *certdeck.Builder, filter string) string {
	n, _ := landFixtureGated(reg, b, filter)
	return n
}

// landFixtureGated is landFixture and the counters the matching alternative
// requires of the land.
func landFixtureGated(reg *cards.Registry, b *certdeck.Builder, filter string) (string, []counterGate) {
	for _, a := range parseFilterStatic(filter) {
		if !a.known || a.self || a.ctrl == ctrlOpp {
			continue
		}
		for _, basic := range []string{"Plains", "Island", "Swamp", "Mountain", "Forest", "Wastes"} {
			c, ok := reg.Lookup(basic)
			if !ok || len(c.Faces) == 0 || !a.test(c.Faces[0]) {
				continue
			}
			return basic, a.counters
		}
		if n := pickLandFixture(reg, b, a.test); n != "" {
			return n, a.counters
		}
	}
	return "", nil
}

// pickLandFixture is the least busy playable nonbasic land keep accepts. The
// mana base is built from basics, so this is only for a subtype a basic does
// not have and a count or a land permission names.
func pickLandFixture(reg *cards.Registry, b *certdeck.Builder, keep func(*cards.Face) bool) string {
	var best *cards.Face
	for _, c := range reg.AllCards() {
		if len(c.Faces) != 1 {
			continue
		}
		f := c.Faces[0]
		if !f.IsLand() || f.IsBasic() || !b.Playable(c) || !keep(f) || deadOnArrival(f) {
			continue
		}
		if best == nil || fixtureLess(f, best) {
			best = f
		}
	}
	if best == nil {
		return ""
	}
	return best.Name
}

// pickFixture is the least busy playable non-land card that keep and extra
// accept, for a placement on the battlefield. Only single-faced cards are
// considered (a start deck binds a front face); lands are landFixture's job.
// A FieldBlock card is never chosen: an unattached Aura (or a non-permanent)
// on the battlefield makes the whole start file unplayable, not just this
// fixture, so a filter only such cards meet keeps its refusal.
func pickFixture(reg *cards.Registry, b *certdeck.Builder, card string, keep, extra func(*cards.Face) bool, skipCard bool) string {
	return pickFixtureIn(reg, b, card, keep, extra, skipCard, true)
}

// pickZoneFixture is pickFixture for a card placed in another zone (a spell
// being cast, or a play permission's hand/library/graveyard card): a FieldBlock
// card is legal there.
func pickZoneFixture(reg *cards.Registry, b *certdeck.Builder, card string, keep, extra func(*cards.Face) bool, skipCard bool) string {
	return pickFixtureIn(reg, b, card, keep, extra, skipCard, false)
}

func pickFixtureIn(reg *cards.Registry, b *certdeck.Builder, card string, keep, extra func(*cards.Face) bool, skipCard, onBattlefield bool) string {
	var best *cards.Face
	for _, c := range reg.AllCards() {
		if len(c.Faces) != 1 {
			continue
		}
		f := c.Faces[0]
		if skipCard && f.Name == card || f.IsLand() || !b.Playable(c) {
			continue
		}
		if onBattlefield && FieldBlock(f) != "" {
			continue
		}
		// A fixture placed with its own as-enters choice is a real move this
		// turn, and the lower refuses the entry stamps setup cannot restate
		// (reasonAsEntersStamp). Prefer any other card, or none: a start that
		// names no fixture refuses by its own reason instead of emitting a
		// position the lower rejects.
		if onBattlefield && asEnters(f) {
			continue
		}
		if !keep(f) || extra != nil && !extra(f) || deadOnArrival(f) {
			continue
		}
		if best == nil || fixtureLess(f, best) {
			best = f
		}
	}
	if best == nil {
		return ""
	}
	return best.Name
}

// fixtureLess ranks fixtures: fewest events or choices (triggers and
// replacements), then fewest options (abilities and statics), then fewest
// keywords, then mana value, nonlegendary first, then name. Every field is a
// face's own, so the order is deterministic.
func fixtureLess(a, c *cards.Face) bool {
	// An event or a choice (a trigger, a replacement) changes what a game asks;
	// a static or an activated ability only adds an option, so it ranks behind.
	events := func(f *cards.Face) int { return len(f.Triggers) + len(f.Repls) }
	surface := func(f *cards.Face) int { return len(f.Abilities) + len(f.Statics) }
	if x, y := events(a), events(c); x != y {
		return x < y
	}
	if x, y := surface(a), surface(c); x != y {
		return x < y
	}
	if x, y := len(a.Keywords), len(c.Keywords); x != y {
		return x < y
	}
	if a.Cmc() != c.Cmc() {
		return a.Cmc() < c.Cmc()
	}
	if a.IsLegendary() != c.IsLegendary() {
		return !a.IsLegendary()
	}
	return a.Name < c.Name
}

// filterGateCounters is the counters the first alternative of filter that
// applies to face requires: the Self alternative when self, else a non-Self
// one that face matches.
func filterGateCounters(filter string, face *cards.Face, self bool) []StartCounter {
	for _, a := range parseFilterStatic(filter) {
		if a.self != self || !a.known || len(a.counters) == 0 {
			continue
		}
		if !self && !a.test(face) {
			continue
		}
		out := make([]StartCounter, 0, len(a.counters))
		for _, g := range a.counters {
			out = append(out, StartCounter{Card: face.Name, Kind: g.placedKind(), N: g.n})
		}
		return out
	}
	return nil
}

// castCarrier puts a carrier in seat s's hand with exactly the basics its
// mana cost takes on that seat's battlefield.
func castCarrier(reg *cards.Registry, s *StartSeat, name string) {
	c, _ := reg.Lookup(name)
	cost := c.Faces[0].ManaCost
	s.Hand = append(s.Hand, name)
	s.Battlefield = append(s.Battlefield, landsFor(splitCost(cost), basicFor(cost))...)
}

// baitPool is the bait candidates beyond the inert carrier pool, built once:
// activation, the permanent faces whose activated ability a bait seat can
// take (a stack target that is an activated ability), and targeted, the
// instant faces whose one ability has exactly one battlefield target a placed
// permanent can meet (a Spell.singleTarget stack target).
type baitPool struct {
	activation []*cards.Face
	targeted   []*cards.Face
}

// baitPools holds each registry's bait pool.
var baitPools sync.Map // *cards.Registry -> *baitPool

// baitPoolOf is reg's bait pool, ranked like fixtureLess so the pick is
// deterministic.
func baitPoolOf(reg *cards.Registry, b *certdeck.Builder) *baitPool {
	if v, ok := baitPools.Load(reg); ok {
		return v.(*baitPool)
	}
	pool := &baitPool{}
	for _, c := range reg.AllCards() {
		if len(c.Faces) != 1 || !b.Playable(c) {
			continue
		}
		f := c.Faces[0]
		if f.IsPermanent() && !f.IsLand() && FieldBlock(f) == "" && !deadOnArrival(f) &&
			len(f.Triggers)+len(f.Statics)+len(f.Repls) == 0 && evergreenOnly(f) && activationAbilitiesOK(f) {
			pool.activation = append(pool.activation, f)
		}
		if f.IsInstant() && targetedCastCarrier(f) {
			pool.targeted = append(pool.targeted, f)
		}
	}
	sort.SliceStable(pool.activation, func(i, j int) bool { return fixtureLess(pool.activation[i], pool.activation[j]) })
	sort.SliceStable(pool.targeted, func(i, j int) bool { return fixtureLess(pool.targeted[i], pool.targeted[j]) })
	baitPools.Store(reg, pool)
	return pool
}

// activationAbilitiesOK reports whether f has an activated ability a bait
// activation can take, and no other one it could not (the engine offers the
// source's ability options in face order, so any one of them may be the bait):
// every non-mana activated ability must be one.
func activationAbilitiesOK(f *cards.Face) bool {
	n := 0
	for _, sa := range f.Abilities {
		if cards.IsManaAbilitySA(sa) {
			continue
		}
		if !activationOK(sa) {
			return false
		}
		n++
	}
	return n > 0
}

// activationOK is whether sa is an activated ability a bait activation can
// take: not a mana ability (a mana ability never uses the stack, so there
// would be nothing to target), no target of its own, and an activation the
// bait seat can pay and take at turn 1's first main priority from the other
// seat (no sorcery-speed or gated activation, a cost of mana, tap, a
// sacrifice or paid life only).
func activationOK(sa *cards.SA) bool {
	if cards.IsManaAbilitySA(sa) {
		return false
	}
	p := sa.Params
	if p["ValidTgts"] != "" || p["TargetType"] != "" || p["TargetValidTargeting"] != "" ||
		p["Optional"] != "" || p["MayChooseTarget"] != "" || p["TargetMin"] == "0" {
		return false
	}
	for _, k := range []string{"SorcerySpeed", "Condition", "ConditionCheckSVar", "Phase", "TurnLimit"} {
		if p[k] != "" {
			return false
		}
	}
	if v := p["Activator"]; v != "" && v != "You" && v != "Any" {
		return false
	}
	if v := p["Cost"]; v != "" {
		mana, why := baitCostTokens(v)
		if why != "" {
			return false
		}
		for _, tok := range mana {
			if tok == "X" || tok == "S" || tok == "C" {
				return false
			}
		}
	}
	return true
}

// baitAbilityOf is f's first non-mana activated ability, the one the engine
// offers first and a bait activation takes, or nil.
func baitAbilityOf(f *cards.Face) *cards.SA {
	for _, sa := range f.Abilities {
		if !cards.IsManaAbilitySA(sa) {
			return sa
		}
	}
	return nil
}

// targetedCastCarrier is whether f is an instant a bait seat can cast as a
// spell with a single target: one ability with exactly one required target on
// the battlefield (no zone, no TargetType, no TargetValidTargeting), a mana
// cost of basic-land tokens only, no keyword, no alternate cost, and only
// numeric effect amounts (no SVar-driven one the start cannot size). It is
// cast from a hand, so FieldBlock (which reads battlefield placement) does
// not gate it.
func targetedCastCarrier(f *cards.Face) bool {
	for _, tok := range splitCost(f.ManaCost) {
		if !manaTokenRE.MatchString(tok) || tok == "X" || tok == "S" || tok == "C" {
			return false
		}
	}
	if len(f.Triggers)+len(f.Statics)+len(f.Repls) > 0 || len(f.Keywords) > 0 {
		return false
	}
	if len(f.Abilities) != 1 {
		return false
	}
	sa := f.Abilities[0]
	if sa.Sub != nil || sa.API == "Charm" {
		return false
	}
	p := sa.Params
	if p["ValidTgts"] == "" || p["TargetType"] != "" || p["TargetValidTargeting"] != "" ||
		p["Cost"] != "" || p["UnlessCost"] != "" || p["Optional"] != "" ||
		p["MayChooseTarget"] != "" || p["TargetMin"] == "0" || p["Choices"] != "" {
		return false
	}
	if zone := strings.ToLower(p["TgtZone"]); zone != "" && zone != "battlefield" {
		return false
	}
	for _, k := range []string{"NumDmg", "NumCards", "LifeAmount", "NumAtt", "NumDef"} {
		if v := p[k]; v != "" {
			if _, err := strconv.Atoi(strings.TrimPrefix(v, "+")); err != nil {
				return false
			}
		}
	}
	return true
}

// baitTgtFor is the target the bait spell's one ability names: a player (the
// focus seat, for a TargetValidTargeting$ "You"/player restriction) or a
// permanent on the bait seat's battlefield (false onBaitSeat: only the focus
// seat's meets the filter, "Permanent.YouCtrl" from the focus play's view).
// The target must meet both the bait's own ValidTgts filter and every required
// TargetValidTargeting$ alternative of the focus play, so the bait's cast and
// the focus play's target are both legal. Only a filter parseFilterPlaced
// reads is placed: an unknown restriction could make either target illegal in
// the engine, and a start that cannot place it keeps the refusal.
func baitTgtFor(reg *cards.Registry, b *certdeck.Builder, card string, sa *cards.SA, sts []stackTgt, seat int) (name string, onBaitSeat, player, ok bool) {
	focus, bait := seat, 1-seat
	needs := false
	for _, st := range sts {
		needs = needs || st.needsTarget
	}
	if needs {
		if targetSeat, ok := baitPlayerSeat(sts, focus); ok {
			for _, raw := range strings.Split(sa.Params["ValidTgts"], ",") {
				raw = strings.TrimSpace(raw)
				if raw == "" {
					continue
				}
				if baitPlayerAllows(raw, targetSeat, bait) {
					return "", false, true, true
				}
			}
		}
	}
	for _, raw := range strings.Split(sa.Params["ValidTgts"], ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		bf, ok := baitPermanentFilter(raw)
		if !ok {
			continue
		}
		side, ok := baitPermanentSide(sts, bf, focus)
		if !ok {
			continue
		}
		if pick := pickBaitPermanent(reg, b, card, sts, bf, side, focus); pick != "" {
			return pick, side == bait, false, true
		}
	}
	return "", false, false, false
}
