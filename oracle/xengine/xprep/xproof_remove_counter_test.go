package xprep

// The xproof multi-amount cluster (gorge-compliance
// xp-xmage-multiamount-counters, cycle 11241210): Mabel, Bitter Recluse's
// "remove up to three counters" election. gorge records one counter-kind pick
// per counter removed; XMage asks one multi-amount dialog ("Remove counters
// (from Ajani Resolute)"), so Prepare must fold the picks into the X=2 amount
// the loyalty message consumes.

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/lower"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

func TestXProofRemoveCounterMultiAmount(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	sp, err := decks.LoadSpellings("../../../oracle/compliance/manifests")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("testdata/xproof/mabel-bitter-recluse.json")
	if err != nil {
		t.Fatal(err)
	}
	var sc oraclegen.Scenario
	if err := json.Unmarshal(raw, &sc); err != nil {
		t.Fatal(err)
	}
	res, err := rules.RunOracleScenarioJSON(reg, raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fails) > 0 {
		t.Fatalf("gorge fails: %v", res.Fails)
	}
	// Precondition: the find really records the per-counter run this fold
	// replaces, and the run is on the resolve step the answer lands on.
	step, picks := -1, 0
	for _, d := range res.Decisions {
		if d.Resume != "counter_kind" {
			continue
		}
		step, picks = d.Step, picks+len(d.Picks)
	}
	if step != 3 || picks != 2 {
		t.Fatalf("precondition: counter-kind ask at step %d with %d picks, want step 3 and 2 picks", step, picks)
	}
	prep, ref := Prepare(reg, sp, lower.Lowered{Item: oraclegen.Item{ID: "xs:XP:mabel-bitter-recluse:f0t0:s1", Scenario: sc}}, res)
	if ref != nil {
		t.Fatalf("refused: %s: %s", ref.Reason, ref.Detail)
	}
	var amounts []oraclegen.XAnswer
	for _, a := range prep.XAnswers[step] {
		if a.Kind == "choice" && a.Value == "LOYALTY" {
			t.Fatalf("raw counter kind still scripted at step %d: %+v", step, prep.XAnswers[step])
		}
		if a.Kind == "amount" {
			amounts = append(amounts, a)
		}
	}
	want := []oraclegen.XAnswer{{Seat: 0, Kind: "amount", Value: "2"}}
	if len(amounts) != len(want) || amounts[0] != want[0] {
		t.Fatalf("amount answers = %+v, want %+v (step answers %+v)", amounts, want, prep.XAnswers[step])
	}
}
