package follower

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// preH1Engines is every -engine value the harness accepted before the
// registry, in the order its usage text listed them ("-engine xmage|forge"
// in cmd/xengine diff and calib). The registry must keep accepting each one.
var preH1Engines = []string{"xmage", "forge"}

func TestNamesCoverPreH1Engines(t *testing.T) {
	if got := Names(); !slices.Equal(got, preH1Engines) {
		t.Errorf("Names() = %q, want %q (usage text and order unchanged)", got, preH1Engines)
	}
	for _, n := range preH1Engines {
		if !Known(n) {
			t.Errorf("%q is no longer a known follower", n)
		}
		f, ok := Lookup(n)
		if !ok || f.Name != n {
			t.Errorf("Lookup(%q) = %+v, %v", n, f, ok)
		}
	}
}

func TestLookupUnknown(t *testing.T) {
	for _, n := range []string{"", "XMage", "Forge", "gorge", "both", "phase"} {
		if Known(n) {
			t.Errorf("Known(%q) = true", n)
		}
		if f, ok := Lookup(n); ok || f.Name != "" || f.ConsumesPayment || f.CastModes != nil {
			t.Errorf("Lookup(%q) = %+v, %v; want the zero Follower", n, f, ok)
		}
	}
}

func TestNamesIsACopy(t *testing.T) {
	n := Names()
	n[0] = "mutated"
	if Names()[0] == "mutated" {
		t.Error("Names() exposes the registry")
	}
}

// TestEntriesWellFormed: every entry has a unique name, a display name, a
// prep stage the harness knows, and a runner script that exists.
func TestEntriesWellFormed(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	seen := map[string]bool{}
	for _, f := range registry {
		if f.Name == "" || f.Display == "" || f.Runner == "" {
			t.Errorf("incomplete entry %+v", f)
		}
		if seen[f.Name] {
			t.Errorf("duplicate entry %q", f.Name)
		}
		seen[f.Name] = true
		if f.Prep != PrepXPrep && f.Prep != PrepForgeExport && f.Prep != PrepFollowerExport {
			t.Errorf("%s: unknown prep %q", f.Name, f.Prep)
		}
		if _, err := os.Stat(filepath.Join(root, f.Runner)); err != nil {
			t.Errorf("%s: runner: %v", f.Name, err)
		}
	}
}

// TestPreH1Behaviour pins the table to what the harness did when it compared
// engine names, so changing a quirk is a deliberate edit of this test too.
func TestPreH1Behaviour(t *testing.T) {
	x, _ := Lookup("xmage")
	f, _ := Lookup("forge")
	cases := []struct {
		name      string
		got, want any
	}{
		{"xmage runner", x.Runner, "scripts/xmage-oracle-run.sh"},
		{"forge runner", f.Runner, "scripts/forge-oracle-run.sh"},
		{"xmage prep", x.Prep, PrepXPrep},
		{"forge prep", f.Prep, PrepForgeExport},
		{"xmage display", x.Display, "XMage"},
		{"forge display", f.Display, "Forge"},
		{"xmage stamps engine", x.StampsEngine, false},
		{"forge stamps engine", f.StampsEngine, true},
		{"xmage consumes payment", x.ConsumesPayment, true},
		// TODO(H1 audit): Forge reads item.payment too; flipping this is a
		// behaviour change kept out of H1.
		{"forge consumes payment", f.ConsumesPayment, false},
		{"xmage turn cap", x.TurnCapMsg, ""},
		{"forge turn cap", f.TurnCapMsg, "passed turn "},
		{"xmage cast modes", x.CastModes == nil, true},
		{"xmage skips damage step", x.SkipsDamageStep, false},
		{"forge skips damage step", f.SkipsDamageStep, true},
		{"xmage stops in cleanup", x.StopsInCleanup, false},
		{"forge stops in cleanup", f.StopsInCleanup, true},
		{"xmage trailing misses", x.TrailingMisses, false},
		{"forge trailing misses", f.TrailingMisses, true},
		{"xmage taps with cast", x.TapsWithCast, true},
		{"forge taps with cast", f.TapsWithCast, false},
		{"xmage trigger order by name", x.TriggerOrderByName, true},
		{"forge trigger order by name", f.TriggerOrderByName, false},
		{"xmage lacks card", x.LacksCardMsg, "Couldn't find a card:"},
		{"forge lacks card", f.LacksCardMsg, ""},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %#v, want %#v", c.name, c.got, c.want)
		}
	}
	want := []string{"", "optionalcost",
		"plot", "adventure_alt", "omen_alt", "modal_spell", "warped",
		"kicked", "kicked1", "kicked2", "kickedboth", "buyback", "entwined",
		"offspring", "replicated", "multikicked"}
	if !slices.Equal(f.CastModes, want) {
		t.Errorf("forge cast modes = %q, want %q", f.CastModes, want)
	}
}

func TestFollowsCastMode(t *testing.T) {
	x, _ := Lookup("xmage")
	f, _ := Lookup("forge")
	for _, m := range []string{"", "foretell", "flashback", "kicked"} {
		if !x.FollowsCastMode(m) {
			t.Errorf("xmage does not follow %q; a nil CastModes follows every mode", m)
		}
	}
	for m, want := range map[string]bool{"": true, "kicked": true, "plot": true, "foretell": false, "flashback": false} {
		if got := f.FollowsCastMode(m); got != want {
			t.Errorf("forge FollowsCastMode(%q) = %v, want %v", m, got, want)
		}
	}
}
