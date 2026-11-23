package lower

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// thornvaultStart is the xproof fixture: p0 has Thornvault Forager and four
// Forests on the battlefield (deck indices 0..4) and Bakersbane Duo on top of
// the library (5); p1 plays Plains.
func thornvaultStart() ([]decks.Deck, *rules.StartSpec) {
	d0 := decks.Deck{Name: "start-p0", Colours: "G", Cards: []string{"Thornvault Forager", "Forest", "Forest", "Forest", "Forest", "Bakersbane Duo"}}
	for len(d0.Cards) < 40 {
		d0.Cards = append(d0.Cards, "Forest")
	}
	d1 := decks.Deck{Name: "start-p1", Colours: "W"}
	for len(d1.Cards) < 40 {
		d1.Cards = append(d1.Cards, "Plains")
	}
	return []decks.Deck{d0, d1}, &rules.StartSpec{Seats: []rules.StartSeat{{Battlefield: []int{0, 1, 2, 3, 4}, Library: []int{5}}, {}}}
}

func TestSpanThornvault(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	pair, start := thornvaultStart()
	opt := game.Options{Set: "BLB", MaxTurns: 2, Start: start, Focus: &game.Focus{Seat: 0, Card: "Thornvault Forager", Kind: "ability", Ability: 2}}
	l, why, tp, err := Span(reg, pair, rules.SeedForStarter(1, 2, 0), opt, false, "xs:BLB:thornvault-forager:f0a2:s1", "xproof start Thornvault Forager f0/a2 seed 1")
	if err != nil {
		t.Fatal(err)
	}
	if len(why) > 0 {
		t.Fatalf("span refused: %v", why)
	}
	if l.Item.Template != SpanTemplate {
		t.Errorf("template %q, want %q", l.Item.Template, SpanTemplate)
	}
	if c := CheckWindow(reg, l); c.Status != StatusOK {
		t.Errorf("CheckWindow: %+v", c)
	}
	if !l.HasRNG || !slices.Equal(l.Item.Ignore, []string{"library_top"}) {
		t.Errorf("HasRNG %v, Ignore %v: want the search's shuffle to mark library_top", l.HasRNG, l.Item.Ignore)
	}
	if l.Truncated != "stall" || l.TruncatedAt != len(tp.Entries) {
		// the game ends at the turn watchdog, after every entry the tape has
		t.Logf("truncated %q at %d of %d entries", l.Truncated, l.TruncatedAt, len(tp.Entries))
	}
	activated := false
	for _, s := range l.Item.Steps {
		activated = activated || s.Op == "activate" && s.Card == "p0:Thornvault Forager"
	}
	if !activated {
		t.Error("no step activates Thornvault Forager")
	}
}

// A game with no turn-1 main phase to cut at (the watchdog ends it first) is
// refused with its reason, not lowered.
func TestSpanNoCut(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	pair, start := thornvaultStart()
	_, why, _, err := Span(reg, pair, rules.SeedForStarter(1, 2, 0), game.Options{Set: "BLB", MaxTurns: 1, Start: start}, false, "x", "x")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(why, []string{"span:no-turn1-cut"}) {
		t.Errorf("reasons %v, want span:no-turn1-cut", why)
	}
}

// windowsGolden is the SHA-256 of Windows' output (items, refusals) over the
// fixed pair and seed below, taken before lowerWindow and windowTape took their
// end parameter: the refactor leaves a window's bytes alone.
const windowsGolden = "701d34f4f911b35d55325ba862b5a592baf59e5bcafe25845b5adc1dc40faaaa"

func TestSpanWindowsUnchanged(t *testing.T) {
	reg, pairs := fdnPairs(t, 1)
	ls, rs, _, err := Windows(reg, pairs[0][:], 100, game.Options{Set: "FDN", MaxTurns: 8}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(ls) == 0 {
		t.Fatal("no window lowered")
	}
	b, err := json.Marshal(struct {
		L []Lowered
		R []Refusal
	}{ls, rs})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	if got := hex.EncodeToString(sum[:]); got != windowsGolden {
		t.Errorf("Windows output sha256 %s, want %s (%d windows, %d refused)", got, windowsGolden, len(ls), len(rs))
	}
}
