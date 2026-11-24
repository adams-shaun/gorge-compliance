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
// ValidTgts split at commas), lower-cased; any one meets it.
type tgtReq struct{ alts []string }

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
				var alts []string
				for _, a := range strings.Split(strings.ToLower(v), ",") {
					if !strings.Contains(a, "inzone") && !strings.Contains(a, "stack") {
						alts = append(alts, strings.TrimSpace(a))
					} else {
						alts = append(alts, "any") // another zone: taken as met
					}
				}
				reqs = append(reqs, tgtReq{alts: alts})
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
			return tgtReq{alts: strings.Split(strings.ToLower(filter), ",")}, true
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

// altMet reports whether permanent f (on the focus seat's side when mine, else
// the opponent's) meets the alternative.
func altMet(n altNeed, f *cards.Face, mine bool) bool {
	if n.combat {
		return false // nothing attacks or blocks at the first main priority
	}
	if n.side == 1 && !mine || n.side == -1 && mine {
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
// refusal its absence is when no permanent could be placed to meet it.
func unmet(reg *cards.Registry, f *StartFile, seat int, reqs []tgtReq) (*tgtReq, string) {
	mine, theirs := &f.Setup.P0, &f.Setup.P1
	if seat == 1 {
		mine, theirs = theirs, mine
	}
	for i := range reqs {
		ok, combat := false, false
		for _, alt := range reqs[i].alts {
			n := parseAlt(alt)
			if !permTypes[n.typ] {
				ok = true // a player, any target or another zone: no permanent needed
				continue
			}
			combat = combat || n.combat
			for side, bf := range [][]string{mine.Battlefield, theirs.Battlefield} {
				for _, name := range bf {
					if c, found := reg.Lookup(name); found && len(c.Faces) > 0 && altMet(n, c.Faces[0], side == 0) {
						ok = true
					}
				}
			}
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
			if why == reasonNeedsCombat || try >= 1 {
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

// placeOppTarget places certdeck's most inert opposing-target pick for the
// unmet target on the seat the focus does not own, and says whether one fit.
func placeOppTarget(reg *cards.Registry, b *certdeck.Builder, card string, r *tgtReq, seat int, f *StartFile) bool {
	typ := "creature"
	for _, alt := range r.alts {
		n := parseAlt(alt)
		if n.side == 1 {
			continue // only the focus seat's own: not placeable on the other side
		}
		if permTypes[n.typ] && n.typ != "permanent" {
			typ = n.typ
		}
		plan, err := b.Build(card, []certdeck.Need{{Class: certdeck.OppTargets, Arg: typ}})
		if err != nil {
			return false
		}
		picks := plan.Opponent.Provided[certdeck.Need{Class: certdeck.OppTargets, Arg: typ}.String()]
		s := &f.Setup.P1
		if seat == 1 {
			s = &f.Setup.P0
		}
		before := len(s.Battlefield)
		if err := pickEnabler(reg, b, s, false, picks); err != nil {
			return false
		}
		return len(s.Battlefield) > before
	}
	return false
}
