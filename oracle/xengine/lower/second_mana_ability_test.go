package lower

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// focusManaAbility lowers a span whose focus seat activates ability 1 of card
// (a source's second mana ability, which asks a colour), and returns the
// lowered window and its first activate step of that card.
func focusManaAbility(t *testing.T, card, land string) (Lowered, oraclegen.Step) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	d0 := decks.Deck{Name: "p0", Colours: "R", Cards: []string{card}}
	d1 := decks.Deck{Name: "p1", Colours: "W"}
	for len(d0.Cards) < 40 {
		d0.Cards = append(d0.Cards, land)
	}
	for len(d1.Cards) < 40 {
		d1.Cards = append(d1.Cards, "Plains")
	}
	start := &rules.StartSpec{Seats: []rules.StartSeat{{Battlefield: []int{0}}, {}}}
	opt := game.Options{Set: "XP", MaxTurns: 2, Start: start,
		Focus: &game.Focus{Seat: 0, Card: card, Kind: "ability", Ability: 1}}
	l, why, _, err := Span(reg, []decks.Deck{d0, d1}, rules.SeedForStarter(1, 2, 0), opt, false,
		"xs:XP:"+card+":f0a1:s1", "xproof start "+card)
	if err != nil {
		t.Fatal(err)
	}
	if len(why) > 0 {
		t.Fatalf("span refused: %v", why)
	}
	if l.Truncated != "" && l.Truncated != "stall" {
		t.Fatalf("truncated %s at entry %d, want no truncation", l.Truncated, l.TruncatedAt)
	}
	if c := CheckWindow(reg, l); c.Status != StatusOK {
		t.Fatalf("CheckWindow: %+v", c)
	}
	for _, st := range l.Item.Steps {
		if st.Op == "activate" && refName(st.Card) == card {
			return l, st
		}
	}
	t.Fatalf("no activate step of %s: %+v", card, l.Item.Steps)
	return l, oraclegen.Step{}
}

// TestLowerSecondManaAbilityColour: a source's second mana ability asks a
// colour (a pain land's "Add {U} or {R}", Sliver Hive's "any color"), so its
// activation sits on the ability ask and the step carries colour answers. A
// queued "choose" owns every choice of the source, the ability choice among
// them, so the step answers that too, in the order the runner asks: an explicit
// colour combination is flattened into one option per colour (the colour pick
// is the ability pick); an "any color" ability is one option, then a colour ask.
func TestLowerSecondManaAbilityColour(t *testing.T) {
	_, st := focusManaAbility(t, "Shivan Reef", "Mountain")
	if st.AbilityIndex == nil || *st.AbilityIndex != 1 {
		t.Fatalf("ability index %v, want 1", st.AbilityIndex)
	}
	if len(st.Answers) != 1 || st.Answers[0].Kind != "choose" || len(st.Answers[0].Pick) != 1 ||
		(st.Answers[0].Pick[0] != "U" && st.Answers[0].Pick[0] != "R") {
		t.Errorf("Shivan Reef answers %+v, want one choose of the colour", st.Answers)
	}

	_, st = focusManaAbility(t, "Sliver Hive", "Mountain")
	if len(st.Answers) != 2 || !reflect.DeepEqual(st.Answers[0], oraclegen.Answer{Kind: "choose", Pick: []string{"Add any color"}}) ||
		st.Answers[1].Kind != "choose" || len(st.Answers[1].Pick) != 1 {
		t.Errorf("Sliver Hive answers %+v, want the any-colour wheel option, then the colour", st.Answers)
	}
}
