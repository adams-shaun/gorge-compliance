package templates

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/levelb"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// wantLb5MiscCarriers pins every corpus carrier, across the declared
// Standard sets, of the three wave-5 class-T mechanisms, by "set card key",
// and the template outcome each must reach (served, or the named skip). It
// fails in both directions: a new carrier, a lost one, or an outcome change.
//
//   - charm-trigger: a triggered ability whose effect is a Charm with
//     targeted modes and no root target slot (charmTriggerFixtures). Since
//     CR 700.2a/603.3c landed (lb-modaltgt) such a trigger with no legal mode
//     never reaches the stack, so the mode fixtures are what fire it.
//   - flip-coin-mod: Edgar's FlipCoinMod shape, read by
//     effects.FlipCoinForced and observed by the coin-flip probe.
//   - nameless-blocker: a CantBlockBy whose ValidBlocker$ only a nameless
//     (face-down) creature satisfies, observed by the face-down blocker.
//
// A requirement level A already settles (CoveredByA) is not generated and is
// not a carrier here. Every charm-trigger row but Ferocification was served
// by an earlier cause/fixture pass before the mode fixtures existed (a mode
// with a non-creature or player target the empty fixture already satisfies);
// the mode fixtures run last, so those rows keep their scenario bytes.
var wantLb5MiscCarriers = map[string]string{
	"BIG Oltec Matterweaver trigger#0.0":       "charm-trigger served",
	"DSK Silent Hallcreeper trigger#0.0":       "charm-trigger served",
	"FDN Demonic Pact trigger#0.0":             "charm-trigger served",
	"FDN Kykar, Zephyr Awakener trigger#0.0":   "charm-trigger served",
	"FDN Sylvan Scavenging trigger#0.0":        "charm-trigger served",
	"HOB Bejeweled Warg trigger#0.0":           "charm-trigger served",
	"HOB Elven Raft-Steerer trigger#0.0":       "charm-trigger served",
	"LCI Breeches, Eager Pillager trigger#0.0": "charm-trigger served",
	"OTJ Ferocification trigger#0.0":           "charm-trigger served",
	"TLA Appa, Loyal Sky Bison trigger#0.1":    "charm-trigger served",
	"TLA Momo, Playful Pet trigger#0.0":        "charm-trigger served",
	"FIN Edgar, King of Figaro static#0.0":     "flip-coin-mod served",
	"MKM Pompous Gadabout static#0.1":          "nameless-blocker served",
}

func TestLb5MiscCarrierCensus(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	root := filepath.Join("..", "..", "..", "..")
	raw, err := os.ReadFile(filepath.Join(root, "oracle", "compliance", "declared.json"))
	if err != nil {
		t.Fatal(err)
	}
	var declared map[string]string
	if err := json.Unmarshal(raw, &declared); err != nil {
		t.Fatal(err)
	}
	sets := make([]string, 0, len(declared))
	for s := range declared {
		sets = append(sets, s)
	}
	sort.Strings(sets)
	if len(sets) < 20 {
		t.Fatalf("precondition: %d declared sets, want the 20 Standard sets", len(sets))
	}
	has := func(n string) bool { _, ok := reg.Lookup(n); return ok }
	folded := compliance.FoldedNames(reg)
	got := map[string]string{}
	for _, set := range sets {
		printed, err := compliance.LoadPrinted(filepath.Join(root, "oracle", "compliance", "printed"), set)
		if err != nil {
			t.Fatalf("%s: %v", set, err)
		}
		for _, name := range printed.Cards {
			card, ok := compliance.CorpusNameFold(has, folded, name)
			if !ok {
				continue
			}
			c, _ := reg.Lookup(card)
			for _, r := range levelb.Requirements(c) {
				if r.Gap != "" || r.CoveredByA || r.Face < 0 || r.Face >= len(c.Faces) {
					continue
				}
				class := lb5MiscClass(reg, c.Faces[r.Face], r)
				if class == "" {
					continue
				}
				outcome := "served"
				if _, skip := GenerateB(reg, card, r); skip != nil {
					outcome = "skip: " + skip.Reason
				}
				got[set+" "+card+" "+r.Key] = class + " " + outcome
			}
		}
	}
	for k, want := range wantLb5MiscCarriers {
		if got[k] != want {
			t.Errorf("%s: got %q, want %q", k, got[k], want)
		}
	}
	for k, g := range got {
		if _, ok := wantLb5MiscCarriers[k]; !ok {
			t.Errorf("unpinned carrier %s: %q", k, g)
		}
	}
}

// lb5MiscClass names the class-T mechanism requirement r carries, or "".
func lb5MiscClass(reg *cards.Registry, f *cards.Face, r levelb.Requirement) string {
	switch {
	case r.Family == "trigger":
		idx := -1
		for i := range f.Triggers {
			if r.Slot == strconv.Itoa(i) {
				idx = i
			}
		}
		if idx >= 0 && len(charmTriggerFixtures(reg, f, &f.Triggers[idx])) > 0 {
			return "charm-trigger"
		}
	case r.Sub == "static.flip-coin-mod":
		return "flip-coin-mod"
	case r.Sub == "static.cant-block-by-blocker-filter":
		if st, _ := staticSlotOf(f, r); namelessBlockerFilter(st.ParamStr(cards.PKValidBlocker)) {
			return "nameless-blocker"
		}
	}
	return ""
}
