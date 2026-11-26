package scen

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/certdeck"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/lower"
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
// the first other token no start setup can pay. A Sac token's shape only is
// pre-cleared here: planCost does the real work and refuses a spec no
// placement meets (fodder, the token kinds, the fixture scan) with the same
// steps-needed:cost:Sac.
func costTokens(cost string) (mana []string, refusal string) {
	for _, tok := range splitCost(cost) {
		switch {
		case manaTokenRE.MatchString(tok):
			mana = append(mana, tok)
		case tok == "T" || tok == "Q" || payLifeRE.MatchString(tok):
		case sacCostShape(tok):
		default:
			if i := strings.IndexByte(tok, '<'); i >= 0 {
				tok = tok[:i]
			}
			return nil, "steps-needed:cost:" + tok
		}
	}
	return mana, ""
}

// sacCostShape reports whether tok is a Sac token costArgs reads: Sac<N/Spec>
// with a whole N of 0 or more. The spec itself is planCost's work.
func sacCostShape(tok string) bool {
	head, args := costArgs(tok)
	if head != "Sac" || len(args) < 2 {
		return false
	}
	n, err := strconv.Atoi(args[0])
	return err == nil && n >= 0
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

// asEntersRefusal is the refusal of a card whose as-enters choice a start that
// places it cannot make: a land is played, not cast, so its choice (a colour or
// a type) cannot be made by the one play the focus takes ("as-enters:land");
// every other card is plain "as-enters".
func asEntersRefusal(f *cards.Face) string {
	if f.IsLand() {
		return "as-enters:land"
	}
	return "as-enters"
}

// asEntersStart serves a trigger key of a card whose own replacement is a
// ChooseColor: the card is cast from hand on turn 1 (castStart), so the three
// engines make the choice by their normal decision and the replay carries the
// pick, and the trigger is then met on the line after the cast:
//
//   - a state trigger on the chosen colour (Mode Always: "when you control no
//     permanents of the chosen color") fires from the cast itself;
//   - "whenever a player casts a spell of the chosen color" (SpellCast, ValidCard
//     Card.ChosenColor) is met by a one-mana carrier of each colour in the hand,
//     the pick not being known when the start is written, and exactly its lands:
//     the focus bot casts the key card first, the bots then cast the carriers.
//
// Any other shape (a type choice, a trigger on another event) refuses by the
// plain as-enters reason, as does a land (asEntersRefusal).
func asEntersStart(reg *cards.Registry, b *certdeck.Builder, face *cards.Face, card, generic string, trig *cards.Trigger, f *StartFile) string {
	if face.IsLand() || !asEntersColour(face) {
		return asEntersRefusal(face)
	}
	if tz := trig.Params["TriggerZones"]; tz != "" && tz != "Battlefield" {
		return asEntersRefusal(face)
	}
	switch trig.Mode {
	case "Always":
		if !strings.Contains(trig.Params["IsPresent"], "ChosenColor") {
			return asEntersRefusal(face)
		}
	case "SpellCast":
		if strings.TrimSpace(trig.Params["ValidCard"]) != "Card.ChosenColor" || trig.Params["ValidActivatingPlayer"] != "" {
			return asEntersRefusal(face)
		}
	default:
		return asEntersRefusal(face)
	}
	if why := castStart(reg, b, face, card, generic, f); why != "" {
		return why
	}
	if trig.Mode == "Always" {
		return ""
	}
	p0 := &f.Setup.P0
	for _, sym := range []string{"W", "U", "B", "R", "G"} {
		name := pickCarrier(reg, b, card, func(cf *cards.Face) bool { return cf.ManaCost == sym })
		if name == "" {
			return "as-enters:carrier"
		}
		castCarrier(reg, p0, name)
	}
	return ""
}

// asEntersColour reports whether the as-enters choice of f is a ChooseColor.
func asEntersColour(f *cards.Face) bool {
	for _, r := range f.Repls {
		if r.Event == "Moved" && strings.Contains(r.Params["ValidCard"], "Card.Self") && r.Params["Destination"] == "Battlefield" &&
			r.With != nil && r.With.API == "ChooseColor" {
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
	plan, why := planCostLine(reg, b, face, card, sa.Params["Cost"], generic)
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
	if plan.zone == zoneBattlefield && plan.bearer == "" && FieldBlock(face) == "aura-unattached" {
		// The focus Aura a start places enters attached (FieldBlock refuses a
		// bare one): the bearer its Enchant keyword takes. A cost that spends
		// the enchanted permanent (Sac<Card.AttachedBy>) already picked it.
		bearer, bs := attachBearer(reg, b, card, face)
		if bearer == "" {
			return "battlefield:aura-unattached"
		}
		plan.bearer, plan.bearerSeat = bearer, bs
	}
	applyCost(plan, card, generic, f)
	f.Focus = &StartFocus{Seat: seat(0), Card: card, Kind: "ability", Ability: idx}
	if plan.attacker != "" {
		// ninjutsu: the cost returns an unblocked attacker, so the focus waits
		// for a scripted combat (one attacker, no block) and takes its
		// activation at its first priority after the blocks (the ability's
		// ActivationPhases are Declare Blockers and later)
		fix, ok := reg.Lookup(combatFixture)
		if !ok || len(fix.Faces) == 0 || !fix.Faces[0].IsCreature() {
			return "steps-needed:cost:Return:fixture"
		}
		if why := combatLine(reg, b, face, fix.Faces[0], card, party{filter: plan.attacker}, nil, 0, f, "steps-needed:cost:Return"); why != "" {
			return why
		}
		f.Focus = &StartFocus{Seat: seat(0), Card: card, Kind: "ability", Ability: idx, Attack: f.Focus.Attack}
	}
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
			r, _ := refuse(asEntersRefusal(face))
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
		// A Transformed trigger places the card on its front face and
		// transforms it there (transformedStart), so the trigger face's
		// as-enters choice is not the placement's; transformedStart checks
		// the front face's. A Cycled trigger's card cycles from hand and
		// never enters the battlefield at all.
		if fam == "trigger" && trig.Mode != "Transformed" && trig.Mode != "Cycled" && asEnters(face) {
			// The card cannot be placed (it would enter without its choice):
			// it is cast, and its trigger is met on the line after the cast.
			if why := asEntersStart(reg, b, face, card, generic, trig, f); why != "" {
				return refuse(why)
			}
			break
		}
		if trig == nil || trig.Mode != "Transformed" && trig.Mode != "Cycled" {
			if r, ok := onBF(); ok {
				return r, nil
			}
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
		case "Drawn", "Discarded", "Sacrificed", "Taps", "TapsForMana", "Untaps", "CommitCrime", "TurnFaceUp", "Cycled":
			if why := eventStart(reg, b, face, card, generic, trig, f); why != "" {
				return refuse(why)
			}
		case "Transformed":
			if why := transformedStart(reg, b, c, face, card, generic, trig, f); why != "" {
				return refuse(why)
			}
		default:
			return refuse("no-generator:trigger:" + trig.Mode)
		}
	default:
		return refuse("no-generator")
	}
	if sa == nil && (trig == nil || trig.Mode != "Transformed") && f.Focus != nil && f.Focus.Kind == "ability" && f.Focus.Card == card && f.Focus.Ability < len(face.Abilities) {
		// a trigger start whose event is the card's own ability (it taps, it is
		// sacrificed): the ability's targets are the focus play's. A
		// Transformed trigger's focus ability is the front face's transform
		// cause, not an ability of the trigger face.
		sa = face.Abilities[f.Focus.Ability]
	}
	if hist != "" {
		if why := castLine(face, card, hist, generic, f); why != "" {
			return refuse(why)
		}
	}
	if fi > 0 && (trig == nil || trig.Mode != "Transformed") {
		// a back-face key is played on the back face of the battlefield card:
		// the first listed copy enters transformed (rules.StartSeat.BackFace).
		// A Transformed trigger is the exception: its start stands the card
		// front-up and transforms it on the line (CR 701.27), so listing the
		// copy as BackFace would enter it on the trigger face without a
		// transform event and never fire the trigger.
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
	// The key's own card, placed on the battlefield, must survive to the first
	// main priority and be able to take the start's line. The checks that
	// read only the card (focusCardBlock) run here, before FieldBlock's
	// generic loop, so the refusal names the start (start:dead-on-arrival,
	// start:echo, ...) rather than surfacing later as play's
	// start-not-exercised. The one that reads the finished start (a CDA
	// toughness counting what the needs place) runs after them.
	if slices.Contains(p0.Battlefield, card) || slices.Contains(p1.Battlefield, card) {
		if why := focusCardBlock(face); why != "" {
			return refuse(why)
		}
	}
	if why := attackBlock(reg, f); why != "" {
		return refuse(why)
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
			if why == "aura-unattached" && seat == p0 && attachKeyAura(reg, b, face, card, p0, p1) {
				continue // the key Aura gets a vanilla bearer
			}
			return refuse("battlefield:" + why)
		}
	}
	// Needs the start's own line already meets, or must not: a combat-damage
	// start's attack must connect (no blocker), and a lifegain start's carrier
	// is the lifegain.
	skip := func(n certdeck.Need) bool {
		switch {
		case n.Class == certdeck.Counters && fam == "activate" && adapts(sa):
			// CR 702.35a: an Adapt activation is withheld while its source
			// carries a +1/+1 counter. The card's own CounterAdded trigger
			// asks certdeck for a counters need (Growth-Chamber Guardian),
			// and placing that on the focus card left the very ability the
			// start replays unofferable.
			return true
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
	// The start now holds everything its needs placed: only a CDA toughness
	// still zero over this finished start (Mortivore, an empty graveyard) is
	// dead on arrival, and only an upkeep the finished start cannot pay
	// (focusUpkeepBlock) costs the card or the focus play's mana before the
	// first main priority. Eluge's Islands, a Forest for Gaea's Liege, are
	// placed by the needs above and are counted here.
	if slices.Contains(p0.Battlefield, card) || slices.Contains(p1.Battlefield, card) {
		if cdaToughness(face) && cdaToughnessZero(reg, face, f) {
			return refuse("start:dead-on-arrival")
		}
		seat := 0
		if slices.Contains(p1.Battlefield, card) {
			seat = 1
		}
		if why := focusUpkeepBlock(reg, face, card, seat, f); why != "" {
			return refuse(why)
		}
	}
	if why, err := focusPlayRefusal(reg, b, face, sa, card, f); err != nil || why != "" {
		if err != nil {
			return Result{}, err
		}
		return refuse(why)
	}
	if why := dayNightRefusal(reg, f); why != "" {
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

// spellLock reports a static on a placed permanent that keeps its controller
// from casting any spell (Form of the Squirrel's "you can't cast spells"). It
// is an enabler gate only (pickEnabler's battlefield pick), not part of
// FieldBlock: with Echo and cumulative-upkeep enablers kept off the
// battlefield, the next pick for a cast key's enabler need (Goldenhide Ox's
// enchantment) is such a lock, and the focus cast would never be offered. A
// limit per turn (NumLimitEachTurn) is not a lock, and a self-only "can't cast
// this" says nothing about the focus play.
func spellLock(face *cards.Face) bool {
	for i := range face.Statics {
		st := &face.Statics[i]
		if st.Mode != "CantBeCast" || strings.TrimSpace(st.Params["Caster"]) != "You" ||
			st.Params["NumLimitEachTurn"] != "" {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(st.Params["ValidCard"])) {
		case "", "card", "permanent":
			return true
		}
	}
	return false
}

// upkeepLiability is why a permanent cannot be guaranteed to be on the
// battlefield (and its controller's mana untouched) at turn 1's first main
// priority, or "": the setup drive plays the game's upkeep before the cut.
// It is not part of FieldBlock: play's loader of an already-written start
// calls that gate on every battlefield card, and a card held in hand or
// placed as the key's own is judged by the callers that know why it is there
// (pickEnabler's battlefield pick, focusCardBlock).
// Echo and cumulative upkeep make the controller pay from the same mana the
// focus play needs; a beginning-of-upkeep or draw trigger that sacrifices the
// permanent or charges it a cost does the same or loses the card outright.
// A trigger that only adds a resource (Aethersquall Ancient's upkeep energy)
// is not a liability: the start's energy need and the trigger agree.
func upkeepLiability(face *cards.Face) string {
	switch {
	case face.HasKeyword("Echo"):
		return "echo"
	case face.HasKeyword("Cumulative upkeep"):
		return "cumulative-upkeep"
	case upkeepCostsSelf(face):
		return "upkeep-cost"
	}
	return ""
}

// upkeepCostsSelf reports a beginning-of-upkeep or draw-step Phase trigger
// that sacrifices its own source or makes its controller pay a cost (mana,
// a discard, a life payment). The trigger's Execute SVar is walked with its
// SubAbility chain; a trigger that only adds counters or draws is not a
// liability. It is the enabler gate's strict read (pickEnabler): the focus
// card's own upkeep is judged over the finished start instead
// (focusUpkeepBlock), because a start may hold the mana the cost takes.
func upkeepCostsSelf(face *cards.Face) bool {
	for i := range face.Triggers {
		t := &face.Triggers[i]
		if !strings.EqualFold(t.Mode, "Phase") {
			continue
		}
		ph := strings.ToLower(t.Params["Phase"])
		if !strings.Contains(ph, "upkeep") && !strings.Contains(ph, "draw") {
			continue
		}
		for sa := t.Effect; sa != nil; sa = sa.Sub {
			if sa.API == "Sacrifice" || sa.Params["UnlessPayer"] != "" || sa.Params["UnlessCost"] != "" || sa.Params["Cost"] != "" {
				return true
			}
		}
	}
	return false
}

// focusUpkeepBlock is why the finished start cannot be assumed to reach turn
// 1's first main priority with the focus card on the battlefield and the
// focus play's mana, or "". The setup drive plays the upkeep before the cut:
// Echo and cumulative upkeep ask their cost then, and a beginning-of-upkeep
// or draw trigger may sacrifice its source or make its controller pay. Like
// the CDA check above it, it judges the finished start:
//   - when the focus play is another card's, or the card is still in hand (a
//     cast or a land drop), the focus card's fate costs the key nothing: the
//     play is still offered (Earthlink's upkeep may sacrifice it before its
//     Kami of False Hope dies);
//   - a trigger that cannot fire from the start's placement (a TriggerZones
//     other than Battlefield, Pyre Zombie's graveyard) or is optional
//     (OptionalDecider, Brass Gnat's "you may pay {1}") never binds;
//   - a "your upkeep" trigger whose source sits under the non-active seat
//     does not fire on turn 1 (the block keys);
//   - otherwise the cost is judged against the start: refuse only if the
//     seat's untapped lands cannot pay it as well as the focus play's own
//     mana, or the hand, graveyard or battlefield resource it spends is not
//     there (Molten-Tail Masticore's discard from an empty hand).
func focusUpkeepBlock(reg *cards.Registry, face *cards.Face, card string, seat int, f *StartFile) string {
	if !focusHoldsCard(f, card) {
		return ""
	}
	if k, ok := face.KeywordParam("Echo"); ok {
		cost := k
		if strings.TrimSpace(cost) == "" {
			cost = face.ManaCost
		}
		if upkeepManaShort(reg, f, seat, cost, focusManaCost(reg, f)) {
			return "start:echo"
		}
	}
	if k, ok := face.KeywordParam("Cumulative upkeep"); ok && strings.TrimSpace(k) != "" {
		if upkeepManaShort(reg, f, seat, k, focusManaCost(reg, f)) {
			return "start:cumulative-upkeep"
		}
	}
	for i := range face.Triggers {
		req, binds := triggerUpkeepReq(&face.Triggers[i], seat, f)
		if !binds {
			continue
		}
		if upkeepReqShort(reg, f, seat, req, focusManaCost(reg, f)) {
			return "start:upkeep-cost"
		}
	}
	return ""
}

// focusHoldsCard reports whether the focus play is on the card itself, or a
// static or scripted combat the card must live for: only then does the
// card's turn-1 upkeep cost the key anything.
func focusHoldsCard(f *StartFile, card string) bool {
	fo := f.Focus
	if fo == nil || fo.Card == "" {
		return true
	}
	if fo.Card != card {
		return false // the focus play is another card's
	}
	switch fo.Kind {
	case "cast", "land":
		return false // in hand until main 1: no upkeep trigger fires yet
	}
	return true
}

// upkeepReq is what one beginning-of-upkeep or draw trigger asks of its
// controller's placed state at turn 1's upkeep.
type upkeepReq struct {
	mana        string // mana tokens the seat must tap
	hand        int    // cards the hand must hold
	grave       int    // matching cards the graveyard must hold
	graveFilter string
	field       string // a permanent type the battlefield must hold (a sacrifice of another)
	self        bool   // the card itself is sacrificed with no cost to avoid it
	atRisk      bool   // the chain sacrifices the card (a cost unpaid loses it)
}

// triggerUpkeepReq is the upkeepReq of one Phase trigger, and whether it
// binds the start at all: false when the trigger cannot fire from the
// start's placement, is optional, is not the active seat's turn, or takes
// nothing the start can lack.
func triggerUpkeepReq(t *cards.Trigger, seat int, f *StartFile) (upkeepReq, bool) {
	req := upkeepReq{}
	ph := strings.ToLower(t.Params["Phase"])
	if !strings.Contains(ph, "upkeep") && !strings.Contains(ph, "draw") {
		return req, false
	}
	if z := t.Params["TriggerZones"]; z != "" && !zoneOnBattlefield(z) {
		return req, false // the trigger never fires from the battlefield (Pyre Zombie)
	}
	if t.Params["OptionalDecider"] != "" {
		return req, false // "you may": the bot declines to no loss (Brass Gnat)
	}
	if strings.EqualFold(t.Params["ValidPlayer"], "You") && seat != f.Active {
		return req, false // "your upkeep": not the active seat's turn (the block keys)
	}
	for sa := t.Effect; sa != nil; sa = sa.Sub {
		// An optional step the controller declines to no effect (an optional
		// sacrifice of another permanent, Yawgmoth Demon's artifact) is no
		// cost; an optional sacrifice of the card itself still may lose it.
		if sa.Params["Optional"] == "True" && !(sa.API == "Sacrifice" && sacValidSelf(sa)) {
			continue
		}
		if c := sa.Params["UnlessCost"]; c != "" {
			mergeUpkeepCost(&req, c)
		} else if c := sa.Params["Cost"]; c != "" {
			mergeUpkeepCost(&req, c)
		}
		switch sa.API {
		case "Sacrifice":
			if sacValidSelf(sa) {
				req.atRisk = true
				if sa.Params["UnlessCost"] == "" && sa.Params["Cost"] == "" && sa.Params["UnlessPayer"] == "" {
					req.self = true
				}
			} else if req.field == "" {
				req.field = sa.Params["SacValid"]
			}
		case "Discard":
			if n := saNum(sa); n > req.hand {
				req.hand = n
			}
		case "ChangeZone":
			if strings.Contains(sa.Params["Origin"], "Graveyard") && strings.Contains(sa.Params["Destination"], "Exile") {
				if n := saNum(sa); n > req.grave {
					req.grave = n
					req.graveFilter = sa.Params["ChangeType"]
				}
			}
		}
	}
	return req, req.mana != "" || req.hand > 0 || req.grave > 0 || req.field != "" || req.self
}

// zoneOnBattlefield reads a TriggerZones list ("Battlefield",
// "Battlefield,Command"): whether it holds Battlefield, the zone a placed
// permanent's trigger fires from.
func zoneOnBattlefield(z string) bool {
	for _, w := range strings.Split(z, ",") {
		if strings.EqualFold(strings.TrimSpace(w), "Battlefield") {
			return true
		}
	}
	return false
}

// saNum is an SA's card count (NumCards, ChangeNum), 1 when it says none.
func saNum(sa *cards.SA) int {
	for _, p := range []string{"NumCards", "ChangeNum", "NumAtt"} {
		if v := sa.Params[p]; v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				return n
			}
		}
	}
	return 1
}

// sacValidSelf reads a Sacrifice's SacValid: the card itself when it names
// none, the card, or a Self alternative of it.
func sacValidSelf(sa *cards.SA) bool {
	v := strings.TrimSpace(sa.Params["SacValid"])
	if v == "" || v == "CARDNAME" || v == "NICKNAME" {
		return true
	}
	if strings.Contains(v, "Other") {
		return false
	}
	return strings.Contains(v, "Self")
}

// mergeUpkeepCost reads one UnlessCost/Cost into req: mana tokens, and the
// non-mana steps a start can lack (a discard, a graveyard exile, a reveal).
func mergeUpkeepCost(req *upkeepReq, cost string) {
	for _, tok := range splitCost(cost) {
		if manaTokenRE.MatchString(tok) {
			req.mana = strings.TrimSpace(req.mana + " " + tok)
			continue
		}
		head, args := costArgs(tok)
		n := 1
		if len(args) >= 1 {
			if v, err := strconv.Atoi(args[0]); err == nil {
				n = v
			}
		}
		switch head {
		case "Discard", "Reveal":
			if n > req.hand {
				req.hand = n
			}
		case "Exile", "ExileFromGrave":
			if head == "ExileFromGrave" || len(args) >= 3 && strings.Contains(args[2], "Graveyard") {
				if n > req.grave {
					req.grave = n
					if len(args) >= 2 {
						req.graveFilter = args[1]
					}
				}
			} else if n > req.hand {
				req.hand = n
			}
		case "PayMana":
			if len(args) >= 1 {
				req.mana = strings.TrimSpace(req.mana + " " + args[0])
			}
		}
	}
}

// upkeepReqShort reports whether the start cannot meet req as well as the
// focus play's own mana. A cost the judge cannot read is no proof of a short
// start.
func upkeepReqShort(reg *cards.Registry, f *StartFile, seat int, req upkeepReq, focusMana []string) bool {
	if req.self {
		return true // sacrificed whatever the start holds
	}
	sd := &f.Setup.P0
	if seat != 0 {
		sd = &f.Setup.P1
	}
	// A resource the start lacks only costs the key when the card's survival
	// rides on it (a sacrifice the cost must avert): a bare mandatory
	// discard or graveyard exile over an empty hand or graveyard is a
	// trigger that does nothing (Enemy of Enlightenment's "each player
	// discards a card"). The mana is the same either way: an unpaid
	// sacrifice cost still sacrifices, and an unpaid bare payment still
	// takes the tapped mana the focus play wants.
	if req.hand > 0 && (req.atRisk || req.mana != "") && len(sd.Hand) < req.hand {
		return true
	}
	if req.atRisk && req.grave > 0 {
		n, ok := seatZoneCount(reg, f, seat, "graveyard", req.graveFilter)
		if ok && n < req.grave {
			return true
		}
	}
	if req.atRisk && req.field != "" {
		n, ok := seatZoneCount(reg, f, seat, "battlefield", req.field)
		if ok && n < 1 {
			return true
		}
	}
	if req.mana != "" {
		return upkeepManaShort(reg, f, seat, req.mana, focusMana)
	}
	return false
}

// seatZoneCount is how many cards of one seat's zone match filter; ok is
// false when no alternative of the filter is readable.
func seatZoneCount(reg *cards.Registry, f *StartFile, seat int, zone, filter string) (int, bool) {
	alts := parseFilter(filter)
	if len(alts) == 0 {
		return 0, false
	}
	known := false
	for _, a := range alts {
		known = known || a.known
	}
	if !known {
		return 0, false
	}
	sd := &f.Setup.P0
	if seat != 0 {
		sd = &f.Setup.P1
	}
	var names []string
	switch zone {
	case "graveyard":
		names = sd.Graveyard
	case "hand":
		names = sd.Hand
	default:
		names = sd.Battlefield
	}
	n := 0
	for _, name := range names {
		c, ok := reg.Lookup(name)
		if !ok || len(c.Faces) == 0 {
			continue
		}
		for _, a := range alts {
			if !a.known || a.ctrl == ctrlOpp {
				continue
			}
			if a.test(c.Faces[0]) {
				n++
				break
			}
		}
	}
	return n, true
}

// focusManaCost is the mana the focus play taps from the same untapped lands
// the upkeep would pay from: a cast focus its card's mana cost (plus a
// kicked cast's kicker), an ability focus the ability's Cost. nil when the
// focus play needs no mana (a land drop, the scripted combat) or its cost
// has none.
func focusManaCost(reg *cards.Registry, f *StartFile) []string {
	fo := f.Focus
	if fo == nil || fo.Card == "" {
		return nil
	}
	c, ok := reg.Lookup(fo.Card)
	if !ok || len(c.Faces) == 0 {
		return nil
	}
	face := c.Faces[0]
	var cost string
	switch fo.Kind {
	case "cast":
		cost = face.ManaCost
		if fo.Mode == "kicked" {
			if k, ok := face.KeywordParam("Kicker"); ok && !strings.Contains(k, ":") {
				cost = strings.TrimSpace(cost + " " + k)
			}
		}
	case "ability":
		if fo.Ability >= 0 && fo.Ability < len(face.Abilities) {
			cost = face.Abilities[fo.Ability].Params["Cost"]
		}
	}
	if cost == "" {
		return nil
	}
	mana, why := costTokens(cost)
	if why != "" {
		return nil
	}
	return mana
}

// basicColour is a basic land's colour letter.
var basicColour = map[string]string{"Plains": "W", "Island": "U", "Swamp": "B", "Mountain": "R", "Forest": "G"}

// upkeepManaShort reports whether seat's untapped lands cannot pay mana as
// well as the focus play's own cost: a basic pays its colour, an untapped
// non-basic land pays generic only, and the focus play's tokens are paid
// first. A cost the judge cannot read is not a short start.
func upkeepManaShort(reg *cards.Registry, f *StartFile, seat int, cost string, focusMana []string) bool {
	mana, why := costTokens(cost)
	if why != "" {
		return false
	}
	need := map[string]int{}
	generic := 0
	for _, toks := range [][]string{focusMana, mana} {
		for _, tok := range toks {
			if n, err := strconv.Atoi(tok); err == nil {
				generic += n
				continue
			}
			first := shardParts(tok)[0]
			if c := basicOf[first]; c != "" && first != "C" {
				need[first]++
			} else {
				generic++ // C, S and a Phyrexian alternative the life pays
			}
		}
	}
	sd := &f.Setup.P0
	if seat != 0 {
		sd = &f.Setup.P1
	}
	pool, avail := 0, map[string]int{}
	for _, name := range sd.Battlefield {
		c, ok := reg.Lookup(name)
		if !ok || len(c.Faces) == 0 || !c.Faces[0].HasTypeWord("Land") || slices.Contains(sd.Tapped, name) {
			continue
		}
		pool++
		if col, ok := basicColour[name]; ok {
			avail[col]++
		}
	}
	for col, n := range need {
		if avail[col] < n {
			return true
		}
		pool -= n
	}
	return pool < generic
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

// adapts reports whether sa is an AB$ PutCounter's Adapt$ activation, which
// gorge offers only while the source has no +1/+1 counters (CR 702.35a,
// rules' adaptGateOK). A start that meets a counters need on the focus card
// would otherwise leave the key's own play unofferable.
func adapts(sa *cards.SA) bool {
	return sa != nil && sa.Params["Adapt"] != ""
}

// placeNeeds adds what certdeck.Derive says the card reads (see the forced
// start above): graveyard fodder; for a lands need, certdeck's two extra
// lands, tapped, so they add no mana; for opp-burn, the target seat's life
// lowered; for a token need, the token itself (tokenScript, minted by
// rules.StartSeat.Tokens) so no enabler has to be cast to make one; for a
// counters need, the counters themselves (placeCounters); for every other
// need, one of certdeck's picks (pickEnabler). A need StartSpec cannot place
// is a refusal: a counters need a removal reads from a player, not a
// permanent (reasonPlayerCounters). skip drops the needs the start's own line
// meets or must not have.
func placeNeeds(reg *cards.Registry, b *certdeck.Builder, c *cards.Card, card, generic string, skip func(certdeck.Need) bool, f *StartFile) (string, error) {
	needs := slices.DeleteFunc(certdeck.Derive(c, b.Types()), skip)
	if len(needs) == 0 {
		return "", nil
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
		case n.Class == certdeck.Token:
			script, ok := tokenScript(reg, n.Arg)
			if !ok {
				return "steps-needed:" + n.Class, nil
			}
			f.Setup.P0.Tokens = append(f.Setup.P0.Tokens, StartToken{Script: script})
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

// genericToken is the token a kindless Token need places: the corpus's Servo,
// an artifact creature token with no abilities (it reads as both a creature
// token and an artifact token) that XMage's setup token table also maps, so
// both followers can replay the start. A corpus without it falls back to the
// most inert artifact creature token.
const genericToken = "c_1_1_a_servo"

// tokenScript is the token definition a Token need places: the canonical
// "<kind> Token" script for a predefined artifact kind (Treasure, Food, Clue,
// Blood, Powerstone, Map, Junk), else genericToken. Only a script XMage's setup
// token table also maps is placed (lower.SetupTokenScript): a start that named
// a token only Forge could replay would refuse at lowering time, so a kind the
// table lacks keeps the clean steps-needed:token refusal. Sorted script keys
// make the pick deterministic; "" is that refusal.
func tokenScript(reg *cards.Registry, kind string) (string, bool) {
	keys := make([]string, 0, len(reg.Tokens))
	for k := range reg.Tokens {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	face := func(k string) *cards.Face {
		c := reg.Tokens[k]
		if c == nil || len(c.Faces) == 0 {
			return nil
		}
		return c.Faces[0]
	}
	if kind != "" {
		for _, k := range keys {
			if f := face(k); f != nil && strings.EqualFold(f.Name, kind+" Token") && lower.SetupTokenScript(k) {
				return k, true
			}
		}
		// A token whose script names the kind (the same test certdeck uses to
		// pick a maker) when the corpus spells its display name differently.
		for _, k := range keys {
			if strings.Contains(strings.ToLower(k), strings.ToLower(kind)) && lower.SetupTokenScript(k) {
				return k, true
			}
		}
		return "", false
	}
	if f := face(genericToken); f != nil && f.IsCreature() && f.IsArtifact() && lower.SetupTokenScript(genericToken) {
		return genericToken, true
	}
	best, bestRank := "", 0
	for _, k := range keys {
		f := face(k)
		if f == nil || !f.IsCreature() || !lower.SetupTokenScript(k) {
			continue
		}
		r := inertRank(f)
		if !f.IsArtifact() {
			r += 3 // a creature token still meets "creature token", but not "artifact token"
		}
		if best == "" || r < bestRank {
			best, bestRank = k, r
		}
	}
	return best, best != ""
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

// entryCounterUnrestatable reports whether f's placement on the battlefield
// cannot restate its counter state: an etbCounter grant with a dynamic amount
// (K:etbCounter:P1P1:X, XKicked, Count$...) re-runs on placement and the start
// schema has no field for what the live entry added. It mirrors the lower
// gate's reasonCountersEntry, so an enabler pick cannot make the generated
// start unlowerable.
func entryCounterUnrestatable(f *cards.Face) bool {
	p, ok := f.KeywordParam("etbCounter")
	if !ok {
		return false
	}
	for _, marker := range []string{"Condition", "CheckSVar", "SVarCompare", "Revolt", "Adamant", "IsPresent", "Optional"} {
		if strings.Contains(p, marker) {
			return true // an entry condition the placement would re-run
		}
	}
	parts := strings.Split(p, ":")
	if len(parts) < 2 {
		return false
	}
	_, err := strconv.Atoi(parts[1])
	return err != nil
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

// enablerRank is inertRank plus a penalty for every battlefield-entry
// replacement effect the placed card brings. The setup placement re-runs a
// Moved replacement (K:ETBReplacement and the printed R:Event$ Moved lines),
// and a body that asks an as-enters choice (Realmwright's basic land type)
// leaves state the scenario cannot restate, so the cut refuses the start.
// Ranking a replacement-bearing card behind one without keeps Wizard's Retort
// off Realmwright. Only entry replacements are penalized: an ordinary trigger
// or static (Shield Sphere's block trigger, Lunarch Veteran's life trigger) is
// not a shape the gate refuses, and penalizing it would reorder enabler picks
// outside that shape.
func enablerRank(f *cards.Face) int {
	r := inertRank(f)
	for i := range f.Repls {
		if f.Repls[i].Event == "Moved" {
			r += 3
		}
	}
	return r
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
		if f := c.Faces[0]; deadOnArrival(f) || entryCounterUnrestatable(f) {
			continue // an X creature with no X dies to SBA the moment it is placed; a dynamic entry counter cannot be restated
		}
		faces, live = append(faces, c.Faces[0]), append(live, name)
	}
	picks = live
	if len(picks) == 0 {
		return nil
	}
	// A battlefield pick must have no beginning-of-upkeep or draw trigger. The
	// span can only decline an optional one whose probe asks a plain
	// trigger_optional yes/no; an enabler's trigger is incidental, and if it
	// asks anything else (Drifter il-Dal's "sacrifice unless you pay {U}" is a
	// choose_n ask) the placement refuses the span. Skip such picks for the
	// battlefield (they can still go in hand below); the next enabler of the
	// need takes their place.
	best := -1
	for i, f := range faces {
		// An Echo or cumulative-upkeep enabler stays off the battlefield too
		// (upkeepLiability): the turn-1 upkeep would have it tap the focus
		// play's mana. In hand it is cast with its own lands, and its upkeep
		// comes after the cut.
		if FieldBlock(f) == "" && upkeepLiability(f) == "" && !spellLock(f) && !upkeepOrDraw(f) && (best < 0 || enablerRank(f) < enablerRank(faces[best])) {
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
