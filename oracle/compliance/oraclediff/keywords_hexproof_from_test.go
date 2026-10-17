package oraclediff

import (
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
)

// TestHexproofFromColourFold: gorge's "Hexproof:CardColors" (Tam, Mindful
// First-Year's "hexproof from each of its colors") expands against the
// permanent's colours onto XMage's per-colour rule text, a direct colour
// line folds to the same word, and a non-colour quality stays outside.
func TestHexproofFromColourFold(t *testing.T) {
	for _, row := range []struct {
		kws    []string
		colors string
		want   string
	}{
		{[]string{"Hexproof:CardColors"}, "G", "hexproof from green"},
		{[]string{"Hexproof:CardColors"}, "UR", "hexproof from blue,hexproof from red"},
		{[]string{"Hexproof:CardColors"}, "", ""},
		{[]string{"hexproof from green"}, "G", "hexproof from green"},
		{[]string{"hexproof from blue", "hexproof from red", "flying"}, "UR", "flying,hexproof from blue,hexproof from red"},
		{[]string{"Hexproof:Black"}, "G", "hexproof from black"},
		{[]string{"Hexproof:Card.White:white"}, "G", "hexproof from white"},
		{[]string{"Hexproof:Card.MonoColor:monocolored", "hexproof from monocolored"}, "G", ""},
		{[]string{"Hexproof", "Ward:1"}, "G", "hexproof,ward"},
	} {
		if got := ComparedKeywordsHexproofFrom(row.kws, row.colors); got != row.want {
			t.Errorf("ComparedKeywordsHexproofFrom(%q, %q) = %q, want %q", row.kws, row.colors, got, row.want)
		}
	}
}

// TestHexproofFromOptInComparesTam: a gorge permanent carrying Tam's grant
// and an XMage one carrying the per-colour keyword agree under the opt-in,
// disagree with XMage's grant missing, and the default keyword opt-ins never
// see either.
func TestHexproofFromOptInComparesTam(t *testing.T) {
	gorge := []rules.OracleSnapPerm{kwPerm("Grizzly Bears", "Hexproof:CardColors")}
	xmage := []rules.OracleSnapPerm{kwPerm("Grizzly Bears", "hexproof from green")}
	none := []rules.OracleSnapPerm{kwPerm("Grizzly Bears")}
	on := []string{CompareKeywordsHexproofFrom}
	if a, b := permKeysOpts(gorge, on), permKeysOpts(xmage, on); a[0] != b[0] {
		t.Fatalf("opt-in: gorge %q != xmage %q", a[0], b[0])
	}
	if a, b := permKeysOpts(gorge, on), permKeysOpts(none, on); a[0] == b[0] {
		t.Fatalf("opt-in does not see the grant: %q", a[0])
	}
	for _, c := range [][]string{{CompareKeywords}, {CompareKeywordsNamed}} {
		if a, b := permKeysOpts(gorge, c), permKeysOpts(none, c); a[0] != b[0] {
			t.Fatalf("%v sees hexproof-from: %q vs %q", c, a[0], b[0])
		}
	}
	if err := ValidateCompare(on); err != nil {
		t.Fatal(err)
	}
}
