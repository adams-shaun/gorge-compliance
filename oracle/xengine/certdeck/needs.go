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
	Banding      = "banding"      // the card is, grants or reads banding (CR 702.22): at least BandingCopies creatures with banding

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
		Artifacts, Enchantments, Lands, LandType, Banding, OppAttackers, OppBlockers, OppTargets, OppType, OppLandType, OppBurn} {
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
	api, mode  string
	trigger    bool
	event      string // a replacement effect's Event$
	p          map[string]string
	target     string // the ValidTgts of this unit or the chain that reaches it
	prevTarget string // the target of the unit before this one in the chain
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
	var chain func(s *cards.SA, target string)
	chain = func(s *cards.SA, target string) {
		for n := 0; s != nil && n < 8; s, n = s.Sub, n+1 {
			prev := target
			if v := s.Params["ValidTgts"]; v != "" {
				target = v
			}
			us = append(us, unit{api: s.API, p: s.Params, target: target, prevTarget: prev})
		}
	}
	for _, f := range c.Faces {
		for _, a := range f.Abilities {
			chain(a, "")
		}
		for _, t := range f.Triggers {
			us = append(us, unit{mode: t.Mode, trigger: true, p: t.Params})
			chain(t.Effect, t.Params["ValidTgts"])
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

// CounterSeat is the seat (0 or 1) whose permanent a counters need puts its
// counters on: 1 when the ability that removes or moves counters reads an
// opponent's permanent (Honest Work enchants a creature an opponent controls;
// Mabel targets another creature), else 0 for the card's own counters, a
// replacement or a cost. It follows Defined$/ValidTgts (with the target of the
// chain a SubAbility runs in), never the card's deck. A need whose only bearer
// is a player is CounterPlayer's, not a seat's.
func CounterSeat(c *cards.Card) int {
	for _, sc := range counterScopes(c) {
		if sc.permanent {
			return sc.seat
		}
	}
	return 0
}

// CounterPlayer reports whether a counters need is read from a player's own
// counters (poison, rad, experience, energy; CR 122.1) and no permanent can
// stand in for it. Forge's Defined$ Player/Opponent/You and ValidTgts$
// Player/Opponent name the player, while OppCtrl names a permanent; a start
// seeds counters only on battlefield cards (rules.StartSeat has no
// player-counter field), so scen refuses such a key by name rather than
// silently placing its counter on a permanent the removal never reads.
func CounterPlayer(c *cards.Card) bool {
	player := false
	for _, sc := range counterScopes(c) {
		if sc.permanent {
			return false
		}
		player = player || sc.player
	}
	return player
}

// counterScope is what one counter-moving unit reads. permanent and seat say
// where a placed counter can be exercised (a battlefield card; seat 1 is the
// other player's); player marks a player's own counters, which no placed
// permanent can stand in for.
type counterScope struct {
	permanent bool
	player    bool
	seat      int
}

// counterScopes classifies every counter-moving unit of c, in script order. It
// is the one walk CounterSeat and CounterPlayer share, so the next selector
// kind is added once, in scopeOfFilter or the switch below, and both follow it.
func counterScopes(c *cards.Card) []counterScope {
	if c == nil {
		return nil
	}
	enchantOpp := false
	for _, f := range c.Faces {
		enchantOpp = enchantOpp || enchantsOpponent(f)
	}
	us := unitsOf(c)
	// A unit the chain analysis did not reach (an unlinked SVar) falls back to
	// the targets the card prints.
	var targets []string
	for _, u := range us {
		if v := u.p["ValidTgts"]; v != "" {
			targets = append(targets, v)
		}
	}
	var out []counterScope
	for _, u := range us {
		if !counterMover(u) {
			continue
		}
		def := strings.ToLower(strings.TrimSpace(u.p["Defined"]))
		switch {
		case def == "self" || def == "cardname" || strings.Contains(def, "youctrl") || strings.Contains(def, "youown"):
			out = append(out, counterScope{permanent: true}) // the card's own counters (or a creature you control)
			continue
		case strings.Contains(def, "oppctrl"):
			out = append(out, counterScope{permanent: true, seat: 1})
			continue
		case strings.HasPrefix(def, "valid "):
			out = append(out, scopeOfFilter(strings.TrimSpace(strings.TrimPrefix(def, "valid "))))
			continue
		case playerSelector(def) || strings.Contains(def, "opponent"):
			// "Opponent" is the player; the permanent selector is OppCtrl,
			// above.
			out = append(out, counterScope{player: true, seat: playerSeat(def)})
			continue
		case def == "enchanted":
			seat := 0
			if enchantOpp {
				seat = 1
			}
			out = append(out, counterScope{permanent: true, seat: seat})
			continue
		}
		if u.api == "MoveCounter" {
			// The source, not the destination, carries the counter: the card's
			// own permanent (Source$ Self, ValidSource$ Creature.YouCtrl) or,
			// for Source$ ParentTarget, whatever the parent ability targets
			// (Black Panther's land you control, Rikku's opponent's creature).
			src := u.p["Source"]
			if src == "" {
				src = u.p["ValidSource"]
			}
			if src != "" {
				l := strings.ToLower(src)
				switch {
				case playerSelector(l):
					out = append(out, counterScope{player: true, seat: playerSeat(l)})
				case strings.EqualFold(src, "ParentTarget"):
					// The parent target may name a player's counters too.
					prev := strings.ToLower(strings.TrimSpace(u.prevTarget))
					if playerSelector(prev) {
						out = append(out, counterScope{player: true, seat: playerSeat(prev)})
					} else {
						seat := 0
						if opponentFilter(u.prevTarget) {
							seat = 1
						}
						out = append(out, counterScope{permanent: true, seat: seat})
					}
				case strings.Contains(l, "oppctrl") || strings.Contains(l, "opponent"):
					out = append(out, counterScope{permanent: true, seat: 1})
				default:
					// Self, Creature, Creature.YouCtrl, ...: the card's own.
					out = append(out, counterScope{permanent: true})
				}
				continue
			}
		}
		if u.api == "RemoveCounterAll" {
			// Removing from all matching permanents reaches the card's own
			// ones too, so the start's own side exercises it; only an explicit
			// opposing or player filter moves it off seat 0.
			v := u.p["ValidCards"]
			sc := scopeOfFilter(v)
			if l := strings.ToLower(v); v != "" && !sc.player &&
				!strings.Contains(l, "oppctrl") && !strings.Contains(l, "opponent") {
				sc.seat = 0
			}
			out = append(out, sc)
			continue
		}
		if v := u.p["ValidTgts"]; v != "" {
			out = append(out, scopeOfFilter(v))
			continue
		}
		switch {
		case def == "targeted", def == "valid", def == "remembered", def == "chosencard":
			target := u.target
			if target == "" {
				for _, v := range targets {
					if opponentFilter(v) {
						target = v
						break
					}
				}
			}
			if target != "" {
				out = append(out, scopeOfFilter(target))
				continue
			}
		}
		// A cost, a replacement or an unlinked selector: the card's own.
		out = append(out, counterScope{permanent: true})
	}
	return out
}

// playerSelector reports whether a Defined$/Source$ value names a player rather
// than a permanent the player controls. "Opponent"/"Player"/"You" are players;
// OppCtrl is a permanent (handled before this).
func playerSelector(def string) bool {
	switch def {
	case "player", "players", "opponent", "opponents", "you", "each", "eachplayer",
		"all", "allplayers", "any", "thisplayer", "thatplayer", "targetplayer",
		"targetedplayer", "triggeredplayer":
		return true
	}
	return false
}

// playerSeat is the seat a player selector names: 1 for an opponent, else 0.
// It is only informational when the counters are a player's.
func playerSeat(def string) int {
	if strings.Contains(def, "opponent") {
		return 1
	}
	return 0
}

// playerAlternative reports whether one comma-separated alternative of a card
// filter names a player ("Player", "Opponent", "Player.IsRemembered") rather
// than a permanent ("Creature", "Permanent.ControlledBy You"). The first
// filter word decides: a controller qualifier later in the alternative is not
// the bearer.
func playerAlternative(alt string) bool {
	first, _, _ := strings.Cut(strings.TrimSpace(alt), ".")
	switch strings.ToLower(first) {
	case "player", "players", "opponent", "opponents", "you", "eachplayer", "allplayers":
		return true
	}
	return false
}

// scopeOfFilter classifies a counter target filter: a player alternative is a
// player's counters (which a start cannot seed), a non-player alternative is a
// permanent. "Permanent,Opponent" is both: the permanent branch is enough, so
// one placed counter exercises the removal. A filter with no controller
// restriction reads as another player's permanent (opponentFilter).
func scopeOfFilter(v string) counterScope {
	var sc counterScope
	for _, alt := range strings.Split(filterText(v), ",") {
		if playerAlternative(alt) {
			sc.player = true
			if strings.Contains(strings.ToLower(alt), "opponent") {
				sc.seat = 1
			}
			continue
		}
		sc.permanent = true
		if opponentFilter(alt) {
			sc.seat = 1
		}
	}
	return sc
}

// counterMover reports whether u removes or moves counters from a permanent:
// the units whose bearer CounterSeat follows.
func counterMover(u unit) bool {
	switch u.api {
	case "RemoveCounter", "RemoveCounterAll", "MoveCounter", "AddOrRemoveCounter":
		return true
	}
	return u.event == "RemoveCounter"
}

// enchantsOpponent reports whether f is an Aura whose Enchant names an
// opponent's permanent ("Creature.OppCtrl").
func enchantsOpponent(f *cards.Face) bool {
	p, ok := f.KeywordParam("Enchant")
	if !ok {
		return false
	}
	l := strings.ToLower(p)
	return strings.Contains(l, "oppctrl") || strings.Contains(l, "opponent")
}

// opponentFilter reports whether a card filter names another player's
// permanent. A filter with no controller restriction targets any player's
// permanent, which certdeck deliberately places on the opponent's side even
// when the card could target its own (a self-targeting "target creature" still
// removes from a real permanent there); YouCtrl, YouOwn and Self name the
// card's own.
func opponentFilter(v string) bool {
	l := strings.ToLower(v)
	if l == "" || strings.Contains(l, "youctrl") || strings.Contains(l, "youown") ||
		strings.Contains(l, "card.self") || strings.Contains(l, "cardname") {
		return false
	}
	return true
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

// compoundTypes are subtypes of two words ("Time Lord"). A face carries one as
// a single "Time Lord" word (gorge's CR 205.3m parse) or, in an older parse, as
// each word of its own; a filter spells them with a space, and both would split
// into two words: they are carried as one word joined by a hyphen.
var compoundTypes = []string{"Time Lord"}

// TypeWords is a face's type line with each compound subtype as one word.
func TypeWords(types []string) []string {
	out := make([]string, 0, len(types))
	for i := 0; i < len(types); i++ {
		joined := false
		for _, c := range compoundTypes {
			a, b, _ := strings.Cut(c, " ")
			if types[i] == c {
				out = append(out, a+"-"+b)
				joined = true
				break
			}
			if types[i] == a && i+1 < len(types) && types[i+1] == b {
				out = append(out, a+"-"+b)
				i++
				joined = true
				break
			}
		}
		if !joined {
			out = append(out, types[i])
		}
	}
	return out
}

// namedX is a card-name predicate ("namedCommand Tower", "!namedNikara, Lair
// Scavenger"): the words after it are a card's name, not a type. A name may
// hold ", "; a bare comma starts the next filter of a list.
var namedX = regexp.MustCompile(`!?(?:named|sameName)(?:[^+.,|]|, )*`)

// filterText is a card filter with its card names removed and its compound
// types joined, ready to split into words.
func filterText(v string) string {
	v = namedX.ReplaceAllString(v, "")
	for _, c := range compoundTypes {
		v = strings.ReplaceAll(v, c, strings.ReplaceAll(c, " ", "-"))
	}
	return v
}

// CreatureTypes is every creature subtype some corpus card prints.
func CreatureTypes(reg *cards.Registry) map[string]bool {
	out := map[string]bool{}
	for _, c := range reg.AllCards() {
		for _, f := range c.Faces {
			if !f.HasTypeWord("Creature") {
				continue
			}
			for _, t := range TypeWords(f.Types) {
				if !coreTypes[t] && !notCreatureTypes[t] {
					out[t] = true
				}
			}
		}
	}
	return out
}

// choiceKeys are Valid* parameters that hold a list of choices or an enum, not
// a card filter: ChooseType's ValidTypes$ lists the types a player may name.
var choiceKeys = map[string]bool{"ValidTypes": true, "ValidChoices": true, "ValidMessage": true,
	"ValidKeyword": true, "ValidMode": true, "ValidZone": true, "ValidDestination": true, "ValidSides": true}

// validKey is a parameter whose value is a card filter.
func validKey(k string) bool {
	if descriptionKey(k) || choiceKeys[k] {
		return false
	}
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
	return strings.HasSuffix(k, "Description") || strings.HasSuffix(k, "Desc") || strings.HasSuffix(k, "Prompt") || k == "Name"
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
				if kind, ok := counterKindArg(v); ok {
					counterArg = firstNonEmpty(counterArg, kind)
				}
			}
			if k == "ValidCounterType" {
				if kind, ok := counterKindArg(firstField(v)); ok {
					counterArg = firstNonEmpty(counterArg, kind)
				}
			}
			if k == "CounterNum" && u.api != "PutCounter" && u.api != "PutCounterAll" &&
				strings.EqualFold(strings.TrimSpace(v), "All") && counterTypeKind(u) == "" {
				// "remove all counters" names the amount, not the kind: it is
				// still a counter need (Honest Work). The unit's own
				// CounterType$/ValidCounterType$ wins; read as a set, because
				// CounterNum sorts before CounterType in sortedKeys.
				counterArg = firstNonEmpty(counterArg, "P1P1")
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
			vf := filterText(v)
			for _, tok := range wordSplit.Split(vf, -1) {
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
				for _, tok := range wordSplit.Split(vf, -1) {
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
	counterArg = normalizeCounterKind(counterArg) // "Any"/"All": the commonest kind
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
	// Banding (CR 702.22) forms bands only when several attackers with banding
	// attack together, and its damage rules (702.22j-k) bite only when a band
	// is blocked or blocks: the other side needs creatures on both sides of
	// combat.
	if strings.Contains(oracle, "banding") || strings.Contains(oracle, "bands with other") {
		add(Banding, "")
		blocks, attacks = true, true
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

// counterKindArg normalizes a Forge counter type. "All" and "Any" name every
// kind, not a kind (Honest Work, Mabel), so they are not counter kinds: a
// wildcard reads as the commonest kind (CR 122.1). A blank value has none.
func counterKindArg(v string) (string, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", false
	}
	switch strings.ToLower(v) {
	case "all", "any":
		return "P1P1", true
	}
	return v, true
}

// normalizeCounterKind is counterKindArg for a value that may be empty.
func normalizeCounterKind(v string) string {
	kind, _ := counterKindArg(v)
	return kind
}

// counterTypeKind is the kind a unit names itself: its CounterType$ (unless it
// puts counters) or the first field of its ValidCounterType$, or "" when it
// names none. It reads the parameters as a set, so map order cannot choose
// between them; a remove-all-counters unit with a named kind keeps that kind
// instead of taking the CounterNum$ All stand-in (Witherscale Wurm's M1M1).
func counterTypeKind(u unit) string {
	if u.api != "PutCounter" && u.api != "PutCounterAll" {
		if kind, ok := counterKindArg(u.p["CounterType"]); ok {
			return kind
		}
	}
	if kind, ok := counterKindArg(firstField(u.p["ValidCounterType"])); ok {
		return kind
	}
	return ""
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
