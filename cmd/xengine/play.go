package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/classify"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/cov"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
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
	Item        *oraclegen.Item `json:"item,omitempty"`
}

// lowered rebuilds the lower.Lowered the row describes, around item.
func (r lowerRow) lowered(item oraclegen.Item) lower.Lowered {
	return lower.Lowered{
		Item: item, StepEntry: r.StepEntry, Truncated: r.Truncated, TruncatedAt: r.TruncatedAt,
		FirstEntry: r.FirstEntry, Swap: r.Swap,
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
	windows := fs.Bool("windows", false, "also emit one-turn windows (needs lower.Windows)")
	ledgerPath := fs.String("ledger", "", "coverage ledger TSV at chunk start (empty: an empty ledger)")
	out := fs.String("out", "", "output dir (default under "+DefaultRoot+")")
	fs.Parse(args)
	if *set == "" {
		return fmt.Errorf("play needs -set")
	}
	if *windows {
		return fmt.Errorf("play -windows: this build has no lower.Windows (xe-5 has not landed); drop the flag")
	}
	if *games < 1 || *chunk < 0 {
		return fmt.Errorf("play needs -games >= 1 and -chunk >= 0")
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
	cands, _, err := decks.Candidates(reg, *manifests, *set)
	if err != nil {
		return err
	}
	if err := decks.Pairable(cands); err != nil {
		return fmt.Errorf("set %s: %w", *set, err)
	}
	return play(reg, cands, led, *set, *chunk, *games, *seed, *out)
}

// gameSeed is the seed game g of a chunk plays with: distinct per (chunk,
// game) and a pure function of the chunk seed.
func gameSeed(seed uint64, chunk, g int) uint64 {
	return seed*1_000_003 + uint64(chunk)*10_007 + uint64(g)
}

func play(reg *cards.Registry, cands []decks.Card, led *cov.Ledger, set string, chunk, games int, seed uint64, out string) error {
	var items, gorge, lowerLog bytes.Buffer
	exercised := cov.New()
	exercised.Merge(led)
	hist := map[string]int{}
	// The pairs are drawn against the ledger as it stood at chunk start.
	for g := 0; g < games; g++ {
		pair := decks.Pair(cands, led, set, chunk, g, seed)
		for _, d := range pair {
			for _, n := range d.Cards {
				if c, ok := reg.Lookup(n); ok {
					exercised.Seed(n, c)
				}
			}
		}
		id := fmt.Sprintf("xe:%s:%d:%d:tape", set, chunk, g)
		row := lowerRow{ID: id, Step: -1}
		tp, err := game.Play(reg, pair, gameSeed(seed, chunk, g), game.Options{Set: set})
		if err != nil || tp == nil {
			row.Status, row.Field, row.Gorge = statusPlayError, "play", fmt.Sprint(err)
			b, _ := jsonl(row)
			lowerLog.Write(b)
			hist[row.Status]++
			continue
		}
		for u, n := range classify.Uses(reg, tp, 0, len(tp.Entries)-1, true) {
			exercised.Add(u.Card, u.Key, cov.Counts{Exercised: n})
		}
		l, err := lower.Tape(reg, tp)
		if err != nil {
			row.Status, row.Field, row.Gorge = statusLowerError, "lower", err.Error()
			b, _ := jsonl(row)
			lowerLog.Write(b)
			hist[row.Status]++
			continue
		}
		row.ID = l.Item.ID
		row.Tape = filepath.Join("tapes", classify.Sanitise(row.ID)+".json")
		tb, err := tp.Bytes()
		if err != nil {
			return err
		}
		if err := writeAtomic(filepath.Join(out, row.Tape), tb); err != nil {
			return err
		}
		c := lower.SelfCheck(reg, l, tp)
		row.Status, row.Step, row.Field, row.Gorge, row.TapeValue = c.Status, c.Step, c.Field, c.Gorge, c.Tape
		row.Truncated, row.TruncatedAt, row.FirstEntry, row.Swap = l.Truncated, l.TruncatedAt, l.FirstEntry, l.Swap
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
