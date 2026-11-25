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

// TestStackTokenSourceBareName: Forge names the Empower Jace planeswalker token
// "Jace Token" and XMage "Jace" (CR 111.4); a stack ability it controls compares
// equal on both, while a card of that name still differs.
func TestStackTokenSourceBareName(t *testing.T) {
	keys := func(src string, perms ...rules.OracleSnapPerm) field {
		return field{"stack", stackKeysOf([]rules.OracleSnapStack{{Kind: "ability", Source: src, Controller: 0}}, perms)}
	}
	g := keys("p0:token:Jace Token", rules.OracleSnapPerm{Ref: "p0:token:Jace Token", Name: "Jace Token", Token: true})
	x := keys("Jace", rules.OracleSnapPerm{Ref: "Jace", Name: "Jace", Token: true})
	if !fieldEqual(g, x) {
		t.Errorf("gorge %s, xmage %s", g.value, x.value)
	}
	card := keys("p0:Jace, Vryn's Prodigy", rules.OracleSnapPerm{Ref: "p0:Jace, Vryn's Prodigy", Name: "Jace, Vryn's Prodigy"})
	if fieldEqual(card, x) {
		t.Errorf("a card and a token: %s, %s", card.value, x.value)
	}
	both := keys("Jace", rules.OracleSnapPerm{Ref: "Jace", Name: "Jace", Token: true}, rules.OracleSnapPerm{Ref: "p1:Jace", Name: "Jace"})
	if fieldEqual(both, g) {
		t.Errorf("a name held by a card too stays a card: %s", both.value)
	}
}
