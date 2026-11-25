package follower

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"testing"
)

// preH1Engines is every -engine value the harness accepted before the
// registry, in the order its usage text listed them ("-engine xmage|forge"
// in cmd/xengine diff and calib). The registry must keep accepting each one.
var preH1Engines = []string{"xmage", "forge"}

// registered is every engine, in registry order: the pre-H1 ones first, then
// the followers added since.
var registered = []string{"xmage", "forge", "phase", "manabrew"}

func TestNamesCoverPreH1Engines(t *testing.T) {
	if got := Names(); !slices.Equal(got, registered) || !slices.Equal(got[:len(preH1Engines)], preH1Engines) {
		t.Errorf("Names() = %q, want %q (pre-H1 usage text and order unchanged, then the newer followers)", got, registered)
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
	for _, n := range []string{"", "XMage", "Forge", "gorge", "both", "Phase", "phase-rs"} {
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
		{"xmage jvm", x.JVM, true},
		{"forge jvm", f.JVM, true},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %#v, want %#v", c.name, c.got, c.want)
		}
	}
	want := []string{"", "optionalcost",
		"plot", "suspend", "foretell", "adventure_alt", "omen_alt", "room_alt", "modal_spell", "warped",
		"mayplay", "alternative", "evoked", "morphed", "megamorphed", "disguised",
		"kicked", "kicked1", "kicked2", "kickedboth", "buyback", "entwined",
		"offspring", "replicated", "multikicked"}
	if !slices.Equal(f.CastModes, want) {
		t.Errorf("forge cast modes = %q, want %q", f.CastModes, want)
	}
}

// TestPhaseEntry pins the phase.rs entry (ticket ph-3-runner).
func TestPhaseEntry(t *testing.T) {
	p, ok := Lookup("phase")
	if !ok {
		t.Fatal("phase is not registered")
	}
	cases := []struct {
		name      string
		got, want any
	}{
		{"display", p.Display, "phase.rs"},
		{"runner", p.Runner, "scripts/phase-oracle-run.sh"},
		{"prep", p.Prep, PrepFollowerExport},
		{"stamps engine", p.StampsEngine, true},
		{"consumes payment", p.ConsumesPayment, true},
		{"turn cap", p.TurnCapMsg, ""},
		{"lacks card", p.LacksCardMsg, "phase lacks card: "},
		{"jvm", p.JVM, false},
		{"skips damage step", p.SkipsDamageStep, false},
		{"skips empty attack step", p.SkipsEmptyAttackStep, true},
		{"stops in cleanup", p.StopsInCleanup, false},
		{"trailing misses", p.TrailingMisses, false},
		{"taps with cast", p.TapsWithCast, false},
		{"trigger order by name", p.TriggerOrderByName, false},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("phase %s = %#v, want %#v", c.name, c.got, c.want)
		}
	}
	// The modes the answerer maps (CAST_MODES in followers/phase/phase-follower/src/steps.rs).
	wantModes := []string{"", "optionalcost", "adventure_alt", "omen_alt", "modal_spell", "warped",
		"kicked", "kicked1", "kicked2", "kickedboth", "buyback", "entwined", "offspring", "replicated", "multikicked"}
	if !slices.Equal(p.CastModes, wantModes) {
		t.Errorf("phase cast modes = %q, want %q", p.CastModes, wantModes)
	}
	// plot is gorge's special action, not phase's cast-from-exile variant: not followed.
	if p.FollowsCastMode("plot") {
		t.Errorf("phase must not follow cast_mode plot")
	}
	for _, m := range wantModes {
		if !p.FollowsCastMode(m) {
			t.Errorf("phase must follow cast_mode %q", m)
		}
	}
	for _, m := range []string{"flashback", "foretell", "escape", "casualty"} {
		if p.FollowsCastMode(m) {
			t.Errorf("phase must not follow cast_mode %q", m)
		}
	}
	if p.RunnerEnv() != "XENGINE_PHASE_RUNNER" || p.MemEnv() != "XENGINE_PHASE_MEM" || p.WaitEnv() != "XENGINE_PHASE_WAIT" {
		t.Errorf("phase env names %s %s %s", p.RunnerEnv(), p.MemEnv(), p.WaitEnv())
	}
}

// TestManabrewEntry pins the Manabrew entry (ticket mb-5-runner).
func TestManabrewEntry(t *testing.T) {
	m, ok := Lookup("manabrew")
	if !ok {
		t.Fatal("manabrew is not registered")
	}
	cases := []struct {
		name      string
		got, want any
	}{
		{"display", m.Display, "Manabrew"},
		{"runner", m.Runner, "scripts/manabrew-oracle-run.sh"},
		{"prep", m.Prep, PrepFollowerExport},
		{"stamps engine", m.StampsEngine, true},
		{"consumes payment", m.ConsumesPayment, true},
		{"turn cap", m.TurnCapMsg, ""},
		{"lacks card", m.LacksCardMsg, "manabrew lacks card: "},
		{"jvm", m.JVM, false},
		{"skips damage step", m.SkipsDamageStep, false},
		{"stops in cleanup", m.StopsInCleanup, false},
		{"trailing misses", m.TrailingMisses, false},
		{"taps with cast", m.TapsWithCast, false},
		{"trigger order by name", m.TriggerOrderByName, false},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("manabrew %s = %#v, want %#v", c.name, c.got, c.want)
		}
	}
	wantModes := []string{"", "adventure_alt", "warped", "kicked", "kicked1", "buyback", "entwined", "multikicked", "replicated"}
	if !slices.Equal(m.CastModes, wantModes) {
		t.Errorf("manabrew cast modes = %q, want %q", m.CastModes, wantModes)
	}
	for _, mode := range wantModes {
		if !m.FollowsCastMode(mode) {
			t.Errorf("manabrew must follow cast mode %q", mode)
		}
	}
	for _, mode := range []string{"omen_alt", "modal_spell", "plot", "kicked2", "kickedboth", "offspring", "optionalcost", "flashback"} {
		if m.FollowsCastMode(mode) {
			t.Errorf("manabrew must not follow cast mode %q: the engine has no such cast", mode)
		}
	}
	if m.RunnerEnv() != "XENGINE_MANABREW_RUNNER" || m.MemEnv() != "XENGINE_MANABREW_MEM" || m.WaitEnv() != "XENGINE_MANABREW_WAIT" {
		t.Errorf("manabrew env names %s %s %s", m.RunnerEnv(), m.MemEnv(), m.WaitEnv())
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
	for m, want := range map[string]bool{"": true, "kicked": true, "plot": true, "suspend": true, "room_alt": true, "mayplay": true, "alternative": true, "evoked": true, "morphed": true, "megamorphed": true, "disguised": true, "foretell": true, "flashback": false} {
		if got := f.FollowsCastMode(m); got != want {
			t.Errorf("forge FollowsCastMode(%q) = %v, want %v", m, got, want)
		}
	}
	// The registry is kept by hand beside the driver, so a mode the driver
	// grew can stay missing from it (mayplay and alternative did). Every
	// mode a Forge driver regression script exercises must be one the
	// registry follows: a script is the repo's evidence that the driver now
	// acts on that mode, so the next mode cannot land without this test
	// naming it.
	for _, m := range forgeScriptCastModes(t, filepath.Join("..", "..", "..", "scripts")) {
		if !f.FollowsCastMode(m) {
			t.Errorf("a Forge driver script exercises cast_mode %q but the registry does not follow it", m)
		}
	}
}

// forgeScriptCastModes returns the cast_mode values the Forge driver
// regression scripts (scripts/forge-oracle-test-*.sh) name, sorted. The
// scripts spell a mode as `cast_mode "x"`, `cast_mode x` or JSON's
// `cast_mode":"x"`.
func forgeScriptCastModes(t *testing.T, dir string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "forge-oracle-test-*.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatalf("no Forge driver scripts under %s", dir)
	}
	re := regexp.MustCompile(`cast_mode[ \t"':]+([a-z][a-z0-9_]*)`)
	seen := map[string]bool{}
	var modes []string
	for _, path := range files {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range re.FindAllStringSubmatch(string(b), -1) {
			if !seen[m[1]] {
				seen[m[1]] = true
				modes = append(modes, m[1])
			}
		}
	}
	if len(modes) == 0 {
		t.Fatal("no cast_mode in the Forge driver scripts")
	}
	sort.Strings(modes)
	return modes
}

// TestEnvNames pins the sweep's per-follower variables to the names
// scripts/xengine-sweep.sh read before the registry, and the derivation for
// a name that is not a shell identifier.
func TestEnvNames(t *testing.T) {
	x, _ := Lookup("xmage")
	f, _ := Lookup("forge")
	odd := Follower{Name: "phase-rs.2"}
	cases := []struct{ got, want string }{
		{x.RunnerEnv(), "XENGINE_XMAGE_RUNNER"},
		{x.MemEnv(), "XENGINE_XMAGE_MEM"},
		{x.WaitEnv(), "XENGINE_XMAGE_WAIT"},
		{f.RunnerEnv(), "XENGINE_FORGE_RUNNER"},
		{f.MemEnv(), "XENGINE_FORGE_MEM"},
		{f.WaitEnv(), "XENGINE_FORGE_WAIT"},
		{odd.EnvPrefix(), "XENGINE_PHASE_RS_2"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
}

func TestAllIsACopy(t *testing.T) {
	a := All()
	if len(a) != len(registry) {
		t.Fatalf("All() has %d entries, registry %d", len(a), len(registry))
	}
	a[0].Name = "mutated"
	if All()[0].Name == "mutated" {
		t.Error("All() exposes the registry")
	}
}
