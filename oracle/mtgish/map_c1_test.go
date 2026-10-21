package mtgish

import (
	"encoding/json"
	"testing"

	"github.com/adams-shaun/gorge/engine/cards"
)

// walkJSON summarises a synthetic mtgish action list.
func walkJSON(t *testing.T, src string) string {
	t.Helper()
	var x any
	if err := json.Unmarshal([]byte(src), &x); err != nil {
		t.Fatal(err)
	}
	e := &effects{}
	e.walk(x)
	return joinTokens(sorted(e.effects))
}

func TestC1Actions(t *testing.T) {
	num := func(n int) string { return `{"_GameNumber":"Integer","args":` + itoa(n) + `}` }
	for _, tc := range []struct {
		name, src, want string
	}{
		// A choice maps to nothing; the action it feeds still counts.
		{"choice", `[{"_Action":"ChooseAPlayer","args":{"_Players":"AnyPlayer"}},{"_Action":"DrawACard"}]`, "draw:1"},
		// A wrapper's nested actions are walked.
		{"wrapper", `[{"_Action":"HavePlayerTakeAction","args":[{"_Player":"You"},{"_Action":"DiscardACardAtRandom"}]}]`, "discard:1"},
		{"damage", `[{"_Action":"HavePermanentDealDamage","args":[{"_Permanent":"ThisPermanent"},` + num(3) + `,{"_DamageRecipient":"X"}]}]`, "damage:3"},
		{"lose for each", `[{"_Action":"LoseLifeForEach","args":[` + num(1) + `,{"_GameNumber":"Count"}]}]`, "loselife:*"},
		{"adapt", `[{"_Action":"Adapt","args":` + num(2) + `}]`, "counters:+1/+1:2"},
		{"keyword action", `[{"_Action":"ExploreWithPermanent","args":{"_Permanent":"ThisPermanent"}}]`, "explore"},
		{"cast", `[{"_Action":"CastASpellFromHandWithoutPaying","args":{"_Spells":"AnySpell"}}]`, "play"},
		// A card copy feeds the cast; only the cast is an effect.
		{"copy then cast", `[{"_Action":"CopyExiledCard","args":{"_CardInExile":"X"}},{"_Action":"CastCopiedCardWithoutPaying"}]`, "play"},
		// Exactly one clash branch runs: merged, not summed.
		{"clash", `[{"_Action":"Clash","args":[{"_Players":"Opponent"},[{"_Action":"DrawACard"}],[{"_Action":"DoNothing"}]]}]`, "clash draw:1"},
		{"villainous", `[{"_Action":"FaceAVillianousChoice","args":[[{"_Action":"DrawACard"}],[{"_Action":"DrawACard"},{"_Action":"DiscardACardAtRandom"}]]}]`, "discard:1 draw:1 villainouschoice"},
		{"must cost", `[{"_Action":"EachPlayerMustCost","args":[{"_Players":"Opponent"},{"_Cost":"DiscardACard"}]}]`, "discard:1"},
		{"may cast cost", `[{"_Action":"MayCost","args":{"_Cost":"CastSpellFromExile","args":[{"_Spells":"AnySpell"},{"_CardInExile":"X"}]}}]`, "play"},
		{"future spell", `[{"_Action":"CreateFutureSpellEffect","args":[{"_FutureSpell":"X"},[{"_SpellEffect":"Cascade"},{"_SpellEffect":"DecreaseManaCost","args":{"_CostReduction":"X"}}]]}]`, "costreduction spell:cascade"},
		{"game effect", `[{"_Action":"CreateGameEffect","args":[{"_Expiration":"UntilEndOfTurn"},{"_GameEffect":"DamageCantBePrevented"}]}]`, "static:damagecantbeprevented"},
		{"exiled card", `[{"_Action":"CreateExiledCardEffect","args":[{"_CardInExile":"X"},[{"_ExiledCardEffect":"AddAbility","args":[{"_Rule":"BasicSuspend"}]},{"_ExiledCardEffect":"IsForetold"}]]}]`, "grant:suspend"},
		{"boon", `[{"_Action":"GetOneTimeBoon","args":[{"_Trigger":"X"},{"_Actions":"ActionList","args":[{"_Action":"DrawACard"}]}]}]`, "delayed draw:1"},
	} {
		if got := walkJSON(t, tc.src); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func TestC1GorgeSide(t *testing.T) {
	for _, tc := range []struct {
		api    string
		params map[string]string
		want   string
	}{
		{"PutCounter", map[string]string{"Adapt": "2"}, "counters:+1/+1:2"},
		{"PutCounter", map[string]string{"Bolster": "X"}, "counters:+1/+1:*"},
		{"PutCounter", map[string]string{"CounterType": "P1P1,LOYALTY"}, "counters:+1/+1:1 counters:loyalty:1"},
		{"PutCounter", map[string]string{"CounterType": "Double Strike"}, "counters:doublestrike:1"},
		{"Animate", map[string]string{"Colors": "ChosenColor", "OverwriteColors": "True"}, "color"},
		{"ChangeZone", map[string]string{"Defined": "Self", "Origin": "Command", "Destination": "Exile"}, "-"},
	} {
		got := joinTokens(sorted(gorgeAPIEffects(&cards.SA{API: tc.api, Params: tc.params}, nil)))
		if got != tc.want {
			t.Errorf("%s %v: %q, want %q", tc.api, tc.params, got, tc.want)
		}
	}
	if got := joinTokens(atEOTTokens("YourSacrifice")); got != "delayed sacrifice" {
		t.Errorf("AtEOT sacrifice: %q", got)
	}
	svars := map[string]string{
		"Cleanup":   "Mode$ Phase | Phase$ Untap | Execute$ ExileSelf | Static$ True",
		"ExileSelf": "DB$ ChangeZone | Defined$ Self | Origin$ Command | Destination$ Exile",
		"Real":      "Mode$ Phase | Phase$ End of Turn | Execute$ Draw",
		"Draw":      "DB$ Draw",
	}
	if !effectCleanupTrigger(svars, "Cleanup") || effectCleanupTrigger(svars, "Real") {
		t.Error("effectCleanupTrigger")
	}
	alts := gorgeAltRefs(&cards.SA{API: "VillainousChoice", Params: map[string]string{"Choices": "A,B"}})
	if joinTokens(alts) != "A B" {
		t.Errorf("gorgeAltRefs %v", alts)
	}
}
