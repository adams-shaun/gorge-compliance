package templates

import "testing"

// TestPlainCreaturesYouControl: the crew-dependency fixture must claim only
// the plain "creatures you control" filter. A counter gate or a non-creature
// type changes which objects the static affects, so the crew dependency is
// not the observation the row needs and the fixture must leave those rows to
// their existing candidates.
func TestPlainCreaturesYouControl(t *testing.T) {
	cases := []struct {
		affected string
		want     bool
	}{
		{"Creature.YouCtrl", true},
		{"creature.youctrl", true},
		{"Creature.YouCtrl+counters_GE1_P1P1", false},
		{"Artifact.nonCreature+YouCtrl", false},
		{"Creature.YouCtrl+attacking", false},
		{"Creature", false},
		{"Creature.YouCtrl,Creature.YouDontCtrl", false},
	}
	for _, c := range cases {
		if got := plainCreaturesYouControl(c.affected); got != c.want {
			t.Errorf("plainCreaturesYouControl(%q) = %v, want %v", c.affected, got, c.want)
		}
	}
}
