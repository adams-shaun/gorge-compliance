package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclediff"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/classify"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/cov"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/follower"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/lower"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/rowio"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/xprep"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
)

func runDiff(args []string) error {
	fs := flag.NewFlagSet("diff", flag.ExitOnError)
	dir := fs.String("cards", ".cards", "corpus dir")
	playDir := fs.String("play", "", "play output dir (required)")
	engine := fs.String("engine", "", "follower: "+strings.Join(follower.Names(), " or ")+" (required)")
	rowsPath := fs.String("rows", "", "follower result rows, JSONL (required)")
	out := fs.String("out", "", "output dir, outside the repository (required)")
	only := fs.String("only", "", "compare this item id only, against the checkpoints the rows carry (a repro rerun)")
	fs.Parse(args)
	if *playDir == "" || *rowsPath == "" || *out == "" || !follower.Known(*engine) {
		return fmt.Errorf("diff needs -play, -engine %s, -rows and -out", strings.Join(follower.Names(), "|"))
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

// followerRowCap is the per-row cap of a follower result file. A row over it
// is not an error: readFollowerRows turns it into a harness row (see
// rowio.Oversized) so one runaway driver transcript cannot lose the chunk.
// It is a variable so a test can lower the cap instead of writing 256 MiB.
var followerRowCap = rowio.DefaultCap

// readFollowerRows reads the follower rows by item id. A row fed to the wrong
// comparison would compare as the other engine's, so a row's engine field must
// agree, and a follower that stamps its rows (Follower.StampsEngine, Forge)
// must carry it.
func readFollowerRows(path, engine string) (map[string]oraclediff.XResult, error) {
	fl, _ := follower.Lookup(engine)
	rows := map[string]oraclediff.XResult{}
	err := rowio.Read(path, followerRowCap, func(b []byte) error {
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
		if fl.StampsEngine && r.Engine != engine {
			return fmt.Errorf("row %q is not a %s row (engine %q)", r.ID, fl.Display, r.Engine)
		}
		rows[r.ID] = r.XResult
		return nil
	}, func(o rowio.Oversized) error {
		x, err := rowio.FollowerOversized(o, engine)
		if err != nil {
			return err
		}
		rows[x.ID] = x
		return nil
	})
	return rows, err
}

func diff(reg *cards.Registry, playDir, engine, rowsPath, out, only string) error {
	fl, _ := follower.Lookup(engine)
	xprepped := fl.Prep == follower.PrepXPrep
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
	// xitems is the item as xprep prepared it for XMage. A repro's input.jsonl
	// must be that item: the raw items.jsonl row carries no xmage_ability, so
	// xmage-oracle-run.sh fails before the divergence. xprep writes it beside
	// the play dir (scripts/xengine-sweep.sh: -play $d -out $d/xitems.jsonl).
	xitems := map[string]oraclegen.Item{}
	if xprepped {
		if err := readLines(filepath.Join(playDir, "xitems.jsonl"), func(b []byte) error {
			var it oraclegen.Item
			if err := json.Unmarshal(b, &it); err != nil {
				return err
			}
			xitems[it.ID] = it
			return nil
		}); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
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

	notSent := map[string]string{}
	// A follower's prep refusals are class not_sent: xprep's for XMage,
	// follower-export's <engine>-prep.jsonl for a follower-export follower.
	// Forge's forge-export refuses nothing.
	refusals := ""
	switch fl.Prep {
	case follower.PrepXPrep:
		refusals = xprepRefusalsFile
	case follower.PrepFollowerExport:
		refusals = engine + followerPrepSuffix
	}
	if refusals != "" {
		if err := readLines(filepath.Join(playDir, refusals), func(b []byte) error {
			var r xprep.Refusal
			if err := json.Unmarshal(b, &r); err != nil {
				return err
			}
			notSent[r.ID] = r.Reason
			return nil
		}); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
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
		if why, left := notSent[r.ID]; !have && left {
			f = classify.Finding{ID: r.ID, Engine: engine, Class: classify.ClassNotSent, Step: -1, Msg: why}
		} else if !have {
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
			// The repro input is the prepared item, so the REPRO re-run can replay
			// the divergence. A not_sent item was refused before xprep wrote a row:
			// it has no prepared form, so it keeps the raw item (its REPRO.md is
			// already unrunnable; changing that is out of scope).
			repro := l
			if xit, ok := xitems[r.ID]; ok {
				repro = r.lowered(xit)
			} else if xprepped && f.Class != classify.ClassNotSent {
				return fmt.Errorf("%s: no prepared item in %s; run xengine xprep first", r.ID, filepath.Join(playDir, "xitems.jsonl"))
			}
			if err := classify.WriteReproFrom(classify.Dir(out, f), f, repro, g, x, origin); err != nil {
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
