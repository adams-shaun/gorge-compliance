package main

import "testing"

// TestOptionalCostCastUsesCastMode: XMage answers an OptionalCost static
// (Crimestopper Sprite's collect evidence) with setChoice(true) on the cast;
// gorge offers it as the "optionalcost" cast option, so the translated cast
// step carries cast_mode and no yes/no answer. setChoice(player, name, 2)
// names two distinct cards (p0:Gray Ogre, p0:Gray Ogre#2).
func TestOptionalCostCastUsesCastMode(t *testing.T) {
	res := translateTapped(t, `addCard(Zone.BATTLEFIELD, playerA, "Island", 3);
        addCard(Zone.HAND, playerA, "Crimestopper Sprite");
        addCard(Zone.GRAVEYARD, playerA, "Gray Ogre", 2);
        castSpell(1, PhaseStep.PRECOMBAT_MAIN, playerA, "Crimestopper Sprite");
        setChoice(playerA, true);
        setChoice(playerA, "Gray Ogre", 2);
        addTarget(playerA, "Crimestopper Sprite");
        assertTapped("Crimestopper Sprite", true);
        assertCounterCount("Crimestopper Sprite", CounterType.STUN, 1);`)
	if res.Status == "refused" {
		t.Fatalf("refused: %s %s", res.Code, res.Detail)
	}
	var cast *step
	var picks []string
	for _, st := range res.Scenario.Steps {
		if st.Op == "cast" && cast == nil {
			cast = st
		}
	}
	for _, g := range res.Groups {
		for _, a := range g.answers {
			picks = append(picks, a.Pick...)
		}
	}
	if cast == nil {
		t.Fatal("no cast step")
	}
	if cast.CastMode != "optionalcost" {
		t.Errorf("cast_mode = %q, want optionalcost", cast.CastMode)
	}
	for _, p := range picks {
		if p == "yes" || p == "no" {
			t.Errorf("yes/no answer left in %v", picks)
		}
	}
	want := []string{"p0:Gray Ogre", "p0:Gray Ogre#2"}
	n := 0
	for _, p := range picks {
		if p == want[0] || p == want[1] {
			n++
		}
	}
	if n != 2 {
		t.Errorf("picks = %v, want both %v", picks, want)
	}
}
