package main

import (
	"slices"
	"strings"
	"testing"
)

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
	// Only the Gray Ogre refs: they must be exactly the two distinct
	// copies, so [p0:Gray Ogre, p0:Gray Ogre] (one copy twice) fails.
	var ogres []string
	for _, p := range picks {
		if strings.HasPrefix(p, "p0:Gray Ogre") {
			ogres = append(ogres, p)
		}
	}
	want := []string{"p0:Gray Ogre", "p0:Gray Ogre#2"}
	if !slices.Equal(ogres, want) {
		t.Errorf("Gray Ogre picks = %v (all picks %v), want %v", ogres, picks, want)
	}
}
