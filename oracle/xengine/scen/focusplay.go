package scen

import (
	"strconv"
	"strings"
	"sync"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/certdeck"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/effects"
)

// The focus play must be offered. A start's focus seat takes its play at the
// first priority of turn 1's precombat main phase (cmd/xengine focusOf), so
// every target the play names has to be met by placed state: a permanent of
// the right kind on the right side, never a spell on the stack or a creature
// in combat (nothing is cast or attacking at that priority). A file whose
// play fails this is refused by name rather than written, and `xengine play
// -start` refuses a game whose focus was never offered all the same
// (reasonStartNotExercised) for what this static check cannot see.
const (
	reasonNeedsStackTarget = "start:needs-stack-target"
	reasonNeedsCombat      = "start:needs-combat"
	reasonNeedsTarget      = "start:needs-target"
)

// deadOnArrival reports whether a creature placed on the battlefield dies to
// state-based actions at once (CR 704.5f): a toughness with no positive base.
// An X creature that enters with counters (Endless One, Walking Ballista,
// Ugin's Conjurant) is 0/0 until its replacement effect puts them, and a
// start places it without its X, so it never survives as a bearer, an
// enabler or a target. A characteristic-defining toughness is the same: the
// start places no cards or counters for its CDA to count (Mortivore with an
// empty graveyard, Soramaro with an empty hand, Scourge of the Skyclaves at
// 20 life), so only a printed positive base (Mwonvuli Ooze's 1+*) proves it
// lives.
func deadOnArrival(f *cards.Face) bool {
	if !f.IsCreature() {
		return false
	}
	if numericToughnessDead(f) {
		return true
	}
	// A CDA has no numeric base: the placement did not put the zone contents
	// or player life its count reads, so an enabler, target or bearer cannot
	// be assumed to survive. The key's own face is checked with the start in
	// hand (cdaToughnessZero), because a static start may place what its CDA
	// counts (Eluge's Islands).
	return cdaToughness(f)
}

// numericToughnessDead reports a creature whose printed toughness begins with
// a number <= 0. A CDA ("*/*", "1+*") or an unparseable toughness is false:
// the caller with the start state decides (cdaToughnessZero), while
// deadOnArrival treats it as dead for a bare placement.
func numericToughnessDead(f *cards.Face) bool {
	if !f.IsCreature() {
		return false
	}
	_, tough, ok := strings.Cut(f.PT, "/")
	if !ok {
		return false // no printed P/T
	}
	tough = strings.TrimSpace(tough)
	base := 0
	for base < len(tough) && tough[base] >= '0' && tough[base] <= '9' {
		base++
	}
	if base == 0 {
		return false
	}
	n, err := strconv.Atoi(tough[:base])
	return err != nil || n <= 0
}

// cdaToughness reports whether the creature's toughness is not a plain
// number: a characteristic-defining value (or a malformed/negative one) the
// start did not place counters or zone contents for.
func cdaToughness(f *cards.Face) bool {
	if !f.IsCreature() {
		return false
	}
	_, tough, ok := strings.Cut(f.PT, "/")
	if !ok {
		return false
	}
	tough = strings.TrimSpace(tough)
	return tough == "" || tough[0] < '0' || tough[0] > '9'
}

// cdaToughnessZero reports whether the CDA that sets face's toughness counts
// a resource the start leaves empty, so the placed creature dies at once. It
// reads the corpus's common CDA forms and says false (not provably zero) for
// one it does not understand, because a false refusal would lose a key whose
// start does place the counted resource.
func cdaToughnessZero(reg *cards.Registry, face *cards.Face, f *StartFile) bool {
	for i := range face.Statics {
		st := &face.Statics[i]
		if !strings.EqualFold(st.Params["CharacteristicDefining"], "True") {
			continue
		}
		expr := face.SVars[st.Params["SetToughness"]]
		if expr == "" {
			continue
		}
		if zero, known := cdaExprZero(reg, face, f, expr); known {
			return zero
		}
	}
	return false
}

// cdaExprZero is cdaToughnessZero's expression read. known is false for a
// form it does not evaluate: the caller must not refuse on a guess.
func cdaExprZero(reg *cards.Registry, face *cards.Face, f *StartFile, expr string) (zero, known bool) {
	switch {
	case strings.HasPrefix(expr, "Count$ValidGraveyard "):
		filter := strings.TrimPrefix(expr, "Count$ValidGraveyard ")
		n, ok := startZoneCount(reg, f, filter, "graveyard")
		if n != 0 || !ok {
			return n == 0, ok
		}
		// A count of the other seat's zone alone (Consuming Aberration's
		// opponent's graveyard) is no zero the generator can vouch for: the
		// start's needs fill their own controller's zone, and the other
		// seat's the generator cannot place.
		if crossSeatZoneFilter(filter) {
			return false, false
		}
		return true, true
	case strings.HasPrefix(expr, "Count$ValidHand "):
		filter := strings.TrimPrefix(expr, "Count$ValidHand ")
		n, ok := startZoneCount(reg, f, filter, "hand")
		if n != 0 || !ok {
			return n == 0, ok
		}
		if crossSeatZoneFilter(filter) {
			return false, false
		}
		return true, true
	case strings.HasPrefix(expr, "Count$Valid "):
		filter := strings.TrimPrefix(expr, "Count$Valid ")
		n, ok := startZoneCount(reg, f, filter, "battlefield")
		if n != 0 || !ok {
			return n == 0, ok
		}
		// The card itself may join the count: its printed types, or a type
		// its own static adds (Ashaya makes its controller's nontoken
		// creatures Forest lands, so it counts itself as a land;
		// Swampbenders makes every land a Swamp). A zero over placed state
		// alone is then no zero. A zone count cannot count the card (it is
		// on the battlefield), so the check is the battlefield branch's.
		return false, !faceCountsItself(face, filter)
	case strings.HasPrefix(expr, "PlayerCountPlayers$HighestLifeTotal"):
		return startHighestLife(f) >= 20, true
	case strings.HasPrefix(expr, "SVar$"):
		// "SVar$Y/NMinus.20": the highest player life total, subtracted from
		// 20 (Scourge of the Skyclaves). Only that one reference is read.
		ref, op, ok := strings.Cut(strings.TrimPrefix(expr, "SVar$"), "/")
		if !ok || !strings.Contains(op, "NMinus") {
			return false, false
		}
		if strings.HasPrefix(face.SVars[ref], "PlayerCountPlayers$HighestLifeTotal") {
			return startHighestLife(f) >= 20, true
		}
	}
	return false, false
}

// startZoneCount is how many cards in zone (both seats, filtered by the
// filter's control clause relative to the key card's controller) the filter
// matches. ok is false when the filter carries a property parseFilter does not
// read: a count the generator cannot take is not a zero.
func startZoneCount(reg *cards.Registry, f *StartFile, filter, zone string) (int, bool) {
	alts := parseFilter(filter)
	if len(alts) == 0 {
		return 0, false
	}
	unknown := false
	for _, a := range alts {
		unknown = unknown || !a.known
	}
	seat := 0
	if f.Focus != nil && f.Focus.Seat != nil {
		seat = *f.Focus.Seat
	}
	n := 0
	for s, sd := range []*StartSeat{&f.Setup.P0, &f.Setup.P1} {
		var names []string
		switch zone {
		case "graveyard":
			names = sd.Graveyard
		case "hand":
			names = sd.Hand
		default:
			names = sd.Battlefield
		}
		for _, name := range names {
			c, ok := reg.Lookup(name)
			if !ok || len(c.Faces) == 0 {
				continue
			}
			matched := false
			for _, a := range alts {
				if !a.known || a.self {
					continue
				}
				if a.ctrl == ctrlYou && s != seat || a.ctrl == ctrlOpp && s == seat {
					continue
				}
				if a.test(c.Faces[0]) {
					matched = true
					break
				}
			}
			if matched {
				n++
			}
		}
	}
	if n == 0 && unknown {
		return 0, false // an unread alternative might match a card the count would see
	}
	return n, true
}

// crossSeatZoneFilter reports a zone filter that counts only the other
// seat's cards (Card.OppOwn): the start's needs fill their own controller's
// zone, so a zero over placed state is not the zero the game reads.
func crossSeatZoneFilter(filter string) bool {
	for _, a := range parseFilter(filter) {
		if a.known && a.ctrl == ctrlOpp {
			return true
		}
	}
	return false
}

// faceCountsItself reports whether the battlefield filter counts the card
// itself: a printed type of its, or a type its own static adds.
func faceCountsItself(face *cards.Face, filter string) bool {
	for _, raw := range strings.Split(filter, ",") {
		raw = strings.TrimSpace(raw)
		head, props, _ := strings.Cut(raw, ".")
		oppos := false
		for _, p := range strings.Split(props, "+") {
			switch p {
			case "OppCtrl", "OppOwn", "YouDontCtrl":
				oppos = true
			}
		}
		if oppos || head == "" || head != "Card" && head != "Permanent" && !typeWord(head) {
			continue
		}
		if face.HasTypeWord(head) {
			return true
		}
		for i := range face.Statics {
			for _, p := range []string{"AddType", "SetTypes", "SetType"} {
				for _, w := range strings.FieldsFunc(face.Statics[i].Params[p], func(r rune) bool {
					return r == '&' || r == ',' || r == ' '
				}) {
					if strings.EqualFold(strings.TrimSpace(w), head) {
						return true
					}
				}
			}
		}
	}
	return false
}

// startHighestLife is the highest life total the start seats hold (20 when a
// seat has no override), the value Scourge of the Skyclaves' CDA subtracts.
func startHighestLife(f *StartFile) int32 {
	hi := int32(20)
	for _, sd := range []*StartSeat{&f.Setup.P0, &f.Setup.P1} {
		if sd.Life != nil && *sd.Life > hi {
			hi = *sd.Life
		}
	}
	return hi
}

// focusCardBlock is why the key's own face cannot be placed on the battlefield
// for the start, or "": a creature whose printed toughness is 0 or less, or a
// dynamic entry counter the start schema cannot restate. It reads only the
// card; a CDA toughness depends on the finished start (cdaToughnessZero, run
// by recipe after the needs are placed), and so does the card's turn-1 upkeep
// (focusUpkeepBlock, also recipe): the start's placed lands and the focus
// play's own mana decide whether the upkeep can be paid. The checks run for
// every key whose card the start puts on the battlefield, not only an ability
// or combat focus: a dies-trigger key whose card is a 0/0 X creature dies
// before the focus play can target it (Hooded Hydra for Murder).
func focusCardBlock(face *cards.Face) string {
	switch {
	case numericToughnessDead(face):
		return "start:dead-on-arrival"
	case entryCounterUnrestatable(face):
		return "start:entry-counter"
	}
	return ""
}

// attackBlock is why the battlefields' scripted attack (Focus.Kind "attack")
// cannot be declared at turn 1's combat, or "": the focus card is not a
// creature, has Defender or a self attack restriction, or enters tapped and
// its own "doesn't untap" replacement keeps it tapped at the first main
// (Grimgrin, Corpse-Born). The start cannot exercise an attack key whose
// attacker cannot attack, and the engine's refusal comes much later as
// start-not-exercised, so the generator refuses it by name.
func attackBlock(reg *cards.Registry, f *StartFile) string {
	fc := f.Focus
	if fc == nil || fc.Kind != "attack" || fc.Card == "" || fc.Seat == nil {
		return ""
	}
	s := focusSeat(f, *fc.Seat)
	if s == nil {
		return ""
	}
	onField := false
	for _, name := range s.Battlefield {
		onField = onField || name == fc.Card
	}
	if !onField {
		return ""
	}
	c, ok := reg.Lookup(fc.Card)
	if !ok || len(c.Faces) == 0 {
		return ""
	}
	face := c.Faces[0]
	if !face.IsCreature() || face.HasKeyword("Defender") || restrictingStatic(face, face, true) != nil {
		return "start:cannot-attack"
	}
	tapped := false
	for _, name := range s.Tapped {
		tapped = tapped || name == fc.Card
	}
	if !tapped {
		for i := range face.Repls {
			r := &face.Repls[i]
			if r.Event == "Moved" && strings.Contains(r.Params["ValidCard"], "Card.Self") &&
				r.Params["Destination"] == "Battlefield" && r.With != nil &&
				r.With.API == "Tap" && strings.Contains(r.With.Params["Defined"], "Self") {
				tapped = true // it enters tapped
				break
			}
		}
	}
	if !tapped {
		return ""
	}
	// The untap step of the focus seat's turn 1 comes before the first main:
	// it untaps a permanent that entered tapped unless its own replacement
	// keeps it from untapping.
	for i := range face.Repls {
		r := &face.Repls[i]
		if r.Event == "Untap" && strings.Contains(r.Params["ValidCard"], "Card.Self") &&
			strings.EqualFold(r.Params["Layer"], "CantHappen") {
			return "start:cannot-attack"
		}
	}
	return ""
}

// tgtReq is one target a play names: the alternatives of its filter (a Forge
// ValidTgts split at commas), lower-cased, and the same alternatives as
// written (raw, for parseFilter's restriction tests); any one meets it. n is
// how many distinct permanents it needs (TargetMin$, at least 1), and unique
// whether they must differ from the permanents the play's earlier targets
// take (TargetUnique$: Cone of Flame's three targets).
type tgtReq struct {
	alts, raw []string
	n         int
	unique    bool
}

// permanentTypeWords are the type words a permanent in the corpus can carry,
// cached per registry, plus the generic battlefield names. A target
// alternative whose head is one of these needs a placed permanent; every other
// head names a player, a spell, any target or another zone, which no placement
// can meet and none needs. Reading the corpus rather than a hand list is what
// keeps a subtype only non-inert cards have (Equipment, Vehicle, an Aura, a
// land subtype) a target the start places instead of one it takes as met.
var permanentTypeWords sync.Map // *cards.Registry -> map[string]bool

func permanentTypeWordsFor(reg *cards.Registry) map[string]bool {
	if v, ok := permanentTypeWords.Load(reg); ok {
		return v.(map[string]bool)
	}
	words := map[string]bool{"permanent": true, "card": true}
	for _, c := range reg.AllCards() {
		for _, f := range c.Faces {
			if !f.IsPermanent() {
				continue
			}
			for _, t := range f.Types {
				words[strings.ToLower(t)] = true
			}
		}
	}
	permanentTypeWords.Store(reg, words)
	return words
}

// permanentTarget reports whether an alternative names a permanent: a type
// word some permanent in the corpus carries, "Permanent" itself, or "Card" on
// the battlefield. "Player", "Any", "Spell" and the other roles are not.
func permanentTarget(reg *cards.Registry, alt string) bool {
	head, _, _ := strings.Cut(alt, ".")
	return permanentTypeWordsFor(reg)[strings.ToLower(strings.TrimSpace(head))]
}

// stackTgt is one target of a play that is a spell or ability on the stack: its
// kind (the spell/activated/triggered alternative TargetType names), the
// alternatives of the ValidTgts filter it must meet as written (raw), the
// TargetType's own restrictions on the object (tt, e.g. Spell.Colorless),
// whether the play may decline it (TargetMin 0, Optional or MayChooseTarget),
// whether it needs a target of its own, single whether the TargetType
// restriction Spell.singleTarget asks the spell to have exactly one target of
// its own (a no-target bait spell never meets it; a targeted bait does), and
// actSelf whether the TargetType restriction Activated.YouCtrl or
// Triggered.YouCtrl asks the ability's controller to be the focus seat, so
// only the focus seat's own activation of the bait serves it.
//
// needsTarget is the target's TargetValidTargeting$ restriction: the spell
// being targeted must itself target a permanent/player (Keep Safe, Outwit,
// Intervene, Cerulean Drake's "targets you", ...). It is not a filter on the
// bait's face but on what the bait does, so it is served by a targeted cast
// bait whose own target meets the restriction (targetedCastBait); a no-target
// inert bait cannot. A restriction no targeted bait can meet keeps
// reasonNeedsStackTarget rather than writing a start whose focus play is never
// offered. ConditionTargetValidTargeting$ and TargetRestriction$ are
// deliberately not read here: they gate the focus play's effect, not which
// spells are legal targets (Meddle is exercised with a no-target bait).
//
// unknownProp is an Activated/Triggered restriction on the ability itself that
// no bait activation can establish (Activated.singleTarget, a property of the
// ability rather than of its source permanent). It keeps the refusal instead
// of accepting an inert activation that is not the target the play asks for.
type stackTgt struct {
	kind        string
	raw         []string
	tt          []filterAlt
	optional    bool
	needsTarget bool
	// valid is the TargetValidTargeting$ restriction read as alternatives: a
	// filter the bait's own target must meet, or a player it must target.
	valid       []validTgt
	single      bool
	actSelf     bool
	unknownProp bool
}

// validTgt is one alternative of a TargetValidTargeting$ restriction: a player
// the targeted spell must target (side 1: the focus seat), or a permanent
// filter its target must meet. any is a plain "Any" restriction, which a player
// or permanent target both meet. side -1 (the opponent) is never produced: the
// start carries only the boolean BaitTargetPlayer and its bait always targets
// the focus seat, so an "Opponent" alternative stays unknown and the play keeps
// its refusal rather than writing a start that targets the wrong player.
type validTgt struct {
	player bool
	side   int
	any    bool
	filter filterAlt
	known  bool
}

// parseTargetValid reads a TargetValidTargeting$ value: a comma list of "You",
// "Player", "Any", or a ValidTgts-style permanent filter
// ("Permanent.YouCtrl+inRealZoneBattlefield"). An alternative no reader
// understands stays unknown, so no bait is picked for it blindly; that includes
// "Opponent", which no start field can name today.
func parseTargetValid(s string) []validTgt {
	var out []validTgt
	for _, raw := range strings.Split(s, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		head := strings.ToLower(strings.TrimSpace(strings.SplitN(raw, ".", 2)[0]))
		switch head {
		case "you":
			out = append(out, validTgt{player: true, side: 1, known: true})
			continue
		case "opponent", "opponents":
			out = append(out, validTgt{})
			continue
		case "player":
			out = append(out, validTgt{player: true, side: 0, known: true})
			continue
		case "any":
			out = append(out, validTgt{any: true, known: true})
			continue
		}
		alts := parseFilterPlaced(raw)
		if len(alts) != 1 || !alts[0].known {
			out = append(out, validTgt{})
			continue
		}
		out = append(out, validTgt{filter: alts[0], known: true})
	}
	return out
}

// stackTargetType reads a Forge TargetType value: a comma list of Spell,
// SpellAbility, Activated and Triggered, each with an optional ".restriction"
// (Spell.singleTarget, Spell.Colorless) or a further comma list
// (Spell.Instant,Spell.Sorcery). A spell alternative is the one a bait can
// serve; the restrictions become a filter on the bait's face, so a bait no
// carrier meets (numTargets) is no bait rather than a wrong one.
// singleTarget is different: it says the spell, or the ability an Activated or
// Triggered target names, must itself have a target, which the bait search
// reads as its single flag (an ability bait activation cannot serve it, so the
// play keeps the refusal).
func stackTargetType(tt string) (kind string, alts []filterAlt, single, actSelf, unknownProp bool) {
	var spell, activated, triggered bool
	for _, tok := range strings.Split(tt, ",") {
		tok = strings.TrimSpace(tok)
		head, rest, hasProp := strings.Cut(tok, ".")
		switch head {
		case "Spell", "SpellAbility":
			spell = true
			switch {
			case !hasProp:
			case rest == "singleTarget":
				single = true
			default:
				alts = append(alts, parseFilter("Spell."+rest)...)
			}
		case "Activated", "Triggered":
			if head == "Activated" {
				activated = true
			} else {
				triggered = true
			}
			switch {
			case !hasProp:
			case rest == "singleTarget":
				single = true
			case rest == "YouCtrl" || rest == "YouOwn":
				// the restriction is on the ability's controller: YouCtrl
				// means only an ability the focus seat itself took can be the
				// target (Lithoform Engine), so the bait activation is its own
				actSelf = true
			case rest == "YouDontCtrl" || rest == "OppCtrl" || rest == "OppOwn":
				// the opponent's ability: the bait seat's own activation
			default:
				unknownProp = true
			}
		}
	}
	switch {
	case spell:
		return "spell", alts, single, actSelf, unknownProp
	case activated:
		return "activated", alts, single, actSelf, unknownProp
	case triggered:
		return "triggered", alts, single, actSelf, unknownProp
	}
	return "", alts, single, actSelf, unknownProp
}

// chainReqs is the targets of the ability chain starting at line params (a
// Params map), following SubAbility$ by name in svars up to a depth. A target
// whose TargetType is Spell, Activated or Triggered is a stack target
// (stacks): it never needs a battlefield permanent, and a play that needs one
// is served by a bait cast instead (stackBait). An optional stack target still
// gets a bait when one fits, so the play's effect is exercised, but it is no
// refusal when none does.
func chainReqs(p map[string]string, svars map[string]string, sub *cards.SA, depth int) (reqs []tgtReq, stacks []stackTgt) {
	for ; depth < 8 && p != nil; depth++ {
		var stack stackTgt
		stack.kind, stack.tt, stack.single, stack.actSelf, stack.unknownProp = stackTargetType(p["TargetType"])
		if stack.kind != "" {
			if v := p["ValidTgts"]; v != "" {
				for _, r := range strings.Split(v, ",") {
					if r = strings.TrimSpace(r); r != "" {
						stack.raw = append(stack.raw, r)
					}
				}
			}
			if len(stack.raw) == 0 {
				stack.raw = []string{"Card"}
			}
			stack.optional = p["TargetMin"] == "0" || p["Optional"] != "" || p["MayChooseTarget"] != ""
			stack.needsTarget = strings.TrimSpace(p["TargetValidTargeting"]) != ""
			if stack.needsTarget {
				stack.valid = parseTargetValid(p["TargetValidTargeting"])
			}
			stacks = append(stacks, stack)
		} else if v := p["ValidTgts"]; v != "" && p["TargetMin"] != "0" && p["Optional"] == "" {
			zone := strings.ToLower(p["TgtZone"])
			if zone == "" && strings.Contains(strings.ToLower(p["Origin"]), "graveyard") {
				// a ChangeZone from a graveyard targets a card there: the
				// graveyard placement (placeGraveTargets) meets it, not the board
				zone = "graveyard"
			}
			if zone == "" || zone == "battlefield" {
				var alts, raw []string
				for _, r := range strings.Split(v, ",") {
					r = strings.TrimSpace(r)
					if a := strings.ToLower(r); !strings.Contains(a, "inzone") && !strings.Contains(a, "stack") {
						alts, raw = append(alts, a), append(raw, r)
					} else {
						alts, raw = append(alts, "any"), append(raw, "Any") // another zone: taken as met
					}
				}
				n, err := strconv.Atoi(p["TargetMin"])
				if err != nil || n < 1 {
					n = 1
				}
				reqs = append(reqs, tgtReq{alts: alts, raw: raw, n: n, unique: p["TargetUnique"] != ""})
			}
		}
		switch {
		case sub != nil:
			p, sub = sub.Params, sub.Sub
		case p["SubAbility"] != "":
			p = parseSVar(svars[p["SubAbility"]])
		default:
			p = nil
		}
	}
	return reqs, stacks
}

// parseSVar reads "DB$ DealDamage | ValidTgts$ ..." as a parameter map.
func parseSVar(line string) map[string]string {
	if line == "" {
		return nil
	}
	m := map[string]string{}
	for _, part := range strings.Split(line, " | ") {
		if k, v, ok := strings.Cut(part, "$"); ok {
			m[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return m
}

// playChoices is the alternative target sets of a play: one set for an
// ordinary ability, one per Charm choice (any one choice may be the one cast).
// stacks is the parallel stack targets of each choice.
func playChoices(face *cards.Face, sa *cards.SA) (choices [][]tgtReq, stacks [][]stackTgt) {
	if sa.API == "Charm" {
		for _, name := range strings.Split(sa.Params["Choices"], ",") {
			reqs, st := chainReqs(parseSVar(face.SVars[strings.TrimSpace(name)]), face.SVars, nil, 0)
			choices, stacks = append(choices, reqs), append(stacks, st)
		}
		return choices, stacks
	}
	reqs, st := chainReqs(sa.Params, face.SVars, sa.Sub, 0)
	return [][]tgtReq{reqs}, [][]stackTgt{st}
}

// enchantReq is the target an Aura's Enchant keyword names ("Enchant:Creature.
// YouCtrl:creature you control"), or false.
func enchantReq(face *cards.Face) (tgtReq, bool) {
	for _, k := range face.Keywords {
		if rest, ok := strings.CutPrefix(k, "Enchant:"); ok {
			filter, _, _ := strings.Cut(rest, ":")
			return tgtReq{alts: strings.Split(strings.ToLower(filter), ","), raw: strings.Split(filter, ",")}, true
		}
	}
	return tgtReq{}, false
}

// altNeed is what one alternative asks of the board: a type word, the side (1
// you, -1 the opponent, 0 either), whether it needs combat, and whether .Other
// excludes the focus play's own source.
type altNeed struct {
	typ    string
	side   int
	combat bool
	other  bool
}

func parseAlt(alt string) altNeed {
	head, rest, _ := strings.Cut(alt, ".")
	n := altNeed{typ: strings.TrimSpace(head)}
	for _, q := range strings.FieldsFunc(rest, func(r rune) bool { return r == '.' || r == '+' }) {
		switch q {
		case "youctrl", "youown":
			n.side = 1
		case "youdontctrl", "oppctrl", "oppown":
			n.side = -1
		case "other":
			n.other = true
		case "blockedthisturn":
			n.combat = true
		}
		if combatProps[q] != "" {
			n.combat = true
		}
	}
	return n
}

// altFilter is the k-th alternative as parseFilterPlaced reads it: its type
// head, every property parseFilter understands, and the tapped state the start
// can place. ok is false when there is no raw alternative to read.
func (r *tgtReq) altFilter(k int) (filterAlt, bool) {
	if k >= len(r.raw) {
		return filterAlt{}, false
	}
	alts := parseFilterPlaced(r.raw[k])
	if len(alts) != 1 {
		return filterAlt{}, false
	}
	return alts[0], true
}

// altMet reports whether permanent f (on the focus seat's side when mine, else
// the opponent's) meets the alternative's filter, and is not the focus play's
// source when the alternative says .Other (Adarkar Valkyrie's "target creature
// other than CARDNAME"). tapped is the placed copy's battlefield state, which
// a filter property such as "tapped"/"untapped" tests. The filter test carries
// the type head as well as the properties, so a subtype (Equipment, Vehicle)
// is not met by any permanent.
func altMet(n altNeed, a filterAlt, f *cards.Face, mine bool, source string, tapped, isToken bool) bool {
	if n.combat {
		return false // nothing attacks or blocks at the first main priority
	}
	if n.side == 1 && !mine || n.side == -1 && mine {
		return false
	}
	if n.other && f.Name == source {
		return false
	}
	if a.history != "" {
		return false // a this-turn event: no placed permanent has undergone it
	}
	if a.test != nil && !a.matchesState(f, isToken, tapped) {
		return false
	}
	return f.IsPermanent()
}

// placeRef names one placed permanent for the TargetUnique bookkeeping: a
// battlefield card by its index, or a minted token by its index in the seat's
// Tokens list. The token flag keeps the two index spaces from colliding, and a
// token index is stable however many cards later placements append.
type placeRef struct {
	side  int
	token bool
	idx   int
}

// unmet is the first target of reqs the placed board cannot meet, with the
// refusal its absence is when no permanent could be placed to meet it. A
// target of n permanents (Switcheroo's "two target creatures") needs n
// distinct ones, and a TargetUnique one permanents no earlier target took.
// source is the focus play's own card: a .Other target must not count it.
func unmet(reg *cards.Registry, f *StartFile, seat int, reqs []tgtReq, source string) (*tgtReq, string) {
	mine, theirs := &f.Setup.P0, &f.Setup.P1
	if seat == 1 {
		mine, theirs = theirs, mine
	}
	taken := map[placeRef]bool{} // a permanent an earlier target took
	for i := range reqs {
		ok, combat := false, false
		var took []placeRef
		for k, alt := range reqs[i].alts {
			n := parseAlt(alt)
			if !n.combat && !permanentTarget(reg, alt) {
				ok = true // a player, any target or another zone: no permanent needed
				continue
			}
			combat = combat || n.combat
			filter, _ := reqs[i].altFilter(k)
			var met []placeRef
			if n.combat && k < len(reqs[i].raw) {
				// a creature in combat: the start's scripted combat (lineMet)
				if side, j, found := lineMet(reg, f, seat, reqs[i].raw[k]); found && !(reqs[i].unique && taken[placeRef{side: side, idx: j}]) {
					met = append(met, placeRef{side: side, idx: j})
				}
			}
			for side, sd := range []*StartSeat{mine, theirs} {
				tapped := sd.TappedCopies()
				for j, name := range sd.Battlefield {
					if reqs[i].unique && taken[placeRef{side: side, idx: j}] {
						continue
					}
					if c, found := reg.Lookup(name); found && len(c.Faces) > 0 && altMet(n, filter, c.Faces[0], side == 0, source, tapped[j], false) {
						met = append(met, placeRef{side: side, idx: j})
					}
				}
				// a minted token is a placed permanent too, and a "target
				// token" filter (Replicator, Dogged Hunter) or a token's type
				// is met only by one. Its own index space cannot collide with
				// a card's and does not shift as later placements append.
				for j, tok := range sd.Tokens {
					ref := placeRef{side: side, token: true, idx: j}
					if reqs[i].unique && taken[ref] {
						continue
					}
					tc, found := reg.Tokens[tok.Script]
					if !found || len(tc.Faces) == 0 || !altMet(n, filter, tc.Faces[0], side == 0, source, tok.Tapped, true) {
						continue
					}
					met = append(met, ref)
				}
			}
			if len(met) >= max(reqs[i].n, 1) {
				ok, took = true, met[:max(reqs[i].n, 1)]
				break
			}
		}
		for _, t := range took {
			taken[t] = true
		}
		if !ok {
			if combat {
				return &reqs[i], reasonNeedsCombat
			}
			return &reqs[i], reasonNeedsTarget
		}
	}
	return nil, ""
}

// focusPlayRefusal is why the focus play cannot be offered at turn 1's first
// main priority, or "": it reads the targets of the spell the focus casts
// (and its Enchant) or of the ability it activates, against the permanents
// the start places (never a spell on the stack or a creature in combat). A
// target on the opponent's side certdeck's needs did not derive (Aether
// Tradewinds' "permanent you don't control") is placed here, from certdeck's
// opposing-target picks, so only a play no placement can serve is refused.
// A play that targets a spell is served by a scripted bait cast (stackBait):
// the start cannot hold a spell on the stack at main 1, but the opponent can
// cast one before the focus play. An ability target, a sorcery-speed cast
// focus, and a spell one no inert bait meets (TargetValidTargeting$), keep the
// refusal.
func focusPlayRefusal(reg *cards.Registry, b *certdeck.Builder, own *cards.Face, sa *cards.SA, card string, f *StartFile) (string, error) {
	fc := f.Focus
	if fc == nil || fc.Seat == nil || (fc.Kind != "cast" && fc.Kind != "ability") {
		return "", nil
	}
	c, ok := reg.Lookup(fc.Card)
	if !ok || len(c.Faces) == 0 {
		return "", nil
	}
	face := c.Faces[0]
	if fc.Card == card && own != nil {
		face = own // the key's own face: a back-face key's ability is not the front's
	}
	var choices [][]tgtReq
	var stacks [][]stackTgt
	castModes := 0
	switch {
	case fc.Kind == "ability" && fc.Card == card && sa != nil:
		choices, stacks = playChoices(face, sa)
	case fc.Kind == "cast":
		for _, a := range face.Abilities {
			if a.Kind != "SP" {
				continue
			}
			castModes++
			ch, st := playChoices(face, a)
			choices, stacks = append(choices, ch...), append(stacks, st...)
		}
		if r, ok := enchantReq(face); ok {
			if len(choices) == 0 {
				choices, stacks = [][]tgtReq{nil}, [][]stackTgt{nil}
			}
			for i := range choices {
				choices[i] = append(choices[i], r)
			}
		}
	}
	// The activation gates the engine reads at offer time (the phase window,
	// the Activation$ condition, the IsPresent$/CheckSVar$ family, a
	// legendary spell's lock, the card's own CantBeCast statics): a gate they
	// cannot satisfy keeps the play from being offered at all, so refuse the
	// start here. A stack target means a bait may be cast first, which can
	// move the active seat, so a turn-owner gate is not decidable yet.
	baitable := false
	for _, sts := range stacks {
		baitable = baitable || len(sts) > 0
	}
	if why := focusGateRefusal(reg, b, face, sa, card, f, baitable); why != "" {
		return why, nil
	}
	if len(choices) == 0 {
		return "", nil // no target to place: a cast with no SP ability (a creature spell) too
	}
	seat := *fc.Seat
	// source is the permanent whose ability the focus play activates: a .Other
	// target must not count it (Adarkar Valkyrie's "target creature other than
	// CARDNAME"). A cast focus's card is on the stack, not the battlefield, so
	// it excludes nothing there.
	source := ""
	if fc.Kind == "ability" {
		source = fc.Card
	}
	// A cast the engine may take by any of its cast modes (more than one SP
	// ability): a bait (and the focus's own cast) must be possible for every
	// mode that requires a stack target, because the start does not script
	// which mode the bot picks. Wash Away's default mode targets a spell "that
	// wasn't cast from its owner's hand", which a bait cast from the hand can
	// never be; its cleave mode can be baited, but the bot takes the default
	// mode. A Charm's choices are not cast modes (they are chosen on
	// resolution), so one unbaited Charm choice does not refuse the cast.
	if fc.Kind == "cast" && castModes > 1 {
		for _, sts := range stacks {
			required := false
			for _, st := range sts {
				required = required || !st.optional
			}
			if !required {
				continue
			}
			if !castableWithBait(face) {
				return reasonNeedsStackTarget, nil
			}
			if _, ok := stackBait(reg, b, card, sts, fc.Kind, sa, seat); !ok {
				return reasonNeedsStackTarget, nil
			}
		}
	}
	reason := ""
	for ci, reqs := range choices {
		var sts []stackTgt
		if ci < len(stacks) {
			sts = stacks[ci]
		}
		var plan stackBaitPlan
		bait := false
		if len(sts) > 0 {
			required := false
			for _, st := range sts {
				required = required || !st.optional
			}
			p, ok := stackBait(reg, b, card, sts, fc.Kind, sa, seat)
			switch {
			case !ok:
				// another choice may not target the stack
				if required {
					reason = reasonNeedsStackTarget
					continue
				}
			case fc.Kind == "cast" && !castableWithBait(face):
				// a sorcery-speed focus spell cannot be cast while the bait is
				// on the stack (CR 117.1a), whoever is the active seat: only an
				// instant or a flash card follows the bait.
				if required {
					reason = reasonNeedsStackTarget
					continue
				}
			default:
				plan, bait = p, true
			}
		}
		for try := 0; ; try++ {
			bad, why := unmet(reg, f, seat, reqs, source)
			if bad == nil {
				if bait {
					// an alternate additional cost ("behold a Jace or pay
					// {1}") is paid in addition to the mana cost; place its
					// mana fallback so the focus cast is affordable with the
					// bait on the stack. No mana fallback: the cast cannot be
					// guaranteed, so keep the refusal.
					lands, ok := baitCostLands(face, basicFor(face.ManaCost))
					if !ok {
						return reasonNeedsStackTarget, nil
					}
					focusSeat(f, seat).Battlefield = append(focusSeat(f, seat).Battlefield, lands...)
					if !placeStackBait(reg, f, seat, plan) {
						return reasonNeedsStackTarget, nil
					}
				}
				return "", nil // a choice every target of which is met
			}
			reason = why
			if try >= maxTargetPlacements {
				break
			}
			if why == reasonNeedsCombat {
				// a creature in combat: script the combat the play waits for
				if !placeLine(reg, b, card, bad, seat, tapsSelf(fc, sa), f) {
					break
				}
				continue
			}
			// an unmet permanent target: place one on the side it names
			placed, tcond := placeOppTarget(reg, b, card, source, bad, seat, f)
			if !placed {
				if tcond != "" {
					return tcond, nil
				}
				break
			}
		}
	}
	return reason, nil
}

// stackBaitPlan is the bait a stack target takes, exactly one of whose three
// forms is set. cast is the spell the focus seat's opponent casts (the
// original form); self is the spell the focus seat itself casts and holds
// priority on (CR 117.3a), for a target only the focus seat could control
// ("target creature spell you control"); ability is the permanent whose
// ability the bait seat activates, for a target that is an activated ability
// (abilitySelf moves that activation to the focus seat, its own battlefield,
// when the target is one only it controls: "Activated.YouCtrl").
// target is the permanent the cast bait spell itself targets (a
// Spell.singleTarget or TargetValidTargeting$ focus play needs a targeted
// bait), targetPlayer whether it targets the focus seat instead (a "targets
// you" restriction), and targetSelf whether that permanent sits on the focus
// seat's battlefield (the bait's filter asks for an opponent-controlled
// permanent); nonInstant marks a cast bait that must be cast on its
// controller's main phase.
type stackBaitPlan struct {
	cast, self, ability, target string
	nonInstant                  bool
	targetSelf                  bool
	targetPlayer                bool
	// abilitySelf is whether the ability bait is activated by the focus seat
	// itself (the carrier sits on its battlefield), for a target only an
	// ability the focus seat controls can be ("Activated.YouCtrl").
	abilitySelf bool
}

// stackBait is the most inert thing the stack targets accept. For spell
// targets: the first instant of the carrier pool that meets every target's
// filter, so the focus seat stays active and no bait land is tapped for a
// sorcery speed; else the first matching permanent or sorcery (nonInstant) --
// except a Spell.singleTarget or TargetValidTargeting$ target, which no inert
// no-target carrier meets and only a targeted cast bait (targetedCastBait)
// serves. A target whose every alternative only the focus seat could control
// (YouCtrl) has no opponent-cast bait: the focus seat casts its own instead
// (selfCastBait). An activated-ability target has no cast bait at all: the bait
// seat activates one of the activation carriers (activationBait). A target no
// form can serve keeps the refusal.
// kind is the focus play's kind ("cast" or "ability"): a self-cast bait works
// only for a cast focus, whose play follows the held self cast on the stack
// (CR 117.3a); an ability focus's activation after its own spell cast cannot
// be lowered (the item's step is the activation, so the replay cannot credit
// the cast it waits behind), and keeps the refusal.
func stackBait(reg *cards.Registry, b *certdeck.Builder, card string, sts []stackTgt, kind string, focus *cards.SA, seat int) (stackBaitPlan, bool) {
	var plan stackBaitPlan
	needed := false
	for _, st := range sts {
		if !st.needsTarget {
			continue
		}
		needed = true
		if st.kind != "spell" {
			return plan, false // only a targeted cast bait serves a spell's target restriction
		}
	}
	if needed {
		name, tgt, tgtSelf, tgtPlayer := targetedCastBait(reg, b, card, sts, seat)
		if name == "" {
			return plan, false
		}
		plan.cast, plan.target, plan.targetSelf, plan.targetPlayer = name, tgt, tgtSelf, tgtPlayer
		return plan, true
	}
	ability, spell := true, true
	single := false
	for _, st := range sts {
		ability = ability && st.kind == "activated"
		spell = spell && st.kind == "spell"
		single = single || st.single
	}
	if ability {
		for _, st := range sts {
			// an activated ability with a single target, or a property of
			// the ability itself, is not met by an inert activation
			if st.single || st.unknownProp {
				return plan, false
			}
		}
		if kind == "ability" && abilityActivationGated(focus) {
			// the focus ability's own gate (IsPresent, a watched SVar) is
			// board state the start does not place
			return plan, false
		}
		name := activationBait(reg, b, card, sts)
		if name == "" {
			return plan, false
		}
		plan.ability = name
		for _, st := range sts {
			if st.actSelf {
				plan.abilitySelf = true
			}
		}
		return plan, true
	}
	if !spell {
		return plan, false // a triggered or mixed target: no bait form serves it
	}
	if single {
		// a no-target carrier is never "a spell with a single target": the
		// bait must itself have one target the start can place
		name, tgt, tgtSelf, tgtPlayer := targetedCastBait(reg, b, card, sts, seat)
		if name == "" {
			return plan, false
		}
		plan.cast, plan.target, plan.targetSelf, plan.targetPlayer = name, tgt, tgtSelf, tgtPlayer
		return plan, true
	}
	name, nonInstant := castBait(reg, b, card, sts)
	if name != "" {
		plan.cast, plan.nonInstant = name, nonInstant
		return plan, true
	}
	name, nonInstant, self := selfCastBait(reg, b, card, sts, kind)
	if self {
		plan.self, plan.nonInstant = name, nonInstant
		return plan, true
	}
	return plan, false
}

// abilityActivationGated reports whether an ability focus's own activation
// asks for state (IsPresent, a watched SVar, a phase or turn gate) that the
// start's fixed board does not establish. An ability-target bait would
// otherwise write a start for a play the engine never offers (Rimewind
// Cryomancer: "Activate only if you control four or more snow permanents").
func abilityActivationGated(sa *cards.SA) bool {
	if sa == nil {
		return false
	}
	for _, k := range []string{
		"Condition", "ConditionCheckSVar", "ConditionDefined", "ConditionPresent",
		"CheckSVar", "SVarCompare", "SecondSVarCompare", "CheckSecondSVar",
		"IsPresent", "PresentCompare", "PresentDefined", "Revolt", "Adamant",
		"Phase", "TurnLimit", "ActivationLimit",
	} {
		if sa.Params[k] != "" {
			return true
		}
	}
	return false
}

// activationBait is the most inert permanent whose activated ability the
// stack targets accept: a card of the activation pool that every target's
// ValidTgts filter (which for an ability target filters the ability's source)
// takes as its source.
func activationBait(reg *cards.Registry, b *certdeck.Builder, card string, sts []stackTgt) string {
	for _, f := range baitPoolOf(reg, b).activation {
		if f.Name == card {
			continue
		}
		if sourceMeets(f, sts) {
			return f.Name
		}
	}
	return ""
}

// targetedCastBait is the most inert instant the stack targets accept whose
// one ability has a target the start can place: a card of the targeted pool
// that meets every target's filter, with the target its own spell names -- a
// permanent and the seat it sits on (bait seat, or focus seat for a filter
// that requires the focus seat's control), or the focus seat itself for a
// TargetValidTargeting$ "You"/player restriction.
func targetedCastBait(reg *cards.Registry, b *certdeck.Builder, card string, sts []stackTgt, seat int) (name, tgt string, tgtSelf, tgtPlayer bool) {
	for _, f := range baitPoolOf(reg, b).targeted {
		if f.Name == card {
			continue
		}
		met := true
		for _, st := range sts {
			if !stackSpell(f, st) {
				met = false
				break
			}
		}
		if !met {
			continue
		}
		if t, onBaitSeat, player, ok := baitTgtFor(reg, b, card, f.Abilities[0], sts, seat); ok {
			return f.Name, t, !onBaitSeat, player
		}
	}
	return "", "", false, false
}

// baitSideFromCtrl is the absolute seat a filter's controller property forces
// (you and opp are the seats the words mean for this reader), or -1 when the
// filter does not name one.
func baitSideFromCtrl(a filterAlt, you, opp int) int {
	switch a.ctrl {
	case ctrlYou:
		return you
	case ctrlOpp:
		return opp
	}
	return -1
}

// baitPermanentFilter reads one alternative of a bait spell's own ValidTgts as
// a permanent filter. "Any" is unrestricted (the player branch is tried
// first), and a filter parseFilterPlaced does not fully read is not used: a
// blind target could make the bait's cast illegal.
func baitPermanentFilter(raw string) (filterAlt, bool) {
	head := strings.ToLower(strings.TrimSpace(strings.SplitN(raw, ".", 2)[0]))
	if head == "any" {
		return filterAlt{ctrl: ctrlAny, known: true, test: func(*cards.Face) bool { return true }}, true
	}
	alts := parseFilterPlaced(raw)
	if len(alts) != 1 || !alts[0].known {
		return filterAlt{}, false
	}
	return alts[0], true
}

// baitPlayerAllows reports whether the bait's own ValidTgts alternative may
// target targetSeat, cast by baitSeat. A plain "Player"/"Any" alternative may
// target any player, "Opponent" only the other seat, "You" only its
// controller; a player restriction this reader does not understand (a
// this-turn history) is not used, so the bait's cast is never made illegal.
func baitPlayerAllows(raw string, targetSeat, baitSeat int) bool {
	head, rest, _ := strings.Cut(strings.ToLower(strings.TrimSpace(raw)), ".")
	switch head {
	case "any":
		return true
	case "you":
		return targetSeat == baitSeat
	case "opponent", "opponents":
		return targetSeat != baitSeat
	case "player":
		if rest == "" {
			return true
		}
		for _, q := range strings.FieldsFunc(rest, func(r rune) bool { return r == '.' || r == '+' }) {
			switch q {
			case "opponent", "opponents", "other":
				return targetSeat != baitSeat
			case "you":
				return targetSeat == baitSeat
			}
		}
	}
	return false
}

// baitPlayerSeat is the absolute seat a player-target bait must name to meet
// every required TargetValidTargeting$ alternative. Only the focus seat is
// nameable: the start field is the boolean BaitTargetPlayer, so an "Opponent"
// alternative arrives here unknown and fails the restriction below. ok is false
// when a restriction has no player alternative, or two restrictions name
// different players.
func baitPlayerSeat(sts []stackTgt, focus int) (seat int, ok bool) {
	seat = -1
	for _, st := range sts {
		if !st.needsTarget {
			continue
		}
		met := false
		for _, v := range st.valid {
			if !v.known || !v.player && !v.any {
				continue
			}
			s := focus
			if seat >= 0 && seat != s {
				continue
			}
			seat, met = s, true
			break
		}
		if !met {
			return 0, false
		}
	}
	if seat < 0 {
		// only a Spell.singleTarget restriction: any player-target bait works,
		// and the focus seat is the inert choice
		seat = focus
	}
	return seat, true
}

// baitPermanentSide is the side (focus seat or bait seat) a permanent bait
// target must sit on so that both the bait's own filter and every required
// TargetValidTargeting$ alternative accept it.
func baitPermanentSide(sts []stackTgt, baitFilter filterAlt, focus int) (int, bool) {
	bait := 1 - focus
	allowed := [2]bool{true, true}
	if s := baitSideFromCtrl(baitFilter, bait, focus); s >= 0 {
		allowed[1-s] = false
	}
	for _, st := range sts {
		if !st.needsTarget {
			continue
		}
		next := [2]bool{}
		for side := 0; side < 2; side++ {
			if !allowed[side] {
				continue
			}
			for _, v := range st.valid {
				if v.player {
					continue
				}
				if v.any {
					next[side] = true
					break
				}
				if !v.known {
					continue
				}
				if s := baitSideFromCtrl(v.filter, focus, bait); s >= 0 && s != side {
					continue
				}
				next[side] = true
				break
			}
		}
		allowed = next
	}
	// the bait seat is the existing default (a target the opponent controls
	// from the focus seat's view); the focus seat is used only when a
	// restriction names it
	side := bait
	if !allowed[side] {
		side = focus
	}
	if !allowed[side] {
		return 0, false
	}
	return side, true
}

// baitPermanentMet reports whether c meets the bait's own target filter and,
// for every required TargetValidTargeting$ alternative, at least one permanent
// alternative on the chosen side.
func baitPermanentMet(sts []stackTgt, baitFilter filterAlt, side, focus int, c *cards.Face) bool {
	if !baitFilter.matchesState(c, false, false) {
		return false
	}
	bait := 1 - focus
	for _, st := range sts {
		if !st.needsTarget {
			continue
		}
		ok := false
		for _, v := range st.valid {
			if v.player {
				continue
			}
			if v.any {
				ok = true
				break
			}
			if !v.known {
				continue
			}
			if s := baitSideFromCtrl(v.filter, focus, bait); s >= 0 && s != side {
				continue
			}
			if v.filter.matchesState(c, false, false) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

// pickBaitPermanent is the most inert permanent the bait target filters
// accept: an inert carrier, a basic land for a land restriction, then the
// corpus's least busy matching card (a subtype only a keyword-bearing card
// has). The start places the returned card on the side the caller chose.
func pickBaitPermanent(reg *cards.Registry, b *certdeck.Builder, card string, sts []stackTgt, baitFilter filterAlt, side, focus int) string {
	keep := func(c *cards.Face) bool {
		return c.IsPermanent() && FieldBlock(c) == "" && !deadOnArrival(c) &&
			baitPermanentMet(sts, baitFilter, side, focus, c)
	}
	if name := pickCarrier(reg, b, card, keep); name != "" {
		return name
	}
	for _, basic := range []string{"Plains", "Island", "Swamp", "Mountain", "Forest", "Wastes"} {
		c, ok := reg.Lookup(basic)
		if ok && len(c.Faces) > 0 && keep(c.Faces[0]) {
			return basic
		}
	}
	if name := pickFixture(reg, b, card, keep, nil, true); name != "" {
		return name
	}
	return pickLandFixture(reg, b, keep)
}

// castBait is the most inert spell the spell targets accept: the first instant
// of the carrier pool that meets every target's filter, so the focus seat
// stays active and no bait land is tapped for a sorcery speed; else the first
// matching permanent or sorcery (nonInstant).
func castBait(reg *cards.Registry, b *certdeck.Builder, card string, sts []stackTgt) (name string, nonInstant bool) {
	pool := carrierPool(reg, b)
	meets := func(f *cards.Face) bool {
		for _, st := range sts {
			if st.kind != "spell" || !stackSpell(f, st) {
				return false
			}
		}
		return true
	}
	for _, instant := range []bool{true, false} {
		for _, f := range pool {
			if f.Name == card || f.IsLand() || f.IsInstant() != instant || !meets(f) {
				continue
			}
			return f.Name, !instant
		}
	}
	return "", false
}

// selfCastBait is the most inert spell of the carrier pool the spell targets
// accept when cast by the focus seat itself: every alternative the target
// reads is one its own spell meets ("Creature.YouCtrl", a plain "Spell"), and
// one no opponent-cast bait meets (YouCtrl) is met by the focus seat's own
// cast. An instant first (the focus seat casts it at any priority), else a
// permanent or sorcery (nonInstant). A Spell.singleTarget target has no self
// bait: a targeted self bait would need its own placed target and the
// focus-seat cast forms in the corpus are plain spells.
func selfCastBait(reg *cards.Registry, b *certdeck.Builder, card string, sts []stackTgt, kind string) (name string, nonInstant, ok bool) {
	if kind != "cast" {
		return "", false, false // an ability focus cannot hold its own cast
	}
	for _, st := range sts {
		if st.single {
			return "", false, false
		}
	}
	pool := carrierPool(reg, b)
	for _, instant := range []bool{true, false} {
		for _, f := range pool {
			if f.Name == card || f.IsLand() || f.IsInstant() != instant || !selfSpellMeets(f, sts) {
				continue
			}
			return f.Name, !instant, true
		}
	}
	return "", false, false
}

// stackSpell reports whether f is a spell the stack target takes: an
// alternative of its ValidTgts filter, and one of its TargetType's own
// restrictions when it has any (Card for a target with none).
func stackSpell(f *cards.Face, st stackTgt) bool {
	ok := false
	for _, raw := range st.raw {
		if spellAlt(raw, f) {
			ok = true
			break
		}
	}
	if !ok {
		return false
	}
	for _, a := range st.tt {
		if !a.known || a.self || a.ctrl == ctrlYou {
			continue
		}
		if a.test(f) {
			return true
		}
	}
	return len(st.tt) == 0
}

// spellAlt reports whether the stack object's face f meets the filter
// alternative raw, as the focus play reads it: an alternative with a property
// parseFilter does not know, the card itself, or a controller only the focus
// seat can have is not one a bait can meet, and an instant or sorcery is never
// a permanent's type.
func spellAlt(raw string, f *cards.Face) bool {
	for _, a := range parseFilter(raw) {
		if !a.known || a.self || a.ctrl == ctrlYou {
			continue
		}
		if a.test(f) {
			return true
		}
	}
	return false
}

// castableWithBait reports whether a cast focus can be cast while the bait is
// on the stack. CR 117.1a lets a sorcery-speed spell be cast only at a moment
// the stack is empty, so a non-instant, non-flash focus cannot follow any
// bait; an instant or a flash card can.
func castableWithBait(face *cards.Face) bool {
	return face.IsInstant() || face.HasKeyword("Flash")
}

// selfSpellMeets is stackSpell for a bait the focus seat casts itself: an
// alternative a controller only the opponent could have is not one its own
// spell meets, and one only the focus seat could control (YouCtrl) is.
func selfSpellMeets(f *cards.Face, sts []stackTgt) bool {
	for _, st := range sts {
		ok := false
		for _, raw := range st.raw {
			if selfSpellAlt(raw, f) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
		met := len(st.tt) == 0
		for _, a := range st.tt {
			if !a.known || a.self || a.ctrl == ctrlOpp {
				continue
			}
			if a.test(f) {
				met = true
				break
			}
		}
		if !met {
			return false
		}
	}
	return true
}

// selfSpellAlt is spellAlt read from the focus seat's side: an alternative the
// opponent of the focus seat alone could control (OppCtrl, YouDontCtrl) is not
// one its own cast meets, and YouCtrl is.
func selfSpellAlt(raw string, f *cards.Face) bool {
	for _, a := range parseFilter(raw) {
		if !a.known || a.self || a.ctrl == ctrlOpp {
			continue
		}
		if a.test(f) {
			return true
		}
	}
	return false
}

// sourceMeets reports whether a permanent face f can be the source of the
// activated ability the stack target takes: an alternative of its ValidTgts
// filter (which for an ability target filters the ability's source permanent),
// read as the focus play reads it (spellAlt).
func sourceMeets(f *cards.Face, sts []stackTgt) bool {
	for _, st := range sts {
		ok := false
		for _, raw := range st.raw {
			if spellAlt(raw, f) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

// baitCostLands is the mana a cast focus pays beyond its ManaCost through an
// AlternateAdditionalCost$ that has a pure-mana alternative ("Behold<1/Jace>:1",
// "tapXType<1/Artifact>:1", "Sac<...>:3 B"). ok is false when no alternative is
// pure mana (only a sacrifice, discard, ...), which a generated start cannot
// guarantee; the caller then keeps the refusal instead of writing a start
// whose focus cast is never offered.
func baitCostLands(face *cards.Face, generic string) ([]string, bool) {
	v, ok := face.KeywordParam("AlternateAdditionalCost")
	if !ok {
		return nil, true
	}
	for _, alt := range strings.Split(v, ":") {
		toks := splitCost(alt)
		if len(toks) == 0 {
			continue
		}
		mana := true
		for _, tok := range toks {
			if !manaTokenRE.MatchString(tok) || tok == "X" || tok == "S" || tok == "C" {
				mana = false
				break
			}
		}
		if mana {
			return landsFor(toks, generic), true
		}
	}
	return nil, false
}

// focusSeat is the setup seat that takes the focus play.
func focusSeat(f *StartFile, seat int) *StartSeat {
	if seat == 1 {
		return &f.Setup.P1
	}
	return &f.Setup.P0
}

// placeBait puts the bait spell in the focus seat opponent's hand with the
// lands its cost takes, and on a non-instant bait makes that seat active so it
// can cast it at turn 1's first main phase.
func placeBait(reg *cards.Registry, f *StartFile, seat int, name string, nonInstant bool) {
	castCarrier(reg, baitSeatOf(f, seat), name)
	f.Focus.Bait = name
	if nonInstant {
		f.Active = 1 - seat
	}
}

// placeStackBait writes the plan's bait into the start: an opponent-cast bait
// through placeBait, with its own target on the bait seat's battlefield when
// the bait spell has one; a focus-seat self-cast bait in the focus seat's hand
// with the lands its cost takes; an ability bait on the bait seat's
// battlefield with the lands its ability cost takes.
func placeStackBait(reg *cards.Registry, f *StartFile, seat int, plan stackBaitPlan) bool {
	switch {
	case plan.cast != "":
		placeBait(reg, f, seat, plan.cast, plan.nonInstant)
		if plan.target != "" {
			s := baitSeatOf(f, seat)
			if plan.targetSelf {
				s = focusSeat(f, seat)
			}
			s.Battlefield = append(s.Battlefield, plan.target)
			f.Focus.BaitTarget = plan.target
		}
		if plan.targetPlayer {
			// the bait spell targets the focus seat itself ("counter target
			// spell that targets you"); no permanent is placed for it
			f.Focus.BaitTargetPlayer = true
		}
	case plan.self != "":
		castCarrier(reg, focusSeat(f, seat), plan.self)
		f.Focus.BaitSelf = plan.self
	case plan.ability != "":
		c, found := reg.Lookup(plan.ability)
		if !found || len(c.Faces) == 0 {
			return false
		}
		sa := baitAbilityOf(c.Faces[0])
		if sa == nil {
			return false
		}
		cost := sa.Params["Cost"]
		mana, why := baitCostTokens(cost)
		if why != "" {
			return false
		}
		bait := append([]string{plan.ability}, landsFor(mana, basicFor(cost))...)
		// an ability target the focus seat itself controls ("Activated.YouCtrl")
		// is met by its own activation: the carrier sits on its battlefield,
		// and its lands come first so the engine pays the bait's cost from
		// them, not from the colour the focus ability needs.
		s := baitSeatOf(f, seat)
		if plan.abilitySelf {
			s = focusSeat(f, seat)
			s.Battlefield = append(bait, s.Battlefield...)
		} else {
			s.Battlefield = append(s.Battlefield, bait...)
		}
		f.Focus.BaitAbility = plan.ability
		f.Focus.BaitAbilitySelf = plan.abilitySelf
	}
	return true
}

// baitSeatOf is the start seat that casts the bait: the seat the focus does
// not take its play from.
func baitSeatOf(f *StartFile, seat int) *StartSeat {
	s := &f.Setup.P1
	if seat == 1 {
		s = &f.Setup.P0
	}
	return s
}

// tapsSelf reports whether the focus play is an ability whose cost taps the
// focus card ({T}), which then cannot attack for its own play.
func tapsSelf(fc *StartFocus, sa *cards.SA) bool {
	if fc.Kind != "ability" || sa == nil {
		return false
	}
	for _, tok := range strings.Fields(sa.Params["Cost"]) {
		if tok == "T" {
			return true
		}
	}
	return false
}

// maxTargetPlacements bounds the permanents focusPlayRefusal places for one
// choice's targets (Cone of Flame: three; Hex: six). It is a safety cap on
// repeated placements, not a target count: a requirement of fewer targets ends
// as soon as unmet sees it, and one of more keeps its named refusal.
const maxTargetPlacements = 8

// placeOppTarget places a permanent for the unmet target, and says whether one
// fit. An alternative a placed permanent can meet goes on the side it names:
// the opponent's first when it allows either ("target permanent"), then the
// focus seat's (YouCtrl/YouOwn), whatever type word it carries -- the corpus's
// permanent type words, not a hand list, so Equipment and Vehicle are placed
// too. The most inert carrier (carrierPool) is tried before the corpus's least
// busy matching fixture, so a subtype only a keyword-bearing card has is still
// placed; a land target's land comes from the mana base. A filter parseFilter
// does not fully read falls back to certdeck's most inert opposing-target pick,
// so no card is placed blind.
// placeOppTarget places a permanent the unmet target requirement r accepts.
// It returns whether it placed one and, when it did not, the named refusal
// for a target whose filter carries a predicate no part of the filter reads
// (the engine's UnknownPredicates census): a blind certdeck pick cannot be
// trusted to meet it, so the play is refused by the condition's name rather
// than written with a target the engine would reject.
func placeOppTarget(reg *cards.Registry, b *certdeck.Builder, card, source string, r *tgtReq, seat int, f *StartFile) (bool, string) {
	s, own := &f.Setup.P1, &f.Setup.P0
	if seat == 1 {
		s, own = own, s
	}
	for _, mine := range []bool{false, true} {
		for k, alt := range r.alts {
			n := parseAlt(alt)
			if n.combat || !permanentTarget(reg, alt) {
				continue
			}
			if n.side == 1 && !mine || n.side == -1 && mine {
				continue
			}
			filter, ok := r.altFilter(k)
			if !ok || !filter.known {
				continue // restrictions parseFilter does not read: no pick placed blind
			}
			sd := s
			if mine {
				sd = own
			}
			if filter.token > 0 {
				// a "target token" filter (Replicator, Dogged Hunter) has no
				// card to place: mint the most inert token the filter takes
				script, ok := pickTargetToken(reg, filter, filter.tapped > 0)
				if !ok {
					continue
				}
				sd.Tokens = append(sd.Tokens, StartToken{Script: script, Tapped: filter.tapped > 0})
				return true, ""
			}
			name := pickTargetPermanent(reg, b, card, source, n, filter, r.raw[k], mine)
			if name == "" {
				continue
			}
			sd.Battlefield = append(sd.Battlefield, name)
			if filter.tapped > 0 {
				// record the state the target was chosen for: an untapped
				// carrier would leave the focus play unoffered (start-not-
				// exercised), which is a refusal, not a warning
				sd.Tapped = append(sd.Tapped, name)
			}
			return true, ""
		}
	}
	// A filter parseFilter does not fully read (a counter gate, say): certdeck
	// can place a pick that meets it (placeCounters) where a carrier cannot.
	// A predicate no part of the filter recognises is different: the matcher
	// itself never matches it, so a blind pick cannot meet it and the play is
	// refused by the condition's name.
	unknown := ""
	typ := "creature"
	for k, alt := range r.alts {
		n := parseAlt(alt)
		if n.side == 1 || n.combat || !permanentTarget(reg, alt) {
			continue // only the focus seat's own: not placeable on the other side
		}
		if filter, ok := r.altFilter(k); ok && filter.history != "" {
			// a this-turn event no placed object has undergone
			if unknown == "" {
				unknown = condKind(filter.history)
			}
			continue
		} else if ok && filter.known {
			continue // read above: placed, or no card meets it
		}
		if preds := effects.UnknownPredicates(r.raw[k]); len(preds) > 0 {
			if unknown == "" {
				unknown = condKind(preds[0])
			}
			continue
		}
		if n.typ != "permanent" && n.typ != "card" {
			typ = n.typ
		}
		plan, err := b.Build(card, []certdeck.Need{{Class: certdeck.OppTargets, Arg: typ}})
		if err != nil {
			break
		}
		picks := plan.Opponent.Provided[certdeck.Need{Class: certdeck.OppTargets, Arg: typ}.String()]
		before := len(s.Battlefield)
		if err := pickEnabler(reg, b, s, false, picks); err != nil {
			break
		}
		return len(s.Battlefield) > before, ""
	}
	if unknown != "" {
		return false, "start:target-condition:" + unknown
	}
	return false, ""
}

// pickTargetPermanent is the most inert permanent a target alternative accepts
// on the side mine names: a carrier of the inert pool first, then the corpus's
// least busy matching fixture (an Equipment or Vehicle carries a keyword the
// pool excludes), then a land for a land target (the pool holds no lands). The
// focus play's own source is never returned for a .Other target.
func pickTargetPermanent(reg *cards.Registry, b *certdeck.Builder, card, source string, n altNeed, a filterAlt, raw string, mine bool) string {
	// the copy placeOppTarget appends is tapped exactly when the alternative
	// requires it; pick the face against that state
	tapped := a.tapped > 0
	keep := func(c *cards.Face) bool {
		return c.IsPermanent() && FieldBlock(c) == "" && !deadOnArrival(c) && altMet(n, a, c, mine, source, tapped, false)
	}
	if name := pickCarrier(reg, b, card, keep); name != "" {
		return name
	}
	if n.typ == "land" {
		if name := landFixture(reg, b, raw); name != "" {
			if c, ok := reg.Lookup(name); ok && len(c.Faces) > 0 && altMet(n, a, c.Faces[0], mine, source, tapped, false) {
				return name
			}
		}
	}
	return pickFixture(reg, b, card, keep, nil, true)
}
