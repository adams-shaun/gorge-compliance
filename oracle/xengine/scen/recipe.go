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

// manaTokenRE is one mana symbol of a Forge cost. A card script writes a
// hybrid, two-generic hybrid or Phyrexian symbol without a slash (ManaCost:2
// WB WB, 2W, UP, RWP); a few write it with one (2/R).
var manaTokenRE = regexp.MustCompile(`^([0-9]+|[XWUBRGCS]|[WUBRGC2]/?[WUBRGCP]|[WUBRG]/?[WUBRG]/?P)$`)

// shardParts is a mana symbol's parts: "WB", "W/B" -> W B; "2W" -> 2 W;
// "RWP" -> R W P; a number or a one-letter symbol is itself.
func shardParts(tok string) []string {
	if strings.Contains(tok, "/") {
		return strings.Split(tok, "/")
	}
	if _, err := strconv.Atoi(tok); err == nil || len(tok) < 2 || !manaTokenRE.MatchString(tok) {
		return []string{tok}
	}
	return strings.Split(tok, "")
}

// firstColour is the first coloured symbol of a mana cost, in cost order
// (hybrid: its first colour); "" for a colourless cost.
func firstColour(cost string) string {
	for _, tok := range strings.Fields(cost) {
		for _, part := range shardParts(tok) {
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
		if tok == "X" {
			continue
		}
		if tok == "S" {
			// {S}: one mana from a snow source -- the snow-covered basic of
			// the card's colour (a plain basic could never pay it)
			out = append(out, "Snow-Covered "+generic)
			continue
		}
		parts := shardParts(tok)
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

// abilityFocus is the start of an activated ability: the card where its
// activation zone has it, the cost met by placed state (planCost), and the
// focus the ability's activation. "" when built, else the refusal.
func abilityFocus(reg *cards.Registry, b *certdeck.Builder, face *cards.Face, card, generic string, idx int, sa *cards.SA, f *StartFile) string {
	plan, why := planCost(reg, b, face, card, sa.Params["Cost"], generic)
	if why != "" {
		return why
	}
	if plan.zone == zoneBattlefield {
		switch z := strings.ToLower(sa.Params["ActivationZone"]); z {
		case "", "battlefield":
		case zoneHand, zoneGraveyard:
			plan.zone = z // unearth, channel, ...: the card sits where the ability is activated from
		default:
			return "start:activation-zone:" + z
		}
	}
	applyCost(plan, card, generic, f)
	f.Focus = &StartFocus{Seat: seat(0), Card: card, Kind: "ability", Ability: idx}
	return ""
}

func newStart(card, key string) *StartFile {
	return &StartFile{Format: StartFormat, Card: card, Key: key, Players: 2, MaxTurns: 1, Filler: "basics", Seeds: 2}
}

func seat(n int) *int { return &n }

// keyFace is the face index a key plays: the f<N> of an activate or trigger key
// (m is its match), the <N> of static#N.S or combat#N.attack; 0 for cast and land.
func keyFace(key string, m []string) int {
	if m == nil {
		if sm := staticKeyRE.FindStringSubmatch(key); sm != nil {
			m = sm
		} else if cm := combatKeyRE.FindStringSubmatch(key); cm != nil {
			m = cm
		}
	}
	if m == nil {
		return 0
	}
	fi, _ := strconv.Atoi(m[1])
	return fi
}

// backFaceRefusal is why a key of face fi cannot start from a card on the
// battlefield, or "". gorge's StartSeat.BackFace enters a card on its second
// face, so only a two-faced transforming or modal card's back face is
// placeable, and only when that face is a permanent. Flip, meld, specialize,
// split and adventure faces have no such placement.
func backFaceRefusal(c *cards.Card, fi int) string {
	switch {
	case fi == 0:
		return ""
	case fi >= len(c.Faces):
		return "no-generator:face" // not reached for an f key (Generate errors on an unknown face first); a static/combat key of an unknown face
	case fi != 1 || len(c.Faces) != 2 || c.AlternateMode != "DoubleFaced" && c.AlternateMode != "Modal":
		mode := strings.ToLower(c.AlternateMode)
		if mode == "" {
			mode = "other"
		}
		return "no-generator:face:" + mode
	}
	if why := FieldBlock(c.Faces[1]); why != "" {
		return "no-generator:face:" + why
	}
	return ""
}

// recipe is the start-file path: the placements by key family, then the needs.
func recipe(reg *cards.Registry, b *certdeck.Builder, c *cards.Card, card, key, fam string, m []string,
	sa *cards.SA, trig *cards.Trigger, hist string, opt Options) (Result, error) {
	fi := keyFace(key, m)
	if why := backFaceRefusal(c, fi); why != "" {
		return refuse(why)
	}
	face := c.Faces[fi]
	f := newStart(card, key)
	p0, p1 := &f.Setup.P0, &f.Setup.P1
	generic := basicFor(face.ManaCost)
	if fi > 0 && (face.ManaCost == "" || face.ManaCost == "no cost") {
		generic = basicFor(c.Faces[0].ManaCost) // a transformed face has no cost: its colours are the front's
	}
	onBF := func() (Result, bool) { // the checks of every recipe that places the card on the battlefield
		if asEnters(face) {
			r, _ := refuse("as-enters")
			return r, true
		}
		return Result{}, false
	}
	switch fam {
	case "cast":
		if why := castStart(reg, b, face, card, generic, f); why != "" {
			return refuse(why)
		}
	case "land":
		p0.Hand = []string{card}
		f.Focus = &StartFocus{Seat: seat(0), Card: card, Kind: "land"}
	case "static", "combat":
		if r, ok := onBF(); ok {
			return r, nil
		}
		var why string
		if fam == "combat" {
			why = combatKeyStart(reg, c, card, key, f)
		} else {
			why = staticKeyStart(reg, b, c, card, key, f)
		}
		if why != "" {
			return refuse(why)
		}
	case "activate", "trigger":
		if r, ok := onBF(); ok {
			return r, nil
		}
		idx, _ := strconv.Atoi(m[3])
		if fam == "activate" {
			if why := abilityFocus(reg, b, face, card, generic, idx, sa, f); why != "" {
				return refuse(why)
			}
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
				if fi > 0 {
					// the card enters as its front face, or is cast as the back one
					return refuse("no-generator:face:back-etb")
				}
				// An ETB is a cast: the card is not on the battlefield yet.
				if why := castStart(reg, b, face, card, generic, f); why != "" {
					return refuse(why)
				}
			case trig.Params["Origin"] == "Battlefield" && (trig.Params["Destination"] == "Graveyard" || trig.Params["Destination"] == "Any") && selfCard(trig):
				// Murder (a creature) or a destroy spell (any other permanent,
				// an Aura first attached) moves the card to the graveyard, which
				// also meets "leaves the battlefield" (Destination Any).
				if why := selfDiesStart(reg, b, face, card, f); why != "" {
					return refuse(why)
				}
			default:
				if why := zoneStart(reg, b, face, card, trig, f); why != "" {
					return refuse(why)
				}
			}
		case "ChangesZoneAll":
			if why := zoneAllStart(reg, b, face, card, trig, f); why != "" {
				return refuse(why)
			}
		case "SpellCast", "SpellCastOrCopy":
			if why := spellStart(reg, b, face, card, generic, trig, f); why != "" {
				return refuse(why)
			}
		case "DamageDone", "AttackersDeclared":
			if why := combatStart(reg, b, face, card, trig, f); why != "" {
				if trig.Mode == "AttackersDeclared" {
					return refuse(why)
				}
				// a combat-damage shape combatStart's one attack does not read
				if why = combatTrigStart(reg, b, face, card, trig, f); why != "" {
					return refuse(why)
				}
			}
		case "Blocks", "AttackerBlocked", "AttackerBlockedByCreature", "DamageDoneOnce", "DamageDealtOnce":
			if why := combatTrigStart(reg, b, face, card, trig, f); why != "" {
				return refuse(why)
			}
		case "BecomesTarget":
			if why := targetStart(reg, b, face, card, trig, f); why != "" {
				return refuse(why)
			}
		case "LifeGained":
			if why := lifeStart(reg, b, card, trig, f); why != "" {
				return refuse(why)
			}
		case "Attacks":
			if why := attacksStart(reg, b, face, card, trig, f); why != "" {
				return refuse(why)
			}
		case "Phase":
			if why := phaseStart(reg, b, face, card, generic, trig, f); why != "" {
				return refuse(why)
			}
		case "Drawn", "Discarded", "Sacrificed", "Taps", "TapsForMana", "Untaps", "CommitCrime", "TurnFaceUp":
			if why := eventStart(reg, b, face, card, generic, trig, f); why != "" {
				return refuse(why)
			}
		default:
			return refuse("no-generator:trigger:" + trig.Mode)
		}
	default:
		return refuse("no-generator")
	}
	if sa == nil && f.Focus != nil && f.Focus.Kind == "ability" && f.Focus.Card == card && f.Focus.Ability < len(face.Abilities) {
		// a trigger start whose event is the card's own ability (it taps, it is
		// sacrificed): the ability's targets are the focus play's
		sa = face.Abilities[f.Focus.Ability]
	}
	if hist != "" {
		if why := castLine(face, card, hist, generic, f); why != "" {
			return refuse(why)
		}
	}
	if fi > 0 {
		// a back-face key is played on the back face of the battlefield card:
		// the first listed copy enters transformed (rules.StartSeat.BackFace)
		placed := false
		for _, sd := range []*StartSeat{p0, p1} {
			if !placed && slices.Contains(sd.Battlefield, card) {
				sd.BackFace = append(sd.BackFace, card)
				placed = true
			}
		}
		if !placed {
			return refuse("no-generator:face:not-on-battlefield")
		}
	}
	for _, seat := range []*StartSeat{p0, p1} {
		for _, n := range seat.Battlefield {
			if n != card {
				continue
			}
			why := FieldBlock(face)
			if why == "" || why == "aura-unattached" && seat.Attached(n) {
				continue
			}
			return refuse("battlefield:" + why)
		}
	}
	// Needs the start's own line already meets, or must not: a combat-damage
	// start's attack must connect (no blocker), and a lifegain start's carrier
	// is the lifegain.
	skip := func(n certdeck.Need) bool {
		switch {
		case trig == nil:
			return false
		case combatTrigModes[trig.Mode]:
			return n.Class == certdeck.OppBlockers
		case trig.Mode == "LifeGained":
			return n.Class == certdeck.Life && n.Arg == "gain"
		case n.Class == certdeck.Bodies && len(f.Setup.P0.Attach) > 0:
			// An Aura start places its own bearer, on the seat its Enchant
			// keyword names: certdeck's body for the Aura would be a second
			// creature, on our side, that the removal could take instead.
			return true
		case n.Class == certdeck.Token && focusCastsToken(reg, b, f, card, triggerFilter(trig)):
			// The trigger's own token filter chose a token-maker focus; the
			// token enters as that cast, so no placed token is needed (and a
			// start cannot place one).
			return true
		}
		return false
	}
	// A static or combat key places its own objects (staticKeyStart,
	// combatKeyStart): what else the card reads (a token, a graveyard) is
	// no part of the key, and a need for it would refuse the key or add state.
	if fam != "static" && fam != "combat" {
		if why, err := placeNeeds(reg, b, c, card, generic, skip, f); err != nil || why != "" {
			if err != nil {
				if trig != nil && combatTrigModes[trig.Mode] {
					// a card certdeck cannot build (an unsupported script, a
					// name XMage spells differently): the combat key keeps the
					// named refusal it had before it had a start
					return refuse("no-generator:trigger:" + trig.Mode + ":unsupported")
				}
				return Result{}, err
			}
			return refuse(why)
		}
	}
	placeGraveTargets(reg, b, face, card, fam, sa, trig, f)
	if why, err := focusPlayRefusal(reg, b, face, sa, card, f); err != nil || why != "" {
		if err != nil {
			return Result{}, err
		}
		return refuse(why)
	}
	return Result{Path: PathStart, Start: f}, nil
}

// FieldBlock is why a start file cannot list face on the battlefield, or "":
// an instant or sorcery is never a permanent (CR 304.4, 307.4), and an Aura
// a start file places has no bearer unless it is in StartSeat.Attach (a
// caller that sees the attachment allows aura-unattached), so a bare Aura
// stays where it is (CR 303.4g). Either way the engines disagree about where
// the card ends up (gorge left Path to Exile on the battlefield and lost an
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
// skip drops the needs the start's own line meets or must not have.
func placeNeeds(reg *cards.Registry, b *certdeck.Builder, c *cards.Card, card, generic string, skip func(certdeck.Need) bool, f *StartFile) (string, error) {
	needs := slices.DeleteFunc(certdeck.Derive(c, b.Types()), skip)
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
	bound := false
	for _, c := range s.Counters {
		if c.Card == card && c.Kind == kind {
			return nil // a cost's counters (planCost) already meet the need
		}
		bound = bound || c.Card == card
	}
	// A start binds one counters entry to each battlefield copy, so a card
	// already carrying a cost's counters takes the need's on a bearer.
	if !slices.Contains(s.Battlefield, on) || bound {
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
		if c, ok := reg.Lookup(name); ok && b.Playable(c) && !deadOnArrival(c.Faces[0]) {
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
	var faces []*cards.Face
	var live []string
	for _, name := range picks {
		c, ok := reg.Lookup(name)
		if !ok || len(c.Faces) == 0 {
			return fmt.Errorf("scen: enabler %q is not in the corpus", name)
		}
		if deadOnArrival(c.Faces[0]) {
			continue // an X creature with no X dies to SBA the moment it is placed
		}
		faces, live = append(faces, c.Faces[0]), append(live, name)
	}
	picks = live
	if len(picks) == 0 {
		return nil
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
			if c, ok := reg.Lookup(name); ok && b.Playable(c) && !deadOnArrival(c.Faces[0]) {
				s.Battlefield = append(s.Battlefield, name)
				return nil
			}
		}
		return fmt.Errorf("scen: no inert Aura bearer (%v) in the corpus", bearers)
	}
	return nil
}
