package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestSupportTargetTimingGeneratedShape pins the Support retime on the shape
// the level-A cast-resolve template emits: {cast}, {resolve}. The enters
// trigger is announced and resolves inside the one resolve step, so no
// checkpoint holds it and the support pick (which gorge asks at resolution as
// a counter_pick) must stay on that step as TARGET answers, never as a
// "choice" XMage would leave unconsumed ("Missing TARGET def").
func TestSupportTargetTimingGeneratedShape(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, name := range []string{"Aerie Auxiliary", "Captured by Lagacs", "Expedition Raptor", "Relief Captain",
		"Together Forever", "Gladehart Cavalry", "Generous Patron", "Saddleback Lagac",
		"Skyboon Evangelist", "Soulblade Renewer", "Blitzball Stadium"} {
		t.Run(name, func(t *testing.T) {
			item, skip := Generate(reg, name)
			if skip != nil {
				t.Fatalf("%s skipped: %+v", name, skip)
			}
			steps := item.Scenario.Steps
			if len(steps) != 2 || steps[0].Op != "cast" || steps[1].Op != "resolve" {
				t.Fatalf("unexpected scenario steps: %+v", steps)
			}
			if len(item.XAnswers) != len(steps) {
				t.Fatalf("xmage answers = %+v, want one entry per step", item.XAnswers)
			}
			got := item.XAnswers[1]
			if len(got) == 0 {
				t.Fatalf("resolve step scripts no support target: %+v", item.XAnswers)
			}
			// Other decisions (a trigger-order choice, a may) can share the step;
			// the support targets lead it, and no choice repeats a pick.
			first := got[0]
			if first.Kind != "target" || first.Value == "" || first.Value == "[target_skip]" {
				t.Fatalf("first resolve-step answer %+v is not a support target (all: %+v)", first, got)
			}
			for _, a := range got {
				if a.Kind == "choice" && a.Value == first.Value {
					t.Fatalf("support pick %q also scripted as a choice: %+v", first.Value, got)
				}
			}
			for _, a := range item.XAnswers[0] {
				if a.Kind == "target" {
					t.Fatalf("cast step scripts target %+v before the trigger", a)
				}
			}
		})
	}
}
