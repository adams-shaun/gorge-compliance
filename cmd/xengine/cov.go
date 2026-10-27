package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/cov"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
)

// listFlag collects a repeated flag.
type listFlag []string

func (l *listFlag) String() string     { return strings.Join(*l, ",") }
func (l *listFlag) Set(s string) error { *l = append(*l, s); return nil }

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func runCov(args []string) error {
	fs := flag.NewFlagSet("cov", flag.ExitOnError)
	var ledgers listFlag
	fs.Var(&ledgers, "ledger", "coverage ledger TSV (repeatable; merged)")
	dir := fs.String("cards", ".cards", "corpus dir")
	manifests := fs.String("manifests", "oracle/compliance/manifests", "set manifests dir")
	sets := fs.String("sets", "", "comma-separated set codes to report")
	missing := fs.Bool("missing", false, "also list each reported card's keys never compared")
	write := fs.String("write", "", "write the merged ledger here (atomically)")
	sub := fs.String("sub", "", "base ledger TSV subtracted once from the merged total (counters saturate at 0)")
	fs.Parse(args)
	if len(ledgers) == 0 {
		return fmt.Errorf("cov needs at least one -ledger")
	}
	merged := cov.New()
	for _, p := range ledgers {
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		l, err := cov.ReadTSV(f)
		f.Close()
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		merged.Merge(l)
	}
	if *sub != "" {
		f, err := os.Open(*sub)
		if err != nil {
			return err
		}
		base, err := cov.ReadTSV(f)
		f.Close()
		if err != nil {
			return fmt.Errorf("%s: %w", *sub, err)
		}
		merged.Sub(base)
	}
	if *write != "" {
		var b bytes.Buffer
		if err := merged.WriteTSV(&b); err != nil {
			return err
		}
		if err := writeAtomic(*write, b.Bytes()); err != nil {
			return err
		}
	}
	if *sets == "" {
		return nil
	}
	reg, err := loadReg(*dir)
	if err != nil {
		return err
	}
	// The report reads the keys each card has, so seed a copy with them.
	rep := cov.New()
	rep.Merge(merged)
	byset := map[string][]string{}
	for _, code := range strings.Split(*sets, ",") {
		cands, _, err := decks.Candidates(reg, *manifests, code)
		if err != nil {
			return err
		}
		for _, c := range cands {
			byset[code] = append(byset[code], c.Name)
			if card, ok := reg.Lookup(c.Name); ok {
				rep.Seed(c.Name, card)
			}
		}
	}
	rep.Report(os.Stdout, byset)
	if *missing {
		for _, code := range sortedKeys(byset) {
			for _, n := range byset[code] {
				card, ok := reg.Lookup(n)
				if !ok {
					continue
				}
				var ks []string
				for _, k := range cov.Keys(card) {
					if rep.Get(n, k.ID).Compared == 0 {
						ks = append(ks, k.ID)
					}
				}
				if len(ks) > 0 {
					fmt.Printf("  %s: %s: never compared: %s\n", code, n, strings.Join(ks, " "))
				}
			}
		}
	}
	return nil
}
