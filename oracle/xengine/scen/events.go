package scen

import (
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/certdeck"
	"github.com/adams-shaun/gorge/engine/cards"
)

// Event causers: the forced-line starts for a trigger whose event is a player
// or permanent action no attack or cast of the card itself brings about: a
// draw, a discard, a sacrifice, a tap, an untap, mana tapped for a mana, a
// crime. Each mode names the event it needs and the fixture that causes it,
// from one table (eventFixtures) the modes share: the draw spell, the discard
// spell, the targeting spell, the sacrifice and tap carriers (sacCarrier,
// tapPool, ranked pools of the corpus's own cards). The card itself
// is the causer when it can be ("Whenever CARDNAME becomes tapped" is its own
// {T} ability or its attack); otherwise a fixture on the seat the trigger's
// filter names is, and the focus play is that fixture's one play, on the seat
// that makes it.
//
// A trigger carrying a parameter the start does not model (a turn-count, a
// condition, a mana colour) is refused by name (eventParamsOK), never started
// without it: the start would exercise the mode but not the trigger's
// condition, and the key would be credited for a game that never met it.

// eventFixtures are the named cards that cause an event, in one place.
var eventFixtures = struct {
	// Draw is the instant a seat casts to draw one card.
	Draw string
	// Discard is the sorcery a seat casts to make the other discard a card.
	// It targets an opponent and nothing else ("target opponent"), so the
	// bot has no other target to choose: the forced line is one line.
	Discard string
	// Crime is a spell whose only legal target is an opponent: casting it
	// is a crime (CR 700.13) with no other target for the bot to pick.
	Crime string
	// Targeters are the spells that target a permanent, one per kind of
	// permanent, tried in order (targetStart).
	Targeters []targeter
}{
	Draw:    "Reach Through Mists",
	Discard: "Coercion",
	Crime:   "Coercion",
	Targeters: []targeter{
		{name: "Murder", takes: (*cards.Face).IsCreature},
		{name: "Disenchant", takes: func(f *cards.Face) bool { return f.HasTypeWord("Artifact") || f.HasTypeWord("Enchantment") }},
		{name: "Lightning Bolt", takes: func(f *cards.Face) bool { return f.IsCreature() || f.IsPlaneswalker() }},
		{name: "Stone Rain", takes: (*cards.Face).IsLand},
	},
}

// targeter is a spell that targets a permanent its takes accepts, whichever
// seat controls it. own is true for a spell that only targets the caster's
// own permanents (a protection trick, "target creature you control").
type targeter struct {
	name  string
	takes func(*cards.Face) bool
	own   bool
}

// ownTargeterSpells are the tricks a seat casts at its own creature.
var ownTargeterSpells = func() []targeter {
	var out []targeter
	for _, n := range ownTargeters {
		out = append(out, targeter{name: n, takes: (*cards.Face).IsCreature, own: true})
	}
	return out
}()

// baseEventParams are the trigger parameters that change nothing a start
// must meet: what the trigger does, who decides, how often.
var baseEventParams = map[string]bool{
	"Mode": true, "Execute": true, "TriggerDescription": true, "OptionalDecider": true, "Secondary": true,
	"ActivationLimit": true, "NoResolvingCheck": true, "TriggerZones": true, "Static": true,
}

// eventParamsOK is "" when every parameter of trig is a base one or in extra,
// else the refusal for mode: the trigger reads a condition the start does not
// model. A trigger zone but the battlefield is refused with it (a graveyard or
// hand trigger needs the card elsewhere).
func eventParamsOK(trig *cards.Trigger, mode string, extra ...string) string {
	none := "no-generator:trigger:" + mode
	if tz := trig.Params["TriggerZones"]; tz != "" && tz != "Battlefield" {
		return none + ":zone"
	}
	for k := range trig.Params {
		if !baseEventParams[k] && !slices.Contains(extra, k) {
			return none + ":condition"
		}
	}
	return ""
}

// playerRef reads a Forge player expression ("You", "Opponent", "Player",
// "Player.Opponent+NonActive") as the seat it names (-1: any player) and
// whether it requires the seat not to be the active one; ok is false for a
// word the start cannot meet ("Player.EnchantedBy").
func playerRef(s string) (seat int, nonActive, ok bool) {
	seat = -1
	for i, tok := range strings.FieldsFunc(s, func(r rune) bool { return r == '.' || r == '+' }) {
		switch tok {
		case "You":
			seat = 0
		case "Opponent":
			seat = 1
		case "Player":
			if i != 0 {
				return 0, false, false
			}
		case "Active":
		case "NonActive", "!Active":
			nonActive = true
		default:
			return 0, false, false
		}
	}
	return seat, nonActive, true
}

// turnWanted is the seat whose turn the trigger is limited to (PlayerTurn,
// OpponentTurn), or -1 for any.
func turnWanted(trig *cards.Trigger) int {
	switch {
	case trig.Params["PlayerTurn"] == "True":
		return 0
	case trig.Params["OpponentTurn"] == "True":
		return 1
	}
	return -1
}

// inZone drops the "is on the battlefield" restrictions of a target filter:
// every start target is a permanent on the battlefield already.
var inZone = strings.NewReplacer(".inZoneBattlefield+", ".", "+inZoneBattlefield", "", ".inZoneBattlefield", "")

// act makes seat s the focus: it takes kind of card (ability idx for an
// ability), the game on s's own turn, or the other seat's when off.
func act(f *StartFile, s int, off bool, card, kind string, ability int) {
	f.Active = s
	if off {
		f.Active = 1 - s
	}
	f.Focus = &StartFocus{Seat: seatPtr(s), Card: card, Kind: kind, Ability: ability}
}

func seatsOf(f *StartFile) [2]*StartSeat { return [2]*StartSeat{&f.Setup.P0, &f.Setup.P1} }

// spell is the named fixture when the corpus holds it and certdeck plays it.
func spell(reg *cards.Registry, b *certdeck.Builder, name string) bool {
	c, ok := reg.Lookup(name)
	return ok && len(c.Faces) > 0 && b.Playable(c)
}

// selfAbility is the index of the first ability of face pred accepts, or -1.
func selfAbility(face *cards.Face, pred func(*cards.SA) bool) int {
	for i, sa := range face.Abilities {
		if pred(sa) {
			return i
		}
	}
	return -1
}

func costHas(sa *cards.SA, tok string) bool {
	return slices.Contains(splitCost(sa.Params["Cost"]), tok)
}

// eventStart builds the start of a trigger of one of the event modes. It
// returns "" when it built the start into f, else the refusal.
func eventStart(reg *cards.Registry, b *certdeck.Builder, face *cards.Face, card, generic string, trig *cards.Trigger, f *StartFile) string {
	switch trig.Mode {
	case "Drawn":
		return drawStart(reg, b, card, trig, f)
	case "Discarded":
		return discardStart(reg, b, card, generic, trig, f)
	case "Sacrificed":
		return sacrificeStart(reg, b, face, card, generic, trig, f)
	case "Taps", "TapsForMana":
		return tapStart(reg, b, face, card, generic, trig, f)
	case "Untaps":
		return untapStart(reg, b, face, card, trig, f)
	case "CommitCrime":
		return crimeStart(reg, b, face, card, trig, f)
	case "Cycled":
		return cycledStart(reg, b, face, card, generic, trig, f)
	case "TurnFaceUp":
		// A face-down permanent cannot be placed, and turning one up takes a
		// second play after the one that casts it face down: the focus is one
		// play (ticket: two-step focus).
		return "no-generator:trigger:TurnFaceUp:face-down"
	}
	return "no-generator:trigger:" + trig.Mode
}

// cycledAbility is the granted ability a printed K:Cycling or K:TypeCycling
// line expands to (CR 702.29a): the play that raises the Cycled event.
func cycledAbility(sa *cards.SA) bool {
	h := cards.KeywordHead(sa.Params["KeywordLine"])
	return h == "Cycling" || h == "TypeCycling"
}

// cycledStart is "When you cycle CARDNAME" (Cycled, ValidCard$ Card.Self):
// the card is the cycled one, so the start is its own cycling activation,
// the same shape a channel key takes (abilityFocus): the card in hand with
// exactly the cycling cost's mana placed, the focus that activation. The
// trigger's description reads on the card in hand, so a TriggerZones the
// start does not name, a player other than the cycler or a gate the
// start cannot meet is refused by name. A watcher (ValidCard$ Card or
// Card.Other: the card elsewhere while another card cycles) takes the
// watcher arm (cycledWatcher): a carrier with cycling cycles on the seat
// the trigger names.
func cycledStart(reg *cards.Registry, b *certdeck.Builder, face *cards.Face, card, generic string, trig *cards.Trigger, f *StartFile) string {
	none := "no-generator:trigger:Cycled"
	if selfCard(trig) {
		if tz := trig.Params["TriggerZones"]; tz != "" {
			return none + ":zone"
		}
		for k := range trig.Params {
			if !baseEventParams[k] && k != "ValidCard" && k != "ValidPlayer" {
				return none + ":condition"
			}
		}
		if vp := trig.Params["ValidPlayer"]; vp != "" && vp != "You" {
			return none + ":player"
		}
		idx := selfAbility(face, cycledAbility)
		if idx < 0 {
			return none + ":ability"
		}
		return abilityFocus(reg, b, face, card, generic, idx, face.Abilities[idx], f)
	}
	return cycledWatcher(reg, b, face, card, generic, trig, f)
}

// cyclingPools holds each registry's cycling carriers, built once.
var cyclingPools sync.Map // *cards.Registry -> []*cards.Face

// cyclingPool is every card the corpus holds that a start can cycle, ranked:
// fewest abilities, triggers, statics and replacements (the most inert card
// cycles, so the one event the start offers is the one the key reads), then
// fewest keywords, then by mana value, nonlegendary first, then name.
func cyclingPool(reg *cards.Registry, b *certdeck.Builder) []*cards.Face {
	if v, ok := cyclingPools.Load(reg); ok {
		return v.([]*cards.Face)
	}
	var out []*cards.Face
	for _, c := range reg.AllCards() {
		if len(c.Faces) != 1 || !b.Playable(c) {
			continue
		}
		if f := c.Faces[0]; selfAbility(f, cycledAbility) >= 0 {
			out = append(out, f)
		}
	}
	load := func(f *cards.Face) int {
		return len(f.Abilities) + len(f.Triggers) + len(f.Statics) + len(f.Repls)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, c := out[i], out[j]
		if la, lc := load(a), load(c); la != lc {
			return la < lc
		}
		if ka, kc := len(a.Keywords), len(c.Keywords); ka != kc {
			return ka < kc
		}
		if a.Cmc() != c.Cmc() {
			return a.Cmc() < c.Cmc()
		}
		if a.IsLegendary() != c.IsLegendary() {
			return !a.IsLegendary()
		}
		return a.Name < c.Name
	})
	cyclingPools.Store(reg, out)
	return out
}

// cyclingCarrier is the card that cycles for a watcher start: the ranked
// pool's first entry other than the watcher whose cycling activation's cost
// the start can meet with placed state (planCost), with the index of the
// granted cycling ability the focus activates.
func cyclingCarrier(reg *cards.Registry, b *certdeck.Builder, card, generic string) (name string, idx int) {
	for _, f := range cyclingPool(reg, b) {
		if f.Name == card {
			continue
		}
		i := selfAbility(f, cycledAbility)
		if _, why := planCost(reg, b, f, f.Name, f.Abilities[i].Params["Cost"], generic); why != "" {
			continue
		}
		return f.Name, i
	}
	return "", -1
}

// auraBearer is an inert permanent the Aura takes, on the watcher's own
// seat, for a watcher start that places an attached Aura on the battlefield.
func auraBearer(reg *cards.Registry, b *certdeck.Builder, card string, face *cards.Face) string {
	req, ok := enchantReq(face)
	if !ok {
		return ""
	}
	for _, alt := range req.raw {
		a := parseFilter(alt)
		for i := range a {
			if !a[i].known || a[i].self || a[i].ctrl == ctrlOpp {
				continue
			}
			if n := pickCarrier(reg, b, card, func(f *cards.Face) bool {
				return f.IsPermanent() && !deadOnArrival(f) && a[i].test(f)
			}); n != "" {
				return n
			}
		}
	}
	return ""
}

// cycledWatcher is the watcher arm of Cycled: the card is not the cycled one
// ("Whenever a player cycles a card", "Whenever you cycle another card"). It
// sits where its TriggerZones put it — the battlefield, or the graveyard a
// return-to-battlefield watcher reads (Benalish Partisan) — while a carrier
// of the corpus (cyclingCarrier) cycles from its hand on the seat the
// trigger's player or cycled-card filter names: the carrier in that hand
// with exactly its cycling cost's mana placed (planCost), the focus that
// activation. FirstTime (Valiant Rescuer's "for the first time each turn")
// the start meets trivially: nothing has cycled yet. A parameter the arm
// does not model is refused by name, as is a watcher the trigger puts on a
// turn the cycler is not active on (cycling is sorcery-speed, the cycler's
// own turn).
func cycledWatcher(reg *cards.Registry, b *certdeck.Builder, face *cards.Face, card, generic string, trig *cards.Trigger, f *StartFile) string {
	none := "no-generator:trigger:Cycled"
	for k := range trig.Params {
		if !baseEventParams[k] && k != "ValidCard" && k != "ValidPlayer" && k != "FirstTime" {
			return none + ":condition"
		}
	}
	tz := trig.Params["TriggerZones"]
	switch tz {
	case "", "Battlefield", "Graveyard":
	default:
		return none + ":zone"
	}
	seat := 0
	if vp := trig.Params["ValidPlayer"]; vp != "" {
		s, na, ok := playerRef(vp)
		if !ok || na || s > 1 {
			return none + ":player"
		}
		seat = max(s, 0)
	} else {
		for _, a := range parseFilter(trig.Params["ValidCard"]) {
			if a.known && a.ctrl == ctrlOpp {
				seat = 1
			}
		}
	}
	if w := turnWanted(trig); w >= 0 && w != seat {
		return none + ":turn"
	}
	name, idx := cyclingCarrier(reg, b, card, generic)
	if name == "" {
		return none + ":carrier"
	}
	carrier, _ := reg.Lookup(name)
	cface := carrier.Faces[0]
	plan, why := planCost(reg, b, cface, name, cface.Abilities[idx].Params["Cost"], generic)
	if why != "" {
		return none + ":cost"
	}
	seats := seatsOf(f)
	if tz == "Graveyard" {
		seats[0].Graveyard = []string{card}
	} else {
		f.Setup.P0.Battlefield = []string{card}
		if blk := FieldBlock(face); blk != "" {
			if blk != "aura-unattached" {
				return none + ":" + blk
			}
			bearer := auraBearer(reg, b, card, face)
			if bearer == "" {
				return none + ":bearer"
			}
			f.Setup.P0.Battlefield = append(f.Setup.P0.Battlefield, bearer)
			f.Setup.P0.Attach = append(f.Setup.P0.Attach, StartAttach{Card: card, To: bearer})
		}
	}
	applyCostSupportOn(plan, generic, f, seats[seat])
	seats[seat].Hand = append(seats[seat].Hand, name)
	act(f, seat, false, name, "ability", idx)
	return ""
}

// drawStart is "whenever you draw a card" and its kin: the seat the draw is
// for casts the draw fixture, as many times as the trigger's Number says (the
// nth card each turn; a start begins after the draw step), on its own turn
// or, for a NonActive player, the other's.
func drawStart(reg *cards.Registry, b *certdeck.Builder, card string, trig *cards.Trigger, f *StartFile) string {
	none := "no-generator:trigger:Drawn"
	if why := eventParamsOK(trig, "Drawn", "ValidCard", "ValidPlayer", "Number", "PlayerTurn", "OpponentTurn"); why != "" {
		return why
	}
	seat, off := 0, false
	switch vp := trig.Params["ValidPlayer"]; {
	case vp != "":
		s, na, ok := playerRef(vp)
		if !ok {
			return none + ":player"
		}
		seat, off = max(s, 0), na
	case trig.Params["ValidCard"] != "":
		alts := parseFilter(trig.Params["ValidCard"])
		if len(alts) == 0 || !alts[0].known {
			return none + ":filter"
		}
		if alts[0].ctrl == ctrlOpp {
			seat = 1
		}
	}
	if w := turnWanted(trig); w >= 0 {
		off = w != seat
	}
	times := 1
	if n := trig.Params["Number"]; n != "" {
		_, cmp, ok := compare("cmcEQ" + n)
		if !ok {
			return none + ":history"
		}
		for times = 1; times <= 3 && !cmp(times); times++ {
		}
		if times > 3 {
			return none + ":history"
		}
	}
	if !spell(reg, b, eventFixtures.Draw) {
		return none + ":fixture"
	}
	f.Setup.P0.Battlefield = []string{card}
	for range times {
		castCarrier(reg, seatsOf(f)[seat], eventFixtures.Draw)
	}
	act(f, seat, off, eventFixtures.Draw, "cast", 0)
	if times > 1 {
		f.Focus.Times = times
	}
	return ""
}

// discardStart is "whenever a player discards a card": the discard fixture, a
// sorcery cast by the seat on its own turn at the other, whose hand is exactly
// one card that matches the trigger's filter (so the card discarded is the
// card the filter takes). "When you discard this card" is the card
// itself in that hand.
func discardStart(reg *cards.Registry, b *certdeck.Builder, card, generic string, trig *cards.Trigger, f *StartFile) string {
	none := "no-generator:trigger:Discarded"
	if why := eventParamsOK(trig, "Discarded", "ValidCard", "ValidPlayer", "ValidCause", "IsPresent", "TriggerController"); why != "" {
		return why
	}
	if c := trig.Params["ValidCause"]; c != "" && c != "SpellAbility.OppCtrl" {
		return none + ":cause"
	}
	if !spell(reg, b, eventFixtures.Discard) {
		return none + ":fixture"
	}
	seats := seatsOf(f)
	self := selfCard(trig)
	victim := 0
	var hand []string
	if self {
		// the card is the discarded one, in its controller's hand
		hand = []string{card}
	} else {
		filter := trig.Params["ValidCard"]
		if filter == "" {
			filter = "Card"
		}
		name, s := landCarrier(reg, filter, generic)
		if name == "" {
			name, s = filterCarrier(reg, b, card, filter, func(*cards.Face) bool { return true })
		}
		if name == "" {
			return none + ":filter"
		}
		victim, hand = s, []string{name}
		if vp := trig.Params["ValidPlayer"]; vp != "" {
			s, _, ok := playerRef(vp)
			if !ok {
				return none + ":player"
			}
			victim = max(s, 0)
		}
		f.Setup.P0.Battlefield = []string{card}
	}
	if present := trig.Params["IsPresent"]; present != "" {
		name, s := landCarrier(reg, present, generic)
		if name == "" {
			name, s = filterCarrier(reg, b, card, present, (*cards.Face).IsPermanent)
		}
		if name == "" {
			return none + ":present"
		}
		seats[s].Battlefield = append(seats[s].Battlefield, name)
	}
	seats[victim].Hand = append(seats[victim].Hand, hand...)
	caster := 1 - victim
	if w := turnWanted(trig); w >= 0 && w != caster {
		return none + ":turn" // a sorcery: the caster's own turn
	}
	castCarrier(reg, seats[caster], eventFixtures.Discard)
	act(f, caster, false, eventFixtures.Discard, "cast", 0)
	return ""
}

// sacrificeStart is "whenever you sacrifice a permanent": the card's own
// sacrifice ability when the filter takes the card itself, else a sacrifice
// carrier (sacCarrier) the filter takes, on the seat the filter or the
// trigger's player names, sacrificing itself.
func sacrificeStart(reg *cards.Registry, b *certdeck.Builder, face *cards.Face, card, generic string, trig *cards.Trigger, f *StartFile) string {
	none := "no-generator:trigger:Sacrificed"
	if why := eventParamsOK(trig, "Sacrificed", "ValidCard", "ValidPlayer", "PlayerTurn"); why != "" {
		return why
	}
	filter := trig.Params["ValidCard"]
	seat := -1
	if vp := trig.Params["ValidPlayer"]; vp != "" {
		s, _, ok := playerRef(vp)
		if !ok {
			return none + ":player"
		}
		seat = s
	}
	if w := turnWanted(trig); w == 1 {
		return none + ":turn"
	}
	if selfCard(trig) && face.IsPermanent() && seat <= 0 {
		if i := selfAbility(face, func(sa *cards.SA) bool { return costHas(sa, "Sac<1/CARDNAME>") }); i >= 0 {
			return abilityFocus(reg, b, face, card, generic, i, face.Abilities[i], f)
		}
	}
	name, s := sacCarrier(reg, b, card, filter)
	if name == "" {
		return none + ":filter"
	}
	if seat >= 0 {
		s = seat
	}
	f.Setup.P0.Battlefield = []string{card}
	seats := seatsOf(f)
	seats[s].Battlefield = append(seats[s].Battlefield, name)
	seats[s].Battlefield = append(seats[s].Battlefield, sacLands(reg, name)...)
	act(f, s, false, name, "ability", 0)
	return ""
}

// tapPools holds each registry's tap carriers, built once.
var tapPools sync.Map // *cards.Registry -> []*cards.Face

// tapPool is every permanent whose one ability is a bare {T} ability with no
// target and no choice (a mana ability, or a fixed gain or draw), inert
// otherwise: the carriers a start taps for "whenever a <permanent> becomes
// tapped". Ranked: mana abilities, then fewest keywords, then by mana value,
// then name.
func tapPool(reg *cards.Registry, b *certdeck.Builder) []*cards.Face {
	if v, ok := tapPools.Load(reg); ok {
		return v.([]*cards.Face)
	}
	var out []*cards.Face
	for _, c := range reg.AllCards() {
		if len(c.Faces) != 1 || !b.Playable(c) {
			continue
		}
		f := c.Faces[0]
		if !f.IsPermanent() || len(f.Abilities) != 1 || len(f.Triggers)+len(f.Statics)+len(f.Repls) > 0 || !evergreenOnly(f) ||
			deadOnArrival(f) || FieldBlock(f) != "" || f.HasKeyword("Vigilance") {
			continue
		}
		sa := f.Abilities[0]
		if sa.Sub != nil || sa.Params["ValidTgts"] != "" || sa.Params["Defined"] != "" && sa.Params["Defined"] != "You" ||
			!slices.Equal(splitCost(sa.Params["Cost"]), []string{"T"}) || sa.Params["SorcerySpeed"] != "" || sa.Params["ActivationLimit"] != "" {
			continue
		}
		if !cards.IsManaAbilitySA(sa) && sa.API != "GainLife" && sa.API != "Draw" {
			continue
		}
		if cards.IsManaAbilitySA(sa) && (sa.Params["Produced"] == "" || strings.Contains(sa.Params["Produced"], "Combo") || sa.Params["Amount"] != "" && sa.Params["Amount"] != "1") {
			continue
		}
		out = append(out, f)
	}
	rank := func(f *cards.Face) int {
		if cards.IsManaAbilitySA(f.Abilities[0]) {
			return 0
		}
		return 1
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, c := out[i], out[j]
		if ra, rc := rank(a), rank(c); ra != rc {
			return ra < rc
		}
		if ka, kc := len(a.Keywords), len(c.Keywords); ka != kc {
			return ka < kc
		}
		if a.Cmc() != c.Cmc() {
			return a.Cmc() < c.Cmc()
		}
		if a.IsLegendary() != c.IsLegendary() {
			return !a.IsLegendary()
		}
		return a.Name < c.Name
	})
	tapPools.Store(reg, out)
	return out
}

// basicLands are the basics a land carrier is drawn from, each with the one
// mana ability a basic land has.
var basicLands = []string{"Forest", "Plains", "Island", "Swamp", "Mountain"}

// tapCarrier is the permanent whose tap the filter takes: for each
// understood alternative, a basic land (a land filter), a tap carrier
// (tapPool), or, for a plain tap, a creature that attacks. It returns the card, the
// seat that controls it and the index of the ability to activate (-1 to
// attack with it).
func tapCarrier(reg *cards.Registry, b *certdeck.Builder, card, filter string, mana bool) (name string, seat, ability int) {
	for _, a := range parseFilter(filter) {
		if !a.known || a.self {
			continue
		}
		s := 0
		if a.ctrl == ctrlOpp {
			s = 1
		}
		for _, basic := range basicLands {
			if c, ok := reg.Lookup(basic); ok && len(c.Faces) > 0 && a.test(c.Faces[0]) && selfAbility(c.Faces[0], cards.IsManaAbilitySA) == 0 {
				return basic, s, 0
			}
		}
		for _, f := range tapPool(reg, b) {
			if f.Name != card && a.test(f) && (!mana || cards.IsManaAbilitySA(f.Abilities[0])) {
				return f.Name, s, 0
			}
		}
		if mana {
			continue
		}
		if n := pickCarrier(reg, b, card, func(f *cards.Face) bool {
			return f.IsCreature() && !f.HasKeyword("Vigilance") && !f.HasKeyword("Defender") && !deadOnArrival(f) && f.IsPermanent() && a.test(f)
		}); n != "" {
			return n, s, -1
		}
	}
	return "", 0, 0
}

// ownTap is how the card taps itself, a permanent with a {T} ability (a mana
// ability for mana) or a creature that attacks without vigilance: the index
// of the ability, or -1 to attack; ok is false when it cannot.
func ownTap(face *cards.Face, mana bool) (ability int, ok bool) {
	if !face.IsPermanent() {
		return 0, false
	}
	pred := func(sa *cards.SA) bool { return costHas(sa, "T") }
	if mana {
		pred = func(sa *cards.SA) bool { return cards.IsManaAbilitySA(sa) && costHas(sa, "T") }
	}
	if i := selfAbility(face, pred); i >= 0 {
		return i, true
	}
	if !mana && face.IsCreature() && !face.HasKeyword("Vigilance") && !face.HasKeyword("Defender") {
		return -1, true
	}
	return 0, false
}

// tapStart is "whenever <a permanent> becomes tapped" (Taps) and "whenever
// you tap <a permanent> for mana" (TapsForMana): the card taps itself (ownTap)
// when the filter takes it, else the tap carrier the filter takes (tapCarrier)
// taps, on the seat the filter names.
func tapStart(reg *cards.Registry, b *certdeck.Builder, face *cards.Face, card, generic string, trig *cards.Trigger, f *StartFile) string {
	mode := trig.Mode
	none := "no-generator:trigger:" + mode
	if why := eventParamsOK(trig, mode, "ValidCard", "Activator"); why != "" {
		return why
	}
	if who := trig.Params["Activator"]; who != "" {
		if s, _, ok := playerRef(who); !ok || s == 1 {
			return none + ":player"
		}
	}
	mana := mode == "TapsForMana"
	filter := trig.Params["ValidCard"]
	takesCard := false // the filter takes the card itself
	for _, a := range parseFilter(filter) {
		takesCard = takesCard || a.self || a.known && !a.other && a.ctrl != ctrlOpp && a.test(face)
	}
	if takesCard {
		if i, ok := ownTap(face, mana); ok {
			if i < 0 {
				f.Setup.P0.Battlefield = []string{card}
				f.Focus = &StartFocus{Seat: seatPtr(0), Card: card, Kind: "attack"}
				return ""
			}
			return abilityFocus(reg, b, face, card, generic, i, face.Abilities[i], f)
		}
	}
	name, s, ab := tapCarrier(reg, b, card, filter, mana)
	if name == "" {
		return none + ":filter"
	}
	f.Setup.P0.Battlefield = []string{card}
	seats := seatsOf(f)
	seats[s].Battlefield = append(seats[s].Battlefield, name)
	if ab < 0 {
		f.Active = s
		f.Focus = &StartFocus{Seat: seatPtr(s), Card: name, Kind: "attack"}
		return ""
	}
	act(f, s, false, name, "ability", ab)
	return ""
}

// untapStart is "whenever <a permanent> becomes untapped" (Untaps): the card
// itself, placed tapped, untaps in its controller's untap step, the first
// the game reaches: the other seat's turn 1, then this seat's turn 2. The
// start has no focus play: the untap step is the event.
func untapStart(reg *cards.Registry, b *certdeck.Builder, face *cards.Face, card string, trig *cards.Trigger, f *StartFile) string {
	none := "no-generator:trigger:Untaps"
	if why := eventParamsOK(trig, "Untaps", "ValidCard"); why != "" {
		return why
	}
	if !face.IsPermanent() {
		return none + ":noncreature"
	}
	filter := trig.Params["ValidCard"]
	ok := filter == "Card.Self"
	for _, a := range parseFilter(filter) {
		ok = ok || a.known && !a.other && !a.self && a.ctrl != ctrlOpp && a.test(face)
	}
	if !ok {
		return none + ":filter"
	}
	f.Setup.P0.Battlefield = []string{card}
	f.Setup.P0.Tapped = []string{card}
	f.Active, f.MaxTurns = 1, 2
	return ""
}

// crimeStart is "whenever you commit a crime" (CommitCrime): a crime is
// targeting an opponent, a permanent they control or a card in their
// graveyard (CR 700.13). The player the trigger names casts the crime
// fixture, a spell whose only legal target is an opponent, so the one target
// the bot can choose is the crime.
func crimeStart(reg *cards.Registry, b *certdeck.Builder, face *cards.Face, card string, trig *cards.Trigger, f *StartFile) string {
	none := "no-generator:trigger:CommitCrime"
	if why := eventParamsOK(trig, "CommitCrime", "ValidPlayer"); why != "" {
		return why
	}
	seat, _, ok := playerRef(trig.Params["ValidPlayer"])
	if !ok {
		return none + ":player"
	}
	seat = max(seat, 0)
	if !spell(reg, b, eventFixtures.Crime) {
		return none + ":fixture"
	}
	seats := seatsOf(f)
	f.Setup.P0.Battlefield = []string{card}
	castCarrier(reg, seats[seat], eventFixtures.Crime)
	act(f, seat, false, eventFixtures.Crime, "cast", 0)
	return ""
}
