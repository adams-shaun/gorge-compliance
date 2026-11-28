package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/certdeck"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/classify"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/scen"
)

// runScen is `xengine scen`: the scenario that exercises one owed ability key
// (a scripted-steps item, an xproof-start-1 file, or a named refusal), and with
// -census the reach table over a whole set.
func runScen(args []string) error {
	fs := flag.NewFlagSet("scen", flag.ExitOnError)
	dir := fs.String("cards", ".cards", "corpus dir")
	manifests := fs.String("manifests", "oracle/compliance/manifests", "set manifests dir (the cards XMage lists, under the spelling it uses)")
	forgeEditions := fs.String("forge-editions", "", "pinned Forge editions dir (default: find the forge-oracle clone, and fail if none; none builds an unfiltered pool that may pick a card Forge's card DB filters)")
	card := fs.String("card", "", "the card (required without -census)")
	key := fs.String("key", "", "the cov key: cast, land, f0/a2, f0/t0, static#0.0, combat#0.block, repl#0.0 (required without -census)")
	prefer := fs.String("prefer", "auto", "auto (a scripted-steps item where a template serves the key) or start (always a start file)")
	set := fs.String("set", "SCEN", "names the item ids; with -census, the manifest to measure")
	census := fs.Bool("census", false, "measure every key of every card of manifest -set: writes reach.tsv and summary.tsv")
	out := fs.String("out", "", "output dir (required)")
	fs.Parse(args)
	if *out == "" {
		return errors.New("scen needs -out")
	}
	if *census {
		if *card != "" || *key != "" {
			return errors.New("scen: -census takes -set, not -card or -key")
		}
		if *set == "SCEN" {
			return errors.New("scen: -census needs -set")
		}
		if err := classify.RefuseRepoPath(*out); err != nil {
			return err
		}
	} else if *card == "" || *key == "" {
		return errors.New("scen needs -card and -key (or -census -set S)")
	}
	reg, err := loadReg(*dir)
	if err != nil {
		return err
	}
	sp, err := decks.LoadSpellings(*manifests)
	if err != nil {
		return err
	}
	b := certdeck.NewBuilder(reg, sp)
	if err := useForgeEditions(b, *forgeEditions); err != nil {
		return err
	}
	opt := scen.Options{Prefer: *prefer, Set: *set}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		return err
	}
	if *census {
		cands, _, err := decks.Candidates(reg, *manifests, *set)
		if err != nil {
			return err
		}
		names := make([]string, len(cands))
		for i, c := range cands {
			names[i] = c.Name
		}
		rows := scen.Census(reg, b, names, opt)
		if err := writeAtomic(filepath.Join(*out, "reach.tsv"), []byte(scen.ReachTSV(rows))); err != nil {
			return err
		}
		sum := scen.SummaryTSV(rows)
		if err := writeAtomic(filepath.Join(*out, "summary.tsv"), []byte(sum)); err != nil {
			return err
		}
		fmt.Printf("scen census %s: %d cards, %d keys -> %s\n%s", *set, len(names), len(rows), *out, sum)
		return nil
	}
	res, err := scen.Generate(reg, b, *card, *key, opt)
	if err != nil {
		return err
	}
	base := filepath.Join(*out, scen.Slug(*card)+"-"+scen.KeyID(*key))
	switch res.Path {
	case scen.PathRefused:
		fmt.Printf("refused %s %s: %s\n", *card, *key, res.Reason)
	case scen.PathStart:
		raw, err := res.Start.Marshal()
		if err != nil {
			return err
		}
		if err := writeAtomic(base+".start.json", raw); err != nil {
			return err
		}
		fmt.Printf("scen %s %s: start -> %s.start.json\n", *card, *key, base)
	default:
		raw, err := json.Marshal(res.Steps)
		if err != nil {
			return err
		}
		if err := writeAtomic(base+".steps.jsonl", append(raw, '\n')); err != nil {
			return err
		}
		fmt.Printf("scen %s %s: steps -> %s.steps.jsonl\n", *card, *key, base)
	}
	return nil
}
