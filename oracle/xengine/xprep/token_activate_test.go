package xprep

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/lower"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestPrepareTokenActivation: an activate step on a token the window created
// (pN:token:<name>) resolves to the token's definition (CR 111.1, 111.4: a
// token has the characteristics its creating effect gives it), so the item
// gets that face's XMage prefix instead of an xmage_ability_unknown refusal.
// The windows are the xproof finds xs:XP:gold-pan:cast:s2 (a Treasure's mana
// ability) and xs:XP:mu-yanling-wind-rider:cast:s2 (Crew 1 on a Vehicle),
// cut after the token step.
func TestPrepareTokenActivation(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	sp, err := decks.LoadSpellings(manifestDir)
	if err != nil {
		t.Fatal(err)
	}
	zero := 0
	act := func(ref string, answers ...oraclegen.Answer) oraclegen.Step {
		return oraclegen.Step{Op: "activate", Seat: 0, Card: ref, AbilityIndex: &zero, Answers: answers}
	}
	pass := func(seat int) oraclegen.Step { return oraclegen.Step{Op: "pass", Seat: seat} }
	lib := func(name string, n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = name
		}
		return out
	}
	cases := []struct {
		name, id, token, want string
		payment               []string
		p0                    oraclegen.Seat
		steps                 []oraclegen.Step
	}{
		{
			name: "Treasure", id: "xs:XP:gold-pan:cast:s2", token: "p0:token:Treasure Token",
			want:    "{T}, Sacrifice",
			payment: []string{"", "", "WW", "", "", "", "", "", ""},
			p0: oraclegen.Seat{
				Battlefield: []string{"Plains", "Plains", "Ornithopter"},
				Hand:        []string{"Gold Pan"},
				LibraryTop:  lib("Plains", 36),
				Life:        int32p(20),
			},
			steps: []oraclegen.Step{
				act("p0:Plains"), act("p0:Plains#2"),
				{Op: "cast", Seat: 0, Card: "p0:Gold Pan"},
				pass(0), pass(1), pass(0), pass(1),
				act("p0:token:Treasure Token", oraclegen.Answer{Kind: "choose", Pick: []string{"G"}}),
				pass(0),
			},
		},
		{
			name: "Vehicle crew", id: "xs:XP:mu-yanling-wind-rider:cast:s2", token: "p0:token:Vehicle Token",
			want:    "Crew 1",
			payment: []string{"", "", "", "", "UUUU", "", "", "", "", "", ""},
			p0: oraclegen.Seat{
				Battlefield: []string{"Island", "Island", "Island", "Island"},
				Hand:        []string{"Mu Yanling, Wind Rider"},
				LibraryTop:  lib("Island", 35),
				Life:        int32p(20),
			},
			steps: []oraclegen.Step{
				act("p0:Island"), act("p0:Island#2"), act("p0:Island#3"), act("p0:Island#4"),
				{Op: "cast", Seat: 0, Card: "p0:Mu Yanling, Wind Rider"},
				pass(0), pass(1), pass(0), pass(1),
				act("p0:token:Vehicle Token", oraclegen.Answer{Kind: "choose", Pick: []string{"p0:Mu Yanling, Wind Rider"}}),
				pass(0),
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			it := oraclegen.Item{
				ID: tc.id, Template: "xproof-start", Payment: tc.payment,
				Scenario: oraclegen.Scenario{
					Name: tc.id, Turn: 1, Why: "an activate step on a token the window created",
					Setup: map[string]oraclegen.Seat{
						"p0": tc.p0,
						"p1": {LibraryTop: lib("Plains", 40), Life: int32p(20)},
					},
					Steps: tc.steps,
				},
			}
			g, err := rules.RunOracleScenarioJSON(reg, it.Raw())
			if err != nil {
				t.Fatal(err)
			}
			if len(g.Fails) > 0 {
				t.Fatalf("gorge rejects the window: %v", g.Fails)
			}
			got, ref := Prepare(reg, sp, lower.Lowered{Item: it}, g)
			if ref != nil {
				t.Fatalf("Prepare refused: %+v", ref)
			}
			step := -1
			for i, st := range got.Steps {
				if st.Card == tc.token {
					step = i
				}
			}
			if step < 0 || step >= len(got.XAbility) {
				t.Fatalf("no XMage prefix for the %s step: %q", tc.token, got.XAbility)
			}
			t.Logf("step %d prefix %q, answers %+v", step, got.XAbility[step], got.XAnswers[step])
			if p := got.XAbility[step]; !strings.HasPrefix(p, tc.want) {
				t.Errorf("step %d prefix = %q, want one starting %q", step, p, tc.want)
			}
		})
	}
}

// TestPrefixesRefusesUnknownToken: a token ref no compiled token script names
// is still refused, with a detail that says so.
func TestPrefixesRefusesUnknownToken(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	zero := 0
	it := oraclegen.Item{ID: "xe:test:unknown-token", Scenario: oraclegen.Scenario{
		Steps: []oraclegen.Step{{Op: "activate", Seat: 0, Card: "p0:token:No Such Token Anywhere", AbilityIndex: &zero}},
	}}
	_, ref := prefixes(reg, it, rules.OracleResult{})
	if ref == nil || ref.Reason != ReasonAbilityUnknown || !strings.Contains(ref.Detail, "token") {
		t.Fatalf("refusal = %+v, want %s naming the token", ref, ReasonAbilityUnknown)
	}
}
