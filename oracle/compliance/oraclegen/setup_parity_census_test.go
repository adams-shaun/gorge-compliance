package oraclegen

import (
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/engine/cards"
)

// TestSetupParityShapeCensus is the corpus-wide ratchet for the shapes the
// lb-setupparity fixes route: a new carrier must be triaged (does the setup
// drive, the answer routing or the offer observation serve it?) before its
// name is added here.
//
//   - transformFaces: back faces whose "as this transforms" replacement
//     (R:Event$ Transform) a setup back-face placement must NOT run
//     (engine/rules/oracle_run.go seats the card with applyingReplacement set).
//   - selfAttackGates: statics whose IsPresent$ reads that the source itself
//     attacked this turn (staticNeedsSelfAttack serves them in the offer
//     observation).
//   - retargetFaces: faces with a top-level ChangeTargets ability, whose
//     redirect ask the generator answers on the target queue
//     (OracleDecision.Retarget).
func TestSetupParityShapeCensus(t *testing.T) {
	reg := censusRegistry(t)
	var transformFaces, selfAttackGates, retargetFaces []string
	for _, c := range reg.AllCards() {
		for i, f := range c.Faces {
			if f == nil {
				continue
			}
			if i > 0 {
				for _, r := range f.Repls {
					if r.Event == "Transform" {
						transformFaces = append(transformFaces, f.Name)
						break
					}
				}
			}
			for _, st := range f.Statics {
				if strings.Contains(strings.ToLower(st.ParamStr(cards.PKIsPresent)), "card.self+attackedthisturn") {
					selfAttackGates = append(selfAttackGates, f.Name)
				}
			}
			for _, sa := range f.Abilities {
				if sa != nil && sa.API == "ChangeTargets" {
					retargetFaces = append(retargetFaces, f.Name)
					break
				}
			}
		}
	}
	slices.Sort(transformFaces)
	slices.Sort(selfAttackGates)
	slices.Sort(retargetFaces)
	for _, c := range []struct {
		what      string
		got, want []string
	}{
		{"transformFaces", transformFaces, []string{"Olag, Ludevic's Hubris", "Sephiroth, One-Winged Angel", "Shinryu, Transcendent Rival"}},
		{"selfAttackGates", selfAttackGates, []string{"The Lunar Whale"}},
		{"retargetFaces", retargetFaces, []string{"Bolt Bend", "Deflecting Swat", "Deflection", "Divert", "Emissary of Grudges",
			"Goblin Flectomancer", "Imp's Mischief", "Meddle", "Misdirection", "Quicksilver Dragon", "Rebound", "Redirect",
			"Redirect Lightning", "Reflecting Mirror", "Reroute", "Ricochet Trap", "Shunt", "Sideswipe", "Silver Wyvern",
			"Spellskite", "Swerve", "Torchling", "Wild Ricochet"}},
	} {
		if !slices.Equal(c.got, c.want) {
			t.Errorf("%s = %q, want %q (triage the new carrier before pinning it)", c.what, c.got, c.want)
		}
	}
}
