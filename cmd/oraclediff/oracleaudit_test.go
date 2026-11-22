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

	got, err := staleOracleScenarios(reg, scn)
	if err != nil {
		t.Fatal(err)
	}
	if scenarioFilesChecked != 3 {
		t.Errorf("checked %d scenario files, want 3", scenarioFilesChecked)
	}
	if len(got) != 1 || got[0].file != staleFile || got[0].recorded != "000000000000" || got[0].current != current || got[0].card != "Test Push" {
		t.Errorf("stale report %v, want only %s", got, staleFile)
	}
}
