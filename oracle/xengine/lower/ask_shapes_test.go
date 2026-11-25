package lower

import (
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge/engine/state"
)

func ask(kind string, top state.ObjID, picks ...game.Pick) game.Entry {
	return game.Entry{Kind: kind, StackTop: top, Picks: picks}
}

// A pass whose resolution asks and then orders triggers is settled in two
// passes by the runner, which would leave the first ask's answers unconsumed
// on the next step: the answers stay on the pass.
func TestSettlesTwice(t *testing.T) {
	pass := game.Entry{Kind: "priority", StackTop: 6, Picks: []game.Pick{{Kind: "pass"}}}
	choose := ask("choose", 6, game.Pick{Kind: "card"})
	order := ask("trigger_order", 0)
	if !settlesTwice([]game.Entry{pass, choose, order}) {
		t.Fatal("a resolution ask then a trigger order must settle twice")
	}
	if settlesTwice([]game.Entry{pass, choose}) || settlesTwice([]game.Entry{pass, order}) {
		t.Fatal("one settling pass is the borrowed-answers path as before")
	}
}

// A mana ability's colour pick is answered on the explicit tap step, never
// left for the decision after it; the pick of a resolving effect (no mana
// record) keeps its answer.
func TestManaAbilityColourAsk(t *testing.T) {
	rec := game.ManaRec{Obj: 78, Ability: 0}
	act := game.Entry{Kind: "priority", Mana: []game.ManaRec{rec},
		Picks: []game.Pick{{Kind: "activate", Obj: 78, Label: "Activate Thornwood Falls for mana"}}}
	col := ask("choose", 9, game.Pick{Kind: "mana", Obj: 78, Label: "Add U"})
	g := colourProduced([]game.Entry{act, col})
	if g[0].Mana[0].Produced != "U" || act.Mana[0].Produced != "" {
		t.Fatalf("produced = %q (input %q), want U on a copy", g[0].Mana[0].Produced, act.Mana[0].Produced)
	}
	ans, why := answersOf(seatMap{}, g)
	if why != nil || len(ans) != 0 {
		t.Fatalf("answers = %v, %v: the colour pick belongs to the tap", ans, why)
	}
	eff := ask("choose", 9, game.Pick{Kind: "mana", Obj: 5, Label: "Add U"})
	if ans, _ := answersOf(seatMap{}, []game.Entry{act, eff}); len(ans) != 1 {
		t.Fatalf("a mana effect's pick was dropped: %v", ans)
	}
	// A cost prefix names the activation's cost in front of the same colour
	// wheel ("Pay 1 life: Add G"), so the label is still the colour ask; an
	// exact bare "Add G" match would drop every costed carrier.
	pref := ask("choose", 9, game.Pick{Kind: "mana", Obj: 78, Label: "Pay 1 life: Add G"})
	if pg := colourProduced([]game.Entry{act, pref}); pg[0].Mana[0].Produced != "G" {
		t.Fatalf("prefixed produced = %q, want G", pg[0].Mana[0].Produced)
	}
}
