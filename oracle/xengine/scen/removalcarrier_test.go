package scen

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/engine/cards"
)

// TestStartRemovalCarrier: a ChangesZone trigger on another creature the
// sacrifice pool cannot meet (a Goblin, a black creature, a creature carrying
// a counter, a creature leaving to hand) is a vanilla creature of that kind
// on the filter's seat and the other seat's one play, Murder or Unsummon,
// aimed at it (xproof-starts-force-the-line: the focus play is the only
// offer). The trigger zone Origin Any with Destination Graveyard ("put into a
// graveyard from anywhere") takes the same dies start.
func TestStartRemovalCarrier(t *testing.T) {
	reg, b := testBuilder(t)
	rows := []struct {
		card, key, spell string
		caster           int // the seat holding the spell: the opposite of the carrier's
		has              func(*cards.Face) bool
		counter          string // a counter kind the carrier must carry
	}{
		{"Knucklebone Witch", "f0/t0", "Murder", 1, func(f *cards.Face) bool { return f.HasTypeWord("Goblin") }, ""},
		{"Plague Belcher", "f0/t1", "Murder", 1, func(f *cards.Face) bool { return f.HasTypeWord("Zombie") }, ""},
		{"Teysa, Orzhov Scion", "f0/t0", "Murder", 1, func(f *cards.Face) bool { return hasShard(f.ManaCost, 'B') }, ""},
		{"Rampart Architect", "f0/t2", "Murder", 1, func(f *cards.Face) bool { return f.HasKeyword("Defender") }, ""},
		{"Colfenor's Urn", "f0/t0", "Murder", 1, func(f *cards.Face) bool { return f.Toughness() >= 4 }, ""},
		{"Sporogenesis", "f0/t1", "Murder", 1, func(f *cards.Face) bool { return f.IsCreature() }, "FUNGUS"},
		{"Dread Tiller", "f0/t1", "Murder", 1, func(f *cards.Face) bool { return f.Toughness() >= 2 }, "M1M1"},
		{"Taeko, the Patient Avalanche", "f0/t0", "Unsummon", 1, func(f *cards.Face) bool { return f.IsCreature() }, ""},
		{"Warped Devotion", "f0/t0", "Unsummon", 1, func(f *cards.Face) bool { return f.IsCreature() }, ""},
	}
	for _, r := range rows {
		t.Run(r.card, func(t *testing.T) {
			res, err := Generate(reg, b, r.card, r.key, Options{Prefer: "start"})
			if err != nil {
				t.Fatal(err)
			}
			if res.Path != PathStart || res.Start == nil {
				t.Fatalf("path %q reason %q, want a start file", res.Path, res.Reason)
			}
			f := res.Start
			if f.Focus == nil || f.Focus.Seat == nil || *f.Focus.Seat != r.caster || f.Focus.Card != r.spell || f.Focus.Kind != "cast" {
				t.Fatalf("focus %+v, want seat %d casting %s", f.Focus, r.caster, r.spell)
			}
			if f.Active != r.caster {
				t.Errorf("active %d, want the caster %d", f.Active, r.caster)
			}
			if len(f.Focus.Targets) != 1 {
				t.Fatalf("focus targets %v, want the one carrier", f.Focus.Targets)
			}
			carrier := f.Focus.Targets[0]
			if !slices.Contains(f.Setup.P0.Battlefield, carrier) {
				t.Errorf("p0 battlefield %v does not hold the carrier %q", f.Setup.P0.Battlefield, carrier)
			}
			if !slices.Contains(f.Setup.P1.Hand, r.spell) {
				t.Errorf("p1 hand %v, want %s", f.Setup.P1.Hand, r.spell)
			}
			c, ok := reg.Lookup(carrier)
			if !ok || !r.has(c.Faces[0]) {
				t.Errorf("carrier %q does not meet the filter", carrier)
			}
			got := false
			for _, ct := range f.Setup.P0.Counters {
				if ct.Card == carrier && ct.Kind == r.counter {
					got = true
				}
			}
			if r.counter != "" && !got {
				t.Errorf("p0 counters %+v, want %s on the carrier %q", f.Setup.P0.Counters, r.counter, carrier)
			}
			if _, err := DecodeStartFile(mustMarshal(t, f)); err != nil {
				t.Errorf("start file does not decode: %v", err)
			}
		})
	}
}

func mustMarshal(t *testing.T, f *StartFile) []byte {
	t.Helper()
	raw, err := f.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// TestStartGraveyardFromAnywhere: Origin Any with Destination Graveyard on a
// permanent is the dies start (Planar Void, Energy Field: a card put into a
// graveyard from anywhere), met by the sacrifice carrier, not a removal spell.
func TestStartGraveyardFromAnywhere(t *testing.T) {
	reg, b := testBuilder(t)
	for _, card := range []string{"Planar Void", "Energy Field"} {
		t.Run(card, func(t *testing.T) {
			res, err := Generate(reg, b, card, "f0/t0", Options{Prefer: "start"})
			if err != nil || res.Start == nil {
				t.Fatalf("%+v %v", res, err)
			}
			f := res.Start
			if f.Focus == nil || f.Focus.Kind != "ability" || f.Focus.Seat == nil || *f.Focus.Seat != 0 || f.Active != 0 {
				t.Errorf("focus %+v active %d, want seat 0 sacrificing its carrier", f.Focus, f.Active)
			}
			if len(f.Setup.P1.Hand) != 0 {
				t.Errorf("p1 hand %v, want no spell: the sacrifice carrier is the only play", f.Setup.P1.Hand)
			}
		})
	}
}

// TestStartRemovalCarrierOtherRefusalsUnchanged: the shapes the removal start
// does not serve keep the refusal they had (a type no vanilla creature has, a
// state the start cannot place, a trigger zone off the battlefield, an
// attachment or Equipment filter, a tapped permanent), and the plain
// sacrifice carrier and aura starts are untouched. The four starts at the
// bottom are the shapes the wider fixture lanes now serve (a land dies to
// Stone Rain, HasCounters places a counter gate, an Equipment enters as a
// fixture cast, a Cartouche enters cast at a bearer): they left the refusal
// list when those arms landed and stay pinned here against regression.
func TestStartRemovalCarrierOtherRefusalsUnchanged(t *testing.T) {
	const filter = "no-generator:trigger:ChangesZone:filter"
	const plain = "no-generator:trigger:ChangesZone"
	checkStartRows(t, []startRow{
		{card: "Indebted Samurai", key: "f0/t0", reason: filter},    // no vanilla Samurai
		{card: "Kithkin Mourncaller", key: "f0/t0", reason: filter}, // attacking: battlefield state
		{card: "Amulet of Vigor", key: "f0/t0", reason: filter},     // a tapped permanent entering
		{card: "Twilight Drover", key: "f0/t0", reason: filter},     // a token dies
		{card: "Nether Traitor", key: "f0/t0", reason: plain},       // trigger zone Graveyard
		{card: "Silvan Reveler", key: "f0/t1", reason: plain},       // trigger zone Graveyard
		{card: "Gift of Wrath", key: "f0/t0", reason: plain},        // Card.Self leaves: not this trigger
		{card: "Cryptid Inspector", key: "f0/t0", reason: filter},   // an enters filter the removal start does not touch
		{card: "Garna, Bloodfist of Keld", key: "f0/t0", maxTurns: 1, p0BF: []string{"Garna, Bloodfist of Keld", "Kami of False Hope"},
			focusCard: "Kami of False Hope", focusKind: "ability"}, // the sacrifice carrier still wins where it fits
		{card: "Akki Raider", key: "f0/t0", active: 1, maxTurns: 1,
			p0BF: []string{"Akki Raider", "Plains"}, p1Hand: []string{"Stone Rain"}, p1BF: rep("Mountain", 3),
			focusSeat: 1, focusCard: "Stone Rain", focusKind: "cast"}, // a land dies: Stone Rain, not Murder
		{card: "Village Pillagers", key: "f0/t1", maxTurns: 1,
			p0Hand: []string{"Murder"}, p0BF: []string{"Village Pillagers", "Swamp", "Swamp", "Swamp"}, p1BF: []string{"Memnite"},
			focusCard: "Murder", focusKind: "cast"}, // a countered opponent creature dies (the gate is placed)
		{card: "Barret, Avalanche Leader", key: "f0/t0", maxTurns: 1,
			p0Hand:    []string{"Bloodthirsty Blade"},
			p0BF:      []string{"Barret, Avalanche Leader", "Plains", "Plains", "Freedom Fighter Recruit", "Kite Shield"},
			focusCard: "Bloodthirsty Blade", focusKind: "cast"}, // an Equipment enters: the fixture cast
		{card: "Trial of Ambition", key: "f0/t1", maxTurns: 1,
			p0Hand: []string{"Cartouche of Solidarity"}, p0BF: []string{"Trial of Ambition", "Plains", "Memnite"},
			focusCard: "Cartouche of Solidarity", focusKind: "cast"}, // a Cartouche enters, cast at its bearer
	})
}
