package mtgish

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/engine/cards"
)

func TestP2DMtgishActions(t *testing.T) {
	for _, tc := range []struct{ name, js, want string }{
		{"reflexive sacrifice is the sacrifice and its trigger",
			`{"_Action":"Reflexive_Sacrifice_WhenYouDo","args":[[{"_Sacrificable":"APermanent","args":{"_Permanents":"IsPermanent"}}],` +
				`{"_Actions":"ActionList","args":[{"_Action":"DrawACard"}]}]}`,
			"draw:1 sacrifice"},
		{"reflexive discard counts the cards",
			`{"_Action":"Reflexive_Discard_WhenACardIsDiscardedThisWay","args":[{"_Discardable":"ACard"},{"_Actions":"ActionList","args":[]}]}`,
			"discard:1"},
		{"reflexive exile is the exile from its zone",
			`{"_Action":"Reflexive_Exile_WhenYouDo","args":[[{"_Exilable":"CardInGraveyards","args":{}}],{"_Actions":"ActionList","args":[]}]}`,
			"move:graveyard>exile"},
		{"must cost is the action",
			`{"_Action":"MustCost","args":{"_Cost":"SacrificeAPermanent","args":{"_Permanents":"IsPermanent"}}}`,
			"sacrifice"},
		{"may cost stays a cost",
			`{"_Action":"MayCost","args":{"_Cost":"SacrificeAPermanent","args":{"_Permanents":"IsPermanent"}}}`,
			"-"},
		{"must pay mana is no effect",
			`{"_Action":"MustCost","args":{"_Cost":"PayMana","args":[]}}`,
			"-"},
		{"every ChooseAnAction branch counts",
			`{"_Action":"ChooseAnAction","args":[[{"_Action":"DrawACard"}],[{"_Action":"GainLife","args":{"_GameNumber":"Integer","args":2}}]]}`,
			"draw:1 gainlife:2"},
		{"hand or graveyard is both moves",
			`{"_Action":"PutACardFromHandOrGraveyardOnBattlefield","args":[]}`,
			"move:graveyard>battlefield move:hand>battlefield"},
		{"may cast among revealed is a cast",
			`{"_Action":"RevealTheTopNumberCardsOfLibrary","args":[{"_GameNumber":"Integer","args":3},` +
				`[{"_RevealTheTopNumberCardsOfLibraryAction":"MayCastASpellFromAmongThemWithoutPaying","args":{}}]]}`,
			"dig:3 play"},
	} {
		if got := mtgishActions(t, tc.js); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestP2DGorgeSide(t *testing.T) {
	for _, tc := range []struct {
		api  string
		p    map[string]string
		want []string
	}{
		{"ChangeZone", map[string]string{"Origin": "Hand,Graveyard", "Destination": "Exile"},
			[]string{"move:hand>exile", "move:graveyard>exile"}},
		{"ChangeZoneAll", map[string]string{"Origin": "Hand,Graveyard", "Destination": "Library"},
			[]string{"move:hand>library"}},
		{"Dig", map[string]string{"DigNum": "1", "Reveal": "True", "DestinationZone": "Library", "LibraryPosition": "0"},
			[]string{"dig:1"}},
		{"Dig", map[string]string{"DigNum": "10", "ChangeNum": "0", "Reveal": "True"},
			[]string{"dig:10", "move:library>library"}},
		{"Token", map[string]string{"TokenScript": "a,b"}, []string{"token:1", "token:1"}},
		{"CopyPermanent", map[string]string{"PumpKeywords": "Haste"}, []string{"tokencopy:1", "grant:haste"}},
		{"CopyPermanent", map[string]string{"DefinedName": "X"}, []string{"token:1"}},
		{"Animate", map[string]string{"RemoveKeywords": "Flying"}, []string{"loseabilities"}},
		{"Animate", map[string]string{"sVars": "X"}, nil},
		{"PutCounter", map[string]string{"CounterType": "P1P1", "CounterNum": "2", "CounterType2": "Reach", "CounterNum2": "1"},
			[]string{"counters:+1/+1:2", "counters:reach:1"}},
	} {
		got := gorgeAPIEffects(&cards.SA{API: tc.api, Params: tc.p}, nil)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s %v: got %v, want %v", tc.api, tc.p, got, tc.want)
		}
	}
	// Reflexive costs, must-pay costs, and player recipients.
	if got := p2dNestedCost(&cards.SA{API: "ImmediateTrigger", Params: map[string]string{"Cost": "Sac<1/Creature> ExileFromGrave<1/Card>"}}); !reflect.DeepEqual(got, []string{"sacrifice", "move:graveyard>exile"}) {
		t.Errorf("reflexive cost: %v", got)
	}
	if got := p2dNestedCost(&cards.SA{API: "Token", Params: map[string]string{"Cost": "Mandatory Discard<2/Card>"}}); !reflect.DeepEqual(got, []string{"discard:2"}) {
		t.Errorf("mandatory cost: %v", got)
	}
	if got := p2dNestedCost(&cards.SA{API: "Draw", Params: map[string]string{"Cost": "Sac<1/Creature>"}}); got != nil {
		t.Errorf("an activated ability's own cost is a cost: %v", got)
	}
	if got := p2dPlayerGrants(&cards.SA{API: "Pump", Params: map[string]string{"Defined": "You"}}, []string{"grant:protection"}); !reflect.DeepEqual(got, []string{"player:protection"}) {
		t.Errorf("player grant: %v", got)
	}
	if got := p2dPlayerGrants(&cards.SA{API: "PumpAll", Params: map[string]string{"ValidTgts": "Player", "ValidCards": "Creature"}}, []string{"grant:haste"}); !reflect.DeepEqual(got, []string{"grant:haste"}) {
		t.Errorf("creatures a player controls: %v", got)
	}
}

func TestP2DSummaryPasses(t *testing.T) {
	s := &Summary{Abilities: []Ability{
		{Kind: "static", Effects: []string{"rule:cantbethetargetofspells"}},
		{Kind: "static", Effects: []string{"rule:cantbethetargetofabilities"}},
	}}
	p2dFoldTargetStatics(s)
	if len(s.Abilities) != 1 || s.Abilities[0].Effects[0] != "rule:cantbethetargetofspellsorabilities" {
		t.Errorf("fold: %+v", s.Abilities)
	}
	if got := p2dSameHeadGrantTokens([]string{"protection:red", "protection:blue"}); got != "grant:protection" {
		t.Errorf("same head: %q", got)
	}
	a := p2dMtgishAbility(Ability{Kind: "triggered", Effects: []string{"rule:isgoadedbyplayer", "replace:aplayerwouldcreatetokens"}})
	if !reflect.DeepEqual(a.Effects, []string{"goad", "replace:aneffectwouldcreateanynumberoftokens"}) {
		t.Errorf("mtgish ability pass: %v", a.Effects)
	}
	a = p2dGorgeAbility(Ability{Kind: "static", Effects: []string{"rule:isgoadedbyplayer", "player:untapadjust"}})
	if !reflect.DeepEqual(a.Effects, []string{"rule:isgoadedbyplayer", "player:cantuntapmorethannumberpermanentsduringtheiruntapstep"}) {
		t.Errorf("gorge ability pass: %v", a.Effects)
	}
}

// p2dSameHeadGrantTokens runs p2dSameHeadGrant over bare keyword tokens.
func p2dSameHeadGrantTokens(kws []string) string {
	t, _ := p2dSameHeadGrant(&Summary{Keywords: kws})
	return t
}
