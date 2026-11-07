package templates_test

import (
	"strings"
	"testing"
)

// TestStaticCrewDependencyMarchOoze: March of the World Ooze's static gives
// creatures you control a base 6/6 and the Ooze type. The scenario has to
// crew a Vehicle after the static resolves, because a Vehicle is not a
// creature until crewed and the static's layer-4 addition and layer-7b P/T
// depend (CR 613.8a) on the crew effect. The frozen lines show the Vehicle
// uncrewed in permanents- and as an artifact creature ooze Vehicle 6/6 in
// permanents+.
func TestStaticCrewDependencyMarchOoze(t *testing.T) {
	it, frozen := continuousItem(t, "March of the World Ooze", "static#0.0")
	if _, ok := line(lastLines(t, frozen, "permanents-"), "c0 o0 Dune Drifter [artifact vehicle]", ""); !ok {
		t.Fatalf("permanents- lacks the uncrewed Vehicle: %v", lastLines(t, frozen, "permanents-"))
	}
	got, ok := line(lastLines(t, frozen, "permanents+"), "c0 o0 Dune Drifter [artifact creature ooze vehicle]", " 6/6")
	if !ok {
		t.Fatalf("permanents+ lacks the crewed Ooze Vehicle at 6/6: %v", lastLines(t, frozen, "permanents+"))
	}
	if strings.Contains(got, "counters=") {
		t.Fatalf("the fixture left counters on the Vehicle: %q", got)
	}
	// The crew is a real scenario step, labelled for XMage, with the tap
	// choice scripted, or the row would not replay in either follower.
	crew := false
	for i, st := range it.Steps {
		if st.Op != "activate" || st.Card != "p0:Dune Drifter" {
			continue
		}
		crew = true
		if len(it.XAbility) <= i || !strings.HasPrefix(it.XAbility[i], "Crew ") {
			t.Fatalf("activate step %d has XAbility %v, want a Crew label", i, it.XAbility)
		}
	}
	if !crew {
		t.Fatalf("scenario has no crew activation: %v", it.Steps)
	}
	if len(it.XAnswers) == 0 {
		t.Fatal("scenario has no XMage answers for the crew cost")
	}
}

// The generated item must be a level-B static item for the same key.
func TestStaticCrewDependencyItemShape(t *testing.T) {
	it, _ := continuousItem(t, "March of the World Ooze", "static#0.0")
	if it.Template != "static#0.0" || it.Card != "March of the World Ooze" {
		t.Fatalf("item %q card %q template %q is not the March static#0.0 level-B row", it.ID, it.Card, it.Template)
	}
}
