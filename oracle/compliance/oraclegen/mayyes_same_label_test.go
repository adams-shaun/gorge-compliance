package oraclegen

import (
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
)

// TestMayYesSkipsPastOptionZeroWithSharedLabel: mayYes pins option 0 of a
// fallback-answered choose, unless the runner deliberately answered past it.
// A CR 704.5j keep among same-named legends (Grub, Notorious Auntie and his
// token copy) shares option 0's label, so the label cannot tell the runner's
// survivor pick (option 1) from option 0: the index decides. Pinning the
// label re-ran the scenario keeping option 0, and both Grubs died.
func TestMayYesSkipsPastOptionZeroWithSharedLabel(t *testing.T) {
	sc := Scenario{Steps: []Step{{Op: "resolve"}}}
	keep := rules.OracleDecision{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Via: "resolve fallback",
		Options: 2, Min: 1, Max: 1, First: "Grub, Notorious Auntie", Picks: []string{"Grub, Notorious Auntie"},
		PickIdx: []int{1}, PickKinds: []string{"keep"},
		OptionRefs: []string{"p0:Grub, Storied Matriarch", "p0:token:Grub, Notorious Auntie"}}
	if _, changed := mayYes(sc, []rules.OracleDecision{keep}); changed {
		t.Fatal("mayYes pinned option 0's label over the runner's option-1 keep")
	}
	// The sibling: the same decision answered at option 0 is still pinned.
	keep.PickIdx = []int{0}
	out, changed := mayYes(sc, []rules.OracleDecision{keep})
	if !changed || len(out.Steps[0].Answers) != 1 || out.Steps[0].Answers[0].Pick[0] != "Grub, Notorious Auntie" {
		t.Fatalf("option-0 keep not pinned: %+v", out.Steps[0].Answers)
	}
}
