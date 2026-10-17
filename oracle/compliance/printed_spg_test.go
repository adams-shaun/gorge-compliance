package compliance

import "testing"

// spgGuests is the ten standard-set Special Guests reprints whose level-A
// problems are exactly the declaration blockers ticket spg1 tracks. The
// printed list is what joins SPG to the standard compliance pass; the ten
// guests must be on it, on the manifest, and covered by committed verdict
// rows (CV-4's level-B pass), so a printed-list revert or a verdict-row drop
// fails this test before the pass silently loses them.
var spgGuests = []string{
	"Akroma's Memorial",
	"Bloom Tender",
	"Condemn",
	"Embercleave",
	"Fiend Artisan",
	"Goblin Bushwhacker",
	"Grim Tutor",
	"Paradise Druid",
	"Sphinx's Tutelage",
	"Temporal Manipulation",
}

// TestSPGPrintedListCoversForgeGuests reads only committed data (the printed
// list, the manifest, the verdict shards), so it runs without the corpus.
func TestSPGPrintedListCoversForgeGuests(t *testing.T) {
	pr, err := LoadPrinted("printed", "SPG")
	if err != nil {
		t.Fatalf("oracle/compliance/printed/SPG.json must be committed: %v", err)
	}
	if pr.Code != "SPG" {
		t.Fatalf("printed file holds code %q, want SPG", pr.Code)
	}
	if len(pr.Cards) < 144 {
		t.Fatalf("printed SPG has %d names, want >= 144 (Forge's Special Guests unique names)", len(pr.Cards))
	}
	if !sortedUnique(pr.Cards) {
		t.Fatalf("printed SPG names are not sorted-unique; the list is committed data, keep it sorted")
	}

	m, err := LoadManifest("manifests", "SPG")
	if err != nil {
		t.Fatalf("oracle/compliance/manifests/SPG.json must be committed: %v", err)
	}
	if len(m.Cards) == 0 {
		t.Fatalf("SPG manifest has no cards; the census precondition is gone")
	}
	exact := map[string]bool{}
	folded := map[string]bool{}
	for _, mc := range m.Cards {
		exact[mc.Name] = true
		folded[FoldName(mc.Name)] = true
	}
	for _, pc := range pr.Cards {
		if !exact[pc] && !folded[FoldName(pc)] {
			t.Errorf("printed SPG name %q matches no manifest name, exact or folded", pc)
		}
	}
	for _, g := range spgGuests {
		if !exact[g] && !folded[FoldName(g)] {
			t.Errorf("guest %q is missing from the SPG manifest", g)
		}
		if !exact[g] {
			t.Errorf("guest %q is not an exact manifest name; a folded carrier must be confirmed and pinned", g)
		}
		onPrinted := false
		for _, pc := range pr.Cards {
			if pc == g || FoldName(pc) == FoldName(g) {
				onPrinted = true
				break
			}
		}
		if !onPrinted {
			t.Errorf("guest %q is missing from printed/SPG.json; the pass would not claim it", g)
		}
	}

	// Every guest keeps at least one committed verdict row with status
	// agree, so the printed list does not strand a card the pass cannot
	// verify. This precondition (CV-4's committed rows) failing is loud.
	v, err := LoadVerdicts("verdicts")
	if err != nil {
		t.Fatalf("committed verdicts must load: %v", err)
	}
	if len(v) == 0 {
		t.Fatalf("no verdict rows loaded from oracle/compliance/verdicts; the glob is not reaching the shards")
	}
	for _, g := range spgGuests {
		rows, ok := v[g]
		if !ok {
			t.Errorf("guest %q has no committed verdict row at all", g)
			continue
		}
		agree := false
		for _, r := range rows {
			if r.Status == "agree" {
				agree = true
				break
			}
		}
		if !agree {
			t.Errorf("guest %q has committed verdict rows but none with status agree", g)
		}
	}
}

func sortedUnique(s []string) bool {
	for i, v := range s {
		if i > 0 && s[i-1] >= v {
			return false
		}
	}
	return len(s) > 0
}
