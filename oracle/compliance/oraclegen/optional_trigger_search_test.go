package oraclegen

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestDeclinedOptionalTriggerSearchScriptsOnlyNo pins Hexhaven Invigorator:
// "Whenever this creature is dealt damage, you may search your library for up
// to that many land cards". Forge's script has no OptionalDecider$, so gorge's
// "may" is the Min-0 search pick (declined here: the runner picks no land).
// XMage's trigger is optional: its
// chooseUse takes the "no" and no search follows, so the generic declined pair
// (no + target_skip) leaves the target_skip unread.
func TestDeclinedOptionalTriggerSearchScriptsOnlyNo(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	sc := Scenario{
		Name: "Hexhaven Invigorator declined search",
		Turn: 1,
		Setup: map[string]Seat{
			"p0": {Battlefield: []string{"Grizzly Bears"}},
			"p1": {Battlefield: []string{"Hexhaven Invigorator"}, LibraryTop: forests()},
		},
		Steps: []Step{
			{Op: "pass", Seat: 0}, {Op: "pass", Seat: 1}, {Op: "pass", Seat: 0}, {Op: "pass", Seat: 1},
			{Op: "attack", Seat: 0, Attackers: []string{"p0:Grizzly Bears"}, Defender: "p1"},
			{Op: "pass", Seat: 0}, {Op: "pass", Seat: 1},
			{Op: "block", Seat: 1, Blocks: [][2]string{{"p1:Hexhaven Invigorator", "p0:Grizzly Bears"}}},
			{Op: "pass", Seat: 0}, {Op: "pass", Seat: 1}, {Op: "pass", Seat: 0}, {Op: "resolve"},
		},
	}
	raw, err := json.Marshal(sc)
	if err != nil {
		t.Fatal(err)
	}
	res, err := rules.RunOracleScenarioJSON(reg, raw)
	if err != nil {
		t.Fatalf("run scenario: %v", err)
	}
	out := XAnswersForScenario(reg, res, sc, nil, nil)
	want := []XAnswer{{Seat: 1, Kind: "choice", Value: "no"}}
	var got []XAnswer
	for _, s := range out {
		got = append(got, s...)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("routed %v, want %v", got, want)
	}
}

func forests() []string {
	out := make([]string, 20)
	for i := range out {
		out[i] = "Forest"
	}
	return out
}
