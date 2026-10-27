// Command xengine drives the cross-engine full-game differential harness
// (DESIGN xengine): gorge plays explore-bot games, each game is lowered to one
// oracle tape scenario a follower (XMage, Forge) replays, and every
// difference is classified and written as a repro directory.
//
//	xengine play -set S [-chunk C] [-games K] [-seed N] [-windows] [-ledger L.tsv] [-out DIR]
//	xengine diff -play DIR -engine xmage|forge -rows ROWS.jsonl -out DIR2 [-only ID]
//	xengine cov  -ledger A.tsv [-ledger B.tsv ...] [-sets FDN,BLB] [-missing] [-write OUT.tsv]
//	xengine sweep-plan -root DIR -sets 'FDN BLB' -chunks N [-stale] [-force]
//
// play writes DIR/tapes/*.json (every game's tape), DIR/items.jsonl (the
// follower input: only the items whose lowering gorge's own runner
// reproduced), DIR/gorge.jsonl ({id, result} per item), DIR/lower.jsonl (every
// item's self-check and each refusal or truncation) and DIR/exercised.tsv
// (the ledger with Exercised credited). diff compares a follower's rows with
// gorge's stored results, never replaying gorge, and writes DIR2/verdicts.jsonl,
// DIR2/compared.tsv and DIR2/finds/<class>/<id>/ (a repro directory per
// non-agree item). sweep-plan lists the SET/CHUNK pairs of a sweep root that
// have no .done marker (scripts/xengine-sweep.sh). Follower output stays
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
		fmt.Fprintln(os.Stderr, "usage: xengine play|diff|cov|sweep-plan [flags]")
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
	case "sweep-plan":
		err = runSweepPlan(os.Args[2:])
	default:
		err = fmt.Errorf("unknown subcommand %q (want play, diff, cov or sweep-plan)", os.Args[1])
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
