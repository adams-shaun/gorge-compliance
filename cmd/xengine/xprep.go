package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"path/filepath"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/classify"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/xprep"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
)

// xprepRefusalsFile sits beside the xitems file: one xprep.Refusal per line.
const xprepRefusalsFile = "xprep.jsonl"

func runXprep(args []string) error {
	fs := flag.NewFlagSet("xprep", flag.ExitOnError)
	dir := fs.String("cards", ".cards", "corpus dir")
	manifests := fs.String("manifests", "oracle/compliance/manifests", "set manifests dir")
	playDir := fs.String("play", "", "play output dir (required)")
	out := fs.String("out", "", "XMage items file, outside the repository (required); refusals go to "+xprepRefusalsFile+" beside it")
	fs.Parse(args)
	if *playDir == "" || *out == "" {
		return fmt.Errorf("xprep needs -play and -out")
	}
	if err := classify.RefuseRepoPath(*out); err != nil {
		return err
	}
	reg, err := loadReg(*dir)
	if err != nil {
		return err
	}
	sp, err := decks.LoadSpellings(*manifests)
	if err != nil {
		return err
	}
	return xprepPlay(reg, sp, *playDir, *out)
}

// xprepPlay prepares every item of playDir/items.jsonl for the XMage driver,
// in file order: the served items go to out (one line each), the refusals to
// xprepRefusalsFile beside it. The items file the other stages read is left
// alone; a refused item has no XMage row, which diff reads as a harness miss.
func xprepPlay(reg *cards.Registry, sp *decks.Spellings, playDir, out string) error {
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
	lrows := map[string]lowerRow{}
	if err := readLines(filepath.Join(playDir, "lower.jsonl"), func(b []byte) error {
		var r lowerRow
		if err := json.Unmarshal(b, &r); err != nil {
			return err
		}
		lrows[r.ID] = r
		return nil
	}); err != nil {
		return err
	}
	var items, refused bytes.Buffer
	hist := map[string]int{}
	n := 0
	if err := readLines(filepath.Join(playDir, "items.jsonl"), func(b []byte) error {
		var it oraclegen.Item
		if err := json.Unmarshal(b, &it); err != nil {
			return err
		}
		n++
		g, ok := gorge[it.ID]
		if !ok {
			return fmt.Errorf("%s: no gorge result in gorge.jsonl", it.ID)
		}
		x, ref := xprep.Prepare(reg, sp, lrows[it.ID].lowered(it), g)
		var line []byte
		var err error
		if ref != nil {
			hist[ref.Reason]++
			line, err = jsonl(ref)
			refused.Write(line)
			return err
		}
		if line, err = jsonl(x); err != nil {
			return err
		}
		items.Write(line)
		return nil
	}); err != nil {
		return err
	}
	if err := writeAtomic(out, items.Bytes()); err != nil {
		return err
	}
	if err := writeAtomic(filepath.Join(filepath.Dir(out), xprepRefusalsFile), refused.Bytes()); err != nil {
		return err
	}
	fmt.Printf("prepared %d of %d items for XMage into %s\n", n-bytes.Count(refused.Bytes(), []byte("\n")), n, out)
	for _, r := range xprep.Reasons {
		if hist[r] > 0 {
			fmt.Printf("  refused %-24s %d\n", r, hist[r])
		}
	}
	return nil
}
