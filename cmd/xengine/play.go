package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/classify"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/cov"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/dside"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/lower"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
)

// Status of a game that never reached the lowering; every other status is
// lower.Check's.
const (
	statusPlayError  = "play_error"
	statusLowerError = "lower_error"
	// statusRefused is a window turn lower.Windows refused: no scenario, no
	// tape, no result; diff skips it.
	statusRefused = "refused"
)

// lowerRow is one line of lower.jsonl: how one game's lowering went. Item is
// present only when the self-check failed (the item never reaches
// items.jsonl, and its lower_gap repro needs it).
type lowerRow struct {
	ID          string          `json:"id"`
	Tape        string          `json:"tape,omitempty"`
	Status      string          `json:"status"`
	Step        int             `json:"step"`
	Field       string          `json:"field,omitempty"`
	Gorge       string          `json:"gorge,omitempty"`
	TapeValue   string          `json:"tape_value,omitempty"`
	Truncated   string          `json:"truncated,omitempty"`
	TruncatedAt int             `json:"truncated_at,omitempty"`
	FirstEntry  int             `json:"first_entry"`
	Swap        bool            `json:"swap,omitempty"`
	StepEntry   []int           `json:"step_entry"`
	Steps       int             `json:"steps"`
	Prelude     int             `json:"prelude,omitempty"`
	HasRNG      bool            `json:"has_rng,omitempty"`
	RNGStep     int             `json:"rng_step,omitempty"`
	Reasons     []string        `json:"reasons,omitempty"`
	Turn        int32           `json:"turn,omitempty"`
	Item        *oraclegen.Item `json:"item,omitempty"`
}

// lowered rebuilds the lower.Lowered the row describes, around item.
func (r lowerRow) lowered(item oraclegen.Item) lower.Lowered {
	return lower.Lowered{
		Item: item, StepEntry: r.StepEntry, Truncated: r.Truncated, TruncatedAt: r.TruncatedAt,
		FirstEntry: r.FirstEntry, Swap: r.Swap, Prelude: r.Prelude, HasRNG: r.HasRNG, RNGStep: r.RNGStep,
	}
}

type gorgeRow struct {
	ID     string             `json:"id"`
	Result rules.OracleResult `json:"result"`
}

func runPlay(args []string) error {
	fs := flag.NewFlagSet("play", flag.ExitOnError)
	dir := fs.String("cards", ".cards", "corpus dir")
	manifests := fs.String("manifests", "oracle/compliance/manifests", "set manifests dir")
	set := fs.String("set", "", "set code (required)")
	chunk := fs.Int("chunk", 0, "chunk number")
	games := fs.Int("games", 4, "games in the chunk")
	seed := fs.Uint64("seed", 1, "chunk seed")
	windows := fs.Bool("windows", false, "also emit one-turn windows")
	ledgerPath := fs.String("ledger", "", "coverage ledger TSV at chunk start (empty: an empty ledger)")
	tapeTurns := fs.String("tape-turns", "", "comma-separated turn counts N: also write each tape truncated at the end of turn N (ids ...:tape:t<N>)")
	xmage := fs.Bool("xmage", false, "leave out the cards XMage spells differently (an XMage item names no per-card spelling)")
	random := fs.String("random", "", "set codes, space or comma separated: draw the decks at random across these sets, ignoring the ledger; -set only names the run in the item ids")
	forgeOnly := fs.String("forge-only", "", "with -random, a file of card names, one per line, to add to the cross-set deal: a set XMage has no class for, so its items go to Forge alone")
	dirac := fs.Bool("dirac", false, "play dirac beside gorge (dirac.jsonl): every game of a set, every other game under -random, draws only cards the set's dirac table plays; with -format commander every game is offered to dirac's cmd5 Commander table, after each card it refuses is replaced by a basic of the deck's colours (commander.json swaps); a pair it still does not hold reads not_applicable")
	diracEvery := fs.Int("dirac-every", 2, "with -random and -dirac, every K-th game (game g with g%K == K-1) draws from one set's dirac-playable cards; 1 makes every game one dirac plays beside gorge, so all four engines see every deck; not used with -format commander")
	format := fs.String("format", "", "game format: empty or constructed, or commander (with -commander-decks)")
	seats := fs.Int("seats", 2, "with -format commander: seats a game deals, 2 to 4, each a distinct deck of -commander-decks. More than two seats are played and recorded by gorge alone: the lowering refuses every window and tape of such a game (seats:<n>)")
	cmdrDecks := fs.String("commander-decks", "", "with -format commander: a directory of Commander deck files; each game deals -seats of them (decks.CommanderPod; two seats is a 1v1 match, decks.CommanderPair)")
	pool := fs.String("pool", "", "a file of card names, one per line (# comments): draw the decks from these cards alone, through the same gorge-supported filter and against the ledger as -set; -set only names the run. Not with -random, -forge-only or -dirac")
	poolFilter := fs.String("pool-filter", "", "with -pool, a file of card names to leave out of the pool (cards a follower refuses)")
	decksDir := fs.String("decks", "", "a directory of gorge deck files (*.json, engine/deck's format): every game plays two different fixed decks of it, main deck only, instead of building 40-card decks; -set only names the run. A deck gorge does not fully support (or, with -xmage, naming a card XMage spells otherwise) is skipped; DIR/decks.json records each. Not with -pool, -random, -forge-only or -dirac")
	out := fs.String("out", "", "output dir (default under "+DefaultRoot+")")
	fs.Parse(args)
	if *set == "" {
		return fmt.Errorf("play needs -set")
	}
	if *decksDir != "" && (*format == "commander" || *cmdrDecks != "") {
		return errors.New("-decks plays fixed constructed lists: it cannot be combined with -format commander or -commander-decks")
	}
	if err := decksFlagErr(*decksDir, *pool, *random, *forgeOnly, *dirac); err != nil {
		return err
	}
	if err := poolFlagErr(*pool, *poolFilter, *random, *forgeOnly, *dirac); err != nil {
		return err
	}
	if *games < 1 || *chunk < 0 {
		return fmt.Errorf("play needs -games >= 1 and -chunk >= 0")
	}
	if *diracEvery < 1 {
		return fmt.Errorf("play needs -dirac-every >= 1")
	}
	if *seats < 2 || *seats > game.MaxSeats {
		return fmt.Errorf("play needs -seats between 2 and %d", game.MaxSeats)
	}
	if *seats != 2 && *format != "commander" {
		return errors.New("-seats other than 2 needs -format commander")
	}
	if err := formatFlagErr(*format, *cmdrDecks, *random, *forgeOnly, *pool, *poolFilter, *dirac); err != nil {
		return err
	}
	turns, err := parseTapeTurns(*tapeTurns)
	if err != nil {
		return err
	}
	if *out == "" {
		*out = filepath.Join(DefaultRoot, fmt.Sprintf("%s-c%d-s%d", *set, *chunk, *seed))
	}
	led := cov.New()
	if *ledgerPath != "" {
		f, err := os.Open(*ledgerPath)
		if err != nil {
			return err
		}
		led, err = cov.ReadTSV(f)
		f.Close()
		if err != nil {
			return err
		}
	}
	reg, err := loadReg(*dir)
	if err != nil {
		return err
	}
	var tables *dside.Tables
	if *dirac {
		tables = dside.NewTables(reg)
	}
	if *decksDir != "" {
		var sp *decks.Spellings
		if *xmage {
			if sp, err = decks.LoadSpellings(*manifests); err != nil {
				return err
			}
		}
		ds, info, derr := loadDeckDir(reg, *decksDir, sp)
		fmt.Println(info)
		if derr != nil {
			return derr
		}
		b, err := json.MarshalIndent(info, "", "  ")
		if err != nil {
			return err
		}
		if err := writeAtomic(filepath.Join(*out, "decks.json"), append(b, '\n')); err != nil {
			return err
		}
		return play(reg, fixedDealer(ds, *chunk, *seed), nil, led, *set, *chunk, *games, *seed, *out, *windows, *xmage, turns)
	}
	opts := decks.Options{XMage: *xmage}
	var deal dealer
	if *format == "commander" {
		ds, info, derr := loadCommanderDecks(reg, *cmdrDecks, *seats, tables)
		if derr != nil {
			return derr
		}
		b, merr := json.MarshalIndent(info, "", "  ")
		if merr != nil {
			return merr
		}
		if err := writeAtomic(filepath.Join(*out, "commander.json"), append(b, '\n')); err != nil {
			return err
		}
		deal = commanderDealer(ds, *seats, *set, *chunk, *seed, *dirac)
	} else {
		var pick picker
		var pinfo *poolInfo
		if *pool != "" {
			cands, info, perr := poolCandidates(reg, *manifests, *pool, *poolFilter, *xmage)
			if perr != nil {
				return perr
			}
			fmt.Println(info)
			pick, pinfo = poolPicker(cands, led), &info
		} else if *random != "" {
			var extra []decks.Card
			if *forgeOnly != "" {
				b, rerr := os.ReadFile(*forgeOnly)
				if rerr != nil {
					return rerr
				}
				extra, _ = decks.CandidatesNamed(reg, nonEmptyLines(string(b)), opts)
			}
			pick, err = randomPicker(reg, *manifests, strings.Fields(strings.ReplaceAll(*random, ",", " ")), opts, tables, extra, *diracEvery)
		} else {
			pick, err = setPicker(reg, *manifests, *set, led, opts, tables)
		}
		if err != nil {
			return err
		}
		if pinfo != nil {
			b, merr := json.MarshalIndent(pinfo, "", "  ")
			if merr != nil {
				return merr
			}
			if err := writeAtomic(filepath.Join(*out, "pool.json"), append(b, '\n')); err != nil {
				return err
			}
		}
		deal = pickDealer(pick, *set, *chunk, *seed)
	}
	return play(reg, deal, tables, led, *set, *chunk, *games, *seed, *out, *windows, *xmage, turns)
}

// formatFlagErr checks -format and -commander-decks against each other and
// against the other deck sources: Commander deals from the deck files alone.
func formatFlagErr(format, cmdrDecks, random, forgeOnly, pool, poolFilter string, dirac bool) error {
	switch format {
	case "", "constructed", "commander":
	default:
		return fmt.Errorf("-format %q: want constructed or commander", format)
	}
	if format != "commander" {
		if cmdrDecks != "" {
			return errors.New("-commander-decks needs -format commander")
		}
		return nil
	}
	if cmdrDecks == "" {
		return errors.New("-format commander needs -commander-decks")
	}
	var with []string
	if random != "" {
		with = append(with, "-random")
	}
	if forgeOnly != "" {
		with = append(with, "-forge-only")
	}
	if pool != "" {
		with = append(with, "-pool")
	}
	if poolFilter != "" {
		with = append(with, "-pool-filter")
	}
	if len(with) > 0 {
		return fmt.Errorf("-format commander deals from -commander-decks: it cannot be combined with %s", strings.Join(with, ", "))
	}
	return nil
}

// A dealer deals game g's pair and the set whose dirac table plays it ("" when none does).
type dealer func(g int) (pair []decks.Deck, table string)

// pickDealer deals from a picker's candidates with decks.Pair: today's path.
func pickDealer(pick picker, set string, chunk int, seed uint64) dealer {
	return func(g int) ([]decks.Deck, string) {
		cands, led, table := pick(g, gameSeed(seed, chunk, g))
		p := decks.Pair(cands, led, set, chunk, g, seed)
		return p[:], table
	}
}

// commanderDealer deals Commander games of seats distinct decks of ds (a 1v1
// match for 2: decks.CommanderPair).
func commanderDealer(ds []decks.Deck, seats int, set string, chunk int, seed uint64, dirac bool) dealer {
	table := ""
	if dirac {
		table = dside.CommanderTable
	}
	return func(g int) ([]decks.Deck, string) {
		return decks.CommanderPod(ds, seats, set, chunk, g, seed), table
	}
}

// commanderInfo records which deck files a Commander run dealt from
// (commander.json).
type commanderInfo struct {
	Dir     string              `json:"dir"`
	Decks   []commanderDeckInfo `json:"decks"`
	Skipped []commanderSkip     `json:"skipped"`
	// Swaps are the cards replaced by basics so dirac's cmd5 table holds the decks
	// (-dirac only): every engine plays the substituted lists, not the files'.
	Swaps []decks.Swap `json:"swaps,omitempty"`
}

type commanderDeckInfo struct {
	Stem      string `json:"stem"`
	Commander string `json:"commander"`
	SHA256    string `json:"sha256"`
}

type commanderSkip struct {
	Card   string `json:"card"`
	Reason string `json:"reason"`
}

// loadCommanderDecks reads dir's deck files and keeps the playable Commander
// decks; fewer than seats (each seat plays a distinct deck) is an error naming
// dir and every skip. With tables, each card dirac's cmd5 table refuses is
// replaced by a basic of the deck's colours (info.Swaps), since gorge's precon
// files hold printed lists the table turns away whole; gorge, Forge and dirac then
// all play the substituted lists.
func loadCommanderDecks(reg *cards.Registry, dir string, seats int, tables *dside.Tables) ([]decks.Deck, commanderInfo, error) {
	files, err := decks.ReadCommanderDir(dir)
	if err != nil {
		return nil, commanderInfo{}, err
	}
	ds, skips := decks.CommanderDecks(reg, files)
	info := commanderInfo{Dir: dir, Decks: []commanderDeckInfo{}, Skipped: []commanderSkip{}}
	sums := map[string]string{}
	for _, f := range files {
		sums[f.Stem] = f.SHA256
	}
	if tables != nil {
		var swaps []decks.Swap
		// No tables built: the decks stay as printed and every game reads not_applicable
		// with the hint (play's dirac loop).
		sw, swaps, serr := tables.SwapRefusedCommander(ds)
		switch {
		case dside.NoTables(serr):
		case serr != nil:
			return nil, info, fmt.Errorf("-dirac: table %s: %w", dside.CommanderTable, serr)
		default:
			ds, info.Swaps = sw, swaps
		}
	}
	for _, d := range ds {
		info.Decks = append(info.Decks, commanderDeckInfo{Stem: d.Name, Commander: d.Commander, SHA256: sums[d.Name]})
	}
	var by []string
	counts := map[string]int{}
	var order []string
	for _, s := range skips {
		info.Skipped = append(info.Skipped, commanderSkip{s.Card, s.Reason})
		by = append(by, fmt.Sprintf("%s: %s", s.Card, s.Reason))
		k := s.Reason
		if i := strings.IndexAny(k, ":["); i > 0 {
			k = strings.TrimSpace(k[:i])
		}
		if counts[k] == 0 {
			order = append(order, k)
		}
		counts[k]++
	}
	sort.Strings(order)
	var parts []string
	for _, k := range order {
		parts = append(parts, fmt.Sprintf("%d %s", counts[k], k))
	}
	if len(ds) < seats {
		return nil, info, fmt.Errorf("-commander-decks %s: %d playable Commander deck(s) of %d files, need %d (%s)", dir, len(ds), len(files), seats, strings.Join(by, "; "))
	}
	fmt.Printf("commander decks: %d of %d files (%s)\n", len(ds), len(files), strings.Join(parts, ", "))
	return ds, info, nil
}

// A picker draws game g's decks: the candidates they come from and the set whose
// dirac table plays them ("" when none does). decks.Pair is a pure function of the
// candidates, the ledger, the names and the seed.
type picker func(g int, seed uint64) (cands []decks.Card, led *cov.Ledger, table string)

// setPicker draws every game from set's candidates against the ledger; with
// tables, from those the set's dirac table plays.
func setPicker(reg *cards.Registry, manifests, set string, led *cov.Ledger, opts decks.Options, tables *dside.Tables) (picker, error) {
	cands, skips, err := decks.CandidatesWith(reg, manifests, set, opts)
	if err != nil {
		return nil, err
	}
	if opts.XMage {
		fmt.Printf("xmage spelling: %d card(s) of %s left out of the decks, %d candidates\n", decks.ExcludedXMage(skips), set, len(cands))
	}
	table := ""
	if tables != nil {
		if cands, err = diracCandidates(tables, set, cands); err != nil {
			return nil, err
		}
		table = set
	} else if err := decks.Pairable(cands); err != nil {
		return nil, fmt.Errorf("set %s: %w", set, err)
	}
	return func(int, uint64) ([]decks.Card, *cov.Ledger, string) { return cands, led, table }, nil
}

// diracCandidates is cands minus the cards set's dirac table does not play, or
// an error when what remains cannot build decks. Both errors name the set, so a
// caller that drops the set can report which one and why instead of guessing.
func diracCandidates(tables *dside.Tables, set string, cands []decks.Card) ([]decks.Card, error) {
	playable, err := tables.Playable(set, cands)
	if err != nil {
		return nil, fmt.Errorf("set %s: %w", set, diracTablesHint(err))
	}
	if err := decks.Pairable(playable); err != nil {
		return nil, fmt.Errorf("set %s: %w", set, err)
	}
	return playable, nil
}

// randomPicker draws decks with no regard to the ledger: from the union of the
// sets' candidates and extra, and with tables every diracEvery-th game (see diracGame)
// from one set's cards its dirac table plays, since no table holds cards of two sets.
func randomPicker(reg *cards.Registry, manifests string, sets []string, opts decks.Options, tables *dside.Tables, extra []decks.Card, diracEvery int) (picker, error) {
	var lists [][]decks.Card
	type tableSet struct {
		code  string
		cands []decks.Card
	}
	var bySet []tableSet
	var drops []error
	for _, code := range sets {
		cands, _, err := decks.CandidatesWith(reg, manifests, code, opts)
		if err != nil {
			return nil, err
		}
		lists = append(lists, cands)
		if tables == nil {
			continue
		}
		playable, err := diracCandidates(tables, code, cands)
		if err != nil {
			if dside.NoTables(err) {
				return nil, err
			}
			drops = append(drops, err)
			continue
		}
		bySet = append(bySet, tableSet{code, playable})
	}
	union := decks.Union(append(lists, extra)...)
	// The dropped sets' reasons come before the union's: the union can be
	// pairable across sets even when no single set can build a dirac-table
	// deck, and its error would hide why each set was dropped.
	if tables != nil && len(bySet) == 0 {
		if len(drops) == 0 {
			return nil, fmt.Errorf("-dirac: no set of %v can build decks", sets)
		}
		return nil, fmt.Errorf("-dirac: no set of %v can build decks: %w", sets, errors.Join(drops...))
	}
	if err := decks.Pairable(union); err != nil {
		return nil, err
	}
	for _, err := range drops {
		fmt.Printf("xengine: -dirac: %v\n", err)
	}
	return func(g int, seed uint64) ([]decks.Card, *cov.Ledger, string) {
		if tables != nil && diracGame(g, diracEvery) {
			s := bySet[rand.New(rand.NewPCG(seed, uint64(g))).IntN(len(bySet))]
			return s.cands, nil, s.code
		}
		return union, nil, ""
	}, nil
}

// diracGame reports whether game g of a random chunk is dealt from one set's dirac
// table: every every-th game, the last of each run of every. every 2 is the odd
// games; 1 is all of them, so every deck is one dirac plays in lockstep.
func diracGame(g, every int) bool { return g%every == every-1 }

func diracTablesHint(err error) error {
	if dside.NoTables(err) {
		return fmt.Errorf("%w (the pin's own: go run -trimpath github.com/adams-shaun/gorge/dirac/cmd/kgen -synth-pools)", err)
	}
	return err
}

// gameSeed is the seed game g of a chunk plays with: distinct per (chunk,
// game) and a pure function of the chunk seed.
func gameSeed(seed uint64, chunk, g int) uint64 {
	return seed*1_000_003 + uint64(chunk)*10_007 + uint64(g)
}

// windowFile is the tape file of a window: a shallow copy of its self-check
// tape cut where the lowering stopped. The next turn's first entry, which the
// self-check reads only for the digest of the last step, is not the window's.
func windowFile(l lower.Lowered) *game.Tape {
	n := len(l.Against.Entries)
	if l.Truncated != "" {
		n = min(n, l.TruncatedAt)
	} else {
		n = 0
		for n < len(l.Against.Entries) && l.Against.Entries[n].Turn == 1 {
			n++
		}
	}
	t := *l.Against
	t.Entries = l.Against.Entries[:n:n]
	return &t
}

// parseTapeTurns is the sorted, distinct turn counts of a -tape-turns list.
func parseTapeTurns(s string) ([]int, error) {
	var out []int
	for _, f := range strings.Split(s, ",") {
		if f = strings.TrimSpace(f); f == "" {
			continue
		}
		n, err := strconv.Atoi(f)
		if err != nil || n < 1 {
			return nil, fmt.Errorf("-tape-turns: %q is not a turn count >= 1", f)
		}
		out = append(out, n)
	}
	sort.Ints(out)
	return slices.Compact(out), nil
}

// reasonTurnPrefix is the Truncated reason of a tape prefix: the lowering was
// cut at a turn boundary on purpose, not because it could not go on.
const reasonTurnPrefix = "turn_prefix"

// tapePrefix is the lowered tape l of t cut at the end of turn n: the longest
// run of its steps that carries out only entries of turns <= n. The prefix has
// the tape's id plus ":t<n>"; its tape file is t cut after the first entry of
// turn n+1, which the self-check reads for the digest of the last step. ok is
// false when there is nothing to cut: the tape ends within turn n, the
// lowering stopped before the boundary, or no step lies before it.
func tapePrefix(l lower.Lowered, t *game.Tape, n int) (p lower.Lowered, tf *game.Tape, ok bool) {
	b := -1
	for i, e := range t.Entries {
		if int(e.Turn) > n {
			b = i
			break
		}
	}
	if b < 0 || (l.Truncated != "" && l.TruncatedAt <= b) {
		return l, nil, false
	}
	k := 0
	for i, e := range l.StepEntry {
		if e >= 0 && e < b {
			k = i + 1
		}
	}
	if k == 0 || k == len(l.Item.Steps) {
		return l, nil, false
	}
	p = l
	p.Item.ID = l.Item.ID + ":t" + strconv.Itoa(n)
	p.Item.Steps = l.Item.Steps[:k:k]
	p.StepEntry = l.StepEntry[:k:k]
	if len(p.Item.Payment) > k {
		p.Item.Payment = p.Item.Payment[:k:k]
	}
	p.Truncated, p.TruncatedAt = reasonTurnPrefix, b
	if p.HasRNG && p.RNGStep >= k {
		p.HasRNG, p.RNGStep = false, 0
	}
	cut := *t
	cut.Entries = t.Entries[: b+1 : b+1]
	return p, &cut, true
}

func play(reg *cards.Registry, deal dealer, tables *dside.Tables, led *cov.Ledger, set string, chunk, games int, seed uint64, out string, windows, xmage bool, tapeTurns []int) error {
	var items, gorge, lowerLog, diracLog bytes.Buffer
	exercised := cov.New()
	exercised.Merge(led)
	hist := map[string]int{}
	// emit writes the tape file tf of the lowered item l and its rows, given
	// its self-check c: the one place a tape and a window take their row shape.
	emit := func(l lower.Lowered, tf *game.Tape, c lower.Check) error {
		row := lowerRow{ID: l.Item.ID, Tape: filepath.Join("tapes", classify.Sanitise(l.Item.ID)+".json")}
		tb, err := tf.Bytes()
		if err != nil {
			return err
		}
		if err := writeAtomic(filepath.Join(out, row.Tape), tb); err != nil {
			return err
		}
		row.Status, row.Step, row.Field, row.Gorge, row.TapeValue = c.Status, c.Step, c.Field, c.Gorge, c.Tape
		row.Truncated, row.TruncatedAt, row.FirstEntry, row.Swap = l.Truncated, l.TruncatedAt, l.FirstEntry, l.Swap
		row.Prelude, row.HasRNG, row.RNGStep = l.Prelude, l.HasRNG, l.RNGStep
		row.StepEntry, row.Steps = l.StepEntry, len(l.Item.Steps)
		if c.Status == lower.StatusOK {
			b, err := jsonl(l.Item)
			if err != nil {
				return err
			}
			items.Write(b)
			if b, err = jsonl(gorgeRow{ID: row.ID, Result: c.Result}); err != nil {
				return err
			}
			gorge.Write(b)
		} else {
			it := l.Item
			row.Item = &it
		}
		b, err := jsonl(row)
		if err != nil {
			return err
		}
		lowerLog.Write(b)
		hist[row.Status]++
		return nil
	}
	// The pairs are drawn against the ledger as it stood at chunk start.
	for g := 0; g < games; g++ {
		pair, table := deal(g)
		for _, d := range pair {
			for _, n := range d.Cards {
				if c, ok := reg.Lookup(n); ok {
					exercised.Seed(n, c)
				}
			}
		}
		id := fmt.Sprintf("xe:%s:%d:%d:tape", set, chunk, g)
		var (
			tp      *game.Tape
			wins    []lower.Lowered
			refused []lower.Refusal
			err     error
			dRow    dside.Row
		)
		opt := game.Options{Set: set}
		if tables != nil {
			dRow = dside.NotApplicable(dside.GameID(set, chunk, g), "decks across sets: no dirac table holds them")
			switch {
			case table == dside.CommanderTable && len(pair) != 2:
				dRow = dside.NotApplicable(dRow.Game, fmt.Sprintf("seats:%d: dirac plays 1v1 Commander", len(pair)))
			case table == dside.CommanderTable:
				if _, terr := tables.CommanderSet(); terr != nil && dside.NoTables(terr) {
					return fmt.Errorf("-dirac: table %s: %w", table, diracTablesHint(terr))
				}
				dRow, opt.Script = tables.CommanderLockstep(dRow.Game, [2]decks.Deck(pair), gameSeed(seed, chunk, g), game.DefaultMaxTurns)
			case table != "":
				dRow, opt.Script = tables.Lockstep(dRow.Game, table, [2]decks.Deck(pair), gameSeed(seed, chunk, g), game.DefaultMaxTurns)
			}
		}
		if windows {
			wins, refused, tp, err = lower.Windows(reg, pair, gameSeed(seed, chunk, g), opt, xmage)
		} else {
			tp, err = game.Play(reg, pair, gameSeed(seed, chunk, g), opt)
		}
		if tables != nil {
			if tp != nil {
				dRow.Locate(tp)
			}
			b, jerr := jsonl(dRow)
			if jerr != nil {
				return jerr
			}
			diracLog.Write(b)
		}
		if err != nil || tp == nil {
			row := lowerRow{ID: id, Step: -1, Status: statusPlayError, Field: "play", Gorge: fmt.Sprint(err)}
			b, _ := jsonl(row)
			lowerLog.Write(b)
			hist[row.Status]++
			continue
		}
		for u, n := range classify.Uses(reg, tp, 0, len(tp.Entries)-1, true) {
			exercised.Add(u.Card, u.Key, cov.Counts{Exercised: n})
		}
		// The whole-game tape, then the windows in cut order, then the
		// refusals in cut order.
		l, err := lower.Tape(reg, tp)
		var refusedErr *lower.RefusedError
		if errors.As(err, &refusedErr) {
			// A pod: the tape and each of its -tape-turns prefixes are refused.
			ids := []string{id}
			for _, n := range tapeTurns {
				ids = append(ids, id+":t"+strconv.Itoa(n))
			}
			for _, rid := range ids {
				b, jerr := jsonl(lowerRow{ID: rid, Status: statusRefused, Step: -1, Reasons: refusedErr.Reasons})
				if jerr != nil {
					return jerr
				}
				lowerLog.Write(b)
				hist[statusRefused]++
			}
		} else if err != nil {
			row := lowerRow{ID: id, Step: -1, Status: statusLowerError, Field: "lower", Gorge: err.Error()}
			b, _ := jsonl(row)
			lowerLog.Write(b)
			hist[row.Status]++
		} else if err := emit(l, tp, lower.SelfCheck(reg, l, tp)); err != nil {
			return err
		}
		if err == nil {
			for _, n := range tapeTurns {
				if pl, pt, ok := tapePrefix(l, tp, n); ok {
					if err := emit(pl, pt, lower.SelfCheck(reg, pl, tp)); err != nil {
						return err
					}
				}
			}
		}
		for _, w := range wins {
			if err := emit(w, windowFile(w), lower.CheckWindow(reg, w)); err != nil {
				return err
			}
		}
		for _, r := range refused {
			row := lowerRow{ID: r.ID, Status: statusRefused, Step: -1, Turn: r.Turn, Reasons: r.Reasons}
			b, err := jsonl(row)
			if err != nil {
				return err
			}
			lowerLog.Write(b)
			hist[row.Status]++
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
	return nil
}

// nonEmptyLines is s's lines, trimmed, without the blank ones.
func nonEmptyLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}
