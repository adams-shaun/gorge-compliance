package mtgish

import (
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/engine/cards"
)

// A made-up card (no Oracle text): a static that doubles triggers, the
// legend-rule exemption, an attack limit, "enters prepared", a per-defender
// can't-attack rule and "as long as an exiled card has flying, this has
// flying".
const d2Line = `{"_OracleCard":"Card","Name":"Test Echo","Typeline":{"Supertypes":[],"Cardtypes":["Creature"],"Subtypes":["Spirit"]},` +
	`"ManaCost":[{"_ManaSymbol":"ManaCostGeneric","args":3}],"Rules":[` +
	`{"_Rule":"APermanentEnteringTheBattlefieldCausesAbilitiesToTriggerAnAdditionalTime","args":[{"_Permanents":"IsCardtype","args":"Creature"},{"_Abilities":"AnyAbility"}]},` +
	`{"_Rule":"TheLegendsRuleDoesntApplyToPermanents","args":{"_Permanents":"IsCardtype","args":"Creature"}},` +
	`{"_Rule":"NoMoreThanNumberCreaturesCanAttack","args":{"_GameNumber":"Integer","args":2}},` +
	`{"_Rule":"AsPermanentEnters","args":[{"_Permanent":"ThisPermanent"},[{"_ReplacementActionWouldEnter":"EntersPrepared"}]]},` +
	`{"_Rule":"PermanentRuleEffect","args":[{"_Permanent":"ThisPermanent"},[{"_PermanentRule":"CantAttackPlayer","args":{"_Player":"You"}}]]},` +
	`{"_Rule":"PermanentLayerEffect","args":[{"_Permanent":"ThisPermanent"},[{"_LayerEffect":"AddAbilityFromAnExiledHasable","args":[{"_CardsInExile":"WasExiledWithPermanent","args":{"_Permanent":"ThisPermanent"}},[{"_CheckHasable":"Flying"},{"_CheckHasable":"ActivatedAbility"}]]}]]}` +
	`],"CardPT":{"Power":3,"Toughness":3}}` + "\n"

func TestD2Mtgish(t *testing.T) {
	cs, err := Read(strings.NewReader(d2Line), nil)
	if err != nil {
		t.Fatal(err)
	}
	s := FromMtgish(cs["Test Echo"])[0]
	got := map[string]string{}
	for _, f := range s.Fields() {
		got[f.Name] = joinTokens(f.Tokens)
	}
	want := map[string]string{
		"abilities": "replacement static static static static static",
		"effects":   "alterattribute:prepared attackrestrict:2 grant:* grant:flying panharmonicon rule:cantattack static:thelegendsruledoesntapply",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}

func TestD2GorgeStatics(t *testing.T) {
	for _, tc := range []struct {
		mode string
		p    map[string]string
		want []string
	}{
		{"Panharmonicon", nil, []string{"panharmonicon"}},
		{"IgnoreLegendRule", nil, []string{"static:thelegendsruledoesntapply"}},
		{"AttackRestrict", map[string]string{"MaxAttackers": "2"}, []string{"attackrestrict:2"}},
		{"MinMaxBlocker", map[string]string{"Max": "1"}, []string{"rule:cantbeblockedbymorethanone"}},
		{"CantTarget", map[string]string{"ValidTarget": "Card.Self", "ValidSA": "Spell"}, []string{"rule:cantbethetargetofspells"}},
		{"CantAttackUnless", map[string]string{"Target": "You", "Cost": "2"}, []string{"rule:cantattackunlesscost"}},
		{"TapPowerValue", map[string]string{"ValidSA": "Activated.Crew+Vehicle,Activated.Saddle+Mount", "Value": "2"},
			[]string{"rule:crewsvehiclesasthoughpowerweregreater", "rule:saddlesmountsasthoughpowerweregreater"}},
		{"OptionalCost", map[string]string{"Cost": "Blight<1>"}, nil},
		// The Continuous parameters its own case does not read.
		{"Continuous", map[string]string{"CanBlockAmount": "1"}, []string{"rule:canblockanadditionalcreature"}},
		{"Continuous", map[string]string{"RaiseMaxHandSize": "-1"}, []string{"player:reducemaximumhandsize"}},
		{"Continuous", map[string]string{"IgnoreEffectCost": "Sac<1/Permanent>"}, nil},
		{"Continuous", map[string]string{"MayLookAt": "Player", "AffectedZone": "Library"}, []string{"player:playswithtopoflibraryrevealed"}},
		{"CantBeCast", map[string]string{"NumLimitEachTurn": "1"}, []string{"player:cantcastmorethannumberspellseachturn"}},
	} {
		if got := gorgeStaticEffects(tc.mode, tc.p); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s %v: %v, want %v", tc.mode, tc.p, got, tc.want)
		}
	}
}

func TestD2GorgeKeywordsAndEnter(t *testing.T) {
	g := &gorgeNorm{s: &Summary{}, f: &cards.Face{}}
	g.keyword("You may choose not to untap CARDNAME during your untap step.")
	g.keyword("CantAttackOrBlock")
	g.keyword("Strive")
	g.keyword("Trample:Planeswalker")
	g.keyword("Aura swap:2 U")
	var abil []string
	for _, a := range g.s.Abilities {
		abil = append(abil, a.Kind+"="+strings.Join(a.Effects, ","))
	}
	if want := []string{"static=rule:maychoosenottountapduringuntap", "static=rule:cantattack", "static=rule:cantblock"}; !reflect.DeepEqual(abil, want) {
		t.Errorf("abilities %v, want %v", abil, want)
	}
	if want := []string{"trampleoverplaneswalkers", "auraswap:{2}{U}"}; !reflect.DeepEqual(g.s.Keywords, want) {
		t.Errorf("keywords %v, want %v", g.s.Keywords, want)
	}
	out, ok := g.d2EnterEffects(&cards.SA{API: "AlterAttribute", Params: map[string]string{"Attributes": "Prepared"}})
	if !ok || !reflect.DeepEqual(out, []string{"alterattribute:prepared"}) {
		t.Errorf("enter AlterAttribute: %v %v", out, ok)
	}
	// "If this would enter, sacrifice a land instead. If you do, put it
	// onto the battlefield": a replacement of the entering, not an as-enters.
	r := &cards.Repl{Event: "Moved", Params: map[string]string{"Destination": "Battlefield", "ValidCard": "Card.Self"},
		With: &cards.SA{API: "Sacrifice", Params: map[string]string{}, Sub: &cards.SA{API: "ChangeZone",
			Params: map[string]string{"Destination": "Battlefield", "Defined": "ReplacedCard"}}}}
	if !g.d2InsteadEnter(r) {
		t.Error("instead-enter replacement read as an as-enters")
	}
}
