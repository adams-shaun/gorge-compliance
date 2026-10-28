package oraclediff

import (
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
)

// TestStackKeysSourcelessSpeedTrigger: the speed trigger has no source card;
// gorge, Forge and XMage name it "", "Start Your Engines!" and "Speed", and
// all three render the same. A spell named "Speed" keeps its name.
func TestStackKeysSourcelessSpeedTrigger(t *testing.T) {
	key := func(kind, src string) string {
		return stackKeys([]rules.OracleSnapStack{{Kind: kind, Source: src, Controller: 0}})
	}
	want := key("ability", "")
	if want != "[ability:@p0]" {
		t.Fatalf("empty-source key = %q", want)
	}
	for _, src := range []string{"Start Your Engines!", "Speed"} {
		if got := key("ability", src); got != want {
			t.Errorf("ability %q = %q, want %q", src, got, want)
		}
	}
	if got := key("spell", "Speed"); got != "[spell:Speed@p0]" {
		t.Errorf("spell Speed = %q", got)
	}
}
