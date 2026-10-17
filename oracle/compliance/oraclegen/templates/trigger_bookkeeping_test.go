package templates

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/levelb"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
)

// bookkeepingClassRows is the wave-4 "bookkeeping static trigger" class
// (operator decision 2026-10-13: a multi-turn template, no exemption): every
// Static$ True record/reset/cleanup line of the Standard sets. Each names
// what the served scenario must show. (Mikey & Don's Static$ may-play rider,
// the class's tenth row, is served by lb4w2-gen's subtype probe and pinned by
// TestLevelBGeneratorResidueRowsServed.)
var bookkeepingClassRows = []struct {
	card, key string
	// turn3 marks a scenario that advances to p0's next turn.
	turn3 bool
	// probe is a card a step must cast or play (the cause or the flicker).
	probe string
	// notFired marks a last window that pins the consumer NOT firing.
	notFired bool
}{
	{card: "Spider-Man 2099", key: "trigger#0.1", probe: "Forest"},
	{card: "Spider-Man 2099", key: "trigger#0.2", turn3: true, probe: "Forest", notFired: true},
	{card: "Superior Foes of Spider-Man", key: "trigger#0.1", probe: "Cloudshift"},
	{card: "Planetarium of Wan Shi Tong", key: "trigger#0.2", probe: "Ghostly Flicker"},
	{card: "Planetarium of Wan Shi Tong", key: "trigger#0.3", turn3: true, probe: "Preordain"},
	{card: "Desert Were-Worm", key: "trigger#0.2", turn3: true},
	{card: "Oblivious Bookworm", key: "trigger#0.1", turn3: true, probe: "Bolrac-Clan Basher"},
	{card: "Oblivious Bookworm", key: "trigger#0.2", turn3: true, probe: "Bolrac-Clan Basher"},
}

// TestBookkeepingClassRows serves every row of the class and checks the
// scenario reaches its consumer the way the row's kind says.
func TestBookkeepingClassRows(t *testing.T) {
	reg := loadGenRegistry(t)
	// Generate as oraclediff gen does: only probes XMage's database holds.
	installManifestXMageKnown(t)
	for _, tc := range bookkeepingClassRows {
		t.Run(tc.card+" "+tc.key, func(t *testing.T) {
			c, ok := reg.Lookup(tc.card)
			if !ok {
				t.Fatalf("%s not in the corpus", tc.card)
			}
			var req *levelb.Requirement
			for _, r := range levelb.Requirements(c) {
				if r.Key == tc.key {
					r := r
					req = &r
				}
			}
			if req == nil {
				t.Fatalf("%s has no requirement %s", tc.card, tc.key)
			}
			it, skip := GenerateB(reg, tc.card, *req)
			if skip != nil {
				t.Fatalf("not served: %s", skip.Reason)
			}
			res, ok := oraclegen.PlaysThrough(reg, it.Scenario)
			if !ok || len(res.Fails) != 0 {
				t.Fatalf("does not play through gorge: ok=%v fails=%v", ok, res.Fails)
			}
			last := res.Snapshots[len(res.Snapshots)-1]
			if want := int32(1); tc.turn3 {
				want = 3
				if last.Turn != want {
					t.Fatalf("scenario ends on turn %d, want p0's next turn (3)", last.Turn)
				}
			} else if last.Turn != want {
				t.Fatalf("scenario ends on turn %d, want 1", last.Turn)
			}
			if tc.probe != "" {
				found := false
				for _, st := range it.Scenario.Steps {
					if (st.Op == "cast" || st.Op == "play" || st.Op == "activate") && st.Card == "p0:"+tc.probe {
						found = true
					}
				}
				if !found {
					t.Fatalf("no step casts or plays %s: %+v", tc.probe, it.Scenario.Steps)
				}
			}
			for _, st := range it.Scenario.Steps {
				if st.Op != "pass_to" || st.Step == "" {
					continue
				}
				checkpoint := st.Step
				if st.Active != "" {
					checkpoint += "@" + st.Active
				}
				if !containsString(PassToSteps(), checkpoint) {
					t.Fatalf("emitted unadvertised pass_to checkpoint %q", checkpoint)
				}
			}
			pinned := false
			for _, st := range it.Scenario.Steps {
				for _, e := range st.Expect {
					if e.TriggerOnStack == "p0:"+tc.card && e.Want != nil && !*e.Want {
						pinned = true
					}
				}
			}
			if pinned != tc.notFired {
				t.Fatalf("not-fired expectation present = %v, want %v", pinned, tc.notFired)
			}
		})
	}
}

// TestBookkeepingClassCensus is the class ratchet over the committed printed
// sets: the bookkeeping sub-family's requirements are exactly the table's
// rows, and every one is served. A new bookkeeping line in a set, or one the template stops
// serving, fails here by name.
func TestBookkeepingClassCensus(t *testing.T) {
	reg := loadGenRegistry(t)
	installManifestXMageKnown(t)
	root := filepath.Join("..", "..", "..", "..")
	has := func(n string) bool { _, ok := reg.Lookup(n); return ok }
	folded := compliance.FoldedNames(reg)
	paths, err := filepath.Glob(filepath.Join(root, "oracle", "compliance", "printed", "*.json"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no printed sets (err %v)", err)
	}
	seen := map[string]bool{}
	var got []string
	for _, p := range paths {
		set := strings.TrimSuffix(filepath.Base(p), ".json")
		printed, err := compliance.LoadPrinted(filepath.Join(root, "oracle", "compliance", "printed"), set)
		if err != nil {
			t.Fatalf("%s: %v", set, err)
		}
		for _, name := range printed.Cards {
			card, ok := compliance.CorpusNameFold(has, folded, name)
			if !ok {
				continue
			}
			c, _ := reg.Lookup(card)
			for _, r := range levelb.Requirements(c) {
				if r.Sub != levelb.BookkeepingSub || seen[card+"|"+r.Key] {
					continue
				}
				seen[card+"|"+r.Key] = true
				row := card + " " + r.Key
				if _, skip := GenerateB(reg, card, r); skip != nil {
					row += " SKIP " + skip.Reason
				}
				got = append(got, row)
			}
		}
	}
	sort.Strings(got)
	var want []string
	for _, tc := range bookkeepingClassRows {
		want = append(want, tc.card+" "+tc.key)
	}
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("bookkeeping class census moved:\n got:\n  %s\n want (all served):\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}
