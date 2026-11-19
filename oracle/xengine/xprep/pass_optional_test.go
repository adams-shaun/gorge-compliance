package xprep

// The Ophidian window (SUP-s174-supp xe:RAND:2:5:w12 / xe:RAND:3:3:w17,
// farm 2026-11-19): the snake attacks unblocked, the defender's empty
// declaration puts its optional draw trigger on the stack, and the next pass
// resolves it. The runners settle the pending trigger_optional ask at the
// resolving pass (gorge's settleResolutionAsk answers it before the pass's
// checkpoint, so gorge's checkpoint stack is []), and the XMage item must
// carry that settled answer on the resolving pass itself — xmage_answers[R]
// — with xmage_pos[R].resolves set, so the driver queues
// CMD_WAIT_RESOLVE_ONE and the ability leaves XMage's stack before the
// checkpoint (both ScenarioReplay versions replay this shape strict-clean;
// the farm row's [ability:Ophidian@p0] at the resolving pass did not
// reproduce on the current tree, see the ticket report).

import (
	"encoding/json"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/lower"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// ophidianWindow is the lowered window cut at the resolving pass: the last
// step is the pass whose checkpoint gorge records with the stack already
// empty. The window carries the live game's answer on the pass itself (the
// settlesTwice shape); the no-answer shape (the settle fallback) is covered
// by the same assertions below.
func ophidianWindow(t *testing.T, withAnswer bool) (*cards.Registry, rules.OracleResult, oraclegen.Scenario) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	answer := ""
	if withAnswer {
		answer = `,"answers":[{"kind":"trigger_optional","pick":["yes"]}]`
	}
	raw := `{
      "name":"ophidian pass resolves optional trigger",
      "turn":2,
      "setup":{
        "p0":{"battlefield":["Ophidian","Island","Island"],"hand":[],"library_top":["Swamp","Swamp","Swamp"],"life":20},
        "p1":{"battlefield":[],"hand":[],"library_top":["Plains","Plains","Plains"],"life":20}
      },
      "steps":[
        {"op":"attack","seat":0,"attackers":["p0:Ophidian"],"defender":"p1"},
        {"op":"pass","seat":0},
        {"op":"pass","seat":1},
        {"op":"pass","seat":0},
        {"op":"pass","seat":1` + answer + `}
      ]
    }`
	res, err := rules.RunOracleScenarioJSON(reg, []byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fails) > 0 {
		for _, l := range res.Transcript {
			t.Log(l)
		}
		t.Fatalf("fails: %v", res.Fails)
	}
	var parsed oraclegen.Scenario
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		t.Fatal(err)
	}
	return reg, res, parsed
}

func assertOphidianSettled(t *testing.T, res rules.OracleResult, sc oraclegen.Scenario) {
	t.Helper()
	last := len(sc.Steps) - 1
	if len(res.Snapshots) < last+2 {
		t.Fatalf("snapshots %d, want one per step plus setup", len(res.Snapshots))
	}
	if stack := res.Snapshots[last+1].Stack; len(stack) != 0 {
		b, _ := json.Marshal(stack)
		t.Fatalf("resolving pass checkpoint stack = %s, want empty", b)
	}
	// The settle recorded the ask at the resolving pass step.
	var dec rules.OracleDecision
	found := false
	for _, d := range res.Decisions {
		if d.GorgeKind == "trigger_optional" {
			dec, found = d, true
		}
	}
	if !found {
		t.Fatal("no trigger_optional decision recorded")
	}
	if dec.Step != last {
		t.Fatalf("trigger_optional decision at step %d, want the resolving pass step %d", dec.Step, last)
	}
	if dec.Resume != "optional" {
		t.Fatalf("decision resume %q, want optional", dec.Resume)
	}
}

func TestPassResolvesOptionalTrigger(t *testing.T) {
	for _, withAnswer := range []bool{true, false} {
		_, res, sc := ophidianWindow(t, withAnswer)
		assertOphidianSettled(t, res, sc)
	}
}

func TestPassResolvesOptionalTriggerXMageItem(t *testing.T) {
	sp, err := decks.LoadSpellings("../../../oracle/compliance/manifests")
	if err != nil {
		t.Fatal(err)
	}
	for _, withAnswer := range []bool{true, false} {
		reg, res, sc := ophidianWindow(t, withAnswer)
		it := oraclegen.Item{ID: "test:ophidian", Scenario: sc}
		prep, ref := Prepare(reg, sp, lower.Lowered{Item: it}, res)
		if ref != nil {
			t.Fatalf("refused: %s: %s", ref.Reason, ref.Detail)
		}
		last := len(sc.Steps) - 1
		ans := prep.XAnswers[last]
		if len(ans) != 1 {
			t.Fatalf("xmage_answers[%d] = %v, want one scripted answer", last, ans)
		}
		if ans[0].Seat != 0 || ans[0].Kind != "choice" || ans[0].Value != "yes" {
			t.Fatalf("xmage_answers[%d] = %+v, want seat 0 choice yes", last, ans[0])
		}
		pos := prep.XPos[last]
		if pos == nil || !pos.Resolves {
			t.Fatalf("xmage_pos[%d] = %+v, want resolves", last, pos)
		}
	}
}
