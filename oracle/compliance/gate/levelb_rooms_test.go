package gate_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/gate"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestLevelBRoomVerdictsAreLookedUpByFace pins the level-B gate's verdict
// lookup for split/Room cards (ticket cli-20261009T031407Z-31a90cb3): the
// pass keys every verdict row by the card's front face ("Bottomless Pool"),
// so the gate must look the rows up there and generate the scenario from the
// face, the way checkA does. Before the fix every Room with a committed row
// reported "no verdict for <whole name>/..." over an existing current row.
func TestLevelBRoomVerdictsAreLookedUpByFace(t *testing.T) {
	t.Parallel()
	// pinnedKey names the requirement each card's pinned verdict row settles,
	// so the no-verdict assertion above reads only those keys.
	pinnedKey := func(card string) string {
		switch card {
		case "Dazzling Theater":
			return "static#0.0"
		case "Central Elevator":
			return "trigger#0.0"
		default:
			return "trigger#0.0"
		}
	}
	root := filepath.Join("..", "..", "..")
	reg := testutil.CorpusRegistry(t)
	verdicts, err := compliance.LoadVerdicts(filepath.Join(root, compliance.VerdictDir))
	if err != nil {
		t.Fatal(err)
	}
	// Precondition: the committed rows this test reads really are there and
	// agree/diverge as assumed; losing them must fail loudly, not pass.
	type want struct {
		card, tmpl, status string
	}
	for _, w := range []want{
		{"Bottomless Pool", "trigger#0.0", compliance.StatusAgree},
		{"Dazzling Theater", "static#0.0", compliance.StatusAgree},
		{"Central Elevator", "trigger#0.0", compliance.StatusAgree},
	} {
		r, ok := verdicts[w.card][w.tmpl]
		if !ok {
			t.Fatalf("precondition: no committed %s verdict row for %q/%q", w.status, w.card, w.tmpl)
		}
		if r.Status != w.status {
			t.Fatalf("precondition: %s/%s verdict is %q, want %q", w.card, w.tmpl, r.Status, w.status)
		}
	}
	probs, err := gate.Check(reg, root, "DSK", "B")
	if err != nil {
		t.Fatal(err)
	}
	byCard := map[string][]string{}
	for _, p := range probs {
		byCard[p.Card] = append(byCard[p.Card], p.Reason)
	}
	// The agreed rows settle their requirement: never a missing-verdict
	// problem for the requirements this test pins (their other requirements
	// may still skip, diverge, or be a NEW item whose verdict the host
	// driver-replay batch owes -- Dazzling Theater/static#1.0 since the
	// room-door static templates landed).
	for _, card := range []string{"Bottomless Pool", "Dazzling Theater", "Central Elevator"} {
		for _, r := range byCard[card] {
			if (strings.HasPrefix(r, "no verdict for") || strings.HasPrefix(r, "verdict is for an older scenario")) &&
				strings.Contains(r, "/"+pinnedKey(card)+"/") {
				t.Errorf("%s: %q; the committed row settles this requirement", card, r)
			}
		}
	}
	// Every committed diverge row of these Rooms is reported as a divergence,
	// with its real detail, never as a missing verdict. The rows are read from
	// the store rather than pinned: rulings (xmage_wrong) and driver fixes keep
	// moving individual Room rows off diverge, and that must not break the
	// lookup this test guards.
	type rowKey struct{ card, key string }
	var diverged []rowKey
	for _, card := range []string{"Central Elevator", "Roaring Furnace"} {
		for key, r := range verdicts[card] {
			if r.Status == compliance.StatusDiverge {
				diverged = append(diverged, rowKey{card, key})
			}
		}
	}
	for _, tc := range diverged {
		found := false
		for _, r := range byCard[tc.card] {
			if strings.Contains(r, "verdict diverge ("+tc.key+")") {
				found = true
			}
			if strings.HasPrefix(r, "no verdict for") {
				t.Errorf("%s: %q; a committed diverge row must be reported as one", tc.card, r)
			}
		}
		if !found {
			t.Errorf("%s: no %q diverge problem among %v", tc.card, tc.key, byCard[tc.card])
		}
	}
	// The regression itself: no DSK problem reports a missing verdict for a
	// whole "A // B" id -- that spelling is never a verdict key.
	for _, p := range probs {
		if strings.Contains(p.Reason, "no verdict for ") && strings.Contains(p.Reason, " // ") {
			t.Errorf("%s: %q; a whole split name is never a verdict key (the row lives under the front face)", p.Card, p.Reason)
		}
	}
}
