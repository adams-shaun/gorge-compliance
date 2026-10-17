// Modal-trigger target fixtures (wave 5, class T: Ferocification).
//
// A triggered ability whose effect is a Charm ("choose one --" over targeted
// modes) carries its targets on the MODE SVars, not on the Execute$ body, so
// AbilitySlotSpecs(trigger.Effect) is empty and triggerFixtures offers only
// the empty fixture. Since the engine applies CR 700.2a/603.3c (a mode with
// no legal target cannot be chosen, and a modal trigger with no choosable
// mode is removed from the stack: lb-modaltgt, d26effe46), that empty
// fixture leaves the trigger with no legal mode, so it never reaches the
// stack in either engine and the requirement was skipped "did not fire".
//
// charmTriggerFixtures builds the target fixtures of each targeted mode, in
// Choices$ order. triggerFires tries them only after every ordinary
// cause/fixture pass failed, so no item an earlier pass serves moves.
package templates

import (
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/cards"
)

// charmTriggerFixtures returns the non-empty target fixtures of a modal
// trigger's targeted modes, or nil when the trigger's effect is not a Charm,
// already names its own slots, or no mode targets.
func charmTriggerFixtures(reg *cards.Registry, f *cards.Face, t *cards.Trigger) []oraclegen.Fixture {
	if t == nil || t.Effect == nil || t.Effect.API != "Charm" {
		return nil
	}
	if len(oraclegen.AbilitySlotSpecs(f, t.Effect)) != 0 {
		return nil
	}
	var out []oraclegen.Fixture
	for _, choice := range strings.Split(t.Effect.ParamStr(cards.PKChoices), ",") {
		choice = strings.TrimSpace(choice)
		if choice == "" {
			continue
		}
		mode := cards.ResolveSVar(f.SVars, choice)
		slots := oraclegen.AbilitySlotSpecs(f, mode)
		if len(slots) == 0 {
			continue
		}
		for _, fx := range oraclegen.Fixtures(reg, slots) {
			if fixtureEmpty(fx) {
				continue
			}
			out = append(out, fx)
		}
	}
	return out
}

// fixtureEmpty reports whether fx places nothing on either seat and plays no
// prelude or combat: the empty fixture triggerFixtures already tried first.
func fixtureEmpty(fx oraclegen.Fixture) bool {
	empty := func(s *oraclegen.Seat) bool {
		return len(s.Battlefield) == 0 && len(s.Tapped) == 0 && len(s.Hand) == 0 && len(s.Graveyard) == 0 &&
			len(s.Exile) == 0 && len(s.Library) == 0 && len(s.LibraryTop) == 0 && len(s.Counters) == 0
	}
	return empty(fx.P0()) && empty(fx.P1()) && len(fx.Prelude()) == 0 && len(fx.CombatSteps()) == 0
}
