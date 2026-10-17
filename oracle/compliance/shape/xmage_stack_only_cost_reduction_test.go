package shape

import (
	"path/filepath"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestXMageStackOnlyCostReductionRuling pins the harness ruling for Swat
// Away static#0.0: XMage's reduction is stack-only, so TestPlayer cannot
// find the UU cast the scenario (correctly, CR 601.2f) pays. Lashwhip
// Predator's static is the same SpellCostReductionSourceEffect
// .setCanWorksOnStackOnly(true) (operator-triaged 2026-10-13), so the ruling
// covers it through its "-" API signature. Bite Down on Crime fails with the
// same harness shape for another cause (XMage prices it with a cost
// adjuster), so the API narrowing must leave it unruled.
func TestXMageStackOnlyCostReductionRuling(t *testing.T) {
	const rulingID = "xmage-stack-only-cost-reduction-unplayable"
	const wantKey = "static | harness | xmage | AssertionError: Can't find ability to activate command: Cast $CARD"

	rs, err := LoadRulings(filepath.Join("..", "..", "..", RulingDir))
	if err != nil {
		t.Fatal(err)
	}
	reg := testutil.CorpusRegistry(t)
	api := func(card string) string {
		c, ok := reg.Lookup(card)
		if !ok || len(c.Faces) == 0 {
			t.Fatalf("%s not in the corpus", card)
		}
		return API(c.Faces[0])
	}
	row := func(card, tmpl, detail string) compliance.VerdictRow {
		return compliance.VerdictRow{Card: card, Template: tmpl, Status: compliance.StatusHarness, Detail: detail}
	}
	cant := func(card, rest string) string {
		return "xmage: AssertionError: Can't find ability to activate command: Cast " + card + rest
	}

	swat := row("Swat Away", "static#0.0", cant("Swat Away", "$target=@p1:Grizzly Bears"))
	swatAPI := api("Swat Away")
	if s, ok := Of(swat); !ok || s.Key() != wantKey {
		t.Fatalf("Swat Away shape %q (ok=%v), want %q", s.Key(), ok, wantKey)
	}
	m, overlap, changed := Classify(&swat, rs, swatAPI)
	if m == nil || m.ID != rulingID || !changed {
		t.Fatalf("Swat Away (api %s): ruling did not apply: %+v", swatAPI, swat)
	}
	if len(overlap) != 0 {
		t.Fatalf("Swat Away: overlapping rulings %v", overlap)
	}
	if swat.Status != compliance.StatusHarness || swat.RulingID != rulingID || swat.Frozen != nil {
		t.Fatalf("Swat Away: classified %+v, want an unfrozen harness row ruled %s", swat, rulingID)
	}

	// Same shape, different cards: the precondition is that their shape
	// really is Swat Away's, so only the API narrowing keeps them out.
	for _, c := range []struct {
		card, tmpl string
		ruled      bool
	}{
		{"Lashwhip Predator", "static#0.0", true},
		{"Bite Down on Crime", "static#0.1", false},
	} {
		r := row(c.card, c.tmpl, cant(c.card, ""))
		if s, ok := Of(r); !ok || s.Key() != wantKey {
			t.Fatalf("%s shape %q (ok=%v), want %q", c.card, s.Key(), ok, wantKey)
		}
		a := api(c.card)
		if a == swatAPI {
			t.Fatalf("%s shares Swat Away's API %q", c.card, a)
		}
		m, _, _ := Classify(&r, rs, a)
		switch {
		case c.ruled && (m == nil || m.ID != rulingID):
			t.Errorf("%s (api %s): not ruled %s", c.card, a, rulingID)
		case !c.ruled && m != nil:
			t.Errorf("%s (api %s): ruled %s; its cause is untriaged", c.card, a, m.ID)
		}
	}

	// The row as committed today still carries the pre-fix driver message;
	// the ruling must not claim it.
	old := row("Swat Away", "static#0.0", "xmage: IllegalArgumentException: pass at step 2 needs seat 0 to be the active player")
	if m, _, _ := Classify(&old, rs, swatAPI); m != nil {
		t.Errorf("pre-fix Swat Away row ruled %s", m.ID)
	}
}
