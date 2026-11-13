package main

import (
	"flag"
	"fmt"
	"path/filepath"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/adjudicate"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/classify"
)

// runAdjudicate gives the items that diverged from a follower a three-way
// verdict and drafts gorge tickets from the gorge_suspect clusters. With
// -phase it also places phase.rs as a fourth column (fourway.jsonl,
// patterns4.tsv), leaving the three-way output byte for byte as it was. Everything
// it writes is off-repo; it never writes a verdict row or a ruling.
func runAdjudicate(args []string) error {
	fs := flag.NewFlagSet("adjudicate", flag.ExitOnError)
	playDir := fs.String("play", "", "play output dir (required)")
	xDiff := fs.String("xmage", "", "xengine diff dir of the XMage follower (required)")
	fDiff := fs.String("forge", "", "xengine diff dir of the Forge follower (required)")
	xRows := fs.String("xmage-rows", "", "XMage rows (default <play>/xmage.jsonl)")
	fRows := fs.String("forge-rows", "", "Forge rows (default <play>/forge.jsonl)")
	pDiff := fs.String("phase", "", "xengine diff dir of the phase.rs follower: adds the four-way table (fourway.jsonl, patterns4.tsv); the three-way output is unchanged")
	pRows := fs.String("phase-rows", "", "phase.rs rows (default <play>/phase.jsonl)")
	out := fs.String("out", "", "output dir, outside the repository (required)")
	ledger := fs.String("ledger", "", "also write the compact ledger shards here (outside the repository; opt-in, nothing is committed by this tool)")
	forgeRef := fs.String("forge-ref", "", "FORGE_ORACLE_REF[:12] stamped on ledger rows")
	verdicts := fs.String("verdicts", compliance.VerdictDir, "committed verdict directory (a card with an agree row there needs two findings to be drafted)")
	fs.Parse(args)
	if *playDir == "" || *xDiff == "" || *fDiff == "" || *out == "" {
		return fmt.Errorf("adjudicate needs -play, -xmage, -forge and -out")
	}
	for _, p := range []string{*out, *ledger} {
		if p == "" {
			continue
		}
		if err := classify.RefuseRepoPath(p); err != nil {
			return err
		}
	}
	if *xRows == "" {
		*xRows = filepath.Join(*playDir, "xmage.jsonl")
	}
	if *fRows == "" {
		*fRows = filepath.Join(*playDir, "forge.jsonl")
	}
	in, err := adjudicate.Load(*playDir, *xDiff, *fDiff, *xRows, *fRows)
	if err != nil {
		return err
	}
	if *pDiff != "" {
		if *pRows == "" {
			*pRows = filepath.Join(*playDir, "phase.jsonl")
		}
		if err := adjudicate.LoadPhase(&in, *pDiff, *pRows); err != nil {
			return err
		}
	}
	if in.HasAgree, err = adjudicate.CommittedAgree(*verdicts); err != nil {
		return err
	}
	res := adjudicate.Run(in)
	if err := adjudicate.Write(*out, res); err != nil {
		return err
	}
	if *ledger != "" {
		if err := adjudicate.WriteLedger(*ledger, res.Rows, *forgeRef); err != nil {
			return err
		}
	}
	hist := map[string]int{}
	for _, r := range res.Rows {
		hist[r.Pattern]++
	}
	fmt.Printf("adjudicated %d items into %s: %d cluster(s), %d ticket draft(s)\n", len(res.Rows), *out, len(res.Clusters), len(res.Drafts))
	for _, p := range []string{"AGREE3", "GF|X", "FX|G", "GX|F", "G|F|X", "GF|–", "G|F|–", "F harness", "INCONSISTENT"} {
		if hist[p] > 0 {
			fmt.Printf("  %-13s %d\n", p, hist[p])
		}
	}
	if res.Four != nil {
		printFour(res.Four)
	}
	return nil
}

// printFour prints the four-way table's pattern and phase.rs vote histograms.
func printFour(rows []adjudicate.FourRow) {
	four, votes := map[string]int{}, map[string]int{}
	for _, r := range rows {
		four[r.Four]++
		votes[r.Phase]++
	}
	fmt.Printf("four-way (G F P X): %d item(s)\n", len(rows))
	for _, k := range sortedKeys(four) {
		fmt.Printf("  %-13s %d\n", k, four[k])
	}
	fmt.Printf("phase.rs:")
	for _, k := range sortedKeys(votes) {
		fmt.Printf(" %s=%d", k, votes[k])
	}
	fmt.Println()
}
