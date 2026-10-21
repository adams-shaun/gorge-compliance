package mtgish

import (
	"testing"

	"github.com/adams-shaun/gorge/engine/cards"
)

func TestP2BMtgishActions(t *testing.T) {
	perm := `{"_Permanent":"ThisPermanent"}`
	for _, tc := range []struct {
		name, src, want string
	}{
		// A MayCost whose cost no Forge cost can express is the action.
		{"may cost put from graveyard", `[{"_Action":"MayCost","args":{"_Cost":"PutACardFromGraveyardIntoHand","args":{"_Cards":"X"}}},{"_Action":"If","args":[{"_Condition":"CostWasPaid"},[{"_Action":"DrawACard"}]]}]`, "draw:1 move:graveyard>hand"},
		// A cost-shaped MayCost paid while the ability resolves is done too.
		{"may cost sacrifice", `[{"_Action":"MayCost","args":{"_Cost":"SacrificeAPermanent","args":{"_Permanents":"X"}}},{"_Action":"If","args":[{"_Condition":"CostWasPaid"},[{"_Action":"DrawACard"}]]}]`, "draw:1 sacrifice"},
		// A MustCost is a mandatory action, except a sacrifice.
		{"must cost bounce", `[{"_Action":"MustCost","args":{"_Cost":"PutPermanentIntoItsOwnersHand","args":` + perm + `}}]`, "move:battlefield>hand"},
		{"must cost sacrifice", `[{"_Action":"MustCost","args":{"_Cost":"SacrificePermanent","args":` + perm + `}}]`, "sacrifice"},
		// Every ChooseAnAction branch counts (merged), not the first only.
		{"choose an action", `[{"_Action":"ChooseAnAction","args":[[{"_Action":"DrawACard"}],[{"_Action":"MillACard"}]]}]`, "draw:1 mill:1"},
		// Counters a card enters or is exiled with.
		{"enters with counter", `[{"_Action":"PutGraveyardCardOntoBattlefield","args":[{"_CardInGraveyard":"X"},[{"_EnterFlag":"EntersWithACounter","args":{"_CounterType":"FinalityCounter"}}]]}]`, "counters:finality:1 move:graveyard>battlefield"},
		{"exiled with counters", `[{"_Action":"Exile","args":[[{"_Exilable":"Spell","args":{"_Spell":"ThisSpell"}}],[{"_ExileFlag":"ExilesWithNumberCounters","args":[{"_GameNumber":"Integer","args":3},{"_CounterType":"TimeCounter"}]}]]}]`, "counters:time:3 move:stack>exile"}, // time counters count like any other
		// A reflexive "put it onto the battlefield. When you do".
		{"reflexive put", `[{"_Action":"Reflexive_PutOnBattlefield_WhenYouDo","args":[[{"_PuttableOnBattlefield":"CardInGraveyard","args":{"_CardInGraveyards":"X"}}],[{"_EnterFlag":"EntersNormally"}],{"_Actions":"ActionList","args":[{"_Action":"DrawACard"}]}]}]`, "draw:1 move:graveyard>battlefield"},
		// An exile from permanents and/or hand.
		{"exile two zones", `[{"_Action":"Exile","args":[[{"_Exilable":"NumberPermanentsOrCardsInHand","args":[]}],[]]}]`, "move:battlefield>exile move:hand>exile"},
		// Look steps: shuffle the rest back, if/else, a granted ability.
		{"look rest shuffled", `[{"_Action":"LookAtTheTopNumberCardsOfLibrary","args":[{"_GameNumber":"Integer","args":3},[{"_LookAtTopOfLibraryAction":"PutTheRemainingCardsBackIntoLibraryAndShuffle"}]]}]`, "dig:3"},
		{"look if else", `[{"_Action":"LookAtTheTopNumberCardsOfLibrary","args":[{"_GameNumber":"Integer","args":3},[{"_LookAtTopOfLibraryAction":"IfElse","args":[{"_Condition":"X"},[{"_LookAtTopOfLibraryAction":"PutAGenericCardIntoHand"}],[{"_LookAtTopOfLibraryAction":"PutAGenericCardIntoGraveyard"}]]}]]}]`, "dig:3 move:library>graveyard move:library>hand"},
		{"look grants", `[{"_Action":"RevealTheTopNumberCardsOfLibrary","args":[{"_GameNumber":"Integer","args":4},[{"_RevealTheTopNumberCardsOfLibraryAction":"CreatePermanentLayerEffect","args":[` + perm + `,[{"_LayerEffect":"AddAbility","args":[{"_Rule":"Haste"}]}]]}]]}]`, "dig:4 grant:haste"},
	} {
		if got := walkJSON(t, tc.src); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestP2BCanonical(t *testing.T) {
	if got := replacementEffects(map[string]any{"_Rule": "ReplaceAnyNumberOfTokensWouldBeCreated"}); joinTokens(got) != "replace:aneffectwouldcreateanynumberoftokens" {
		t.Errorf("token replacement: %v", got)
	}
	if got := canonGrantP2B("grant:protectionfromcolor"); got != "grant:protection" {
		t.Errorf("canonGrantP2B: %q", got)
	}
	if got := grantTokenP2B(map[string]any{"_Rule": "BandsWithOthers"}, &Summary{}); got != "grant:bandswithother" {
		t.Errorf("bands: %q", got)
	}
	if got := grantTokenP2B(map[string]any{"_Rule": "X"}, &Summary{Keywords: []string{"protection:black", "protection:red"}}); got != "grant:protection" {
		t.Errorf("two protections: %q", got)
	}
}

func TestP2BGorgeSide(t *testing.T) {
	for _, tc := range []struct {
		name string
		sa   cards.SA
		want string
	}{
		{"exile from two origins", cards.SA{API: "ChangeZone", Params: map[string]string{"Origin": "Battlefield,Graveyard", "Destination": "Exile"}}, "move:graveyard>exile"},
		{"look at one card, else hand", cards.SA{API: "Dig", Params: map[string]string{"DigNum": "1", "ChangeNum": "1", "Optional": "True", "DestinationZone": "Battlefield", "DestinationZone2": "Hand"}}, "move:library>hand"},
		{"alternative destination", cards.SA{API: "ChangeZone", Params: map[string]string{"Origin": "Graveyard", "Destination": "Hand", "DestinationAlternative": "Battlefield"}}, "move:graveyard>battlefield"},
	} {
		if got := joinTokens(paramTokensP2B(&tc.sa)); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
	follow := &cards.SA{API: "ChangeZone", Params: map[string]string{"Origin": "Library", "Destination": "Hand", "ChangeType": "Land.IsRemembered"}}
	if !searchFollowUpP2B(follow) {
		t.Error("a remembered-card distribution is part of the search")
	}
}
