// Command xengine drives the cross-engine full-game differential harness
// (DESIGN xengine): gorge plays explore-bot games, each game is lowered to one
// oracle tape scenario a follower (XMage, Forge) replays, and every
// difference is classified and written as a repro directory.
//
//	xengine play -set S [-chunk C] [-games K] [-seed N] [-windows] [-tape-turns 1,2,4,8] [-ledger L.tsv] [-out DIR]
//	xengine diff -play DIR -engine xmage|forge -rows ROWS.jsonl -out DIR2 [-only ID]
//	xengine cov  -ledger A.tsv [-ledger B.tsv ...] [-sets FDN,BLB] [-sub BASE.tsv] [-missing] [-write OUT.tsv]
//	xengine calib -play DIR -diff DIR2 -engine xmage|forge
//	xengine xprep -play DIR -out xitems.jsonl [-manifests DIR]
//	xengine adjudicate -play DIR -xmage DIFF_X -forge DIFF_F -out DIR3 [-ledger DIR] [-forge-ref REF12]
//	xengine sweep-plan -root DIR -sets 'FDN BLB' -chunks N [-stale] [-force]
//
// play writes DIR/tapes/*.json (every game's tape), DIR/items.jsonl (the
// follower input: only the items whose lowering gorge's own runner
// reproduced), DIR/gorge.jsonl ({id, result} per item), DIR/lower.jsonl (every
// item's self-check and each refusal or truncation) and DIR/exercised.tsv
// (the ledger with Exercised credited); -tape-turns N,... adds each tape cut
// at the end of turn N (id ...:tape:t<N>). diff compares a follower's rows with
// gorge's stored results, never replaying gorge, and writes DIR2/verdicts.jsonl,
// DIR2/compared.tsv and DIR2/finds/<class>/<id>/ (a repro directory per
// non-agree item). calib reports, from a play dir and its diff dir, how many
// lower_ok windows a follower replayed and how far it followed tape prefixes
// (CALIB.md, calib.json, with the gate applied). xprep fills the XMage driver's fields (scripted answers,
// ability rule-text prefixes) on DIR's items and writes the ones it can serve
// to xitems.jsonl, the rest with their reason to xprep.jsonl beside it; play's
// -xmage leaves the cards XMage spells differently out of the decks. sweep-plan lists the SET/CHUNK pairs of a sweep root that
// have no .done marker (scripts/xengine-sweep.sh). adjudicate gives the
// items one follower diverged on a three-way pattern (oraclediff.ThreeWay) from both followers' diffs, writes DIR3/findings.jsonl,
// patterns.tsv, notes.md and a tickets/<cluster>.md draft per gorge_suspect cluster; -ledger adds the compact ledger shards. All of it is off-repo. Follower output stays
// outside the repository.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/adams-shaun/gorge/engine/cards"
)

// DefaultRoot is where host runs write by default.
const DefaultRoot = "/mnt/sata/gorge-training/xengine"

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: xengine play|diff|cov|calib|xprep|adjudicate|sweep-plan [flags]")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "play":
		err = runPlay(os.Args[2:])
	case "diff":
		err = runDiff(os.Args[2:])
	case "cov":
		err = runCov(os.Args[2:])
	case "calib":
		err = runCalib(os.Args[2:])
	case "xprep":
		err = runXprep(os.Args[2:])
	case "adjudicate":
		err = runAdjudicate(os.Args[2:])
	case "sweep-plan":
		err = runSweepPlan(os.Args[2:])
	default:
		err = fmt.Errorf("unknown subcommand %q (want play, diff, cov, calib, xprep, adjudicate or sweep-plan)", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "xengine:", err)
		os.Exit(1)
	}
}

func loadReg(dir string) (*cards.Registry, error) {
	if r, err := cards.LoadRegistry(cards.CachePath(dir)); err == nil {
		return r, nil
	}
	return cards.OpenCorpus(dir)
}

// writeAtomic writes b to path through a temp file in the same directory and a
// rename, so a reader never sees half a file.
func writeAtomic(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	_, werr := f.Write(b)
	cerr := f.Close()
	if werr != nil || cerr != nil {
		os.Remove(f.Name())
		if werr != nil {
			return werr
		}
		return cerr
	}
	if err := os.Rename(f.Name(), path); err != nil {
		os.Remove(f.Name())
		return err
	}
	return nil
}

// jsonl is v's JSON, one line.
func jsonl(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	return append(b, '\n'), err
}

// readLines calls each with every non-empty line of path.
func readLines(path string, each func([]byte) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 256<<20)
	for sc.Scan() {
		if len(sc.Bytes()) == 0 {
			continue
		}
		if err := each(append([]byte(nil), sc.Bytes()...)); err != nil {
			return err
		}
	}
	return sc.Err()
}
