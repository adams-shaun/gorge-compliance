package oraclediff

import "testing"

// eqTable answers NWay's pair questions from a list of agreeing pairs; every
// other pair of the listed engines is known and differs.
func eqTable(agree ...string) func(a, b string) (bool, bool) {
	set := map[string]bool{}
	for _, p := range agree {
		set[p] = true
		set[string(p[1])+string(p[0])] = true
	}
	return func(a, b string) (bool, bool) { return set[a+b], true }
}

var testNames = map[string]string{"F": "forge", "P": "phase", "X": "xmage"}

// With gorge, Forge and XMage alone, NWay spells the three-way patterns'
// class strings and gives the matching labels.
func TestNWayThreeEnginesSpellThreeWay(t *testing.T) {
	for _, c := range []struct {
		agree []string
		want  Pattern
		label string
	}{
		{[]string{"FX"}, ForgeXMageG, NWayGorgeSuspect},
		{[]string{"GF"}, GorgeForgeX, NWaySuspect("xmage")},
		{[]string{"GX"}, GorgeXMageF, NWaySuspect("forge")},
		{nil, AllDiffer, NWayAllDiffer},
	} {
		v := NWay([]string{"F", "X"}, nil, eqTable(c.agree...), testNames)
		if v.Pattern != string(c.want) || v.Label != c.label {
			t.Errorf("%v: got %q/%q, want %q/%q", c.agree, v.Pattern, v.Label, c.want, c.label)
		}
	}
	if v := NWay([]string{"F", "X"}, nil, eqTable("GF", "GX", "FX"), testNames); v.Pattern != "AGREE3" || v.Label != "" {
		t.Errorf("all agree: %q/%q", v.Pattern, v.Label)
	}
}

func TestNWayFourEngines(t *testing.T) {
	for _, c := range []struct {
		name  string
		agree []string
		marks map[string]string
		want  string
		label string
		vote  string
	}{
		{"phase confirms a gorge suspect", []string{"FX", "FP", "PX"}, nil, "FPX|G", NWayGorgeSuspect, PhaseWithOthers},
		{"phase sides with gorge", []string{"FX", "GP"}, nil, "GP|FX", NWaySplit, PhaseWithGorge},
		{"phase alone", []string{"GF", "GX", "FX"}, nil, "GFX|P", NWaySuspect("phase"), PhaseAlone},
		{"phase a third reading", []string{"FX"}, nil, "FX|G|P", NWayGorgeSuspect, PhaseAlone},
		{"everyone differs", nil, nil, "G|F|P|X", NWayAllDiffer, PhaseAlone},
		{"all agree", []string{"GF", "GX", "FX", "GP", "FP", "PX"}, nil, "AGREE4", "", PhaseWithGorge},
		{"phase lacks the card", []string{"FX"}, map[string]string{"P": MarkUnsupported}, "FX|G P?", NWayGorgeSuspect, PhaseUnsupported},
		{"phase harness", []string{"GF"}, map[string]string{"P": MarkUncompared}, "GF|X P–", NWaySuspect("xmage"), PhaseUncompared},
		{"xmage lacks, phase with forge", []string{"FP"}, map[string]string{"X": MarkUnsupported}, "FP|G X?", NWayGorgeSuspect, PhaseWithOthers},
		{"agreement among those that compared", []string{"GF"}, map[string]string{"P": MarkUnsupported, "X": MarkUncompared}, "GF P? X–", "", PhaseUnsupported},
	} {
		t.Run(c.name, func(t *testing.T) {
			v := NWay([]string{"F", "P", "X"}, c.marks, eqTable(c.agree...), testNames)
			if v.Pattern != c.want || v.Label != c.label || v.Vote("P") != c.vote {
				t.Fatalf("got %q/%q/%s, want %q/%q/%s", v.Pattern, v.Label, v.Vote("P"), c.want, c.label, c.vote)
			}
		})
	}
}

// A pair NWay was not told about neither joins nor splits two engines.
func TestNWayUnknownPair(t *testing.T) {
	eq := func(a, b string) (bool, bool) {
		switch a + b {
		case "GF":
			return false, true
		case "GX":
			return false, true
		case "FX":
			return false, false // not compared
		}
		return false, true
	}
	if v := NWay([]string{"F", "X"}, nil, eq, testNames); v.Pattern != "G|F|X" {
		t.Fatalf("got %q", v.Pattern)
	}
}

func TestNWayInconsistent(t *testing.T) {
	// G = F, F = X, but G != X: not transitive.
	eq := func(a, b string) (bool, bool) { return a+b != "GX", true }
	v := NWay([]string{"F", "X"}, nil, eq, testNames)
	if !v.Inconsistent || v.Pattern != string(Inconsistent) {
		t.Fatalf("got %+v", v)
	}
}
