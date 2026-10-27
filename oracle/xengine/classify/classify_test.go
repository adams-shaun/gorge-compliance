package classify

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclediff"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/lower"
	"github.com/adams-shaun/gorge/engine/rules"
)

// snap is one hand-built checkpoint: two seats, 20 life, an empty board.
func snap(name string) rules.OracleSnapshot {
	pl := func(seat int) rules.OracleSnapPlayer {
		return rules.OracleSnapPlayer{Seat: seat, Life: 20, Hand: []string{"Forest"}, LibraryCount: 30}
	}
	return rules.OracleSnapshot{
		Checkpoint: name, Turn: 1, Step: "main1",
		Players: []rules.OracleSnapPlayer{pl(0), pl(1)},
	}
}

// fixture is a three-checkpoint item (setup, two passes) whose second pass
// carries tape entry 1; draws is the RNG entry 1 consumed.
func fixture(draws uint64) (lower.Lowered, *game.Tape, rules.OracleResult) {
	item := lower.Lowered{
		Item: oraclegen.Item{ID: "xe:T:0:0:tape", Template: lower.Template, Scenario: oraclegen.Scenario{
			Steps: []oraclegen.Step{{Op: "pass"}, {Op: "pass"}},
		}},
		StepEntry: []int{0, 1},
	}
	tp := &game.Tape{
		Decks:   [2]game.Deck{{Cards: []string{"Forest", "Island"}}, {Cards: []string{"Swamp"}}},
		Entries: []game.Entry{{Kind: "priority"}, {Kind: "priority", RNG: draws}},
	}
	g := rules.OracleResult{Snapshots: []rules.OracleSnapshot{snap("setup"), snap("step 0 (pass)"), snap("step 1 (pass)")}}
	return item, tp, g
}

// follower is g's snapshots as a follower row, then edited by edit.
func follower(g rules.OracleResult, edit func(*oraclediff.XResult)) oraclediff.XResult {
	x := oraclediff.XResult{ID: "xe:T:0:0:tape"}
	for _, s := range g.Snapshots {
		c := s
		c.Players = append([]rules.OracleSnapPlayer(nil), s.Players...)
		x.Snapshots = append(x.Snapshots, c)
	}
	if edit != nil {
		edit(&x)
	}
	return x
}

func TestClassifyTable(t *testing.T) {
	token := func(name string) rules.OracleSnapPerm {
		return rules.OracleSnapPerm{Ref: "p0:token:T", Name: name, Token: true, Types: []string{"Creature", name}, Colors: "W", PT: "1/1"}
	}
	cases := []struct {
		name  string
		draws uint64
		g     func(*rules.OracleResult)
		edit  func(*oraclediff.XResult)
		want  string
	}{
		{"agree", 1, nil, nil, ClassAgree},
		{"harness", 1, nil, func(x *oraclediff.XResult) { x.Harness = "cannot express the step" }, ClassHarness},
		{"xmage_lacks", 1, nil, func(x *oraclediff.XResult) { x.Harness = "Couldn't find a card: Island" }, ClassXMageLacks},
		{"decision_shape strict_miss", 1, nil, func(x *oraclediff.XResult) { x.StrictMiss = "asked a target" }, ClassDecisionShape},
		{"decision_shape leftover", 1, nil, func(x *oraclediff.XResult) { x.Leftover = "1 answer" }, ClassDecisionShape},
		{"rng", 1, nil, func(x *oraclediff.XResult) { x.Snapshots[2].Players[1].Hand = []string{"Island"} }, ClassRNG},
		{"projection_gap other", 0, nil, func(x *oraclediff.XResult) { x.Snapshots[2].Players[1].Hand = []string{"other:Flurry"} }, ClassProjectionGap},
		{"projection_gap token name",
			0,
			func(g *rules.OracleResult) { g.Snapshots[2].Permanents = []rules.OracleSnapPerm{token("Soldier")} },
			func(x *oraclediff.XResult) { x.Snapshots[2].Permanents = []rules.OracleSnapPerm{token("Knight")} },
			ClassProjectionGap},
		{"diverge", 1, nil, func(x *oraclediff.XResult) { x.Snapshots[2].Players[0].Life = 19 }, ClassDiverge},
		{"token missing is a diverge",
			0,
			func(g *rules.OracleResult) { g.Snapshots[2].Permanents = []rules.OracleSnapPerm{token("Soldier")} },
			func(x *oraclediff.XResult) { x.Snapshots[2].Permanents = nil }, ClassDiverge},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			item, tp, g := fixture(c.draws)
			if c.g != nil {
				c.g(&g)
			}
			x := follower(g, nil)
			if c.edit != nil {
				// A follower row starts as gorge's snapshots.
				x = follower(g, c.edit)
			}
			f := Classify(item, tp, g, x, "xmage")
			if f.Class != c.want {
				t.Fatalf("class %q, want %q (finding %+v)", f.Class, c.want, f)
			}
			if c.want != ClassAgree && c.want != ClassHarness && c.want != ClassXMageLacks && c.want != ClassDecisionShape && f.Checkpoint != "step 1 (pass)" {
				t.Errorf("checkpoint %q, want step 1 (pass)", f.Checkpoint)
			}
		})
	}
	t.Run("lower_gap", func(t *testing.T) {
		item, _, _ := fixture(0)
		f := GapFinding(item, lower.Check{Status: lower.StatusGap, Step: 1, Field: "p0.life", Gorge: "20", Tape: "19"})
		if f.Class != ClassLowerGap || f.Field != "p0.life" || f.Engine != "" {
			t.Fatalf("%+v", f)
		}
		if Credited(f.Class) {
			t.Error("lower_gap must not be credited")
		}
	})
}

// A difference in a hand with no engine draw on the tape since the last
// agreeing checkpoint is an engine divergence, not a shuffle.
func TestClassifyRngNeedsDraw(t *testing.T) {
	handDiff := func(x *oraclediff.XResult) { x.Snapshots[2].Players[1].Hand = []string{"Island"} }
	for _, c := range []struct {
		draws uint64
		want  string
	}{{0, ClassDiverge}, {2, ClassRNG}} {
		item, tp, g := fixture(c.draws)
		f := Classify(item, tp, g, follower(g, handDiff), "xmage")
		if f.Class != c.want {
			t.Errorf("draws %d: class %q, want %q", c.draws, f.Class, c.want)
		}
	}
	// A draw on an earlier, agreeing step does not excuse a later difference.
	item, tp, g := fixture(0)
	tp.Entries[0].RNG = 3
	if f := Classify(item, tp, g, follower(g, handDiff), "xmage"); f.Class != ClassDiverge {
		t.Errorf("draw before the last agreeing checkpoint: class %q, want diverge", f.Class)
	}
	// ... and a draw before the setup checkpoint counts only for a setup difference.
	item, tp, g = fixture(0)
	tp.GenesisRNG = 7
	setup := func(x *oraclediff.XResult) { x.Snapshots[0].Players[0].LibraryTop = []string{"Forest"} }
	if f := Classify(item, tp, g, follower(g, setup), "xmage"); f.Class != ClassRNG || f.Step != -1 {
		t.Errorf("setup library difference after the genesis shuffle: %+v", f)
	}
}

func TestReproRefusesRepoPath(t *testing.T) {
	item, tp, g := fixture(0)
	f := Classify(item, tp, g, follower(g, func(x *oraclediff.XResult) { x.Snapshots[2].Players[0].Life = 19 }), "xmage")
	// The package directory is the test's working directory, inside the module.
	if err := WriteRepro(filepath.Join(".", "finds"), f, item, g, oraclediff.XResult{}); err == nil {
		os.RemoveAll("finds")
		t.Fatal("WriteRepro wrote inside the repository")
	}
	root, _ := filepath.Abs("../../..")
	if err := WriteRepro(filepath.Join(root, "finds-x"), f, item, g, oraclediff.XResult{}); err == nil {
		os.RemoveAll(filepath.Join(root, "finds-x"))
		t.Fatal("WriteRepro wrote into the module root")
	}
	// Outside the module it writes the whole directory.
	dir := filepath.Join(t.TempDir(), "r")
	x := follower(g, func(x *oraclediff.XResult) { x.Snapshots[2].Players[0].Life = 19 })
	if err := WriteRepro(dir, f, item, g, x); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"scenario.json", "gorge.json", "xmage.json", "verdict.json", "REPRO.md", "input.jsonl"} {
		if _, err := os.Stat(filepath.Join(dir, n)); err != nil {
			t.Errorf("missing %s", n)
		}
	}
	md, _ := os.ReadFile(filepath.Join(dir, "REPRO.md"))
	for _, want := range []string{"go run -trimpath ./cmd/xengine diff", "scripts/xmage-oracle-run.sh"} {
		if !strings.Contains(string(md), want) {
			t.Errorf("REPRO.md lacks %q:\n%s", want, md)
		}
	}
}
