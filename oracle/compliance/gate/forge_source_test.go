package gate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen/templates"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// forgeFixtureRoot builds a throwaway repo root over the real XMage
// manifests: TRK's printed list and the verdict rows the test writes.
func forgeFixtureRoot(t *testing.T, reg *cards.Registry) string {
	t.Helper()
	root := t.TempDir()
	for _, d := range []string{
		filepath.Join(root, "oracle", "compliance"),
		filepath.Join(root, "oracle", "compliance", "printed"),
		filepath.Join(root, "scenarios", "oracle"),
	} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	manifests, err := filepath.Abs(filepath.Join("..", "..", "..", "oracle", "compliance", "manifests"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(manifests, filepath.Join(root, "oracle", "compliance", "manifests")); err != nil {
		t.Fatal(err)
	}
	printed := compliance.Printed{Code: "TRK", Source: "test", Cards: []string{
		"Steam Vents", "Eject the Warp Core", "Consider the Prime Directive",
		"Giant Growth", "Lightning Bolt", "Forest",
	}}
	b, err := json.Marshal(printed)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "oracle", "compliance", "printed", "TRK.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	// loadShared installs the XMage name table process-wide, so a test that
	// points it at a fixture root restores the real one when it ends.
	t.Cleanup(func() {
		if _, err := loadShared(reg, filepath.Join("..", "..", "..")); err != nil {
			t.Errorf("restoring the XMage name table: %v", err)
		}
	})
	return root
}

// forgeRow builds the row a level-A verdict would carry for card.
func forgeRow(t *testing.T, reg *cards.Registry, card, source string) compliance.VerdictRow {
	t.Helper()
	it, skip := templates.Generate(reg, card)
	if skip != nil {
		t.Fatalf("%s: %s", card, skip.Reason)
	}
	fz, err := Freeze(reg, it)
	if err != nil {
		t.Fatal(err)
	}
	return compliance.VerdictRow{
		Card: card, Template: it.Template, ID: it.ID, ScenarioSHA: ItemSHA(it),
		XMageRef: "test-ref", Source: source, Status: compliance.StatusAgree, Frozen: fz,
	}
}

// TestGateCountsForgeVerdictsAtLevelA: a Forge-only verdict passes level A
// exactly where an XMage agree row would, including a card XMage does not
// implement; the passing split counts it as Forge and never as XMage.
func TestGateCountsForgeVerdictsAtLevelA(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	root := forgeFixtureRoot(t, reg)
	rows := []compliance.VerdictRow{
		forgeRow(t, reg, "Steam Vents", ""),
		forgeRow(t, reg, "Eject the Warp Core", compliance.SourceForge),
		forgeRow(t, reg, "Giant Growth", compliance.SourceForge),
	}
	if err := compliance.ReplaceVerdicts(filepath.Join(root, compliance.VerdictDir), rows); err != nil {
		t.Fatal(err)
	}
	probs, pass, err := CheckPassing(reg, root, "TRK")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"Consider the Prime Directive": "no verdict for ",
		"Lightning Bolt":               "XMage does not implement it",
	}
	if len(probs) != len(want) {
		t.Fatalf("problems %v, want %d", probs, len(want))
	}
	for _, p := range probs {
		prefix, ok := want[p.Card]
		if !ok {
			t.Errorf("unexpected problem for %s: %s", p.Card, p.Reason)
			continue
		}
		if !strings.HasPrefix(p.Reason, prefix) {
			t.Errorf("%s: reason %q, want prefix %q", p.Card, p.Reason, prefix)
		}
	}
	if pass != (Passing{XMage: 1, Forge: 2}) {
		t.Errorf("passing split %+v, want XMage:1 Forge:2 Hand:0", pass)
	}
	check, err := Check(reg, root, "TRK", "A")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(check, probs) {
		t.Errorf("Check problems %v, CheckPassing %v", check, probs)
	}
}

// TestRowOKForgeRowCountsOnlyAsLevelAAgree: a Forge row counts at level A
// only on an agree row for a level-A template; anything else is a problem.
func TestRowOKForgeRowCountsOnlyAsLevelAAgree(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	it, skip := templates.Generate(reg, "Shock")
	if skip != nil {
		t.Fatal(skip.Reason)
	}
	fz, err := Freeze(reg, it)
	if err != nil {
		t.Fatal(err)
	}
	row := compliance.VerdictRow{
		Card: it.Card, Template: it.Template, ID: it.ID, ScenarioSHA: ItemSHA(it),
		Status: compliance.StatusAgree, Frozen: fz, Source: compliance.SourceForge,
	}
	var got string
	bad := func(_ string, format string, a ...any) { got = fmt.Sprintf(format, a...) }

	wrong := row
	wrong.Status = compliance.StatusXMageWrong
	got = ""
	if rowOK(reg, it, map[string]compliance.VerdictRow{it.Template: wrong}, bad) {
		t.Fatal("a Forge xmage_wrong row passed at level A")
	}
	if want := "forge verdict xmage_wrong (cast-resolve) counts only as agree"; got != want {
		t.Errorf("xmage_wrong reason %q, want %q", got, want)
	}

	levelB := it
	levelB.Template = "activate#0"
	levelBRow := row
	levelBRow.Template = levelB.Template
	got = ""
	if rowOK(reg, levelB, map[string]compliance.VerdictRow{levelB.Template: levelBRow}, bad) {
		t.Fatal("a Forge row on a level-B template passed at level A")
	}
	if want := "forge verdict (activate#0) counts at level A only"; got != want {
		t.Errorf("level-B reason %q, want %q", got, want)
	}

	got = ""
	if !rowOK(reg, it, map[string]compliance.VerdictRow{it.Template: row}, bad) {
		t.Fatalf("a Forge agree row on the level-A template failed: %s", got)
	}
}
