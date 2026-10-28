package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
)

// A broad Sac filter can admit the source itself. The answer sent to XMage
// must follow the engine's actual payment pick, not additionally name the
// catalogue fixture.
func TestActivateSacAnswersUseObservedPick(t *testing.T) {
	reg := loadGenRegistry(t)
	card, ok := reg.Lookup("Kingpin's Enforcers")
	if !ok || len(card.Faces) == 0 || len(card.Faces[0].Abilities) == 0 {
		t.Fatal("precondition: Kingpin's Enforcers activation missing from corpus")
	}
	cost := card.Faces[0].Abilities[0].ParamStr(cards.PKCost)
	if !strings.Contains(cost, "Sac<1/Artifact;Creature/") {
		t.Fatalf("precondition: expected source-matching Sac filter, got %q", cost)
	}
	observed := rules.OracleDecision{
		Step: 0, Seat: 0, Kind: "choose_n",
		Picks:     []string{"Llanowar Elves", "Kingpin's Enforcers"},
		PickKinds: []string{"permanent", "sacrifice"},
	}
	answers := make([][]oraclegen.XAnswer, 1)
	addActivationCostAnswers(answers, 0, cost, "", []rules.OracleDecision{observed})
	if len(answers[0]) != 1 || answers[0][0].Kind != "choice" || answers[0][0].Value != "Kingpin's Enforcers" {
		t.Fatalf("XMage answers = %+v, want exactly the observed sacrifice", answers[0])
	}
}

// NICKNAME is the source's own name in Forge cost text, just like CARDNAME;
// the fixture path must not offer another permanent for this self-sacrifice.
func TestActivateSacNicknameHasNoFixture(t *testing.T) {
	cost := "Sac<1/NICKNAME>"
	if !sacSelf(cost) {
		t.Fatalf("precondition: %q must be recognized as self-sacrifice", cost)
	}
	if fixture, ok := sacFilterFixture(cost); ok {
		t.Fatalf("self-sacrifice unexpectedly has fixture %q", fixture)
	}
}

// The generic XAnswersForScenario path now emits a forced single-object cost
// pick as a plain-name choice (gen.go hasCostPick). For a Sac that names Room
// the cost path must supersede that plain spelling with the exact-ref alias,
// not add a second answer: XMage cannot match a Room by its plain front name.
func TestActivateSacAliasSupersedesPlainName(t *testing.T) {
	observed := rules.OracleDecision{
		Step: 0, Seat: 0, Kind: "choose_n", Options: 1, Min: 1, Max: 1,
		Picks:           []string{"Bottomless Pool"},
		PickRefs:        []string{"p0:Bottomless Pool"},
		PickRefsInexact: []bool{false},
		PickKinds:       []string{"sacrifice"},
	}
	answers := make([][]oraclegen.XAnswer, 1)
	// The generic answer already in the step, as XAnswersForScenario leaves it.
	answers[0] = []oraclegen.XAnswer{{Seat: 0, Kind: "choice", Value: "Bottomless Pool"}}
	addActivationCostAnswers(answers, 0, "Sac<1/Room>", "", []rules.OracleDecision{observed})
	if len(answers[0]) != 1 || answers[0][0].Kind != "choice" || answers[0][0].Value != "@p0:Bottomless Pool" {
		t.Fatalf("XMage answers = %+v, want exactly the exact-ref alias", answers[0])
	}
}
