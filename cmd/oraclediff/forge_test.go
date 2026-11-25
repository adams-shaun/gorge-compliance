package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/gate"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclediff"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/lower"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

const forgeFixtures = "testdata/forge"

func fixtureLines(t *testing.T, name string) [][]byte {
	t.Helper()
	return readLinesBytes(t, filepath.Join(forgeFixtures, name))
}

// exportFixture exports the 3-item fixture to a fresh temp file and returns
// its bytes and path.
func exportFixture(t *testing.T, reg *cards.Registry) ([]byte, string) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "forge-req.jsonl")
	n, err := forgeExport(reg, filepath.Join(forgeFixtures, "items.jsonl"), out)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("exported %d requests, want 3", n)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return b, out
}

func decodeRequests(t *testing.T, b []byte) []forgeRequest {
	t.Helper()
	var out []forgeRequest
	for _, l := range bytes.Split(bytes.TrimRight(b, "\n"), []byte("\n")) {
		var rq forgeRequest
		if err := json.Unmarshal(l, &rq); err != nil {
			t.Fatal(err)
		}
		out = append(out, rq)
	}
	return out
}

// TestForgeExportByteStable: two exports of the same scenarios are
// byte-identical, and each request carries its Item exactly as gen wrote it
// (so ItemSHA is the scenario_sha), gorge's decisions and the ability line.
func TestForgeExportByteStable(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	a, _ := exportFixture(t, reg)
	b, _ := exportFixture(t, reg)
	if !bytes.Equal(a, b) {
		t.Fatalf("two exports differ:\n%s\n---\n%s", a, b)
	}
	items := fixtureLines(t, "items.jsonl")
	reqs := decodeRequests(t, a)
	if len(reqs) != len(items) {
		t.Fatalf("%d requests for %d items", len(reqs), len(items))
	}
	for i, rq := range reqs {
		var it oraclegen.Item
		if err := json.Unmarshal(items[i], &it); err != nil {
			t.Fatal(err)
		}
		itemBytes, _ := json.Marshal(rq.Item)
		if !bytes.Equal(itemBytes, items[i]) {
			t.Errorf("item %d is not verbatim:\n got %s\nwant %s", i, itemBytes, items[i])
		}
		if rq.ID != it.ID || rq.ScenarioSHA != gate.ItemSHA(it) {
			t.Errorf("item %d id/sha = %q/%q, want %q/%q", i, rq.ID, rq.ScenarioSHA, it.ID, gate.ItemSHA(it))
		}
		if rq.Abilities == nil || rq.GorgeDecisions == nil {
			t.Errorf("item %d: abilities/gorge_decisions must encode as {} and [], not null", i)
		}
	}
	// Precondition for the assertions below: the cast at a creature posed a
	// target decision gorge answered, and the Cage's step 0 names an index.
	if len(reqs[0].GorgeDecisions) == 0 || reqs[0].GorgeDecisions[0].Kind != "target" {
		t.Fatalf("Shock request lacks gorge's target decision: %+v", reqs[0].GorgeDecisions)
	}
	if reqs[2].Item.Steps[0].AbilityIndex == nil {
		t.Fatal("fixture's Cage step 0 has no ability_index")
	}
	if len(reqs[0].Abilities) != 0 {
		t.Errorf("a cast with no ability_index got ability lines: %v", reqs[0].Abilities)
	}
	if l := reqs[2].Abilities["0"]; !strings.HasPrefix(l, "AB$ PutCounter") {
		t.Errorf("Cage step 0 ability line = %q, want the script's AB$ PutCounter line", l)
	}
	if len(reqs[2].Abilities) != 1 {
		t.Errorf("Cage abilities = %v, want only step 0", reqs[2].Abilities)
	}
}

// TestForgeExportScenarioSHAMatchesWrittenItem: the scenario_sha is the SHA of
// the Item bytes the request actually carries, for every recorded template
// forgeDeclarations rewrites. A script fixture is pasted from an export and
// TestForgeScriptFixturesCarryTheirItemSHA recomputes gate.ItemSHA on the
// pasted request's own item, so a sha computed before the rewrite is stale the
// moment a declaration is exported and the paste fails.
func TestForgeExportScenarioSHAMatchesWrittenItem(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	steps := []oraclegen.Step{
		{Op: "pass_to", Step: "main1"},
		{Op: "attack", Seat: 0, Defender: "p1", Attackers: []string{}},
		{Op: "pass", Seat: 1},
		{Op: "attack", Seat: 0, Defender: "p1", Attackers: []string{"p0:Bear"}},
	}
	var scen bytes.Buffer
	for _, tmpl := range []string{lower.Template, lower.WindowTemplate, lower.SpanTemplate} {
		it := oraclegen.Item{ID: "xe:" + tmpl, Template: tmpl, Scenario: oraclegen.Scenario{Steps: steps}}
		b, err := json.Marshal(it)
		if err != nil {
			t.Fatal(err)
		}
		scen.Write(append(b, '\n'))
	}
	dir := t.TempDir()
	scenPath, out := filepath.Join(dir, "items.jsonl"), filepath.Join(dir, "req.jsonl")
	if err := os.WriteFile(scenPath, scen.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := forgeExport(reg, scenPath, out); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	reqs := decodeRequests(t, raw)
	if len(reqs) != 3 {
		t.Fatalf("exported %d requests, want 3", len(reqs))
	}
	for i, rq := range reqs {
		// The empty attack at step 1 is rewritten to a pass_to, so the
		// sha assertion is not vacuous.
		if len(rq.Item.Steps) != len(steps) || rq.Item.Steps[1].Op != "pass_to" {
			t.Errorf("request %d (%s): exported steps %+v, want step 1 rewritten", i, rq.ID, rq.Item.Steps)
		}
		if got, want := rq.ScenarioSHA, gate.ItemSHA(rq.Item); got != want {
			t.Errorf("request %d (%s): scenario_sha %s is not its written item's %s", i, rq.ID, got, want)
		}
	}
}

// TestForgeExportRefusesRepoPath: the request carries script text, so it is
// never written inside the repository (nor under oracle/compliance/verdicts).
func TestForgeExportRefusesRepoPath(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, out := range []string{
		filepath.Join("..", "..", compliance.VerdictDir, "forge-req.jsonl"),
		filepath.Join("..", "..", "forge-req.jsonl"),
	} {
		_, err := forgeExport(reg, filepath.Join(forgeFixtures, "items.jsonl"), out)
		if err == nil || !strings.Contains(err.Error(), "inside the repository") {
			t.Errorf("export to %s: err = %v, want a repository refusal", out, err)
		}
		if _, serr := os.Stat(out); serr == nil {
			os.Remove(out)
			t.Errorf("export to %s created the file", out)
		}
	}
}

// forgeRowFor builds the Forge result row for a request: snapshots are
// gorge's own (so the rows AGREE) and mutate edits them before encoding.
func forgeRowFor(t *testing.T, reg *cards.Registry, rq forgeRequest, mutate func(*oraclediff.XResult)) []byte {
	t.Helper()
	g, err := rules.RunOracleScenarioJSON(reg, rq.Item.Raw())
	if err != nil || len(g.Fails) != 0 || len(g.Snapshots) < 2 {
		t.Fatalf("gorge run of %s: err %v fails %v snapshots %d", rq.ID, err, g.Fails, len(g.Snapshots))
	}
	x := oraclediff.XResult{Name: rq.Item.Name, ID: rq.ID, Strict: true, MS: 7}
	for _, s := range g.Snapshots {
		s.Players = append([]rules.OracleSnapPlayer(nil), s.Players...)
		x.Snapshots = append(x.Snapshots, s)
	}
	if mutate != nil {
		mutate(&x)
	}
	b, err := json.Marshal(struct {
		oraclediff.XResult
		Engine   string `json:"engine"`
		ForgeRef string `json:"forge_ref"`
	}{x, "forge", "0123456789abcdef"})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func writeLines(t *testing.T, path string, lines ...[]byte) {
	t.Helper()
	if err := os.WriteFile(path, append(bytes.Join(lines, []byte("\n")), '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readRows(t *testing.T, path string) []ForgeRow {
	t.Helper()
	var rows []ForgeRow
	for _, l := range readLinesBytes(t, path) {
		var r ForgeRow
		if err := json.Unmarshal(l, &r); err != nil {
			t.Fatal(err)
		}
		rows = append(rows, r)
	}
	return rows
}

// TestForgeDiffVerdictsAndCache: AGREE, DIVERGE and HARNESS come out of the
// unchanged comparator; Forge's rows are cached by request sha, so a second
// run with no -forge file reproduces them, and a request whose gorge
// decisions changed (a different sha) misses the cache.
func TestForgeDiffVerdictsAndCache(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	_, reqPath := exportFixture(t, reg)
	reqLines := readLinesBytes(t, reqPath)
	reqs := decodeRequests(t, bytes.Join(reqLines, []byte("\n")))
	tmp := t.TempDir()

	agree := forgeRowFor(t, reg, reqs[0], nil)
	diverge := forgeRowFor(t, reg, reqs[1], func(x *oraclediff.XResult) {
		x.Snapshots[1].Players[0].Life++
	})
	// The third request gets no Forge row at all.
	forgePath := filepath.Join(tmp, "forge.jsonl")
	writeLines(t, forgePath, agree, diverge)
	cacheDir := filepath.Join(tmp, "cache", "ref-driver")
	out1 := filepath.Join(tmp, "diff1.jsonl")
	counts, err := forgeDiff(reg, reqPath, forgePath, cacheDir, out1)
	if err != nil {
		t.Fatal(err)
	}
	rows := readRows(t, out1)
	if len(rows) != 3 {
		t.Fatalf("%d rows, want 3", len(rows))
	}
	if rows[0].Verdict.Status != oraclediff.Agree || rows[0].ForgeMS != 7 {
		t.Errorf("row 0 = %+v, want AGREE with forge_ms 7", rows[0])
	}
	if v := rows[1].Verdict; v.Status != oraclediff.Diverge || v.Field != "p0.life" {
		t.Errorf("row 1 verdict = %+v, want DIVERGE on p0.life", v)
	}
	if v := rows[2].Verdict; v.Status != oraclediff.Harness || v.Engine != "forge" || v.Msg != "no Forge result" {
		t.Errorf("row 2 verdict = %+v, want a forge HARNESS 'no Forge result'", v)
	}
	if counts["AGREE"] != 1 || counts["DIVERGE:p0.life"] != 1 || counts["HARNESS"] != 1 {
		t.Errorf("counts = %v", counts)
	}
	for i, r := range rows {
		if r.RequestSHA != gate.Hash(reqLines[i]) || r.ScenarioSHA != reqs[i].ScenarioSHA {
			t.Errorf("row %d shas = %s/%s", i, r.RequestSHA, r.ScenarioSHA)
		}
	}
	if x, ok := (oraclediff.Cache{Dir: cacheDir}).Get(rows[0].RequestSHA); !ok || x.MS != 7 {
		t.Errorf("Forge row was not cached under the request sha: %+v %v", x, ok)
	}

	// A second pass with no Forge file replays from the cache alone.
	out2 := filepath.Join(tmp, "diff2.jsonl")
	if _, err := forgeDiff(reg, reqPath, "", cacheDir, out2); err != nil {
		t.Fatal(err)
	}
	b1, _ := os.ReadFile(out1)
	b2, _ := os.ReadFile(out2)
	if !bytes.Equal(b1, b2) {
		t.Errorf("cache-only pass differs:\n%s\n---\n%s", b1, b2)
	}

	// Change gorge's decisions on request 0: its sha moves, so the cached
	// Forge answers must not be reused.
	var rq0 forgeRequest
	if err := json.Unmarshal(reqLines[0], &rq0); err != nil {
		t.Fatal(err)
	}
	rq0.GorgeDecisions[0].PickIdx = []int{rq0.GorgeDecisions[0].PickIdx[0] + 1}
	changed, _ := json.Marshal(rq0)
	if gate.Hash(changed) == gate.Hash(reqLines[0]) {
		t.Fatal("test bug: the edited request has the same sha")
	}
	req2 := filepath.Join(tmp, "req2.jsonl")
	writeLines(t, req2, changed)
	out3 := filepath.Join(tmp, "diff3.jsonl")
	if _, err := forgeDiff(reg, req2, "", cacheDir, out3); err != nil {
		t.Fatal(err)
	}
	if v := readRows(t, out3)[0].Verdict; v.Msg != "no Forge result" {
		t.Errorf("a request with new decisions hit the old cache entry: %+v", v)
	}
}

// TestForgeDiffForgeHarnessRow: the canned Forge harness row (a card
// missing from Forge's database) is a HARNESS verdict attributed to forge,
// never a disagreement; and an XMage row (no engine marker) is refused.
func TestForgeDiffForgeHarnessRow(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	_, reqPath := exportFixture(t, reg)
	tmp := t.TempDir()
	out := filepath.Join(tmp, "diff.jsonl")
	if _, err := forgeDiff(reg, reqPath, filepath.Join(forgeFixtures, "harness-row.jsonl"), filepath.Join(tmp, "c"), out); err != nil {
		t.Fatal(err)
	}
	v := readRows(t, out)[0].Verdict
	if v.Status != oraclediff.Harness || v.Engine != "forge" || !strings.Contains(v.Msg, "Couldn't find a card") {
		t.Errorf("verdict = %+v, want forge HARNESS carrying Forge's message", v)
	}
	_, err := forgeDiff(reg, reqPath, filepath.Join(forgeFixtures, "xmage-row.jsonl"), filepath.Join(tmp, "c2"), filepath.Join(tmp, "diff2.jsonl"))
	if err == nil || !strings.Contains(err.Error(), "not a Forge row") {
		t.Errorf("an XMage row was accepted as Forge's: err = %v", err)
	}
}

func TestRefuseRepoPathAllowsTheIgnoredBuildDir(t *testing.T) {
	root, _ := filepath.Abs("../..")
	if err := refuseRepoPath(filepath.Join(root, ".oracle-build", "forge", "runs", "r")); err != nil {
		t.Errorf("under .oracle-build: %v", err)
	}
	if err := refuseRepoPath(filepath.Join(root, ".oracle-buildx", "r")); err == nil {
		t.Error("a sibling of .oracle-build was accepted")
	}
}

// TestForgeDiffNeverWritesRepoOrVerdicts: -out inside the repository (above
// all oracle/compliance/verdicts) is refused before anything is created.
func TestForgeDiffNeverWritesRepoOrVerdicts(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	_, reqPath := exportFixture(t, reg)
	out := filepath.Join("..", "..", compliance.VerdictDir, "forge-diff.jsonl")
	_, err := forgeDiff(reg, reqPath, "", t.TempDir(), out)
	if err == nil || !strings.Contains(err.Error(), "inside the repository") {
		t.Fatalf("err = %v, want a repository refusal", err)
	}
	if _, serr := os.Stat(out); serr == nil {
		os.Remove(out)
		t.Fatal("forge-diff created a file under oracle/compliance/verdicts")
	}
}

// TestForgeDiffCacheDir: the cache directory is named after both pins.
func TestForgeDiffCacheDir(t *testing.T) {
	t.Setenv("FORGE_ORACLE_DIR", "/off/repo")
	got, err := forgeCacheDir("", "0123456789abcdef0123", "fedcba9876543210ffff")
	if err != nil || got != "/off/repo/cache/0123456789ab-fedcba987654" {
		t.Errorf("cache dir = %q, %v", got, err)
	}
	if _, err := forgeCacheDir("", "ref", ""); err == nil {
		t.Error("a missing driver sha was accepted")
	}
	if got, _ := forgeCacheDir("/x", "", ""); got != "/x" {
		t.Errorf("explicit -cache = %q", got)
	}
}

// staticRequestJSON returns the request object beginning at the first
// `{"id":` on line, or "" when the line has none. A shell script spells a
// request either as a heredoc line or inside a printf argument, so cut at the
// balanced closing brace rather than at the end of the line.
func staticRequestJSON(line string) string {
	at := strings.Index(line, `{"id":`)
	if at < 0 {
		return ""
	}
	depth := 0
	inString, escape := false, false
	for i := at; i < len(line); i++ {
		c := line[i]
		if inString {
			switch {
			case escape:
				escape = false
			case c == '\\':
				escape = true
			case c == '"':
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return line[at : i+1]
			}
		}
	}
	return ""
}

// TestForgeScriptFixturesCarryTheirItemSHA: every hand-authored request in
// scripts/forge-oracle-test-*.sh carries a scenario_sha, and it is its own
// item's. A copied request can otherwise keep the scenario it was copied from (the Done fixture
// did), so two different scenarios share an identity and a consumer keyed by
// it conflates them. The check is over all such scripts, so a new fixture or a
// later answers edit is covered without listing the files.
func TestForgeScriptFixturesCarryTheirItemSHA(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "scripts", "forge-oracle-test-*.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no Forge driver scripts")
	}
	checked := 0
	for _, path := range files {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			// A shell-quoted apostrophe ('"'"') is one apostrophe once the
			// shell has run; read the request as the driver will.
			line = strings.ReplaceAll(line, `'"'"'`, `'`)
			if !strings.Contains(line, `{"id":`) && !strings.Contains(line, `"scenario_sha"`) {
				continue
			}
			raw := staticRequestJSON(line)
			if raw == "" {
				t.Errorf("%s:%d: the line holds no one-line {\"id\": request to check", path, i+1)
				continue
			}
			var rq forgeRequest
			if err := json.Unmarshal([]byte(raw), &rq); err != nil {
				t.Fatalf("%s:%d: static request is not JSON: %v", path, i+1, err)
			}
			if rq.Item.ID == "" {
				t.Errorf("%s:%d: request %q has no item", path, i+1, rq.ID)
				continue
			}
			if rq.ScenarioSHA == "" {
				t.Errorf("%s:%d: request %q carries no scenario_sha (want %s)", path, i+1, rq.ID, gate.ItemSHA(rq.Item))
				continue
			}
			checked++
			// The live-export variants in the odds-or-evens fixture use a
			// request id with a suffix; the item keeps gorge's id.
			if got, want := rq.ScenarioSHA, gate.ItemSHA(rq.Item); got != want {
				t.Errorf("%s:%d: scenario_sha %s is not item %q's %s", path, i+1, got, rq.ID, want)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no static Forge request fixture with a scenario_sha was checked")
	}
}

// TestFollowerDiffIsForgeDiff: forge-diff is follower-diff -engine forge, byte
// for byte, and follower-diff refuses a follower whose items are not Forge
// requests.
func TestFollowerDiffIsForgeDiff(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	cardsDir, err := filepath.Abs(filepath.Join("..", "..", ".cards"))
	if err != nil {
		t.Fatal(err)
	}
	_, reqPath := exportFixture(t, reg)
	reqs := decodeRequests(t, bytes.Join(readLinesBytes(t, reqPath), []byte("\n")))
	tmp := t.TempDir()
	forgePath := filepath.Join(tmp, "forge.jsonl")
	writeLines(t, forgePath, forgeRowFor(t, reg, reqs[0], nil), forgeRowFor(t, reg, reqs[1], func(x *oraclediff.XResult) {
		x.Snapshots[1].Players[0].Life++
	}))
	run := func(f func([]string) error, name string, extra ...string) []byte {
		out := filepath.Join(tmp, name+".jsonl")
		args := append(extra, "-cards", cardsDir, "-req", reqPath, "-forge", forgePath, "-cache", filepath.Join(tmp, name+"-cache"), "-out", out)
		if err := f(args); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		b, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	old := run(runForgeDiff, "forge-diff")
	gen := run(func(a []string) error { return runFollowerDiff("follower-diff", "", a) }, "follower-diff", "-engine", "forge")
	if !bytes.Equal(old, gen) {
		t.Errorf("follower-diff -engine forge differs from forge-diff:\n%s\n---\n%s", gen, old)
	}
	for _, e := range []string{"xmage", "nosuch"} {
		err := runFollowerDiff("follower-diff", "", []string{"-engine", e, "-req", reqPath, "-out", filepath.Join(tmp, "x.jsonl")})
		if err == nil || !strings.Contains(err.Error(), "reads Forge requests") {
			t.Errorf("-engine %s: err = %v, want a refusal", e, err)
		}
	}
}
