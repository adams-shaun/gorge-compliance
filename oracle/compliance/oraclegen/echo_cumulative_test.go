package oraclegen

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// xproofEchoScenario runs one copied xproof find's scenario through gorge and
// returns its generated XMage answers plus the recorded decisions.
func xproofEchoScenario(t *testing.T, file string) ([][]XAnswer, []rules.OracleDecision) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	raw, err := os.ReadFile("testdata/echo-cumulative/" + file + ".json")
	if err != nil {
		t.Fatal(err)
	}
	res, err := rules.RunOracleScenarioJSON(reg, raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fails) > 0 {
		t.Fatalf("gorge fails: %v", res.Fails)
	}
	var sc Scenario
	if err := json.Unmarshal(raw, &sc); err != nil {
		t.Fatal(err)
	}
	return XAnswersForScenario(reg, res, sc, nil, nil), res.Decisions
}

// findElection returns the step of the first decision whose pick kind is want.
func findElection(t *testing.T, ds []rules.OracleDecision, want string) int {
	t.Helper()
	for _, d := range ds {
		if len(d.PickKinds) == 1 && d.PickKinds[0] == want {
			return d.Step
		}
	}
	t.Fatalf("no %s election in %+v", want, ds)
	return -1
}

// TestEchoCumulativeNonManaPayChooseUseThenTargets pins the xpt-bcbe2139
// Skizzik Surger find: XMage's EchoEffect asks chooseUse("Pay Sacrifice two
// lands?") and, on yes, SacrificeTargetCost.choose reads the TARGET queue for
// the two Mountains. Gorge's echo_pay label and the lands' action picks
// therefore script choice yes then one joined target answer, never the label.
// A decline is the lone boolean no. A mana echo (Karmic Guide, {2}{W}{W}) is
// the same chooseUse: XMage pays the mana itself, so the pay face is yes with
// no target ask. An echo DISCARD (Deepcavern Imp) keeps makeChoose: its action
// pick is not a sacrifice, so the generic choice answer stays.
func TestEchoCumulativeNonManaPayChooseUseThenTargets(t *testing.T) {
	got, ds := xproofEchoScenario(t, "skizzik-surger")
	step := findElection(t, ds, "echo_pay")
	// One "^"-joined target: SacrificeTargetCost picks one Target object
	// whose selection splits the record into both lands.
	want := []XAnswer{
		{Seat: 0, Kind: "choice", Value: "yes"},
		{Seat: 0, Kind: "target", Value: "@p0:Mountain^@p0:Mountain#2"},
	}
	if step >= len(got) || !reflect.DeepEqual(got[step], want) {
		t.Fatalf("Skizzik Surger xmage_answers[%d] = %+v, want %+v", step, got[step], want)
	}

	sc := Scenario{Steps: []Step{{Op: "pass"}}}
	decline := rules.OracleDecision{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
		Options: 2, Min: 1, Max: 1, Picks: []string{"Sacrifice Skizzik Surger"},
		PickRefs: []string{"p0:Skizzik Surger"}, PickKinds: []string{"echo_sac"}}
	if got := XAnswersForScenario(nil, rules.OracleResult{Decisions: []rules.OracleDecision{decline}}, sc, nil, nil); !reflect.DeepEqual(got, [][]XAnswer{{{Seat: 0, Kind: "choice", Value: "no"}}}) {
		t.Fatalf("echo_sac decline answers = %+v, want the lone choice no", got)
	}

	// A mana echo asks the same chooseUse and pays from the pool; no target
	// follows. The pay kind is what routes it, not TriggerCostMana.
	mana := rules.OracleDecision{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
		Options: 2, Min: 1, Max: 1, Picks: []string{"Pay 2 W W"},
		PickRefs: []string{"p0:Karmic Guide"}, PickKinds: []string{"echo_pay"}}
	if got := XAnswersForScenario(nil, rules.OracleResult{Decisions: []rules.OracleDecision{mana}}, sc, nil, nil); !reflect.DeepEqual(got, [][]XAnswer{{{Seat: 0, Kind: "choice", Value: "yes"}}}) {
		t.Fatalf("mana echo pay answers = %+v, want the lone choice yes", got)
	}

	// The discard arm shares echo_action_pick but pays through makeChoose,
	// so its pick stays a choice.
	discardPay := rules.OracleDecision{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
		Options: 2, Min: 1, Max: 1, Picks: []string{"Discard 1 card"},
		PickRefs: []string{"p0:Deepcavern Imp"}, PickKinds: []string{"echo_pay"}}
	discardPick := rules.OracleDecision{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
		Options: 2, Min: 1, Max: 1, Picks: []string{"Grizzly Bears"},
		PickRefs: []string{"p0:Grizzly Bears"}, PickKinds: []string{"echo_action_pick"}}
	wantDiscard := [][]XAnswer{{{Seat: 0, Kind: "choice", Value: "yes"}, {Seat: 0, Kind: "choice", Value: "Grizzly Bears"}}}
	if got := XAnswersForScenario(nil, rules.OracleResult{Decisions: []rules.OracleDecision{discardPay, discardPick}}, sc, nil, nil); !reflect.DeepEqual(got, wantDiscard) {
		t.Fatalf("echo discard answers = %+v, want %+v", got, wantDiscard)
	}

	// A one-pick echo answer is the bare ref, not "@ref": the driver binds a
	// ref-shaped target answer before targetName resolves it, while an "@ref"
	// is passed through and an object that entered after setup has no alias
	// yet. OptionRefs make the pick same-name ambiguous, so the generator
	// names the exact ref rather than the bare name.
	postElection := rules.OracleDecision{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
		Options: 2, Min: 1, Max: 1, Picks: []string{"Sacrifice a land"},
		PickRefs: []string{"p0:Forest#3"}, PickKinds: []string{"echo_pay"}}
	postPick := rules.OracleDecision{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose",
		Options: 3, Min: 1, Max: 1, Picks: []string{"Forest"},
		PickRefs:   []string{"p0:Forest#3"},
		OptionRefs: []string{"p0:Forest#1", "p0:Forest#2", "p0:Forest#3"},
		PickKinds:  []string{"echo_action_pick"}}
	wantPost := [][]XAnswer{{{Seat: 0, Kind: "choice", Value: "yes"}, {Seat: 0, Kind: "target", Value: "p0:Forest#3"}}}
	if got := XAnswersForScenario(nil, rules.OracleResult{Decisions: []rules.OracleDecision{postElection, postPick}}, sc, nil, nil); !reflect.DeepEqual(got, wantPost) {
		t.Fatalf("post-setup echo answer = %+v, want the bare ref %+v", got, wantPost)
	}
}

// TestCumulativeUpkeepSacLandPayChooseUse pins the Polar Kraken find:
// CumulativeUpkeepEffect asks chooseUse("Sacrifice 1 land per age (1 age
// counter(s))?") and its SacrificeTargetCost then takes the TARGET queue for
// the one Island. At age >= 2 XMage pays one copy of the cost per age counter,
// and each copy's SacrificeTargetCost asks its own TargetSacrifice, so the two
// picks must be two target answers: one joined record answers only the first
// ask (the second has no scripted answer).
func TestCumulativeUpkeepSacLandPayChooseUse(t *testing.T) {
	got, ds := xproofEchoScenario(t, "polar-kraken")
	step := findElection(t, ds, "cumulative_pay")
	want := []XAnswer{
		{Seat: 0, Kind: "choice", Value: "yes"},
		{Seat: 0, Kind: "target", Value: "p0:Island"},
	}
	if step >= len(got) || !reflect.DeepEqual(got[step], want) {
		t.Fatalf("Polar Kraken xmage_answers[%d] = %+v, want %+v", step, got[step], want)
	}

	got2, ds2 := xproofEchoScenario(t, "polar-kraken-age2")
	step2 := findNthElection(t, ds2, "cumulative_pay", 2)
	want2 := []XAnswer{
		{Seat: 0, Kind: "choice", Value: "yes"},
		{Seat: 0, Kind: "target", Value: "p0:Island#2"},
		{Seat: 0, Kind: "target", Value: "p0:Island#3"},
	}
	if step2 >= len(got2) || !reflect.DeepEqual(got2[step2], want2) {
		t.Fatalf("Polar Kraken age 2 xmage_answers[%d] = %+v, want %+v", step2, got2[step2], want2)
	}
}

// findNthElection returns the step of the n-th (1-based) decision whose single
// pick kind is want. The age-2 fixture reaches Polar Kraken's second upkeep, so
// it holds the age-1 election first and the age-2 election second.
func findNthElection(t *testing.T, ds []rules.OracleDecision, want string, n int) int {
	t.Helper()
	wantN := n
	for _, d := range ds {
		if len(d.PickKinds) != 1 || d.PickKinds[0] != want {
			continue
		}
		n--
		if n == 0 {
			return d.Step
		}
	}
	t.Fatalf("no %s election #%d in %+v", want, wantN, ds)
	return -1
}
