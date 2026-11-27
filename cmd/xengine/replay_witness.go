package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclediff"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/classify"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
)

const (
	regressionsHeader = "card\tkey\twitness\tcheckpoint\tfield\tprio"
	replayHeader      = "card\tkey\twitness\tresult\tcheckpoint\tfield"
)

// replayResult is one (witness, key it names) row of replay.tsv.
type replayResult struct{ card, key, witness, result, checkpoint, field string }

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

// runReplayWitness is `xengine replay-witness`: it replays the stored
// witness items (xproof-witness-1, written by `xengine keyproof`) through
// gorge alone at the current pin and turns a key whose witness now differs
// from its stored, follower-agreed result into `diverged`.
func runReplayWitness(args []string) error {
	fs := flag.NewFlagSet("replay-witness", flag.ExitOnError)
	var witnesses multiFlag
	fs.Var(&witnesses, "witness", "witness .jsonl file (repeatable; decompress the release asset first)")
	witnessDir := fs.String("witness-dir", "", "dir of witness .jsonl files, read in file-name order")
	dir := fs.String("cards", ".cards", "corpus dir")
	measured := fs.String("measured", "", "measurement date YYYY-MM-DD (required: no clock is read)")
	sha := fs.String("gorge-sha", "", "gorge commit stamped on rows (default the first 12 of GORGE_PIN)")
	out := fs.String("out", "", "output dir, outside the repository (required)")
	fs.Parse(args)
	if *measured == "" || *out == "" {
		return fmt.Errorf("replay-witness needs -witness or -witness-dir, -measured and -out")
	}
	paths, err := witnessPaths(witnesses, *witnessDir)
	if err != nil {
		return err
	}
	if *sha == "" {
		if b, err := os.ReadFile("GORGE_PIN"); err == nil {
			*sha = strings.TrimSpace(string(b))
			*sha = (*sha)[:min(12, len(*sha))]
		}
	}
	reg, err := loadReg(*dir)
	if err != nil {
		return err
	}
	return replayWitnessIn(reg, *dir, paths, *out, *measured, *sha)
}

// witnessPaths is the replay list: the -witness flags in the order given,
// then every *.jsonl file of dir in file-name order. At least one witness is
// required.
func witnessPaths(witnesses []string, dir string) ([]string, error) {
	paths := append([]string(nil), witnesses...)
	if dir != "" {
		found, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
		if err != nil {
			return nil, err
		}
		sort.Strings(found)
		paths = append(paths, found...)
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("replay-witness needs -witness or -witness-dir, -measured and -out")
	}
	return paths, nil
}

type rwKey struct {
	held, changed []string // witness ids, file order
	refs          map[string]string
	harness       bool
	firstDiv      string // "<checkpoint> <field>" of the first changed witness
}

// replayWitness is replayWitnessIn with the default corpus dir (script_sha
// cells read card scripts from it).
func replayWitness(reg *cards.Registry, paths []string, out, measured, gorgeSHA string) error {
	return replayWitnessIn(reg, ".cards", paths, out, measured, gorgeSHA)
}

func replayWitnessIn(reg *cards.Registry, cardsDir string, paths []string, out, measured, gorgeSHA string) error {
	if err := classify.RefuseRepoPath(out); err != nil {
		return err
	}
	keys := map[classify.Use]*rwKey{}
	type regression struct{ card, key, witness, checkpoint, field string }
	var regs []regression
	var results []replayResult
	nWit, nHeld, nChanged, nErr := 0, 0, 0, 0
	for _, path := range paths {
		line := 0
		err := readLines(path, func(b []byte) error {
			line++
			var w kpWitness
			if err := json.Unmarshal(b, &w); err != nil {
				return fmt.Errorf("%s line %d: %w", path, line, err)
			}
			if w.Format != "xproof-witness-1" {
				return fmt.Errorf("%s line %d: format %q, want xproof-witness-1", path, line, w.Format)
			}
			nWit++
			state, cp, field := "held", "", ""
			g, err := rules.RunOracleScenarioJSON(reg, w.Item.Raw())
			if err != nil {
				state = "harness"
				nErr++
			} else if v := oraclediff.CompareOpts(g, nil, oraclediff.XResult{ID: w.ID, Snapshots: w.Gorge.Snapshots}, w.Item.Compare, w.Item.Ignore...); v.Status == oraclediff.Agree {
				nHeld++
			} else {
				state, cp, field = "changed", v.Checkpoint, v.Field
				nChanged++
			}
			for _, u := range w.Keys {
				use := classify.Use{Card: u.Card, Key: u.Key}
				result := "harness"
				switch state {
				case "held":
					result = "held"
				case "changed":
					result = "changed"
				}
				cpCell, fieldCell := "-", "-"
				if result == "changed" {
					cpCell, fieldCell = dash(cp), dash(field)
				}
				results = append(results, replayResult{u.Card, u.Key, w.ID, result, cpCell, fieldCell})
				k := keys[use]
				if k == nil {
					k = &rwKey{refs: map[string]string{}}
					keys[use] = k
				}
				switch state {
				case "held":
					k.held = append(k.held, w.ID)
					k.refs[w.ID] = w.Refs
				case "changed":
					k.changed = append(k.changed, w.ID)
					if k.firstDiv == "" {
						k.firstDiv = strings.TrimSpace(cp + " " + field)
					}
					regs = append(regs, regression{u.Card, u.Key, w.ID, dash(cp), dash(field)})
				default:
					k.harness = true
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
	}

	var useList []classify.Use
	for u := range keys {
		useList = append(useList, u)
	}
	sort.Slice(useList, func(i, j int) bool {
		if useList[i].Card != useList[j].Card {
			return useList[i].Card < useList[j].Card
		}
		return useList[i].Key < useList[j].Key
	})
	tab := newKpTable(reg, keyproofArgs{Cards: cardsDir, GorgeSHA: gorgeSHA, Measured: measured})
	hist := map[string]int{}
	for _, u := range useList {
		k := keys[u]
		sort.Strings(k.held)
		sort.Strings(k.changed)
		state, reason, refs, witness, cite, deciding := "owed", "harness", "-", "-", "-", ""
		switch {
		case len(k.changed) > 0:
			state, reason, deciding = "diverged", "witness-changed", k.changed[0]
			witness, cite = deciding, dash(k.firstDiv)
		case len(k.held) > 0:
			state, reason, deciding = "agreed", "-", k.held[0]
			witness, refs = deciding, dash(k.refs[deciding])
		}
		hist[state]++
		source := "steps"
		switch {
		case strings.HasPrefix(deciding, "xs:"):
			source = "start"
		case strings.HasPrefix(deciding, "xe:"):
			source = "game"
		}
		tab.add(u, state, reason, refs, source, len(k.held), len(k.changed), witness, "-", cite, "-")
	}

	sort.Slice(regs, func(i, j int) bool {
		x, y := regs[i], regs[j]
		if x.card != y.card {
			return x.card < y.card
		}
		if x.key != y.key {
			return x.key < y.key
		}
		return x.witness < y.witness
	})
	var rb bytes.Buffer
	rb.WriteString(regressionsHeader + "\n")
	for _, r := range regs {
		fmt.Fprintf(&rb, "%s\t%s\t%s\t%s\t%s\tP1\n", r.card, r.key, r.witness, r.checkpoint, r.field)
	}
	if err := writeAtomic(filepath.Join(out, "keyproof.tsv"), tab.buf.Bytes()); err != nil {
		return err
	}
	if err := writeAtomic(filepath.Join(out, "regressions.tsv"), rb.Bytes()); err != nil {
		return err
	}
	sort.Slice(results, func(i, j int) bool {
		x, y := results[i], results[j]
		if x.card != y.card {
			return x.card < y.card
		}
		if x.key != y.key {
			return x.key < y.key
		}
		return x.witness < y.witness
	})
	var pb bytes.Buffer
	pb.WriteString(replayHeader + "\n")
	for _, r := range results {
		fmt.Fprintf(&pb, "%s\t%s\t%s\t%s\t%s\t%s\n", r.card, r.key, r.witness, r.result, r.checkpoint, r.field)
	}
	if err := writeAtomic(filepath.Join(out, "replay.tsv"), pb.Bytes()); err != nil {
		return err
	}
	fmt.Printf("replay-witness %s: %d witnesses, %d held, %d changed, %d harness; keys agreed %d, diverged %d\n",
		dash(gorgeSHA), nWit, nHeld, nChanged, nErr, hist["agreed"], hist["diverged"])
	return nil
}
