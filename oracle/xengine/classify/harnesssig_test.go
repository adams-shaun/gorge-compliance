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
