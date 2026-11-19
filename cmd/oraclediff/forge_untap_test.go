package main

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// untapDecision returns the only untap election in ds. It does not use
// isUntapElection: the exported decision is empty on purpose, and must still
// be findable to assert that.
func untapDecision(t *testing.T, ds []rules.OracleDecision) rules.OracleDecision {
	t.Helper()
	var found []rules.OracleDecision
	for _, d := range ds {
		if d.Kind == "choose_n" && d.Resume == "untap" {
			found = append(found, d)
		}
	}
	if len(found) != 1 {
		t.Fatalf("%d untap elections, want 1: %+v", len(found), ds)
	}
	return found[0]
}

func snapshotPerm(s rules.OracleSnapshot, ref string) *rules.OracleSnapPerm {
	for i := range s.Permanents {
		if s.Permanents[i].Ref == ref {
			return &s.Permanents[i]
		}
	}
	return nil
}

// TestForgeUntapSameName is the Frantic Search regression (SUP-s101-supp
// xe:RAND:0:7:w14). Gorge's untap ask offers every land and the recorded
// election picked the already-untapped base Island, a legal no-op (CR 701.26b:
// only a tapped permanent can be untapped). Forge's UntapEffect offers only
// tapped permanents, so the request must not name that Island as a pick: the
// driver's name/owner fallback would untap a different same-named Island.
// When a pick is a real one, the request must name the exact offered object
// from the candidate list (pick_idx into option_refs), not the bare name.
//
// The fixture is the xe:RAND:0:7:w14 window item as xengine play wrote it, so
// the test goes through forgeExportItem and gorge's runner exactly as
// forge-export does.
func TestForgeUntapSameName(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	raw, err := os.ReadFile("testdata/forge/untap-same-name.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	var it oraclegen.Item
	if err := json.Unmarshal(bytes.TrimSpace(raw), &it); err != nil {
		t.Fatal(err)
	}
	rq, g := forgeExportItem(reg, it, true)

	// Precondition: gorge posed the election and picked the base Island,
	// which the checkpoint entering the step has untapped.
	posed := untapDecision(t, g.Decisions)
	if posed.Step < 0 || posed.Step+1 >= len(g.Snapshots) {
		t.Fatalf("untap election step %d has no before/after checkpoint (%d snapshots)", posed.Step, len(g.Snapshots))
	}
	if !reflect.DeepEqual(posed.PickRefs, []string{"p0:Island"}) || posed.Min != 0 || posed.Max != 3 {
		t.Fatalf("precondition: gorge's untap election = %+v, want p0:Island 0..3", posed)
	}
	if base := snapshotPerm(g.Snapshots[posed.Step], "p0:Island"); base == nil || base.Tapped {
		t.Fatalf("precondition: step %d p0:Island = %+v, want untapped", posed.Step, base)
	}

	// The exported election is the empty answer min 0 allows: no bare
	// "p0:Island" reaches the driver's same-name fallback.
	got := untapDecision(t, rq.GorgeDecisions)
	if got.Kind != "choose_n" || got.Min != 0 || got.Max != 3 {
		t.Errorf("exported untap election = %+v, want choose_n 0..3", got)
	}
	if len(got.Picks) != 0 || len(got.PickKinds) != 0 || len(got.PickRefs) != 0 || len(got.ObjectPicks) != 0 {
		t.Errorf("exported untap election keeps a pick: %+v; the already-untapped base Island is not a Forge candidate", got)
	}

	// Done-means outcome: the answer untaps nothing, and gorge's own
	// checkpoints already show exactly one untapped p0 Island (the base)
	// before and after the resolving pass, so Forge's untapped set matches.
	untappedIslands := func(s rules.OracleSnapshot) []string {
		var out []string
		for _, p := range s.Permanents {
			if p.Owner == 0 && p.Name == "Island" && !p.Tapped {
				out = append(out, p.Ref)
			}
		}
		return out
	}
	if got := untappedIslands(g.Snapshots[posed.Step]); !reflect.DeepEqual(got, []string{"p0:Island"}) {
		t.Fatalf("precondition: untapped p0 Islands entering step %d = %v, want [p0:Island]", posed.Step, got)
	}
	if got := untappedIslands(g.Snapshots[posed.Step+1]); !reflect.DeepEqual(got, []string{"p0:Island"}) {
		t.Errorf("gorge untapped p0 Islands after the resolving pass = %v, want [p0:Island]", got)
	}

	// A real pick of a tapped same-named Island stays, named by the exact
	// offered object its pick_idx selects rather than the bare name.
	snaps := append([]rules.OracleSnapshot(nil), g.Snapshots...)
	snaps[posed.Step].Permanents = []rules.OracleSnapPerm{
		{Ref: "p0:Island", Name: "Island"},
		{Ref: "p0:Island#2", Name: "Island", Tapped: true},
	}
	real := rules.OracleDecision{
		Step: posed.Step, Seat: 0, Kind: "choose_n", Options: 2,
		Picks: []string{"Island"}, PickIdx: []int{1},
		PickRefs: []string{"p0:Island"}, PickRefsInexact: []bool{true},
		ObjectPicks: []string{"p0:Island"},
		OptionRefs:  []string{"p0:Island", "p0:Island#2"},
		PickKinds:   []string{"untap"}, Resume: "untap", Min: 0, Max: 3,
	}
	out := forgeUntapNoOps(rules.OracleResult{Snapshots: snaps}, []rules.OracleDecision{real})
	if len(out) != 1 {
		t.Fatalf("%d decisions, want 1: %+v", len(out), out)
	}
	if want := []string{"p0:Island#2"}; !reflect.DeepEqual(out[0].PickRefs, want) || !reflect.DeepEqual(out[0].ObjectPicks, want) {
		t.Errorf("real same-name pick = refs %v object_picks %v, want the exact option %v",
			out[0].PickRefs, out[0].ObjectPicks, want)
	}
	if !reflect.DeepEqual(out[0].PickKinds, []string{"untap"}) || out[0].Picks[0] != "Island" {
		t.Errorf("real same-name pick lost its kind or label: %+v", out[0])
	}
	if real.PickRefs[0] != "p0:Island" || real.ObjectPicks[0] != "p0:Island" {
		t.Errorf("the input decision was mutated: %+v", real)
	}

	// The sibling shape from the farm (SUP-s108/s109, the other seat's
	// untapped land) is dropped the same way; a non-untap election at the
	// same step is untouched.
	snaps[posed.Step].Permanents = append(snaps[posed.Step].Permanents,
		rules.OracleSnapPerm{Ref: "p1:Island", Name: "Island"})
	other := rules.OracleDecision{
		Step: posed.Step, Seat: 0, Kind: "choose_n", Options: 1,
		Picks: []string{"Island"}, PickIdx: []int{0},
		PickRefs: []string{"p1:Island"}, ObjectPicks: []string{"p1:Island"},
		OptionRefs: []string{"p1:Island"},
		PickKinds:  []string{"untap"}, Resume: "untap", Min: 0, Max: 3,
	}
	search := rules.OracleDecision{
		Step: posed.Step, Seat: 0, Kind: "choose_n", Options: 1,
		Picks: []string{"Island"}, PickRefs: []string{"p1:Island"}, PickKinds: []string{"search"},
	}
	out = forgeUntapNoOps(rules.OracleResult{Snapshots: snaps}, []rules.OracleDecision{other, search})
	if len(out[0].Picks) != 0 || len(out[0].PickRefs) != 0 || len(out[0].PickKinds) != 0 {
		t.Errorf("other seat's already-untapped pick not dropped: %+v", out[0])
	}
	if !reflect.DeepEqual(out[1], search) {
		t.Errorf("non-untap decision changed: %+v", out[1])
	}

	// An exact ask (min > 0) whose only pick is a no-op cannot become empty:
	// an empty answer would be illegal, so it is left as it is.
	exact := other
	exact.Min, exact.Max = 1, 1
	out = forgeUntapNoOps(rules.OracleResult{Snapshots: snaps}, []rules.OracleDecision{exact})
	if !reflect.DeepEqual(out[0], exact) {
		t.Errorf("an exact untap ask was emptied: %+v", out[0])
	}
}
