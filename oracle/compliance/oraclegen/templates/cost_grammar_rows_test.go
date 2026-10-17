package templates

import (
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
	costvocab "github.com/adams-shaun/gorge/engine/rules/cost"
	"github.com/adams-shaun/gorge/engine/rules/pay"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// cost_grammar_rows_test.go pins the Level B rows the variable and compound
// cost grammar serves (wave-5 class C, lb5-costgrammar): Fabrication
// Foundry's group exile with an announced total, Baron Helmut Zemo's black
// mana symbol floor behind a Boast prelude, Sidisi, Regent of the Mire's
// sacrifice filter bound to its target, and Celestial Reunion's optional
// "choose a creature type and behold two of it".

// TestCostGrammarRowsServed: each row generates, plays through gorge with no
// failure, and scripts XMage the way the measured driver run agreed with.
func TestCostGrammarRowsServed(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		name, key string
		check     func(t *testing.T, it oraclegen.Item)
	}{
		{"Fabrication Foundry", "activate#0.1", func(t *testing.T, it oraclegen.Item) {
			// The exiled artifact is the scripted choice; XMage announces no
			// X (its cost elects any set reaching the target's mana value).
			st := activateStepIndex(it.Scenario.Steps)
			if !costGrammarHasAnswer(it.XAnswers, st, "choice", "Bonesplitter") || costGrammarHasAnswerPrefix(it.XAnswers, st, "X=") {
				t.Fatalf("activate answers = %+v, want the Bonesplitter choice and no X announce", it.XAnswers[st])
			}
		}},
		{"Baron Helmut Zemo", "activate#0.0", func(t *testing.T, it oraclegen.Item) {
			if !stepsHaveOp(it.Scenario.Steps, "attack") || !stepsHaveOp(it.Scenario.Steps, "pass_to") {
				t.Fatalf("no Boast prelude (attack, pass_to main2): %+v", it.Scenario.Steps)
			}
			st := activateStepIndex(it.Scenario.Steps)
			for _, a := range it.XAnswers[st] {
				if a.Kind == "target" {
					t.Fatalf("a target answer rides the group exile cost: %+v", it.XAnswers[st])
				}
			}
			res := it.XAnswers[st+1]
			if len(res) != 2 || res[0].Value != "[choice_skip]" || res[1].Value != "no" {
				t.Fatalf("declined copies answered %+v, want [choice_skip] then no", res)
			}
		}},
		{"Sidisi, Regent of the Mire", "activate#0.0", nil},
		{"Celestial Reunion", "static#0.0", func(t *testing.T, it oraclegen.Item) {
			got := make([]string, 0, len(it.XAnswers[0]))
			for _, a := range it.XAnswers[0] {
				got = append(got, a.Value)
			}
			want := "yes,X=2,Bear,Runeclaw Bear,Bear Cub"
			if strings.Join(got, ",") != want {
				t.Fatalf("cast answers %v, want %s (the type and behold picks follow X)", got, want)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := activateRowRequirement(t, reg, tc.name, tc.key)
			it, skip := GenerateB(reg, tc.name, req)
			if skip != nil {
				t.Fatalf("%s %s skipped: %s", tc.name, tc.key, skip.Reason)
			}
			res, ok := oraclegen.PlaysThrough(reg, it.Scenario)
			if !ok || len(res.Fails) != 0 {
				t.Fatalf("%s %s does not play through gorge: ok=%v fails=%v", tc.name, tc.key, ok, res.Fails)
			}
			if tc.check != nil {
				tc.check(t, it)
			}
		})
	}
}

func costGrammarHasAnswer(as [][]oraclegen.XAnswer, step int, kind, value string) bool {
	for _, a := range as[step] {
		if a.Kind == kind && a.Value == value {
			return true
		}
	}
	return false
}

func costGrammarHasAnswerPrefix(as [][]oraclegen.XAnswer, step int, prefix string) bool {
	for _, a := range as[step] {
		if strings.HasPrefix(a.Value, prefix) {
			return true
		}
	}
	return false
}

// TestGroupExileFixturesCoverCensus is the generator half of the group exile
// ratchet: every corpus cost token with an open-ended "N+" exile count has a
// groupExileShapes entry, every entry is still carried by the corpus, and
// each entry's fixture cards really satisfy the set constraint (a GE floor is
// reached by the whole set and by no smaller one, so the election is the
// whole fixture; an EQ-X shape's fixtures reach some total).
func TestGroupExileFixturesCoverCensus(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	served := map[int]bool{}
	var carriers []string
	for _, c := range reg.AllCards() {
		for _, f := range c.Faces {
			for _, sa := range f.Abilities {
				for _, tok := range costTokens(sa.ParamStr(cards.PKCost)) {
					if !isGroupExileToken(tok) {
						continue
					}
					carriers = append(carriers, f.Name)
					if _, ok := groupExileFixtures(tok); !ok {
						t.Errorf("%s: group exile token %s has no groupExileShapes entry", f.Name, tok)
						continue
					}
					part := rules.ParseCost(tok).Exile[0]
					for i, s := range groupExileShapes {
						if s.zone == part.Zone && s.spec == part.Spec && s.group == part.Group {
							served[i] = true
						}
					}
				}
			}
		}
	}
	sort.Strings(carriers)
	if len(carriers) == 0 {
		t.Fatal("no corpus card carries a group exile token: the census is vacuous")
	}
	t.Logf("group exile carriers: %v", carriers)
	for i, s := range groupExileShapes {
		if !served[i] {
			t.Errorf("groupExileShapes[%d] (%s %s) has no corpus carrier: stale entry", i, s.spec, s.group.Raw)
		}
		values := make([]int32, 0, len(s.cards))
		for _, name := range s.cards {
			c, ok := reg.Lookup(name)
			if !ok {
				t.Fatalf("fixture %s is not in the corpus", name)
			}
			f := c.Faces[0]
			switch s.group.Prop {
			case costvocab.GroupCMC:
				values = append(values, f.Cmc())
			case costvocab.GroupManaSymbols:
				values = append(values, costvocab.ManaSymbolsOfColor(rules.ParseCost(f.ManaCost), s.group.Color))
			}
		}
		switch s.group.Cmp {
		case costvocab.GroupGE:
			if !pay.GroupFeasible(values, len(values), s.group, s.group.N) {
				t.Errorf("%s fixtures %v (values %v) do not reach %d", s.group.Raw, s.cards, values, s.group.N)
			}
			if len(values) > 1 && pay.GroupFeasible(values[:len(values)-1], len(values)-1, s.group, s.group.N) {
				t.Errorf("%s fixtures %v: a smaller set already reaches %d, so the election is not the whole fixture", s.group.Raw, s.cards, s.group.N)
			}
		case costvocab.GroupEQ:
			if len(pay.GroupSums(values, 1)) == 0 {
				t.Errorf("%s fixtures %v reach no total", s.group.Raw, s.cards)
			}
		}
	}
}

// isGroupExileToken reports an Exile/ExileFromGrave token with an "N+" count.
func isGroupExileToken(tok string) bool {
	if !strings.HasPrefix(tok, "Exile<") && !strings.HasPrefix(tok, "ExileFromGrave<") {
		return false
	}
	payload, ok := bracketPayload(tok)
	if !ok {
		return false
	}
	n, _, _ := strings.Cut(payload, "/")
	return strings.HasSuffix(n, "+")
}
