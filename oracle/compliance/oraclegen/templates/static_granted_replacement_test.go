package templates

import (
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/engine/cards"
)

// TestStaticGrantedReplacementServed: the two DFT AddReplacementEffect$ rows
// this template serves generate a scenario that replays and whose own
// observation holds. The observation is checked gorge-side by the generator's
// gate-off control, so a row whose granted replacement the engine does not
// apply can only skip.
func TestStaticGrantedReplacementServed(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct{ card, key string }{
		{"Far Fortune, End Boss", "static#0.0"},
		{"Vnwxt, Verbose Host", "static#0.1"},
	} {
		t.Run(tc.card, func(t *testing.T) {
			req := counterReq(t, reg, tc.card, tc.key)
			it, skip := GenerateB(reg, tc.card, req)
			if skip != nil {
				t.Fatalf("GenerateB skipped: %s", skip.Reason)
			}
			// Precondition: the scenario really supplies the Condition$
			// MaxSpeed gate, so the observation is about the grant and not
			// a grant that fired ungated.
			if got := it.Setup["p0"].Speed; got != maxSpeed {
				t.Fatalf("gate-on fixture has p0 at speed %d, want %d", got, maxSpeed)
			}
			if len(it.Scenario.Steps) == 0 {
				t.Fatal("served item has no observation steps")
			}
			res, ok := runStatic(reg, it.Scenario)
			if !ok {
				t.Fatal("served scenario does not replay in gorge")
			}
			if len(res.Fails) != 0 {
				t.Fatalf("served scenario's own expectations fail: %v", res.Fails)
			}
			if len(res.Snapshots) == 0 {
				t.Fatal("served scenario produced no snapshot")
			}
			last := res.Snapshots[len(res.Snapshots)-1]
			switch tc.card {
			case "Far Fortune, End Boss":
				// Shock's 2 plus the granted 1 leaves p1 at 17.
				if life, ok := snapshotLife(last, 1); !ok || life != 17 {
					t.Fatalf("p1 life = %d (ok=%v), want 17", life, ok)
				}
			case "Vnwxt, Verbose Host":
				// Divination draws two; each is replaced by a draw of two,
				// so p0's hand (Divination cast out of it) holds four.
				if got := len(last.Players[0].Hand); got != 4 {
					t.Fatalf("p0 hand = %d, want 4 (each of Divination's two draws replaced by two)", got)
				}
			}
		})
	}
}

// TestStaticGrantedReplacementFailClosed: an AddReplacementEffect$ body this
// template does not observe is NOT served, so the row keeps its named
// replacement-grant gap. Scion of Halaster grants to a COMMANDER (the XMage
// driver rejects command-zone setup, so no cross-engine fixture exists), and
// Cloudsteel Kirin's grant is a bodyless Layer$ CantHappen line (its row is
// served by the Flying grant it also carries, so only the replacement arm is
// pinned unserved there, not the row's skip).
func TestStaticGrantedReplacementFailClosed(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct {
		name, key string
		namedGap  bool
	}{
		{"Scion of Halaster", "static#0.0", true},
		{"Cloudsteel Kirin", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, ok := reg.Lookup(tc.name)
			if !ok || len(c.Faces) == 0 {
				t.Fatalf("precondition: %s not in the corpus", tc.name)
			}
			f := c.Faces[0]
			key := tc.key
			if key == "" {
				for i, st := range f.Statics {
					if st.HasParam(cards.PKAddReplacementEffect) {
						key = "static#0." + strconv.Itoa(i)
					}
				}
			}
			req := counterReq(t, reg, tc.name, key)
			st, _ := staticSlotOf(f, req)
			// PRECONDITION: the slot really carries an AddReplacementEffect$
			// grant, so the assertion is about a grant and not an empty slot.
			if !st.HasParam(cards.PKAddReplacementEffect) {
				t.Fatalf("precondition: %s %s does not grant a replacement effect: %v", tc.name, req.Key, st.Params)
			}
			if _, served := staticGrantedReplacementItem(reg, f, tc.name, req, st); served {
				t.Fatal("a granted replacement was served, want no observation")
			}
			if !tc.namedGap {
				return
			}
			_, skip := GenerateB(reg, tc.name, req)
			if skip == nil || !strings.Contains(skip.Reason, staticGrantReplacementReason) {
				t.Fatalf("skip = %v, want the named replacement-grant gap %q", skip, staticGrantReplacementReason)
			}
		})
	}
}
