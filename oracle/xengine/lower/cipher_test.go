package lower

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// writItem is the xproof find xs:XP:writ-of-return:cast:s1: Writ of Return
// returns Memnite, then its cipher encode ask is answered by the last step.
func writItem(answer oraclegen.Answer) oraclegen.Item {
	grave := []string{"Swamp", "Swamp", "Swamp", "Swamp", "Swamp", "Swamp", "Swamp", "Memnite"}
	lib := make([]string, 26)
	for i := range lib {
		lib[i] = "Swamp"
	}
	steps := []oraclegen.Step{}
	life := int32(20)
	for _, c := range []string{"p0:Swamp", "p0:Swamp#2", "p0:Swamp#3", "p0:Swamp#4", "p0:Swamp#5"} {
		zero := 0
		steps = append(steps, oraclegen.Step{Op: "activate", Seat: 0, Card: c, AbilityIndex: &zero})
	}
	steps = append(steps,
		oraclegen.Step{Op: "cast", Seat: 0, Card: "p0:Writ of Return", Targets: []string{"p0:Memnite"}},
		oraclegen.Step{Op: "pass", Seat: 0},
		oraclegen.Step{Op: "pass", Seat: 1},
		oraclegen.Step{Op: "pass", Seat: 0, Answers: []oraclegen.Answer{answer}})
	return oraclegen.Item{ID: "xs:XP:writ-of-return:cast:s1", Template: SpanTemplate, Scenario: oraclegen.Scenario{
		Name: "writ", Turn: 1,
		Setup: map[string]oraclegen.Seat{
			"p0": {Battlefield: []string{"Swamp", "Swamp", "Swamp", "Swamp", "Swamp"}, Hand: []string{"Writ of Return"},
				Graveyard: grave, LibraryTop: lib, Life: &life},
			"p1": {Battlefield: []string{"Crookshank Kobolds"}, Life: &life}},
		Steps: steps}}
}

// cipherDecisions is gorge's decisions for the Writ item answered as given,
// then reshaped for Forge.
func cipherDecisions(t *testing.T, answer oraclegen.Answer) (raw, forge []rules.OracleDecision) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	g, err := rules.RunOracleScenarioJSON(reg, writItem(answer).Raw())
	if err != nil {
		t.Fatal(err)
	}
	return g.Decisions, CipherAsYesNo(g.Decisions)
}

// TestLowerCipherDeclineIsNo: gorge asks cipher's "you may" (CR 702.99a) as
// one KModes decision; Forge asks a yes/no confirm, then a creature choice. A
// decline lowers to the yes/no answered "no" alone, an encode to "yes" and
// then the creature pick.
func TestLowerCipherDeclineIsNo(t *testing.T) {
	raw, forge := cipherDecisions(t, oraclegen.Answer{Kind: "modes", Pick: []string{}})
	var ask int
	for i, d := range raw {
		if isCipherAsk(d) {
			ask = i
			if len(d.Picks) != 0 {
				t.Fatalf("decline run picked %v", d.Picks)
			}
			goto found
		}
	}
	t.Fatalf("no cipher ask in gorge's decisions: %+v", raw)
found:
	if len(forge) != len(raw) {
		t.Fatalf("decline: %d decisions, want %d: %+v", len(forge), len(raw), forge)
	}
	d := forge[ask]
	if d.Kind != "choose_n" || !reflect.DeepEqual(d.PickKinds, []string{"no"}) || !reflect.DeepEqual(d.Picks, []string{"No"}) || len(d.PickRefs) != 0 {
		t.Errorf("decline = %+v, want choose_n [no]", d)
	}

	raw, forge = cipherDecisions(t, oraclegen.Answer{Kind: "modes", Pick: []string{"p0:Memnite"}})
	for i, d := range raw {
		if !isCipherAsk(d) {
			continue
		}
		if len(forge) != len(raw)+1 {
			t.Fatalf("encode: %d decisions, want %d: %+v", len(forge), len(raw)+1, forge)
		}
		yes, pick := forge[i], forge[i+1]
		if yes.Kind != "choose_n" || !reflect.DeepEqual(yes.PickKinds, []string{"yes"}) || yes.Step != d.Step || yes.Seat != d.Seat {
			t.Errorf("encode confirm = %+v, want choose_n [yes] on step %d", yes, d.Step)
		}
		if !reflect.DeepEqual(pick, d) || len(pick.PickRefs) != 1 || pick.PickRefs[0] != "p0:Memnite" {
			t.Errorf("encode creature choice = %+v, want the original decision naming p0:Memnite", pick)
		}
		return
	}
	t.Fatalf("no cipher ask in the encode run: %+v", raw)
}
