package mtgish

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/engine/cards"
)

func TestP2JParamTokens(t *testing.T) {
	for _, tc := range []struct {
		name string
		sa   cards.SA
		want []string
	}{
		{"extra turn skipping its untap step", cards.SA{API: "AddTurn", Params: map[string]string{"SkipUntap": "True"}},
			[]string{"player:skipsuntapstep"}},
		{"extra turn alone", cards.SA{API: "AddTurn", Params: map[string]string{"NumTurns": "1"}}, nil},
		{"animate removing the non-mana abilities", cards.SA{API: "Animate", Params: map[string]string{"RemoveNonManaAbilities": "True"}},
			[]string{"loseabilities"}},
		{"conjured card with counters", cards.SA{API: "MakeCard", Params: map[string]string{"WithCounter": "CHARGE", "WithCounterNum": "8"}},
			[]string{"counters:charge:8"}},
		{"conjured card without counters", cards.SA{API: "MakeCard", Params: map[string]string{"Conjure": "True"}}, nil},
	} {
		if got := p2jParamTokens(&tc.sa); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestP2JCloneExtras(t *testing.T) {
	sa := &cards.SA{API: "Clone", Params: map[string]string{"IntoPlayTapped": "True", "PumpKeywords": "Haste"}}
	if got, want := p2jCloneExtras(sa), []string{"etbtapped", "grant:haste"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if got := p2jCloneExtras(&cards.SA{API: "Clone", Params: map[string]string{}}); got != nil {
		t.Errorf("plain clone: got %v", got)
	}
}

func TestP2JEffectRepl(t *testing.T) {
	anywhere := cards.Repl{Event: "Moved", Params: map[string]string{
		"Destination": "Graveyard", "ValidCard": "Card.!token+YouOwn", "ReplaceWith": "Exile"}}
	if got := p2jEffectRepl(anywhere); !reflect.DeepEqual(got, []string{"effect"}) {
		t.Errorf("graveyard-to-exile from anywhere: got %v", got)
	}
	for name, mod := range map[string]map[string]string{
		"from the stack": {"Origin": "Stack"},
		"remembered":     {"ValidCard": "Card.IsRemembered"},
		"cast through":   {"ValidLKI": "Card.CastSa Spell.MayPlaySource"},
	} {
		p := map[string]string{}
		for k, v := range anywhere.Params {
			p[k] = v
		}
		for k, v := range mod {
			p[k] = v
		}
		if got := p2jEffectRepl(cards.Repl{Event: "Moved", Params: p}); got != nil {
			t.Errorf("%s: got %v, want none", name, got)
		}
	}
}

func TestP2JChooseNumberRefs(t *testing.T) {
	sa := &cards.SA{API: "ChooseNumber", Params: map[string]string{"GuessWrong": "A", "GuessCorrect": "B", "NotLowest": "C", "Highest": "D"}}
	got := map[string]bool{}
	for _, n := range extSVarRefs["ChooseNumber"](sa) {
		got[n] = true
	}
	for _, n := range []string{"A", "B", "C", "D"} {
		if !got[n] {
			t.Errorf("ChooseNumber does not follow %s", n)
		}
	}
}

func TestP2JMtgishMappings(t *testing.T) {
	e := &effects{}
	extAction["PayAnyAmountOfLife"](e, nil)
	if !reflect.DeepEqual(e.effects, []string{"loselife:*"}) {
		t.Errorf("PayAnyAmountOfLife: %v", e.effects)
	}
	if got := d2PlayerSynonyms["CantBlockWithCreatures"]; got != "rule:cantblock" {
		t.Errorf("CantBlockWithCreatures: %q", got)
	}
	if got := lookMove("ConjureADuplicateOfCardOntoTheBattlefield"); got != "conjure" {
		t.Errorf("lookMove conjure: %q", got)
	}
	if got := lookMove("MayPutACardOfTypeOntoTheBattlefield"); got != "move:library>battlefield" {
		t.Errorf("lookMove battlefield: %q", got)
	}
}
