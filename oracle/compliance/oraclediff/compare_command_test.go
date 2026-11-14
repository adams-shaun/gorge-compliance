package oraclediff

import (
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
)

func TestCompareCommandOptIn(t *testing.T) {
	g := rules.OracleResult{Snapshots: []rules.OracleSnapshot{snap("setup")}}
	x := XResult{Snapshots: []rules.OracleSnapshot{xsnap("setup")}}
	g.Snapshots[0].Players[0].Command = []string{"Isamaru, Hound of Konda"}
	x.Snapshots[0].Players[0].Command = []string{}
	if v := CompareOpts(g, nil, x, nil); v.Status != Agree {
		t.Fatalf("command compared without the opt-in: %+v", v)
	}
	v := CompareOpts(g, nil, x, []string{CompareCommand})
	if v.Status != Diverge || v.Field != "p0.command" || v.Gorge != "[Isamaru, Hound of Konda]" || v.XMage != "[]" {
		t.Fatalf("opted in: got %+v, want p0.command divergence", v)
	}
	x.Snapshots[0].Players[0].Command = []string{"Isamaru, Hound of Konda"}
	if v := CompareOpts(g, nil, x, []string{CompareCommand}); v.Status != Agree {
		t.Fatalf("equal command zones: %+v", v)
	}
}

func TestCompareCommandKeepsFieldOrder(t *testing.T) {
	s := snap("setup")
	names := func(compare []string) []string {
		var out []string
		for _, f := range fields(s, false, compare) {
			out = append(out, f.name)
		}
		return out
	}
	base := names(nil)
	want := []string{"turn", "step", "active", "over", "players"}
	for _, p := range []string{"p0", "p1"} {
		for _, f := range []string{"life", "counters", "hand", "graveyard", "exile", "library_count", "library_top", "pool"} {
			want = append(want, p+"."+f)
		}
	}
	want = append(want, "permanents", "stack")
	if !slices.Equal(base, want) {
		t.Fatalf("default fields moved:\n got %v\nwant %v", base, want)
	}
	var wantOn []string
	for _, n := range want {
		wantOn = append(wantOn, n)
		if strings.HasSuffix(n, ".exile") {
			base := strings.TrimSuffix(n, ".exile")
			wantOn = append(wantOn, base+".command", base+".commander_casts", base+".commander_damage")
		}
	}
	if got := names([]string{CompareCommand}); !slices.Equal(got, wantOn) {
		t.Fatalf("with command:\n got %v\nwant %v", got, wantOn)
	}
}

func TestValidateCompareCommand(t *testing.T) {
	if err := ValidateCompare([]string{"command"}); err != nil {
		t.Fatalf("command rejected: %v", err)
	}
	err := ValidateCompare([]string{"commandx"})
	if err == nil || !strings.Contains(err.Error(), `"command"`) {
		t.Fatalf("commandx: got %v, want an error naming command", err)
	}
}

func TestCompareCommanderTallies(t *testing.T) {
	g := rules.OracleResult{Snapshots: []rules.OracleSnapshot{snap("setup")}}
	x := XResult{Snapshots: []rules.OracleSnapshot{xsnap("setup")}}
	g.Snapshots[0].Players[1].CommanderDamage = map[string]int32{"p0:Isamaru, Hound of Konda": 3}
	if v := CompareOpts(g, nil, x, nil); v.Status != Agree {
		t.Fatalf("tallies compared without the opt-in: %+v", v)
	}
	v := CompareOpts(g, nil, x, []string{CompareCommand})
	if v.Status != Diverge || v.Field != "p1.commander_damage" || v.Gorge != "p0:Isamaru, Hound of Konda=3" {
		t.Fatalf("opted in: got %+v, want p1.commander_damage divergence", v)
	}
	x.Snapshots[0].Players[1].CommanderDamage = map[string]int32{"p0:Isamaru, Hound of Konda": 3}
	g.Snapshots[0].Players[0].CommanderCasts = map[string]int32{"B": 2, "A": 1, "Z": 0}
	x.Snapshots[0].Players[0].CommanderCasts = map[string]int32{"A": 1}
	v = CompareOpts(g, nil, x, []string{CompareCommand})
	if v.Status != Diverge || v.Field != "p0.commander_casts" || v.Gorge != "A=1,B=2" || v.XMage != "A=1" {
		t.Fatalf("casts: got %+v, want p0.commander_casts A=1,B=2 vs A=1", v)
	}
	x.Snapshots[0].Players[0].CommanderCasts = map[string]int32{"B": 2, "A": 1}
	if v := CompareOpts(g, nil, x, []string{CompareCommand}); v.Status != Agree {
		t.Fatalf("equal tallies: %+v", v)
	}
}
