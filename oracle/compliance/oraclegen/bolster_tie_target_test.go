package oraclegen

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestBolsterTiePickRidesTargetQueue pins the xproof Dromoka, the Eternal find
// (xs:XP:dromoka-the-eternal:f0t0:s1). p0 attacks with Dromoka (5/5) and
// Nadaar, Selfless Paladin (3/3, a Dragon), so "Whenever a Dragon you control
// attacks, bolster 2" triggers twice. The first bolster takes Nadaar (the lone
// least toughness); the second sees a 5/5 tie, which gorge records as a
// choose_n/counter_pick. XMage's BolsterEffect builds a TargetPermanent and
// calls controller.chooseTarget on that tie, so TestPlayer reads the TARGET
// queue: scripting gorge's pick as a "choice" is never consumed ("Missing
// TARGET def", leftover "found [Dromoka, the Eternal]"). The pick must script
// one target answer and no choice.
func TestBolsterTiePickRidesTargetQueue(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	sc := Scenario{
		Name: "Dromoka, the Eternal bolster tie",
		Setup: map[string]Seat{
			"p0": {
				Battlefield: []string{"Dromoka, the Eternal", "Nadaar, Selfless Paladin"},
				LibraryTop: []string{"Forest", "Plains", "Forest", "Forest", "Plains", "Forest",
					"Forest", "Plains", "Forest", "Plains", "Forest", "Forest", "Forest", "Plains",
					"Plains", "Plains", "Plains", "Plains", "Plains", "Forest"},
			},
			"p1": {
				Battlefield: []string{"Shield Sphere"},
				LibraryTop: []string{"Plains", "Plains", "Plains", "Plains", "Plains",
					"Plains", "Plains", "Plains", "Plains", "Plains"},
			},
		},
		Steps: []Step{
			{Op: "pass", Seat: 0},
			{Op: "pass", Seat: 1},
			{Op: "pass", Seat: 0},
			{Op: "pass", Seat: 1},
			{Op: "attack", Seat: 0, Defender: "p1",
				Attackers: []string{"p0:Dromoka, the Eternal", "p0:Nadaar, Selfless Paladin"},
				Answers: []Answer{{Kind: "trigger_order", Pick: []string{
					"p0:Dromoka, the Eternal", "p0:Dromoka, the Eternal", "p0:Nadaar, Selfless Paladin"}}}},
			{Op: "pass", Seat: 0},
			{Op: "pass", Seat: 1},
			{Op: "pass", Seat: 0, Answers: []Answer{{Kind: "choose", Pick: []string{"Dungeon of the Mad Mage"}}}},
			{Op: "pass", Seat: 1},
			{Op: "pass", Seat: 0},
			{Op: "pass", Seat: 1},
			{Op: "pass", Seat: 0},
			{Op: "pass", Seat: 1},
			{Op: "pass", Seat: 0, Answers: []Answer{{Kind: "choose", Pick: []string{"p0:Dromoka, the Eternal"}}}},
			{Op: "pass", Seat: 1},
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
	if len(res.Fails) > 0 {
		t.Fatalf("gorge fails: %v", res.Fails)
	}
	var pick *rules.OracleDecision
	for i := range res.Decisions {
		d := &res.Decisions[i]
		if pickKind(*d, 0) == "counter_pick" && len(d.PickRefs) == 1 && d.PickRefs[0] == "p0:Dromoka, the Eternal" {
			pick = d
			break
		}
	}
	if pick == nil {
		t.Fatalf("precondition: no bolster tie counter_pick for Dromoka: %+v", res.Decisions)
	}
	if want := []string{"Dromoka, the Eternal"}; !reflect.DeepEqual(pick.Picks, want) {
		t.Fatalf("precondition: tie pick = %v, want %v", pick.Picks, want)
	}
	out := XAnswersForScenario(reg, res, sc, nil, nil)
	if pick.Step < 0 || pick.Step >= len(out) {
		t.Fatalf("decision step %d outside answers (%d steps)", pick.Step, len(out))
	}
	want := []XAnswer{{Seat: 0, Kind: "target", Value: "Dromoka, the Eternal"}}
	if got := out[pick.Step]; !reflect.DeepEqual(got, want) {
		t.Fatalf("step %d answers = %+v, want %+v", pick.Step, got, want)
	}
	for _, a := range out[pick.Step] {
		if a.Kind == "choice" {
			t.Fatalf("bolster tie pick still scripts a choice answer: %+v", out[pick.Step])
		}
	}
}

// TestBolsterTiePickResolvesCopiedSource pins the copy shape: gorge's
// stackSourceRef leaves a copy permanent's bound ref ("p0:Clone") on the stack
// object while its displayed face is the copied card, so the resolving source
// must be read from the snapshot permanent's Name, not from the ref. A Clone of
// Dromoka, the Eternal still bolsters through chooseTarget in XMage.
func TestBolsterTiePickResolvesCopiedSource(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	snap := func(source, trigger, name string) []rules.OracleSnapshot {
		return []rules.OracleSnapshot{{
			Permanents: []rules.OracleSnapPerm{{Ref: source, Name: name}},
			Stack:      []rules.OracleSnapStack{{Kind: "ability", Source: source, Trigger: trigger}},
		}}
	}
	d := rules.OracleDecision{Step: 0, Kind: "choose_n", Picks: []string{"Dromoka, the Eternal"},
		PickKinds: []string{"counter_pick"}}
	sc := Scenario{Steps: []Step{{Op: "pass"}}}
	cases := []struct {
		name  string
		snaps []rules.OracleSnapshot
		want  bool
	}{
		{"copy displays Dromoka", snap("p0:Clone", "0", "Dromoka, the Eternal"), true},
		{"original Dromoka", snap("p0:Dromoka, the Eternal", "0", "Dromoka, the Eternal"), true},
		{"copy of a non-bolster card", snap("p0:Clone", "0", "Shield Sphere"), false},
		{"copy source gone from the snapshot", []rules.OracleSnapshot{{
			Stack: []rules.OracleSnapStack{{Kind: "ability", Source: "p0:Clone", Trigger: "0"}}}}, false},
	}
	for _, c := range cases {
		if got := bolsterTiePick(reg, sc, rules.OracleResult{Snapshots: c.snaps}, d); got != c.want {
			t.Errorf("%s: bolsterTiePick = %v, want %v", c.name, got, c.want)
		}
	}
}
