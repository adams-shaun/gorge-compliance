package oraclediff

import (
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
)

// TestStackKeysSourcelessDesignationTrigger: the monarch's (CR 725.2)
// inherent triggered abilities have no source card. gorge names them "",
// Forge and XMage name them after the designation, and all three render the
// same. A spell with the same name keeps its name.
func TestStackKeysSourcelessDesignationTrigger(t *testing.T) {
	key := func(kind, src string) string {
		return stackKeys([]rules.OracleSnapStack{{Kind: kind, Source: src, Controller: 0}})
	}
	want := key("ability", "")
	if want != "[ability:@p0]" {
		t.Fatalf("empty-source key = %q", want)
	}
	if got := key("ability", "The Monarch"); got != want {
		t.Errorf("ability %q = %q, want %q", "The Monarch", got, want)
	}
	if got := key("spell", "The Monarch"); got != "[spell:The Monarch@p0]" {
		t.Errorf("spell %q = %q, want its own name", "The Monarch", got)
	}
}

// TestStackInitiativeTriggerIsSourceless: the CR 726.2 initiative trigger
// ("whenever a player takes the initiative, that player ventures into the
// Undercity") has no source card. gorge names it "", Forge and XMage name it
// "The Initiative", and the stacks compare equal.
func TestStackInitiativeTriggerIsSourceless(t *testing.T) {
	keys := func(src string) field {
		return field{"stack", stackKeys([]rules.OracleSnapStack{{Kind: "ability", Source: src, Controller: 0}})}
	}
	g, x := keys(""), keys("The Initiative")
	if !fieldEqual(g, x) {
		t.Errorf("gorge's %s, follower's %s", g.value, x.value)
	}
	if g.value != "[ability:@p0]" || x.value != "[ability:@p0]" {
		t.Errorf("rendered: %q, %q", g.value, x.value)
	}
	if key := stackKeys([]rules.OracleSnapStack{{Kind: "spell", Source: "The Initiative", Controller: 0}}); key != "[spell:The Initiative@p0]" {
		t.Errorf("spell The Initiative = %q, want its own name", key)
	}
}
