package templates

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
)

// chooseTypeBeholdFixtures are the two same-type creatures a
// "choose a creature type and behold N creatures of that type" cost beholds
// (Celestial Reunion's ChooseCreatureType<1> Behold<2/Creature.ChosenType>).
// Bears, so the level-A cast's library search (which finds Grizzly Bears)
// reveals a card of the chosen type and the paid branch puts it onto the
// battlefield instead of into the hand.
var chooseTypeBeholdFixtures = []string{"Runeclaw Bear", "Bear Cub"}

// chooseTypeBeholdType is the creature type the fixtures share, the answer to
// both engines' type choice.
const chooseTypeBeholdType = "Bear"

// chooseTypeBeholdSetup is the setup of the compound optional cost
// "ChooseCreatureType<1> Behold<N/Creature.ChosenType>": the fixture creatures
// on p0's battlefield, and XMage's picks after the "pay it?" yes -- the
// creature type, then the beheld permanents. gorge poses the type choice
// with the one payable type, and beholds exactly the N fixtures, so neither
// is a scripted gorge answer.
func chooseTypeBeholdSetup(toks []string) (optionalCostSetup, bool) {
	if len(toks) != 2 || toks[0] != "ChooseCreatureType<1>" {
		return optionalCostSetup{}, false
	}
	payload, ok := bracketPayload(toks[1])
	if !ok || !strings.HasPrefix(toks[1], "Behold<") {
		return optionalCostSetup{}, false
	}
	n, spec, _ := strings.Cut(payload, "/")
	spec, _, _ = strings.Cut(spec, "/")
	count, err := strconv.Atoi(n)
	if err != nil || count != len(chooseTypeBeholdFixtures) || spec != "Creature.ChosenType" {
		return optionalCostSetup{}, false
	}
	picks := append([]string{chooseTypeBeholdType}, chooseTypeBeholdFixtures...)
	return optionalCostSetup{
		fixture: func(fx *oraclegen.Fixture) {
			for _, c := range chooseTypeBeholdFixtures {
				fx.P0().Battlefield = appendFixtureUnique(fx.P0().Battlefield, c)
			}
		},
		picks:       picks,
		picksAfterX: true,
	}, true
}

// afterXAnnouncement inserts picks after the last "X=<n>" announcement in a
// step's XMage answers (at the front when there is none).
func afterXAnnouncement(answers, picks []oraclegen.XAnswer) []oraclegen.XAnswer {
	at := 0
	for i, a := range answers {
		if a.Kind == "choice" && strings.HasPrefix(a.Value, "X=") {
			at = i + 1
		}
	}
	out := make([]oraclegen.XAnswer, 0, len(answers)+len(picks))
	out = append(out, answers[:at]...)
	out = append(out, picks...)
	return append(out, answers[at:]...)
}
