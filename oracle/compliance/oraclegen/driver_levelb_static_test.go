package oraclegen_test

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// snapshotBody returns the text of ScenarioReplay.snapshot's implementation,
// so the assertions below cannot be satisfied by a keywords mention elsewhere
// in the file. The driver has more than one snapshot overload (a two-argument
// wrapper delegates to the one that takes the priority holder), so select the
// body that builds the permanents array rather than the first signature.
func snapshotBody(t *testing.T, java string) string {
	t.Helper()
	const sig = "private JsonObject snapshot(String checkpoint, Game g"
	for i := 0; ; {
		j := strings.Index(java[i:], sig)
		if j < 0 {
			break
		}
		j += i
		rest := java[j:]
		k := strings.Index(rest, "\n    }\n")
		if k < 0 {
			break
		}
		if body := rest[:k]; strings.Contains(body, `s.add("permanents", perms)`) {
			return body
		}
		i = j + len(sig)
	}
	t.Fatal("ScenarioReplay.java has no snapshot method that builds permanents")
	return ""
}

// TestDriverSnapshotEmitsPermanentKeywords pins L12: the XMage snapshot adds a
// sorted, de-duplicated `keywords` array to every permanent, built from the
// permanent's ability names, so the opt-in field is not
// empty on the XMage side.
func TestDriverSnapshotEmitsPermanentKeywords(t *testing.T) {
	b, err := os.ReadFile(scenarioReplayPath)
	if err != nil {
		t.Fatal(err)
	}
	java := string(b)
	snap := snapshotBody(t, java)

	// The property is added inside the per-permanent loop, not on the
	// snapshot or a player.
	loop := strings.Index(snap, "for (Permanent perm : g.getBattlefield().getAllActivePermanents())")
	kw := strings.Index(snap, `o.add("keywords"`)
	end := strings.Index(snap, `s.add("permanents", perms)`)
	if loop < 0 || end < 0 || kw < 0 || kw < loop || kw > end {
		t.Fatalf("snapshot does not add a keywords property per permanent (loop %d, keywords %d, end %d)", loop, kw, end)
	}

	helper := regexp.MustCompile(`(?s)List<String> keywordNames\(Permanent perm, Game g\) \{.*?\n    \}\n`).FindString(java)
	if helper == "" {
		t.Fatal("ScenarioReplay.java has no keywordNames helper")
	}
	for what, needle := range map[string]string{
		"reads the game-state abilities (so granted keywords show)": "perm.getAbilities(g)",
		"takes the ability's rule name":                             "a.getRule()",
		"restricts to keyword ability classes":                      `"mage.abilities.keyword."`,
		"sorts and de-duplicates":                                   "java.util.TreeSet<String>",
		"strips HTML tags (menace's <i>(reminder)</i> hint)":        `replaceAll("<[^>]*>", "")`,
		"cuts reminder text at the first paren":                     `rule.indexOf("(")`,
	} {
		if !strings.Contains(helper, needle) {
			t.Errorf("keywordNames does not do this: %s (want %q)", what, needle)
		}
	}
	if !strings.Contains(snap, "keywordNames(perm, g)") {
		t.Error("snapshot does not call keywordNames for each permanent")
	}
}
