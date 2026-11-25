package scen

import (
	"strconv"
	"strings"
	"sync"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/certdeck"
	"github.com/adams-shaun/gorge/engine/cards"
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
// state-based actions at once (CR 704.5f): a printed toughness of 0 or less.
// An X creature that enters with counters (Endless One, Walking Ballista,
// Ugin's Conjurant) is 0/0 until its replacement effect puts them, and a
// start places it without its X, so it never survives as a bearer, an
// enabler or a target.
func deadOnArrival(f *cards.Face) bool {
	if !f.IsCreature() {
		return false
	}
	_, tough, ok := strings.Cut(f.PT, "/")
	if !ok || strings.ContainsAny(tough, "*+-") {
		return false // no PT, a characteristic-defining toughness, or a modifier
	}
	n, err := strconv.Atoi(strings.TrimSpace(tough))
	return err == nil && n <= 0
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
// TargetType's own restrictions on the object (tt, e.g. Spell.singleTarget,
// Spell.Colorless), and whether the play may decline it (TargetMin 0, Optional
// or MayChooseTarget).
//
// needsTarget is the target's TargetValidTargeting$ restriction: the spell
// being targeted must itself target a permanent/player (Keep Safe, Outwit,
// Intervene, ...). It is not a filter on the bait's face but on what the bait
// does, and an inert bait (carrierPool: a no-target instant/sorcery, a vanilla
// permanent) never has a target, so no bait can meet it. A required such
// target therefore keeps reasonNeedsStackTarget rather than writing a start
// whose focus play is never offered. ConditionTargetValidTargeting$ and
// TargetRestriction$ are deliberately not read here: they gate the focus
// play's effect, not which spells are legal targets (Meddle is exercised with a
// no-target bait).
type stackTgt struct {
	kind        string
	raw         []string
	tt          []filterAlt
	optional    bool
	needsTarget bool
}

// stackTargetType reads a Forge TargetType value: a comma list of Spell,
// SpellAbility, Activated and Triggered, each with an optional ".restriction"
// (Spell.singleTarget, Spell.Colorless) or a further comma list
// (Spell.Instant,Spell.Sorcery). A spell alternative is the one a bait can
// serve; the restrictions become a filter on the bait's face, so a bait no
// carrier meets (singleTarget, numTargets) is no bait rather than a wrong one.
func stackTargetType(tt string) (kind string, alts []filterAlt) {
	var spell, activated, triggered bool
	for _, tok := range strings.Split(tt, ",") {
		tok = strings.TrimSpace(tok)
		head, rest, hasProp := strings.Cut(tok, ".")
		switch head {
		case "Spell", "SpellAbility":
			spell = true
			filter := "Spell"
			if hasProp {
				filter += "." + rest
			}
			alts = append(alts, parseFilter(filter)...)
		case "Activated":
			activated = true
		case "Triggered":
			triggered = true
		}
	}
	switch {
	case spell:
		return "spell", alts
	case activated:
		return "activated", alts
	case triggered:
		return "triggered", alts
	}
	return "", alts
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
		stack.kind, stack.tt = stackTargetType(p["TargetType"])
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

// altFilter is the k-th alternative as parseFilter reads it: its type head and
// every property parseFilter understands (a property it does not read is not
// tested). ok is false when there is no raw alternative to read.
func (r *tgtReq) altFilter(k int) (filterAlt, bool) {
	if k >= len(r.raw) {
		return filterAlt{}, false
	}
	alts := parseFilter(r.raw[k])
	if len(alts) != 1 {
		return filterAlt{}, false
	}
	return alts[0], true
}

// altMet reports whether permanent f (on the focus seat's side when mine, else
// the opponent's) meets the alternative's filter, and is not the focus play's
// source when the alternative says .Other (Adarkar Valkyrie's "target creature
// other than CARDNAME"). The filter test carries the type head as well as the
// properties, so a subtype (Equipment, Vehicle) is not met by any permanent.
func altMet(n altNeed, a filterAlt, f *cards.Face, mine bool, source string) bool {
	if n.combat {
		return false // nothing attacks or blocks at the first main priority
	}
	if n.side == 1 && !mine || n.side == -1 && mine {
		return false
	}
	if n.other && f.Name == source {
		return false
	}
	if a.test != nil && !a.matches(f, false) {
		return false
	}
	return f.IsPermanent()
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
	taken := map[[2]int]bool{} // {side, index} of a permanent an earlier target took
	for i := range reqs {
		ok, combat := false, false
		var took [][2]int
		for k, alt := range reqs[i].alts {
			n := parseAlt(alt)
			if !n.combat && !permanentTarget(reg, alt) {
				ok = true // a player, any target or another zone: no permanent needed
				continue
			}
			combat = combat || n.combat
			filter, _ := reqs[i].altFilter(k)
			var met [][2]int
			if n.combat && k < len(reqs[i].raw) {
				// a creature in combat: the start's scripted combat (lineMet)
				if side, j, found := lineMet(reg, f, seat, reqs[i].raw[k]); found && !(reqs[i].unique && taken[[2]int{side, j}]) {
					met = append(met, [2]int{side, j})
				}
			}
			for side, bf := range [][]string{mine.Battlefield, theirs.Battlefield} {
				for j, name := range bf {
					if reqs[i].unique && taken[[2]int{side, j}] {
						continue
					}
					if c, found := reg.Lookup(name); found && len(c.Faces) > 0 && altMet(n, filter, c.Faces[0], side == 0, source) {
						met = append(met, [2]int{side, j})
					}
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
	if len(choices) == 0 {
		return "", nil
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
			if _, _, ok := stackBait(reg, b, card, sts); !ok {
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
		bait, nonInstant := "", false
		if len(sts) > 0 {
			required := false
			for _, st := range sts {
				required = required || !st.optional
			}
			name, ni, ok := stackBait(reg, b, card, sts)
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
				bait, nonInstant = name, ni
			}
		}
		for try := 0; ; try++ {
			bad, why := unmet(reg, f, seat, reqs, source)
			if bad == nil {
				if bait != "" {
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
					placeBait(reg, f, seat, bait, nonInstant)
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
			if !placeOppTarget(reg, b, card, source, bad, seat, f) {
				break
			}
		}
	}
	return reason, nil
}

// stackBait is the most inert spell the stack targets accept: the first
// instant of the carrier pool that meets every target's filter, so the focus
// seat stays active and no bait land is tapped for a sorcery speed; else the
// first matching permanent or sorcery (nonInstant). A filter no carrier meets
// ("Card.cmcEQX", "Card.wasCastFromGraveyard"), a target only the focus seat
// could control, a target whose TargetValidTargeting$ needs the bait itself to
// have a target (an inert bait never does), and an ability target (a bait
// would have to be activated, not cast) have no bait and keep the refusal.
func stackBait(reg *cards.Registry, b *certdeck.Builder, card string, sts []stackTgt) (name string, nonInstant, ok bool) {
	for _, st := range sts {
		if st.needsTarget {
			return "", false, false // an inert carrier is never a spell with a target
		}
	}
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
	s := &f.Setup.P1
	if seat == 1 {
		s = &f.Setup.P0
	}
	castCarrier(reg, s, name)
	f.Focus.Bait = name
	if nonInstant {
		f.Active = 1 - seat
	}
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
// choice's targets (Cone of Flame: three).
const maxTargetPlacements = 4

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
func placeOppTarget(reg *cards.Registry, b *certdeck.Builder, card, source string, r *tgtReq, seat int, f *StartFile) bool {
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
			name := pickTargetPermanent(reg, b, card, source, n, filter, r.raw[k], mine)
			if name == "" {
				continue
			}
			sd := s
			if mine {
				sd = own
			}
			sd.Battlefield = append(sd.Battlefield, name)
			return true
		}
	}
	// A filter parseFilter does not fully read (a counter gate, say): certdeck
	// can place a pick that meets it (placeCounters) where a carrier cannot.
	typ := "creature"
	for k, alt := range r.alts {
		n := parseAlt(alt)
		if n.side == 1 || n.combat || !permanentTarget(reg, alt) {
			continue // only the focus seat's own: not placeable on the other side
		}
		if filter, ok := r.altFilter(k); ok && filter.known {
			continue // read above: placed, or no card meets it
		}
		if n.typ != "permanent" && n.typ != "card" {
			typ = n.typ
		}
		plan, err := b.Build(card, []certdeck.Need{{Class: certdeck.OppTargets, Arg: typ}})
		if err != nil {
			return false
		}
		picks := plan.Opponent.Provided[certdeck.Need{Class: certdeck.OppTargets, Arg: typ}.String()]
		before := len(s.Battlefield)
		if err := pickEnabler(reg, b, s, false, picks); err != nil {
			return false
		}
		return len(s.Battlefield) > before
	}
	return false
}

// pickTargetPermanent is the most inert permanent a target alternative accepts
// on the side mine names: a carrier of the inert pool first, then the corpus's
// least busy matching fixture (an Equipment or Vehicle carries a keyword the
// pool excludes), then a land for a land target (the pool holds no lands). The
// focus play's own source is never returned for a .Other target.
func pickTargetPermanent(reg *cards.Registry, b *certdeck.Builder, card, source string, n altNeed, a filterAlt, raw string, mine bool) string {
	keep := func(c *cards.Face) bool {
		return c.IsPermanent() && FieldBlock(c) == "" && !deadOnArrival(c) && altMet(n, a, c, mine, source)
	}
	if name := pickCarrier(reg, b, card, keep); name != "" {
		return name
	}
	if n.typ == "land" {
		if name := landFixture(reg, b, raw); name != "" {
			if c, ok := reg.Lookup(name); ok && len(c.Faces) > 0 && altMet(n, a, c.Faces[0], mine, source) {
				return name
			}
		}
	}
	return pickFixture(reg, b, card, keep, nil, true)
}
