package main

import (
	"flag"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

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
	fshadow := fs.String("fshadow", "", "xengine fshadow output dir: its divergence-<n>.json findings get signatures, a class census (census.tsv) and one brief per class under tickets/; without -play the three-way inputs are not needed")
	manifest := fs.String("manifest", "", "with -fshadow: the deck manifest TSV (edh-manifest.tsv) whose decks rank a card; private data, never committed")
	decklists := fs.String("decklists", "", "with -fshadow: the stored decklists (default edh-decks.jsonl beside -manifest)")
	cardsDir := fs.String("cards", ".cards", "with -fshadow: corpus dir, for each implicated card's IR (its primitive and paper factor)")
	setManifests := fs.String("set-manifests", "oracle/compliance/manifests", "with -fshadow: the committed set manifests (a card printed only in an Un- set ranks as digital)")
	gorgeSrc := fs.String("gorge-src", "", "with -fshadow: the gorge module dir, for the briefs' handler file:function (default `go list -m` of gorge)")
	classesPath := fs.String("classes", DefaultClasses, "the diagnosed-classes register: findings a row matches are labelled diagnosed (findings.jsonl); a header-only file changes nothing")
	fs.Parse(args)
	if *out == "" || (*fshadow == "" && (*playDir == "" || *xDiff == "" || *fDiff == "")) {
		return fmt.Errorf("adjudicate needs -out and either -play, -xmage and -forge, or -fshadow")
	}
	for _, p := range []string{*out, *ledger} {
		if p == "" {
			continue
		}
		if err := classify.RefuseRepoPath(p); err != nil {
			return err
		}
	}
	var in adjudicate.Input
	if *playDir != "" {
		if *xDiff == "" || *fDiff == "" {
			return fmt.Errorf("adjudicate -play needs -xmage and -forge")
		}
		if *xRows == "" {
			*xRows = filepath.Join(*playDir, "xmage.jsonl")
		}
		if *fRows == "" {
			*fRows = filepath.Join(*playDir, "forge.jsonl")
		}
		var err error
		if in, err = adjudicate.Load(*playDir, *xDiff, *fDiff, *xRows, *fRows); err != nil {
			return err
		}
	}
	if *fshadow != "" {
		if err := loadFShadow(&in, *fshadow, *manifest, *decklists, *cardsDir, *setManifests, *gorgeSrc); err != nil {
			return err
		}
	}
	var err error
	if *pDiff != "" && *playDir != "" {
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
	if *playDir != "" {
		classes, err := adjudicate.ReadClasses(*classesPath)
		if err != nil {
			return err
		}
		if len(classes) > 0 {
			reg, err := loadReg(*cardsDir)
			if err != nil {
				return fmt.Errorf("-cards %s: %v", *cardsDir, err)
			}
			if err := diagnoseFindings(reg, *playDir, &res, classes); err != nil {
				return err
			}
		}
	}
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
	if res.Census != nil {
		fmt.Printf("fshadow: %d finding(s), %d class(es), %d harness class(es); census.tsv\n", len(in.FShadow), len(res.Census.Classes), len(res.Census.Harness))
	}
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

// loadFShadow adds the fshadow findings under dir to in, with the ranker the
// census reads: the implicated cards' IR from the corpus, manifest deck counts
// (one deck each without -manifest), Un- set membership and handler names.
func loadFShadow(in *adjudicate.Input, dir, manifest, decklists, cardsDir, setManifests, gorgeSrc string) error {
	divs, err := adjudicate.LoadFShadow(dir)
	if err != nil {
		return err
	}
	in.FShadow = divs
	reg, err := loadReg(cardsDir)
	if err != nil {
		return fmt.Errorf("-cards %s: %v", cardsDir, err)
	}
	var unOnly map[string]bool
	if setManifests != "" {
		if unOnly, err = adjudicate.UnOnlyNames(setManifests); err != nil {
			return err
		}
	}
	in.Rank = adjudicate.Ranker{Primitive: adjudicate.RegistryPrimitive(reg), Paper: adjudicate.RegistryPaper(reg, unOnly)}
	if manifest != "" {
		if decklists == "" {
			decklists = filepath.Join(filepath.Dir(manifest), "edh-decks.jsonl")
		}
		if in.Rank.Decks, err = adjudicate.LoadDeckCounts(manifest, decklists); err != nil {
			return err
		}
	}
	if gorgeSrc == "" {
		if b, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/adams-shaun/gorge").Output(); err == nil {
			gorgeSrc = strings.TrimSpace(string(b))
		}
	}
	in.Rank.Suspect = adjudicate.HandlerSuspect(gorgeSrc)
	return nil
}
