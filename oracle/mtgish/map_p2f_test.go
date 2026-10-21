package mtgish

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/engine/cards"
)

func TestP2FMtgishActions(t *testing.T) {
	exileTarget := `{"_Action":"MayCost","args":{"_Cost":"Exile","args":[[{"_ExilableCost":"Permanent","args":{"_Permanent":"Ref_TargetPermanent"}}],[]]}}`
	for _, tc := range []struct{ name, js, want string }{
		{"a flicker's may-cost exile is the exile",
			`{"_Actions":"ActionList","args":[` + exileTarget + `,{"_Action":"If","args":[{"_Condition":"CostWasPaid"},[{"_Action":"PutExiledCardOntoBattlefield","args":[{"_CardInExile":"TheCardExiledThisWay"},[]]}]]}]}`,
			"move:battlefield>exile move:exile>battlefield"},
		{"a flicker inside an If",
			`{"_Actions":"ActionList","args":[{"_Action":"If","args":[{"_Condition":"X"},[` + exileTarget + `,{"_Action":"If","args":[{"_Condition":"CostWasPaid"},[{"_Action":"PutExiledCardOntoBattlefield","args":[{"_CardInExile":"TheCardExiledThisWay"},[]]}]]}]]}]}`,
			"move:battlefield>exile move:exile>battlefield"},
		{"a may-cost exile with no return stays a cost",
			`{"_Actions":"ActionList","args":[` + exileTarget + `,{"_Action":"If","args":[{"_Condition":"CostWasPaid"},[{"_Action":"DrawACard"}]]}]}`,
			"draw:1"},
		{"may put from hand or graveyard is both moves",
			`[{"_Action":"MayCost","args":{"_Cost":"Or","args":[{"_Cost":"PutACardFromHandOnBattlefield","args":[]},{"_Cost":"PutACardFromPlayersGraveyardOnBattlefield","args":[]}]}}]`,
			"move:graveyard>battlefield move:hand>battlefield"},
		{"may play a land or cast a spell is a play",
			`[{"_Action":"MayCost","args":{"_Cost":"Or","args":[{"_Cost":"PlayALandFromTopOfLibrary","args":[]},{"_Cost":"CastTopSpellOfLibraryWithoutPaying","args":[]}]}}]`,
			"play"},
		{"shuffled back is no move",
			`{"_Action":"RevealCardsFromTheTopOfLibraryUntilACardOfTypeIsRevealed","args":[{"_Cards":"X"},[{"_RevealTheTopNumberCardsOfLibraryAction":"PutTheCardFoundThisWayOntoTheBattlefield"},{"_RevealTheTopNumberCardsOfLibraryAction":"PutTheRemainingCardsBackIntoLibraryAndShuffle"}]]}`,
			"diguntil move:library>battlefield"},
		{"a sacrifice inside a reveal is the sacrifice",
			`{"_Action":"RevealTheTopNumberCardsOfLibrary","args":[{"_GameNumber":"Integer","args":2},[{"_RevealTheTopNumberCardsOfLibraryAction":"SacrificePermanent","args":{"_Permanent":"ThisPermanent"}}]]}`,
			"dig:2 sacrifice"},
		{"a found card cast is a play",
			`{"_Action":"SearchLibrary","args":[{"_SearchLibraryAction":"CastFoundCardsWithoutPaying"}]}`,
			"play search:*"},
		{"spell or permanent is one of the two",
			`{"_Action":"ReturnSpellOrPermanentToOwnersHand","args":{}}`,
			"move:battlefield>hand move:stack>hand"},
		{"two single recipients are two damages",
			`{"_Action":"PermanentDealsDamage","args":[{"_Permanent":"ThisPermanent"},{"_GameNumber":"Integer","args":2},{"_DamageRecipient":"MultipleRecipients","args":[{"_DamageRecipient":"Ref_AnyTarget"},{"_DamageRecipient":"Player","args":{"_Player":"You"}}]}]}`,
			"damage:2 damage:2"},
		{"each creature and each player is one damage",
			`{"_Action":"SpellDealsDamage","args":[{"_Spell":"ThisSpell"},{"_GameNumber":"Integer","args":1},{"_DamageRecipient":"MultipleRecipients","args":[{"_DamageRecipient":"EachPermanent","args":{}},{"_DamageRecipient":"EachPlayer","args":{}}]}]}`,
			"damage:1"},
		{"a copy's if-permanent-spell grant",
			`{"_Action":"CopySpellAndMayChooseNewTargetsWithEffects","args":[{"_Spell":"X"},[{"_SpellEffect":"IfPermanentSpell","args":[{"_SpellEffect":"AddAbility","args":[{"_Rule":"Haste"}]}]}]]}`,
			"copyspell grant:haste"},
		{"can't phase in",
			`{"_Action":"CreateGameEffect","args":[{"_Expiration":"UntilEndOfTurn"},{"_GameEffect":"PermanentCantPhaseIn","args":{}}]}`,
			"cantphasein"},
	} {
		if got := mtgishActions(t, tc.js); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
	if got := enterEffects([]any{map[string]any{"_ReplacementActionWouldEnter": "EntersWithACounterOfChoice",
		"args": []any{map[string]any{"_CounterType": "FlyingCounter"}, map[string]any{"_CounterType": "HexproofCounter"}}}}); !reflect.DeepEqual(sorted(got), []string{"counters:flying:1", "counters:hexproof:1"}) {
		t.Errorf("counter of choice: %v", got)
	}
	if got := playerEffectToken("MayActivateLoyaltyAbilityOfPlaneswalkerAnyTimeTheyCouldCastAnInstant"); got != "flash" {
		t.Errorf("loyalty at instant speed: %q", got)
	}
	if got := p2fCDATypes(map[string]any{"_CDA_Types": "AddCreatureTypes", "args": []any{"Cleric"}}); got != "cda" {
		t.Errorf("fixed CDA types: %q", got)
	}
	if got := p2fCDATypes(map[string]any{"_CDA_Types": "AddCreatureTypeVariable", "args": map[string]any{}}); got != "type" {
		t.Errorf("chosen CDA type: %q", got)
	}
	if got := p2fStackSpellToken(map[string]any{"_SpellEffect": "CantBeCast"}); got != "player:cantcastspells" {
		t.Errorf("can't be cast: %q", got)
	}
}

func TestP2FGorgeSide(t *testing.T) {
	for _, tc := range []struct {
		api  string
		p    map[string]string
		want []string
	}{
		{"Dig", map[string]string{"DigNum": "3", "DestinationZone": "Exile", "DestinationZone2": "Graveyard"},
			[]string{"dig:3", "move:library>exile", "move:library>graveyard"}},
		{"Dig", map[string]string{"DigNum": "1", "DestinationZone": "Exile"}, []string{"move:library>exile"}},
		{"ChangeZone", map[string]string{"Origin": "Battlefield,Stack", "Destination": "Hand", "ValidTgts": "Card"},
			[]string{"move:battlefield>hand", "move:stack>hand"}},
		{"PutCounterAll", map[string]string{"CounterType": "P1P1", "CounterNum": "2", "CounterType2": "Reach", "CounterNum2": "1"},
			[]string{"counters:+1/+1:2", "counters:reach:1"}},
	} {
		got := gorgeAPIEffects(&cards.SA{API: tc.api, Params: tc.p}, nil)
		got = append(got, paramTokensP2B(&cards.SA{API: tc.api, Params: tc.p})...)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s %v: got %v, want %v", tc.api, tc.p, got, tc.want)
		}
	}

	g := &gorgeNorm{s: &Summary{}, f: &cards.Face{}}
	chain := func(a *cards.SA) string {
		e := &effects{}
		g.chain(e, a, 0)
		return joinTokens(sorted(e.effects))
	}
	// A search parked in the library is delivered by later moves.
	search := &cards.SA{API: "ChangeZone", Params: map[string]string{"Origin": "Library", "Destination": "Library", "ChangeType": "Creature"},
		Sub: &cards.SA{API: "ChangeZone", Params: map[string]string{"Origin": "Library", "Destination": "Hand", "Defined": "Remembered"}}}
	if got := chain(search); got != "search:library" {
		t.Errorf("parked search: %q", got)
	}
	// A DigUntil whose next SA shuffles the revealed cards back.
	dig := &cards.SA{API: "DigUntil", Params: map[string]string{"Valid": "Creature", "FoundDestination": "Battlefield", "RevealedDestination": "Library"},
		Sub: &cards.SA{API: "Shuffle", Params: map[string]string{}}}
	if got := chain(dig); got != "diguntil move:library>battlefield" {
		t.Errorf("shuffled after: %q", got)
	}
	for _, tc := range []struct {
		sa   cards.SA
		want string
	}{
		{cards.SA{API: "PutCounter", Params: map[string]string{"CounterType": "M1M1", "CounterNum": "3", "RemovePhase": "End of Turn"}},
			"counters:-1/-1:3 countersremove delayed"},
		{cards.SA{API: "DigUntil", Params: map[string]string{"Valid": "Aura", "FoundDestination": "Battlefield", "OptionalFoundMove": "True", "OptionalNoDestination": "Hand"}},
			"diguntil move:library>battlefield move:library>hand"},
		{cards.SA{API: "ChangeZone", Params: map[string]string{"Origin": "Graveyard,Hand,Library", "Destination": "Exile", "ChangeType": "Card"}},
			"move:graveyard>exile move:hand>exile search:exile"},
		{cards.SA{API: "Pump", Params: map[string]string{"KW": "Prevent all combat damage that would be dealt by CARDNAME."}},
			"prevent"},
	} {
		if got := chain(&tc.sa); got != tc.want {
			t.Errorf("%s %v: got %q, want %q", tc.sa.API, tc.sa.Params, got, tc.want)
		}
	}
}
