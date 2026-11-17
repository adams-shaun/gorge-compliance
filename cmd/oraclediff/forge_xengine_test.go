package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/gate"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/cov"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/lower"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestForgeExportXengineItem: forge-export takes the items an xengine play
// writes unchanged. Two FDN games are played and lowered the way `xengine
// play` does; every request line decodes, carries gorge_decisions (a list, not
// null) and has an abilities entry for every activate step whose card resolves
// to a corpus ability.
func TestForgeExportXengineItem(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	cands, _, err := decks.Candidates(reg, "../../oracle/compliance/manifests", "FDN")
	if err != nil {
		t.Fatal(err)
	}
	led := cov.New()
	var items bytes.Buffer
	for g := 0; g < 2; g++ {
		pair := decks.Pair(cands, led, "FDN", 0, g, 1)
		tp, err := game.Play(reg, pair[:], 1_000_003+uint64(g), game.Options{Set: "FDN"})
		if err != nil {
			t.Fatal(err)
		}
		l, err := lower.Tape(reg, tp)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(l.Item)
		items.Write(append(b, '\n'))
	}
	dir := t.TempDir()
	scen, out := filepath.Join(dir, "items.jsonl"), filepath.Join(dir, "req.jsonl")
	if err := os.WriteFile(scen, items.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	n, err := forgeExport(reg, scen, out)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("exported %d requests, want 2", n)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	reqs := decodeRequests(t, raw)
	activates := 0
	for i, line := range bytes.Split(bytes.TrimRight(raw, "\n"), []byte("\n")) {
		var keys map[string]json.RawMessage
		if err := json.Unmarshal(line, &keys); err != nil {
			t.Fatal(err)
		}
		if gd := string(keys["gorge_decisions"]); gd == "" || gd == "null" {
			t.Errorf("request %d: gorge_decisions = %q, want a list", i, gd)
		}
		rq := reqs[i]
		if rq.Item.Template != lower.Template || rq.Abilities == nil {
			t.Errorf("request %d: template %q, abilities %v", i, rq.Item.Template, rq.Abilities)
		}
		for s, st := range rq.Item.Steps {
			if st.Op != "activate" || st.AbilityIndex == nil {
				continue
			}
			activates++
			c, ok := reg.Lookup(refCardName(st.Card))
			if !ok || len(c.Faces) == 0 {
				continue
			}
			f := c.Faces[0]
			if k := *st.AbilityIndex; k >= 0 && k < len(f.Abilities) && f.Abilities[k] != nil {
				if rq.Abilities[fmt.Sprint(s)] == "" {
					t.Errorf("request %d step %d (%s ability %d): no abilities entry", i, s, st.Card, k)
				}
			}
		}
	}
	if activates == 0 {
		t.Fatal("no activate step in the exported tapes: the test exercises nothing")
	}
}

// TestForgeExportDropsNamedManaAbilityChoice: an activate step that names its
// ability by ability_index already sends the driver that exact line, so the
// "Choose a mana ability of <name>" wheel decision at that step is dropped.
// The same decision on a cast step, and a colour pick of the source's
// any-colour ability, are real choices and stay.
func TestForgeExportDropsNamedManaAbilityChoice(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	item := func(st oraclegen.Step) oraclegen.Item {
		return oraclegen.Item{ID: "xe:manaab", Template: lower.Template,
			Scenario: oraclegen.Scenario{Steps: []oraclegen.Step{st}}}
	}
	activate := func(card string, k int) oraclegen.Step {
		return oraclegen.Step{Op: "activate", Seat: 0, Card: card, AbilityIndex: &k}
	}
	wheel := func(pick string, options int) rules.OracleDecision {
		return rules.OracleDecision{
			Step: 0, Seat: 0, Kind: "choose_n", Options: options, Min: 1, Max: 1,
			Picks: []string{pick}, PickIdx: []int{0}, PickRefs: []string{pick},
			PickKinds: []string{"mana"}, GorgeKind: "choose", First: pick, Via: "answer",
		}
	}
	build := func(it oraclegen.Item, d rules.OracleDecision) []rules.OracleDecision {
		return forgeBuildRequest(reg, it, rules.OracleResult{}, []rules.OracleDecision{d}, true).GorgeDecisions
	}

	// Turtle Lair's first ability is "{T}: Add {C}"; the wheel ask that chose
	// it is the decision the driver never poses.
	if got := build(item(activate("p0:Turtle Lair", 0)), wheel("Add C", 2)); len(got) != 0 {
		t.Fatalf("named mana-ability wheel exported: %+v", got)
	}
	// The union wheel flattens an explicit two-colour ability into one option
	// per colour; its pick still names the ability at ability_index.
	if got := build(item(activate("p0:Hidden Lair", 1)), wheel("Add B", 2)); len(got) != 0 {
		t.Fatalf("flattened combo mana-ability wheel exported: %+v", got)
	}
	// A cast's payment window is a real choice, and the payment array covers it.
	cast := oraclegen.Step{Op: "cast", Seat: 0, Card: "p0:Grizzly Bears"}
	if got := build(item(cast), wheel("Add C", 2)); len(got) != 1 {
		t.Fatalf("cast step decision dropped: %+v", got)
	}
	// Turtle Lair's second ability adds any colour: "Add W" is its colour ask,
	// not the wheel, so it must stay.
	if got := build(item(activate("p0:Turtle Lair", 1)), wheel("Add W", 5)); len(got) != 1 {
		t.Fatalf("any-colour colour pick dropped: %+v", got)
	}
}

// TestXengineForgeExportEmptyDeclaration: an xengine item's empty attack or
// block step reaches the Forge driver as a pass_to decision "priority" (which
// the driver completes without consuming a priority), the pass before a
// stated attack or block becomes a pass_to of that decision (which completes
// inside Forge's declaration callback), the scenario SHA still names the
// unrewritten item, and a non-xengine item and the other steps are left alone.
func TestXengineForgeExportEmptyDeclaration(t *testing.T) {
	steps := []oraclegen.Step{
		{Op: "pass_to", Step: "main1"},
		{Op: "attack", Seat: 1, Defender: "p0", Attackers: []string{}, Expect: []oraclegen.Expect{{}}},
		{Op: "block", Seat: 0, Blocks: [][2]string{}},
		{Op: "pass", Seat: 1},
		{Op: "attack", Seat: 0, Defender: "p1", Attackers: []string{"p0:Bear"}},
		{Op: "pass", Seat: 0},
		{Op: "block", Seat: 1, Blocks: [][2]string{{"p1:Bear", "p0:Lion"}}},
		{Op: "attack", Seat: 0, Defender: "p1"},
	}
	ops := []string{"pass_to", "attack", "block", "pass", "attack", "pass", "block", "attack"}
	mk := func(tmpl string) oraclegen.Item {
		return oraclegen.Item{ID: "xe:t", Template: tmpl, Scenario: oraclegen.Scenario{Steps: steps}}
	}
	got := forgeDeclarations(mk(lower.Template))
	if len(got.Steps) != len(steps) {
		t.Fatalf("step count %d, want %d", len(got.Steps), len(steps))
	}
	for _, i := range []int{1, 2, 7} {
		if g := got.Steps[i]; g.Op != "pass_to" || g.Decision != "priority" || g.Step != "" || g.Active != "" {
			t.Errorf("empty declaration %d: %+v", i, g)
		}
	}
	if g := got.Steps[1]; len(g.Expect) != 1 {
		t.Errorf("empty attack lost its expectations: %+v", g)
	}
	for i, want := range map[int]string{3: "attackers", 5: "blockers"} {
		if g := got.Steps[i]; g.Op != "pass_to" || g.Decision != want {
			t.Errorf("pass %d before a stated declaration: %+v, want pass_to %s", i, g, want)
		}
	}
	for _, i := range []int{0, 4, 6} {
		if got.Steps[i].Op != ops[i] {
			t.Errorf("step %d changed: %+v", i, got.Steps[i])
		}
	}
	for i, op := range ops {
		if steps[i].Op != op {
			t.Fatalf("the input item's step %d was mutated: %+v", i, steps[i])
		}
	}
	if g := forgeDeclarations(mk(lower.WindowTemplate)); g.Steps[1].Op != "pass_to" {
		t.Errorf("window item not rewritten: %+v", g.Steps[1])
	}
	for i, st := range forgeDeclarations(mk("other")).Steps {
		if st.Op != ops[i] {
			t.Errorf("non-xengine item rewritten at %d: %+v", i, st)
		}
	}

	reg := testutil.CorpusRegistry(t)
	dir := t.TempDir()
	scen, out := filepath.Join(dir, "items.jsonl"), filepath.Join(dir, "req.jsonl")
	it := mk(lower.Template)
	b, _ := json.Marshal(it)
	if err := os.WriteFile(scen, append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := forgeExport(reg, scen, out); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	rq := decodeRequests(t, raw)[0]
	if rq.Item.Steps[1].Op != "pass_to" || rq.Item.Steps[1].Decision != "priority" || rq.Item.Steps[5].Decision != "blockers" {
		t.Errorf("exported steps %+v", rq.Item.Steps)
	}
	if rq.ScenarioSHA != gate.ItemSHA(it) {
		t.Errorf("scenario_sha %s, want the unrewritten item's %s", rq.ScenarioSHA, gate.ItemSHA(it))
	}
}

// TestForgeDropsDeclarationDecisions: the driver declares from the step's own
// list and never takes gorge's attackers/blockers decision, so that decision
// must not travel in the request for an empty declaration (rewritten to a
// pass_to "priority") or a stated one (calib-forge-3 BLB xe:BLB:0:11:w3: step
// 10 attacks with Valley Mightcaller and the decision was left over). Other
// kinds at the same step, a declaration kind at another step, and non-xengine
// items are kept.
func TestForgeDropsDeclarationDecisions(t *testing.T) {
	steps := []oraclegen.Step{
		{Op: "pass_to", Step: "main1"},
		{Op: "attack", Seat: 1, Defender: "p0", Attackers: []string{}},
		{Op: "block", Seat: 0, Blocks: [][2]string{}},
		{Op: "pass", Seat: 0},
		{Op: "attack", Seat: 0, Defender: "p1", Attackers: []string{"p0:Bear"}},
	}
	ds := []rules.OracleDecision{
		{Step: 1, Kind: "attackers"},
		{Step: 1, Kind: "target"},
		{Step: 2, Kind: "blockers"},
		{Step: 2, Kind: "attackers"},
		{Step: 4, Kind: "attackers", Picks: []string{"Attack with Bear at b"}},
	}
	mk := func(tmpl string) oraclegen.Item {
		return oraclegen.Item{ID: "xe:t", Template: tmpl, Scenario: oraclegen.Scenario{Steps: steps}}
	}
	got := forgeDropDeclarationDecisions(mk(lower.Template), ds)
	want := []rules.OracleDecision{ds[1], ds[3]}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("decisions %+v, want %+v", got, want)
	}
	if len(ds) != 5 || ds[0].Kind != "attackers" {
		t.Errorf("the input decisions were mutated: %+v", ds)
	}
	if got := forgeDropDeclarationDecisions(mk("other"), ds); len(got) != len(ds) {
		t.Errorf("non-xengine item lost decisions: %+v", got)
	}

	rq := forgeBuildRequest(testutil.CorpusRegistry(t), mk(lower.Template), rules.OracleResult{}, ds, true)
	if !reflect.DeepEqual(rq.GorgeDecisions, want) {
		t.Errorf("exported decisions %+v, want %+v", rq.GorgeDecisions, want)
	}
	if rq.Item.Steps[1].Op != "pass_to" || rq.Item.Steps[1].Decision != "priority" {
		t.Errorf("empty attack not rewritten: %+v", rq.Item.Steps[1])
	}
}

// TestForgeExportLibrarySearchPick: an Evolving Wilds activated and resolved
// by passes (the shape xengine lowers) logs gorge's search pick on the pass
// that resolves it, the step Forge's driver is at when Forge asks, so the
// export keeps that decision there as the object choice the driver consumes.
// When the item ends on the pass that leaves the ability on the stack, gorge
// poses no search at all, the export carries none, and Forge's ask past the
// last step is a trailing miss the classifier absorbs
// (oracle/xengine/classify TestClassifyForgeTrailingMiss), not an export gap.
func TestForgeExportLibrarySearchPick(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	k := 0
	build := func(steps ...oraclegen.Step) []rules.OracleDecision {
		it := oraclegen.Item{ID: "xe:search", Template: lower.Template, Scenario: oraclegen.Scenario{
			Name: "search", Setup: map[string]oraclegen.Seat{"p0": {
				Battlefield: []string{"Evolving Wilds"}, LibraryTop: []string{"Forest"}}},
			Steps: steps}}
		rq, _ := forgeExportItem(reg, it, true)
		return rq.GorgeDecisions
	}
	act := oraclegen.Step{Op: "activate", Seat: 0, Card: "p0:Evolving Wilds", AbilityIndex: &k}
	pass0 := oraclegen.Step{Op: "pass", Seat: 0}
	resolve := oraclegen.Step{Op: "pass", Seat: 1,
		Answers: []oraclegen.Answer{{Kind: "choose", Pick: []string{"p0:Forest"}}}}

	got := build(act, pass0, resolve)
	if len(got) != 1 {
		t.Fatalf("resolved search: %d decisions, want 1: %+v", len(got), got)
	}
	d := got[0]
	if d.Step != 2 || d.Seat != 0 || d.Kind != "choose_n" || !reflect.DeepEqual(d.PickKinds, []string{"search"}) ||
		!reflect.DeepEqual(d.PickRefs, []string{"p0:Forest"}) || d.Min != 0 || d.Max != 1 {
		t.Errorf("search decision = step %d seat %d %s kinds %v refs %v min %d max %d, want step 2 seat 0 choose_n [search] [p0:Forest] 0..1",
			d.Step, d.Seat, d.Kind, d.PickKinds, d.PickRefs, d.Min, d.Max)
	}

	if got := build(act, pass0); len(got) != 0 {
		t.Errorf("item ending on the pass before resolution carries decisions: %+v", got)
	}
}

// TestForgeExportExploreYesNo: gorge's explore pick (CR 701.44a) is a
// choose_n with resume "explore" and one pick of kind "graveyard" or "top";
// Forge poses it as the yes/no "Put <card> in your graveyard?". The export
// rewrites it to a choose_n whose single pick has kind yes (graveyard) or no
// (back on top), the shape the driver's DecisionQueue.isYesNo matches, on
// the same step and seat, with no object refs.
func TestForgeExportExploreYesNo(t *testing.T) {
	mk := func(pick, label string, idx int) rules.OracleDecision {
		return rules.OracleDecision{
			Step: 1, Seat: 0, Kind: "choose_n", Options: 2,
			Picks: []string{label}, PickIdx: []int{idx},
			PickRefs: []string{"p0:Grizzly Bears"}, PickRefsInexact: []bool{true},
			OptionRefs:  []string{"p0:Grizzly Bears", "p0:Grizzly Bears"},
			ObjectPicks: []string{"p0:Grizzly Bears"}, PickKinds: []string{pick},
			Resume: "explore", GorgeKind: "choose", First: "Put it into your graveyard", Min: 1, Max: 1,
		}
	}
	other := rules.OracleDecision{Step: 0, Seat: 0, Kind: "choose_n", Picks: []string{"Forest"},
		PickRefs: []string{"p0:Forest"}, PickKinds: []string{"search"}, Resume: "search"}
	in := []rules.OracleDecision{other,
		mk("graveyard", "Put it into your graveyard", 0),
		mk("top", "Put it back on top of your library", 1),
		mk("keep", "Keep it", 1)}
	orig := fmt.Sprintf("%+v", in)
	got := forgeExploreAsYesNo(in)
	if fmt.Sprintf("%+v", in) != orig {
		t.Fatalf("input modified: %+v", in)
	}
	if len(got) != 4 || !reflect.DeepEqual(got[0], other) {
		t.Fatalf("non-explore decision changed or count off: %+v", got)
	}
	for i, want := range []struct{ label, kind string }{{"Yes", "yes"}, {"No", "no"}, {"No", "no"}} {
		d := got[i+1]
		if d.Step != 1 || d.Seat != 0 || d.Kind != "choose_n" ||
			!reflect.DeepEqual(d.Picks, []string{want.label}) || !reflect.DeepEqual(d.PickKinds, []string{want.kind}) ||
			len(d.PickRefs) != 0 || len(d.ObjectPicks) != 0 || len(d.OptionRefs) != 0 {
			t.Errorf("explore decision %d = %+v, want step 1 seat 0 choose_n [%s] [%s] no refs", i, d, want.label, want.kind)
		}
	}
	if same := forgeExploreAsYesNo([]rules.OracleDecision{other}); len(same) != 1 || !reflect.DeepEqual(same[0], other) {
		t.Errorf("list with no explore pick changed: %+v", same)
	}

	// End to end: the real explore of Merfolk Branchwalker, graveyard pick.
	reg := testutil.CorpusRegistry(t)
	it := oraclegen.Item{ID: "xe:explore", Template: lower.Template, Scenario: oraclegen.Scenario{
		Name: "explore", Setup: map[string]oraclegen.Seat{"p0": {
			Hand: []string{"Merfolk Branchwalker"}, Battlefield: []string{"Forest", "Forest", "Forest"},
			LibraryTop: []string{"Grizzly Bears", "Forest", "Forest"}}},
		Steps: []oraclegen.Step{{Op: "cast", Seat: 0, Card: "p0:Merfolk Branchwalker", Mana: "GG"},
			{Op: "resolve", Seat: 0}, {Op: "resolve", Seat: 0}}}}
	rq, _ := forgeExportItem(reg, it, true)
	if len(rq.GorgeDecisions) != 1 {
		t.Fatalf("explore item: %d decisions, want 1: %+v", len(rq.GorgeDecisions), rq.GorgeDecisions)
	}
	d := rq.GorgeDecisions[0]
	if d.Step != 1 || d.Kind != "choose_n" || !reflect.DeepEqual(d.PickKinds, []string{"yes"}) || !reflect.DeepEqual(d.Picks, []string{"Yes"}) {
		t.Errorf("exported explore decision = %+v, want step 1 choose_n [yes] [Yes]", d)
	}
}
