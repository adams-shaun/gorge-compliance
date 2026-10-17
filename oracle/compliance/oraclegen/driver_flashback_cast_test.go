package oraclegen_test

import (
	"os"
	"strings"
	"testing"
)

// TestDriverFlashbackCast pins the driver's cast_mode "flashback" wiring:
// XMage offers a graveyard flashback as its own SpellAbility ("Flashback
// <cost>"), so the cast arm must pay the step's mana, then activate that
// ability by its alias-scoped rule prefix instead of castSpell-ing the card
// by name, and castModeSupported must admit it.
func TestDriverFlashbackCast(t *testing.T) {
	b, err := os.ReadFile(scenarioReplayPath)
	if err != nil {
		t.Fatal(err)
	}
	java := string(b)
	cast, ok := caseBody(java, "cast")
	if !ok {
		t.Fatal("ScenarioReplay.java has no cast case")
	}
	manaAt := strings.Index(cast, `addPool(pl, g, mana)`)
	fbAt := strings.Index(cast, `if ("flashback".equals(castMode))`)
	castAt := strings.Index(cast, `castSpell(turn, phase, p, card)`)
	if manaAt < 0 || castAt < 0 {
		t.Fatalf("cast arm lost its mana or castSpell path; the assertions below would be vacuous:\n%s", cast)
	}
	if fbAt < 0 {
		t.Fatal(`cast arm has no "flashback" branch`)
	}
	if !(manaAt < fbAt && fbAt < castAt) {
		t.Errorf("flashback branch must follow the mana add (%d) and precede the ordinary cast (%d); it is at %d", manaAt, castAt, fbAt)
	}
	end := strings.Index(cast[fbAt:], "return;")
	if end < 0 {
		t.Fatal(`the "flashback" branch does not return`)
	}
	branch := cast[fbAt : fbAt+end]
	if !strings.Contains(branch, "flashbackCommand(") || !strings.Contains(branch, "activateAbility(turn, phase, p, fbText)") {
		t.Errorf("flashback branch must activate the alias-scoped Flashback ability:\n%s", branch)
	}
	if strings.Contains(branch, "castSpell") {
		t.Errorf("flashback is activated by ability text, not castSpell by name:\n%s", branch)
	}
	i := strings.Index(java, "static boolean castModeSupported(String mode)")
	if i < 0 {
		t.Fatal("castModeSupported not found")
	}
	helper := java[i : i+strings.Index(java[i:], "}\n")]
	if !strings.Contains(helper, `mode.equals("flashback")`) {
		t.Error(`castModeSupported does not admit "flashback"`)
	}
}
