package oraclegen

import (
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// Toils of Night and Day's XMage effect asks "Tap X?" first and "Untap X?"
// only on a no, so gorge's two untap picks on tapped Islands script
// "no, yes" per target; a single "yes" would re-tap them
// (xp-xmage-toils-tap-then-untap, triage xpt-a0473e96).
func TestTapOrUntapToilsUntapPicksScriptNoYes(t *testing.T) {
	untap := func(name string) rules.OracleDecision {
		return rules.OracleDecision{Step: 4, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Resume: "taporuntap",
			Options: 2, Min: 1, Max: 1, Picks: []string{"Untap " + name}, PickIdx: []int{0}, PickKinds: []string{"untap"}}
	}
	ds := []rules.OracleDecision{untap("Island"), untap("Island")}
	sc := Scenario{Steps: []Step{
		{Op: "activate", Seat: 0, Card: "p0:Island"},
		{Op: "activate", Seat: 0, Card: "p0:Island#2"},
		{Op: "cast", Seat: 0, Card: "p0:Toils of Night and Day", Targets: []string{"p0:Island", "p0:Island#2"}},
		{Op: "pass", Seat: 0},
		{Op: "pass", Seat: 1},
	}}
	snaps := make([]rules.OracleSnapshot, len(sc.Steps)+1)
	snaps[4].Stack = []rules.OracleSnapStack{{Kind: "spell", Source: "p0:Toils of Night and Day"}}
	if !tapThenUntapStep(sc, snaps, 4) {
		t.Fatal("step 4 resolving Toils of Night and Day is not a tap-then-untap step")
	}
	out := xanswersSetup(ds, len(sc.Steps), nil, nil, nil, nil, xanswersExtra{tapThenUntap: map[int]bool{4: true}, snaps: snaps})
	want := []XAnswer{{0, "choice", "no"}, {0, "choice", "yes"}, {0, "choice", "no"}, {0, "choice", "yes"}}
	if !reflect.DeepEqual(out[4], want) {
		t.Fatalf("step 4 answers = %+v, want %+v", out[4], want)
	}
	// The tap and no-op arms.
	tap := untap("Island")
	tap.PickKinds = []string{"tap"}
	if got := tapThenUntapChoices(tap); !reflect.DeepEqual(got, []string{"yes"}) {
		t.Fatalf("tap pick = %v, want [yes]", got)
	}
	noop := untap("Island")
	noop.PickIdx = []int{1}
	if got := tapThenUntapChoices(noop); !reflect.DeepEqual(got, []string{"no", "no"}) {
		t.Fatalf("no-op pick = %v, want [no no]", got)
	}
	// A MayTapOrUntapTargetEffect source keeps the single state-changing yes.
	snaps[4].Stack[0].Source = "p0:Twiddle"
	if tapThenUntapStep(sc, snaps, 4) {
		t.Fatal("Twiddle routed as a tap-then-untap face")
	}
	out = xanswersSetup(ds[:1], len(sc.Steps), nil, nil, nil, nil, xanswersExtra{snaps: snaps})
	if want := []XAnswer{{0, "choice", "yes"}}; !reflect.DeepEqual(out[4], want) {
		t.Fatalf("Twiddle step answers = %+v, want %+v", out[4], want)
	}
}

// Census: every face in xmageTapThenUntapFaces is a corpus face with a
// TapOrUntap ability.
func TestTapThenUntapFacesCensus(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	if len(xmageTapThenUntapFaces) != 1 {
		t.Fatalf("xmageTapThenUntapFaces has %d entries; adding one needs an XMage-source proof (see its comment)", len(xmageTapThenUntapFaces))
	}
	seen := map[string]bool{}
	for _, c := range reg.AllCards() {
		if c == nil {
			continue
		}
		for _, f := range c.Faces {
			if f == nil || !xmageTapThenUntapFaces[strings.ToLower(f.Name)] {
				continue
			}
			has := false
			for _, sa := range f.Abilities {
				has = has || sa.API == "TapOrUntap"
			}
			for _, body := range f.SVars {
				has = has || svarParams(body)["DB"] == "TapOrUntap"
			}
			if !has {
				t.Errorf("%s: no TapOrUntap ability", f.Name)
			}
			seen[strings.ToLower(f.Name)] = true
		}
	}
	if len(seen) != len(xmageTapThenUntapFaces) {
		t.Errorf("listed faces found in the corpus: %v of %d", seen, len(xmageTapThenUntapFaces))
	}
}
