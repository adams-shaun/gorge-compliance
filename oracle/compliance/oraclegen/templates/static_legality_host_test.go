package templates

import (
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
)

// TestHostLegalityRowsServed pins the FDN rows the host-legality, life,
// mana-conversion, look-at and named-card templates serve (each verified
// AGREE against XMage when it landed).
func TestHostLegalityRowsServed(t *testing.T) {
	for _, c := range []struct{ name, key string }{
		{"Pacifism", "static#0.0"},
		{"Pacifism", "static#0.1"},
		{"Gate Colossus", "static#0.0"},
		{"Stromkirk Noble", "static#0.0"},
		{"Giant Cindermaw", "static#0.0"},
		{"Vizier of the Menagerie", "static#0.0"},
		{"Vizier of the Menagerie", "static#0.2"},
		{"Sorcerous Spyglass", "static#0.0"},
		{"Ball Lightning", "combat#0.block"},
	} {
		it := generateBKey(t, c.name, c.key)
		if it.ID != c.name+"/"+c.key+"/v1" {
			t.Errorf("%s %s: generated %q", c.name, c.key, it.ID)
		}
	}
}

// The Spyglass item scripts XMage's card-name answer -- the probe the
// scenario's resolve step names -- exactly once.
func TestNamedCardItemScriptsTheName(t *testing.T) {
	it := generateBKey(t, "Sorcerous Spyglass", "static#0.0")
	found := 0
	for _, step := range it.XAnswers {
		for _, a := range step {
			if a == (oraclegen.XAnswer{Seat: 0, Kind: "choice", Value: activatedProbe}) {
				found++
			}
		}
	}
	if found != 1 {
		t.Fatalf("%d %q name answers in %+v, want exactly one", found, activatedProbe, it.XAnswers)
	}
}
