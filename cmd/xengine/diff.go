package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclediff"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/classify"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/cov"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/lower"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
)

func runDiff(args []string) error {
	fs := flag.NewFlagSet("diff", flag.ExitOnError)
	dir := fs.String("cards", ".cards", "corpus dir")
	playDir := fs.String("play", "", "play output dir (required)")
	engine := fs.String("engine", "", "follower: xmage or forge (required)")
	rowsPath := fs.String("rows", "", "follower result rows, JSONL (required)")
	out := fs.String("out", "", "output dir, outside the repository (required)")
	only := fs.String("only", "", "compare this item id only, against the checkpoints the rows carry (a repro rerun)")
	fs.Parse(args)
	if *playDir == "" || *rowsPath == "" || *out == "" || (*engine != "xmage" && *engine != "forge") {
		return fmt.Errorf("diff needs -play, -engine xmage|forge, -rows and -out")
	}
	if err := classify.RefuseRepoPath(*out); err != nil {
		return err
	}
	reg, err := loadReg(*dir)
	if err != nil {
		return err
	}
	return diff(reg, *playDir, *engine, *rowsPath, *out, *only)
}

// readFollowerRows reads the follower rows by item id. A Forge row carries
// "engine":"forge"; one fed to the wrong comparison would compare as the other
// engine's, so the engine field must agree.
func readFollowerRows(path, engine string) (map[string]oraclediff.XResult, error) {
	rows := map[string]oraclediff.XResult{}
	err := readLines(path, func(b []byte) error {
		var r struct {
			oraclediff.XResult
			Engine string `json:"engine"`
		}
		if err := json.Unmarshal(b, &r); err != nil {
			return err
		}
		if r.Engine != "" && r.Engine != engine {
			return fmt.Errorf("row %q is a %s row, not %s", r.ID, r.Engine, engine)
		}
		if engine == "forge" && r.Engine != "forge" {
			return fmt.Errorf("row %q is not a Forge row (engine %q)", r.ID, r.Engine)
		}
		rows[r.ID] = r.XResult
		return nil
	})
	return rows, err
}

func diff(reg *cards.Registry, playDir, engine, rowsPath, out, only string) error {
	rows, err := readFollowerRows(rowsPath, engine)
	if err != nil {
		return err
	}
	items := map[string]oraclegen.Item{}
	if err := readLines(filepath.Join(playDir, "items.jsonl"), func(b []byte) error {
		var it oraclegen.Item
		if err := json.Unmarshal(b, &it); err != nil {
			return err
		}
		items[it.ID] = it
		return nil
	}); err != nil {
		return err
	}
	gorge := map[string]rules.OracleResult{}
	if err := readLines(filepath.Join(playDir, "gorge.jsonl"), func(b []byte) error {
		var r gorgeRow
		if err := json.Unmarshal(b, &r); err != nil {
			return err
		}
		gorge[r.ID] = r.Result
		return nil
	}); err != nil {
		return err
	}
	var lrows []lowerRow
	if err := readLines(filepath.Join(playDir, "lower.jsonl"), func(b []byte) error {
		var r lowerRow
		if err := json.Unmarshal(b, &r); err != nil {
			return err
		}
		if only == "" || r.ID == only {
			lrows = append(lrows, r)
		}
		return nil
	}); err != nil {
		return err
	}
	if only != "" && len(lrows) == 0 {
		return fmt.Errorf("no item %q in %s", only, playDir)
	}
	sort.SliceStable(lrows, func(i, j int) bool { return lrows[i].ID < lrows[j].ID })
	if st, err := os.Stat(filepath.Join(out, "finds")); err == nil && st.IsDir() {
		return fmt.Errorf("%s already holds finds; pick a fresh -out", out)
	}

	absPlay, _ := filepath.Abs(playDir)
	absRows, _ := filepath.Abs(rowsPath)
	origin := classify.Origin{Play: absPlay, Rows: absRows}
	compared := cov.New()
	hist := map[string]int{}
	var verdicts bytes.Buffer
	for _, r := range lrows {
		var f classify.Finding
		switch {
		case r.Status == statusRefused:
			// A refused window is a coverage row: no scenario, no gorge
			// result, nothing for a follower to answer.
			continue
		case r.Status == statusPlayError || r.Status == statusLowerError:
			// No tape and no item: counted as a lowering gap, nothing to repro.
			f = classify.Finding{ID: r.ID, Class: classify.ClassLowerGap, Step: -1, Field: r.Field, Gorge: r.Gorge, Msg: r.Status}
			emit(&verdicts, hist, f)
			continue
		case r.Status != lower.StatusOK:
			if r.Item == nil {
				return fmt.Errorf("%s: lower.jsonl row has status %s but no item", r.ID, r.Status)
			}
			l := r.lowered(*r.Item)
			f = classify.GapFinding(l, lower.Check{Status: r.Status, Step: r.Step, Field: r.Field, Gorge: r.Gorge, Tape: r.TapeValue})
			if err := classify.WriteReproFrom(classify.Dir(out, f), f, l, rules.OracleResult{}, oraclediff.XResult{}, origin); err != nil {
				return err
			}
			emit(&verdicts, hist, f)
			continue
		}
		it, ok := items[r.ID]
		if !ok {
			return fmt.Errorf("%s: lower_ok in lower.jsonl but not in items.jsonl", r.ID)
		}
		g, ok := gorge[r.ID]
		if !ok {
			return fmt.Errorf("%s: no gorge result in gorge.jsonl", r.ID)
		}
		l := r.lowered(it)
		tb, err := os.ReadFile(filepath.Join(playDir, r.Tape))
		if err != nil {
			return err
		}
		tp, err := game.ReadTape(bytes.NewReader(tb))
		if err != nil {
			return fmt.Errorf("%s: %w", r.Tape, err)
		}
		x, have := rows[r.ID]
		if !have {
			f = classify.Finding{ID: r.ID, Engine: engine, Class: classify.ClassHarness, Step: -1, Msg: "no " + engine + " result"}
		} else {
			if only != "" && len(x.Snapshots) < len(g.Snapshots) {
				// A rerun of the scenario cut at the diverging step.
				g.Snapshots = g.Snapshots[:len(x.Snapshots)]
			}
			f = classify.Classify(l, tp, g, x, engine)
			if classify.Credited(f.Class) {
				lo, hi := classify.LoweredRange(l, tp)
				for _, u := range classify.Sorted(classify.Uses(reg, tp, lo, hi, true)) {
					compared.Add(u.Card, u.Key, cov.Counts{Compared: 1})
				}
			}
		}
		if f.Class != classify.ClassAgree {
			if err := classify.WriteReproFrom(classify.Dir(out, f), f, l, g, x, origin); err != nil {
				return err
			}
		}
		emit(&verdicts, hist, f)
	}
	var tsv bytes.Buffer
	if err := compared.WriteTSV(&tsv); err != nil {
		return err
	}
	if err := writeAtomic(filepath.Join(out, "verdicts.jsonl"), verdicts.Bytes()); err != nil {
		return err
	}
	if err := writeAtomic(filepath.Join(out, "compared.tsv"), tsv.Bytes()); err != nil {
		return err
	}
	fmt.Printf("compared %d items against %s into %s\n", len(lrows)-refusedRows(lrows), engine, out)
	for _, c := range classify.Classes {
		if hist[c] > 0 {
			fmt.Printf("  %-15s %d\n", c, hist[c])
		}
	}
	return nil
}

func emit(w *bytes.Buffer, hist map[string]int, f classify.Finding) {
	b, _ := jsonl(f)
	w.Write(b)
	hist[f.Class]++
}

// refusedRows counts the refused rows, which are not items.
func refusedRows(rows []lowerRow) (n int) {
	for _, r := range rows {
		if r.Status == statusRefused {
			n++
		}
	}
	return n
}
