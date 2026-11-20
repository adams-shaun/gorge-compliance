// Package certdeck builds the two decks that certify one card: a target deck
// that holds what the card needs from its own side (snow mana, a named type,
// tokens, counters, a full graveyard, equipment, ...) and an opponent deck that
// supplies what it needs from the other (attackers to block, blockers to
// attack into, targets, spells to counter).
//
// The needs are read from the card's script parameters and Oracle text at run
// time (Derive); nothing here is a committed card list. Each need class maps to
// a deterministic ranking of corpus cards, so one corpus and one target give
// byte-identical decks.
package certdeck

import (
	"regexp"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
)

// Need classes. The first group is what the target's own deck supplies, the
// second what the opponent's supplies.
const (
	Snow         = "snow"         // snow lands and snow permanents
	Type         = "type"         // Arg: a creature type the card reads (Elf)
	Counters     = "counters"     // Arg: a counter type (P1P1), cards that put them
	Token        = "token"        // Arg: "" or a token kind (treasure), token makers
	Graveyard    = "graveyard"    // self-mill and discard outlets
	Equipment    = "equipment"    // the card reads Equipped: equipment
	Aura         = "aura"         // the card reads Enchanted: auras
	Bodies       = "bodies"       // the card is an Equipment or Aura: creatures
	Life         = "life"         // Arg: "gain" or "total": lifegain (and, for total, opposing burn)
	Artifacts    = "artifacts"    // control N artifacts
	Enchantments = "enchantments" // control N enchantments
	Lands        = "lands"        // control N lands: ramp and a longer mana base
	LandType     = "landtype"     // Arg: a land type the card names (Forest, Desert): basics of it, or lands with it

	OppAttackers = "opp-attackers" // the card blocks: attackers to block
	OppBlockers  = "opp-blockers"  // the card attacks: blockers to attack into
	OppTargets   = "opp-targets"   // Arg: creature, artifact, enchantment, planeswalker, land or spell
	OppType      = "opp-type"      // Arg: a creature type the card targets or counts on the other side
	OppLandType  = "opp-landtype"  // Arg: a land type the card reads on the other side (Island): basics of it
	OppBurn      = "opp-burn"      // damage that moves the target deck's life total
)

// classOrder is the canonical order of the classes: Derive and ParseNeeds sort
// by it, so a need list is a pure function of the card.
var classOrder = map[string]int{}

func init() {
	for i, c := range []string{Snow, Type, Counters, Token, Graveyard, Equipment, Aura, Bodies, Life,
		Artifacts, Enchantments, Lands, LandType, OppAttackers, OppBlockers, OppTargets, OppType, OppLandType, OppBurn} {
		classOrder[c] = i
	}
}

// OppSide is true for the classes the opponent's deck supplies.
func OppSide(class string) bool { return strings.HasPrefix(class, "opp-") }

// Need is one thing a card needs a deck to hold.
type Need struct{ Class, Arg string }

func (n Need) String() string {
	if n.Arg == "" {
		return n.Class
	}
	return n.Class + ":" + n.Arg
}

func sortNeeds(ns []Need) []Need {
	seen := map[Need]bool{}
	out := ns[:0:0]
	for _, n := range ns {
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if a, b := classOrder[out[i].Class], classOrder[out[j].Class]; a != b {
			return a < b
		}
		return out[i].Arg < out[j].Arg
	})
	return out
}

// unit is one parsed script line: an ability (API set), a trigger or static
// (Mode set) or an SVar's right-hand side, as its $-parameters.
type unit struct {
	api, mode string
	trigger   bool
	event     string // a replacement effect's Event$
	p         map[string]string
}

// repl is whether u is a replacement effect.
func (u unit) repl() bool { return u.event != "" }

// parseLine reads "DB$ Mill | Defined$ You | ..." or "Count$Valid Land.YouCtrl".
func parseLine(s string) unit {
	u := unit{p: map[string]string{}}
	for i, part := range strings.Split(s, " | ") {
		k, v, ok := strings.Cut(part, "$")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if i == 0 && (k == "DB" || k == "AB" || k == "SP") {
			u.api = v
			continue
		}
		u.p[k] = v
	}
	return u
}

// unitsOf is every unit of every face of c, in script order (SVars by name).
func unitsOf(c *cards.Card) []unit {
	var us []unit
	var chain func(s *cards.SA)
	chain = func(s *cards.SA) {
		for n := 0; s != nil && n < 8; s, n = s.Sub, n+1 {
			us = append(us, unit{api: s.API, p: s.Params})
		}
	}
	for _, f := range c.Faces {
		for _, a := range f.Abilities {
			chain(a)
		}
		for _, t := range f.Triggers {
			us = append(us, unit{mode: t.Mode, trigger: true, p: t.Params})
			chain(t.Effect)
		}
		for _, s := range f.Statics {
			us = append(us, unit{mode: s.Mode, p: s.Params})
		}
		for _, r := range f.Repls {
			us = append(us, unit{event: r.Event, p: r.Params})
		}
		names := make([]string, 0, len(f.SVars))
		for k := range f.SVars {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, k := range names {
			us = append(us, parseLine(f.SVars[k]))
		}
	}
	return us
}

// sortedKeys is m's keys in order: a unit's params are a map, and nothing
// that reaches an output may follow map order.
func sortedKeys(m map[string]string) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func oracleOf(c *cards.Card) string {
	var b strings.Builder
	for _, f := range c.Faces {
		b.WriteString(strings.ToLower(f.Oracle))
		b.WriteByte('\n')
	}
	return b.String()
}

var (
	wordSplit    = regexp.MustCompile(`[^A-Za-z0-9_\-']+`)
	counterTypeX = regexp.MustCompile(`counters?_[A-Z]{2}\d+_([A-Za-z0-9]+)|CardCounters\.([A-Za-z0-9]+)`)
)

// coreTypes are the words of a type line that are not subtypes.
var coreTypes = map[string]bool{
	"Artifact": true, "Battle": true, "Conspiracy": true, "Creature": true, "Dungeon": true,
	"Enchantment": true, "Instant": true, "Kindred": true, "Land": true, "Phenomenon": true,
	"Plane": true, "Planeswalker": true, "Scheme": true, "Sorcery": true, "Tribal": true,
	"Vanguard": true, "Basic": true, "Legendary": true, "Snow": true, "World": true,
	"Ongoing": true, "Token": true,
}

// notCreatureTypes are subtypes of the other card types: a land (Forest,
// Desert), an artifact (Equipment, Treasure), an enchantment (Aura, Saga) or a
// spell (Arcane, Adventure). A Land Creature or an Artifact Creature prints
// them next to its creature types and they must not read as creature types.
var notCreatureTypes = map[string]bool{
	"Plains": true, "Island": true, "Swamp": true, "Mountain": true, "Forest": true,
	"Desert": true, "Gate": true, "Lair": true, "Locus": true, "Mine": true, "Power-Plant": true,
	"Tower": true, "Cave": true, "Sphere": true, "Urza's": true,
	"Equipment": true, "Vehicle": true, "Fortification": true, "Contraption": true, "Treasure": true,
	"Food": true, "Clue": true, "Blood": true, "Gold": true, "Powerstone": true, "Map": true, "Junk": true,
	"Aura": true, "Saga": true, "Curse": true, "Shrine": true, "Class": true, "Case": true, "Room": true,
	"Background": true, "Role": true, "Arcane": true, "Trap": true, "Adventure": true, "Lesson": true,
	"Siege": true, "Spacecraft": true, "Attraction": true,
}

// basicLandTypes are the five basic land types, in WUBRG order.
var basicLandTypes = [5]string{"Plains", "Island", "Swamp", "Mountain", "Forest"}

// otherLandTypes are the land subtypes a card may name that have no basic land.
var otherLandTypes = map[string]bool{"Desert": true, "Gate": true, "Lair": true, "Locus": true, "Mine": true,
	"Power-Plant": true, "Tower": true, "Cave": true, "Sphere": true, "Urza's": true}

// basicIndex is the WUBRG index of a basic land type, or -1.
func basicIndex(t string) int {
	for i, b := range basicLandTypes {
		if b == t {
			return i
		}
	}
	return -1
}

func isLandType(t string) bool { return basicIndex(t) >= 0 || otherLandTypes[t] }

// CreatureTypes is every creature subtype some corpus card prints.
func CreatureTypes(reg *cards.Registry) map[string]bool {
	out := map[string]bool{}
	for _, c := range reg.AllCards() {
		for _, f := range c.Faces {
			if !f.HasTypeWord("Creature") {
				continue
			}
			for _, t := range f.Types {
				if !coreTypes[t] && !notCreatureTypes[t] {
					out[t] = true
				}
			}
		}
	}
	return out
}

// validKey is a parameter whose value is a card filter.
func validKey(k string) bool {
	return strings.HasPrefix(k, "Valid") || strings.HasPrefix(k, "Affected") || k == "ChangeType" ||
		k == "Count" || strings.HasPrefix(k, "Present") || strings.HasPrefix(k, "IsPresent")
}

// counterCostX reads a cost that removes counters from another object:
// "SubCounter<1/P1P1/Creature.YouCtrl/a creature you control>". A cost with no
// third field (or a self one) removes the card's own counters, a loyalty or
// charge cost, and needs no partner.
var counterCostX = regexp.MustCompile(`(?:SubCounter|RemoveAnyCounter)<[0-9X]+/([A-Za-z0-9]+)/([^/>]+)`)

// descriptionKey is a parameter that holds English text, not a filter.
func descriptionKey(k string) bool {
	return strings.HasSuffix(k, "Description") || strings.HasSuffix(k, "Prompt") || k == "Name"
}

// Derive reads the needs of c from its script parameters and Oracle text. types
// is CreatureTypes(reg). The result is sorted and has no duplicates.
func Derive(c *cards.Card, types map[string]bool) []Need {
	if c == nil || len(c.Faces) == 0 {
		return nil
	}
	us := unitsOf(c)
	oracle := oracleOf(c)
	var ns []Need
	add := func(class, arg string) { ns = append(ns, Need{class, arg}) }

	isEquipment, isAura := false, false
	delve := false
	for _, f := range c.Faces {
		if f.HasTypeWord("Equipment") {
			isEquipment = true
		}
		if f.HasTypeWord("Aura") {
			isAura = true
		}
		// Delve, escape and dredge read the graveyard like a count does.
		delve = delve || f.HasKeyword("Delve") || f.HasKeyword("Escape") || f.HasKeyword("Dredge")
	}

	if strings.Contains(oracle, "snow") || strings.Contains(oracle, "{s}") {
		add(Snow, "")
	}

	counterArg := ""
	tokenKind := ""
	var lifeGain, lifeTotal, graveyard, wantArt, wantEnch, wantLand, mentionsToken bool
	var wantEquip, wantAura bool
	var blocks, attacks bool
	for _, u := range us {
		if u.trigger {
			switch {
			case u.mode == "Blocks" || u.mode == "BlockersDeclared":
				blocks = true
			case strings.HasPrefix(u.mode, "Attack"):
				attacks = true
			case u.mode == "LifeGained":
				lifeGain = true
			case strings.HasPrefix(u.mode, "CounterAdded"):
				counterArg = firstNonEmpty(counterArg, "P1P1")
			}
		}
		switch {
		case u.event == "AddCounter" || u.event == "RemoveCounter":
			// A counter replacement with no ValidCounterType (Winding
			// Constrictor) reads every kind: the commonest stands in.
			counterArg = firstNonEmpty(counterArg, "P1P1")
		case u.event == "CreateToken":
			mentionsToken = true
		}
		if u.api == "Proliferate" || u.api == "MoveCounter" || u.api == "ReplaceCounter" {
			counterArg = firstNonEmpty(counterArg, "P1P1")
		}
		if u.api == "ReplaceToken" {
			mentionsToken = true
		}
		if u.api == "Counter" || u.api == "CounterAbility" {
			add(OppTargets, "spell")
		}
		for _, k := range sortedKeys(u.p) {
			v := u.p[k]
			if k == "CounterType" && u.api != "PutCounter" && u.api != "PutCounterAll" {
				counterArg = firstNonEmpty(counterArg, v)
			}
			if k == "ValidCounterType" {
				counterArg = firstNonEmpty(counterArg, firstField(v))
			}
			if k == "Cost" {
				if m := counterCostX.FindStringSubmatch(v); m != nil && m[1] != "LOYALTY" &&
					!strings.HasPrefix(m[2], "CARDNAME") && !strings.HasPrefix(m[2], "Card.Self") {
					counterArg = firstNonEmpty(counterArg, m[1])
				}
			}
			if strings.HasPrefix(k, "ValidToken") {
				mentionsToken = true
			}
			if validKey(k) && (strings.Contains(v, "ounters") || strings.Contains(v, "CardCounters")) {
				arg := "P1P1"
				if m := counterTypeX.FindStringSubmatch(v); m != nil {
					arg = firstNonEmpty(m[1], m[2], arg)
				}
				counterArg = firstNonEmpty(counterArg, arg)
			}
			if strings.Contains(v, "LifeTotal") || strings.Contains(k, "LifeTotal") {
				lifeTotal = true
			}
			if !descriptionKey(k) {
				// "Equipped"/"Enchanted" read the permanent a card is attached to:
				// a card that is not itself one reads it for a partner.
				for _, tok := range wordSplit.Split(v, -1) {
					if l := strings.ToLower(tok); strings.HasPrefix(l, "equipped") {
						wantEquip = true
					} else if strings.HasPrefix(l, "enchanted") {
						wantAura = true
					}
				}
			}
			if !validKey(k) {
				continue
			}
			if strings.Contains(v, "Graveyard") && (k == "Count" || strings.Contains(k, "Graveyard") || strings.HasPrefix(k, "Present")) {
				graveyard = true
			}
			tgt := strings.HasPrefix(k, "ValidTgts")
			// A replacement's filter names what it applies to, not what the card
			// needs to control (Winding Constrictor's Artifact.YouCtrl).
			counts := !tgt && !u.repl()
			for _, tok := range wordSplit.Split(v, -1) {
				opp := strings.Contains(v, "OppCtrl") || strings.Contains(v, "Opponent")
				switch {
				case tok == "token" || tok == "Token" || tok == "Tokens":
					mentionsToken = true
				case tok == "Artifact":
					wantArt = wantArt || (counts && strings.Contains(v, "YouCtrl"))
				case tok == "Enchantment":
					wantEnch = wantEnch || (counts && strings.Contains(v, "YouCtrl"))
				case tok == "Land":
					wantLand = wantLand || (counts && strings.Contains(v, "YouCtrl"))
				case tok == "Aura":
					wantAura = wantAura || !tgt
				case tok == "Equipment":
					wantEquip = wantEquip || !tgt
				case isLandType(tok):
					// A land type names lands (a fetch, an untap), whoever's; only an
					// explicit opposing filter puts the need on the other side.
					if opp {
						add(OppLandType, tok)
					} else {
						add(LandType, tok)
					}
				case tokenKinds[tok]:
					tokenKind = firstNonEmpty(tokenKind, strings.ToLower(tok))
				case types[tok]:
					if opp || (tgt && !strings.Contains(v, "YouCtrl")) {
						add(OppType, tok)
					} else {
						add(Type, tok)
					}
				}
			}
			if tgt {
				opp := !strings.Contains(v, "YouCtrl")
				for _, tok := range wordSplit.Split(v, -1) {
					switch tok {
					case "Creature":
						if opp {
							add(OppTargets, "creature")
						}
					case "Artifact", "Enchantment", "Planeswalker", "Land":
						if opp {
							add(OppTargets, strings.ToLower(tok))
						}
					case "Spell":
						add(OppTargets, "spell")
					}
				}
			}
		}
		if strings.HasPrefix(u.p["TargetType"], "Spell") {
			add(OppTargets, "spell")
		}
	}
	if delve || strings.Contains(oracle, "in your graveyard") || strings.Contains(oracle, "from your graveyard") ||
		strings.Contains(oracle, "delirium") || strings.Contains(oracle, "threshold") {
		graveyard = true
	}
	if graveyard {
		add(Graveyard, "")
	}
	if counterArg == "Any" {
		counterArg = "P1P1" // "a counter of any type": the commonest one stands in
	}
	if counterArg != "" {
		add(Counters, counterArg)
	}
	if mentionsToken || tokenKind != "" {
		add(Token, tokenKind)
	}
	switch {
	case isEquipment || isAura:
		add(Bodies, "")
	default:
		if wantEquip {
			add(Equipment, "")
		}
		if wantAura {
			add(Aura, "")
		}
	}
	if blocks {
		add(OppAttackers, "")
	}
	if attacks {
		add(OppBlockers, "")
	}
	if lifeGain {
		add(Life, "gain")
	}
	if lifeTotal || lifeThresholdX.MatchString(oracle) {
		add(Life, "total")
		add(OppBurn, "")
	}
	if wantArt {
		add(Artifacts, "")
	}
	if wantEnch {
		add(Enchantments, "")
	}
	if wantLand {
		add(Lands, "")
	}
	return sortNeeds(ns)
}

// firstField is the first of a comma-separated list ("P1P1,M1M1").
func firstField(v string) string {
	f, _, _ := strings.Cut(v, ",")
	return strings.TrimSpace(f)
}

var lifeThresholdX = regexp.MustCompile(`\d+ or (more|less|fewer) life|life total is|your life total|have \d+ or more life`)

// tokenKinds are the predefined token artifacts a card may read.
var tokenKinds = map[string]bool{"Treasure": true, "Food": true, "Clue": true, "Blood": true, "Powerstone": true, "Map": true, "Junk": true}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// ParseNeeds is the needs -need names: "auto" is Derive's, else a comma-separated
// list of "class" or "class:arg" (a class that takes an argument and gets none
// gets its default: counters P1P1, life total, token any).
func ParseNeeds(spec string, c *cards.Card, types map[string]bool) ([]Need, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" || spec == "auto" {
		return Derive(c, types), nil
	}
	var ns []Need
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		class, arg, _ := strings.Cut(part, ":")
		if _, ok := classOrder[class]; !ok {
			return nil, &UnknownNeedError{part}
		}
		switch class {
		case Counters:
			arg = firstNonEmpty(arg, "P1P1")
		case Life:
			arg = firstNonEmpty(arg, "total")
		}
		ns = append(ns, Need{class, arg})
		if class == Life && arg == "total" {
			ns = append(ns, Need{OppBurn, ""})
		}
		if (class == Type || class == OppType || class == OppTargets || class == LandType || class == OppLandType) && arg == "" {
			return nil, &UnknownNeedError{part + " (needs :arg)"}
		}
	}
	return sortNeeds(ns), nil
}

// UnknownNeedError is a -need name that is no class.
type UnknownNeedError struct{ Name string }

func (e *UnknownNeedError) Error() string {
	cs := make([]string, 0, len(classOrder))
	for c := range classOrder {
		cs = append(cs, c)
	}
	sort.Slice(cs, func(i, j int) bool { return classOrder[cs[i]] < classOrder[cs[j]] })
	return "certdeck: unknown need " + e.Name + " (want auto or " + strings.Join(cs, ", ") + ")"
}
