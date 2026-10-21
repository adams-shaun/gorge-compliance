package mtgish

import (
	"testing"

	"github.com/adams-shaun/gorge/engine/cards"
)

func TestP2IMtgishActions(t *testing.T) {
	for _, tc := range []struct {
		name, src, want string
	}{
		{"look, pay life or put in graveyard", `[{"_Action":"LookAtTheTopNumberCardsOfLibrary","args":[{"_GameNumber":"Integer","args":5},[{"_LookAtTopOfLibraryAction":"ForEachCardPutIntoGraveyardUnlessCost","args":{"_LookAtTopOfLibraryCost":"PayLife","args":{"_GameNumber":"Integer","args":2}}}]]}]`, "dig:5 loselife:2 move:library>graveyard"},
		{"repeated sacrifice cost", `[{"_Action":"RepeatedMayCost","args":[{"_GameNumber":"Integer","args":2},{"_Cost":"SacrificeAPermanent","args":{"_Permanents":"AnyPermanent"}}]}]`, "sacrifice"},
		{"pay any amount of life", `[{"_Action":"PayAnyAmountOfLife"}]`, "loselife:*"},
		{"token enters with counters", `[{"_Action":"CreateTokens","args":[[{"_CreatableToken":"OracleToken","args":"X"}],[{"_TokenFlag":"EntersWithNumberCounters","args":[{"_GameNumber":"Integer","args":3},{"_CounterType":"PTCounter","args":[1,1]}]}]]}]`, "counters:+1/+1:3 tokencopy:1"},
		{"exile a milled card", `[{"_Action":"MayCost","args":{"_Cost":"Exile","args":[[{"_ExilableCost":"ACardOfTypeMilledThisWay","args":{"_CardsInLibrary":"X"}}],[]]}}]`, "move:graveyard>exile"},
	} {
		if got := walkJSON(t, tc.src); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestP2IUnlessPayer(t *testing.T) {
	g := &gorgeNorm{}
	for _, tc := range []struct {
		name  string
		api   string
		defd  string
		payer string
		trig  bool
		want  bool
	}{
		{"default payer is you", "Tap", "", "", false, true},
		{"counter unless: the spell's controller pays", "Counter", "", "", false, false},
		{"damage to the targeted controller", "DealDamage", "TargetedController", "", false, false},
		{"explicit you", "DealDamage", "You", "You", false, true},
		{"your upkeep's player", "DealDamage", "You", "TriggeredPlayer", true, true},
		{"another trigger's player", "DealDamage", "You", "TriggeredPlayer", false, false},
	} {
		g.trigYou = tc.trig
		a := &cards.SA{API: tc.api, Params: map[string]string{"Defined": tc.defd}}
		if got := p2iUnlessPaidByYou(g, a, tc.payer); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestP2IParamTokens(t *testing.T) {
	a := &cards.SA{API: "ChangeZone", Params: map[string]string{"AttachAfter": "Creature.YouCtrl"}}
	if got := joinTokens(p2iParamTokens(a)); got != "attach" {
		t.Errorf("AttachAfter: %q", got)
	}
	if got := p2iParamTokens(&cards.SA{API: "ChangeZone", Params: map[string]string{}}); got != nil {
		t.Errorf("no AttachAfter: %v", got)
	}
}
