package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/classify"
)

// doneFile marks a chunk whose every stage exited 0.
const doneFile = ".done"

// runSweepPlan prints the chunks scripts/xengine-sweep.sh still has to do.
func runSweepPlan(args []string) error {
	fs := flag.NewFlagSet("sweep-plan", flag.ExitOnError)
	root := fs.String("root", "", "sweep output root, outside the repository (required)")
	sets := fs.String("sets", "", "set codes, space or comma separated, in run order (required)")
	chunks := fs.Int("chunks", 0, "chunks per set, numbered 0..N-1 (required)")
	stale := fs.Bool("stale", false, "move each unfinished chunk directory aside to <dir>.stale.<n>")
	force := fs.Bool("force", false, "plan every chunk, finished or not")
	fs.Parse(args)
	if *root == "" || *chunks < 1 {
		return fmt.Errorf("sweep-plan needs -root, -sets and -chunks >= 1")
	}
	codes := strings.Fields(strings.ReplaceAll(*sets, ",", " "))
	if len(codes) == 0 {
		return fmt.Errorf("sweep-plan needs -sets")
	}
	if err := classify.RefuseRepoPath(*root); err != nil {
		return err
	}
	plan, err := sweepPlan(*root, codes, *chunks, *stale, *force)
	if err != nil {
		return err
	}
	for _, p := range plan {
		fmt.Println(p)
	}
	return nil
}

// sweepPlan lists "SET/CHUNK" for every chunk without a .done marker (every
// chunk with force), sets in the given order and chunks ascending. With stale,
// an existing unfinished chunk directory is renamed to <dir>.stale.<n>, n the
// next free integer, so the chunk is redone into a fresh directory.
func sweepPlan(root string, sets []string, chunks int, stale, force bool) ([]string, error) {
	var plan []string
	for _, s := range sets {
		for c := 0; c < chunks; c++ {
			dir := filepath.Join(root, s, fmt.Sprint(c))
			if _, err := os.Stat(filepath.Join(dir, doneFile)); err == nil && !force {
				continue
			}
			if _, err := os.Stat(dir); err == nil && stale {
				if err := moveAside(dir); err != nil {
					return nil, err
				}
			}
			plan = append(plan, fmt.Sprintf("%s/%d", s, c))
		}
	}
	return plan, nil
}

func moveAside(dir string) error {
	for n := 1; ; n++ {
		to := fmt.Sprintf("%s.stale.%d", dir, n)
		if _, err := os.Lstat(to); err == nil {
			continue
		}
		return os.Rename(dir, to)
	}
}
