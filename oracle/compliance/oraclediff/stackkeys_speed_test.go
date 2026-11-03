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

func TestStackComparesTokensByKind(t *testing.T) {
	keys := func(src string) field {
		return field{"stack", stackKeys([]rules.OracleSnapStack{{Kind: "ability", Source: src, Controller: 0}})}
	}
	if a, b := keys("p0:token:Token"), keys("Jace Token"); !fieldEqual(a, b) {
		t.Errorf("gorge's %s, Forge's %s", a.value, b.value)
	}
	if a, b := keys("p0:token:Food Token"), keys("Food Token"); a.value != b.value {
		t.Errorf("rendered: %s, %s", a.value, b.value)
	}
	if a, b := keys("p0:Jace, Vryn's Prodigy"), keys("Jace Token"); fieldEqual(a, b) {
		t.Errorf("a card and a token: %s", a.value)
	}
}
