package main

// forge-verdicts writes Forge-only verdict rows from a Forge pass. A level-A
// scenario gorge and Forge agree on earns an agree row with source "forge"
// when XMage has not compared it; an XMage comparison always supersedes it.
// Nothing Forge produced (a snapshot value, a message, script text) enters
// the row: only gorge's own frozen expectation and the two pins do.

import (
	"encoding/json"
	"flag"
	"fmt"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/gate"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclediff"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen/templates"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
)

func runForgeVerdicts(args []string) error {
	fs := flag.NewFlagSet("forge-verdicts", flag.ExitOnError)
	dir := fs.String("cards", ".cards", "corpus dir")
	scen := fs.String("scenarios", "", "scenario JSONL (from gen)")
	forgeDiff := fs.String("forge-diff", "", "forge-diff JSONL")
	ref := fs.String("oracle-ref", "", "FORGE_ORACLE_REF of this pass (full ref)")
	driver := fs.String("driver", "", "Forge driver sha")
	verdicts := fs.String("verdicts", compliance.VerdictDir, "committed verdict directory")
	dryRun := fs.Bool("dry-run", false, "count the rows without writing")
	fs.Parse(args)
	if *scen == "" || *forgeDiff == "" || *ref == "" || *driver == "" {
		return fmt.Errorf("forge-verdicts needs -scenarios, -forge-diff, -oracle-ref and -driver")
	}
	reg, err := loadReg(*dir)
	if err != nil {
		return err
	}
	frows, _, err := readForgeRows(*forgeDiff)
	if err != nil {
		return err
	}
	rows, counts, err := forgeVerdictRows(reg, *scen, frows, *ref, *driver)
	if err != nil {
		return err
	}
	if *dryRun {
		printCounts("forge-verdicts", counts)
		fmt.Printf("forge-verdicts: dry run, %d rows would be written\n", len(rows))
	} else {
		wrote, kept, err := compliance.MergeForgeVerdicts(*verdicts, rows)
		if err != nil {
			return err
		}
		printCounts("forge-verdicts", counts)
		fmt.Printf("forge-verdicts: wrote %d, kept %d (an XMage comparison stands)\n", wrote, kept)
	}
	fmt.Println("record the split: go run -trimpath ./cmd/oraclediff status -all -sets <SETS> -write-ratchet")
	return nil
}

// forgeVerdictRows turns a gen file and its forge-diff rows into the Forge-only
// verdict rows a Forge pass may write. It counts every scenario it rejects
// under its first matching bucket: a level-B template, a scenario Forge never
// answered, a Forge result that is not AGREE, or a gorge replay error.
func forgeVerdictRows(reg *cards.Registry, scenPath string, frows map[string]ForgeRow, ref, driver string) ([]compliance.VerdictRow, map[string]int, error) {
	levelA := make(map[string]bool, len(templates.All))
	for _, t := range templates.All {
		levelA[t.ID] = true
	}
	counts := map[string]int{}
	var rows []compliance.VerdictRow
	err := readLines(scenPath, func(b []byte) error {
		var it oraclegen.Item
		if err := json.Unmarshal(b, &it); err != nil {
			return err
		}
		sha := gate.ItemSHA(it)
		if !levelA[it.Template] {
			counts["level-B template"]++
			return nil
		}
		f, ok := frows[sha]
		if !ok {
			counts["no forge-diff row"]++
			return nil
		}
		if f.Verdict.Status != oraclediff.Agree {
			counts["forge-diff "+string(f.Verdict.Status)]++
			return nil
		}
		g, gerr := rules.RunOracleScenarioJSON(reg, it.Raw())
		if gerr != nil {
			counts["gorge replay error"]++
			return nil
		}
		rows = append(rows, compliance.VerdictRow{
			Card:        it.Card,
			Template:    it.Template,
			ID:          it.ID,
			ScenarioSHA: sha,
			Source:      compliance.SourceForge,
			ForgeRef:    ref,
			ForgeDriver: short12(driver),
			Status:      compliance.StatusAgree,
			Frozen:      oraclediff.FreezeOpts(g, it.Compare, forgeIgnore(it)...),
		})
		counts["written"]++
		return nil
	})
	return rows, counts, err
}
