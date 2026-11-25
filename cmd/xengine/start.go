package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclediff"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/cov"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/lower"
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
	// SeedBase is where the file's seed search starts (0 or absent: 1), so a
	// key retried in a later cycle plays new games, not the same Seeds again.
	SeedBase uint64      `json:"seed_base"`
	Focus    *startFocus `json:"focus"`
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
	// BackFace names battlefield cards that enter on their back face.
	BackFace []string       `json:"back_face"`
	Counters []startCounter `json:"counters"`
	Attach   []startAttach  `json:"attach"`
	Tokens   []startToken   `json:"tokens"`
	Life     *int32         `json:"life"`
}

type startAttach struct {
	Card string `json:"card"`
	To   string `json:"to"`
	// Opp: the bearer is on the other seat's battlefield (an Aura whose Enchant
	// keyword is "creature an opponent controls").
	Opp bool `json:"opp"`
}

type startCounter struct {
	Card string `json:"card"`
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
func startFlagErr(set, random, pool, decksDir string, dirac, windows bool, tapeTurns, format, forgeOnly string) error {
	if set == "" {
		return errors.New("-start needs -set (it names the item ids)")
	}
	for _, f := range []struct {
		name string
		on   bool
	}{
		{"-random", random != ""}, {"-pool", pool != ""}, {"-decks", decksDir != ""}, {"-dirac", dirac},
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
		out.Bait = o.Bait
		out.Targets = o.Targets
	}
	if out.Seat < 0 || out.Seat > 1 {
		return out, fmt.Errorf("focus seat %d: no seat lists %q", out.Seat, f.Card)
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

// playStart plays every start file of paths (see startFiles) and writes the
// items, gorge rows, lowering log, exercised ledger and tapes of the games into
// out, as play does. set names the item ids; xmage says XMage replays them.
func playStart(reg *cards.Registry, paths []string, set, out string, xmage bool) error {
	files, err := startFiles(paths)
	if err != nil {
		return err
	}
	var items, gorge, lowerLog bytes.Buffer
	exercised := cov.New()
	hist := map[string]int{}
	em := &emitter{out: out, items: &items, gorge: &gorge, lowerLog: &lowerLog, hist: hist}
	games := 0
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
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		d1, s1, err := startDeckAt(reg, "start-p1", 1, f.Setup.P1, f.Setup.P0)
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
		for s := 1; s <= f.Seeds; s++ {
			games++
			id := scen.StartID(set, f.Card, f.Key, s)
			name := fmt.Sprintf("xproof start %s %s seed %d", f.Card, f.Key, s)
			took := -1
			opt := game.Options{Set: set, Start: &rules.StartSpec{Seats: []rules.StartSeat{s0, s1}}, Focus: &focus, FocusTook: &took}
			if f.MaxTurns > 0 {
				// max_turns is the turns played: the engine's watchdog stalls
				// the game as the turn after them begins.
				opt.MaxTurns = f.MaxTurns + 1
			}
			seed := seeds[s-1]
			l, reasons, tp, err := lower.Span(reg, pair, seed, opt, xmage, id, name)
			if err != nil || tp == nil {
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
				b, err := jsonl(lowerRow{ID: id, Status: statusRefused, Step: -1, Reasons: []string{reasonStartNotExercised}})
				if err != nil {
					return err
				}
				lowerLog.Write(b)
				hist[statusRefused]++
				continue
			}
			if lost := backFaceLost(reg, f, l.Item); lost != "" {
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
	fmt.Printf("played %d games into %s\n", games, out)
	for _, s := range sortedKeys(hist) {
		fmt.Printf("  %-14s %d\n", s, hist[s])
	}
	return nil
}
