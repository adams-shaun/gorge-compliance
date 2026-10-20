package mtgish

import (
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/engine/cards"
)

func TestD1TriggerClassesMeet(t *testing.T) {
	// Each pair is one event spelled by both sides.
	for _, tc := range []struct {
		mode    string
		p       map[string]string
		variant string
		want    string
	}{
		{"ChaosEnsues", nil, "WhenChaosEnsues", "chaosensues"},
		{"PlaneswalkedTo", nil, "WhenAPlayerEncountersAPhenomenon", "planeswalkedto"},
		{"SetInMotion", nil, "WhenAPlayerSetsASchemeInMotion", "setinmotion"},
		{"TurnFaceUp", nil, "WhenAPermanentIsTurnedFaceUp", "turnfaceup"},
		{"Cycled", nil, "WhenAPlayerCyclesACard", "cycled"},
		{"RolledDieOnce", nil, "WhenAPlayerRollsAnyNumberOfDice", "rolleddie"},
		{"RolledDie", nil, "WhenAPlayerRollsADiesHighestNaturalResult", "rolleddie"},
		{"CounterRemovedOnce", nil, "WhenAnyNumberOfCountersOfTypeAreRemovedFromAPermanent", "counterremoved"},
		{"Countered", nil, "WhenASpellOrAbilityCountersASpell", "countered"},
		{"AttackerUnblocked", nil, "WhenACreatureAttacksAndIsntBlocked", "unblocked"},
		{"PayCumulativeUpkeep", nil, "WhenAPermanentsCumulativeUpkeepCostIsPaid", "paycumulativeupkeep"},
		{"DamagePreventedOnce", nil, "WhenDamageThatWouldBeDealtToAPlayerIsPrevented", "damageprevented"},
		{"AbilityCast", nil, "WhenAPlayerSacrificesAnyNumberOfPermanentsToActivateAnAbility", "abilitycast"},
		{"CounterPlayerAddedAll", nil, "WhenAPlayerGetsEnergy", "counters"},
		{"Exiled", map[string]string{"Origin": "Battlefield"}, "WhenAPermanentIsExiled", "ltb"},
		{"Phase", map[string]string{"Phase": "EndCombat"}, "AtTheEndOfCombat", "end-combat"},
		{"Phase", map[string]string{"FirstUpkeepThisGame": "True"}, "AtTheBeginningOfTheFirstUpkeepOfTheGame", "upkeep"},
		{"Always", nil, "WhenAPlayerControlsNoPermanents", "state"},
		{"ChangesZoneAll", map[string]string{"Origin": "Graveyard", "Destination": "Exile"}, "WhenAnyNumberOfGraveyardCardsLeave", "zone"},
	} {
		if got := gorgeTriggerClass(tc.mode, tc.p); got != tc.want {
			t.Errorf("gorgeTriggerClass(%s, %v) = %q, want %q", tc.mode, tc.p, got, tc.want)
		}
		if got := triggerClass(tc.variant); got != tc.want {
			t.Errorf("triggerClass(%s) = %q, want %q", tc.variant, got, tc.want)
		}
	}
	// Overrides leave the plain word classes alone.
	if got := triggerClass("WhenACreatureAttacks"); got != "attacks" {
		t.Errorf("attacks: %q", got)
	}
}

func TestD1TriggerEvents(t *testing.T) {
	desc := func(p map[string]string) map[string]string {
		p["TriggerDescription"] = "x"
		return p
	}
	for _, tc := range []struct {
		mode string
		p    map[string]string
		want []string
	}{
		// Undescribed bookkeeping and static permission halves: nothing.
		{"TurnBegin", map[string]string{"Static": "True"}, nil},
		{"ChangesZone", map[string]string{"Origin": "Battlefield", "Destination": "Any"}, nil},
		{"SpellCast", desc(map[string]string{"Static": "True", "Secondary": "True"}), nil},
		// A described static trigger (a triggered mana ability) stays.
		{"ManaAdded", desc(map[string]string{"Static": "True"}), []string{"manaadded"}},
		// A keyword's expansion stays without a description.
		{"ChangesZone", map[string]string{"Destination": "Battlefield", "KeywordLine": "Bushido:1"}, []string{"etb"}},
		// One ability per event.
		{"SpellAbilityCast", desc(map[string]string{"ValidSA": "Instant.YouCtrl,Sorcery.YouCtrl,Activated.YouCtrl"}), []string{"cast", "abilitycast"}},
		{"SpellAbilityCast", desc(map[string]string{"ValidSA": "Spell.nonCreature"}), []string{"cast"}},
		{"SpellAbilityCast", desc(map[string]string{"ValidSA": "SpellAbility.!ManaAbility"}), []string{"cast", "abilitycast"}},
		{"ChangesZone", desc(map[string]string{"Origin": "Battlefield", "Destination": "Graveyard,Exile"}), []string{"dies", "ltb"}},
		{"ChangesZone", desc(map[string]string{"Origin": "Battlefield", "Destination": "Exile,Hand,Library"}), []string{"ltb"}},
	} {
		if got := gorgeTriggerEvents(tc.mode, tc.p); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("gorgeTriggerEvents(%s, %v) = %v, want %v", tc.mode, tc.p, got, tc.want)
		}
	}
	trig := func(v string) any { return map[string]any{"_Trigger": v} }
	if got := dedup(triggerEvents(trig("WhenAPlayerPlaysACard"))); !reflect.DeepEqual(got, []string{"cast", "landplayed"}) {
		t.Errorf("plays a card: %v", got)
	}
	if got := dedup(triggerEvents(trig("WhenAPlayerLosesControlOfAPermanent"))); !reflect.DeepEqual(got, []string{"changescontroller", "ltb"}) {
		t.Errorf("loses control: %v", got)
	}
}

// A synthetic modal trigger (a made-up card): "whenever an opponent
// sacrifices or discards, draw" written as TriggerModalA, whose
// modes are plain [trigger, actions] pairs.
const d1ModalLine = `{"_OracleCard":"Card","Name":"Test Modal Watcher","Typeline":{"Supertypes":[],"Cardtypes":["Enchantment"],"Subtypes":[]},` +
	`"ManaCost":[{"_ManaSymbol":"ManaCostB"}],"Rules":[{"_Rule":"TriggerModalA","args":[` +
	`[{"_Trigger":"WhenAPlayerSacrificesAPermanent","args":[{"_Players":"Opponent"},{"_Permanents":"IsNonToken"}]},{"_Actions":"ActionList","args":[{"_Action":"DrawACard"}]}],` +
	`[{"_Trigger":"WhenAPlayerDiscardsACard","args":[{"_Players":"Opponent"},{"_CardsInHand":"IsPermanent"}]},{"_Actions":"ActionList","args":[{"_Action":"DrawACard"}]}]` +
	`]}]}` + "\n"

func TestD1TriggerModalPairs(t *testing.T) {
	cs, err := Read(strings.NewReader(d1ModalLine), nil)
	if err != nil {
		t.Fatal(err)
	}
	c := cs["Test Modal Watcher"]
	if c == nil {
		t.Fatalf("Read: %v", SortedNames(cs))
	}
	s := FromMtgish(c)[0]
	var got []string
	for _, a := range s.Abilities {
		got = append(got, a.Shape())
		if !reflect.DeepEqual(a.Effects, []string{"draw:1"}) {
			t.Errorf("%s effects %v", a.Shape(), a.Effects)
		}
	}
	if got = sorted(got); !reflect.DeepEqual(got, []string{"triggered:discard", "triggered:sacrifice"}) {
		t.Errorf("abilities %v", got)
	}
}

// A synthetic script (a made-up Attraction, not a corpus card): a Visit
// ability, an undescribed bookkeeping trigger and a "dies or is exiled"
// trigger.
const d1Script = "Name:Test Ride\nManaCost:no cost\nTypes:Artifact Attraction\n" +
	"K:Visit:TrigDraw\n" +
	"SVar:TrigDraw:DB$ Draw | NumCards$ 1\n" +
	"T:Mode$ ChangesZone | Origin$ Battlefield | Destination$ Any | ValidCard$ Card.Self | Execute$ TrigReset | Static$ True\n" +
	"SVar:TrigReset:DB$ Cleanup | ClearRemembered$ True\n" +
	"T:Mode$ ChangesZone | Origin$ Battlefield | Destination$ Graveyard,Exile | ValidCard$ Card.Self | Execute$ TrigDraw | TriggerDescription$ x\n"

func TestD1GorgeFace(t *testing.T) {
	c, diags := cards.ParseBytes("test_ride.txt", []byte(d1Script))
	if c == nil {
		t.Fatalf("parse: %v", diags)
	}
	s := FromGorge("Test Ride", c, nil)[0]
	var got []string
	for _, a := range s.Abilities {
		got = append(got, a.Shape())
	}
	if got = sorted(got); !reflect.DeepEqual(got, []string{"triggered:dies", "triggered:ltb", "triggered:visit"}) {
		t.Errorf("abilities %v", got)
	}
	if !reflect.DeepEqual(s.Keywords, []string{"visit"}) {
		t.Errorf("keywords %v", s.Keywords)
	}
	if got := sorted(s.Fields()[len(s.Fields())-1].Tokens); !reflect.DeepEqual(got, []string{"draw:1", "draw:1", "draw:1"}) {
		t.Errorf("effects %v", got)
	}
}
