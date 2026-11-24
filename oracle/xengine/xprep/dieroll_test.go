package xprep

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/lower"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestAddDieRollAnswers: each recorded die roll becomes one kind "die" answer
// on its step, carrying the NATURAL result (the driver applies its own
// modifier). A roll outside the item's steps is not scripted, and an answer
// list shorter than the step count grows to it.
func TestAddDieRollAnswers(t *testing.T) {
	rolls := []oraclegen.DieRoll{
		{Step: 1, Seat: 0, Sides: 6, Natural: 3, Result: 4},
		{Step: 0, Seat: 1, Sides: 20, Natural: 17, Result: 17},
		{Step: 2, Seat: 0, Sides: 6, Natural: 5, Result: 5}, // past the steps
	}
	got := addDieRollAnswers([][]oraclegen.XAnswer{{}}, rolls, 2)
	want := [][]oraclegen.XAnswer{
		{{Seat: 1, Kind: "die", Value: "17"}},
		{{Seat: 0, Kind: "die", Value: "3"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("answers %+v, want %+v", got, want)
	}
	if nothing := addDieRollAnswers(nil, nil, 2); nothing != nil {
		t.Fatalf("no rolls changed the answers: %+v", nothing)
	}
}

// TestAddDieRollAnswersSameStepOrder pins the queue order when a step already
// carries a decision and also rolls: the die leads, then the step's scripted
// decisions. XMage's CHOICE queue is FIFO: TestPlayer.rollDieResult consumes
// only a head that starts with DIE_ROLL, and Ebony Fly's ability rolls the
// d6 before it asks the animate yes/no, so ["Yes", die] strict-misses while
// [die, "Yes"] works.
func TestAddDieRollAnswersSameStepOrder(t *testing.T) {
	rolls := []oraclegen.DieRoll{{Step: 1, Seat: 0, Sides: 6, Natural: 3, Result: 3}}
	existing := [][]oraclegen.XAnswer{
		nil,
		{{Seat: 0, Kind: "choice", Value: "yes"}},
	}
	got := addDieRollAnswers(existing, rolls, 2)
	want := [][]oraclegen.XAnswer{
		nil,
		{
			{Seat: 0, Kind: "die", Value: "3"},
			{Seat: 0, Kind: "choice", Value: "yes"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("answers %+v, want %+v", got, want)
	}
}

// TestAddDieRollAnswersEbonyFlyStep6 pins the real shape the farm row
// xe:RAND:0:7:w7 lowers to: one d6, natural 3, on scenario step 6 (the pass
// that resolves Ebony Fly's activation), where gorge's own answer list
// already holds the animate "yes". The die must come first in that step's
// queue, and no other step changes.
func TestAddDieRollAnswersEbonyFlyStep6(t *testing.T) {
	answers := make([][]oraclegen.XAnswer, 20)
	answers[6] = []oraclegen.XAnswer{{Seat: 0, Kind: "choice", Value: "yes"}}
	rolls := []oraclegen.DieRoll{
		{Step: 6, Seat: 0, Sides: 6, Natural: 3, Result: 3},
	}
	got := addDieRollAnswers(answers, rolls, 20)
	want := []oraclegen.XAnswer{
		{Seat: 0, Kind: "die", Value: "3"},
		{Seat: 0, Kind: "choice", Value: "yes"},
	}
	if !reflect.DeepEqual(got[6], want) {
		t.Fatalf("step 6 answers %+v, want %+v", got[6], want)
	}
	for i, as := range got {
		if i != 6 && len(as) != 0 {
			t.Fatalf("step %d changed: %+v", i, as)
		}
	}
}

// TestAddDieRollAnswersMultiDie keeps the two-die case: a step that rolls
// more than once queues its dice in log order ahead of its decisions.
func TestAddDieRollAnswersMultiDie(t *testing.T) {
	answers := make([][]oraclegen.XAnswer, 20)
	answers[6] = []oraclegen.XAnswer{{Seat: 0, Kind: "choice", Value: "yes"}}
	rolls := []oraclegen.DieRoll{
		{Step: 6, Seat: 0, Sides: 6, Natural: 3, Result: 3},
		{Step: 6, Seat: 0, Sides: 6, Natural: 5, Result: 5},
	}
	got := addDieRollAnswers(answers, rolls, 20)
	want := []oraclegen.XAnswer{
		{Seat: 0, Kind: "die", Value: "3"},
		{Seat: 0, Kind: "die", Value: "5"},
		{Seat: 0, Kind: "choice", Value: "yes"},
	}
	if !reflect.DeepEqual(got[6], want) {
		t.Fatalf("step 6 answers %+v, want %+v", got[6], want)
	}
	for i, as := range got {
		if i != 6 && len(as) != 0 {
			t.Fatalf("step %d changed: %+v", i, as)
		}
	}
}

// TestSameOutcomeReplaysDieRolls: Prepare's retarget rerun must force the
// tape's die roll. SelfCheck's result in gorge.jsonl is the forced-roll run,
// so a rerun that draws its own d20 lands in a different row, its snapshots
// differ, and Prepare refuses a correct item with ReasonRetarget. The seed
// search keeps a case where the unforced roll really changes the snapshots, so
// the test fails rather than silently agreeing if the roll stops riding along.
func TestSameOutcomeReplaysDieRolls(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	pair := []decks.Deck{
		{Name: "start-p0", Colours: "B", Cards: append([]string{"Herald of Hadar"}, dieRollDeck(39, "Swamp")...)},
		{Name: "start-p1", Colours: "W", Cards: dieRollDeck(40, "Plains")},
	}
	start := &rules.StartSpec{Seats: []rules.StartSeat{
		{Battlefield: []int{0, 1, 2, 3, 4, 5, 6}},
		{},
	}}
	const maxSeed = 80
	var (
		l   lower.Lowered
		g   rules.OracleResult
		got bool
	)
	for n := 1; n <= maxSeed && !got; n++ {
		cand, why, _, err := lower.Span(reg, pair, dieRollSeed(n), game.Options{
			Set: "XP", MaxTurns: 2, Start: start,
			Focus: &game.Focus{Seat: 0, Card: "Herald of Hadar", Kind: "ability"},
		}, false, "xs:XP:herald-of-hadar:ability", "xproof start Herald of Hadar ability")
		if err != nil {
			t.Fatal(err)
		}
		if len(why) > 0 {
			t.Fatalf("seed %d: span refused: %v", n, why)
		}
		natural17 := false
		for _, r := range cand.Item.DieRolls {
			if r.Sides == 20 && r.Natural == 17 {
				natural17 = true
			}
		}
		if !natural17 {
			continue
		}
		forced, err := rules.RunOracleScenarioJSON(reg, cand.Item.Raw())
		if err != nil {
			t.Fatal(err)
		}
		unforcedJSON, err := json.Marshal(cand.Item.Scenario)
		if err != nil {
			t.Fatal(err)
		}
		unforced, err := rules.RunOracleScenarioJSON(reg, unforcedJSON)
		if err != nil {
			t.Fatal(err)
		}
		if reflect.DeepEqual(unforced.Snapshots, forced.Snapshots) {
			continue
		}
		l, g, got = cand, forced, true
	}
	if !got {
		t.Fatalf("no game seed in 1..%d rolls a natural 17 that changes the snapshots", maxSeed)
	}
	if why := sameOutcome(reg, l.Item, l.Item.Scenario, g); why != "" {
		t.Fatalf("the rerun dropped the tape's die roll: %s", why)
	}
}

// dieRollDeck is n copies of name.
func dieRollDeck(n int, name string) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = name
	}
	return out
}

// dieRollSeed is game n for active seat 0: the same starter search lower's
// shape tests use.
func dieRollSeed(n int) uint64 {
	next := uint64(1)
	var seed uint64
	for k := 1; k <= n; k++ {
		seed = rules.SeedForStarter(next, 2, 0)
		next = seed + 1
	}
	return seed
}
