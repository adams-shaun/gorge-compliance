package mtgish

import (
	"encoding/json"
	"testing"

	"github.com/adams-shaun/gorge/engine/cards"
)

// mtgishActions summarises a synthetic mtgish action list (made-up
// abilities in mtgish's JSON shape, no card text).
func mtgishActions(t *testing.T, js string) string {
	t.Helper()
	var x any
	if err := json.Unmarshal([]byte(js), &x); err != nil {
		t.Fatal(err)
	}
	e := &effects{}
	e.walk(x)
	return joinTokens(sorted(e.effects))
}

func TestC2MtgishActions(t *testing.T) {
	for _, tc := range []struct{ name, js, want string }{
		{"repeat walks its body once",
			`{"_Action":"RepeatableActions","args":[{"_Action":"DrawACard"},{"_Action":"MayAction","args":{"_Action":"RepeatThisProcess"}}]}`,
			"draw:1"},
		{"dice table is alternatives",
			`{"_Action":"ValueActions","args":[{"_GameNumber":"TheDiceResult"},[` +
				`{"_ValueAction":"ValueAction","args":[{"_GameRange":"BetweenValues","args":[1,9]},[{"_Action":"DrawACard"}]]},` +
				`{"_ValueAction":"ValueAction","args":[{"_GameRange":"BetweenValues","args":[10,20]},[{"_Action":"DrawNumberCards","args":{"_GameNumber":"Integer","args":2}}]]}]]}`,
			"draw:*"},
		{"a player's choice of action is alternatives, walked once",
			`{"_Action":"PlayerChooseAnAction","args":[{"_Player":"You"},[` +
				`{"_Action":"PutPermanentOnBottomOfOwnersLibrary","args":{"_Permanent":"ThisPermanent"}},` +
				`{"_Action":"PutPermanentInOwnersLibraryNthFromTheTop","args":[{"_Permanent":"ThisPermanent"},{"_GameNumber":"Integer","args":2}]}]]}`,
			"move:battlefield>library"},
		{"monstrosity", `{"_Action":"Monstrosity","args":{"_GameNumber":"Integer","args":3}}`, "counters:+1/+1:3"},
		{"monstrosity X", `{"_Action":"Monstrosity","args":{"_GameNumber":"ValueX"}}`, "counters:+1/+1:*"},
		{"must sacrifice", `{"_Action":"PlayerMustCost","args":[{"_Player":"You"},{"_Cost":"SacrificeAPermanent","args":{"_Permanents":"IsPermanent"}}]}`, "sacrifice"},
		{"three-zone search",
			`{"_Action":"SearchPlayersLibraryAndGraveyardAndHand","args":[{"_Player":"Ref_TargetPlayer"},[{"_SearchLibraryAction":"ExileFoundCards"}]]}`,
			"move:graveyard>exile move:hand>exile search:exile"},
		{"one-pool search",
			`{"_Action":"SearchLibraryAndGraveyard","args":[{"_SearchLibraryAction":"PutFoundCardsIntoHand"}]}`,
			"search:hand"},
		{"whole zones shuffled", `{"_Action":"ShuffleHandAndGraveyardIntoLibrary"}`, "move:*>library"},
		{"cards from hand onto library carry the count",
			`{"_Action":"PutNumCardsFromHandOnTopOfLibraryAnyOrder","args":{"_GameNumber":"Integer","args":2}}`,
			"move:hand>library:2"},
		{"reveals and notes are not effects", `{"_Action":"RevealACardFromHandAtRandom"}`, "-"},
		{"keyword action", `{"_Action":"VentureIntoTheDungeon"}`, "venture"},
		{"damage amount", `{"_Action":"PlaneDealsDamage","args":[{"_Plane":"ThisPlane"},{"_GameNumber":"Integer","args":2},{"_DamageRecipient":"EachPlayer"}]}`, "damage:2"},
		{"multi draw",
			`{"_Action":"MultiDraw","args":[{"_MultiDrawable":"DrawNumberCards","args":{"_GameNumber":"Integer","args":2}},{"_MultiDrawable":"EachPlayerDrawsACard","args":{"_Players":"AnyPlayer"}}]}`,
			"draw:1 draw:2"},
	} {
		if got := mtgishActions(t, tc.js); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestC2GorgeSide(t *testing.T) {
	for _, tc := range []struct {
		name string
		sa   cards.SA
		want string
	}{
		{"monstrosity", cards.SA{API: "PutCounter", Params: map[string]string{"Monstrosity": "3"}}, "counters:+1/+1:3"},
		{"support", cards.SA{API: "PutCounter", Params: map[string]string{"Support": "2"}}, "counters:+1/+1:1"},
		{"ungoad", cards.SA{API: "Goad", Params: map[string]string{"NoLonger": "True"}}, "ungoad"},
		{"perpetual", cards.SA{API: "PumpAll", Params: map[string]string{"NumAtt": "+1", "Duration": "Perpetual"}}, "perpetual"},
		{"target player's library is searched",
			cards.SA{API: "ChangeZone", Params: map[string]string{"Origin": "Library", "Destination": "Exile", "ValidTgts": "Opponent"}},
			"search:exile"},
		{"library and graveyard is one search",
			cards.SA{API: "ChangeZone", Params: map[string]string{"Origin": "Graveyard,Library", "Destination": "Hand"}},
			"search:hand"},
		{"a filtered reveal's rest moves",
			cards.SA{API: "Dig", Params: map[string]string{"DigNum": "1", "ChangeNum": "All", "ChangeValid": "Creature", "DestinationZone": "Hand", "DestinationZone2": "Graveyard"}},
			"move:library>graveyard move:library>hand"},
	} {
		if got := joinTokens(sorted(gorgeAPIEffects(&tc.sa, nil))); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
	if c2PlayerTargets("Creature") || !c2PlayerTargets("Player,Opponent") {
		t.Error("c2PlayerTargets")
	}
}

func TestC2GorgeChase(t *testing.T) {
	svars := map[string]string{
		"Win":  "DB$ Draw | NumCards$ 2",
		"Lose": "DB$ Discard | NumCards$ 1",
		"R1":   "DB$ Draw | NumCards$ 1",
		"R2":   "DB$ Draw | NumCards$ 3",
	}
	g := &gorgeNorm{s: &Summary{}, f: &cards.Face{SVars: svars}}
	for _, tc := range []struct {
		name string
		sa   cards.SA
		want string
	}{
		{"coin branches are summed",
			cards.SA{API: "FlipCoin", Params: map[string]string{"WinSubAbility": "Win", "LoseSubAbility": "Lose"}},
			"coin discard:1 draw:2"},
		{"dice results are alternatives",
			cards.SA{API: "RollDice", Params: map[string]string{"ResultSubAbilities": "1-9:R1,10-20:R2"}},
			"dice draw:*"},
	} {
		e := &effects{}
		g.chain(e, &tc.sa, 0)
		if got := joinTokens(sorted(e.effects)); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}
