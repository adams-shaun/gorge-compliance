package oraclegen

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
)

// A divided TRIGGER (Magmatic Core: X damage divided among any number of target
// creatures, X its age counters) is XMage's TargetAmount, which accepts only
// "<ref>^X=<share>". gorge asks no division for a lone target (CR 603.3d,
// 601.2d) and records the ask's Divided total only for a literal
// DividedAsYouChoose$, so the target answered on a resolve step used to be
// scripted bare ("Must be target amount, but found unknown target").

// magmaticCoreRun builds the result a run of the find's shape leaves: Magmatic
// Core's end-step trigger (printed trigger 0) on the stack with age age
// counters on the Core, its root target ask answered via the resolve step's
// answers (Via "answer", Divided 0: gorge records no total for the SVar). The
// run itself is not replayed here: a Core needs a cumulative-upkeep payment
// each turn before an end step, which is the XMage driver's replay to prove.
// div, when set, is the "divide" ask gorge poses after two or more targets.
func magmaticCoreRun(t *testing.T, age int32, div *rules.OracleDecision, refs ...string) ([]string, rules.OracleResult, Scenario) {
	t.Helper()
	reg := twinBoltRegistry(t)
	sc := Scenario{Steps: []Step{{Op: "pass_to", Seat: 0}, {Op: "resolve", Seat: 0}}}
	ask := rules.OracleDecision{Step: 0, Seat: 0, Kind: "target", Via: "answer", GorgeKind: "target",
		Options: len(refs) + 1, Min: 0, Max: len(refs), PickRefs: refs}
	for k, r := range refs {
		ask.Picks = append(ask.Picks, oraclediffRefName(r))
		ask.PickIdx = append(ask.PickIdx, k)
	}
	ds := []rules.OracleDecision{ask}
	if div != nil {
		ds = append(ds, *div)
	}
	core := rules.OracleSnapPerm{Ref: "p0:Magmatic Core", Name: "Magmatic Core", Counters: map[string]int32{"AGE": age}}
	res := rules.OracleResult{Decisions: ds, Snapshots: []rules.OracleSnapshot{
		{},
		{Permanents: []rules.OracleSnapPerm{core},
			Stack: []rules.OracleSnapStack{{Kind: "ability", Source: "p0:Magmatic Core", Trigger: "0"}}},
		{Permanents: []rules.OracleSnapPerm{core}},
	}}
	var got []string
	for _, step := range XAnswersForScenario(reg, res, sc, nil, nil) {
		for _, a := range step {
			if a.Kind == "target" {
				got = append(got, a.Value)
			}
		}
	}
	return got, res, sc
}

func TestDividedTriggerLoneTargetFromAnswersScriptsShare(t *testing.T) {
	got, _, _ := magmaticCoreRun(t, 3, nil, "p1:Crimson Kobolds")
	if want := []string{"p1:Crimson Kobolds^X=3"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("lone trigger target: got %v want %v", got, want)
	}
}

// Three points over two targets is a free division: gorge's cast-time "divide"
// ask (Min == points above one each) names where the extra point goes.
func TestDividedTriggerTwoTargetsScriptsOneSharePerTarget(t *testing.T) {
	div := &rules.OracleDecision{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Options: 2, Min: 1, Max: 1,
		Picks: []string{"Serra Angel"}, PickIdx: []int{1}, PickKinds: []string{"divide"}}
	got, _, _ := magmaticCoreRun(t, 3, div, "p1:Grizzly Bears", "p1:Serra Angel")
	want := []string{"p1:Grizzly Bears^X=1", "p1:Serra Angel^X=2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("two trigger targets: got %v want %v", got, want)
	}
}

// As many points as targets has one legal division and no ask: one each.
func TestDividedTriggerTwoTargetsOnePointEach(t *testing.T) {
	got, _, _ := magmaticCoreRun(t, 2, nil, "p1:Grizzly Bears", "p1:Serra Angel")
	want := []string{"p1:Grizzly Bears^X=1", "p1:Serra Angel^X=1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("one point each: got %v want %v", got, want)
	}
}

// A trigger whose total the snapshot cannot resolve (no age counters left to
// read) stays unmarked, so its bare answer is unchanged.
func TestDividedTriggerUnresolvedTotalStaysBare(t *testing.T) {
	got, _, _ := magmaticCoreRun(t, 0, nil, "p1:Crimson Kobolds")
	if want := []string{"Crimson Kobolds"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unresolved total: got %v want %v", got, want)
	}
}

// The Forge view never marks the trigger's ask: Forge asks no allocation for a
// lone target, and its driver reads the unmarked decisions.
func TestDividedTriggerForgeViewUnchanged(t *testing.T) {
	_, res, sc := magmaticCoreRun(t, 3, nil, "p1:Crimson Kobolds")
	reg := twinBoltRegistry(t)
	if got := CastDivisions(reg, sc, res); !reflect.DeepEqual(got, res.Decisions) {
		t.Fatalf("Forge view changed:\n got %+v\nwant %+v", got, res.Decisions)
	}
}
