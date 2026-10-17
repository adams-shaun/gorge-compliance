package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/levelb"
)

// TestRoomDoorTriggersCastTheirDoor is the corpus census of the Room-door
// mechanism: setup places a Room with both doors locked (CR 709.5), so any
// trigger on either door fires only after the door is cast. Every servable
// trigger requirement of every Room in the corpus must therefore either
// generate a scenario that casts a door from hand, or skip for a reason other
// than "did not fire" (which is exactly what a locked-door placement yields).
func TestRoomDoorTriggersCastTheirDoor(t *testing.T) {
	reg := oracleHarnessCorpus(t)
	rooms, served := 0, 0
	for _, c := range reg.AllCards() {
		if !levelb.IsRoomCard(c) {
			continue
		}
		rooms++
		name := c.Faces[0].Name + " // " + c.Faces[1].Name
		for _, req := range levelb.Requirements(c) {
			if req.Family != "trigger" || req.Gap != "" || req.CoveredByA {
				continue
			}
			item, skip := GenerateB(reg, name, req)
			if skip != nil {
				if strings.Contains(skip.Reason, "did not fire") {
					t.Errorf("%s %s: %s (locked-door placement; the door must be cast)", name, req.Key, skip.Reason)
				}
				continue
			}
			served++
			p0 := item.Scenario.Setup["p0"]
			if containsString(p0.Battlefield, name) {
				t.Errorf("%s %s: the Room is placed on the battlefield with both doors locked", name, req.Key)
			}
			cast := false
			for _, st := range item.Steps {
				cast = cast || (st.Op == "cast" && strings.HasPrefix(st.Card, "p0:"))
			}
			if !cast {
				t.Errorf("%s %s: no door cast step in %+v", name, req.Key, item.Steps)
			}
		}
	}
	// Preconditions: the census saw the DSK Rooms and served the five named
	// by the ticket (front-door triggers on a locked placement).
	if rooms < 30 || served < 30 {
		t.Fatalf("census vacuous: %d Rooms, %d served trigger requirements", rooms, served)
	}
	for _, card := range []string{
		"Charred Foyer // Warm Welcome", "Defiled Crypt // Cadaver Lab",
		"Dollmaker's Shop // Porcelain Gallery", "Funeral Room // Awakening Hall",
		"Smoky Lounge // Misty Salon",
	} {
		item, skip := GenerateB(reg, card, requirement(t, card, "trigger#0.0"))
		if skip != nil {
			t.Errorf("%s trigger#0.0 skipped: %s", card, skip.Reason)
			continue
		}
		if item.Scenario.Setup["p0"].Battlefield != nil && containsString(item.Scenario.Setup["p0"].Battlefield, card) {
			t.Errorf("%s placed locked", card)
		}
	}
}
