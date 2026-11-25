package xprep

// The xproof discard-cost cluster (gorge-compliance
// xp-xmage-discard-cost-card-pick, cycles 11250348 and 11250405): Borborygmos
// Enraged's "Discard<1/Land>" activation. A pick among two same-name cards is
// still a recorded choice (the XMage driver matches it by name), while a
// one-card hand is forced and scripts nothing; the driver relies on that to
// tell a discard cost's own ask from a later step's queued pick.

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

func TestXPrepDiscardCostSameNamePick(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	sp, err := decks.LoadSpellings("../../../oracle/compliance/manifests")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("testdata/xproof/borborygmos-enraged-discard-cost.json")
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
	prep, ref := Prepare(reg, sp, lower.Lowered{Item: oraclegen.Item{ID: "xs:XP:borborygmos-enraged:f0a0:s1", Scenario: sc}}, res)
	if ref != nil {
		t.Fatalf("refused: %s: %s", ref.Reason, ref.Detail)
	}
	choices := func(step int) []oraclegen.XAnswer {
		var out []oraclegen.XAnswer
		if step < len(prep.XAnswers) {
			for _, a := range prep.XAnswers[step] {
				if a.Kind == "choice" {
					out = append(out, a)
				}
			}
		}
		return out
	}
	// Step 0 discards the lone Mountain and step 20 the last one: forced, so
	// nothing is scripted. Step 19 picks among two Mountains: one answer.
	for _, step := range []int{0, 20} {
		if got := choices(step); len(got) != 0 {
			t.Fatalf("step %d (a one-card hand) scripted %+v, want none", step, got)
		}
	}
	want := oraclegen.XAnswer{Seat: 0, Kind: "choice", Value: "Mountain"}
	if got := choices(19); len(got) != 1 || got[0] != want {
		t.Fatalf("step 19 (a pick among two Mountains) scripted %+v, want exactly [%+v]", got, want)
	}
}
