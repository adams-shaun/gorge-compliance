package xprep

// The xproof decision_shape clusters (gorge-compliance xp-xmage-choice-defs,
// cycles 11240626..11240753): fixed-start windows whose XMage strict replay
// asked a CHOICE or TARGET the item did not script, or left a scripted answer
// unused. Each testdata window is the find's scenario (libraries shortened),
// replayed through gorge's own runner and prepared for XMage; the assertions
// pin the answers on the step whose ask XMage poses. Each also replays
// strict-clean on the ScenarioReplay driver (the PR's repro runs).

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/lower"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

func TestXProofChoiceDefs(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	sp, err := decks.LoadSpellings("../../../oracle/compliance/manifests")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		file string
		step int
		want []oraclegen.XAnswer
	}{
		// A forced X = 0 cast (no spare mana): XMage announces a spell's X
		// over 0..any, so the announce is scripted (castAnnouncesX).
		{"the-goose-mother", 2, []oraclegen.XAnswer{{Seat: 0, Kind: "choice", Value: "X=0"}}},
		// Collect evidence 4 over a graveyard short of it: XMage's
		// CollectEvidenceCost cannot be paid, so DoIfCostPaid asks nothing and
		// the decline is not scripted (unposeCollectEvidenceDeclines).
		{"surveillance-monitor", 8, nil},
		// A Dig's all-to-bottom remainder of four: XMage orders the first
		// three and places the last unasked.
		{"growing-rites", 7, []oraclegen.XAnswer{{Seat: 0, Kind: "choice", Value: "Forest"}, {Seat: 0, Kind: "choice", Value: "Forest"}, {Seat: 0, Kind: "choice", Value: "Forest"}}},
		// A look-at-three whose rest goes to the graveyard: the take is a
		// LookLibraryAndPickControllerEffect chooseTarget, the TARGET queue.
		{"sage-of-days", 7, []oraclegen.XAnswer{{Seat: 0, Kind: "target", Value: "Island"}}},
		{"polygraph-orb", 9, []oraclegen.XAnswer{{Seat: 0, Kind: "target", Value: "Swamp^Swamp"}}},
		// Two triggers from distinct sources on a step that also targets:
		// the order demotes to XMage's rule-text key, then Clement's target.
		{"clement-the-worrywort", 5, []oraclegen.XAnswer{
			{Seat: 0, Kind: "choice", Value: "Whenever {this} or another creature you control enters, return up to one target creature you control with lesser mana value to its owner's hand."},
			{Seat: 0, Kind: "target", Value: "Valley Mightcaller"},
		}},
		// Forage's exile arm: gorge exiles three graveyard cards unasked, so
		// the checkpoint names them for XMage's graveyard pick.
		{"bushy-bodyguard", 6, []oraclegen.XAnswer{{Seat: 0, Kind: "choice", Value: "yes"}, {Seat: 0, Kind: "choice", Value: "Forest^Forest^Forest"}}},
		// Endure is XMage's text-choice chooseUse: counters are "yes".
		{"dusyut-earthcarver", 10, []oraclegen.XAnswer{{Seat: 0, Kind: "choice", Value: "yes"}}},
		// The opponent's hand is empty: gorge's Optional$ confirmation has no
		// pick after it and XMage asks nothing, so only the target remains.
		{"severance-priest", 5, []oraclegen.XAnswer{{Seat: 0, Kind: "target", Value: "p1"}}},
		// A paid sacrifice unless cost: chooseUse "Pay the cost?" is yes, then
		// the cost's sacrifice group is one makeChoose over the choice queue
		// (SacrificeTargetCost.pay -> TargetImpl.choose), answered as one
		// aliased choice. The pick must not be the head of chooseUse's queue.
		{"primeval-force", 9, []oraclegen.XAnswer{
			{Seat: 0, Kind: "choice", Value: "yes"},
			{Seat: 0, Kind: "choice", Value: "@p0:Forest^@p0:Forest#2^@p0:Forest#3"},
		}},
		{"rathi-dragon", 8, []oraclegen.XAnswer{
			{Seat: 0, Kind: "choice", Value: "yes"},
			{Seat: 0, Kind: "choice", Value: "@p0:Mountain^@p0:Mountain#2"},
		}},
	} {
		t.Run(tc.file, func(t *testing.T) {
			raw, err := os.ReadFile("testdata/xproof/" + tc.file + ".json")
			if err != nil {
				t.Fatal(err)
			}
			res, err := rules.RunOracleScenarioJSON(reg, raw)
			if err != nil {
				t.Fatal(err)
			}
			if len(res.Fails) > 0 {
				t.Fatalf("gorge fails: %v", res.Fails)
			}
			// The window's ask is gorge's own: a recorded decision on the
			// step, so an empty want is a dropped answer, not a missing ask.
			recorded := false
			for _, d := range res.Decisions {
				recorded = recorded || d.Step == tc.step
			}
			if !recorded {
				t.Fatalf("gorge recorded no decision at step %d: %+v", tc.step, res.Decisions)
			}
			var sc oraclegen.Scenario
			if err := json.Unmarshal(raw, &sc); err != nil {
				t.Fatal(err)
			}
			prep, ref := Prepare(reg, sp, lower.Lowered{Item: oraclegen.Item{ID: "xs:XP:" + tc.file, Scenario: sc}}, res)
			if ref != nil {
				t.Fatalf("refused: %s: %s", ref.Reason, ref.Detail)
			}
			if got := prep.XAnswers[tc.step]; !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("xmage_answers[%d] = %+v, want %+v", tc.step, got, tc.want)
			}
		})
	}
}
