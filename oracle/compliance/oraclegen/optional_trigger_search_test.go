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

// TestAcceptedOptionalTriggerEmptySearchKeepsPair pins the negative: Hunting
// Cheetah's trigger line carries OptionalDecider$ You, so gorge asks the
// yes/no first. A "yes" with a search that finds nothing is not a decline of
// the trigger; XMage consumes the yes and runs the search, so the generic
// target_skip must stay.
func TestAcceptedOptionalTriggerEmptySearchKeepsPair(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	sc := Scenario{
		Name: "Hunting Cheetah accepted empty search",
		Turn: 1,
		Setup: map[string]Seat{
			"p0": {Battlefield: []string{"Hunting Cheetah"}, LibraryTop: forests()},
			"p1": {},
		},
		Steps: []Step{
			{Op: "pass", Seat: 0}, {Op: "pass", Seat: 1}, {Op: "pass", Seat: 0}, {Op: "pass", Seat: 1},
			{Op: "attack", Seat: 0, Attackers: []string{"p0:Hunting Cheetah"}, Defender: "p1"},
			{Op: "pass", Seat: 0}, {Op: "pass", Seat: 1},
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
	var sawOptional, sawEmptySearch bool
	for _, d := range res.Decisions {
		if d.Kind == "yesno" && d.GorgeKind == "trigger_optional" {
			sawOptional = true
		}
		if d.Kind == "choose_n" && d.Resume == "search" && len(d.Picks) == 0 {
			sawEmptySearch = true
		}
		if optionalTriggerSearchDecline(reg, sc, res, d) {
			t.Fatalf("decision %+v marked as an optional-trigger decline", d)
		}
	}
	// Non-vacuous: the scenario really asked the yes/no, then an empty search.
	if !sawOptional || !sawEmptySearch {
		t.Fatalf("scenario lost its shape: trigger_optional=%v emptySearch=%v decisions=%+v",
			sawOptional, sawEmptySearch, res.Decisions)
	}
}
