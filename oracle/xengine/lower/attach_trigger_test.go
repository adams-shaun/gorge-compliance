package lower

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
)

// A cast trigger's target ask (Drowner Initiate's "target player mills two
// cards", CR 603.3d) is posed after the cast committed, so it is not the
// spell's target: a targetless creature spell keeps Targets empty and the
// player pick stays an answer.
func TestAttachCastTriggerTargetStaysAnswer(t *testing.T) {
	trig := game.Trig{Name: "Drowner Initiate"}
	tgt := game.Entry{Kind: "target", Max: 1, Picks: []game.Pick{{Kind: "player"}}}
	ans := []oraclegen.Answer{{Kind: "target", Pick: []string{"p1"}}}

	st := oraclegen.Step{Op: "cast", Card: "p0:Sygg, River Guide"}
	attach(&st, ans, []game.Entry{{Kind: "priority", Triggers: []game.Trig{trig}}, tgt})
	if len(st.Targets) != 0 {
		t.Errorf("targetless spell's Targets = %v, want none", st.Targets)
	}
	if len(st.Answers) != 1 || !slices.Equal(st.Answers[0].Pick, []string{"p1"}) {
		t.Errorf("Answers = %+v, want the trigger's player pick", st.Answers)
	}

	// A spell with its own target keeps it in Targets; the trigger's stays an
	// answer (the spell's last ask committed the cast and the trigger went on the stack).
	own := game.Entry{Kind: "target", Max: 1, Picks: []game.Pick{{Kind: "card"}}, Triggers: []game.Trig{trig}}
	st = oraclegen.Step{Op: "cast", Card: "p0:Shock"}
	two := []oraclegen.Answer{{Kind: "target", Pick: []string{"p1:Bear"}}, {Kind: "target", Pick: []string{"p1"}}}
	attach(&st, two, []game.Entry{{Kind: "priority"}, own, tgt})
	if !slices.Equal(st.Targets, []string{"p1:Bear"}) {
		t.Errorf("own-target spell's Targets = %v, want [p1:Bear]", st.Targets)
	}
	if len(st.Answers) != 1 || !slices.Equal(st.Answers[0].Pick, []string{"p1"}) {
		t.Errorf("Answers = %+v, want the trigger pick", st.Answers)
	}
}
