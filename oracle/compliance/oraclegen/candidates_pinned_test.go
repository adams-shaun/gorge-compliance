package oraclegen

import (
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestPinnedSubtypeFixturesResolve: every pin is a real fixture its picker
// accepts under the committed manifests -- a pin the registry or XMageKnown
// rejected would silently fall back to the corpus-order scan the pins exist
// to retire.
func TestPinnedSubtypeFixturesResolve(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	names, err := compliance.LoadXMageNames("../manifests")
	if err != nil {
		t.Fatal(err)
	}
	SetXMageKnown(names)
	t.Cleanup(func() { SetXMageKnown(nil) })
	for sub, want := range pinnedSubtypeFixtures {
		if got, ok := registrySubtype(reg, sub); !ok || got != want {
			t.Errorf("registrySubtype(%q) = %q,%v, want the pin %q", sub, got, ok, want)
		}
	}
	for sub, want := range pinnedQuietSubtypeFixtures {
		if got, ok := registryQuietSubtype(reg, sub); !ok || got != want {
			t.Errorf("registryQuietSubtype(%q) = %q,%v, want the pin %q", sub, got, ok, want)
		}
	}
}

// TestSubtypeFixtureIgnoresInstalledManifests is the determinism ratchet for
// the pinned subtypes: the pick is the same with the committed manifests
// installed and with none (a unit test's state), where the corpus-order scan
// returned another card -- Rydia, Summoner of Mist's Saga fixture was Age of
// Ultron in the pass and A Golden Opportunity in a test registry.
func TestSubtypeFixtureIgnoresInstalledManifests(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	names, err := compliance.LoadXMageNames("../manifests")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { SetXMageKnown(nil) })
	pick := func(installed bool) (string, string) {
		if installed {
			SetXMageKnown(names)
		} else {
			SetXMageKnown(nil)
		}
		saga, _ := registrySubtype(reg, "Saga")
		elf, _ := registryQuietSubtype(reg, "Elf")
		return saga, elf
	}
	sagaIn, elfIn := pick(true)
	sagaOut, elfOut := pick(false)
	if sagaIn != "Age of Ultron" || sagaIn != sagaOut {
		t.Fatalf("Saga fixture %q (manifests) vs %q (none), want Age of Ultron both", sagaIn, sagaOut)
	}
	if elfIn != elfOut {
		t.Fatalf("quiet Elf fixture %q (manifests) vs %q (none), want one card", elfIn, elfOut)
	}
}
