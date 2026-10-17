package gate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

func TestDeclaredFileIsWellFormed(t *testing.T) {
	d, err := compliance.LoadDeclared(filepath.Join("..", "declared.json"))
	if err != nil {
		t.Fatal(err)
	}
	for set := range d {
		if _, err := os.Stat(filepath.Join("..", "manifests", compliance.ManifestFileName(set))); err != nil {
			t.Errorf("declared set %s has no manifest", set)
		}
		if strings.ToUpper(set) != set {
			t.Errorf("declared set %q: codes are upper case", set)
		}
	}
}

// TestGateRefusesSetWithoutPrintedList is section 11.3 C7: a set with no
// oracle/compliance/printed list cannot be declared, because the XMage manifest
// the gate would fall back to omits the cards XMage lacks. Every other
// problem is still reported, so status stays useful for such a set.
func TestGateRefusesSetWithoutPrintedList(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	root := filepath.Join("..", "..", "..")
	if _, err := compliance.LoadPrinted(filepath.Join(root, "oracle", "compliance", "printed"), "G00"); err == nil {
		t.Fatal("G00 has a printed list; pick a set without one")
	}
	probs, err := Check(reg, root, "G00", "A")
	if err != nil {
		t.Fatal(err)
	}
	if len(probs) == 0 || probs[0].Card != "*" || !strings.Contains(probs[0].Reason, "no printed list") {
		t.Fatalf("G00 without a printed list: problems %v, want a leading \"*\" no-printed-list refusal", probs)
	}
	if fra, err := Check(reg, root, "FRA", "A"); err != nil {
		t.Fatal(err)
	} else {
		for _, p := range fra {
			if strings.Contains(p.Reason, "no printed list") {
				t.Errorf("FRA has a printed list but was refused: %v", p)
			}
		}
	}
}

func TestLevelMeaningSaysWhatLevelADoesNotCover(t *testing.T) {
	m := LevelMeaning("A")
	for _, want := range []string{"cast-and-resolve", "NOT exercised", "level B"} {
		if !strings.Contains(m, want) {
			t.Errorf("LevelMeaning(A) = %q, missing %q", m, want)
		}
	}
}
