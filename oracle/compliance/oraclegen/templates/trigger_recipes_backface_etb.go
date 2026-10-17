// A back face's own "when CARDNAME enters" trigger (trigger.etb-other with a
// Card.Self filter on a face after 0). Setup can stand the card on its back
// face, but that placement is not an entry: the trigger fires only when the
// back face ENTERS, and no transform does that (CR 712.18: transforming does
// not move the object). The one way a transforming double-faced card enters
// on its back face in the corpus is Craft, whose resolution returns the card
// transformed from exile. So the cause is the front face's own Craft
// activation (craftPrelude, shared with the activate and static templates)
// with the front face standing up and the materials in the graveyard.
//
// Every other back-face self-ETB (a card that is returned transformed by a
// spell, disturb, meld) has no cause here and returns the NAMED skip, never
// the generic "did not fire"; TestBackFaceSelfETBCensus pins the corpus split.
package templates

import (
	"slices"
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
)

// backFaceSelfETB reports whether t is f's own "when this enters" trigger on a
// face after 0 of a card, and that face's index.
func backFaceSelfETB(c *cards.Card, f *cards.Face, t *cards.Trigger) (face int, ok bool) {
	if t.ModeKind() != cards.TriggerChangesZone ||
		!strings.EqualFold(t.ParamStr(cards.PKDestination), "Battlefield") ||
		!namesSelfFold(t.ParamStr(cards.PKValidCard)) {
		return 0, false
	}
	for i, face := range c.Faces {
		if i > 0 && face == f {
			return i, true
		}
	}
	return 0, false
}

// backFaceEntersRecipe builds the craft cause for a back face's self-ETB.
// handled is false when the trigger is not a back-face self-ETB at all.
func backFaceEntersRecipe(reg *cards.Registry, f *cards.Face, name string, t *cards.Trigger) (causes []triggerCause, why string, handled bool) {
	c, found := reg.Lookup(name)
	if !found {
		return nil, "", false
	}
	if _, ok := backFaceSelfETB(c, f, t); !ok {
		return nil, "", false
	}
	if len(c.Faces) < 2 || craftAbilityIndex(c.Faces[0]) < 0 {
		return nil, "back-face enters trigger: the card only enters on its back face by a return-transformed or disturb effect the generator has no cause for (no Craft)", true
	}
	steps, xab, materials, cost, ok := craftPrelude(c.Faces[0], name)
	if !ok {
		return nil, "back-face enters trigger: craft cost has no fixture the generator can place", true
	}
	// The cause is the activation alone: the probe's own passes resolve the
	// Craft ability, and the entering back face's trigger is then on the stack.
	steps[0].Answers = activationXAnswers(cost)
	return []triggerCause{{
		selfFrontUp:  true,
		battlefield:  backFaceETBTargetFixture(t, materials),
		graveyard:    materials,
		steps:        steps[:1],
		xability:     xab[:1],
		activateCost: cost,
	}}, "", true
}

// backFaceETBTargetFixture is the p0 creature a back-face ETB that targets a
// creature you control needs (Dinosaur Headdress attaches to one): the Craft
// materials are exiled, so without it the trigger has no legal target and is
// removed from the stack. The pick is never one of the materials, so the
// Craft cost's exile choice is unchanged. nil for any other trigger.
func backFaceETBTargetFixture(t *cards.Trigger, materials []string) []string {
	if t.Effect == nil {
		return nil
	}
	tgts := t.Effect.ParamStr(cards.PKValidTgts)
	if !strings.Contains(tgts, "Creature") || !strings.Contains(tgts, "YouCtrl") {
		return nil
	}
	for _, c := range []string{"Grizzly Bears", "Llanowar Elves", "Elvish Mystic"} {
		if !slices.Contains(materials, c) {
			return []string{c}
		}
	}
	return nil
}
