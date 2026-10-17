package templates

import (
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/rules"
	costvocab "github.com/adams-shaun/gorge/engine/rules/cost"
	"github.com/adams-shaun/gorge/engine/state"
)

// activate_group_exile.go serves the open-ended group exile costs
// (engine/rules/cost CostPart.AtLeast with a withTotal* Group): Fabrication
// Foundry's Exile<1+/Artifact.Other+withTotalCMCEQX>, Baron Helmut Zemo's
// ExileFromGrave<1+/Card.Black+withTotalManaSymbols_Black_GE15> and The
// Capitoline Triad's ExileFromGrave<1+/Card.Historic+withTotalCMCGE30>. Each
// shape names the catalogue cards the fixture places in the part's zone and
// the scenario exiles (XMage's cost selector is answered with the same
// cards). TestGroupExileFixturesCoverCensus holds the table to the corpus.

// groupExileShape is one served group-exile cost shape.
type groupExileShape struct {
	zone  state.Zone
	spec  string
	group costvocab.GroupTotal
	cards []string
}

// groupExileShapes are the served shapes, keyed by the parsed part.
var groupExileShapes = []groupExileShape{
	{
		// "Exile one or more other artifacts you control with total mana
		// value X": one 1-mana artifact, so the only announcement is X = 1.
		zone: state.ZBattlefield, spec: "Artifact.Other",
		group: costvocab.GroupTotal{Prop: costvocab.GroupCMC, Cmp: costvocab.GroupEQ, Var: "X", Raw: "withTotalCMCEQX"},
		cards: []string{"Bonesplitter"},
	},
	{
		// "black cards ... with fifteen or more black mana symbols among
		// their mana costs": 4 + 4 + 5 + 4 = 17 black symbols, and no
		// three of them reach fifteen, so the whole set is the payment.
		zone: state.ZGraveyard, spec: "Card.Black",
		group: costvocab.GroupTotal{Prop: costvocab.GroupManaSymbols, Color: 'B', Cmp: costvocab.GroupGE, N: 15, Raw: "withTotalManaSymbols_Black_GE15"},
		cards: []string{"Phyrexian Obliterator", "Nefarious Lich", "Pox Plague", "Lich"},
	},
	{
		// "historic cards ... with total mana value 30 or greater":
		// 12 + 11 + 7 = 30.
		zone: state.ZGraveyard, spec: "Card.Historic",
		group: costvocab.GroupTotal{Prop: costvocab.GroupCMC, Cmp: costvocab.GroupGE, N: 30, Raw: "withTotalCMCGE30"},
		cards: []string{"Blightsteel Colossus", "Darksteel Colossus", "Myr Battlesphere"},
	},
}

// groupExileFixtures returns the catalogue cards a group exile token is paid
// with, ok=false for a token that is not an AtLeast group part or names a
// shape the table does not serve.
func groupExileFixtures(tok string) ([]string, bool) {
	c := rules.ParseCost(tok)
	if len(c.Exile) != 1 || len(c.Unknown) != 0 || !c.Exile[0].AtLeast {
		return nil, false
	}
	part := c.Exile[0]
	for _, s := range groupExileShapes {
		if s.zone == part.Zone && s.spec == part.Spec && s.group == part.Group {
			return s.cards, true
		}
	}
	return nil, false
}

// boastPrelude is the Boast activation restriction's setup (CR 702.142a:
// "activate only if this creature attacked this turn"): the source attacks
// p1, and the scenario moves on to the second main phase.
func boastPrelude(name string) conditionPrelude {
	return conditionPrelude{steps: []oraclegen.Step{
		{Op: "attack", Seat: 0, Defender: "p1", Attackers: []string{"p0:" + name}},
		{Op: "pass_to", Step: "main2"},
	}}
}

// groupExilePaid reports whether a cost carrying a group exile part was paid
// at the activate step: the gorge run recorded an exilecost pick there. A
// cost without such a part is always reported paid.
func groupExilePaid(cost string, step int, decisions []rules.OracleDecision) bool {
	if !costHasGroupExile(cost) {
		return true
	}
	for _, d := range decisions {
		if d.Step != step || d.Seat != 0 {
			continue
		}
		for i := range d.PickKinds {
			if d.PickKinds[i] == "exilecost" {
				return true
			}
		}
	}
	return false
}

// dropGroupExileTargets removes the target-queue answers the generic routing
// records for a group exile cost's observed picks (xmQueue routes an
// exilecost pick to the target queue): XMage's group exile costs elect with
// target.choose, which TestPlayer serves from the CHOICE queue, and the
// catalogue choice answers addActivationCostAnswers scripts are that
// election. A leftover target answer is consumed by a later dialog instead
// (measured: Baron Helmut Zemo's copies were cast from the stale target
// queue).
func dropGroupExileTargets(answers [][]oraclegen.XAnswer, step int, cost string, decisions []rules.OracleDecision) {
	if step < 0 || step >= len(answers) || !costHasGroupExile(cost) {
		return
	}
	picked := map[string]bool{}
	for _, d := range decisions {
		if d.Step != step || d.Seat != 0 {
			continue
		}
		for i, k := range d.PickKinds {
			if k == "exilecost" && i < len(d.Picks) {
				picked[strings.ToLower(d.Picks[i])] = true
			}
		}
	}
	kept := answers[step][:0]
	for _, a := range answers[step] {
		if a.Seat == 0 && a.Kind == "target" && picked[strings.ToLower(a.Value)] {
			continue
		}
		kept = append(kept, a)
	}
	answers[step] = kept
}

// costHasGroupExile reports whether cost carries a served group exile token.
func costHasGroupExile(cost string) bool {
	for _, tok := range costTokens(cost) {
		if _, ok := groupExileFixtures(tok); ok {
			return true
		}
	}
	return false
}
