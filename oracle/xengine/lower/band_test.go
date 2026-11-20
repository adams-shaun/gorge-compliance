package lower

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestBandAnswers: a band declaration is several KChoose asks after the attack
// declaration. The lowering attaches them to the attack step as its Answers,
// in tape order: an accepted member first (gorge's pick ref), then the next
// leader's decline (the option label). Losing the order would replay the
// decline before the join, so the follower would form no band even though the
// recorded game did (CR 702.22c).
func TestBandAnswers(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	tp := &game.Tape{
		Format: game.TapeFormat, Set: "SC15", Seed: 1, Starter: 0,
		Decks: []game.Deck{
			{Name: "-c0-g0-band", Cards: []string{"Benalish Hero", "Kjeldoran Warrior", "Grizzly Bears", "Plains"}},
			{Name: "-c0-g0-band-b", Cards: []string{"Plains"}},
		},
		Open: []game.Opening{
			{Hand: []string{"Benalish Hero", "Kjeldoran Warrior", "Grizzly Bears"}, Library: []string{"Plains"}},
			{Hand: []string{"Plains"}, Library: []string{"Plains"}},
		},
		Entries: []game.Entry{
			{Seq: 1, Seat: 0, Kind: "priority", Turn: 1, Step: "main1",
				Picks: []game.Pick{{Kind: "pass", Player: 0}}},
			{Seq: 2, Seat: 0, Kind: "attackers", Turn: 1, Step: "declare-attackers",
				Picks: []game.Pick{
					{Kind: "attacker", Ref: "p0:Benalish Hero", Player: 1},
					{Kind: "attacker", Ref: "p0:Kjeldoran Warrior", Player: 1},
					{Kind: "attacker", Ref: "p0:Grizzly Bears", Player: 1},
				}},
			{Seq: 3, Seat: 0, Kind: "choose", Turn: 1, Step: "declare-attackers",
				Picks: []game.Pick{{Kind: "band", Obj: 4, Ref: "p0:Grizzly Bears", Player: 0,
					Label: "Band Grizzly Bears with Benalish Hero"}}},
			{Seq: 4, Seat: 0, Kind: "choose", Turn: 1, Step: "declare-attackers",
				Picks: []game.Pick{{Kind: "band", Player: 0,
					Label: "No more creatures in Kjeldoran Warrior's band"}}},
		},
	}
	l, err := Tape(reg, tp)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Item.Steps) != 2 || l.Item.Steps[0].Op != "pass" || l.Item.Steps[1].Op != "attack" {
		t.Fatalf("steps = %+v, want the first-main pass then the attack", l.Item.Steps)
	}
	st := l.Item.Steps[1]
	if want := []string{"p0:Benalish Hero", "p0:Kjeldoran Warrior", "p0:Grizzly Bears"}; !reflect.DeepEqual(st.Attackers, want) {
		t.Fatalf("attackers = %v, want %v", st.Attackers, want)
	}
	want := []oraclegen.Answer{
		{Kind: "choose", Pick: []string{"p0:Grizzly Bears"}},
		{Kind: "choose", Pick: []string{"No more creatures in Kjeldoran Warrior's band"}},
	}
	if !reflect.DeepEqual(st.Answers, want) {
		t.Fatalf("attack answers = %#v, want %#v (accepted member first, decline second)", st.Answers, want)
	}
}
