package mtgish

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeTemp(t *testing.T, name, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReadExclusions(t *testing.T) {
	ok := "# header\n\ncard\tTest Card\tan Un-card\ntoken\tunmapped:Action.Visit\tAttractions\n"
	ex, err := ReadExclusions(writeTemp(t, "ok.tsv", ok))
	if err != nil {
		t.Fatal(err)
	}
	if ex.Card["Test Card"] != "an Un-card" || ex.Token["unmapped:Action.Visit"] != "Attractions" {
		t.Errorf("read %+v", ex)
	}
	for name, body := range map[string]string{
		"columns":   "card\tTest Card\n",
		"reason":    "card\tTest Card\t  \n",
		"kind":      "set\tUNF\tUn-set\n",
		"token":     "token\tvisit\tnot an unmapped spelling\n",
		"bare":      "token\tunmapped:\tempty name\n",
		"duplicate": "card\tA\tx\ncard\tA\ty\n",
		"padded":    "card\t A\tx\n",
	} {
		if _, err := ReadExclusions(writeTemp(t, name+".tsv", body)); err == nil {
			t.Errorf("%s: accepted %q", name, body)
		}
	}
}

func TestRowExcludedAndUnused(t *testing.T) {
	ex := Exclusions{
		Card:  map[string]string{"Gone": "r", "Never Seen": "r"},
		Token: map[string]string{"unmapped:A.X": "r", "unmapped:A.Unused": "r"},
	}
	for _, c := range []struct {
		r    Row
		want bool
	}{
		{Row{Gorge: "unmapped:A.X", Mtgish: "-"}, true},
		{Row{Gorge: "draw:1", Mtgish: "unmapped:A.X"}, true},
		{Row{Gorge: "unmapped:A.X unmapped:A.Y", Mtgish: "-"}, false},
		{Row{Gorge: "draw:1", Mtgish: "draw:2"}, false},
	} {
		if got := ex.RowExcluded(c.r); got != c.want {
			t.Errorf("RowExcluded(%q / %q) = %v, want %v", c.r.Gorge, c.r.Mtgish, got, c.want)
		}
	}
	res := Result{
		ExcludedCards: []string{"Gone"},
		Excluded:      []Row{{Card: "B", Gorge: "unmapped:A.X", Mtgish: "-"}},
	}
	want := []string{"card\tNever Seen", "token\tunmapped:A.Unused"}
	if got := ex.Unused(res); !reflect.DeepEqual(got, want) {
		t.Errorf("Unused = %q, want %q", got, want)
	}
}

func TestStatsCountsExcludedFaces(t *testing.T) {
	res := Result{
		Pairs: []Pair{
			{Card: "A", Gorge: []*Summary{{Card: "A"}}},
			{Card: "B", Gorge: []*Summary{{Card: "B"}}},
			{Card: "C", Gorge: []*Summary{{Card: "C"}}},
		},
		Rows: []Row{
			{Card: "B", Tier: T4, Gorge: "unmapped:A.Y", Mtgish: "-"},
			{Card: "C", Tier: T4, Gorge: "draw:1", Mtgish: "-"},
		},
		Excluded: []Row{
			{Card: "A", Tier: T4, Gorge: "unmapped:A.X", Mtgish: "-"},
			{Card: "B", Tier: T4, Gorge: "unmapped:A.X", Mtgish: "-"},
		},
	}
	got := res.Stats()[T4]
	if want := (TierStats{Faces: 3, Agree: 0, Disagree: 1, Gap: 1, Excluded: 1}); got != want {
		t.Errorf("T4 stats %+v, want %+v", got, want)
	}
	sub := res.Subset([]string{"A", "Z"})
	if len(sub.Pairs) != 1 || len(sub.Excluded) != 1 || len(sub.Rows) != 0 || !reflect.DeepEqual(sub.NoMtgish, []string{"Z"}) {
		t.Errorf("Subset %+v", sub)
	}
}

func TestMergeTriage(t *testing.T) {
	live := []Row{
		{Card: "A", Tier: T4, Field: "effects", Gorge: "draw:1", Mtgish: "draw:2"},
		{Card: "B", Tier: T4, Field: "effects", Gorge: "unmapped:A.Y", Mtgish: "-"},
		{Card: "C", Tier: T1, Field: "pt", Gorge: "1/1", Mtgish: "2/2"},
		{Card: "D", Tier: T3, Field: "targets", Gorge: "creature", Mtgish: "creaturex0..1"},
	}
	tri := []Row{
		{Card: "A", Tier: T4, Field: "effects", Gorge: "draw:1", Mtgish: "draw:2", Class: "mtgish-bug", Note: "from triage"},
		{Card: "Gone", Tier: T1, Field: "pt", Gorge: "1/1", Mtgish: "-", Class: "equivalent", Note: "x"},
	}
	ledger := []Row{
		{Card: "A", Tier: T4, Field: "effects", Gorge: "draw:1", Mtgish: "draw:2", Class: "pending", Note: "old"},
		{Card: "C", Tier: T1, Field: "pt", Gorge: "1/1", Mtgish: "2/2", Class: "forge-bug", Note: "kept"},
	}
	out, unused := MergeTriage(live, true, tri, ledger)
	classes := []string{}
	for _, r := range out {
		classes = append(classes, r.Class+":"+r.Note)
	}
	want := []string{"mtgish-bug:from triage", "normalizer:unmapped token", "forge-bug:kept", "pending:not yet triaged"}
	if !reflect.DeepEqual(classes, want) {
		t.Errorf("merged %q, want %q", classes, want)
	}
	if len(unused[0]) != 1 || unused[0][0].Card != "Gone" || len(unused[1]) != 0 {
		t.Errorf("unused %+v", unused)
	}
	if out, _ := MergeTriage(live[3:], false); out[0].Class != "untriaged" {
		t.Errorf("without -pending a new disagreement is %q, want untriaged", out[0].Class)
	}
}

func TestLedgersRoundTripByTier(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, KnownDir), 0o755); err != nil {
		t.Fatal(err)
	}
	rows := []Row{
		{Card: "A", Tier: T4, Field: "effects", Gorge: "draw:1", Mtgish: "draw:2", Class: "mtgish-bug", Note: "n"},
		{Card: "B", Tier: T1, Field: "pt", Gorge: "1/1", Mtgish: "2/2", Class: "digital-variant", Note: "n"},
	}
	n, err := WriteLedgers(root, rows)
	if err != nil {
		t.Fatal(err)
	}
	if n[T1] != 1 || n[T2] != 0 || n[T4] != 1 {
		t.Errorf("written %v", n)
	}
	back, err := ReadLedgers(root)
	if err != nil || len(back) != 2 {
		t.Fatalf("read back %+v, %v", back, err)
	}
	// A row filed under another tier's file is an error.
	b, _ := os.ReadFile(LedgerPath(root, T4))
	if err := os.WriteFile(LedgerPath(root, T2), b, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadLedgers(root); err == nil || !strings.Contains(err.Error(), "T2 ledger") {
		t.Errorf("misfiled row: err %v", err)
	}
}
