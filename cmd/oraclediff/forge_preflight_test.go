package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/follower"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/xprep"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// passes is four priority passes: two rounds, both seats.
const passes = `{"op":"pass","seat":0},{"op":"pass","seat":1},{"op":"pass","seat":0},{"op":"pass","seat":1}`

// TestForgeExportRefusesUnknownCard: forge-export asks the runner's
// --preflight which cards the pinned Forge cannot resolve, and an item naming
// one is a prep row beside the requests, not a request. The three names the
// xengine farm caught as Forge harness "Couldn't find a card" (Gobland,
// Indicate, Mox Poison — corpus cards the Forge pin's cardsfolder predates)
// are refused; Grizzly Bears and Shock are not.
func TestForgeExportRefusesUnknownCard(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	fl, _ := follower.Lookup("forge")
	runner := filepath.Join(followerFixtures, "preflight.sh")
	dir := t.TempDir()
	namesCopy := filepath.Join(dir, "names-seen.txt")
	t.Setenv("FAKE_NAMES_COPY", namesCopy)
	t.Setenv("FAKE_REFUSE", "Gobland|Indicate|Mox Poison")
	scen := filepath.Join(dir, "items.jsonl")
	item := func(id, zone, card, other string) string {
		s := `{"id":"` + id + `","card":"` + card + `","template":"hand","setup":{"p0":{"` + zone + `":["` + card + `"]}`
		if other != "" {
			s += `,"p1":{"battlefield":["` + other + `"]}`
		}
		return s + `},"steps":[` + passes + `]}` + "\n"
	}
	// A refused card sits on the battlefield (as the farm's finds had it) or
	// in a hand; the clean item is the sent one.
	items := item("gob", "battlefield", "Gobland", "") +
		item("ind", "hand", "Indicate", "") +
		item("mox", "hand", "Mox Poison", "") +
		item("shock", "hand", "Shock", "Grizzly Bears")
	if err := os.WriteFile(scen, []byte(items), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "forge-req.jsonl")
	st, err := forgeExportRefusing(reg, fl, runner, scen, out)
	if err != nil {
		t.Fatal(err)
	}
	if st.Items != 4 || st.Sent != 1 || st.Refused != 3 || st.Asked != 5 || st.Lacking != 3 {
		t.Errorf("stats = %+v, want 4 items, 1 sent, 3 refused, 5 asked, 3 lacking", st)
	}
	names, _ := os.ReadFile(namesCopy)
	if got, want := string(names), "Gobland\nGrizzly Bears\nIndicate\nMox Poison\nShock\n"; got != want {
		t.Errorf("names file = %q, want %q", got, want)
	}

	prep, err := os.ReadFile(filepath.Join(dir, "forge-prep.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var ref []xprep.Refusal
	for _, l := range strings.Split(strings.TrimRight(string(prep), "\n"), "\n") {
		var r xprep.Refusal
		if err := json.Unmarshal([]byte(l), &r); err != nil {
			t.Fatal(err)
		}
		ref = append(ref, r)
	}
	want := []xprep.Refusal{
		{ID: "gob", Reason: "forge_lacks_card", Detail: "Gobland: not in the fake database"},
		{ID: "ind", Reason: "forge_lacks_card", Detail: "Indicate: not in the fake database"},
		{ID: "mox", Reason: "forge_lacks_card", Detail: "Mox Poison: not in the fake database"},
	}
	if !reflect.DeepEqual(ref, want) {
		t.Errorf("prep rows = %+v, want %+v", ref, want)
	}

	req, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(req), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("%d request lines, want 1 (the clean item):\n%s", len(lines), req)
	}
	var rq forgeRequest
	if err := json.Unmarshal([]byte(lines[0]), &rq); err != nil {
		t.Fatal(err)
	}
	if rq.ID != "shock" {
		t.Errorf("the sent request is %q, want shock", rq.ID)
	}
}

// TestForgeExportDefaultRunner: runForgeExport's -runner defaults to the
// registry's runner for forge, so the sweep's forge lane and a manual
// forge-export both preflight through forge-oracle-run.sh — and an explicit
// -runner overrides it, as forge-pass.sh and follower-check.sh now pass one.
// The defaulting is exercised for real: run from a temporary directory,
// where the relative default "scripts/forge-oracle-run.sh" does not exist,
// so the preflight exec's failure names the runner that was used.
func TestForgeExportDefaultRunner(t *testing.T) {
	fl, ok := follower.Lookup("forge")
	if !ok {
		t.Fatal("no forge follower in the registry")
	}
	if want := "scripts/forge-oracle-run.sh"; fl.Runner != want {
		t.Errorf("forge runner = %q, want %q", fl.Runner, want)
	}
	if fl.Prep != follower.PrepForgeExport {
		t.Errorf("forge prep = %v, want PrepForgeExport", fl.Prep)
	}

	repo, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	// runForgeExport loads its own registry from -cards; without the corpus
	// there is nothing to default for.
	cards := filepath.Join(repo, ".cards")
	if _, err := os.Stat(cards); err != nil {
		t.Skipf("no .cards corpus at %s: run make fetch-cards compile-cards", cards)
	}
	// The fake fixture, resolved before the cwd moves to the temp directory.
	fake, err := filepath.Abs(filepath.Join(followerFixtures, "preflight.sh"))
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	t.Chdir(tmp)
	scen := filepath.Join(tmp, "items.jsonl")
	if err := os.WriteFile(scen, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	// No -runner: the default (the registry's runner, relative to the
	// repository root) is used, and from this cwd it cannot be executed —
	// the error must name it.
	out := filepath.Join(tmp, "forge-req.jsonl")
	if err = runForgeExport([]string{"-cards", cards, "-scenarios", scen, "-out", out}); err == nil ||
		!strings.Contains(err.Error(), "scripts/forge-oracle-run.sh --preflight") {
		t.Fatalf("default runner error = %v, want one naming scripts/forge-oracle-run.sh --preflight", err)
	}

	// An explicit -runner wins: the fake fixture answers, the export runs,
	// and with an empty scenario file it writes an empty request file.
	out2 := filepath.Join(tmp, "forge-req2.jsonl")
	if err := runForgeExport([]string{"-cards", cards, "-scenarios", scen, "-out", out2, "-runner", fake}); err != nil {
		t.Fatalf("explicit -runner: %v", err)
	}
	req, err := os.ReadFile(out2)
	if err != nil {
		t.Fatal(err)
	}
	if len(req) != 0 {
		t.Errorf("explicit runner wrote %d request bytes for 0 items, want 0", len(req))
	}
}
