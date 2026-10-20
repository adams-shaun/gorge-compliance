// Command mtgishxc cross-checks gorge's compiled card definitions against
// mtgish (https://github.com/i5jb/mtgish), an independent typed encoding of
// the same cards parsed from Oracle text. Both sides are normalised to one
// token summary per face (T1 characteristics, T2 ability shape, T3 targets,
// T4 effects) and diffed; see docs/design/2026-10-20-mtgish-crosscheck.md.
//
//	go run ./cmd/mtgishxc -set FDN -set BLB      # report -> .coverage/mtgish/
//	go run ./cmd/mtgishxc -all                   # every card on both sides (+ FDN/BLB tables)
//	go run ./cmd/mtgishxc -card "Banishing Light" # both summaries, for triage
//	go run ./cmd/mtgishxc -set BLB -tier T4       # one tier's rows only
//	go run ./cmd/mtgishxc -all -write-ledger      # rewrite known/T1..T4.tsv, keeping triage
//	go run ./cmd/mtgishxc -all -write-ledger -triage a.tsv -triage b.tsv
//	                                              # ... merging extra triaged rows by key
//
// The ledger is corpus-wide, so -write-ledger needs -all; -set runs only
// report (their rows carry the ledger's triage). New gaps are written as
// class normalizer, new disagreements as untriaged (refused by the ratchet)
// or, with -pending, as pending. known/excluded.tsv applies to every run.
//
// The data is `make mtgish-fetch`'s .mtgish/<ref>/mtgish.lines.json. The
// report holds summary tokens and card names only, but it is generated:
// it is written to the gitignored .coverage/mtgish/.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/mtgish"
	"github.com/adams-shaun/gorge/engine/cards"
)

type multi []string

func (m *multi) String() string     { return strings.Join(*m, ",") }
func (m *multi) Set(v string) error { *m = append(*m, v); return nil }

func main() {
	var sets, cardNames multi
	flag.Var(&sets, "set", "set code to compare (repeatable)")
	flag.Var(&cardNames, "card", "print both summaries of this card side by side (repeatable)")
	all := flag.Bool("all", false, "compare every card both sides know")
	tier := flag.String("tier", "", "restrict the report's rows and tables to one tier (T1..T4)")
	corpus := flag.String("cards", ".cards", "gorge corpus directory")
	data := flag.String("data", "", "mtgish.lines.json (default .mtgish/<lock ref>/mtgish.lines.json)")
	out := flag.String("out", ".coverage/mtgish", "report directory")
	writeLedger := flag.Bool("write-ledger", false, "with -all: rewrite the corpus ledger oracle/mtgish/known/T1..T4.tsv, keeping existing triage")
	var triage multi
	flag.Var(&triage, "triage", "with -write-ledger: a TSV of triaged rows (the ledger's 7 columns) merged by key, over the ledger's own triage (repeatable)")
	pending := flag.Bool("pending", false, "with -write-ledger: write an untriaged disagreement as class pending instead of untriaged")
	flag.Parse()

	opt := ledgerOpts{write: *writeLedger, triage: triage, pending: *pending}
	if err := run(sets, cardNames, *all, *tier, *corpus, *data, *out, opt); err != nil {
		fmt.Fprintln(os.Stderr, "mtgishxc:", err)
		os.Exit(1)
	}
}

// ledgerOpts are the -write-ledger flags.
type ledgerOpts struct {
	write   bool
	triage  []string
	pending bool
}

func run(sets, cardNames []string, all bool, tierFlag, corpus, data, out string, lo ledgerOpts) error {
	if len(sets) == 0 && len(cardNames) == 0 && !all {
		return fmt.Errorf("give -set, -card or -all")
	}
	if lo.write && !all {
		return fmt.Errorf("-write-ledger needs -all: the ledger is corpus-wide (oracle/mtgish/known/T1..T4.tsv)")
	}
	if (len(lo.triage) > 0 || lo.pending) && !lo.write {
		return fmt.Errorf("-triage and -pending go with -all -write-ledger")
	}
	ex, err := mtgish.ReadExclusions(mtgish.ExclusionsFile)
	if err != nil {
		return err
	}
	var triage [][]mtgish.Row
	for _, f := range lo.triage {
		rows, err := mtgish.ReadLedger(f)
		if err != nil {
			return err
		}
		for _, r := range rows {
			if !mtgish.ValidClass(r.Class) {
				return fmt.Errorf("%s: %s %s %s: class %q is not one of %v", f, r.Card, r.Tier, r.Field, r.Class, mtgish.Classes)
			}
			if strings.TrimSpace(r.Note) == "" {
				return fmt.Errorf("%s: %s %s %s: empty note", f, r.Card, r.Tier, r.Field)
			}
		}
		triage = append(triage, rows)
	}
	var tierOnly mtgish.Tier
	if tierFlag != "" {
		t := strings.TrimPrefix(strings.ToUpper(tierFlag), "T")
		n, err := strconv.Atoi(t)
		if err != nil || n < 1 || n > len(mtgish.Tiers) {
			return fmt.Errorf("-tier %q: want T1..T4", tierFlag)
		}
		tierOnly = mtgish.Tier(n)
	}
	if data == "" {
		ref, err := mtgish.ReadLockRef(mtgish.LockFile)
		if err != nil {
			return err
		}
		data = mtgish.DataPath(".", ref)
	}
	if _, err := os.Stat(data); err != nil {
		return fmt.Errorf("%s: %v (run make mtgish-fetch)", data, err)
	}
	reg, err := cards.SharedCorpus(corpus)
	if err != nil {
		return err
	}

	// The cards to keep while loading: the named sets' and cards', or all.
	perSet := map[string][]string{}
	want := map[string]bool{}
	for _, s := range sets {
		names, err := mtgish.ManifestNames(".", s)
		if err != nil {
			return err
		}
		perSet[s] = names
		for _, n := range names {
			want[n] = true
		}
	}
	for _, n := range cardNames {
		want[n] = true
	}
	var filter func(string) bool
	if !all {
		filter = func(n string) bool { return want[n] }
	}
	ms, err := mtgish.Load(data, filter)
	if err != nil {
		return err
	}

	for _, n := range cardNames {
		printCard(n, reg, ms)
	}
	if len(sets) == 0 && !all {
		return nil
	}

	var rep strings.Builder
	fmt.Fprintf(&rep, "# mtgish cross-check\n\nData: `%s`. Generated by `cmd/mtgishxc`; never committed.\n\n", data)
	runs := []string{}
	results := map[string]mtgish.Result{}
	for _, s := range sets {
		runs = append(runs, s)
		results[s] = mtgish.RunWith(perSet[s], reg, ms, ex)
	}
	if all {
		runs = append(runs, "ALL")
		results["ALL"] = mtgish.RunWith(mtgish.SortedNames(ms), reg, ms, ex)
	}
	// The corpus ledger, if present, triages every run's rows.
	ledger, lerr := mtgish.ReadLedgers(".")
	if lerr != nil && lo.write {
		fmt.Fprintf(os.Stderr, "mtgishxc: %v: writing the ledger from scratch\n", lerr)
	}
	writeExclusions(&rep, ex, results, runs)
	for _, s := range runs {
		res := results[s]
		var rows []mtgish.Row
		if s == "ALL" && lo.write {
			var unused [][]mtgish.Row
			rows, unused = mtgish.MergeTriage(res.Rows, lo.pending, append(triage, ledger)...)
			for i, f := range lo.triage {
				for _, r := range unused[i] {
					fmt.Fprintf(os.Stderr, "mtgishxc: %s: matches nothing live: %s\n", f, r.Key())
				}
			}
			if n := len(unused[len(unused)-1]); n > 0 {
				fmt.Fprintf(os.Stderr, "mtgishxc: dropped %d stale ledger rows\n", n)
			}
			n, err := mtgish.WriteLedgers(".", rows)
			if err != nil {
				return err
			}
			for _, t := range mtgish.Tiers {
				fmt.Fprintf(os.Stderr, "mtgishxc: wrote %s (%d rows)\n", mtgish.LedgerPath(".", t), n[t])
			}
		} else {
			rows, _ = mtgish.MergeTriage(res.Rows, false, ledger)
		}
		fmt.Print(headline(s, res))
		if s == "ALL" {
			// The kernel sets keep their own headline tables, ahead of
			// the corpus rows.
			for _, k := range mtgish.KernelSets {
				names, err := mtgish.ManifestNames(".", k)
				if err != nil {
					return err
				}
				sub := res.Subset(names)
				writeTierTable(&rep, k+" (kernel set, within ALL)", sub, tierOnly)
				fmt.Print(headline(k, sub))
			}
		}
		writeSet(&rep, s, res, rows, tierOnly)
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	name := "report-" + strings.Join(runs, "-") + ".md"
	path := filepath.Join(out, name)
	if err := os.WriteFile(path, []byte(rep.String()), 0o644); err != nil {
		return err
	}
	fmt.Println("report:", path)
	return nil
}

// writeExclusions puts the exclusions in the report header: the excluded
// cards (counted and named) and the excluded tokens with their reasons.
func writeExclusions(b *strings.Builder, ex mtgish.Exclusions, results map[string]mtgish.Result, runs []string) {
	if len(ex.Card)+len(ex.Token) == 0 {
		b.WriteString("Exclusions (`" + mtgish.ExclusionsFile + "`): none.\n\n")
		return
	}
	fmt.Fprintf(b, "Exclusions (`%s`): %d cards, %d tokens.\n\n", mtgish.ExclusionsFile, len(ex.Card), len(ex.Token))
	for _, s := range runs {
		res := results[s]
		fmt.Fprintf(b, "- %s: %d cards excluded, %d rows excluded\n", s, len(res.ExcludedCards), len(res.Excluded))
	}
	b.WriteString("\n")
	for _, kind := range []struct {
		name string
		m    map[string]string
	}{{"card", ex.Card}, {"token", ex.Token}} {
		keys := make([]string, 0, len(kind.m))
		for k := range kind.m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(b, "- excluded %s `%s`: %s\n", kind.name, k, kind.m[k])
		}
	}
	b.WriteString("\n")
}

func pct(n, d int) string {
	if d == 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f%%", 100*float64(n)/float64(d))
}

func headline(set string, res mtgish.Result) string {
	st := res.Stats()
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %d cards compared (%d without a gorge card, %d without an mtgish card), %d rows\n",
		set, len(res.Pairs), len(res.NoGorge), len(res.NoMtgish), len(res.Rows))
	for _, t := range mtgish.Tiers {
		s := st[t]
		fmt.Fprintf(&b, "  %s faces %d  agree %d (%s)  disagree %d  gap-only %d", t, s.Faces, s.Agree, pct(s.Agree, s.Faces), s.Disagree, s.Gap)
		if s.Excluded > 0 {
			fmt.Fprintf(&b, "  excluded %d", s.Excluded)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// writeTierTable writes a run's per-tier headline table.
func writeTierTable(b *strings.Builder, title string, res mtgish.Result, tierOnly mtgish.Tier) {
	st := res.Stats()
	fmt.Fprintf(b, "## %s\n\n%d cards compared; %d lack a gorge card, %d lack an mtgish card; %d excluded.\n\n",
		title, len(res.Pairs), len(res.NoGorge), len(res.NoMtgish), len(res.ExcludedCards))
	b.WriteString("| tier | faces | agree | agree % | disagree | gap only | excluded only |\n|---|---|---|---|---|---|---|\n")
	for _, t := range mtgish.Tiers {
		if tierOnly != 0 && t != tierOnly {
			continue
		}
		s := st[t]
		fmt.Fprintf(b, "| %s | %d | %d | %s | %d | %d | %d |\n", t, s.Faces, s.Agree, pct(s.Agree, s.Faces), s.Disagree, s.Gap, s.Excluded)
	}
	b.WriteString("\n")
}

func writeSet(b *strings.Builder, set string, res mtgish.Result, rows []mtgish.Row, tierOnly mtgish.Tier) {
	writeTierTable(b, set, res, tierOnly)
	// Rows by tier and class.
	byClass := map[string]map[mtgish.Tier]int{}
	classes := []string{}
	for _, r := range rows {
		if tierOnly != 0 && r.Tier != tierOnly {
			continue
		}
		if byClass[r.Class] == nil {
			byClass[r.Class] = map[mtgish.Tier]int{}
			classes = append(classes, r.Class)
		}
		byClass[r.Class][r.Tier]++
	}
	sort.Strings(classes)
	b.WriteString("| class | T1 | T2 | T3 | T4 | total |\n|---|---|---|---|---|---|\n")
	for _, c := range classes {
		tot := 0
		fmt.Fprintf(b, "| %s", c)
		for _, t := range mtgish.Tiers {
			fmt.Fprintf(b, " | %d", byClass[c][t])
			tot += byClass[c][t]
		}
		fmt.Fprintf(b, " | %d |\n", tot)
	}
	// Top unmapped tokens across both sides.
	um := map[string]int{}
	for _, r := range rows {
		for _, side := range []string{r.Gorge, r.Mtgish} {
			for _, tok := range strings.Fields(side) {
				if mtgish.IsUnmapped(tok) {
					um[tok]++
				}
			}
		}
	}
	if len(um) > 0 {
		type kv struct {
			k string
			v int
		}
		var l []kv
		for k, v := range um {
			l = append(l, kv{k, v})
		}
		sort.Slice(l, func(i, j int) bool { return l[i].v > l[j].v || l[i].v == l[j].v && l[i].k < l[j].k })
		b.WriteString("\nTop unmapped tokens (rows mentioning each):\n\n")
		for i, x := range l {
			if i == 40 {
				fmt.Fprintf(b, "- ... %d more\n", len(l)-40)
				break
			}
			fmt.Fprintf(b, "- `%s` %d\n", x.k, x.v)
		}
	}
	if len(res.NoGorge)+len(res.NoMtgish) > 0 {
		fmt.Fprintf(b, "\nWithout a gorge card: %s\n\nWithout an mtgish card: %s\n", strings.Join(res.NoGorge, "; "), strings.Join(res.NoMtgish, "; "))
	}
	if len(res.ExcludedCards) > 0 {
		fmt.Fprintf(b, "\nExcluded cards (not compared): %s\n", strings.Join(res.ExcludedCards, "; "))
	}
	b.WriteString("\n| card | tier | field | gorge only | mtgish only | class | note |\n|---|---|---|---|---|---|---|\n")
	for _, r := range rows {
		if tierOnly != 0 && r.Tier != tierOnly {
			continue
		}
		fmt.Fprintf(b, "| %s | %s | %s | `%s` | `%s` | %s | %s |\n", r.Card, r.Tier, r.Field, r.Gorge, r.Mtgish, r.Class, r.Note)
	}
	b.WriteString("\n")
	if len(res.Excluded) > 0 {
		fmt.Fprintf(b, "### %s: excluded rows\n\nEvery unmapped token of these rows is excluded; they are not gaps and need no ledger line.\n\n", set)
		b.WriteString("| card | tier | field | gorge only | mtgish only |\n|---|---|---|---|---|\n")
		for _, r := range res.Excluded {
			if tierOnly != 0 && r.Tier != tierOnly {
				continue
			}
			fmt.Fprintf(b, "| %s | %s | %s | `%s` | `%s` |\n", r.Card, r.Tier, r.Field, r.Gorge, r.Mtgish)
		}
		b.WriteString("\n")
	}
}

// printCard prints a card's two summaries side by side, then its rows.
func printCard(name string, reg *cards.Registry, ms map[string]*mtgish.Card) {
	gc, gok := reg.Lookup(name)
	mc, mok := ms[name]
	if !gok || !mok {
		fmt.Printf("%s: gorge has it: %v, mtgish has it: %v\n", name, gok, mok)
		return
	}
	p, rows := mtgish.Compare(name, gc, mc, mtgish.TypeIndexFor(reg))
	n := len(p.Gorge)
	if len(p.Mtgish) > n {
		n = len(p.Mtgish)
	}
	for i := 0; i < n; i++ {
		var l, r []string
		if i < len(p.Gorge) {
			l = strings.Split(strings.TrimRight(p.Gorge[i].Render(), "\n"), "\n")
		}
		if i < len(p.Mtgish) {
			r = strings.Split(strings.TrimRight(p.Mtgish[i].Render(), "\n"), "\n")
		}
		width := 60
		for _, s := range l {
			if len(s) > width {
				width = len(s)
			}
		}
		fmt.Printf("%-*s | %s\n", width, "GORGE", "MTGISH")
		for j := 0; j < len(l) || j < len(r); j++ {
			var a, c string
			if j < len(l) {
				a = l[j]
			}
			if j < len(r) {
				c = r[j]
			}
			fmt.Printf("%-*s | %s\n", width, a, c)
		}
	}
	for _, row := range rows {
		fmt.Printf("  DIFF %s %s: gorge-only %s | mtgish-only %s\n", row.Tier, row.Field, row.Gorge, row.Mtgish)
	}
	fmt.Println()
}
