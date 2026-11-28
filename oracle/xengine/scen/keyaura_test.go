package scen

import (
	"slices"
	"testing"
)

// TestStartKeyAuraGetsBearer: an activate or trigger key of an Aura placed on
// the battlefield is attached to a vanilla bearer its Enchant keyword takes
// (StartAttach), where it was refused battlefield:aura-unattached. The bearer
// is inert, so the key's play stays the one the focus names
// (xproof-starts-force-the-line: "an Aura gets a vanilla bearer").
func TestStartKeyAuraGetsBearer(t *testing.T) {
	reg, b := testBuilder(t)
	rows := []struct {
		card, key string
		oppBearer bool   // the bearer is on seat 1 (no fixture today: kept for an Enchant creature an opponent controls)
		focus     string // the focus card, "" for the Aura itself
		kind      string
	}{
		{"Gaea's Embrace", "f0/a0", false, "Gaea's Embrace", "ability"}, // activated ability, Defined$ Enchanted
		{"Mourning", "f0/a0", false, "Mourning", "ability"},
		{"Ghitu Firebreathing", "f0/a1", false, "Ghitu Firebreathing", "ability"},
		{"Brilliant Wings", "f0/t0", false, "", "cast"}, // a creature you control enters: the carrier is cast
		{"Prison Term", "f0/t0", false, "", "cast"},     // an opponent's creature enters: the carrier is cast by seat 1
	}
	for _, r := range rows {
		t.Run(r.card+"/"+r.key, func(t *testing.T) {
			res, err := Generate(reg, b, r.card, r.key, Options{Prefer: "start"})
			if err != nil {
				t.Fatal(err)
			}
			if res.Path != PathStart || res.Start == nil {
				t.Fatalf("path %q reason %q, want a start file", res.Path, res.Reason)
			}
			f := res.Start
			wantSeat := 0 // the Aura is the key card on seat 0; Prison Term fires on a creature seat 1 controls entering
			if r.card == "Prison Term" {
				wantSeat = 1
			}
			attached(t, f, r.card)
			if len(f.Setup.P0.Attach) != 1 || f.Setup.P0.Attach[0].Opp != r.oppBearer {
				t.Fatalf("p0 attach %+v, want one attachment with opp=%v", f.Setup.P0.Attach, r.oppBearer)
			}
			bearer := f.Setup.P0.Attach[0].To
			c, ok := reg.Lookup(bearer)
			if !ok || len(c.Faces) == 0 || !c.Faces[0].IsCreature() || FieldBlock(c.Faces[0]) != "" ||
				len(c.Faces[0].Abilities)+len(c.Faces[0].Triggers) != 0 {
				t.Errorf("bearer %q is not a vanilla creature", bearer)
			}
			if f.Focus == nil || f.Focus.Seat == nil || *f.Focus.Seat != wantSeat || f.Focus.Kind != r.kind || r.focus != "" && f.Focus.Card != r.focus {
				t.Errorf("focus %+v, want seat %d %s %q", f.Focus, wantSeat, r.kind, r.focus)
			}
			if !slices.Contains(f.Setup.P0.Battlefield, r.card) {
				t.Errorf("p0 battlefield %v does not hold the Aura", f.Setup.P0.Battlefield)
			}
			if _, err := DecodeStartFile(mustMarshalStart(t, f)); err != nil {
				t.Errorf("start file does not decode: %v", err)
			}
		})
	}
}

func mustMarshalStart(t *testing.T, f *StartFile) []byte {
	t.Helper()
	raw, err := f.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// TestStartKeyAuraRefusalsUnchanged: an Aura whose Enchant keyword no card
// carrier can satisfy (a player; Enchant:Forest and Enchant:land Auras now get
// a basic land bearer, see TestScenKeyAuraLandBearer) keeps its refusal, and
// the starts that already attached their Aura (a dies trigger on the bearer,
// an Aura destroyed) are untouched.
func TestStartKeyAuraRefusalsUnchanged(t *testing.T) {
	checkStartRows(t, []startRow{
		{card: "Archnemesis", key: "f0/t1", reason: "battlefield:aura-unattached"}, // Enchant player
	})
	reg, b := testBuilder(t)
	res, err := Generate(reg, b, "Dead Man's Chest", "f0/t0", Options{Prefer: "start"})
	if err != nil || res.Start == nil {
		t.Fatalf("Dead Man's Chest: %+v %v", res, err)
	}
	if got := res.Start.Setup.P0.Attach; len(got) != 1 || !got[0].Opp {
		t.Errorf("Dead Man's Chest attach %+v, want the one attachment to the opposing bearer", got)
	}
}
