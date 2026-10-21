package mtgish

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/engine/cards"
)

// Synthetic abilities only: made-up actions in mtgish's JSON shape and
// made-up script parameters, no card text.

func TestP2cMtgishActions(t *testing.T) {
	sac := `{"_Cost":"SacrificeAPermanent","args":{"_Permanents":"IsPermanent"}}`
	for _, tc := range []struct{ name, js, want string }{
		{"a cost that must be paid is done", `{"_Action":"MustCost","args":` + sac + `}`, "sacrifice"},
		{"an optional cost paid is done", `{"_Action":"MayCost","args":` + sac + `}`, "sacrifice"},
		{"a mana payment is not", `{"_Action":"MayCost","args":{"_Cost":"PayMana","args":[]}}`, "-"},
		{"an energy payment is not", `{"_Action":"MustCost","args":{"_Cost":"PayEnergy","args":2}}`, "-"},
		{"revealing a card already seen is not", `{"_Action":"MayCost","args":{"_Cost":"RevealTopCardOfLibrary"}}`, "-"},
		{"tapping permanents", `{"_Action":"MayCost","args":{"_Cost":"TapAPermanent","args":{"_Permanents":"IsPermanent"}}}`, "tap"},
		{"paying any amount of life", `{"_Action":"MayCost","args":{"_Cost":"PayAnyAmountOfLife"}}`, "loselife:*"},
		{"a reflexive sacrifice is done",
			`{"_Action":"Reflexive_Sacrifice_WhenYouDo","args":[[{"_Sacrificable":"Permanent","args":{"_Permanent":"ThisPermanent"}}],{"_Actions":"ActionList","args":[{"_Action":"DrawACard"}]}]}`,
			"draw:1 sacrifice"},
		{"a reflexive exile names its zone",
			`{"_Action":"Reflexive_Exile_WhenYouDo","args":[[{"_Exilable":"CardInGraveyards","args":{}}],{"_Actions":"ActionList","args":[]}]}`,
			"move:graveyard>exile"},
		{"an action inside a reveal is walked",
			`{"_Action":"RevealLibrary","args":[{"_RevealTheTopNumberCardsOfLibraryAction":"If","args":[{"_Condition":"X"},[` +
				`{"_RevealTheTopNumberCardsOfLibraryAction":"SpellDealsDamage","args":[{"_Spell":"ThisSpell"},{"_GameNumber":"Integer","args":8},{"_DamageRecipient":"Player"}]}]]},` +
				`{"_RevealTheTopNumberCardsOfLibraryAction":"PutTheRemainingCardsBackIntoLibraryAndShuffle"}]}`,
			"damage:8 dig:*"},
		{"casting from among the looked-at cards",
			`{"_Action":"LookAtTheTopNumberCardsOfLibrary","args":[{"_GameNumber":"Integer","args":3},[{"_LookAtTopOfLibraryAction":"MayCastASpellFromAmongThemWithoutPaying"}]]}`,
			"dig:3 play"},
		{"a look's optional pick is its move",
			`{"_Action":"LookAtTheTopNumberCardsOfLibrary","args":[{"_GameNumber":"Integer","args":4},[{"_LookAtTopOfLibraryAction":"MayCost","args":{"_LookAtTopOfLibraryCost":"PutACardOfTypeOntoTheBattlefield","args":[]}}]]}`,
			"dig:4 move:library>battlefield"},
		{"a goad given by an action is the goad verb",
			`{"_Action":"CreatePermanentRuleEffectUntil","args":[{"_Permanent":"ThisPermanent"},[{"_PermanentRule":"IsGoadedByPlayer"}],{"_Expiration":"UntilEndOfTurn"}]}`,
			"goad"},
		{"a token of a named card is a copy",
			`{"_Action":"CreateTokens","args":[[{"_CreatableToken":"OracleToken","args":"Some Card"}],[]]}`,
			"tokencopy:1"},
		{"a may-cost spell damage is the damage",
			`{"_Action":"MayCost","args":{"_Cost":"HaveSpellDealDamage","args":[{"_Spell":"ThisSpell"},{"_GameNumber":"Integer","args":3},{"_DamageRecipient":"Player"}]}}`,
			"damage:3"},
		{"a waterbend cost is a payment",
			`{"_Action":"MayCost","args":{"_Cost":"Waterbend","args":{"_GameNumber":"Integer","args":2}}}`,
			"-"},
		{"playing a land from the top is a play",
			`{"_Action":"MayCost","args":{"_Cost":"PlayALandFromTopOfLibrary","args":{"_Cards":"IsCardtype","args":"Land"}}}`,
			"play"},
		{"each player's token copies",
			`{"_Action":"EachPlayerCreatesTokens","args":[{"_Players":"Opponent"},[{"_CreatableToken":"TokenCopyOfPermanent","args":[]}],[]]}`,
			"tokencopy:*"},
	} {
		if got := mtgishActions(t, tc.js); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
	if got := replacementEffects(node{"_Rule": "ReplaceAPlayerWouldCreateTokens"}); strings.Join(got, " ") != "replace:aneffectwouldcreateanynumberoftokens" {
		t.Errorf("token replacement synonym: %v", got)
	}
}

func TestP2cGorgeChain(t *testing.T) {
	svars := map[string]string{
		"Sac":     "DB$ Draw | NumCards$ 1 | Cost$ Mandatory Sac<1/CARDNAME>",
		"MaySac":  "DB$ ImmediateTrigger | Cost$ Sac<1/Creature.Other/another creature> | Execute$ Dmg",
		"Dmg":     "DB$ DealDamage | NumDmg$ 2",
		"Unless":  "DB$ Tap | Defined$ Self | UnlessCost$ Discard<1/Card> | UnlessPayer$ You",
		"Theirs":  "DB$ Tap | Defined$ Self | UnlessCost$ PayLife<2> | UnlessPayer$ TargetedController",
		"Copy":    "DB$ CopyPermanent | Defined$ Self | PumpKeywords$ Haste | AtEOTTrig$ You_Sacrifice",
		"OwnTrig": "DB$ CopyPermanent | Defined$ Self | AtEOTTrig$ Sacrifice",
	}
	g := &gorgeNorm{s: &Summary{}, f: &cards.Face{SVars: svars}}
	for _, tc := range []struct{ name, top, want string }{
		{"a mandatory sub-ability cost is done", "Sac", "draw:1 sacrifice"},
		{"an optional reflexive cost is done", "MaySac", "damage:2 sacrifice"},
		{"an unless cost its controller pays is done", "Unless", "discard:1 tap"},
		{"an unless cost another player pays stays a cost", "Theirs", "tap"},
		{"a copy's keywords and its delayed sacrifice", "Copy", "delayed grant:haste sacrifice tokencopy:1"},
		{"a copy's own printed trigger is the token's", "OwnTrig", "tokencopy:1"},
	} {
		e := &effects{}
		// Depth 1: the ability resolves as a trigger's or sub-ability.
		g.chain(e, cards.ResolveSVar(svars, tc.top), 1)
		if got := joinTokens(sorted(e.effects)); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
	// An activated ability's own cost (depth 0) stays a cost.
	e := &effects{}
	g.chain(e, &cards.SA{Kind: "AB", API: "Draw", Params: map[string]string{"NumCards": "1", "Cost": "Sac<1/CARDNAME>"}}, 0)
	if got := joinTokens(sorted(e.effects)); got != "draw:1" {
		t.Errorf("activation cost: got %q", got)
	}
}

func TestP2cGorgeAPI(t *testing.T) {
	for _, tc := range []struct {
		name string
		sa   cards.SA
		want string
	}{
		{"one card looked at and left on top does not move",
			cards.SA{API: "Dig", Params: map[string]string{"DigNum": "1", "ChangeNum": "All", "ChangeValid": "Land", "LibraryPosition2": "0"}},
			"dig:1 move:library>hand"},
		{"revealed cards shuffled back are not a move",
			cards.SA{API: "DigUntil", Params: map[string]string{"FoundDestination": "Battlefield", "RevealedDestination": "Library", "Shuffle": "True"}},
			"diguntil move:library>battlefield"},
		{"gaining all creature types is a type change",
			cards.SA{API: "Animate", Params: map[string]string{"Power": "4", "Toughness": "4", "AddAllCreatureTypes": "True"}},
			"setpt type"},
	} {
		if got := joinTokens(sorted(gorgeAPIEffects(&tc.sa, nil))); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
	if got := p2cCostParts("2 B Sac<1/Creature.Other/another creature> PayLife<3>"); len(got) != 4 {
		t.Errorf("cost parts %q", got)
	}
}
