package main

import (
	"bytes"
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
	dirac := fs.Bool("dirac", false, "play dirac beside gorge (dirac.jsonl): every game of a set, every other game under -random, draws only cards the set's dirac table plays")
	diracEvery := fs.Int("dirac-every", 2, "with -random and -dirac, every K-th game (game g with g%K == K-1) draws from one set's dirac-playable cards; 1 makes every game one dirac plays beside gorge, so all four engines see every deck")
	out := fs.String("out", "", "output dir (default under "+DefaultRoot+")")
	fs.Parse(args)
	if *set == "" {
		return fmt.Errorf("play needs -set")
	}
	if *games < 1 || *chunk < 0 {
		return fmt.Errorf("play needs -games >= 1 and -chunk >= 0")
	}
	if *diracEvery < 1 {
		return fmt.Errorf("play needs -dirac-every >= 1")
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
	opts := decks.Options{XMage: *xmage}
	var pick picker
	if *random != "" {
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
	return play(reg, pick, tables, led, *set, *chunk, *games, *seed, *out, *windows, *xmage, turns)
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
		if cands, err = tables.Playable(set, cands); err != nil {
			return nil, fmt.Errorf("set %s: %w", set, diracTablesHint(err))
		}
		table = set
	}
	if err := decks.Pairable(cands); err != nil {
		return nil, fmt.Errorf("set %s: %w", set, err)
	}
	return func(int, uint64) ([]decks.Card, *cov.Ledger, string) { return cands, led, table }, nil
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
	for _, code := range sets {
		cands, _, err := decks.CandidatesWith(reg, manifests, code, opts)
		if err != nil {
			return nil, err
		}
		lists = append(lists, cands)
		if tables == nil {
			continue
		}
		playable, err := tables.Playable(code, cands)
		if dside.NoTables(err) {
			return nil, diracTablesHint(err)
		}
		if err == nil && decks.Pairable(playable) == nil {
			bySet = append(bySet, tableSet{code, playable})
		}
	}
	union := decks.Union(append(lists, extra)...)
	if err := decks.Pairable(union); err != nil {
		return nil, err
	}
	if tables != nil && len(bySet) == 0 {
		return nil, fmt.Errorf("-dirac: no set of %v has a dirac table that can build decks", sets)
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

func play(reg *cards.Registry, pick picker, tables *dside.Tables, led *cov.Ledger, set string, chunk, games int, seed uint64, out string, windows, xmage bool, tapeTurns []int) error {
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
		cands, drawLed, table := pick(g, gameSeed(seed, chunk, g))
		pair := decks.Pair(cands, drawLed, set, chunk, g, seed)
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
			if table != "" {
				dRow, opt.Script = tables.Lockstep(dRow.Game, table, pair, gameSeed(seed, chunk, g), game.DefaultMaxTurns)
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
		if err != nil {
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
