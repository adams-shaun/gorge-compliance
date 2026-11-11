package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/follower"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/xprep"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

const followerFixtures = "testdata/followerexport"

// fakeFollower is a follower-export follower the registry does not hold; its
// runner is the test preflight script.
func fakeFollower(t *testing.T) (follower.Follower, string) {
	t.Helper()
	runner, err := filepath.Abs(filepath.Join(followerFixtures, "preflight.sh"))
	if err != nil {
		t.Fatal(err)
	}
	return follower.Follower{Name: "fakefol", Display: "FakeFol", Runner: runner, Prep: follower.PrepFollowerExport}, runner
}

func splitLines(b []byte) [][]byte {
	b = bytes.TrimRight(b, "\n")
	if len(b) == 0 {
		return nil
	}
	return bytes.Split(b, []byte("\n"))
}

// TestFollowerExportRefusalSplit: an item naming a card the preflight refuses
// is a prep row, not a request; every other item's request is forge-export's
// line for it, byte for byte, plus "neutral" (so the scenario_sha, the
// decisions and the rewrites are forge-export's). The names file lists the
// cards and no player ref.
func TestFollowerExportRefusalSplit(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	fl, runner := fakeFollower(t)
	dir := t.TempDir()
	namesCopy := filepath.Join(dir, "names-seen.txt")
	t.Setenv("FAKE_NAMES_COPY", namesCopy)
	t.Setenv("FAKE_REFUSE", "Collector's Cage")
	out := filepath.Join(dir, "fakefol-req.jsonl")
	st, err := followerExport(reg, fl, runner, filepath.Join(forgeFixtures, "items.jsonl"), out)
	if err != nil {
		t.Fatal(err)
	}
	if st.Items != 3 || st.Sent != 2 || st.Refused != 1 || st.Asked != 3 || st.Lacking != 1 {
		t.Errorf("stats = %+v, want 3 items, 2 sent, 1 refused, 3 asked, 1 lacking", st)
	}
	names, _ := os.ReadFile(namesCopy)
	if got, want := string(names), "Collector's Cage\nGrizzly Bears\nShock\n"; got != want {
		t.Errorf("names file = %q, want %q", got, want)
	}

	prep, err := os.ReadFile(filepath.Join(dir, "fakefol-prep.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var ref []xprep.Refusal
	for _, l := range splitLines(prep) {
		var r xprep.Refusal
		if err := json.Unmarshal(l, &r); err != nil {
			t.Fatal(err)
		}
		ref = append(ref, r)
	}
	want := []xprep.Refusal{{ID: "Collector's Cage/activate#0.0/v1", Reason: "fakefol_lacks_card", Detail: "Collector's Cage: not in the fake database"}}
	if !reflect.DeepEqual(ref, want) {
		t.Errorf("prep rows = %+v, want %+v", ref, want)
	}

	fwd, _ := exportFixture(t, reg)
	forgeLines := splitLines(fwd)
	got, _ := os.ReadFile(out)
	lines := splitLines(got)
	if len(lines) != 2 {
		t.Fatalf("%d request lines, want 2", len(lines))
	}
	for i, l := range lines {
		f := forgeLines[i] // the refused item is the last
		prefix := append(append([]byte(nil), f[:len(f)-1]...), []byte(`,"neutral":`)...)
		if !bytes.HasPrefix(l, prefix) {
			t.Errorf("request %d is not forge-export's line plus neutral:\n got %s\nwant %s...", i, l, prefix)
		}
		var rq followerRequest
		if err := json.Unmarshal(l, &rq); err != nil {
			t.Fatal(err)
		}
		if rq.Neutral.Ability == nil || rq.Neutral.Token == nil || rq.Neutral.Mode == nil {
			t.Errorf("request %d: neutral maps must encode as {}: %+v", i, rq.Neutral)
		}
	}

	// Refusing two cards: an item naming either is refused, with both
	// reasons when it names both.
	t.Setenv("FAKE_REFUSE", "Grizzly Bears|Collector's Cage")
	st, err = followerExport(reg, fl, runner, filepath.Join(forgeFixtures, "items.jsonl"), out)
	if err != nil {
		t.Fatal(err)
	}
	if st.Sent != 1 || st.Refused != 2 {
		t.Errorf("two refused cards: stats %+v, want 1 sent, 2 refused", st)
	}
	prep, _ = os.ReadFile(filepath.Join(dir, "fakefol-prep.jsonl"))
	if !strings.Contains(string(prep), `"detail":"Collector's Cage: not in the fake database; Grizzly Bears: not in the fake database"`) {
		t.Errorf("prep rows lack the two-card detail:\n%s", prep)
	}
}

// TestFollowerExportPreflightFailure: a preflight that exits nonzero, prints a
// line without a tab or names a card it was not asked about fails the whole
// export.
func TestFollowerExportPreflightFailure(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	fl, runner := fakeFollower(t)
	cases := []struct {
		env, val, want string
	}{
		{"FAKE_EXIT", "3", "exited 3"},
		{"FAKE_RAW", "Shock", "has no tab"},
		{"FAKE_RAW", "Lightning Bolt\tnope", "not asked about"},
	}
	for _, c := range cases {
		t.Run(c.env+"="+c.val, func(t *testing.T) {
			t.Setenv(c.env, c.val)
			out := filepath.Join(t.TempDir(), "req.jsonl")
			_, err := followerExport(reg, fl, runner, filepath.Join(forgeFixtures, "items.jsonl"), out)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want one containing %q", err, c.want)
			}
			if _, err := os.Stat(out); err == nil {
				t.Error("a failed preflight still wrote requests")
			}
		})
	}
	t.Run("missing runner", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "req.jsonl")
		if _, err := followerExport(reg, fl, filepath.Join(t.TempDir(), "absent.sh"), filepath.Join(forgeFixtures, "items.jsonl"), out); err == nil {
			t.Fatal("a missing runner exported")
		}
	})
}

// TestFollowerExportRefusesRepoPath: requests and prep rows stay out of the
// repository, like forge-export's.
func TestFollowerExportRefusesRepoPath(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	fl, runner := fakeFollower(t)
	if _, err := followerExport(reg, fl, runner, filepath.Join(forgeFixtures, "items.jsonl"), "req-inside.jsonl"); err == nil {
		os.Remove("req-inside.jsonl")
		os.Remove("fakefol-prep.jsonl")
		t.Fatal("follower-export wrote inside the repository")
	}
}

// TestFollowerExportEngineFlag: -engine must name a registered follower whose
// runner reads Forge requests; xmage's does not.
func TestFollowerExportEngineFlag(t *testing.T) {
	for _, e := range []string{"", "xmage", "nosuch"} {
		if err := runFollowerExport([]string{"-engine", e, "-scenarios", "x", "-out", "y"}); err == nil || !strings.Contains(err.Error(), "reads Forge requests") {
			t.Errorf("-engine %q: err = %v", e, err)
		}
	}
	fl, _ := fakeFollower(t)
	defer follower.RegisterForTest(fl)()
	if got := forgeRequestFollowers(); !reflect.DeepEqual(got, []string{"fakefol", "forge"}) {
		t.Errorf("forge-request followers = %v, want fakefol and forge", got)
	}
}

// syntheticRegistry compiles syntheticScripts.
func syntheticRegistry(t *testing.T) *cards.Registry {
	t.Helper()
	root := t.TempDir()
	for rel, src := range syntheticScripts {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	reg, _, err := cards.CompileDir(filepath.Join(root, "cardsfolder"))
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

func ip(i int) *int { return &i }

// TestNeutralIdentities: the neutral block on synthetic cards. Ordinals count
// per class in script order (a land with two mana and two other activated
// abilities), an intrinsic Forest ability is its own class, loyalty costs read
// +N/-N, a keyword-minted Equip carries its keyword instead of an ordinal,
// setup tokens carry their characteristics and a charm's pick its Choices$
// position.
func TestNeutralIdentities(t *testing.T) {
	reg := syntheticRegistry(t)
	act := func(card string, k int) oraclegen.Step {
		return oraclegen.Step{Op: "activate", Seat: 0, Card: card, AbilityIndex: ip(k)}
	}
	it := oraclegen.Item{ID: "synthetic", Scenario: oraclegen.Scenario{
		Setup: map[string]oraclegen.Seat{
			"p0": {
				Battlefield: []string{"Test Barrens", "Test Grove", "Test Walker", "Test Blade"},
				Tokens:      []oraclegen.SetupToken{{Script: "wu_1_1_test_spirit_flying"}, {Script: "c_test_clue"}, {Script: "wu_1_1_test_spirit_flying"}},
			},
			"p1": {Tokens: []oraclegen.SetupToken{{Script: "no_such_stem"}}},
		},
		Steps: []oraclegen.Step{
			act("p0:Test Barrens", 0),
			act("p0:Test Barrens", 1),
			act("p0:Test Barrens", 2),
			act("p0:Test Barrens", 3),
			act("p0:Test Grove", 0),
			act("p0:Test Walker", 0),
			act("p0:Test Walker", 1),
			act("p0:Test Blade", 0),
			act("p0:token:Clue Token", 0),
			{Op: "cast", Seat: 0, Card: "p0:Test Charm"},
			act("p0:Nowhere", 0),
		},
	}}
	charm := rules.OracleDecision{Step: 9, Kind: "mode", Resume: "cast_modes",
		Picks: []string{"CARDNAME deals 2 damage to target player.", "You gain 3 life."}, PickIdx: []int{0, 1},
		OptionRefs: []string{"p0:Test Charm", "p0:Test Charm"}, PickRefs: []string{"p0:Test Charm", "p0:Test Charm"}}
	discard := rules.OracleDecision{Step: 9, Kind: "mode", Resume: "discard", Picks: []string{"Discard Test Grove"}}
	unknown := rules.OracleDecision{Step: 9, Kind: "mode", Resume: "cast_modes", Picks: []string{"Not a mode"}, OptionRefs: []string{"p0:Test Charm"}}
	notCharm := rules.OracleDecision{Step: 9, Kind: "mode", Resume: "modes", Picks: []string{"Double Strike"}, OptionRefs: []string{"p0:Test Walker"}}
	rq := forgeRequest{Item: it, GorgeDecisions: []rules.OracleDecision{discard, charm, unknown, notCharm}}
	c := census{}
	n := neutralFor(reg, rq, rules.OracleResult{}, c)

	wantAbility := map[string]neutralAbility{
		"0": {Class: "mana", Ordinal: ip(0), Cost: "{T}", Text: "Add {C}.", Produces: "C"},
		"1": {Class: "activated", Ordinal: ip(0), Cost: "{4}", Text: "Put two +1/+1 counters on ~."},
		"2": {Class: "mana", Ordinal: ip(1), Cost: "{1}, {T}", Text: "Add {W} or {U}.", Produces: "WU"},
		"3": {Class: "activated", Ordinal: ip(1), Cost: "{2}, {T}, Sac<1/~>", Text: "Draw a card."},
		"4": {Class: "intrinsic_mana", Ordinal: ip(0), Cost: "{T}", Produces: "G"},
		"5": {Class: "loyalty", Ordinal: ip(0), Cost: "+1", Text: "You gain 2 life."},
		"6": {Class: "loyalty", Ordinal: ip(1), Cost: "-2", Text: "Draw a card."},
		"7": {Class: "activated", Keyword: "Equip:2", Cost: "{2}", Text: "Equip 2"},
		"8": {Class: "activated", Ordinal: ip(0), Cost: "{2}, Sac<1/~/this token>", Text: "Draw a card."},
	}
	if !reflect.DeepEqual(n.Ability, wantAbility) {
		gb, _ := json.MarshalIndent(n.Ability, "", " ")
		wb, _ := json.MarshalIndent(wantAbility, "", " ")
		t.Errorf("neutral.ability:\n got %s\nwant %s", gb, wb)
	}
	wu, empty := "WU", ""
	wantToken := map[string]neutralToken{
		"wu_1_1_test_spirit_flying": {Name: "Spirit Token", Supertypes: []string{"Legendary"}, Types: []string{"Creature"}, Subtypes: []string{"Spirit"},
			Colours: &wu, Power: ip(1), Toughness: ip(1), Keywords: []string{"Flying"}, Text: "Flying"},
		"c_test_clue": {Name: "Clue Token", Types: []string{"Artifact"}, Subtypes: []string{"Clue"}, Colours: &empty,
			Text: "Synthetic test clue.\nSecond line."},
	}
	if !reflect.DeepEqual(n.Token, wantToken) {
		gb, _ := json.MarshalIndent(n.Token, "", " ")
		wb, _ := json.MarshalIndent(wantToken, "", " ")
		t.Errorf("neutral.token:\n got %s\nwant %s", gb, wb)
	}
	wantMode := map[string]neutralMode{
		"1": {Step: 9, Picks: []neutralModePick{{Ordinal: 2, Text: "~ deals 2 damage to target player."}, {Ordinal: 0, Text: "You gain 3 life."}}},
	}
	if !reflect.DeepEqual(n.Mode, wantMode) {
		t.Errorf("neutral.mode = %+v, want %+v", n.Mode, wantMode)
	}
	for k, v := range map[string]int{
		"ability.steps":                       10,
		"ability.class":                       9,
		"ability.ordinal":                     8,
		"ability.ordinal_omit:keyword_minted": 1,
		"ability.omit:face_unresolved":        1,
		"ability.text_omit:intrinsic":         1,
		"token.stems":                         3,
		"token.filled":                        2,
		"token.omit:no_script":                1,
		"mode.decisions":                      3,
		"mode.filled":                         1,
		"mode.omit:label_unmatched":           1,
		"mode.omit:not_a_charm":               1,
	} {
		if c[k] != v {
			t.Errorf("census[%q] = %d, want %d (census %v)", k, c[k], v, c)
		}
	}
}

func TestNeutralCostAndProduces(t *testing.T) {
	for in, want := range map[string]string{
		"T":                                      "{T}",
		"1 W W T":                                "{1}{W}{W}, {T}",
		"X R":                                    "{X}{R}",
		"W/U 2/B G/P":                            "{W/U}{2/B}{G/P}",
		"Q PayLife<2>":                           "{Q}, PayLife<2>",
		"AddCounter<0/LOYALTY>":                  "+0",
		"SubCounter<X/LOYALTY>":                  "-X",
		"tapXType<2/Creature> Exile<1/CARDNAME>": "tapXType<2/Creature>, Exile<1/~>",
	} {
		if got, ok := neutralCost(in); !ok || got != want {
			t.Errorf("neutralCost(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	if _, ok := neutralCost(""); ok {
		t.Error("an empty cost normalised")
	}
	for in, want := range map[string]string{
		"C": "C", "Any": "any", "Combo Any": "any", "Combo G W": "WG", "R R": "R", "{G}": "G", "Chosen": "", "Combo ColorIdentity": "", "": "",
	} {
		got, ok := neutralProduces(&cards.SA{Kind: "AB", API: "Mana", Params: map[string]string{"Produced": in}})
		if got != want || ok != (want != "") {
			t.Errorf("neutralProduces(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
}
