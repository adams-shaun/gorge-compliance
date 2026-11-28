package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclediff"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/cov"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/dside"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/lower"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/pincap"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/scen"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
)

// startFormat is the format tag of a start file.
const startFormat = "xproof-start-1"

// reasonStartNotExercised is the refusal of a start whose focus seat was never
// offered the focus play.
const reasonStartNotExercised = "start-not-exercised"

// reasonStartFaceLost is the refusal of a start that places a card on its back
// face whose lowered setup no longer does: the card turned to its front before
// the recorded span (an upkeep "transform this"), so the tape plays the front
// face and must not credit the back-face key.
const reasonStartFaceLost = "start-face-lost"

// reasonStartPlayerCountersPin is the refusal of a start whose seat holds
// player counters on a pin whose rules.StartSeat has no PlayerCounters field.
// The file is refused per id (its other starts in the directory still play);
// it clears when the gorge pin bump lands.
const reasonStartPlayerCountersPin = "start-player-counters-pin"

// errPlayerCountersPin marks the start that needs a field the pinned gorge
// does not have, so playStart can refuse its ids instead of aborting the batch.
var errPlayerCountersPin = errors.New("player counters need rules.StartSeat.PlayerCounters, which the pinned gorge does not have yet (blocked on the gorge pin bump)")

// reasonStartRoomLocked is the refusal of a start that places a Room on the
// battlefield and focuses a door's ability: a Room that enters without being
// cast as a door gets neither unlocked designation (CR 709.5d), so it has no
// name and none of that door's rules text (CR 709.5). The CR 709.5e unlock
// special action is an explicit step that no scenario op drives through all
// three runners, so the start would exercise nothing of the key.
const reasonStartRoomLocked = "start-room-locked"

// startDeckSize is the size a seat's deck is filled to with basics.
const startDeckSize = 40

// startFile is one xproof start: a scripted position (gorge's
// rules.Config.Start) for one card's one key, and how many games to play on
// from it.
type startFile struct {
	Format   string `json:"format"`
	Card     string `json:"card"`
	Key      string `json:"key"`
	Players  int    `json:"players"`
	Active   int    `json:"active"`
	MaxTurns int    `json:"max_turns"`
	Setup    struct {
		P0 startSeat `json:"p0"`
		P1 startSeat `json:"p1"`
	} `json:"setup"`
	Filler string `json:"filler"`
	Seeds  int    `json:"seeds"`
	// DayNight is the designation the start has at turn 1's first-main cut
	// ("day" or "night", empty when neither), as scen.StartFile declares it.
	// The runner only validates the spelling: genesis and the turn-1 untap
	// check re-derive the designation from the placements (a gated Moved
	// DayTime entry makes it day, then CR 731.2a makes it night; a daybound
	// permanent is read by the state-based check after the untap check), and
	// the lowered item's replay does the same.
	DayNight string `json:"day_night"`
	// SeedBase is where the file's seed search starts (0 or absent: 1), so a
	// key retried in a later cycle plays new games, not the same Seeds again.
	SeedBase uint64 `json:"seed_base"`
	// Prelude is the card PreludeSeat casts from hand at turn 1's first main
	// priority, before the focus play (see scen.StartFile): the key card of an
	// as-enters start is not placed at genesis, so its entry, choice and turn
	// stamps are tape events.
	Prelude string `json:"prelude"`
	// PreludeSeat is the seat that casts Prelude (0 when unset).
	PreludeSeat int         `json:"prelude_seat"`
	Focus       *startFocus `json:"focus"`
	// Compare is the item's opt-in comparison fields (oraclediff.Compare*),
	// carried onto the lowered item: a start whose key is a keyword grant is
	// observable only with "keywords" (null: the item compares nothing extra).
	Compare []string `json:"compare"`
}

// startSeat is one seat's position, as card names.
type startSeat struct {
	Hand        []string `json:"hand"`
	Battlefield []string `json:"battlefield"`
	Graveyard   []string `json:"graveyard"`
	Exile       []string `json:"exile"`
	Library     []string `json:"library"`
	Tapped      []string `json:"tapped"`
	// Commanders names the cards the seat designates as its commanders
	// (scen.StartSeat.Commanders, CR 903.3): the named cards sit in Hand or
	// Battlefield as any other card of the file, so the decks need no change;
	// the runner only passes the designation through game.Options
	// StartCommanders for the Setup hook to record in
	// Game.Players[seat].Commanders, the list the engine reads for
	// Card.IsCommander (Game.CommanderOf).
	Commanders []string `json:"commanders"`
	// BackFace names battlefield cards that enter on their back face.
	BackFace []string       `json:"back_face"`
	Counters []startCounter `json:"counters"`
	// PlayerCounters seed counters on the seat itself, not a permanent
	// (startCounter.Card empty): the energy a PayEnergy<N> cost spends. They
	// ride rules.StartSeat.PlayerCounters when the pinned gorge has that field;
	// a pin without it rejects a start that names them (there is no fallback
	// seed).
	PlayerCounters []startCounter `json:"player_counters"`
	Attach         []startAttach  `json:"attach"`
	Tokens         []startToken   `json:"tokens"`
	Life           *int32         `json:"life"`
}

type startAttach struct {
	Card string `json:"card"`
	To   string `json:"to"`
	// Opp: the bearer is on the other seat's battlefield (an Aura whose Enchant
	// keyword is "creature an opponent controls").
	Opp bool `json:"opp"`
}

type startCounter struct {
	// Card names the battlefield card. Empty means the seat itself
	// (PlayerCounters), so that JSON omits the field.
	Card string `json:"card,omitempty"`
	Kind string `json:"kind"`
	N    int32  `json:"n"`
}

// startToken is a token the file mints onto the seat's battlefield, by its
// token definition script (gorge's Config.Tokens key). It names no deck card.
type startToken struct {
	Script string `json:"script"`
	Tapped bool   `json:"tapped"`
}

// startFocus overrides the defaults of the focus seat (focusOf).
type startFocus struct {
	Seat    *int   `json:"seat"`
	Card    string `json:"card"`
	Kind    string `json:"kind"`
	Ability int    `json:"ability"`
	Mode    string `json:"mode"`
	Times   int    `json:"times"`
	// Bait is the spell the focus seat's opponent casts before the focus play
	// (game.Focus.Bait).
	Bait string `json:"bait"`
	// BaitSelf is the spell the focus seat itself casts (and holds priority
	// on) before its play (game.Focus.BaitSelf); BaitAbility the permanent
	// whose ability the bait seat activates (game.Focus.BaitAbility);
	// BaitTarget the permanent the bait spell targets (game.Focus.BaitTarget).
	BaitSelf    string `json:"baitSelf"`
	BaitAbility string `json:"baitAbility"`
	// BaitAbilitySelf is whether the focus seat itself activates the
	// BaitAbility permanent (it sits on its own battlefield).
	BaitAbilitySelf bool   `json:"baitAbilitySelf,omitempty"`
	BaitTarget      string `json:"baitTarget"`
	// BaitTargetPlayer is whether the bait spell targets the focus seat
	// itself (game.Focus.BaitTargetPlayer).
	BaitTargetPlayer bool `json:"baitTargetPlayer,omitempty"`
	// Targets are the permanents the focus play must target, in the order the
	// play asks for them (game.Focus.Targets).
	Targets []string `json:"targets"`
	// Attack and Block are the scripted combat the focus play waits for
	// (game.Focus.Attack, Block).
	Attack string `json:"attack"`
	Block  string `json:"block"`
}

// startFlagErr is the error of -start given with a flag it does not combine
// with, or without -set.
//
// The dirac parameter is the flag it does not refuse: dirac follows gorge
// (operator 11-24), so it needs nothing of the deal -start replaces and answers each
// start file's scripted position beside gorge (see playStartDirac). The flags named
// below each pick another way to build the game, which a scripted position leaves no
// room for; the caller hands every one of them here, dirac included, so the list of
// refusals is one list.
func startFlagErr(set, random, pool, decksDir string, dirac, windows bool, tapeTurns, format, forgeOnly string) error {
	if set == "" {
		return errors.New("-start needs -set (it names the item ids)")
	}
	for _, f := range []struct {
		name string
		on   bool
	}{
		{"-random", random != ""}, {"-pool", pool != ""}, {"-decks", decksDir != ""},
		{"-windows", windows}, {"-tape-turns", tapeTurns != ""}, {"-format", format != ""}, {"-forge-only", forgeOnly != ""},
	} {
		if f.on {
			return fmt.Errorf("-start plays scripted positions: it cannot be combined with %s", f.name)
		}
	}
	return nil
}

// startFiles is the start files of paths: a file as it is, a directory as its
// *.json files in sorted order.
func startFiles(paths []string) ([]string, error) {
	var out []string
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil {
			return nil, err
		}
		if !st.IsDir() {
			out = append(out, p)
			continue
		}
		m, err := filepath.Glob(filepath.Join(p, "*.json"))
		if err != nil {
			return nil, err
		}
		sort.Strings(m)
		out = append(out, m...)
	}
	if len(out) == 0 {
		return nil, errors.New("-start: no start file")
	}
	return out, nil
}

func readStartFile(path string) (*startFile, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var f startFile
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	switch {
	case f.Format != startFormat:
		return nil, fmt.Errorf("%s: format %q, want %q", path, f.Format, startFormat)
	case f.Card == "" || f.Key == "":
		return nil, fmt.Errorf("%s: card and key are required", path)
	case f.Players != 2:
		return nil, fmt.Errorf("%s: players: only 2", path)
	case f.Active != 0 && f.Active != 1:
		return nil, fmt.Errorf("%s: active %d, want 0 or 1", path, f.Active)
	case f.MaxTurns < 0:
		return nil, fmt.Errorf("%s: max_turns %d", path, f.MaxTurns)
	case f.Seeds < 1:
		return nil, fmt.Errorf("%s: seeds %d, want >= 1", path, f.Seeds)
	case f.Filler != "basics":
		return nil, fmt.Errorf("%s: filler %q, want \"basics\"", path, f.Filler)
	case f.DayNight != "" && f.DayNight != scen.DayNightDay && f.DayNight != scen.DayNightNight:
		return nil, fmt.Errorf("%s: day_night %q, want %q, %q or empty", path, f.DayNight, scen.DayNightDay, scen.DayNightNight)
	}
	for _, s := range []startSeat{f.Setup.P0, f.Setup.P1} {
		for _, c := range s.PlayerCounters {
			if c.Card != "" {
				return nil, fmt.Errorf("%s: player counter %q names card %q; a player counter goes on the seat", path, c.Kind, c.Card)
			}
			if err := playerCounterKindError(c.Kind); err != nil {
				return nil, fmt.Errorf("%s: %w", path, err)
			}
			if c.N < 1 {
				return nil, fmt.Errorf("%s: player counter %q n %d, want >= 1", path, c.Kind, c.N)
			}
		}
	}
	if err := oraclediff.ValidateCompare(f.Compare); err != nil {
		return nil, fmt.Errorf("%s: compare: %w", path, err)
	}
	return &f, nil
}

// gameSeeds is the seed of each of the file's Seeds games. Game s's is the
// first seed after game s-1's that names the active seat as the starter
// (rules.SeedForStarter), the search starting at seed_base (absent: 1, the
// brief's), so no two of a file's games share a seed, which
// SeedForStarter(s, ...) alone would give them (seeds 1 and 2 both resolve to
// 2 for seat 0).
func (f *startFile) gameSeeds() []uint64 {
	next := f.SeedBase
	if next == 0 {
		next = 1
	}
	out := make([]uint64, 0, f.Seeds)
	for range f.Seeds {
		seed := rules.SeedForStarter(next, 2, f.Active)
		out = append(out, seed)
		next = seed + 1
	}
	return out
}

var keyFA = regexp.MustCompile(`^f(\d+)/a(\d+)$`)

// focusOf is the focus the file names, or its defaults: the first seat listing
// the card, the card, and the action its key names.
func (f *startFile) focusOf() (game.Focus, error) {
	var out game.Focus
	seats := [2]startSeat{f.Setup.P0, f.Setup.P1}
	out.Seat = -1
	for i, s := range seats {
		if out.Seat < 0 && s.lists(f.Card) {
			out.Seat = i
		}
	}
	out.Card = f.Card
	switch m := keyFA.FindStringSubmatch(f.Key); {
	case m != nil:
		out.Kind = "ability"
		out.Ability, _ = strconv.Atoi(m[2])
	case f.Key == "cast":
		out.Kind = "cast"
	case f.Key == "land":
		out.Kind = "land"
	}
	if o := f.Focus; o != nil {
		if o.Seat != nil {
			out.Seat = *o.Seat
		}
		if o.Card != "" {
			out.Card = o.Card
		}
		if o.Kind != "" {
			out.Kind, out.Ability, out.Mode, out.Times = o.Kind, o.Ability, o.Mode, o.Times
		}
		out.Attack, out.Block = o.Attack, o.Block
		out.Bait, out.BaitSelf, out.BaitAbility, out.BaitTarget =
			o.Bait, o.BaitSelf, o.BaitAbility, o.BaitTarget
		out.BaitAbilitySelf = o.BaitAbilitySelf
		out.BaitTargetPlayer = o.BaitTargetPlayer
		out.Targets = o.Targets
	}
	out.Prelude, out.PreludeSeat = f.Prelude, f.PreludeSeat
	if out.Prelude != "" {
		if out.PreludeSeat < 0 || out.PreludeSeat > 1 {
			return out, fmt.Errorf("prelude seat %d: no seat casts %q", out.PreludeSeat, out.Prelude)
		}
		if !slices.Contains(seats[out.PreludeSeat].Hand, out.Prelude) {
			return out, fmt.Errorf("prelude %q: not in seat %d's hand", out.Prelude, out.PreludeSeat)
		}
	}
	if out.Seat < 0 || out.Seat > 1 {
		return out, fmt.Errorf("focus seat %d: no seat lists %q", out.Seat, f.Card)
	}
	if out.BaitTargetPlayer && out.Bait == "" {
		// the flag says the bait spell's own target is the focus player; with
		// no bait spell there is nothing to target, and the focus bot would
		// answer an unrelated target ask with the focus seat
		return out, fmt.Errorf("focus baitTargetPlayer needs a bait spell")
	}
	return out, nil
}

// lists reports whether any zone of the seat names card.
func (s startSeat) lists(card string) bool {
	for _, z := range [][]string{s.Hand, s.Battlefield, s.Graveyard, s.Exile, s.Library} {
		for _, n := range z {
			if n == card {
				return true
			}
		}
	}
	return false
}

// startDeck is a seat's deck and its scripted position: the listed cards in
// the order hand, battlefield, graveyard, exile, library, then basics to 40.
func startDeck(reg *cards.Registry, name string, s startSeat) (decks.Deck, rules.StartSeat, error) {
	return startDeckAt(reg, name, 0, s, startSeat{})
}

// startDeckAt is startDeck for one seat index: an Attach entry binds the Aura
// to a bearer on this same seat, or (Opp) on other, the opposing seat's
// position, whose deck lists hand then battlefield first (rules.StartAttach).
func startDeckAt(reg *cards.Registry, name string, seat int, s, other startSeat) (decks.Deck, rules.StartSeat, error) {
	var d decks.Deck
	var ss rules.StartSeat
	d.Name = name
	var bf []int
	var frontCost []string
	attached := map[string]bool{}
	for _, a := range s.Attach {
		attached[a.Card] = true
	}
	add := func(names []string, into *[]int, bfZone bool) error {
		backs := map[string]int{} // BackFace names left to place, by name
		for _, n := range s.BackFace {
			backs[n]++
		}
		for _, n := range names {
			c, ok := reg.Lookup(n)
			if !ok || len(c.Faces) == 0 {
				return fmt.Errorf("start %s: card %q not found", name, n)
			}
			if bfZone {
				// gorge and Forge place such a card differently (scen.FieldBlock),
				// so the setup could never agree: refuse the file. An attached Aura
				// enters with its bearer, so it is the one exception.
				face := c.Faces[0]
				if backs[n] > 0 && len(c.Faces) > 1 {
					face = c.Faces[1] // it enters transformed: the back face is the permanent
					backs[n]--
				}
				if why := scen.FieldBlock(face); why != "" &&
					!(why == "aura-unattached" && attached[n]) {
					return fmt.Errorf("start %s: %q cannot be listed on the battlefield (%s): list it in hand", name, n, why)
				}
			}
			*into = append(*into, len(d.Cards))
			if bfZone {
				bf = append(bf, len(d.Cards))
			}
			// the front face: a start naming a double-faced card by its back face
			// places it front-up unless the seat's BackFace lists it (bound below)
			d.Cards = append(d.Cards, c.Faces[0].Name)
			frontCost = append(frontCost, c.Faces[0].ManaCost)
		}
		return nil
	}
	for _, z := range []struct {
		names []string
		into  *[]int
		bf    bool
	}{{s.Hand, &ss.Hand, false}, {s.Battlefield, &ss.Battlefield, true}, {s.Graveyard, &ss.Graveyard, false}, {s.Exile, &ss.Exile, false}, {s.Library, &ss.Library, false}} {
		if err := add(z.names, z.into, z.bf); err != nil {
			return d, ss, err
		}
	}
	bind := func(card string, used map[int]bool) (int, error) {
		// d.Cards holds front-face names, so resolve the listed name the same
		// way add did: a double-faced card is listed as "A // B" or by its back.
		want := card
		if c, ok := reg.Lookup(card); ok && len(c.Faces) > 0 {
			want = c.Faces[0].Name
		}
		for _, i := range bf {
			if d.Cards[i] == want && !used[i] {
				used[i] = true
				return i, nil
			}
		}
		return 0, fmt.Errorf("start %s: no battlefield copy of %q left to bind", name, card)
	}
	tapped, counted := map[int]bool{}, map[int]bool{}
	for _, n := range s.Tapped {
		i, err := bind(n, tapped)
		if err != nil {
			return d, ss, err
		}
		ss.Tapped = append(ss.Tapped, i)
	}
	for _, c := range s.Counters {
		if err := startCounterKindError(c.Kind); err != nil {
			return d, ss, fmt.Errorf("start %s: %w", name, err)
		}
		i, err := bind(c.Card, counted)
		if err != nil {
			return d, ss, err
		}
		ss.Counters = append(ss.Counters, rules.StartCounter{Card: i, Kind: c.Kind, N: c.N})
	}
	// gorge applies counters in the order given: the caller sorts by (Card, Kind).
	sort.SliceStable(ss.Counters, func(a, b int) bool {
		x, y := ss.Counters[a], ss.Counters[b]
		return x.Card < y.Card || x.Card == y.Card && x.Kind < y.Kind
	})
	auraCards, auraBearers := map[int]bool{}, map[int]bool{}
	for _, a := range s.Attach {
		bearers := s.Battlefield
		if a.Opp {
			bearers = other.Battlefield
		}
		if !slices.Contains(s.Battlefield, a.Card) || !slices.Contains(bearers, a.To) {
			return d, ss, fmt.Errorf("start %s: attach %q to %q: both must be on the battlefield", name, a.Card, a.To)
		}
		card, err := bind(a.Card, auraCards)
		if err != nil {
			return d, ss, err
		}
		if a.Opp {
			// The other deck is built from the same listing order (hand, then
			// battlefield): its first battlefield copy is the bearer.
			ss.Attach = append(ss.Attach, rules.StartAttach{Card: card, ToSeat: 1 - seat,
				To: len(other.Hand) + slices.Index(other.Battlefield, a.To)})
			continue
		}
		bearer, err := bind(a.To, auraBearers)
		if err != nil {
			return d, ss, err
		}
		ss.Attach = append(ss.Attach, rules.StartAttach{Card: card, ToSeat: seat, To: bearer})
	}
	backed := map[int]bool{}
	for _, n := range s.BackFace {
		i, err := bind(n, backed)
		if err != nil {
			return d, ss, err
		}
		if c, _ := reg.Lookup(n); c == nil || len(c.Faces) < 2 {
			return d, ss, fmt.Errorf("start %s: %q has no back face", name, n)
		}
		ss.BackFace = append(ss.BackFace, i)
	}
	for _, t := range s.Tokens {
		// An unknown script mints nothing (gorge skips it), so refuse the file
		// rather than silently start without the token the key needs.
		if t.Script == "" {
			return d, ss, fmt.Errorf("start %s: token with no script", name)
		}
		if _, ok := reg.Token(t.Script); !ok {
			return d, ss, fmt.Errorf("start %s: token script %q not found", name, t.Script)
		}
		ss.Tokens = append(ss.Tokens, rules.StartToken{Script: t.Script, Tapped: t.Tapped})
	}
	ss.Life = s.Life
	// Player counters ride rules.StartSeat.PlayerCounters. A pin without the
	// field is an explicit error, not a silent drop: the focus ability would
	// otherwise run unaffordable and be refused as start-not-exercised far from
	// the cause.
	cs, err := playerCountersOf(s.PlayerCounters)
	if err != nil {
		return d, ss, fmt.Errorf("start %s: %w", name, err)
	}
	if err := applyStartPlayerCounters(&ss, cs); err != nil {
		return d, ss, fmt.Errorf("start %s: %w", name, err)
	}
	// Filler: the colours of the listed cards' front-face costs, WUBRG order,
	// one basic each round robin; Plains when there is no coloured symbol.
	var colours string
	for _, l := range "WUBRG" {
		for _, cost := range frontCost {
			if hasShardLetter(cost, byte(l)) {
				colours += string(l)
				break
			}
		}
	}
	if colours == "" {
		colours = "W"
	}
	basic := map[byte]string{'W': "Plains", 'U': "Island", 'B': "Swamp", 'R': "Mountain", 'G': "Forest"}
	for i := 0; len(d.Cards) < startDeckSize; i++ {
		d.Cards = append(d.Cards, basic[colours[i%len(colours)]])
	}
	d.Colours = colours
	return d, ss, nil
}

// startCounterKindError is why kind cannot be seeded, or nil: a wildcard
// ("All", "Any") or a Forge placeholder ("EachFromSource", "ExistingCounter")
// names kinds rather than one, and the followers cannot apply it (Honest
// Work's CounterType$ All was seeded literally before certdeck normalised it).
func startCounterKindError(kind string) error {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "", "all", "any", "eachfromsource", "existingcounter":
		return fmt.Errorf("counter kind %q is not a counter kind", kind)
	}
	return nil
}

// playerCounterKindError is why kind is not a player counter this repo seeds,
// or nil. Only the kinds the engines track ride the field: ENERGY (CR 107.14),
// POISON and EXPERIENCE. A cost can only spend the first today, but a start
// that needs the other two is still expressible instead of silently dropped.
func playerCounterKindError(kind string) error {
	switch strings.ToUpper(strings.TrimSpace(kind)) {
	case "ENERGY", "POISON", "EXPERIENCE":
		return nil
	}
	return fmt.Errorf("player counter kind %q is not ENERGY, POISON or EXPERIENCE", kind)
}

// playerCountersOf validates a seat's player counters and folds them into the
// shared, sorted pincap form. A player counter names no card: an entry with a
// card would place the counters on a permanent that a PayEnergy cost never
// reads.
func playerCountersOf(cs []startCounter) ([]pincap.Counter, error) {
	out := make([]pincap.Counter, 0, len(cs))
	for _, c := range cs {
		if c.Card != "" {
			return nil, fmt.Errorf("player counter %q names card %q; a player counter goes on the seat", c.Kind, c.Card)
		}
		if err := playerCounterKindError(c.Kind); err != nil {
			return nil, err
		}
		if c.N < 1 {
			return nil, fmt.Errorf("player counter %q n %d, want >= 1", c.Kind, c.N)
		}
		out = append(out, pincap.Counter{Kind: strings.ToUpper(strings.TrimSpace(c.Kind)), N: c.N})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Kind < out[j].Kind })
	// Fold a repeat of one kind, so the cost's total is one entry and every
	// follower applies it once.
	folded := out[:0]
	for _, c := range out {
		if n := len(folded); n > 0 && folded[n-1].Kind == c.Kind {
			folded[n-1].N += c.N
			continue
		}
		folded = append(folded, c)
	}
	return folded, nil
}

// setStartPlayerCounters is pincap.SetStartPlayerCounters, a var so a test can
// prove a start's counters reach the pinned field even while the pin has none.
var setStartPlayerCounters = pincap.SetStartPlayerCounters

// applyStartPlayerCounters writes cs into ss.PlayerCounters through the pin
// capability. A start that names counters on a pin without the field is an
// explicit error: the alternative is a game with no energy, refused much later
// as start-not-exercised, which hides the missing gorge feature.
func applyStartPlayerCounters(ss *rules.StartSeat, cs []pincap.Counter) error {
	if len(cs) == 0 {
		return nil
	}
	if !setStartPlayerCounters(ss, cs) {
		return errPlayerCountersPin
	}
	return nil
}

// hasShardLetter reports whether the Forge mana cost has a shard with the
// colour letter l (a hybrid shard "W/U" has both).
func hasShardLetter(cost string, l byte) bool {
	for _, shard := range strings.Fields(cost) {
		for _, part := range strings.Split(shard, "/") {
			if len(part) == 1 && part[0] == l {
				return true
			}
		}
	}
	return false
}

// spanFile is the tape file of a span: its self-check tape, cut where the
// lowering stopped when it did, else whole. (windowFile keeps turn 1 only.)
func spanFile(l lower.Lowered) *game.Tape {
	n := len(l.Against.Entries)
	if l.Truncated != "" {
		n = min(n, l.TruncatedAt)
	}
	t := *l.Against
	t.Entries = l.Against.Entries[:n:n]
	return &t
}

// backFaceLost is the first card the start file places on its back face
// (either seat's back_face) that the lowered item's setup no longer lists on a
// back face, or "". The lowering relabels the seats (the active one is p0), so
// the item's two seats are searched together; it names a card by its front face.
func backFaceLost(reg *cards.Registry, f *startFile, it oraclegen.Item) string {
	var kept []string
	for _, sd := range it.Setup {
		kept = append(kept, sd.BackFace...)
	}
	for _, sd := range []startSeat{f.Setup.P0, f.Setup.P1} {
		for _, n := range sd.BackFace {
			if !slices.Contains(kept, canonicalCard(reg, n)) {
				return n
			}
		}
	}
	return ""
}

// roomDoorLocked reports the Room, by the start's spelling, that setup places
// on a battlefield while the start's focus is a door's ability, or "". A Room
// placed by setup was not cast as a door, so it enters with neither unlocked
// designation (CR 709.5d) and none of either door's rules text is live
// (CR 709.5); the key's static, trigger or activation would never be
// exercised. A "cast" focus is the one shape that unlocks the door it casts
// (CR 709.5d), so it stays a valid start. The test is role-based (the card's
// Room type), not a list of key spellings: every key family of a setup Room
// is covered, and a new one cannot slip through.
func roomDoorLocked(reg *cards.Registry, f *startFile, focus game.Focus) string {
	if focus.Kind == "cast" {
		return ""
	}
	c, ok := reg.Lookup(focus.Card)
	if !ok || len(c.Faces) == 0 || !c.Faces[0].IsRoom() {
		return ""
	}
	want := canonicalCard(reg, focus.Card)
	for _, sd := range []startSeat{f.Setup.P0, f.Setup.P1} {
		for _, n := range sd.Battlefield {
			if canonicalCard(reg, n) == want {
				return focus.Card
			}
		}
	}
	return ""
}

// playStart plays every start file of paths (see startFiles) and writes the
// items, gorge rows, lowering log, exercised ledger and tapes of the games into
// out, as play does. set names the item ids; xmage says XMage replays them.
func playStart(reg *cards.Registry, paths []string, set, out string, xmage bool) error {
	return playStartDirac(reg, paths, set, out, xmage, nil)
}

// playStartDirac is playStart with dirac beside gorge: tables, when non-nil, is the
// dirac cardset server of the -dirac run, and each start file gets one line of
// out/dirac.jsonl holding dirac's answer for its scripted position, as play's sweep
// gives one to each game it plays (see playStartDirac). Nil tables plays gorge alone
// and writes no dirac file.
func playStartDirac(reg *cards.Registry, paths []string, set, out string, xmage bool, tables *dside.Tables) error {
	files, err := startFiles(paths)
	if err != nil {
		return err
	}
	if tables != nil {
		// A table this pin and corpus has none of is the same answer for every
		// file, so say it once here rather than in the reason of every row (the
		// set lane does the same, see randomPicker).
		if _, terr := tables.Table(set); terr != nil {
			fmt.Printf("xengine: -dirac: %v\n", diracTablesHint(terr))
		}
	}
	var items, gorge, lowerLog, diracLog bytes.Buffer
	exercised := cov.New()
	hist := map[string]int{}
	diracHist := map[string]int{}
	diracN := 0
	em := &emitter{out: out, items: &items, gorge: &gorge, lowerLog: &lowerLog, hist: hist}
	games := 0
	// noteDirac writes one start file's dirac row.
	noteDirac := func(row dside.Row) error {
		b, err := jsonl(row)
		if err != nil {
			return err
		}
		diracLog.Write(b)
		diracHist[row.Status]++
		diracN++
		return nil
	}
	for _, path := range files {
		f, err := readStartFile(path)
		if err != nil {
			return err
		}
		focus, err := f.focusOf()
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		d0, s0, err := startDeckAt(reg, "start-p0", 0, f.Setup.P0, f.Setup.P1)
		var d1 decks.Deck
		var s1 rules.StartSeat
		if err == nil {
			d1, s1, err = startDeckAt(reg, "start-p1", 1, f.Setup.P1, f.Setup.P0)
		}
		if errors.Is(err, errPlayerCountersPin) {
			// One start the pin cannot hold must not lose the rest of the
			// directory: refuse each of its ids and play on.
			for s := 1; s <= f.Seeds; s++ {
				games++
				b, jerr := jsonl(lowerRow{ID: scen.StartID(set, f.Card, f.Key, s), Status: statusRefused, Step: -1, Reasons: []string{reasonStartPlayerCountersPin}})
				if jerr != nil {
					return jerr
				}
				lowerLog.Write(b)
				hist[statusRefused]++
			}
			if tables != nil {
				if err := noteDirac(dside.NotApplicable(startGameID(set, f), "start not expressible: "+reasonStartPlayerCountersPin)); err != nil {
					return err
				}
			}
			continue
		}
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		pair := []decks.Deck{d0, d1}
		for _, d := range pair {
			for _, n := range d.Cards {
				if c, ok := reg.Lookup(n); ok {
					exercised.Seed(n, c)
				}
			}
		}
		seeds := f.gameSeeds()
		// dirac answers the game gorge plays, so it runs after gorge's focus game
		// is accepted, once per seed, and only for a seed gorge actually played: a
		// key gorge refuses (room locked, key not exercised, a lowering refusal)
		// has no game for dirac to agree with. The seeds' rows fold into one row
		// per key (startFileRow), the granularity adjudicate reads them at.
		startSpec := &rules.StartSpec{Seats: []rules.StartSeat{s0, s1}}
		var seedRows []dside.Row
		gorgeWhy := ""
		refuse := func(why string) {
			if gorgeWhy == "" {
				gorgeWhy = why
			}
		}
		expressible := ""
		if tables != nil {
			expressible = startNotExpressible(f, startSpec)
		}
		for s := 1; s <= f.Seeds; s++ {
			games++
			id := scen.StartID(set, f.Card, f.Key, s)
			if locked := roomDoorLocked(reg, f, focus); locked != "" {
				refuse(reasonStartRoomLocked + ":" + locked)
				b, err := jsonl(lowerRow{ID: id, Status: statusRefused, Step: -1, Reasons: []string{reasonStartRoomLocked + ":" + locked}})
				if err != nil {
					return err
				}
				lowerLog.Write(b)
				hist[statusRefused]++
				continue
			}
			name := fmt.Sprintf("xproof start %s %s seed %d", f.Card, f.Key, s)
			took := -1
			opt := game.Options{Set: set, Start: &rules.StartSpec{Seats: []rules.StartSeat{s0, s1}}, Focus: &focus, FocusTook: &took}
			if len(f.Setup.P0.Commanders) > 0 || len(f.Setup.P1.Commanders) > 0 {
				// A designated commander is an ordinary dealt card of the
				// seat's deck (startDeckAt placed it); only the designation
				// travels, as file-ordered names the Setup hook resolves to
				// object IDs. This never rides the Commander-format lane
				// (decks.Deck.Commander), which needs a commander in every
				// deck and switches the life total and format.
				opt.StartCommanders = [][]string{f.Setup.P0.Commanders, f.Setup.P1.Commanders}
			}
			if f.MaxTurns > 0 {
				// max_turns is the turns played: the engine's watchdog stalls
				// the game as the turn after them begins.
				opt.MaxTurns = f.MaxTurns + 1
			}
			seed := seeds[s-1]
			l, reasons, tp, err := lower.Span(reg, pair, seed, opt, xmage, id, name)
			if err != nil || tp == nil {
				refuse("gorge's focus game did not play: " + fmt.Sprint(err))
				row := lowerRow{ID: id, Step: -1, Status: statusPlayError, Field: "play", Gorge: fmt.Sprint(err)}
				b, _ := jsonl(row)
				lowerLog.Write(b)
				hist[row.Status]++
				continue
			}
			if (focus.Kind != "" || focus.Attack != "" || focus.Block != "") && took == 0 {
				// The focus seat was never offered the focus play (no target, wrong
				// timing, an enabler that died), or the scripted combat of a focus
				// with no play (a combat trigger) was never declared: the game
				// exercises nothing of the key, so the file is refused, not written.
				refuse(reasonStartNotExercised)
				b, err := jsonl(lowerRow{ID: id, Status: statusRefused, Step: -1, Reasons: []string{reasonStartNotExercised}})
				if err != nil {
					return err
				}
				lowerLog.Write(b)
				hist[statusRefused]++
				continue
			}
			if lost := backFaceLost(reg, f, l.Item); lost != "" {
				refuse(reasonStartFaceLost + ":" + lost)
				b, err := jsonl(lowerRow{ID: id, Status: statusRefused, Step: -1, Reasons: []string{reasonStartFaceLost + ":" + lost}})
				if err != nil {
					return err
				}
				lowerLog.Write(b)
				hist[statusRefused]++
				continue
			}
			// The item names the card it starts and (in its id) the key it was
			// written for, so a static or combat key -- which no tape entry
			// names -- is credited too (credit). Card is the front-face spelling
			// startDeck dealt, not the file's (a joined "A // B" start would
			// otherwise credit a second spelling of the same key).
			l.Item.Card = canonicalCard(reg, f.Card)
			for u, n := range credit(reg, l.Item, tp, 0, len(tp.Entries)-1, true) {
				exercised.Add(u.Card, u.Key, cov.Counts{Exercised: n})
			}
			if len(reasons) > 0 {
				refuse("gorge refused the start: " + strings.Join(reasons, ", "))
				b, err := jsonl(lowerRow{ID: id, Status: statusRefused, Step: -1, Reasons: reasons})
				if err != nil {
					return err
				}
				lowerLog.Write(b)
				hist[statusRefused]++
				continue
			}
			for _, c := range f.Compare {
				if !slices.Contains(l.Item.Compare, c) {
					l.Item.Compare = append(l.Item.Compare, c)
				}
			}
			if err := em.emit(l, spanFile(l), lower.CheckWindow(reg, l)); err != nil {
				return err
			}
			if tables != nil && expressible == "" {
				// gorge played and kept this seed: dirac replays its tape from the
				// same scripted position and seed, decision for decision. A report
				// beside the rows, never read by them (operator 11-24).
				seedRows = append(seedRows, startDirac(tables, startGameID(set, f), set, [2]decks.Deck{d0, d1},
					seed, startSpec, tp))
			}
		}
		if tables != nil {
			var row dside.Row
			switch {
			case expressible != "":
				row = dside.NotApplicable(startGameID(set, f), expressible)
			default:
				row = startFileRow(startGameID(set, f), seedRows, gorgeWhy)
			}
			if err := noteDirac(row); err != nil {
				return err
			}
		}
	}
	var tsv bytes.Buffer
	if err := exercised.WriteTSV(&tsv); err != nil {
		return err
	}
	for _, f := range []struct {
		name string
		b    []byte
	}{
		{"items.jsonl", items.Bytes()}, {"gorge.jsonl", gorge.Bytes()},
		{"lower.jsonl", lowerLog.Bytes()}, {"exercised.tsv", tsv.Bytes()},
	} {
		if err := writeAtomic(filepath.Join(out, f.name), f.b); err != nil {
			return err
		}
	}
	if tables != nil {
		if err := writeAtomic(filepath.Join(out, "dirac.jsonl"), diracLog.Bytes()); err != nil {
			return err
		}
	}
	fmt.Printf("played %d games into %s\n", games, out)
	for _, s := range sortedKeys(hist) {
		fmt.Printf("  %-14s %d\n", s, hist[s])
	}
	if tables != nil {
		fmt.Printf("dirac answered %d starts\n", diracN)
		for _, s := range sortedKeys(diracHist) {
			fmt.Printf("  %-14s %d\n", s, diracHist[s])
		}
	}
	return nil
}

// startDirac is one start seed's dirac lockstep. A var so a test can put an answer
// on the record that no built table produces on demand (a divergence at a chosen
// decision, a refusal); the real one is dside.StartLockstep, which plays the seed's
// scripted position beside gorge and never fails the run.
var startDirac = func(tables *dside.Tables, gameID, set string, pair [2]decks.Deck, seed uint64, start *rules.StartSpec, tape *game.Tape) dside.Row {
	return tables.StartLockstep(gameID, set, pair, seed, start, tape)
}

// startGameID is the id every item of the file shares (the prefix dirac.jsonl keys the
// file's row by): its first item's id without the seed ordinal.
func startGameID(set string, f *startFile) string {
	id := scen.StartID(set, f.Card, f.Key, 1)
	gameID, _, ok := dside.SplitID(id)
	if !ok {
		gameID = strings.TrimSuffix(id, ":s1")
	}
	return gameID
}

// startFileRow is one start file's dirac.jsonl row, from the lockstep of each seed
// gorge played (the row is the key's: adjudicate reads one row for every seed item
// of it). A seed that diverged makes the row diverged, so no seed's divergence is
// hidden; otherwise the first seed dirac could not carry through (a hang, an
// unmodelled rule, a capacity, a tape it could not replay) is the row, and only
// seeds that all agreed make it clean. A file gorge played no seed of has no game for
// dirac to agree with: not_applicable, saying why gorge refused it.
func startFileRow(gameID string, rows []dside.Row, gorgeWhy string) dside.Row {
	if len(rows) == 0 {
		return dside.NotApplicable(gameID, "start not played by gorge: "+gorgeWhy)
	}
	rank := func(status string) int {
		switch status {
		case dside.StatusDiverged:
			return 0
		case dside.StatusHang:
			return 1
		case dside.StatusUnmodelled:
			return 2
		case dside.StatusCapacity:
			return 3
		case dside.StatusNA:
			return 4
		}
		return 5
	}
	best := rows[0]
	for _, r := range rows[1:] {
		if rank(r.Status) < rank(best.Status) {
			best = r
		}
	}
	best.Game = gameID
	return best
}

// kernelMirrored is every field of the pinned rules.StartSeat dirac's kernel mirrors
// from the gorge start: dirac/oracle kernelStart copies exactly these (the zones, the
// taps, the back faces, the life total, the counters and the attachments), so a start
// that names only these needs no exception. It is checked against the struct by
// reflection at every start (startNotExpressible), so a field gorge adds to
// StartSeat is refused by default until someone reads kernelStart and names it here:
// the list cannot silently fall behind the kernel, only run ahead of it (which
// TestKernelMirroredFieldsExist holds).
var kernelMirrored = map[string]bool{
	"Hand": true, "Battlefield": true, "Graveyard": true, "Exile": true, "Library": true,
	"Tapped": true, "BackFace": true, "Life": true, "Counters": true, "Attach": true,
}

// kernelRefused are the fields a seat can name that dirac's kernel does not mirror,
// each with the reason it cannot: Tokens, which kernelStart refuses outright
// ("tokens are not modelled by the kernel"); PlayerCounters, which kernelStart
// silently drops (it copies the zones, the taps, the back faces, the life, the card
// counters and the attachments, and never reads the seat's own counters) and which
// must therefore be refused here, or dirac would answer a position without them while
// gorge answers one with them; and Commanders, which only the start file's seat has —
// the designation rides game.Options StartCommanders, so the kernel never sees it. The
// two seat structs share these field names, so one list answers for both; a field
// either struct carries that neither list names is refused by firstNotMirrored under
// its own name, and TestKernelMirroredFieldsExist is the failure that sends someone to
// read kernelStart and name it in the right list when gorge or a start file adds one.
var kernelRefused = map[string]string{
	"Tokens":         "mints tokens, which dirac's kernel start does not model",
	"PlayerCounters": "seats player counters, which dirac's kernel start does not mirror",
	"Commanders":     "designates commanders, which ride game.Options StartCommanders rather than the start, so dirac's kernel never sees the designation",
}

// startNotExpressible is why dirac cannot mirror this start's scripted position,
// naming it, or "" when it can. gorge's side of the lockstep opens in the very
// rules.StartSpec its own game did; the row is only dirac's answer if dirac's kernel
// opens in the same position, which is what kernelMirrored holds. A commander
// designation is the one thing a start file names that does not even ride the spec:
// it travels on game.Options StartCommanders and gorge's Setup hook records it, so
// dirac's kernel has nothing to mirror it from and would hold an ordinary card while
// gorge holds its commander. Each of these would leave the two engines in different
// positions, and the row would report the missing injection as dirac disagreeing with
// gorge: refuse it with the reason instead of guessing.
// firstNotMirrored is why one seat (of either seat struct, t its type) names a field
// dirac's kernel cannot mirror, or "" when it names none. Fields are visited in
// declaration order, so the reason of a seat naming two unmirrored fields is the same
// on every run. Anything the kernel's side of the lockstep can be handed that no list
// names is refused under its own name: dirac's kernel mirrors exactly kernelMirrored,
// so a field unknown to the lists must not ride into the lockstep unseen — until
// someone reads kernelStart and names it (TestKernelMirroredFieldsExist), the only
// thing a new field can do is fail the run's rows with its name in the reason.
func firstNotMirrored(t reflect.Type, seat any) string {
	v := reflect.ValueOf(seat)
	for j := 0; j < t.NumField(); j++ {
		name := t.Field(j).Name
		if kernelMirrored[name] || v.Field(j).IsZero() {
			continue
		}
		why, ok := kernelRefused[name]
		if !ok {
			why = fmt.Sprintf("seats %s, which dirac's kernel start does not mirror", name)
		}
		return why
	}
	return ""
}

func startNotExpressible(f *startFile, start *rules.StartSpec) string {
	// The file's own seats first: a name the file spells never reaches the spec
	// (gorge may reject the game before then, or hold the position gorge alone
	// understands), so the refusal reads what the file asked for, not a spec an
	// earlier stage already trimmed.
	ft := reflect.TypeOf(startSeat{})
	for i, s := range []startSeat{f.Setup.P0, f.Setup.P1} {
		if why := firstNotMirrored(ft, s); why != "" {
			return fmt.Sprintf("start not expressible: seat %d %s", i, why)
		}
	}
	if start == nil {
		return ""
	}
	st := reflect.TypeOf(rules.StartSeat{})
	for i, seat := range start.Seats {
		if why := firstNotMirrored(st, seat); why != "" {
			return fmt.Sprintf("start not expressible: seat %d %s", i, why)
		}
	}
	return ""
}
