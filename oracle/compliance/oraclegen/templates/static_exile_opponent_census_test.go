package templates_test

// static_exile_opponent_census_test.go is the corpus-wide census of the
// opponent-reachable exiled-with may-play class (ticket lb-mayplayopp): every
// Mode$ Continuous MayPlay$ True static whose Affected$ names ExiledWithSource
// WITHOUT a YouOwn qualifier, so the cards it covers can be an OPPONENT's
// (Azula, Cunning Usurper; Intellect Devourer; Nightveil Specter). Before the
// ticket the may-play spell walk visited only the caster's own cards, and
// the class's opponent-owned rows were skipped with a named engine gap.
//
// It is a ratchet: served may only rise and each skip reason may only fall.
// When a fixture or engine change serves another row, lower its skip and
// raise served here in the same commit; a regression fails.

import (
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/levelb"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen/templates"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// wantOpponentExiledWithCensus is the measured split at lb-mayplayopp: 22
// rows. Azula, Cunning Usurper and Null Summoner -- the two rows that carried
// the "opponent-owned exiled card is not offered" gap -- are two of the three
// served. The 15 opponent-owned rows are the source's Dig-exile class
// (Maralen, Fae Ascendant and the other Dig carriers): lb-digexiledwith made
// the Dig exile record the exiled-with association, so the old "cards exiled
// by the source's Dig carry no exiled-with association" reason no longer
// holds, and those rows now name the remaining may-play walk gap.
// Re-pinned by lb4w2-engine: that "walk gap" was never measured -- the walk
// offers opponent-owned cards (Azula, Null Summoner) -- and the Dig rows only
// lacked a library-origin fixture. It now serves Maralen, Fae Ascendant (an
// enters Dig of a targeted opponent's library), and the other 14 rows name
// the real gap: no fixture exiles an opponent's card with their source.
// lb5-exileperm served Valgavoth, Terror Eater: the engine now opens a
// MayPlayAltManaCost$ static's zone permission and prices the cast at the
// alternative, and static_offer_altcost.go observes the offered cast and its
// life payment.
var wantOpponentExiledWithCensus = map[string]int{
	"served": 5,
	"skip:static no fixture exiles an opponent's card with the source (its exile is not an enters ChangeZone or Dig of a targeted opponent's cards)": 14,
	"skip:static look-at not observable": 2,
	"skip:static no mana cost":           1,
}

// opponentExiledWithStatic reports whether st is the census class.
func opponentExiledWithStatic(st cards.Static) bool {
	if st.Mode != "Continuous" || strings.TrimSpace(st.ParamStr(cards.PKMayPlay)) != "True" {
		return false
	}
	affected := st.ParamStr(cards.PKAffected)
	return strings.Contains(affected, "ExiledWithSource") && !strings.Contains(affected, "YouOwn")
}

func TestOpponentExiledWithMayPlayCensus(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	got := map[string]int{}
	for _, c := range reg.AllCards() {
		for _, r := range levelb.Requirements(c) {
			if r.Family != "static" || r.Face >= len(c.Faces) {
				continue
			}
			slot, err := strconv.Atoi(r.Slot)
			if err != nil || slot >= len(c.Faces[r.Face].Statics) || !opponentExiledWithStatic(c.Faces[r.Face].Statics[slot]) {
				continue
			}
			switch {
			case r.Gap != "":
				got["gap:"+r.Gap]++
			default:
				if _, skip := templates.GenerateB(reg, c.Faces[0].Name, r); skip != nil {
					got["skip:"+skip.Reason]++
				} else {
					got["served"]++
				}
			}
		}
	}
	if got["served"] == 0 {
		t.Fatalf("precondition: the census served no row (the class filter or the corpus is empty): %v", got)
	}
	if got["served"] < wantOpponentExiledWithCensus["served"] {
		t.Errorf("served fell: got %d, want >= %d", got["served"], wantOpponentExiledWithCensus["served"])
	}
	for k, n := range got {
		if k != "served" && n > wantOpponentExiledWithCensus[k] {
			t.Errorf("%s grew: got %d, want <= %d", k, n, wantOpponentExiledWithCensus[k])
		}
	}
}
