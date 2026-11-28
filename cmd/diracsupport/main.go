// Command diracsupport writes the per-card dirac support snapshot the carddb
// `dirac` writer reads (oracle/compliance/dirac-gaps.tsv).
//
// gorge's support page counts a card supported only after kgen lowers it, which
// needs the GPL Forge corpus (.cards, never committed here). The committed
// dirac/support lists name the refused and the evidenced cards, but a card that
// lost its lowering after its evidence was recorded is still on an evidence
// list, so the lists alone over-count. This command lowers the whole corpus
// through kgen.SupportFor at the pinned gorge and records exactly the cards the
// lists cannot place: every corpus card that is not plain `supported` or
// `refused` (unverified, not lowered) plus the supported cards whose lowering is
// two-seat only. The carddb writer fills in the rest from the lists, compares
// the snapshot's pin with GORGE_PIN, and degrades a stale snapshot's rows (its
// why says which pin measured them) instead of refusing them.
//
//	go run ./cmd/diracsupport -cards .cards > oracle/compliance/dirac-gaps.tsv
//
// The output holds card names, statuses and no refusal node text (a Forge
// script parameter), so no Forge script text. It is deterministic (byte-sorted names, no clock).
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/dirac/cardset"
	"github.com/adams-shaun/gorge/dirac/kgen"
)

func main() {
	cards := flag.String("cards", ".cards", "the Forge corpus directory (holds kgen/ALL.txt)")
	pinFile := flag.String("pin", "GORGE_PIN", "file holding the gorge commit the snapshot is measured at")
	flag.Parse()
	if err := run(*cards, *pinFile, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "diracsupport:", err)
		os.Exit(1)
	}
}

func run(cardsDir, pinFile string, out *os.File) error {
	pin, err := os.ReadFile(pinFile)
	if err != nil {
		return err
	}
	all, err := os.ReadFile(cardsDir + "/kgen/ALL.txt")
	if err != nil {
		return fmt.Errorf("%w (run a kgen test or `kgen -census-std` once to write the ALL list)", err)
	}
	var names []string
	for _, l := range strings.Split(string(all), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			names = append(names, l)
		}
	}
	sort.Strings(names)
	sup, err := kgen.SupportFor(cardsDir, names)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(out)
	fmt.Fprintf(w, "#dirac-gaps gorge-pin %s\n", strings.TrimSpace(string(pin)))
	for _, n := range names {
		c, ok := sup[n]
		if !ok {
			return fmt.Errorf("%s: kgen returned no support", n)
		}
		switch c.Status {
		case cardset.StatusNotLowered:
			// The refusal node names a Forge script parameter: not committed.
			fmt.Fprintf(w, "%s\tnot_lowered\tkgen does not lower it\n", n)
		case cardset.StatusUnverified:
			fmt.Fprintf(w, "%s\tunverified\tin no evidence list\n", n)
		case cardset.StatusSupported:
			if c.TwoSeat {
				fmt.Fprintf(w, "%s\tsupported\ttwo-seat only: not supported in a game of three or more seats\n", n)
			}
		}
	}
	return w.Flush()
}
