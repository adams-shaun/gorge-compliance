package oraclegen

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
)

// TestXMageTriggerOrderKeysTable is the class table for the same-source
// trigger-order key: XMage's chooseTriggeredAbility matches a choice with
// getRule().startsWith(choice), and getRule() leaves "{this}" literal and
// prints an ability word as "<i>Word</i> &mdash; ". Each row is one shape the
// rewrite or the distinctness rule must handle; the Ashling row is the
// measured harness (trigger#1.1/#1.2), replayed AGREE in XMage with this key.
func TestXMageTriggerOrderKeysTable(t *testing.T) {
	for _, tc := range []struct {
		name  string
		picks []string
		want  []string // nil: not keyable, the caller keeps the old text form
	}{
		{
			name: "Ashling, Rimebound: CARDNAME and NICKNAME become {this}; the first sentence keys",
			picks: []string{
				"Ashling, Rimebound: Whenever this creature transforms into CARDNAME and at the beginning of your first main phase, add two mana of any one color. Spend this mana only to cast spells with mana value 4 or greater.",
				"Ashling, Rimebound: At the beginning of your first main phase, you may pay {R}. If you do, transform NICKNAME.",
			},
			want: []string{"Whenever this creature transforms into {this} and at the beginning of your first main phase, add two mana of any one color."},
		},
		{
			name: "an ability-word header is italicised with an em-dash entity",
			picks: []string{
				"Source: Landfall — Whenever a land you control enters, double the number of +1/+1 counters on this creature.",
				"Source: At the beginning of combat on your turn, put a +1/+1 counter on this creature.",
			},
			want: []string{"<i>Landfall</i> &mdash; Whenever a land you control enters, double the number of +1/+1 counters on this creature."},
		},
		{
			name: "the source's printed name is a self-reference too",
			picks: []string{
				"Grub, Notorious Auntie: When Grub, Notorious Auntie enters, draw a card. Then discard a card.",
				"Grub, Notorious Auntie: At the beginning of your end step, you lose 1 life.",
			},
			want: []string{"When {this} enters, draw a card."},
		},
		{
			name: "a first sentence another, different pick shares extends to the whole text",
			picks: []string{
				"Source: When this enters, draw a card. Then discard a card.",
				"Source: When this enters, draw a card. Then gain 1 life.",
				"Source: Offspring",
			},
			want: []string{"When this enters, draw a card. Then discard a card.", "When this enters, draw a card. Then gain 1 life."},
		},
		{
			name:  "identical abilities need no telling apart",
			picks: []string{"Source: When this enters, do a thing.", "Source: When this enters, do a thing."},
			want:  []string{"When this enters, do a thing."},
		},
		{
			name:  "a whole text that prefixes another pick's different text cannot key",
			picks: []string{"Source: When this enters, draw a card", "Source: When this enters, draw a card and gain 1 life."},
		},
		{
			name:  "a single pick is never asked",
			picks: []string{"Source: When this enters, draw a card."},
		},
	} {
		d := rules.OracleDecision{Kind: "order", GorgeKind: "trigger_order", Picks: tc.picks}
		got, ok := xmageTriggerOrderKeys(d)
		if tc.want == nil {
			if ok {
				t.Errorf("%s: keys = %q, want not keyable", tc.name, got)
			}
			continue
		}
		if !ok || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: keys = %q (ok %v), want %q", tc.name, got, ok, tc.want)
		}
	}
}

// TestSameSourceOrderGameplayStepKeysRuleText pins the measured Ashling,
// Rekindled/trigger#1.1 stream: the turn-3 order step scripts one rule-text
// key (consumed by XMage's order ask), and the resolve step's decline and
// colour follow in gorge's resolution order. The old verbatim texts sat
// unconsumed at the queue head and the colour dialog threw on them ("Choice
// key [Whenever this creature transforms into CARDNAME ...] not found in
// [White, Blue, Black, Red, Green]").
func TestSameSourceOrderGameplayStepKeysRuleText(t *testing.T) {
	got := XAnswers([]rules.OracleDecision{ashlingOrder(1), ashlingDecline(2), manaColourPick(2)}, 3, nil)
	key := XAnswer{0, "choice", "Whenever this creature transforms into {this} and at the beginning of your first main phase, add two mana of any one color."}
	if len(got) != 3 || len(got[1]) != 1 || got[1][0] != key {
		t.Fatalf("order step = %#v, want only the rule-text key %#v", got, key)
	}
	for _, a := range got[2] {
		if a.Value == key.Value {
			t.Fatalf("resolve step %#v carries the order key; it belongs to the order step only", got[2])
		}
	}
}
