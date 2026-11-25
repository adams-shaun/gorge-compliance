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
	single      bool
	actSelf     bool
}

// stackTargetType reads a Forge TargetType value: a comma list of Spell,
// SpellAbility, Activated and Triggered, each with an optional ".restriction"
// (Spell.singleTarget, Spell.Colorless) or a further comma list
// (Spell.Instant,Spell.Sorcery). A spell alternative is the one a bait can
// serve; the restrictions become a filter on the bait's face, so a bait no
// carrier meets (numTargets) is no bait rather than a wrong one.
// Spell.singleTarget is different: it says the bait spell must itself have a
// target, which the bait search reads as its single flag.
func stackTargetType(tt string) (kind string, alts []filterAlt, single, actSelf bool) {
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
			// the restriction is on the ability's controller: YouCtrl means
			// only an ability the focus seat itself took can be the target
			// (Lithoform Engine), so the bait activation is its own
			if hasProp && (rest == "YouCtrl" || rest == "YouOwn") {
				actSelf = true
			}
		}
	}
	switch {
	case spell:
		return "spell", alts, single, actSelf
	case activated:
		return "activated", alts, single, actSelf
	case triggered:
		return "triggered", alts, single, actSelf
	}
	return "", alts, single, actSelf
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
		stack.kind, stack.tt, stack.single, stack.actSelf = stackTargetType(p["TargetType"])
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
		case "blockedthisturn":
			n.combat = true
		}
		if combatProps[q] != "" {
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
	return func(f *cards.Face) bool { return alts[0].matches(f, false) }
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
			if !permTypes[n.typ] && !n.combat {
				ok = true // a player, any target or another zone: no permanent needed
				continue
			}
			combat = combat || n.combat
			test := reqs[i].restrictionOf(k)
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
			if _, ok := stackBait(reg, b, card, sts, fc.Kind); !ok {
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
			p, ok := stackBait(reg, b, card, sts, fc.Kind)
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
			bad, why := unmet(reg, f, seat, reqs)
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
			// an unmet permanent target: place one on the opponent's side, when
			// the target may be the opponent's
			if !placeOppTarget(reg, b, card, bad, seat, f) {
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
// Spell.singleTarget focus play needs a targeted bait); nonInstant marks a
// cast bait that must be cast on its controller's main phase.
type stackBaitPlan struct {
	cast, self, ability, target string
	nonInstant                  bool
	// abilitySelf is whether the ability bait is activated by the focus seat
	// itself (the carrier sits on its battlefield), for a target only an
	// ability the focus seat controls can be ("Activated.YouCtrl").
	abilitySelf bool
}

// stackBait is the most inert thing the stack targets accept. For spell
// targets: the first instant of the carrier pool that meets every target's
// filter, so the focus seat stays active and no bait land is tapped for a
// sorcery speed; else the first matching permanent or sorcery (nonInstant) --
// except a Spell.singleTarget target, which no inert no-target carrier meets
// and only a targeted cast bait (targetedCastBait) serves. A target whose
// every alternative only the focus seat could control (YouCtrl) has no
// opponent-cast bait: the focus seat casts its own instead (selfCastBait). An
// activated-ability target has no cast bait at all: the bait seat activates
// one of the activation carriers (activationBait). A target whose
// TargetValidTargeting$ needs the bait itself to have a target (an inert bait
// never does) and a target no form can serve have no bait and keep the
// refusal.
// kind is the focus play's kind ("cast" or "ability"): a self-cast bait works
// only for a cast focus, whose play follows the held self cast on the stack
// (CR 117.3a); an ability focus's activation after its own spell cast cannot
// be lowered (the item's step is the activation, so the replay cannot credit
// the cast it waits behind), and keeps the refusal.
func stackBait(reg *cards.Registry, b *certdeck.Builder, card string, sts []stackTgt, kind string) (stackBaitPlan, bool) {
	var plan stackBaitPlan
	for _, st := range sts {
		if st.needsTarget {
			return plan, false // an inert carrier is never a spell with a target
		}
	}
	ability, spell := true, true
	single := false
	for _, st := range sts {
		ability = ability && st.kind == "activated"
		spell = spell && st.kind == "spell"
		single = single || st.single
	}
	if ability {
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
		name, tgt := targetedCastBait(reg, b, card, sts)
		if name == "" {
			return plan, false
		}
		plan.cast, plan.target = name, tgt
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
// that meets every target's filter, with the permanent its target names.
func targetedCastBait(reg *cards.Registry, b *certdeck.Builder, card string, sts []stackTgt) (name, tgt string) {
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
		if t, _ := baitTgtFor(reg, b, card, f.Abilities[0]); t != "" {
			return f.Name, t
		}
	}
	return "", ""
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
		for _, a := range st.tt {
			if !a.known || a.self || a.ctrl == ctrlOpp {
				continue
			}
			if a.test(f) {
				return true
			}
		}
		if len(st.tt) != 0 {
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
	baitSeatOf(f, seat)
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
			s.Battlefield = append(s.Battlefield, plan.target)
			f.Focus.BaitTarget = plan.target
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
		mana, why := costTokens(cost)
		if why != "" {
			return false
		}
		// an ability target the focus seat itself controls ("Activated.YouCtrl")
		// is met by its own activation: the carrier sits on its battlefield
		s := baitSeatOf(f, seat)
		if plan.abilitySelf {
			s = focusSeat(f, seat)
		}
		s.Battlefield = append(s.Battlefield, plan.ability)
		s.Battlefield = append(s.Battlefield, landsFor(mana, basicFor(cost))...)
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
