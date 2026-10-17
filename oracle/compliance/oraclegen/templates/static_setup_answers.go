package templates

import (
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/rules"
)

// withStaticSetupChoices transcribes the replay's Step<0 as-enters choices
// (oraclegen.IsSetupChoice and SetupNumberChoice — the answers gorge's setup
// pipeline gave while placing the scenario's seeded permanents) into the
// served item's xmage_answers[0], in decision order (the order
// HoistSetupChoices preserves on the templates that reorder a step's stream
// afterwards). XMage poses the same dialog while its build() places the
// permanent, so the answer must lead the queue or XMage's own AI answers the
// as-enters ask and the row compares two different chosen types (Door of
// Destinies' charge counter × chosen type: gorge scripted "Bear", XMage's
// unscripted Door chose nothing).
//
// Every caller that already derives XAnswers from a replay
// (XAnswersForScenario) scripts these decisions itself — the cast path's
// base.XAnswers is exactly that derivation — so a non-empty base is returned
// unchanged. A card whose scenario records no such ask gets nil back: the
// item bytes stay identical (XAnswers is omitempty).
func withStaticSetupChoices(base [][]oraclegen.XAnswer, res rules.OracleResult) [][]oraclegen.XAnswer {
	if len(base) > 0 {
		return base
	}
	var lead []oraclegen.XAnswer
	for _, d := range res.Decisions {
		if d.Step >= 0 {
			continue
		}
		if oraclegen.IsSetupChoice(d) {
			lead = append(lead, oraclegen.XAnswer{Seat: d.Seat, Kind: "setup_choice", Value: d.Picks[0]})
			continue
		}
		if a, ok := oraclegen.SetupNumberChoice(d); ok {
			lead = append(lead, a)
		}
	}
	if len(lead) == 0 {
		return nil
	}
	return [][]oraclegen.XAnswer{lead}
}
