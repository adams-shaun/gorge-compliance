package main

// Tests for the oracle-text audit (SC11): the normalisation that lets a
// Forge script's Oracle line compare with the exported current text, and the
// stale-scenario report.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/cards/oracletext"
)

const testPushScript = "Name:Test Push\nManaCost:B\nTypes:Instant\n" +
	"A:SP$ Destroy | ValidTgts$ Creature | SpellDescription$ Destroy it.\n" +
	`Oracle:Destroy target creature. (It can't be regenerated.)\n“CARDNAME” deals ~* no more than 2 damage — and deals 2 damage to its controller.` + "\n"

// cardFromScript compiles one Forge script, the way the corpus reader does.
func cardFromScript(t *testing.T, src string) *cards.Card {
	t.Helper()
	c, diags := cards.ParseBytes("t.txt", []byte(src))
	if len(diags) != 0 {
		t.Fatalf("script diagnostics: %v", diags)
	}
	return c
}

// TestOracleAuditNormalise pins the comparison: reminder text, the
// placeholders (CARDNAME and ~), quote variants and whitespace folding all
// compare equal; any changed word (a number here) compares as diff. It runs
// the real path: script side compiled by gorge's parser, export side as the
// TSV carries it, both through the same normaliseOracleText.
func TestOracleAuditNormalise(t *testing.T) {
	c := cardFromScript(t, testPushScript)
	script, _ := scriptDigest(c)
	if c.Faces[0].Oracle == "" {
		t.Fatal("precondition: the parser dropped the Oracle line")
	}

	current := func(text string) string {
		d, _ := exportDigest(oracleExportRow{sha: "ignored", text: text}, c.Faces[0].Name)
		return d
	}

	// Equal: the same content with the placeholder spelled out, curly quotes,
	// a different reminder text and real newlines instead of `\n` escapes.
	equals := []string{
		`Destroy target creature. “Test Push” deals Test Push* no more than 2 damage — and deals 2 damage to its controller.`,
		"Destroy target creature. (Regeneration can't save it.)\n“Test Push” deals Test Push* no more than 2 damage — and deals 2 damage to its controller.",
		`  Destroy   target	creature. (It can't be regenerated.) “Test Push” deals Test Push* no more than 2 damage — and deals 2 damage to its controller.  `,
	}
	for i, text := range equals {
		if got := current(text); got != script {
			t.Errorf("variant %d normalises to %s, want the script digest %s", i, got, script)
		}
	}

	// Diff: one number changed (an erratum), and any other word change.
	diffs := []string{
		`Destroy target creature. “Test Push” deals Test Push* no more than 3 damage — and deals 2 damage to its controller.`,
		`Destroy target permanent. “Test Push” deals Test Push* no more than 2 damage — and deals 2 damage to its controller.`,
		"Destroy target creature. Test Push deals Test Push* no more than 2 damage.",
	}
	for i, text := range diffs {
		if got := current(text); got == script {
			t.Errorf("variant %d compares equal, want diff", i)
		}
	}
}

// TestOracleAuditNormaliseIdempotent: the export may already be normalised
// (SC01 folds it too), so normalising twice must not change the digest.
func TestOracleAuditNormaliseIdempotent(t *testing.T) {
	once := normaliseOracleText(`(Reminder here.) “Name”\nspells ~ out`, "Name")
	if twice := normaliseOracleText(once, "Name"); twice != once {
		t.Errorf("not idempotent: %q -> %q", once, twice)
	}
	if strings.Contains(once, "(") || strings.Contains(once, "\n") {
		t.Errorf("normalisation left reminder or newline: %q", once)
	}
}

// TestOracleAuditStaleScenario: a scenario whose oracle_sha records text the
// corpus no longer carries is reported stale; one recorded against the
// current text is not. End-to-end through staleOracleScenarios against a real
// (tiny) corpus registry.
func TestOracleAuditStaleScenario(t *testing.T) {
	dir := t.TempDir()
	card := filepath.Join(dir, "cardsfolder", "t")
	if err := os.MkdirAll(card, 0o755); err != nil {
		t.Fatal(err)
	}
	src := "Name:Test Push\nManaCost:B\nTypes:Instant\n" +
		`Oracle:Destroy target creature.` + "\n"
	if err := os.WriteFile(filepath.Join(card, "test_push.txt"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	reg, err := cards.OpenCorpus(dir)
	if err != nil {
		t.Fatal(err)
	}
	c, ok := reg.Lookup("Test Push")
	if !ok {
		t.Fatal("test card missing from the tiny corpus")
	}
	current := oracletext.Digest(c)

	scn := filepath.Join(dir, "scenarios")
	fresh := filepath.Join(scn, "keywords", "test-push.json")
	if err := os.MkdirAll(filepath.Dir(fresh), 0o755); err != nil {
		t.Fatal(err)
	}
	writeScenario := func(path, sha string) {
		t.Helper()
		body := `{"card":"Test Push","oracle_sha":"` + sha + `","scenarios":[{"name":"s","cr":["701.3a"],"why":"w","expect":[{}]}]}`
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeScenario(fresh, current)
	staleFile := filepath.Join(scn, "keywords", "old-test-push.json")
	writeScenario(staleFile, "000000000000")
	noSHA := filepath.Join(scn, "keywords", "noshatest-push.json")
	writeScenario(noSHA, "")

	got, checked, err := staleOracleScenarios(reg, scn, nil)
	if err != nil {
		t.Fatal(err)
	}
	if checked != 3 {
		t.Errorf("checked %d scenario files, want 3", checked)
	}
	if len(got) != 1 || got[0].file != staleFile || got[0].recorded != "000000000000" || got[0].current != current || got[0].card != "Test Push" || got[0].reason != "script" {
		t.Errorf("stale report %v, want only %s", got, staleFile)
	}

	// The corpus sha still matches, but the current Oracle text (the export)
	// differs from the script: every scenario with a recorded sha is stale,
	// the one without a sha is not.
	verdicts := map[string]oracleAuditRow{"Test Push": {card: "Test Push", verdict: "diff", scriptSHA: "aaaaaaaaaaaa", oracleSHA: "bbbbbbbbbbbb"}}
	got, _, err = staleOracleScenarios(reg, scn, verdicts)
	if err != nil {
		t.Fatal(err)
	}
	reasons := map[string]string{}
	for _, s := range got {
		reasons[s.file] = s.reason
	}
	if len(got) != 2 || reasons[fresh] != "export" || reasons[staleFile] != "script" {
		t.Errorf("with a diff verdict got %v, want fresh=export stale=script", got)
	}
	verdicts["Test Push"] = oracleAuditRow{verdict: "ok"}
	if got, _, _ = staleOracleScenarios(reg, scn, verdicts); len(got) != 1 {
		t.Errorf("ok verdict: got %v, want only the script-stale file", got)
	}
}

// TestOracleAuditExportCollision: the export holds both "X" (the real text)
// and "X // X" (empty), as it does for ~2.4k real cards. In either row order
// the card compares ok or diff, never empty, and the verdict is identical.
func TestOracleAuditExportCollision(t *testing.T) {
	dir := t.TempDir()
	card := filepath.Join(dir, "cardsfolder", "t")
	if err := os.MkdirAll(card, 0o755); err != nil {
		t.Fatal(err)
	}
	src := "Name:Test Push\nManaCost:B\nTypes:Instant\nOracle:Destroy target creature.\n"
	if err := os.WriteFile(filepath.Join(card, "test_push.txt"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	real := "Test Push\tdeadbeef\tDestroy target creature.\n"
	empty := "Test Push // Test Push\tdeadbeef\t\\n\n"
	for _, tc := range []struct{ name, rows, want string }{
		{"real-first", real + empty, "ok"},
		{"empty-first", empty + real, "ok"},
		{"changed-real-first", "Test Push\tx\tDestroy target permanent.\n" + empty, "diff"},
		{"changed-empty-first", empty + "Test Push\tx\tDestroy target permanent.\n", "diff"},
	} {
		exp := filepath.Join(dir, tc.name+".tsv")
		out := filepath.Join(dir, tc.name+".out.tsv")
		if err := os.WriteFile(exp, []byte(tc.rows), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := runOracleAudit(dir, exp, out, "", ""); err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(out)
		f := strings.Split(strings.TrimSpace(string(b)), "\t")
		if len(f) != 4 || f[0] != "Test Push" || f[1] != tc.want {
			t.Errorf("%s: row %q, want verdict %s", tc.name, string(b), tc.want)
		}
	}
}

// TestOracleAuditMultiFacedLookup: a multi-faced card resolves to its own
// joined "A // B" row even when an unrelated bare "A" row (a theme card or a
// token) exists, and a meld-style "X // X" empty row still never hides the
// real bare "X" row.
func TestOracleAuditMultiFacedLookup(t *testing.T) {
	dir := t.TempDir()
	card := filepath.Join(dir, "cardsfolder", "t")
	if err := os.MkdirAll(card, 0o755); err != nil {
		t.Fatal(err)
	}
	split := "Name:Zed\nManaCost:R\nTypes:Sorcery\nAlternateMode:Split\nOracle:Draw a card.\n\nALTERNATE\n\n" +
		"Name:Quux\nManaCost:G\nTypes:Sorcery\nOracle:Gain 2 life.\n"
	meld := "Name:Mox Meld\nManaCost:B\nTypes:Instant\nOracle:Destroy target creature.\n"
	for name, src := range map[string]string{"zed_quux.txt": split, "mox_meld.txt": meld} {
		if err := os.WriteFile(filepath.Join(card, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	themeRow := "Zed\tx\t(Theme color: {B})\n"
	splitRow := "Zed // Quux\tx\tDraw a card.\\nGain 2 life.\n"
	meldReal := "Mox Meld\tx\tDestroy target creature.\n"
	meldEmpty := "Mox Meld // Mox Meld\tx\t\\n\n"
	for _, tc := range []struct{ name, rows, card, want string }{
		{"split-theme-first", themeRow + splitRow, "Zed", "ok"},
		{"split-theme-last", splitRow + themeRow, "Zed", "ok"},
		{"split-changed", themeRow + "Zed // Quux\tx\tDraw 2 cards.\\nGain 2 life.\n", "Zed", "diff"},
		{"meld-real-first", meldReal + meldEmpty, "Mox Meld", "ok"},
		{"meld-empty-first", meldEmpty + meldReal, "Mox Meld", "ok"},
		{"split-only-empty", "Zed // Quux\tx\t\\n\n", "Zed", "empty"},
	} {
		exp := filepath.Join(dir, tc.name+".tsv")
		out := filepath.Join(dir, tc.name+".out.tsv")
		if err := os.WriteFile(exp, []byte(tc.rows), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := runOracleAudit(dir, exp, out, "", ""); err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(out)
		got := ""
		for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			if f := strings.Split(l, "\t"); len(f) == 4 && f[0] == tc.card {
				got = f[1]
			}
		}
		if got != tc.want {
			t.Errorf("%s: %s verdict %q, want %q (out %q)", tc.name, tc.card, got, tc.want, string(b))
		}
	}
}

// TestOracleAuditFoldSelfRefsEmptyName: an empty or blank face name must not
// panic the fold.
func TestOracleAuditFoldSelfRefsEmptyName(t *testing.T) {
	if got := foldSelfRefs("Draw  a card.", []string{"", "  "}); got != "Draw a card." {
		t.Errorf("got %q", got)
	}
}

// TestOracleAuditFoldSelfRefs: Scryfall's "this creature" / short-name
// rewording folds to the same text as the full name.
func TestOracleAuditFoldSelfRefs(t *testing.T) {
	names := []string{"Zurgo Bellstriker"}
	a := foldSelfRefs("Zurgo Bellstriker can't block. Zurgo attacks.", names)
	b := foldSelfRefs("This creature can't block. This creature attacks.", names)
	if a != b {
		t.Errorf("fold %q != %q", a, b)
	}
	if foldSelfRefs("Draw 2 cards.", names) == foldSelfRefs("Draw 3 cards.", names) {
		t.Error("fold hid a number change")
	}
}
