// Focused test for Angelic Sell-Sword's self power-floor attack trigger
// (IsPresent$ Card.Self+powerGE6). The trigger-condition path must pass the
// card's own face to activationPresentPrelude so the source is raised with
// +1/+1 counters to meet the floor, rather than dereferencing a nil face.
package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/engine/cards"
)

func TestTriggerSelfPowerFloorAngelicSellSword(t *testing.T) {
	reg := loadGenRegistry(t)
	const name = "Angelic Sell-Sword"
	card, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("precondition: %s not in the corpus", name)
	}
	if len(card.Faces[0].Triggers) < 2 {
		t.Fatalf("precondition: %s has %d triggers, want the Attacks trigger at index 1", name, len(card.Faces[0].Triggers))
	}
	// Precondition: the served trigger really is the self power floor; without
	// it the assertion below would pass on an unrelated trigger.
	gate := card.Faces[0].Triggers[1].ParamStr(cards.PKIsPresent)
	if !strings.Contains(strings.ToLower(gate), "self") || !strings.Contains(strings.ToLower(gate), "powerge6") {
		t.Fatalf("precondition: %s trigger[1] IsPresent$ = %q, want Card.Self+powerGE6", name, gate)
	}
	it := triggerItem(t, reg, name, "trigger#0.1")
	// The 4/4 source is raised by two +1/+1 counters to reach power 6.
	if got := it.Scenario.Setup["p0"].Counters[name]["P1P1"]; got != 2 {
		t.Fatalf("%s: setup counters %v, want 2 P1P1 on the source", name, it.Scenario.Setup["p0"].Counters)
	}
	atk := attackStep(t, it.Scenario.Steps).Attackers
	if len(atk) != 1 || atk[0] != "p0:"+name {
		t.Fatalf("%s: attackers = %v, want the source attacking", name, atk)
	}
}
