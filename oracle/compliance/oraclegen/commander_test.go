package oraclegen

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

const cmdrName = "Isamaru, Hound of Konda"

func cmdrItem(steps []Step) Item {
	return Item{Scenario: Scenario{
		Name: "cmdr", CR: []string{"903.8"}, Why: "commander",
		Format: FormatCommander,
		Setup: map[string]Seat{
			"p0": {Command: []string{cmdrName}, Hand: []string{"Plains"}},
			"p1": {Command: []string{cmdrName}},
		},
		Steps: steps,
	}}
}

func runCmdr(t *testing.T, steps []Step) rules.OracleResult {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	res, err := rules.RunOracleScenarioJSON(reg, cmdrItem(steps).Raw())
	if err != nil {
		t.Fatalf("commander item did not run: %v", err)
	}
	return res
}

func TestCommanderItemSetup(t *testing.T) {
	res := runCmdr(t, []Step{})
	if len(res.Fails) != 0 {
		t.Fatalf("fails: %v", res.Fails)
	}
	if len(res.Snapshots) == 0 {
		t.Fatal("no snapshots")
	}
	snap := res.Snapshots[0]
	for i := 0; i < 2; i++ {
		if got := snap.Players[i].Life; got != 40 {
			t.Errorf("seat %d life = %d, want 40 (CR 903.7)", i, got)
		}
	}
	p0 := snap.Players[0]
	if len(p0.Command) != 1 || p0.Command[0] != cmdrName {
		t.Errorf("p0 command = %v, want [%s]", p0.Command, cmdrName)
	}
	for _, z := range append(append([]string{}, p0.LibraryTop...), p0.Hand...) {
		if z == cmdrName {
			t.Errorf("%s is also in library_top/hand: %+v", cmdrName, p0)
		}
	}
}

func TestCommanderCastFromCommandZone(t *testing.T) {
	res := runCmdr(t, []Step{{Op: "cast", Seat: 0, Card: "p0:" + cmdrName, Mana: "W"}})
	if len(res.Fails) != 0 {
		t.Fatalf("fails: %v\n%s", res.Fails, strings.Join(res.Transcript, "\n"))
	}
	last := res.Snapshots[len(res.Snapshots)-1]
	if len(last.Players[0].Command) != 0 {
		t.Errorf("command zone after cast = %v, want empty", last.Players[0].Command)
	}
	found := false
	for _, s := range last.Stack {
		if strings.Contains(s.Source, "Isamaru") {
			found = true
		}
	}
	if !found {
		t.Errorf("no Isamaru on the stack: %+v", last.Stack)
	}
}

func TestCommanderTaxOnRecast(t *testing.T) {
	steps := func(mana string) []Step {
		return []Step{
			{Op: "cast", Seat: 0, Card: "p0:" + cmdrName, Mana: "W"},
			{Op: "resolve"},
			{Op: "move", Seat: 0, Card: "p0:" + cmdrName, To: "command"},
			{Op: "cast", Seat: 0, Card: "p0:" + cmdrName, Mana: mana},
		}
	}
	res := runCmdr(t, steps("W"))
	named := false
	for _, f := range res.Fails {
		if strings.Contains(f, "step 3") {
			named = true
		}
	}
	if !named {
		t.Errorf("recast with {W} should fail at step 3 (CR 903.8); fails: %v", res.Fails)
	}
	res = runCmdr(t, steps("WCC"))
	if len(res.Fails) != 0 {
		t.Fatalf("recast with {W}{C}{C} failed: %v\n%s", res.Fails, strings.Join(res.Transcript, "\n"))
	}
	last := res.Snapshots[len(res.Snapshots)-1]
	found := false
	for _, s := range last.Stack {
		if strings.Contains(s.Source, "Isamaru") {
			found = true
		}
	}
	if !found {
		t.Errorf("no Isamaru on the stack after the taxed recast: %+v", last.Stack)
	}
}

func TestItemWithoutFormatKeepsBytes(t *testing.T) {
	it := Item{Scenario: Scenario{
		Name: "plain", CR: []string{"1"}, Why: "w",
		Setup: map[string]Seat{"p0": {Hand: []string{"Plains"}}},
		Steps: []Step{},
	}}
	whole, err := json.Marshal(it)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range [][]byte{it.Raw(), whole} {
		s := string(b)
		if strings.Contains(s, `"format"`) || strings.Contains(s, `"command"`) {
			t.Errorf("constructed item marshals a commander field: %s", s)
		}
	}
}
