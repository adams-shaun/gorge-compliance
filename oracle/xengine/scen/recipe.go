package scen

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/certdeck"
	"github.com/adams-shaun/gorge/engine/cards"
)

// basicOf maps a colour letter to its basic land; C is Wastes.
var basicOf = map[string]string{"W": "Plains", "U": "Island", "B": "Swamp", "R": "Mountain", "G": "Forest", "C": "Wastes"}

var manaTokenRE = regexp.MustCompile(`^([0-9]+|[XWUBRGCS]|[WUBRGC2]/[WUBRGCP])$`)

// firstColour is the first coloured symbol of a mana cost, in cost order
// (hybrid: its first colour); "" for a colourless cost.
func firstColour(cost string) string {
	for _, tok := range strings.Fields(cost) {
		for _, part := range strings.Split(tok, "/") {
			if _, ok := basicOf[part]; ok && part != "C" {
				return part
			}
		}
	}
	return ""
}

// basicFor is the basic land of a card's first colour (Plains when colourless).
func basicFor(cost string) string {
	if c := firstColour(cost); c != "" {
		return basicOf[c]
	}
	return "Plains"
}

// landsFor is one basic per coloured symbol of the mana tokens (hybrid: its
// first colour), generic N as N basics of generic, X as none.
func landsFor(manaTokens []string, generic string) []string {
	var out []string
	for _, tok := range manaTokens {
		if n, err := strconv.Atoi(tok); err == nil {
			for i := 0; i < n; i++ {
				out = append(out, generic)
			}
			continue
		}
		if tok == "X" || tok == "S" {
			continue
		}
		parts := strings.Split(tok, "/")
		if parts[0] == "2" && len(parts) == 2 { // {2/W}: pay the colour
			parts = parts[1:]
		}
		if b, ok := basicOf[parts[0]]; ok {
			out = append(out, b)
		}
	}
	return out
}

// splitCost splits a Forge cost into space-separated tokens, keeping a
// <...> argument (which may hold spaces) inside its token.
func splitCost(cost string) []string {
	var out []string
	depth, start := 0, -1
	for i, r := range cost {
		switch {
		case r == '<':
			depth++
		case r == '>':
			depth--
		}
		if r == ' ' && depth == 0 {
			if start >= 0 {
				out = append(out, cost[start:i])
				start = -1
			}
			continue
		}
		if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		out = append(out, cost[start:])
	}
	return out
}

var payLifeRE = regexp.MustCompile(`^PayLife<[0-9]+>$`)

// costTokens splits a cost into its mana tokens and its other tokens, refusing
// the first other token no start setup can pay.
func costTokens(cost string) (mana []string, refusal string) {
	for _, tok := range splitCost(cost) {
		switch {
		case manaTokenRE.MatchString(tok):
			mana = append(mana, tok)
		case tok == "T" || tok == "Q" || tok == "Sac<1/CARDNAME>" || payLifeRE.MatchString(tok):
		default:
			if i := strings.IndexByte(tok, '<'); i >= 0 {
				tok = tok[:i]
			}
			return nil, "steps-needed:cost:" + tok
		}
	}
	return mana, ""
}

// asEnters reports an as-enters choice on face: a replacement of the card's own
// move to the battlefield whose replacement effect is a Choose* API.
func asEnters(f *cards.Face) bool {
	for _, r := range f.Repls {
		if r.Event != "Moved" || !strings.Contains(r.Params["ValidCard"], "Card.Self") || r.Params["Destination"] != "Battlefield" {
			continue
		}
		if r.With != nil && strings.HasPrefix(r.With.API, "Choose") {
			return true
		}
	}
	return false
}

// selfCard reports whether a trigger is about the card itself: some
// alternative of its ValidCard is Card with the Self property
// ("Card.Self+kicked", "Card.wasCastByYou+Self").
func selfCard(t *cards.Trigger) bool {
	for _, alt := range strings.Split(t.Params["ValidCard"], ",") {
		props, ok := strings.CutPrefix(strings.TrimSpace(alt), "Card.")
		if ok && slices.Contains(strings.Split(props, "+"), "Self") {
			return true
		}
	}
	return false
}

// castLine meets a cast-history need (scen.castHistory) on the start's own
// line: the focus must be seat 0 casting the card from hand, which makes it
// cast; for "kicked" the lands for the kicker cost are added too and the focus
// takes the kicked cast option. "" when met, else the refusal.
func castLine(face *cards.Face, card, hist, generic string, f *StartFile) string {
	fo := f.Focus
	if fo == nil || fo.Seat == nil || *fo.Seat != 0 || fo.Kind != "cast" || fo.Card != card || !slices.Contains(f.Setup.P0.Hand, card) {
		return "history:" + hist
	}
	if hist != "kicked" {
		return ""
	}
	k, ok := face.KeywordParam("Kicker")
	if !ok {
		return "history:kicked:no-kicker" // multikicker, an and/or kicker read another way
	}
	if strings.Contains(k, ":") {
		return "history:kicked:multi-cost" // "kicked1"/"kicked2": the focus names one mode
	}
	mana, why := costTokens(k)
	if why != "" {
		return why
	}
	f.Setup.P0.Battlefield = append(f.Setup.P0.Battlefield, landsFor(mana, generic)...)
	fo.Mode = "kicked"
	return ""
}

func newStart(card, key string) *StartFile {
	return &StartFile{Format: StartFormat, Card: card, Key: key, Players: 2, MaxTurns: 1, Filler: "basics", Seeds: 2}
}

func seat(n int) *int { return &n }

// recipe is the start-file path: the placements by key family, then the needs.
func recipe(reg *cards.Registry, b *certdeck.Builder, c *cards.Card, card, key, fam string, m []string,
	sa *cards.SA, trig *cards.Trigger, hist string, opt Options) (Result, error) {
	face := c.Faces[0]
	f := newStart(card, key)
	p0, p1 := &f.Setup.P0, &f.Setup.P1
	generic := basicFor(face.ManaCost)
	onBF := func() (Result, bool) { // the checks of every recipe that places the card on the battlefield
		if asEnters(face) {
			r, _ := refuse("as-enters")
			return r, true
		}
		return Result{}, false
	}
	switch fam {
	case "cast":
		p0.Hand = []string{card}
		p0.Battlefield = landsFor(splitCost(face.ManaCost), generic)
		f.Focus = &StartFocus{Seat: seat(0), Card: card, Kind: "cast"}
	case "land":
		p0.Hand = []string{card}
		f.Focus = &StartFocus{Seat: seat(0), Card: card, Kind: "land"}
	case "static", "combat":
		if r, ok := onBF(); ok {
			return r, nil
		}
		return refuse("no-generator:" + fam)
	case "activate", "trigger":
		if m[1] != "0" {
			return refuse("no-generator:face")
		}
		if r, ok := onBF(); ok {
			return r, nil
		}
		idx, _ := strconv.Atoi(m[3])
		if fam == "activate" {
			mana, why := costTokens(sa.Params["Cost"])
			if why != "" {
				return refuse(why)
			}
			p0.Battlefield = append([]string{card}, landsFor(mana, generic)...)
			f.Focus = &StartFocus{Seat: seat(0), Card: card, Kind: "ability", Ability: idx}
			break
		}
		if band := trig.Params["ClassBand"]; band != "" && band != "1" {
			// a Class level's trigger: the start places the Class at level 1
			return refuse("steps-needed:class-level")
		}
		switch trig.Mode {
		case "ChangesZone":
			switch {
			case trig.Params["Destination"] == "Battlefield" && selfCard(trig):
				// An ETB is a cast: the card is not on the battlefield yet.
				p0.Hand = []string{card}
				p0.Battlefield = landsFor(splitCost(face.ManaCost), generic)
				f.Focus = &StartFocus{Seat: seat(0), Card: card, Kind: "cast"}
			case trig.Params["Origin"] == "Battlefield" && (trig.Params["Destination"] == "Graveyard" || trig.Params["Destination"] == "Any") && selfCard(trig):
				// Murder takes a creature to the graveyard, which also meets "leaves
				// the battlefield" (Destination Any).
				if !face.IsCreature() {
					return refuse("no-generator:trigger:ChangesZone:noncreature-self")
				}
				murder, ok := reg.Lookup("Murder")
				if !ok || len(murder.Faces) == 0 {
					return Result{}, fmt.Errorf("scen: Murder is not in the corpus")
				}
				mc := murder.Faces[0].ManaCost
				p0.Battlefield = []string{card}
				p1.Hand = []string{"Murder"}
				p1.Battlefield = landsFor(splitCost(mc), basicFor(mc))
				f.Active = 1
				f.Focus = &StartFocus{Seat: seat(1), Card: "Murder", Kind: "cast"}
			default:
				if why := zoneStart(reg, b, face, card, trig, f); why != "" {
					return refuse(why)
				}
			}
		case "Attacks":
			p0.Battlefield = []string{card}
			f.Focus = &StartFocus{Seat: seat(0), Card: card, Kind: "attack"}
		case "Phase":
			if why := phaseStart(reg, b, face, card, generic, trig, f); why != "" {
				return refuse(why)
			}
		default:
			return refuse("no-generator:trigger:" + trig.Mode)
		}
	default:
		return refuse("no-generator")
	}
	if hist != "" {
		if why := castLine(face, card, hist, generic, f); why != "" {
			return refuse(why)
		}
	}
	for _, bf := range [][]string{p0.Battlefield, p1.Battlefield} {
		for _, n := range bf {
			if n != card {
				continue
			}
			if why := FieldBlock(face); why != "" {
				return refuse("battlefield:" + why)
			}
		}
	}
	if why, err := placeNeeds(reg, b, c, card, generic, f); err != nil || why != "" {
		if err != nil {
			return Result{}, err
		}
		return refuse(why)
	}
	return Result{Path: PathStart, Start: f}, nil
}

// FieldBlock is why a start file cannot list face on the battlefield, or "":
// an instant or sorcery is never a permanent (CR 304.4, 307.4), and an Aura
// a start file places has no bearer (a start file has no attach), so it stays
// where it is (CR 303.4g). Either way the engines disagree about where the
// card ends up (gorge left Path to Exile on the battlefield and lost an
// unattached Aura from the library; Forge put both in hand), so the setup
// could never agree.
func FieldBlock(face *cards.Face) string {
	if !face.IsPermanent() {
		return "not-a-permanent"
	}
	for _, t := range face.Types {
		if strings.EqualFold(t, "Aura") {
			return "aura-unattached"
		}
	}
	return ""
}

// The forced start (operator rule): a start is built so the only plays it
// offers are the ones that exercise the key. A need is met by placed state
// where it can be (a permanent on the battlefield, a life total, tapped
// lands), so no play is needed to meet it; an enabler that must be cast
// (an instant, a sorcery, an Aura) comes with exactly the lands its cost
// takes, and an Aura with one inert bearer. Placed enablers prefer cards no
// one can act on, so the bots and the three engines face one small decision
// tree.

// burnedLife is the life an opp-burn need starts the target seat at: below
// the opponent's 20, as the burn certdeck would have cast leaves it.
const burnedLife int32 = 17

// bearers are the inert creatures an Aura enabler's caster gets to enchant,
// first the corpus holds and certdeck accepts: no ability, defender.
var bearers = []string{"Wall of Stone", "Wall of Wood"}

// reasonPlayerCounters refuses a counters need whose removal reads a player's
// counters (poison, rad, experience, energy): rules.StartSeat seeds battlefield
// cards only, so no start position can exercise it and a placed permanent would
// silently stand in for the player (Final Act, Survivor's Med Kit). A target
// that also offers a permanent (Permanent,Opponent) keeps its permanent branch.
const reasonPlayerCounters = "steps-needed:counters:player"

// placeNeeds adds what certdeck.Derive says the card reads (see the forced
// start above): graveyard fodder; for a lands need, certdeck's two extra
// lands, tapped, so they add no mana; for opp-burn, the target seat's life
// lowered; for a counters need, the counters themselves (placeCounters); for
// every other need, one of certdeck's picks (pickEnabler). A need StartSpec
// cannot place is a refusal: tokens (rules.StartSeat has no tokens yet) and
// counters a removal reads from a player, not a permanent (reasonPlayerCounters).
func placeNeeds(reg *cards.Registry, b *certdeck.Builder, c *cards.Card, card, generic string, f *StartFile) (string, error) {
	needs := certdeck.Derive(c, b.Types())
	if len(needs) == 0 {
		return "", nil
	}
	for _, n := range needs {
		if n.Class == certdeck.Token {
			return "steps-needed:" + n.Class, nil
		}
	}
	plan, err := b.Build(card, needs)
	if err != nil {
		return "", err
	}
	var counterNeeds []certdeck.Need
	targetBearers := map[string]bool{}
	for _, n := range needs {
		switch {
		case n.Class == certdeck.Graveyard:
			for i := 0; i < 7; i++ {
				f.Setup.P0.Graveyard = append(f.Setup.P0.Graveyard, generic)
			}
		case n.Class == certdeck.Lands:
			for i := 0; i < 2; i++ {
				f.Setup.P0.Battlefield = append(f.Setup.P0.Battlefield, generic)
				f.Setup.P0.Tapped = append(f.Setup.P0.Tapped, generic)
			}
		case n.Class == certdeck.Counters:
			if certdeck.CounterPlayer(c) {
				return reasonPlayerCounters, nil
			}
			counterNeeds = append(counterNeeds, n)
		case n.Class == certdeck.OppBurn:
			if f.Setup.P0.Life == nil {
				l := burnedLife
				f.Setup.P0.Life = &l
			}
		case certdeck.OppSide(n.Class):
			before := len(f.Setup.P1.Battlefield)
			if err := pickEnabler(reg, b, &f.Setup.P1, f.Active != 1, plan.Opponent.Provided[n.String()]); err != nil {
				return "", err
			}
			if n.Class == certdeck.OppTargets {
				// The card certdeck picked for this target need is the
				// permanent a counters need should read.
				for _, name := range f.Setup.P1.Battlefield[before:] {
					if slices.Contains(plan.Opponent.Provided[n.String()], name) {
						targetBearers[name] = true
					}
				}
			}
		default:
			if err := pickEnabler(reg, b, &f.Setup.P0, f.Active != 0, plan.Self.Provided[n.String()]); err != nil {
				return "", err
			}
		}
	}
	// Counters last: the permanent a removal reads may be an enabler another
	// need places, such as the creature an opposing Aura enchants.
	for _, n := range counterNeeds {
		if err := placeCounters(reg, b, c, card, n.Arg, targetBearers, f); err != nil {
			return "", err
		}
	}
	return "", nil
}

// placedCounters is how many counters of the needed kind a counters need
// places: one meets "with a counter", "for each counter" and a remove cost.
const placedCounters int32 = 1

// placeCounters meets a counters need of kind with placed counters (the
// start's counters field, which gorge's applyStart emits and the lowering
// restates as setup counters for the followers), not a card that would have
// to put them. On seat 0 it is the focus card where the battlefield lists it,
// else an inert bearer (bearers). On seat 1 it is the permanent the removal
// reads (CounterSeat): a permanent already placed for a target need, else an
// inert bearer for the opponent.
func placeCounters(reg *cards.Registry, b *certdeck.Builder, c *cards.Card, card, kind string, targetBearers map[string]bool, f *StartFile) error {
	if certdeck.CounterSeat(c) == 1 {
		s := &f.Setup.P1
		on := counterBearer(reg, s, targetBearers)
		if on == "" {
			var err error
			on, err = inertCounterBearer(reg, b)
			if err != nil {
				return err
			}
			s.Battlefield = append(s.Battlefield, on)
		}
		s.Counters = append(s.Counters, StartCounter{Card: on, Kind: kind, N: placedCounters})
		return nil
	}
	s := &f.Setup.P0
	on := card
	if !slices.Contains(s.Battlefield, on) {
		var err error
		on, err = inertCounterBearer(reg, b)
		if err != nil {
			return err
		}
		s.Battlefield = append(s.Battlefield, on)
	}
	s.Counters = append(s.Counters, StartCounter{Card: on, Kind: kind, N: placedCounters})
	return nil
}

// counterBearer is the permanent s already lists that a removal should read:
// first a card placed for one of the card's target needs (a creature, a
// planeswalker, an artifact, ...), then a creature or planeswalker, and none
// when s has no such permanent.
func counterBearer(reg *cards.Registry, s *StartSeat, targetBearers map[string]bool) string {
	used := map[string]bool{}
	for _, c := range s.Counters {
		used[c.Card] = true
	}
	for _, name := range s.Battlefield {
		if !used[name] && targetBearers[name] {
			return name
		}
	}
	for _, name := range s.Battlefield {
		if used[name] {
			continue
		}
		c, ok := reg.Lookup(name)
		if !ok || len(c.Faces) == 0 {
			continue
		}
		if face := c.Faces[0]; face.IsCreature() || face.IsPlaneswalker() {
			return name
		}
	}
	return ""
}

// inertCounterBearer is the first inert creature (bearers) the corpus holds
// that certdeck plays.
func inertCounterBearer(reg *cards.Registry, b *certdeck.Builder) (string, error) {
	for _, name := range bearers {
		if c, ok := reg.Lookup(name); ok && b.Playable(c) {
			return name, nil
		}
	}
	return "", fmt.Errorf("scen: no inert counter bearer (%v) in the corpus", bearers)
}

// inertRank orders placeable enablers, lowest first: a noncreature with no
// activated ability (nothing to activate, nothing to attack or block with),
// then a creature with none, then the rest.
func inertRank(f *cards.Face) int {
	switch {
	case len(f.Abilities) > 0:
		return 2
	case f.IsCreature():
		return 1
	}
	return 0
}

// pickEnabler meets one need on seat s from certdeck's picks (in its
// ranking): the most inert pick FieldBlock admits goes on the battlefield;
// failing one, a pick goes in hand with the lands for its mana cost on the
// battlefield (the first pick; for a seat whose turn the start is not, offTurn,
// the first instant-speed pick, since that seat cannot cast a sorcery-speed
// one before the start ends), and an Aura also gets an inert bearer (bearers)
// under s.
func pickEnabler(reg *cards.Registry, b *certdeck.Builder, s *StartSeat, offTurn bool, picks []string) error {
	if len(picks) == 0 {
		return nil
	}
	faces := make([]*cards.Face, len(picks))
	for i, name := range picks {
		c, ok := reg.Lookup(name)
		if !ok || len(c.Faces) == 0 {
			return fmt.Errorf("scen: enabler %q is not in the corpus", name)
		}
		faces[i] = c.Faces[0]
	}
	best := -1
	for i, f := range faces {
		if FieldBlock(f) == "" && (best < 0 || inertRank(f) < inertRank(faces[best])) {
			best = i
		}
	}
	if best >= 0 {
		s.Battlefield = append(s.Battlefield, picks[best])
		return nil
	}
	pick := 0
	if offTurn {
		for i, f := range faces {
			if f.IsInstant() || f.HasKeyword("Flash") {
				pick = i
				break
			}
		}
	}
	f := faces[pick]
	s.Hand = append(s.Hand, picks[pick])
	s.Battlefield = append(s.Battlefield, landsFor(splitCost(f.ManaCost), basicFor(f.ManaCost))...)
	if FieldBlock(f) == "aura-unattached" {
		for _, name := range bearers {
			if c, ok := reg.Lookup(name); ok && b.Playable(c) {
				s.Battlefield = append(s.Battlefield, name)
				return nil
			}
		}
		return fmt.Errorf("scen: no inert Aura bearer (%v) in the corpus", bearers)
	}
	return nil
}
