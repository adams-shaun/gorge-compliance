package compliance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// forgeAgree is a Forge-only row a Forge pass may merge.
func forgeAgree(card, template, sha string) VerdictRow {
	return VerdictRow{
		Card: card, Template: template, ID: template, ScenarioSHA: sha,
		Source: SourceForge, ForgeRef: strings.Repeat("a", 40), ForgeDriver: "0123456789ab",
		Status: StatusAgree, Frozen: []Frozen{{Field: "checkpoints", Value: "new"}},
	}
}

func readShard(t *testing.T, dir, card string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, Shard(card)+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestMergeForgeVerdictsNeverReplacesAnXMageComparison(t *testing.T) {
	dir := t.TempDir()
	// One card per shard, so a kept row's whole shard file must stay byte
	// for byte identical.
	old := []VerdictRow{
		{Card: "Alpha", Template: "cast-resolve", ID: "a", ScenarioSHA: "old-a", XMageRef: "xm", Status: StatusAgree,
			Frozen: []Frozen{{At: "step 1", Field: "p0.life", Value: "20"}}},
		{Card: "Bravo", Template: "cast-resolve", ID: "b", ScenarioSHA: "old-b", XMageRef: "xm", Status: StatusDiverge,
			Detail: `step 1 p0.life: gorge "20", xmage "17"`},
		{Card: "Charlie", Template: "cast-resolve", ID: "c", ScenarioSHA: "old-c", XMageRef: "xm", Status: StatusHarness,
			Detail: "xmage: harness"},
		{Card: "Delta", Template: "cast-resolve", ID: "d", ScenarioSHA: "old-d", XMageRef: "xm", Status: StatusXMageLacks,
			Detail: "xmage: no card"},
		{Card: "Echo", Template: "cast-resolve", ID: "e", ScenarioSHA: "old-e", XMageRef: "xm", Status: StatusXMageWrong,
			Detail: `step 1 p0.life: gorge "20", xmage "17"`, Ruling: "CR 1", Frozen: []Frozen{{Field: "checkpoints", Value: "old"}}},
		{Card: "Foxtrot", Template: "cast-resolve", ID: "f", ScenarioSHA: "old-f", Source: SourceForge,
			ForgeRef: "old", ForgeDriver: "old", Status: StatusAgree, Frozen: []Frozen{{Field: "checkpoints", Value: "old"}}},
	}
	if err := MergeVerdicts(dir, old); err != nil {
		t.Fatal(err)
	}
	untouched := map[string][]byte{}
	for _, card := range []string{"Alpha", "Bravo", "Echo"} {
		untouched[card] = readShard(t, dir, card)
	}

	var rows []VerdictRow
	for _, card := range []string{"Alpha", "Bravo", "Charlie", "Delta", "Echo", "Foxtrot", "Golf"} {
		rows = append(rows, forgeAgree(card, "cast-resolve", "new-"+strings.ToLower(card)))
	}
	wrote, kept, err := MergeForgeVerdicts(dir, rows)
	if err != nil {
		t.Fatal(err)
	}
	if wrote != 4 || kept != 3 {
		t.Fatalf("wrote %d, kept %d; want 4, 3", wrote, kept)
	}
	for _, card := range []string{"Alpha", "Bravo", "Echo"} {
		if got := readShard(t, dir, card); string(got) != string(untouched[card]) {
			t.Errorf("%s's XMage row changed:\n got %s\nwant %s", card, got, untouched[card])
		}
	}
	all, err := LoadVerdicts(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, card := range []string{"Charlie", "Delta", "Foxtrot", "Golf"} {
		got, ok := all[card]["cast-resolve"]
		if !ok || !got.FromForge() || got.ScenarioSHA != "new-"+strings.ToLower(card) {
			t.Errorf("%s = %+v, want a Forge row at the new sha", card, got)
		}
	}

	// A row that is not Forge's agree writes nothing at all.
	for _, tc := range []struct {
		name string
		row  VerdictRow
	}{
		{name: "not Forge", row: VerdictRow{Card: "Hotel", Template: "cast-resolve", Status: StatusAgree, ScenarioSHA: "s"}},
		{name: "not agree", row: VerdictRow{Card: "Hotel", Template: "cast-resolve", Source: SourceForge, Status: StatusDiverge, ScenarioSHA: "s"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := t.TempDir()
			_, _, err := MergeForgeVerdicts(bad, []VerdictRow{forgeAgree("India", "cast-resolve", "s"), tc.row})
			if err == nil || !strings.Contains(err.Error(), "is not a Forge agree row") {
				t.Fatalf("err = %v, want a refusal naming the row", err)
			}
			entries, readErr := os.ReadDir(bad)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if len(entries) != 0 {
				t.Fatalf("a refused merge wrote %d files", len(entries))
			}
		})
	}
}

func TestMergeVerdictsXMageSupersedesForge(t *testing.T) {
	dir := t.TempDir()
	var forge []VerdictRow
	for _, card := range []string{"Papa", "Quebec", "Romeo", "Sierra"} {
		forge = append(forge, forgeAgree(card, "cast-resolve", "s1"))
	}
	if err := MergeVerdicts(dir, forge); err != nil {
		t.Fatal(err)
	}
	xmage := []VerdictRow{
		{Card: "Papa", Template: "cast-resolve", ScenarioSHA: "s1", XMageRef: "xm", Status: StatusAgree,
			Frozen: []Frozen{{Field: "checkpoints", Value: "x"}}},
		{Card: "Quebec", Template: "cast-resolve", ScenarioSHA: "s1", XMageRef: "xm", Status: StatusDiverge,
			Detail: `step 1 p0.life: gorge "20", xmage "17"`},
		{Card: "Romeo", Template: "cast-resolve", ScenarioSHA: "s1", XMageRef: "xm", Status: StatusHarness, Detail: "xmage: harness"},
		{Card: "Sierra", Template: "cast-resolve", ScenarioSHA: "s2", XMageRef: "xm", Status: StatusHarness, Detail: "xmage: harness"},
	}
	if err := MergeVerdicts(dir, xmage); err != nil {
		t.Fatal(err)
	}
	all, err := LoadVerdicts(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, card := range []string{"Papa", "Quebec"} {
		got := all[card]["cast-resolve"]
		if got.FromForge() || got.ScenarioSHA != "s1" {
			t.Errorf("%s = %+v, want the XMage comparison at s1", card, got)
		}
	}
	if got := all["Romeo"]["cast-resolve"]; !got.FromForge() || got.ScenarioSHA != "s1" {
		t.Errorf("Romeo = %+v, want the Forge row kept when XMage merely harnessed it", got)
	}
	if got := all["Sierra"]["cast-resolve"]; got.FromForge() || got.Status != StatusHarness || got.ScenarioSHA != "s2" {
		t.Errorf("Sierra = %+v, want the XMage harness row at the new sha", got)
	}
}

// TestMergeForgeVerdictsRowJSON pins the privacy rule: a merged row carries no
// field a Forge snapshot could fill.
func TestMergeForgeVerdictsRowJSON(t *testing.T) {
	b, err := json.Marshal(forgeAgree("Shock", "cast-resolve", "s"))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{`"detail"`, `"msg"`, `"xmage"`, `"gorge"`, `"ruling"`, `"xmage_ref"`, `"review"`} {
		if strings.Contains(string(b), forbidden) {
			t.Errorf("Forge row JSON carries %s: %s", forbidden, b)
		}
	}
}
