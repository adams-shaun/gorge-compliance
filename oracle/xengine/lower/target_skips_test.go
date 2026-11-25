package lower

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestLowerCastLaterUpToSlotSkips: Relive the Past has three independent
// "up to one" target objects (artifact, land, non-Aura enchantment, CR
// 601.2c). With only lands in the graveyard gorge poses one ask, for the land
// object, and the cast step keeps the flat list [Forest]. XMage poses all
// three objects in order, so a lone ref set inline lands on the artifact
// object and the cast is never found (xproof xs:XP:relive-the-past:cast:s2).
// The lowering must name the two empty objects: a skip before the target for
// slot 0 and one after it for slot 2.
func TestLowerCastLaterUpToSlotSkips(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	// hand: Relive; battlefield: 6 Forest + Plains for {5}{G}{W}; graveyard:
	// 7 Forests and nothing an artifact or enchantment slot accepts.
	cards := slices.Concat([]string{"Relive the Past"}, repeatCard(6, "Forest"), []string{"Plains"},
		repeatCard(7, "Forest"), repeatCard(25, "Forest"))
	pair := []decks.Deck{
		{Name: "start-p0", Colours: "GW", Cards: cards},
		{Name: "start-p1", Colours: "W", Cards: repeatCard(40, "Plains")},
	}
	start := &rules.StartSpec{Seats: []rules.StartSeat{
		{Hand: []int{0}, Battlefield: []int{1, 2, 3, 4, 5, 6, 7}, Graveyard: []int{8, 9, 10, 11, 12, 13, 14}},
		{},
	}}
	l := spanShape(t, reg, pair, start, startGameSeed(0, 1), "Relive the Past",
		"xs:XP:relive-the-past:cast:s1", "xproof start Relive the Past cast seed 1")
	cast := -1
	for i, st := range l.Item.Steps {
		if st.Op == "cast" && st.Card == "p0:Relive the Past" {
			cast = i
			break
		}
	}
	if cast < 0 {
		t.Fatalf("no Relive the Past cast step in %+v", l.Item.Steps)
	}
	if got := l.Item.Steps[cast].Targets; len(got) != 1 {
		t.Fatalf("cast targets = %v, want the one graveyard land", got)
	}
	if len(l.Item.XTargetSkips) != len(l.Item.Steps) {
		t.Fatalf("XTargetSkips has %d entries for %d steps: no plan was lowered", len(l.Item.XTargetSkips), len(l.Item.Steps))
	}
	want := []oraclegen.XTargetSkip{{At: 0, Slot: 0}, {At: 1, Slot: 2}}
	if got := l.Item.XTargetSkips[cast]; !slices.Equal(got, want) {
		t.Fatalf("cast step skips = %v, want %v (close the artifact object before the land, the enchantment object after it)", got, want)
	}
	for i, list := range l.Item.XTargetSkips {
		if i != cast && len(list) > 0 {
			t.Errorf("step %d (%s) carries skips %v", i, l.Item.Steps[i].Op, list)
		}
	}
	f := castFace(reg, l.Item.Steps[cast].Card)
	if f == nil {
		t.Fatal("no face for the cast card")
	}
	if err := oraclegen.CheckTargetSkips(oraclegen.SlotSpecs(f), l.Item.Steps, l.Item.XTargetSkips); err != nil {
		t.Fatalf("plan fails its own validation: %v", err)
	}
}
