package scen

import (
	"strconv"
	"strings"

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

// permTypes are the target types a placed permanent can meet; every other
// type word (Player, Any, Card, ...) is taken as met.
var permTypes = map[string]bool{
	"creature": true, "permanent": true, "artifact": true, "enchantment": true,
	"land": true, "planeswalker": true, "battle": true,
}

// chainReqs is the targets of the ability chain starting at line params (a
// Params map), following SubAbility$ by name in svars up to a depth. needsStack
// is true when the chain targets a spell or ability on the stack.
func chainReqs(p map[string]string, svars map[string]string, sub *cards.SA, depth int) (reqs []tgtReq, needsStack bool) {
	for ; depth < 8 && p != nil; depth++ {
		if p["TargetType"] == "Spell" || p["TargetType"] == "Activated" || strings.EqualFold(p["TargetType"], "Triggered") {
			needsStack = true
		}
		if v := p["ValidTgts"]; v != "" && p["TargetMin"] != "0" && p["Optional"] == "" {
			zone := strings.ToLower(p["TgtZone"])
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
	return reqs, needsStack
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
// needsStack is whether every choice targets the stack.
func playChoices(face *cards.Face, sa *cards.SA) (choices [][]tgtReq, needsStack bool) {
	if sa.API == "Charm" {
		allStack := true
		for _, name := range strings.Split(sa.Params["Choices"], ",") {
			reqs, st := chainReqs(parseSVar(face.SVars[strings.TrimSpace(name)]), face.SVars, nil, 0)
			choices = append(choices, reqs)
			allStack = allStack && st
		}
		return choices, allStack && len(choices) > 0
	}
	reqs, st := chainReqs(sa.Params, face.SVars, sa.Sub, 0)
	return [][]tgtReq{reqs}, st
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

// altNeed is what one alternative asks of the board: a permanent type, the
// side (1 you, -1 the opponent, 0 either) and whether it needs combat.
type altNeed struct {
	typ    string
	side   int
	combat bool
}

func parseAlt(alt string) altNeed {
	head, rest, _ := strings.Cut(alt, ".")
	n := altNeed{typ: strings.TrimSpace(head)}
	for _, q := range strings.FieldsFunc(rest, func(r rune) bool { return r == '.' || r == '+' }) {
		switch q {
		case "youctrl":
			n.side = 1
		case "youdontctrl", "oppctrl":
			n.side = -1
		case "attacking", "blocking", "attackingyou", "blockedthisturn", "attackedthisturn":
			n.combat = true
		}
	}
	return n
}

// restriction is the test of a target alternative's restrictions beyond its
// type and side (nonLand, a colour, withFlying, cmcLE3), from parseFilter;
// nil when it has none, or holds a property parseFilter does not read, which
// is then taken as met, as before restrictions were read at all.
func restriction(raw string) func(*cards.Face) bool {
	_, props, _ := strings.Cut(raw, ".")
	plain := true
	for _, p := range strings.FieldsFunc(props, func(r rune) bool { return r == '+' }) {
		switch p {
		case "YouCtrl", "OppCtrl", "YouDontCtrl", "Other":
		default:
			plain = false
		}
	}
	if plain {
		return nil // a type and a side, which parseAlt already reads
	}
	alts := parseFilter(raw)
	if len(alts) != 1 || !alts[0].known || alts[0].self {
		return nil
	}
	return alts[0].test
}

// altMet reports whether permanent f (on the focus seat's side when mine, else
// the opponent's) meets the alternative, and its restrictions (restriction)
// when test is non-nil.
func altMet(n altNeed, test func(*cards.Face) bool, f *cards.Face, mine bool) bool {
	if n.combat {
		return false // nothing attacks or blocks at the first main priority
	}
	if n.side == 1 && !mine || n.side == -1 && mine {
		return false
	}
	if test != nil && !test(f) {
		return false
	}
	switch n.typ {
	case "creature":
		return f.IsCreature()
	case "artifact":
		return f.HasTypeWord("Artifact")
	case "enchantment":
		return f.HasTypeWord("Enchantment")
	case "land":
		return f.IsLand()
	case "planeswalker":
		return f.IsPlaneswalker()
	}
	return f.IsPermanent()
}

// unmet is the first target of reqs the placed board cannot meet, with the
// refusal its absence is when no permanent could be placed to meet it. A
// target of n permanents (Switcheroo's "two target creatures") needs n
// distinct ones, and a TargetUnique one permanents no earlier target took.
func unmet(reg *cards.Registry, f *StartFile, seat int, reqs []tgtReq) (*tgtReq, string) {
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
			if !permTypes[n.typ] {
				ok = true // a player, any target or another zone: no permanent needed
				continue
			}
			combat = combat || n.combat
			test := reqs[i].restrictionOf(k)
			var met [][2]int
			for side, bf := range [][]string{mine.Battlefield, theirs.Battlefield} {
				for j, name := range bf {
					if reqs[i].unique && taken[[2]int{side, j}] {
						continue
					}
					if c, found := reg.Lookup(name); found && len(c.Faces) > 0 && altMet(n, test, c.Faces[0], side == 0) {
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
func focusPlayRefusal(reg *cards.Registry, b *certdeck.Builder, sa *cards.SA, card string, f *StartFile) (string, error) {
	fc := f.Focus
	if fc == nil || fc.Seat == nil || (fc.Kind != "cast" && fc.Kind != "ability") {
		return "", nil
	}
	c, ok := reg.Lookup(fc.Card)
	if !ok || len(c.Faces) == 0 {
		return "", nil
	}
	face := c.Faces[0]
	var choices [][]tgtReq
	switch {
	case fc.Kind == "ability" && fc.Card == card && sa != nil:
		var st bool
		if choices, st = playChoices(face, sa); st {
			return reasonNeedsStackTarget, nil
		}
	case fc.Kind == "cast":
		stackOnly := false
		for _, a := range face.Abilities {
			if a.Kind != "SP" {
				continue
			}
			ch, st := playChoices(face, a)
			choices, stackOnly = append(choices, ch...), stackOnly || st
		}
		if stackOnly {
			return reasonNeedsStackTarget, nil
		}
		if r, ok := enchantReq(face); ok {
			if len(choices) == 0 {
				choices = [][]tgtReq{nil}
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
	reason := ""
	for _, reqs := range choices {
		for try := 0; ; try++ {
			bad, why := unmet(reg, f, seat, reqs)
			if bad == nil {
				return "", nil // a choice every target of which is met
			}
			reason = why
			if why == reasonNeedsCombat || try >= maxTargetPlacements {
				break
			}
			// an unmet permanent target: place one on the opponent's side, when
			// the target may be the opponent's
			if !placeOppTarget(reg, b, card, bad, seat, f) {
				break
			}
		}
	}
	return reason, nil
}

// maxTargetPlacements bounds the permanents focusPlayRefusal places for one
// choice's targets (Cone of Flame: three).
const maxTargetPlacements = 4

// restrictionOf is restriction of the k-th alternative, nil when r has no
// raw form of it.
func (r *tgtReq) restrictionOf(k int) func(*cards.Face) bool {
	if k >= len(r.raw) {
		return nil
	}
	return restriction(r.raw[k])
}

// placeOppTarget places a permanent for the unmet target, and says whether
// one fit: for an alternative whose restrictions parseFilter reads, the most
// inert carrier (carrierPool) that meets them, on the seat the focus does not
// own; for one only the focus seat's own permanent meets ("target creature
// you control": a fight spell's first target), the most inert carrier of its
// type on the focus seat's side; otherwise certdeck's most inert
// opposing-target pick.
func placeOppTarget(reg *cards.Registry, b *certdeck.Builder, card string, r *tgtReq, seat int, f *StartFile) bool {
	s, own := &f.Setup.P1, &f.Setup.P0
	if seat == 1 {
		s, own = own, s
	}
	for k, alt := range r.alts {
		n := parseAlt(alt)
		test := r.restrictionOf(k)
		if n.side == 1 || n.combat || !permTypes[n.typ] || test == nil {
			continue
		}
		name := pickCarrier(reg, b, card, func(c *cards.Face) bool {
			return c.IsPermanent() && FieldBlock(c) == "" && !deadOnArrival(c) && altMet(n, test, c, false)
		})
		if name != "" {
			s.Battlefield = append(s.Battlefield, name)
			return true
		}
	}
	for k, alt := range r.alts {
		n := parseAlt(alt)
		if n.side != 1 || n.combat || !permTypes[n.typ] {
			continue
		}
		test := r.restrictionOf(k)
		name := pickCarrier(reg, b, card, func(c *cards.Face) bool {
			return c.IsPermanent() && FieldBlock(c) == "" && !deadOnArrival(c) && altMet(n, test, c, true)
		})
		if name != "" {
			own.Battlefield = append(own.Battlefield, name)
			return true
		}
	}
	typ := "creature"
	for k, alt := range r.alts {
		n := parseAlt(alt)
		if n.side == 1 {
			continue // only the focus seat's own: not placeable on the other side
		}
		if r.restrictionOf(k) != nil {
			continue // no carrier meets its restrictions: no pick placed blind
		}
		if permTypes[n.typ] && n.typ != "permanent" {
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
