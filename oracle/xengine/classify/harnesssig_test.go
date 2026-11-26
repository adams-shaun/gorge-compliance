package classify

import "testing"

func TestHarnessSignature(t *testing.T) {
	for _, c := range []struct {
		name string
		x, f Finding
		want string
	}{
		{"no findings", Finding{}, Finding{}, "x=none+f=none"},
		{"class only", Finding{ID: "a", Class: ClassNotSent}, Finding{ID: "a", Class: ClassAgree}, "x=not_sent+f=agree"},
		{"digits and names drop", Finding{ID: "a", Class: ClassDecisionShape, Msg: `strict_miss: Missing discard unless type answer at step 7: 2 options, 1..2 to pick leftover:`},
			Finding{ID: "a", Class: ClassHarness, Msg: `forge: cannot take "Aang, Swift Savior" at step 12`},
			"x=decision_shape~missing-discard-unless-type-answer-at+f=harness~cannot-take-at-step"},
		{"same cause, other numbers", Finding{ID: "b", Class: ClassDecisionShape, Msg: `strict_miss: Missing discard unless type answer at step 9: 3 options, 1..3 to pick leftover:`},
			Finding{ID: "b", Class: ClassHarness, Msg: `forge: cannot take "Archive Dragon" at step 3`},
			"x=decision_shape~missing-discard-unless-type-answer-at+f=harness~cannot-take-at-step"},
	} {
		if got := HarnessSignature(c.x, c.f); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

// XMage's real messages are "<engine>: AssertionError: <cause>": the slug must
// name the cause, so distinct causes give distinct signatures.
func TestHarnessSignatureXMageCauses(t *testing.T) {
	msgs := []string{
		"xmage: AssertionError: Found wrong choice command (invalid target or miss skip command):\nYes\nTarget: selected 0, more possible 1",
		"xmage: AssertionError: Can't find ability to activate command: @act1 waterbend {X}",
		"strict_miss: AssertionError: Missing TARGET def for turn 3, step PRECOMBAT_MAIN, PlayerA\nAbility: Sage of Days [c1] - EntersBattlefieldTriggeredAbility",
		"strict_miss: AssertionError: Missing CHOICE def for turn 3, step PRECOMBAT_MAIN, PlayerA\nAbility: Bushy Bodyguard [4] - EntersBattlefieldTriggeredAbility",
		"strict_miss:  leftover: AssertionError: (Choices of PlayerA) Count are not equal (found [Yes]). Make sure you use target.chooseXXX",
	}
	want := []string{
		"decision_shape~found-wrong-choice-command-invalid-target",
		"decision_shape~cant-find-ability-to-activate-command",
		"decision_shape~missing-target-def-for-turn-step",
		"decision_shape~missing-choice-def-for-turn-step",
		"decision_shape~leftover-choices-of-playera-count-are",
	}
	seen := map[string]string{}
	for i, m := range msgs {
		got := engineSig(Finding{ID: "i", Class: ClassDecisionShape, Msg: m})
		if got != want[i] {
			t.Errorf("%q: %q, want %q", m, got, want[i])
		}
		if prev, dup := seen[got]; dup {
			t.Errorf("%q and %q share signature %q", prev, m, got)
		}
		seen[got] = m
	}
	// the same cause on another card and step is one signature
	a := engineSig(Finding{ID: "a", Class: ClassDecisionShape, Msg: msgs[2]})
	b := engineSig(Finding{ID: "b", Class: ClassDecisionShape, Msg: "strict_miss: AssertionError: Missing TARGET def for turn 9, step PRECOMBAT_MAIN, PlayerB\nAbility: Polygraph Orb [N]"})
	if a != b {
		t.Errorf("same cause, other card: %q != %q", a, b)
	}
}
