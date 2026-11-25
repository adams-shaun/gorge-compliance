package scen

import (
	"bytes"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/certdeck"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/lower"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

const manifests = "../../compliance/manifests"

func testBuilder(t *testing.T) (*cards.Registry, *certdeck.Builder) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	sp, err := decks.LoadSpellings(manifests)
	if err != nil {
		t.Fatal(err)
	}
	return reg, certdeck.NewBuilder(reg, sp)
}

func rep(name string, n int) []string {
	var out []string
	for i := 0; i < n; i++ {
		out = append(out, name)
	}
	return out
}

func cat(parts ...[]string) []string {
	var out []string
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

type want struct {
	path, reason string
	// check, for a start result, asserts the file's placements.
	check func(t *testing.T, f *StartFile)
}

func focusIs(t *testing.T, f *StartFile, seat int, card, kind string, ability int) {
	t.Helper()
	if f.Focus == nil || f.Focus.Seat == nil {
		t.Fatalf("focus missing a seat: %+v", f.Focus)
	}
	if *f.Focus.Seat != seat || f.Focus.Card != card || f.Focus.Kind != kind || f.Focus.Ability != ability {
		t.Errorf("focus = seat %d %q %q ability %d, want seat %d %q %q ability %d",
			*f.Focus.Seat, f.Focus.Card, f.Focus.Kind, f.Focus.Ability, seat, card, kind, ability)
	}
}

func listIs(t *testing.T, what string, got, want []string) {
	t.Helper()
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

// Needs certdeck.Derive returns for the fixture cards (printed from Derive and
// Build at this pin): Thornvault Forager [type:Squirrel graveyard] (enabler
// Frog-Squirrels); Crypt Feaster [graveyard opp-blockers] (certdeck's
// first pick, Shifting Wall, is a 0/0 X creature that dies when placed, so the
// enabler on p1 is Shield Sphere); Banner of Kinship [counters:FELLOWSHIP]; every other fixture
// card none.
func TestScenFixtures(t *testing.T) {
	reg, b := testBuilder(t)
	rows := []struct {
		card, key string
		auto      want
		start     want
	}{
		{"Thornvault Forager", "f0/a2", want{path: "steps"}, want{path: "start", check: func(t *testing.T, f *StartFile) {
			listIs(t, "p0 battlefield", f.Setup.P0.Battlefield, cat([]string{"Thornvault Forager"}, rep("Forest", 4), []string{"Frog-Squirrels"}))
			listIs(t, "p0 graveyard", f.Setup.P0.Graveyard, rep("Forest", 7))
			focusIs(t, f, 0, "Thornvault Forager", "ability", 2)
		}}},
		{"Thornvault Forager", "f0/a1", want{path: "steps"}, want{path: "refused", reason: "steps-needed:cost:Forage"}},
		{"Llanowar Elves", "f0/a0", want{path: "steps"}, want{path: "start", check: func(t *testing.T, f *StartFile) {
			listIs(t, "p0 battlefield", f.Setup.P0.Battlefield, []string{"Llanowar Elves"})
			focusIs(t, f, 0, "Llanowar Elves", "ability", 0)
		}}},
		{"Inspiring Overseer", "f0/t0", want{path: "start", check: overseer}, want{path: "start", check: overseer}},
		{"Crypt Feaster", "f0/t0", want{path: "steps"}, want{path: "start", check: func(t *testing.T, f *StartFile) {
			listIs(t, "p0 battlefield", f.Setup.P0.Battlefield, []string{"Crypt Feaster"})
			listIs(t, "p0 graveyard", f.Setup.P0.Graveyard, rep("Swamp", 7))
			listIs(t, "p1 battlefield", f.Setup.P1.Battlefield, []string{"Shield Sphere"})
			focusIs(t, f, 0, "Crypt Feaster", "attack", 0)
		}}},
		// an upkeep trigger is the card cast on turn 1 and the game run to its
		// controller's next upkeep (turn 3): placed on the battlefield, the
		// lowering refuses it at the cut (trigger:upkeep_or_draw)
		{"Phyrexian Arena", "f0/t0", want{path: "steps"}, want{path: "start", check: func(t *testing.T, f *StartFile) {
			listIs(t, "p0 hand", f.Setup.P0.Hand, []string{"Phyrexian Arena"})
			listIs(t, "p0 battlefield", f.Setup.P0.Battlefield, rep("Swamp", 3))
			focusIs(t, f, 0, "Phyrexian Arena", "cast", 0)
			if f.Active != 0 || f.MaxTurns != 3 {
				t.Errorf("active %d max_turns %d, want 0 and 3", f.Active, f.MaxTurns)
			}
		}}},
		{"Gleaming Barrier", "f0/t0", want{path: "steps"}, want{path: "start", check: func(t *testing.T, f *StartFile) {
			listIs(t, "p0 battlefield", f.Setup.P0.Battlefield, []string{"Gleaming Barrier"})
			listIs(t, "p1 hand", f.Setup.P1.Hand, []string{"Murder"})
			listIs(t, "p1 battlefield", f.Setup.P1.Battlefield, rep("Swamp", 3))
			focusIs(t, f, 1, "Murder", "cast", 0)
			if f.Active != 1 {
				t.Errorf("active %d, want 1", f.Active)
			}
		}}},
		{"Scoured Barrens", "land", want{path: "start", check: barrens}, want{path: "start", check: barrens}},
		{"Aggressive Mammoth", "static#0.0", want{path: "steps"}, want{path: "start", check: mammothStatic}},
		{"Magnigoth Sentry", "combat#0.block", want{path: "steps"}, want{path: "start", check: sentryBlock}},
		{"Magnigoth Sentry", "combat#0.attack", want{path: "steps"}, want{path: "start", check: sentryAttack}},
		{"Banner of Kinship", "static#0.0", want{path: "steps"}, want{path: "refused", reason: "as-enters"}},
		{"Gatekeeper of Malakir", "f0/t0", want{path: "start", check: gatekeeper}, want{path: "start", check: gatekeeper}},
	}
	for _, r := range rows {
		for _, prefer := range []string{"auto", "start"} {
			w := r.auto
			if prefer == "start" {
				w = r.start
			}
			t.Run(r.card+"/"+r.key+"/"+prefer, func(t *testing.T) {
				res, err := Generate(reg, b, r.card, r.key, Options{Prefer: prefer})
				if err != nil {
					t.Fatal(err)
				}
				if res.Path != w.path || res.Reason != w.reason {
					t.Fatalf("path %q reason %q, want %q %q", res.Path, res.Reason, w.path, w.reason)
				}
				switch res.Path {
				case "steps":
					if res.Steps == nil || res.Steps.ID == "" || res.Start != nil {
						t.Fatalf("steps result malformed: %+v", res)
					}
				case "start":
					if res.Start == nil || res.Steps != nil {
						t.Fatalf("start result malformed: %+v", res)
					}
					if w.check != nil {
						w.check(t, res.Start)
					}
				}
			})
		}
	}
}

func overseer(t *testing.T, f *StartFile) {
	listIs(t, "p0 hand", f.Setup.P0.Hand, []string{"Inspiring Overseer"})
	listIs(t, "p0 battlefield", f.Setup.P0.Battlefield, rep("Plains", 3))
	focusIs(t, f, 0, "Inspiring Overseer", "cast", 0)
}

// mammothStatic: "other creatures you control have trample" reads a second
// creature, which the start places beside the card; no action is forced.
func mammothStatic(t *testing.T, f *StartFile) {
	// "other creatures you control have trample": the card and a creature carrier
	listIs(t, "p0 battlefield", f.Setup.P0.Battlefield, []string{"Aggressive Mammoth", "Memnite"})
	if f.Focus != nil && f.Focus.Kind != "" {
		t.Errorf("focus %+v, want no forced action", f.Focus)
	}
	// the grant is a keyword: observable only when the item compares keywords
	if !reflect.DeepEqual(f.Compare, []string{"keywords"}) {
		t.Errorf("compare %v, want [keywords]", f.Compare)
	}
}

// sentryAttack: the card attacks into the fixture blocker.
func sentryAttack(t *testing.T, f *StartFile) {
	listIs(t, "p0 battlefield", f.Setup.P0.Battlefield, []string{"Magnigoth Sentry"})
	listIs(t, "p1 battlefield", f.Setup.P1.Battlefield, []string{"Grizzly Bears"})
	focusIs(t, f, 0, "Magnigoth Sentry", "attack", 0)
	if f.Active != 0 {
		t.Errorf("active %d, want 0", f.Active)
	}
}

// sentryBlock: the opponent is active and its fixture attacks the card.
func sentryBlock(t *testing.T, f *StartFile) {
	listIs(t, "p0 battlefield", f.Setup.P0.Battlefield, []string{"Magnigoth Sentry"})
	listIs(t, "p1 battlefield", f.Setup.P1.Battlefield, []string{"Grizzly Bears"})
	focusIs(t, f, 0, "Magnigoth Sentry", "block", 0)
	if f.Active != 1 {
		t.Errorf("active %d, want 1", f.Active)
	}
}

func barrens(t *testing.T, f *StartFile) {
	listIs(t, "p0 hand", f.Setup.P0.Hand, []string{"Scoured Barrens"})
	focusIs(t, f, 0, "Scoured Barrens", "land", 0)
}

// fixtureKeys are the start-path rows of the fixture table.
var fixtureKeys = [][2]string{
	{"Thornvault Forager", "f0/a2"}, {"Llanowar Elves", "f0/a0"}, {"Inspiring Overseer", "f0/t0"},
	{"Crypt Feaster", "f0/t0"}, {"Phyrexian Arena", "f0/t0"}, {"Gleaming Barrier", "f0/t0"}, {"Scoured Barrens", "land"},
}

// A start result marshals to bytes the strict decoder accepts and that decode
// back to the same file.
func TestScenStartFileRoundTrip(t *testing.T) {
	reg, b := testBuilder(t)
	for _, k := range fixtureKeys {
		res, err := Generate(reg, b, k[0], k[1], Options{Prefer: "start"})
		if err != nil || res.Path != PathStart {
			t.Fatalf("%v: %+v %v", k, res, err)
		}
		raw, err := res.Start.Marshal()
		if err != nil {
			t.Fatal(err)
		}
		back, err := DecodeStartFile(raw)
		if err != nil {
			t.Fatalf("%v: %v\n%s", k, err, raw)
		}
		if !reflect.DeepEqual(back, res.Start) {
			t.Errorf("%v: round trip differs:\n%+v\n%+v", k, back, res.Start)
		}
	}
	if _, err := DecodeStartFile([]byte(`{"format":"xproof-start-2","card":"A","key":"cast"}`)); err == nil {
		t.Error("a foreign format decoded")
	}
	if _, err := DecodeStartFile([]byte(`{"format":"xproof-start-1","card":"A","key":"cast","bogus":1}`)); err == nil {
		t.Error("an unknown field decoded")
	}
}

// Two Generate calls give identical bytes, for a start and a steps result.
func TestScenDeterministic(t *testing.T) {
	reg, b := testBuilder(t)
	gen := func(card, key, prefer string) []byte {
		res, err := Generate(reg, b, card, key, Options{Prefer: prefer, Set: "FDN"})
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(res)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	for _, k := range append([][2]string{
		{"Thornvault Forager", "f0/a1"}, {"Banner of Kinship", "static#0.0"},
		{"Adagia, Windswept Bastion", "static#0.0"}, {"Adelbert Steiner", "static#0.0"},
		{"Alien Symbiosis", "static#0.1"}, {"Archmage of Runes", "static#0.0"},
		{"Omniscience", "static#0.0"}, {"Enigma Drake", "static#0.0"},
	}, fixtureKeys...) {
		for _, prefer := range []string{"auto", "start"} {
			if a, c := gen(k[0], k[1], prefer), gen(k[0], k[1], prefer); !bytes.Equal(a, c) {
				t.Errorf("%v %s differs between calls", k, prefer)
			}
		}
	}
	res, _ := Generate(reg, b, "Llanowar Elves", "f0/a0", Options{Set: "FDN"})
	if res.Steps == nil || res.Steps.ID != "xs:FDN:llanowar-elves:f0a0:steps" {
		t.Errorf("steps id: %+v", res.Steps)
	}
}

// TestStartForcedPlacements: a start meets each need with placed state where
// it can, so the only plays it offers are the ones that exercise the key
// (the forced start, recipe.go). An instant, sorcery or Aura enabler never
// sits on the battlefield: listed there, gorge left Path to Exile on the
// battlefield and dropped Strange Augmentation from the library while Forge
// put both in hand (xp-setup-extra-hand-card). Beza: opp-burn is p0's life
// lowered, not Taste of Blood that p1 cannot cast; lands are two tapped
// Plains; life:total is Lunarch Veteran, the pick with nothing to activate
// (Fountain of Youth would compete for Beza's mana). Lord Skitter's Blessing:
// the Aura is cast from hand with its Swamp and an inert bearer. Coliseum
// Behemoth: Vampiric Rites, a placeable pick, over the Aura Kaya's Ghostform.
// Aven Interrupter: p1's spell to exile is the first instant pick (Howl from
// Beyond, not the sorcery Indicate p1 cannot cast on p0's turn), with its
// Swamp.
func TestStartForcedPlacements(t *testing.T) {
	reg, b := testBuilder(t)
	life := func(f *StartFile) int32 {
		if f.Setup.P0.Life == nil {
			return 20
		}
		return *f.Setup.P0.Life
	}
	rows := []struct {
		card, key              string
		p0Hand, p0BF, p0Tapped []string
		p0Life                 int32
		p1Hand, p1BF           []string
	}{
		{"Beza, the Bounding Spring", "cast",
			[]string{"Beza, the Bounding Spring"},
			cat(rep("Plains", 4), []string{"Lunarch Veteran"}, rep("Plains", 2)), rep("Plains", 2), 17,
			nil, nil},
		{"Lord Skitter's Blessing", "f0/t0",
			[]string{"Lord Skitter's Blessing", "Strange Augmentation"},
			cat(rep("Swamp", 3), []string{"Wall of Stone"}), nil, 20,
			nil, nil},
		{"Coliseum Behemoth", "f0/t0",
			[]string{"Coliseum Behemoth"}, rep("Forest", 7), nil, 20,
			nil, []string{"Fountain of Youth", "Vampiric Rites"}},
		{"Aven Interrupter", "f0/t0",
			[]string{"Aven Interrupter"}, rep("Plains", 3), nil, 20,
			[]string{"Howl from Beyond"}, []string{"Swamp"}},
	}
	for _, r := range rows {
		t.Run(r.card, func(t *testing.T) {
			res, err := Generate(reg, b, r.card, r.key, Options{Prefer: "start"})
			if err != nil {
				t.Fatal(err)
			}
			if res.Start == nil {
				t.Fatalf("path %q reason %q, want a start file", res.Path, res.Reason)
			}
			f := res.Start
			listIs(t, "p0 hand", f.Setup.P0.Hand, r.p0Hand)
			listIs(t, "p0 battlefield", f.Setup.P0.Battlefield, r.p0BF)
			listIs(t, "p0 tapped", f.Setup.P0.Tapped, r.p0Tapped)
			if got := life(f); got != r.p0Life {
				t.Errorf("p0 life %d, want %d", got, r.p0Life)
			}
			listIs(t, "p1 hand", f.Setup.P1.Hand, r.p1Hand)
			listIs(t, "p1 battlefield", f.Setup.P1.Battlefield, r.p1BF)
			for _, bf := range [][]string{f.Setup.P0.Battlefield, f.Setup.P1.Battlefield} {
				for _, n := range bf {
					c, _ := reg.Lookup(n)
					if why := FieldBlock(c.Faces[0]); why != "" {
						t.Errorf("%s listed on the battlefield: %s", n, why)
					}
				}
			}
		})
	}
}

// gatekeeper: "if it was kicked" is met by the line itself, a kicked cast:
// lands for {B}{B} and the kicker {B}, and the focus takes the kicked option.
func gatekeeper(t *testing.T, f *StartFile) {
	listIs(t, "p0 hand", f.Setup.P0.Hand, []string{"Gatekeeper of Malakir"})
	listIs(t, "p0 battlefield", f.Setup.P0.Battlefield, rep("Swamp", 3))
	focusIs(t, f, 0, "Gatekeeper of Malakir", "cast", 0)
	if f.Focus.Mode != "kicked" {
		t.Errorf("focus mode %q, want kicked", f.Focus.Mode)
	}
}

// TestStartHistoryAndCounters: a counters need is placed counters (on an inert
// bearer when the card is not on the battlefield), a cast-history key is the
// focus's own cast of the card from hand, and a narrower cast history the
// line cannot produce stays refused by name.
func TestStartHistoryAndCounters(t *testing.T) {
	reg, b := testBuilder(t)
	rows := []struct {
		card, key, reason string
		hand, bf          []string
		counters          []StartCounter
	}{
		{card: "Badgermole", key: "f0/t0", hand: []string{"Badgermole"},
			bf: cat(rep("Forest", 5), []string{"Wall of Stone"}), counters: []StartCounter{{Card: "Wall of Stone", Kind: "P1P1", N: 1}}},
		{card: "North Wind Avatar", key: "f0/t0", hand: []string{"North Wind Avatar"},
			bf: cat(rep("Island", 4), []string{"Mountain"})},
		// "Card.wasCastByYou+Self": Self is not the first property
		{card: "Weftwalking", key: "f0/t0", hand: []string{"Weftwalking"}, bf: rep("Island", 6)},
		{card: "Spiders-Man, Heroic Horde", key: "f0/t0", reason: "history:websling"},
		{card: "Rocket-Powered Goblin Glider", key: "f0/t0", reason: "history:castfrom"},
	}
	for _, r := range rows {
		t.Run(r.card, func(t *testing.T) {
			res, err := Generate(reg, b, r.card, r.key, Options{})
			if err != nil {
				t.Fatal(err)
			}
			if r.reason != "" {
				if res.Path != PathRefused || res.Reason != r.reason {
					t.Fatalf("path %q reason %q, want refused %q", res.Path, res.Reason, r.reason)
				}
				return
			}
			if res.Start == nil {
				t.Fatalf("path %q reason %q, want a start file", res.Path, res.Reason)
			}
			f := res.Start
			focusIs(t, f, 0, r.card, "cast", 0)
			listIs(t, "p0 hand", f.Setup.P0.Hand, r.hand)
			listIs(t, "p0 battlefield", f.Setup.P0.Battlefield, r.bf)
			if !reflect.DeepEqual(f.Setup.P0.Counters, r.counters) {
				t.Errorf("p0 counters %+v, want %+v", f.Setup.P0.Counters, r.counters)
			}
		})
	}
}

// TestStartPlayerCounters: a counters need whose removal reads a player
// (Final Act's "each opponent loses all counters", Survivor's Med Kit's
// RadAway on a player) cannot be seeded by a start file, which places counters
// only on battlefield cards (rules.StartSeat has no player-counter field). The
// key is refused by name instead of putting the counter on a permanent the
// removal never reads. Two permanent-counter cards still get a placed
// permanent on the seat their removal reads.
func TestStartPlayerCounters(t *testing.T) {
	reg, b := testBuilder(t)
	for _, tc := range []struct{ card, key string }{
		{"Final Act", "cast"},
		{"Survivor's Med Kit", "f0/a0"},
	} {
		t.Run(tc.card, func(t *testing.T) {
			res, err := Generate(reg, b, tc.card, tc.key, Options{Prefer: "start"})
			if err != nil {
				t.Fatal(err)
			}
			if res.Path != PathRefused || res.Reason != reasonPlayerCounters {
				t.Fatalf("path %q reason %q, want refused %q", res.Path, res.Reason, reasonPlayerCounters)
			}
		})
	}
	for _, tc := range []struct{ card, key string }{
		{"Honest Work", "f0/t0"},
		{"Mabel, Bitter Recluse", "f0/t0"},
	} {
		t.Run(tc.card, func(t *testing.T) {
			res, err := Generate(reg, b, tc.card, tc.key, Options{Prefer: "start"})
			if err != nil {
				t.Fatal(err)
			}
			if res.Path != PathStart || res.Start == nil {
				t.Fatalf("path %q reason %q, want a start file", res.Path, res.Reason)
			}
			cs := res.Start.Setup.P1.Counters
			if len(cs) != 1 || cs[0].Kind != "P1P1" || !slices.Contains(res.Start.Setup.P1.Battlefield, cs[0].Card) {
				t.Errorf("p1 counters %+v on battlefield %v, want one P1P1 on a placed permanent",
					cs, res.Start.Setup.P1.Battlefield)
			}
		})
	}
}

// TestScenUnlistedByXMageIsGenerated: a card no XMage manifest lists is still
// generated, not refused not-sent. Forge is the reference oracle (operator
// 11-24): the XMage leg marks the item not_sent and keyproof credits a Forge
// agreement refs F. The refusal owed about a fifth of each xproof cycle.
func TestScenUnlistedByXMageIsGenerated(t *testing.T) {
	reg, b := testBuilder(t)
	const card = "Croakid Amphibonaut"
	if b.Sp.Lists(reg, card) {
		t.Fatalf("%s is listed by an XMage manifest now: pick another unlisted card", card)
	}
	res, err := Generate(reg, b, card, "cast", Options{Set: "SCEN"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Path == "refused" {
		t.Errorf("%s cast refused %q, want a steps or start scenario", card, res.Reason)
	}
}

// TestStartxmagemissing is the xproof repro: every example card no XMage
// manifest lists must still get a start file. A card with needs (Azog's
// trigger, a cast that reads the graveyard) used to refuse in certdeck.Build
// before a start was written, so Forge, XMage and gorge never played the key;
// the XMage leg is already marked not_sent downstream (xprep's unlisted) and
// keyproof credits a Forge agreement refs F. Brazen Bluff is not here: its
// focus targets a spell on the stack, which no start can hold, so it refuses
// with start:needs-stack-target whatever XMage lists.
func TestStartxmagemissing(t *testing.T) {
	reg, b := testBuilder(t)
	examples := []struct{ card, key string }{
		{"Azog, Moria's Ruin", "f0/t0"},
		{"Blastoneuron", "cast"},
		{"Borg Cube", "cast"},
		{"Brad Boimler, Eager Ensign", "cast"},
		{"Branching Timeline", "cast"},
	}
	absent, starts := 0, 0
	for _, tc := range examples {
		if _, ok := b.Sp.XMage(reg, tc.card); ok {
			continue // XMage lists the card now: the XMage leg is no longer inapplicable
		}
		absent++
		res, err := Generate(reg, b, tc.card, tc.key, Options{Prefer: "start"})
		if err != nil {
			t.Errorf("%s %s: %v", tc.card, tc.key, err)
			continue
		}
		if res.Path != PathStart || res.Start == nil {
			t.Errorf("%s %s path %q reason %q, want a start file", tc.card, tc.key, res.Path, res.Reason)
			continue
		}
		starts++
	}
	if absent < 2 || starts < 2 {
		t.Fatalf("only %d example cards are unlisted and %d got a start; the repro needs at least two of each", absent, starts)
	}
}

// startRow is one forced-line start: the placements it must make and the
// focus that makes the key's event happen.
type startRow struct {
	card, key, reason       string
	active, maxTurns        int
	p0Hand, p0BF, p1Hand    []string
	p1BF                    []string
	focusSeat               int
	focusCard, focusKind    string
	focusAbility, focusTime int
}

func checkStartRows(t *testing.T, rows []startRow) {
	t.Helper()
	reg, b := testBuilder(t)
	for _, r := range rows {
		t.Run(r.card+"/"+r.key, func(t *testing.T) {
			res, err := Generate(reg, b, r.card, r.key, Options{Prefer: "start"})
			if err != nil {
				t.Fatal(err)
			}
			if r.reason != "" {
				if res.Path != PathRefused || res.Reason != r.reason {
					t.Fatalf("path %q reason %q, want refused %q", res.Path, res.Reason, r.reason)
				}
				return
			}
			if res.Start == nil {
				t.Fatalf("path %q reason %q, want a start file", res.Path, res.Reason)
			}
			f := res.Start
			if f.Active != r.active || f.MaxTurns != r.maxTurns {
				t.Errorf("active %d max_turns %d, want %d and %d", f.Active, f.MaxTurns, r.active, r.maxTurns)
			}
			listIs(t, "p0 hand", f.Setup.P0.Hand, r.p0Hand)
			listIs(t, "p0 battlefield", f.Setup.P0.Battlefield, r.p0BF)
			listIs(t, "p1 hand", f.Setup.P1.Hand, r.p1Hand)
			listIs(t, "p1 battlefield", f.Setup.P1.Battlefield, r.p1BF)
			if r.focusKind == "" {
				if f.Focus != nil {
					t.Errorf("focus %+v, want none", f.Focus)
				}
				return
			}
			focusIs(t, f, r.focusSeat, r.focusCard, r.focusKind, r.focusAbility)
			if f.Focus.Times != r.focusTime {
				t.Errorf("focus times %d, want %d", f.Focus.Times, r.focusTime)
			}
			raw, err := f.Marshal()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := DecodeStartFile(raw); err != nil {
				t.Errorf("start file does not decode: %v", err)
			}
		})
	}
}

// TestStartPhaseTriggers: a beginning-of-step trigger later in turn 1 than
// the span's cut (beginning of combat, the end step) is the card on the
// battlefield; "if it's tapped" (the DSK Survivors' second main phase) is the
// card attacking; a step before the cut (draw, the first main phase) is the
// card cast on turn 1 and the game run to that step's next turn for the
// player ValidPlayer names (turn 3 for you, turn 2 for any player). A
// descended or Class-level trigger stays refused by name. Carriers: the
// cards are from the DSK/BLB/LCI refusal tail of the 11-24 census.
func TestStartPhaseTriggers(t *testing.T) {
	checkStartRows(t, []startRow{
		{card: "Sinister Monolith", key: "f0/t0", maxTurns: 1, p0BF: []string{"Sinister Monolith"}},
		{card: "Cautious Survivor", key: "f0/t0", maxTurns: 1, p0BF: []string{"Cautious Survivor"},
			focusCard: "Cautious Survivor", focusKind: "attack"},
		{card: "Hulking Raptor", key: "f0/t0", maxTurns: 3, p0Hand: []string{"Hulking Raptor"}, p0BF: rep("Forest", 4),
			focusCard: "Hulking Raptor", focusKind: "cast"},
		{card: "Dictate of Kruphix", key: "f0/t0", maxTurns: 2, p0Hand: []string{"Dictate of Kruphix"}, p0BF: rep("Island", 3),
			focusCard: "Dictate of Kruphix", focusKind: "cast"},
		{card: "Bandit's Talent", key: "f0/t2", reason: "steps-needed:class-level"},
	})
}

// TestStartZoneTriggers: a ChangesZone trigger on another card. An enters
// trigger is the carrier entering: a basic played for landfall (Icetill
// Explorer; the lands need adds two tapped Forests), Memnite ({0} vanilla
// artifact creature) cast for a creature, Nyxborn Courser (vanilla
// enchantment creature) for an enchantment, and on the opponent's turn for
// an OppCtrl filter (Authority of the Consuls). A dies trigger is Kami of
// False Hope sacrificing itself (Garna). An unplaceable filter is refused.
func TestStartZoneTriggers(t *testing.T) {
	checkStartRows(t, []startRow{
		{card: "Dazzling Angel", key: "f0/t0", maxTurns: 1, p0Hand: []string{"Memnite"}, p0BF: []string{"Dazzling Angel"},
			focusCard: "Memnite", focusKind: "cast"},
		{card: "Icetill Explorer", key: "f0/t0", maxTurns: 1, p0Hand: []string{"Forest"},
			p0BF:      cat([]string{"Icetill Explorer"}, rep("Forest", 2)),
			focusCard: "Forest", focusKind: "land"},
		{card: "Authority of the Consuls", key: "f0/t0", active: 1, maxTurns: 1, p0BF: []string{"Authority of the Consuls"},
			p1Hand: []string{"Memnite"}, focusSeat: 1, focusCard: "Memnite", focusKind: "cast"},
		{card: "Garna, Bloodfist of Keld", key: "f0/t0", maxTurns: 1, p0BF: []string{"Garna, Bloodfist of Keld", "Kami of False Hope"},
			focusCard: "Kami of False Hope", focusKind: "ability"},
		{card: "Cryptid Inspector", key: "f0/t0", reason: "no-generator:trigger:ChangesZone:filter"},
	})
	reg, b := testBuilder(t)
	res, err := Generate(reg, b, "Entity Tracker", "f0/t0", Options{Prefer: "start"})
	if err != nil || res.Start == nil {
		t.Fatalf("Entity Tracker: %+v %v", res, err)
	}
	if f := res.Start; !slices.Equal(f.Setup.P0.Hand, []string{"Nyxborn Courser"}) || f.Focus.Card != "Nyxborn Courser" ||
		!slices.Equal(f.Setup.P0.Battlefield[:4], cat([]string{"Entity Tracker"}, rep("Plains", 3))) {
		t.Errorf("Entity Tracker: hand %v battlefield %v focus %+v, want Nyxborn Courser and its three Plains",
			f.Setup.P0.Hand, f.Setup.P0.Battlefield, f.Focus)
	}
}

// TestStartzonetriggers is the dispatch's repro spelling. It covers the
// ChangesZone/ChangesZoneAll causes the plain carrier recipes missed: a token
// entering (Anointer Priest, Advent of the Wurm cast), an Aura attached and
// destroyed (Aspect of Mongoose, Banewasp Affliction), a creature damaged by
// the card and then dying (Abattoir Ghoul), a card leaving a graveyard
// (Attuned Hunter, Regrowth), and an Equipment attached then losing its bearer
// (Avarice Amulet, Magnetic Theft and Ball Lightning's end-step sacrifice).
// The destruction focuses name their target: the key Aura or creature, its
// bearer, or (Magnetic Theft) the Equipment and the sacrificial bearer.
func TestStartzonetriggers(t *testing.T) {
	reg, b := testBuilder(t)
	rows := []struct {
		card, key string
		check     func(*testing.T, *StartFile)
	}{
		{"Abattoir Ghoul", "f0/t0", func(t *testing.T, f *StartFile) {
			if f.Focus == nil || f.Focus.Attack != "Abattoir Ghoul" || f.Focus.Block == "" || f.Active != 0 {
				t.Fatalf("focus %+v active %d, want the Ghoul attacking into a blocker", f.Focus, f.Active)
			}
			if !slices.Contains(f.Setup.P1.Battlefield, f.Focus.Block) {
				t.Errorf("p1 battlefield %v does not hold the blocker %q", f.Setup.P1.Battlefield, f.Focus.Block)
			}
		}},
		{"Anointer Priest", "f0/t0", func(t *testing.T, f *StartFile) {
			if f.Focus == nil || f.Focus.Kind != "cast" || len(f.Setup.P0.Hand) != 1 || f.Focus.Card != f.Setup.P0.Hand[0] {
				t.Fatalf("focus %+v hand %v, want a token-maker cast", f.Focus, f.Setup.P0.Hand)
			}
			if len(f.Setup.P0.Battlefield) == 0 || f.Setup.P0.Battlefield[0] != "Anointer Priest" {
				t.Errorf("p0 battlefield %v, want Anointer Priest first", f.Setup.P0.Battlefield)
			}
		}},
		{"Attuned Hunter", "f0/t0", func(t *testing.T, f *StartFile) {
			if f.Focus == nil || f.Focus.Kind != "cast" || f.Focus.Card != "Regrowth" {
				t.Fatalf("focus %+v, want Regrowth cast", f.Focus)
			}
			if len(f.Setup.P0.Graveyard) == 0 {
				t.Error("p0 graveyard is empty, so nothing can leave it")
			}
			if len(f.Setup.P0.Battlefield) == 0 || f.Setup.P0.Battlefield[0] != "Attuned Hunter" {
				t.Errorf("p0 battlefield %v, want Attuned Hunter first", f.Setup.P0.Battlefield)
			}
		}},
		{"Aspect of Mongoose", "f0/t0", func(t *testing.T, f *StartFile) {
			focusIs(t, f, 1, "Naturalize", "cast", 0)
			attached(t, f, "Aspect of Mongoose")
			if !slices.Equal(f.Focus.Targets, []string{"Aspect of Mongoose"}) {
				t.Errorf("focus targets %v, want the Aura itself", f.Focus.Targets)
			}
		}},
		{"Avarice Amulet", "f0/t0", func(t *testing.T, f *StartFile) {
			focusIs(t, f, 0, "Magnetic Theft", "cast", 0)
			if !slices.Contains(f.Setup.P0.Battlefield, "Avarice Amulet") ||
				!slices.Contains(f.Setup.P0.Battlefield, "Ball Lightning") {
				t.Errorf("p0 battlefield %v, want Avarice Amulet and Ball Lightning", f.Setup.P0.Battlefield)
			}
			if !slices.Equal(f.Focus.Targets, []string{"Avarice Amulet", "Ball Lightning"}) {
				t.Errorf("focus targets %v, want the Equipment then Ball Lightning", f.Focus.Targets)
			}
		}},
		{"Banewasp Affliction", "f0/t0", func(t *testing.T, f *StartFile) {
			focusIs(t, f, 1, "Murder", "cast", 0)
			attached(t, f, "Banewasp Affliction")
			if want := attachBearerOf(t, f, "Banewasp Affliction"); !slices.Equal(f.Focus.Targets, []string{want}) {
				t.Errorf("focus targets %v, want the attached bearer %q", f.Focus.Targets, want)
			}
		}},
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
			raw, err := res.Start.Marshal()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := DecodeStartFile(raw); err != nil {
				t.Fatalf("start file does not decode: %v\n%s", err, raw)
			}
			r.check(t, res.Start)
		})
	}
}

// attached asserts the file places card attached to a bearer on the same seat.
func attached(t *testing.T, f *StartFile, card string) {
	t.Helper()
	for _, a := range f.Setup.P0.Attach {
		bearers := f.Setup.P0.Battlefield
		if a.Opp {
			bearers = f.Setup.P1.Battlefield
		}
		if a.Card == card && a.To != "" && slices.Contains(bearers, a.To) {
			return
		}
	}
	t.Errorf("p0 attach %+v battlefield %v, want %q attached to a placed bearer", f.Setup.P0.Attach, f.Setup.P0.Battlefield, card)
}

// attachBearerOf is the bearer card is attached to, or "".
func attachBearerOf(t *testing.T, f *StartFile, card string) string {
	t.Helper()
	for _, a := range f.Setup.P0.Attach {
		if a.Card == card {
			return a.To
		}
	}
	return ""
}

// TestStartAttachedBearerSeat: an Aura whose Enchant keyword is "creature an
// opponent controls" (Dead Man's Chest) is attached to a bearer on the
// opponent's seat, which the start's attach marks Opp; and a self-dies trigger
// with its own target (Keiga, Vengeant Vampire) still has a creature on the
// opposing side for it to take besides the lands and the removal spell.
func TestStartAttachedBearerSeat(t *testing.T) {
	reg, b := testBuilder(t)
	res, err := Generate(reg, b, "Dead Man's Chest", "f0/t0", Options{Prefer: "start"})
	if err != nil || res.Start == nil {
		t.Fatalf("Dead Man's Chest: %+v %v", res, err)
	}
	f := res.Start
	if len(f.Setup.P0.Attach) != 1 || !f.Setup.P0.Attach[0].Opp {
		t.Fatalf("p0 attach %+v, want the Aura attached to an opposing bearer", f.Setup.P0.Attach)
	}
	attached(t, f, "Dead Man's Chest")
	bearer := f.Setup.P0.Attach[0].To
	if slices.Contains(f.Setup.P0.Battlefield, bearer) {
		t.Errorf("p0 battlefield %v holds the bearer %q of an opponent-only Enchant", f.Setup.P0.Battlefield, bearer)
	}
	if !slices.Equal(f.Focus.Targets, []string{bearer}) {
		t.Errorf("focus targets %v, want the bearer %q", f.Focus.Targets, bearer)
	}
	for _, card := range []string{"Keiga, the Tide Star", "Vengeant Vampire"} {
		res, err := Generate(reg, b, card, "f0/t0", Options{Prefer: "start"})
		if err != nil || res.Start == nil {
			t.Fatalf("%s: %+v %v", card, res, err)
		}
		creature := false
		for _, n := range res.Start.Setup.P1.Battlefield {
			if c, ok := reg.Lookup(n); ok && len(c.Faces) > 0 && c.Faces[0].IsCreature() {
				creature = true
			}
		}
		if !creature {
			t.Errorf("%s: p1 battlefield %v holds no creature for the trigger's target", card, res.Start.Setup.P1.Battlefield)
		}
	}
}

// TestStartSelfDiesKeepsGraveTarget: a dies trigger whose effect returns or
// exiles a card from the graveyard still needs its own target placed. The
// focus play (Murder) names no graveyard target, so an override that read only
// the focus card dropped Myr Retriever's artifact and the other self-dies
// triggers' targets; placeGraveTargets now places both the key's and the
// focus play's.
func TestStartSelfDiesKeepsGraveTarget(t *testing.T) {
	reg, b := testBuilder(t)
	for _, card := range []string{"Myr Retriever", "Vesperlark", "Junk Diver", "Workshop Assistant"} {
		t.Run(card, func(t *testing.T) {
			res, err := Generate(reg, b, card, "f0/t0", Options{Prefer: "start"})
			if err != nil {
				t.Fatal(err)
			}
			if res.Path != PathStart || res.Start == nil {
				t.Fatalf("path %q reason %q, want a start file", res.Path, res.Reason)
			}
			f := res.Start
			if f.Focus == nil || f.Focus.Card != "Murder" || !slices.Equal(f.Focus.Targets, []string{card}) {
				t.Fatalf("focus %+v, want Murder targeting %s", f.Focus, card)
			}
			for _, n := range f.Setup.P0.Graveyard {
				c, ok := reg.Lookup(n)
				if ok && len(c.Faces) > 0 && !c.Faces[0].IsBasic() {
					return
				}
			}
			t.Errorf("p0 graveyard %v holds only basics, want the trigger's nonbasic target", f.Setup.P0.Graveyard)
		})
	}
}

// TestStartSpellCastTriggers: a cast trigger is a carrier spell the filter

// takes, cast by the seat ValidActivatingPlayer names: Reach Through Mists
// ({U} instant, draw a card) for a noncreature spell, on the opponent's turn
// for "whenever you cast a spell during an opponent's turn" (Brineborn
// Cutthroat); "your second spell each turn" is two casts of Memnite (focus
// times 2).
func TestStartSpellCastTriggers(t *testing.T) {
	checkStartRows(t, []startRow{
		{card: "Brineborn Cutthroat", key: "f0/t0", active: 1, maxTurns: 1, p0Hand: []string{"Reach Through Mists"},
			p0BF: []string{"Brineborn Cutthroat", "Island"}, focusCard: "Reach Through Mists", focusKind: "cast"},
	})
	reg, b := testBuilder(t)
	res, err := Generate(reg, b, "Cori-Steel Cutter", "f0/t0", Options{Prefer: "start"})
	if err != nil || res.Start == nil {
		t.Fatalf("Cori-Steel Cutter: %+v %v", res, err)
	}
	f := res.Start
	if !slices.Equal(f.Setup.P0.Hand, rep("Memnite", 2)) || f.Focus.Card != "Memnite" || f.Focus.Times != 2 || f.Setup.P0.Battlefield[0] != "Cori-Steel Cutter" {
		t.Errorf("Cori-Steel Cutter: hand %v battlefield %v focus %+v, want two Memnite cast twice", f.Setup.P0.Hand, f.Setup.P0.Battlefield, f.Focus)
	}
	res, err = Generate(reg, b, "Case of the Ransacked Lab", "f0/t1", Options{Prefer: "start"})
	if err != nil || res.Start == nil || !slices.Equal(res.Start.Setup.P0.Hand, []string{"Reach Through Mists"}) {
		t.Errorf("Case of the Ransacked Lab (Instant,Sorcery): %+v %v", res.Start, err)
	}
}

// TestStartspellcasttriggers: the SpellCast:filter refusal class. A cast
// trigger with no ValidCard and TargetsValid$ Card.Self (heroic) is the card
// on the battlefield and a carrier spell that targets it, cast by its
// controller; a filter property the resolver knows (Card.Historic) is a
// carrier the property takes. These cards refused "no-generator:trigger:
// SpellCast:filter" at current develop.
func TestStartspellcasttriggers(t *testing.T) {
	checkStartRows(t, []startRow{
		// heroic: the card on the battlefield, Ranger's Guile cast at it
		{card: "Akroan Crusader", key: "f0/t0", maxTurns: 1, p0Hand: []string{"Ranger's Guile"},
			p0BF: []string{"Akroan Crusader", "Forest"}, focusCard: "Ranger's Guile", focusKind: "cast"},
		{card: "Akroan Line Breaker", key: "f0/t0", maxTurns: 1, p0Hand: []string{"Ranger's Guile"},
			p0BF: []string{"Akroan Line Breaker", "Forest"}, focusCard: "Ranger's Guile", focusKind: "cast"},
		{card: "Akroan Skyguard", key: "f0/t0", maxTurns: 1, p0Hand: []string{"Ranger's Guile"},
			p0BF: []string{"Akroan Skyguard", "Forest"}, focusCard: "Ranger's Guile", focusKind: "cast"},
		{card: "Anax and Cymede", key: "f0/t0", maxTurns: 1, p0Hand: []string{"Ranger's Guile"},
			p0BF: []string{"Anax and Cymede", "Forest"}, focusCard: "Ranger's Guile", focusKind: "cast"},
		{card: "Artisan of Forms", key: "f0/t0", maxTurns: 1, p0Hand: []string{"Ranger's Guile"},
			p0BF: []string{"Artisan of Forms", "Forest"}, p1BF: []string{"Phyrexian Walker"},
			focusCard: "Ranger's Guile", focusKind: "cast"},
		// heroic plus IsSingleTarget: Ranger's Guile names one target
		{card: "Zada, Hedron Grinder", key: "f0/t0", maxTurns: 1, p0Hand: []string{"Ranger's Guile"},
			p0BF: []string{"Zada, Hedron Grinder", "Forest"}, focusCard: "Ranger's Guile", focusKind: "cast"},
		// an Aura filter: Rancor is the Aura that targets the card
		{card: "Druid of Horns", key: "f0/t0", maxTurns: 1, p0Hand: []string{"Rancor", "Web"},
			p0BF: []string{"Druid of Horns", "Forest", "Forest", "Wall of Stone"}, focusCard: "Rancor", focusKind: "cast"},
		// a filter property the resolver knows: Memnite is historic
		{card: "Artificer's Assistant", key: "f0/t0", maxTurns: 1, p0Hand: []string{"Memnite"},
			p0BF: []string{"Artificer's Assistant"}, focusCard: "Memnite", focusKind: "cast"},
		// no ValidCard at all (Extort's "whenever you cast a spell"): any
		// carrier is a spell, so the first one casts
		{card: "Basilica Guards", key: "f0/t0", maxTurns: 1, p0Hand: []string{"Memnite"},
			p0BF: []string{"Basilica Guards"}, focusCard: "Memnite", focusKind: "cast"},
		// a second cast gate: the bare carrier does not meet it, so the
		// start refuses rather than writing an event the trigger rejects.
		// ValidCard$ Card.Party: the pin does not evaluate Party (fails closed), so no start can fire it
		{card: "Angel of Unity", key: "f0/t1", reason: "no-generator:trigger:SpellCast:filter"},
		{card: "Riku of Many Paths", key: "f0/t0", reason: "no-generator:trigger:SpellCast:filter"},         // ValidSA$ Spell.Modal
		{card: "Voracious Bibliophile", key: "f0/t0", reason: "no-generator:trigger:SpellCast:filter"},      // ValidSA$ Spell.numTargets GE1
		{card: "Verazol, the Split Current", key: "f0/t0", reason: "no-generator:trigger:SpellCast:filter"}, // ValidSA$ Spell.Kicked
		{card: "Virtual Assistant", key: "f0/t0", reason: "no-generator:trigger:SpellCast:filter"},          // ValidSA$ Spell.Teamwork
		{card: "Dragonlord Kolaghan", key: "f0/t0", reason: "no-generator:trigger:SpellCast:filter"},        // ValidSAonCard$
		{card: "Erayo, Soratami Ascendant", key: "f0/t0", reason: "no-generator:trigger:SpellCast:filter"},  // CheckSVar/SVarCompare
		{card: "Ivy, Gleeful Spellthief", key: "f0/t0", reason: "no-generator:trigger:SpellCast:filter"},    // IsSingleTarget + non-Self TargetsValid
		{card: "Ambitious Augmenter", key: "f0/t1", reason: "no-generator:trigger:SpellCast:filter"},        // Increment$
	})
	// A non-Self TargetsValid ("a spell that targets a creature you
	// control") and a self-target shape the heroic arm cannot meet
	// (Beamsplitter Mage's CanTargetOtherCondition) are the other lane's
	// start-not-exercised class, not this ticket's. The heroic arm must
	// leave those keys alone: they generated a start at develop and still
	// must, rather than becoming a new :filter refusal.
	reg, b := testBuilder(t)
	for _, card := range []string{"Feather, the Redeemed", "Leyline of Resonance", "Orvar, the All-Form", "Beamsplitter Mage"} {
		res, err := Generate(reg, b, card, "f0/t0", Options{Prefer: "start"})
		if err != nil {
			t.Errorf("%s: %v", card, err)
			continue
		}
		if res.Path != PathStart || res.Start == nil {
			t.Errorf("%s: path %q reason %q, want the pre-existing start", card, res.Path, res.Reason)
		}
	}
}

// TestBareCastGateWhitelist: the no-filter carrier is safe only when the
// trigger asks nothing more of the cast. A param outside the accounted set
// -- a known gate or a future one -- refuses, and the self-target arm keeps
// the one target shape it meets.
func TestBareCastGateWhitelist(t *testing.T) {
	trig := func(params map[string]string) *cards.Trigger {
		return &cards.Trigger{Mode: "SpellCast", Params: params}
	}
	base := map[string]string{"Mode": "SpellCast", "TriggerZones": "Battlefield",
		"Execute": "X", "TriggerDescription": "x"}
	if !bareCast(trig(base), false) {
		t.Error("a bare SpellCast trigger was refused")
	}
	for _, tc := range []struct {
		name   string
		params map[string]string
		self   bool
	}{
		{"ValidSA", map[string]string{"Mode": "SpellCast", "ValidSA": "Spell.Modal"}, false},
		{"Increment", map[string]string{"Mode": "SpellCast", "Increment": "True"}, false},
		{"CheckSVar", map[string]string{"Mode": "SpellCast", "CheckSVar": "NumCast", "SVarCompare": "EQ4"}, false},
		{"unknown gate", map[string]string{"Mode": "SpellCast", "FutureGate": "True"}, false},
		{"non-Self TargetsValid", map[string]string{"Mode": "SpellCast", "TargetsValid": "Creature.YouCtrl"}, false},
		{"self-target in the no-filter arm", map[string]string{"Mode": "SpellCast", "TargetsValid": "Card.Self"}, false},
		{"IsSingleTarget in the no-filter arm", map[string]string{"Mode": "SpellCast", "IsSingleTarget": "True"}, false},
		{"CanTargetOtherCondition", map[string]string{"Mode": "SpellCast", "TargetsValid": "Card.Self", "CanTargetOtherCondition": "Creature.YouCtrl"}, true},
	} {
		if bareCast(trig(tc.params), tc.self) {
			t.Errorf("%s accepted, want refused", tc.name)
		}
	}
	self := map[string]string{"Mode": "SpellCast", "TargetsValid": "Card.Self", "Execute": "X"}
	if !bareCast(trig(self), true) {
		t.Error("a heroic self-target trigger was refused")
	}
	if bareCast(trig(self), false) {
		t.Error("a self-target trigger was accepted by the no-filter arm")
	}
	single := map[string]string{"Mode": "SpellCast", "TargetsValid": "Card.Self",
		"IsSingleTarget": "True", "ValidCard": "Instant,Sorcery", "Execute": "X"}
	if !bareCast(trig(single), true) {
		t.Error("a heroic single-target trigger was refused")
	}
	if bareCast(trig(single), false) {
		t.Error("a single-target trigger was accepted by the no-filter arm")
	}
}

// TestFilterProps: the named-property table parseFilter reads, including its
// "non" form, and the conservative default for a property it does not know.
func TestFilterProps(t *testing.T) {
	reg, _ := testBuilder(t)
	face := func(name string) *cards.Face {
		t.Helper()
		c, ok := reg.Lookup(name)
		if !ok || len(c.Faces) == 0 {
			t.Fatalf("%s is not in the corpus", name)
		}
		return c.Faces[0]
	}
	takes := func(filter, name string) bool {
		t.Helper()
		alts := parseFilter(filter)
		if len(alts) != 1 {
			t.Fatalf("%s: %d alternatives, want 1", filter, len(alts))
		}
		return alts[0].test(face(name))
	}
	for _, tc := range []struct {
		filter, card string
		want         bool
	}{
		{"Card.Historic", "Memnite", true},              // artifact
		{"Card.Historic", "Anax and Cymede", true},      // legendary
		{"Card.Historic", "Reach Through Mists", false}, // neither
		{"Card.MultiColor", "Anax and Cymede", true},
		{"Card.MultiColor", "Memnite", false},
		{"Card.Outlaw", "Ragavan, Nimble Pilferer", true}, // Pirate (CR 700.12)
		{"Card.Outlaw", "Anax and Cymede", false},
		{"Card.nonOutlaw", "Anax and Cymede", true},
		{"Card.nonOutlaw", "Ragavan, Nimble Pilferer", false},
		{"Card.nonColorless", "Anax and Cymede", true},
		{"Card.nonColorless", "Memnite", false},
		{"Card.Colorless", "Memnite", true},
		{"Card.Colorless", "Anax and Cymede", false},
	} {
		if got := takes(tc.filter, tc.card); got != tc.want {
			t.Errorf("%s on %s = %v, want %v", tc.filter, tc.card, got, tc.want)
		}
	}
	// The pin fails an unrecognised predicate closed, so a start built on one
	// could never fire: each stays an unknown alternative (a refusal) until the
	// pin learns it. This is the known-unsupported set at the current pin; a
	// bump that learns one fails here and the entry is deleted.
	unsupported := []string{"Party", "nonParty", "Multicolor", "nonMulticolor", "nonMultiColor", "nonHistoric"}
	for p := range filterProps {
		for _, q := range []string{p, "non" + p} {
			if supported, listed := pinMatches(q), slices.Contains(unsupported, q); supported == listed {
				t.Errorf("Card.%s: pin evaluates it = %v, but known-unsupported = %v", q, supported, listed)
			}
		}
	}
	for _, q := range unsupported {
		if alts := parseFilter("Card." + q); len(alts) != 1 || alts[0].known {
			t.Errorf("Card.%s: %+v, want one unknown alternative", q, alts)
		}
	}
	if alts := parseFilter("Card.!wasCastFromYourHand"); len(alts) != 1 || alts[0].known {
		t.Errorf("Card.!wasCastFromYourHand: %+v, want one unknown alternative", alts)
	}
}

// TestStartmisctriggers: the event modes (eventStart, eventFixtures): the
// start causes the event the trigger reads with one fixture play on the seat
// that makes it. A discard is Coercion cast at the seat whose hand is exactly
// the card the filter takes (Abyssal Nocturnus: the opponent's Swamp,
// Aclazotz: a land; Ajani's Last Stand: the card itself, discarded by the
// opponent's Coercion, with the Plains its IsPresent reads); Coercion's only
// target is an opponent, so the bot has no other target to choose; a draw is
// Reach Through Mists cast as often as the trigger's Number says (Zimone and
// Dina's second card) by the seat the filter names (Sheoldred: the opponent);
// a sacrifice is a carrier sacrificing itself; a tap is the card's own {T}
// ability or its attack (Zhur-Taa Druid taps itself for mana, Zar Ojanen
// attacks); a crime is Coercion cast at the opponent (Vadmir); an
// untap is the card placed tapped through the other seat's turn 1 to its own
// untap step; a target is the opponent's Murder, Disenchant (an enchantment)
// or the card's controller's trick. A turned-face-up trigger is refused by
// name: a face-down permanent cannot be placed and turning it up is a second
// play. A trigger whose condition the start does not model is refused by name.
func TestStartmisctriggers(t *testing.T) {
	checkStartRows(t, []startRow{
		{card: "Abyssal Nocturnus", key: "f0/t0", maxTurns: 1, p0Hand: []string{"Coercion"},
			p0BF: cat([]string{"Abyssal Nocturnus"}, rep("Swamp", 3)), p1Hand: rep("Swamp", 1),
			focusCard: "Coercion", focusKind: "cast"},
		{card: "Ajani's Last Stand", key: "f0/t1", active: 1, maxTurns: 1, p0Hand: []string{"Ajani's Last Stand"},
			p0BF: rep("Plains", 2), p1Hand: []string{"Coercion"}, p1BF: rep("Swamp", 3),
			focusSeat: 1, focusCard: "Coercion", focusKind: "cast"},
		{card: "Zimone and Dina", key: "f0/t0", maxTurns: 1, p0Hand: rep("Reach Through Mists", 2),
			p0BF: cat([]string{"Zimone and Dina"}, rep("Island", 2)), focusCard: "Reach Through Mists", focusKind: "cast", focusTime: 2},
		{card: "Sheoldred, the Apocalypse", key: "f0/t1", active: 1, maxTurns: 1, p0BF: []string{"Sheoldred, the Apocalypse"},
			p1Hand: []string{"Reach Through Mists"}, p1BF: []string{"Island"}, focusSeat: 1, focusCard: "Reach Through Mists", focusKind: "cast"},
		{card: "Zodiark, Umbral God", key: "f0/t1", maxTurns: 1, p0BF: []string{"Zodiark, Umbral God", "Kami of False Hope"},
			focusCard: "Kami of False Hope", focusKind: "ability"},
		{card: "Zar Ojanen, Scion of Efrava", key: "f0/t0", maxTurns: 1, p0BF: []string{"Zar Ojanen, Scion of Efrava"},
			focusCard: "Zar Ojanen, Scion of Efrava", focusKind: "attack"},
		{card: "Zhur-Taa Druid", key: "f0/t0", maxTurns: 1, p0BF: []string{"Zhur-Taa Druid"},
			focusCard: "Zhur-Taa Druid", focusKind: "ability"},
		{card: "Vadmir, New Blood", key: "f0/t0", maxTurns: 1, p0Hand: []string{"Coercion"},
			p0BF:      cat([]string{"Vadmir, New Blood"}, rep("Swamp", 3)),
			focusCard: "Coercion", focusKind: "cast"},
		{card: "Wild Defiance", key: "f0/t0", active: 1, maxTurns: 1, p0BF: []string{"Wild Defiance", "Memnite"},
			p1Hand: []string{"Murder"}, p1BF: rep("Swamp", 3), focusSeat: 1, focusCard: "Murder", focusKind: "cast"},
		{card: "Treacherous Blessing", key: "f0/t2", active: 1, maxTurns: 1, p0BF: []string{"Treacherous Blessing"},
			p1Hand: []string{"Disenchant"}, p1BF: rep("Plains", 2), focusSeat: 1, focusCard: "Disenchant", focusKind: "cast"},
		{card: "Acid-Spewer Dragon", key: "f0/t0", reason: "no-generator:trigger:TurnFaceUp:face-down"},
		{card: "Ainok Survivalist", key: "f0/t0", reason: "no-generator:trigger:TurnFaceUp:face-down"},
		{card: "Xyris, the Writhing Storm", key: "f0/t0", reason: "no-generator:trigger:Drawn:condition"},
	})
	reg, b := testBuilder(t)
	// the discard and crime fixtures take no target but an opponent: the bot
	// has no other target to choose, so the focus play is the forced line
	for _, name := range []string{eventFixtures.Discard, eventFixtures.Crime} {
		c, ok := reg.Lookup(name)
		if !ok || len(c.Faces[0].Abilities) == 0 || c.Faces[0].Abilities[0].Params["ValidTgts"] != "Opponent" {
			t.Errorf("fixture %s: want a spell whose one target is an opponent", name)
		}
	}
	// an untap: the card placed tapped, the game run to its own untap step
	// (the other seat's turn 1, then turn 2), with no focus play
	res, err := Generate(reg, b, "Aerie Worshippers", "f0/t0", Options{Prefer: "start"})
	if err != nil || res.Start == nil {
		t.Fatalf("Aerie Worshippers: %+v %v", res, err)
	}
	f := res.Start
	if !slices.Equal(f.Setup.P0.Battlefield, []string{"Aerie Worshippers"}) || !slices.Equal(f.Setup.P0.Tapped, []string{"Aerie Worshippers"}) ||
		f.Active != 1 || f.MaxTurns != 2 || f.Focus != nil {
		t.Errorf("Aerie Worshippers: battlefield %v tapped %v active %d max_turns %d focus %+v, want it placed tapped, seat 1 active, 2 turns, no focus",
			f.Setup.P0.Battlefield, f.Setup.P0.Tapped, f.Active, f.MaxTurns, f.Focus)
	}
}

// TestStartCosts: an activated ability's non-mana cost is met by start
// state (planCost), never by a play: Memnite to sacrifice for "another
// creature" (Agency Coroner), a basic placed tapped for a land (Ripchain
// Razorkin, so it adds no mana), a basic in hand to discard (Possessed Goat),
// the card in hand for "discard this card" (Bedhead Beastie) and in the
// graveyard for "exile this card from your graveyard" (Faerie Dreamthief), a
// vanilla creature with power 3 or more to crew 3 (Apocalypse Runner), and
// the loyalty a planeswalker's ultimate takes beyond its printed loyalty
// (Liliana, Dreadhorde General: 9 of 6). A token cost (Baylen taps tokens)
// stays refused.
func TestStartCosts(t *testing.T) {
	checkStartRows(t, []startRow{
		{card: "Agency Coroner", key: "f0/a0", maxTurns: 1, p0BF: cat([]string{"Agency Coroner"}, rep("Swamp", 3), []string{"Memnite"}),
			focusCard: "Agency Coroner", focusKind: "ability"},
		{card: "Possessed Goat", key: "f0/a0", maxTurns: 1, p0Hand: []string{"Plains"}, p0BF: cat([]string{"Possessed Goat"}, rep("Plains", 3)),
			focusCard: "Possessed Goat", focusKind: "ability"},
		{card: "Bedhead Beastie", key: "f0/a0", maxTurns: 1, p0Hand: []string{"Bedhead Beastie"}, p0BF: rep("Mountain", 3),
			focusCard: "Bedhead Beastie", focusKind: "ability"},
		{card: "Baylen, the Haymaker", key: "f0/a0", reason: "steps-needed:cost:tapXType"},
	})
	reg, b := testBuilder(t)
	gen := func(card, key string) *StartFile {
		t.Helper()
		res, err := Generate(reg, b, card, key, Options{Prefer: "start"})
		if err != nil || res.Start == nil {
			t.Fatalf("%s %s: %+v %v", card, key, res, err)
		}
		return res.Start
	}
	if f := gen("Ripchain Razorkin", "f0/a0"); !slices.Equal(f.Setup.P0.Battlefield, cat([]string{"Ripchain Razorkin"}, rep("Mountain", 4))) ||
		!slices.Equal(f.Setup.P0.Tapped, []string{"Mountain"}) {
		t.Errorf("Ripchain Razorkin: battlefield %v tapped %v, want three Mountains for {2}{R} and one tapped to sacrifice",
			f.Setup.P0.Battlefield, f.Setup.P0.Tapped)
	}
	if f := gen("Faerie Dreamthief", "f0/a0"); len(f.Setup.P0.Graveyard) == 0 || f.Setup.P0.Graveyard[0] != "Faerie Dreamthief" ||
		slices.Contains(f.Setup.P0.Battlefield, "Faerie Dreamthief") {
		t.Errorf("Faerie Dreamthief: graveyard %v battlefield %v, want the card in the graveyard", f.Setup.P0.Graveyard, f.Setup.P0.Battlefield)
	}
	f := gen("Apocalypse Runner", "f0/a1")
	if len(f.Setup.P0.Battlefield) != 2 || f.Setup.P0.Battlefield[0] != "Apocalypse Runner" {
		t.Fatalf("Apocalypse Runner: battlefield %v, want the Vehicle and one crew", f.Setup.P0.Battlefield)
	}
	crew, _ := reg.Lookup(f.Setup.P0.Battlefield[1])
	if cf := crew.Faces[0]; !cf.IsCreature() || cf.Power() < 3 || len(cf.Abilities)+len(cf.Triggers) > 0 {
		t.Errorf("Apocalypse Runner crew %s: want a vanilla creature with power 3 or more", cf.Name)
	}
	f = gen("Liliana, Dreadhorde General", "f0/a2")
	if want := []StartCounter{{Card: "Liliana, Dreadhorde General", Kind: "LOYALTY", N: 3}}; !reflect.DeepEqual(f.Setup.P0.Counters, want) {
		t.Errorf("Liliana: counters %+v, want %+v", f.Setup.P0.Counters, want)
	}
}

// TestStartCombat: an attack trigger is the card (or Memnite, for a
// noncreature card such as a Vehicle) attacking, on the battlefield since
// the turn began; a combat-damage start drops the opp-blockers need, so the
// attack connects.
func TestStartCombat(t *testing.T) {
	checkStartRows(t, []startRow{
		{card: "Dragon Mage", key: "f0/t0", maxTurns: 1, p0BF: []string{"Dragon Mage"}, focusCard: "Dragon Mage", focusKind: "attack"},
		// the opp-blockers need places a wall: "whenever you attack" fires blocked or not
		{card: "Boosted Sloop", key: "f0/t0", maxTurns: 1, p0BF: []string{"Boosted Sloop", "Memnite"}, p1BF: []string{"Shield Sphere"},
			focusCard: "Memnite", focusKind: "attack"},
	})
}

// TestStartBecomesTarget: ward is the opponent casting Murder at the card on
// its turn, valiant the card's controller casting Ranger's Guile (target
// creature you control, so an opposing creature a need placed is never the
// target).
func TestStartBecomesTarget(t *testing.T) {
	checkStartRows(t, []startRow{
		{card: "Heartfire Hero", key: "f0/t0", maxTurns: 1, p0Hand: []string{"Ranger's Guile"}, p0BF: []string{"Heartfire Hero", "Forest"},
			focusCard: "Ranger's Guile", focusKind: "cast"},
		{card: "Armored Armadillo", key: "f0/t0", active: 1, maxTurns: 1, p0BF: []string{"Armored Armadillo"},
			p1Hand: []string{"Murder"}, p1BF: rep("Swamp", 3), focusSeat: 1, focusCard: "Murder", focusKind: "cast"},
	})
}

// TestStartLifeGained: "whenever you gain life" is Chaplain's Blessing ({W}
// sorcery, gain 5 life) cast, and the life:gain need's enabler (Zuran Orb)
// is dropped: the carrier is the lifegain.
func TestStartLifeGained(t *testing.T) {
	checkStartRows(t, []startRow{
		{card: "Drogskol Reaver", key: "f0/t0", maxTurns: 1, p0Hand: []string{"Chaplain's Blessing"}, p0BF: []string{"Drogskol Reaver", "Plains"},
			focusCard: "Chaplain's Blessing", focusKind: "cast"},
	})
}

// TestStartCombatStaticRefusals: a combat or static key a start cannot set up
// is refused by a reason narrower than "no-generator:static"/"combat".
func TestStartCombatStaticRefusals(t *testing.T) {
	reg, b := testBuilder(t)
	for _, r := range [][3]string{
		{"Karn, Argent Defender", "static#0.0", "no-generator:static:DisableTriggers"},
		{"Academy Wall", "combat#0.attack", "no-generator:combat:defender"},
		{"Aether Syphon", "static#0.0", "no-generator:static:unobserved"}, // a granted trigger is not seen
		{"Armory Mice", "static#0.0", "no-generator:static:conditional"},  // a this-turn count
		{"Aettir and Priwen", "static#0.0", "no-generator:static:computed"},
		// the attack/block is not offered, or the effect is not seen
		{"Magus of the Moat", "combat#0.attack", "no-generator:combat:restricted"},
		{"Frenetic Raptor", "combat#0.block", "no-generator:combat:restricted"},
		{"Blazing Archon", "combat#0.block", "no-generator:combat:restricted"},
	} {
		res, err := Generate(reg, b, r[0], r[1], Options{Prefer: "start"})
		if err != nil {
			t.Fatal(err)
		}
		if res.Path != PathRefused || res.Reason != r[2] {
			t.Errorf("%s %s: path %q reason %q, want refused %q", r[0], r[1], res.Path, res.Reason, r[2])
		}
	}
}

// permanents is the faces of the card names a seat lists on the battlefield.
func permanents(t *testing.T, reg *cards.Registry, names []string) []*cards.Face {
	t.Helper()
	var out []*cards.Face
	for _, n := range names {
		c, ok := reg.Lookup(n)
		if !ok {
			t.Fatalf("%q is not in the corpus", n)
		}
		out = append(out, c.Faces[0])
	}
	return out
}

func TestStartDeadOnArrival(t *testing.T) {
	reg, _ := testBuilder(t)
	for name, want := range map[string]bool{
		"Endless One": true, "Walking Ballista": true, "Ugin's Conjurant": true, "Phyrexian Marauder": true,
		"Grizzly Bears": false, "Wall of Stone": false, "Shield Sphere": false, "Tarmogoyf": false, "Forest": false,
	} {
		c, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("%q is not in the corpus", name)
		}
		if got := deadOnArrival(c.Faces[0]); got != want {
			t.Errorf("deadOnArrival(%s) = %v, want %v", name, got, want)
		}
	}
}

// TestStartCombatStaticIgnoreOtherNeeds: a static or combat key places its own
// objects only. An unrelated need of the card (a token trigger: certdeck's
// token need) neither refuses the key nor adds state.
func TestStartCombatStaticIgnoreOtherNeeds(t *testing.T) {
	reg, b := testBuilder(t)
	for _, r := range []struct {
		card, key string
		p0, p1    []string
	}{
		{"Arahbo, the First Fang", "static#0.0", []string{"Arahbo, the First Fang", "Sanctuary Cat"}, nil},
		{"Gadrak, the Crown-Scourge", "combat#0.block", []string{"Gadrak, the Crown-Scourge"}, []string{"Grizzly Bears"}},
	} {
		res, err := Generate(reg, b, r.card, r.key, Options{Prefer: "start"})
		if err != nil || res.Path != PathStart {
			t.Fatalf("%s %s: %v path %q reason %q, want a start", r.card, r.key, err, res.Path, res.Reason)
		}
		f := res.Start
		listIs(t, r.card+" p0 battlefield", f.Setup.P0.Battlefield, r.p0)
		listIs(t, r.card+" p1 battlefield", f.Setup.P1.Battlefield, r.p1)
		if len(f.Setup.P0.Graveyard)+len(f.Setup.P0.Tapped) != 0 {
			t.Errorf("%s: unrelated state placed: graveyard %v tapped %v", r.card, f.Setup.P0.Graveyard, f.Setup.P0.Tapped)
		}
	}
}

// TestStartStaticObserved: a static whose effect the comparison cannot see is
// refused; a keyword grant carries the keyword opt-in; a land the filter reads
// is a placed basic.
func TestStartStaticObserved(t *testing.T) {
	reg, b := testBuilder(t)
	gen := func(card, key string) Result {
		res, err := Generate(reg, b, card, key, Options{Prefer: "start"})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	if res := gen("Primal Rage", "static#0.0"); res.Path != PathStart || !reflect.DeepEqual(res.Start.Compare, []string{"keywords"}) {
		t.Errorf("Primal Rage: path %q, want a start comparing keywords", res.Path)
	}
	if res := gen("Aggressive Mammoth", "static#0.0"); res.Path != PathStart || !reflect.DeepEqual(res.Start.Compare, []string{"keywords"}) {
		t.Errorf("Aggressive Mammoth: path %q, want a start comparing keywords", res.Path)
	}
	// a pump alone compares nothing extra
	if res := gen("Glorious Anthem", "static#0.0"); res.Path != PathStart || len(res.Start.Compare) != 0 {
		t.Errorf("Glorious Anthem: path %q start %+v, want a start with no extra compare", res.Path, res.Start)
	}
}

// TestStartFocusIsOffered: a start's focus play is legal at the first focus-seat
// priority (the focus seat takes it at turn 1's first main phase), or the key is
// refused by name. A 0/0 creature with an enters-with-counters replacement (Endless
// One, Ugin's Conjurant, Phyrexian Marauder) dies to SBA when a start places it, so
// it is never an enabler or a bearer: Branching Bolt and Cloak of Confusion get a
// creature that lives. Bone to Ash targets a creature spell, so it gets a scripted
// bait cast (StartFocus.Bait, TestStartstacktarget); Burning Oil's attacking or
// blocking creature is a scripted attack (StartFocus.Attack), and Maze of Shadows'
// attacking creature with shadow one no carrier meets.
func TestStartFocusIsOffered(t *testing.T) {
	reg, b := testBuilder(t)
	creature := func(names []string) bool {
		for _, f := range permanents(t, reg, names) {
			if f.IsCreature() && !deadOnArrival(f) {
				return true
			}
		}
		return false
	}
	rows := []struct {
		card, key, reason string
		check             func(t *testing.T, f *StartFile)
	}{
		{card: "Aether Tradewinds", key: "cast", check: func(t *testing.T, f *StartFile) {
			// "target permanent you don't control" needs a permanent on p1
			if len(f.Setup.P1.Battlefield) == 0 {
				t.Error("p1 has no permanent for the you-don't-control target")
			}
		}},
		{card: "Bone to Ash", key: "cast", check: func(t *testing.T, f *StartFile) {
			if f.Focus.Bait == "" || !slices.Contains(f.Setup.P1.Hand, f.Focus.Bait) {
				t.Errorf("focus %+v p1 hand %v, want a bait spell the opponent casts", f.Focus, f.Setup.P1.Hand)
			}
		}},
		{card: "Burning Oil", key: "cast", check: func(t *testing.T, f *StartFile) {
			// the attacking target is a carrier of p0's that attacks first
			if f.Focus.Attack == "" || f.Focus.Block != "" || f.Active != 0 || !slices.Contains(f.Setup.P0.Battlefield, f.Focus.Attack) {
				t.Errorf("focus %+v active %d p0 battlefield %v, want p0 attacking with a creature of its own", *f.Focus, f.Active, f.Setup.P0.Battlefield)
			}
		}},
		{card: "Maze of Shadows", key: "f0/a1", reason: "start:needs-combat"},
		{card: "Branching Bolt", key: "cast", check: func(t *testing.T, f *StartFile) {
			if !creature(f.Setup.P1.Battlefield) {
				t.Errorf("p1 battlefield %v has no creature that survives", f.Setup.P1.Battlefield)
			}
		}},
		{card: "Cloak of Confusion", key: "cast", check: func(t *testing.T, f *StartFile) {
			if !creature(f.Setup.P0.Battlefield) {
				t.Errorf("p0 battlefield %v has no creature that survives, for the Aura to enchant", f.Setup.P0.Battlefield)
			}
		}},
		{card: "Asceticism", key: "f0/a0", check: func(t *testing.T, f *StartFile) {
			if !creature(f.Setup.P1.Battlefield) {
				t.Errorf("p1 battlefield %v has no creature that survives", f.Setup.P1.Battlefield)
			}
		}},
		{card: "Copper Carapace", key: "f0/a0", check: func(t *testing.T, f *StartFile) {
			if !creature(f.Setup.P0.Battlefield) {
				t.Errorf("p0 battlefield %v has no creature that survives, to equip", f.Setup.P0.Battlefield)
			}
		}},
		{card: "Anathemancer", key: "f0/a0", check: func(t *testing.T, f *StartFile) {
			// unearth is activated from the graveyard
			if !slices.Contains(f.Setup.P0.Graveyard, "Anathemancer") || slices.Contains(f.Setup.P0.Battlefield, "Anathemancer") {
				t.Errorf("p0 graveyard %v battlefield %v, want Anathemancer in the graveyard only", f.Setup.P0.Graveyard, f.Setup.P0.Battlefield)
			}
		}},
	}
	for _, r := range rows {
		t.Run(r.card, func(t *testing.T) {
			res, err := Generate(reg, b, r.card, r.key, Options{Prefer: "start"})
			if err != nil {
				t.Fatal(err)
			}
			if r.reason != "" {
				if res.Path != PathRefused || res.Reason != r.reason {
					t.Fatalf("path %q reason %q, want refused %q", res.Path, res.Reason, r.reason)
				}
				return
			}
			if res.Start == nil {
				t.Fatalf("path %q reason %q, want a start file", res.Path, res.Reason)
			}
			for _, bf := range [][]string{res.Start.Setup.P0.Battlefield, res.Start.Setup.P1.Battlefield} {
				for _, f := range permanents(t, reg, bf) {
					if deadOnArrival(f) {
						t.Errorf("%s is placed on the battlefield and dies to SBA", f.Name)
					}
				}
			}
			r.check(t, res.Start)
		})
	}
}

// TestStartstacktarget: a focus play that targets a plain spell needs a spell
// on the stack, and a start holds at main 1 with an empty one, so the 304
// corpus cast keys whose ability has an exact TargetType$ Spell were refused as
// start:needs-stack-target (Absorb, Bone to Ash, ...). The start now scripts
// the opponent's bait cast (Focus.Bait): the most inert spell of the kind the
// target filter takes, in the opponent's hand with its lands, so the focus play
// has a legal target (CR 115.1a). The residual classes have their own bait
// forms: an activated-ability target is met by the bait seat's activation of a
// placed permanent (Focus.BaitAbility, Bind), a Spell.singleTarget target by a
// targeted bait cast with its own placed target (Focus.BaitTarget, Divert), and
// a target only the focus seat could control by the focus seat's own held
// pre-cast (Focus.BaitSelf, Double Major).
func TestStartstacktarget(t *testing.T) {
	reg, b := testBuilder(t)
	for _, c := range []struct {
		card string
		// creature is whether the key's target must be a creature spell
		// (Bone to Ash's "Counter target creature spell").
		creature bool
		// baitForm is the bait class the card's stack target takes: "cast"
		// (the opponent's plain-spell cast), "ability", "abilitySelf",
		// "targeted" or "self".
		baitForm string
		// key is the cast key by default; an ability bait form may take the
		// key of the targeting ability instead
		key string
	}{
		{card: "Absorb"},                      // Counter target spell
		{card: "Admiral's Order"},             // Counter target spell
		{card: "Assert Authority"},            // Counter target spell, exile instead
		{card: "Condescend"},                  // Counter target spell unless its controller pays {X}
		{card: "Bone to Ash", creature: true}, // Counter target creature spell
		{card: "Bind", baitForm: "ability"},   // Counter target activated ability
		{card: "Squelch", baitForm: "ability"},
		{card: "Interdict", baitForm: "ability"}, // from an artifact, creature, enchantment, or land
		{card: "Divert", baitForm: "targeted"},   // target spell with a single target
		{card: "Deflection", baitForm: "targeted"},
		{card: "Misdirection", baitForm: "targeted"},
		{card: "Double Major", baitForm: "self", creature: true}, // target creature spell you control
		{card: "Increasing Vengeance", baitForm: "self"},         // target instant or sorcery spell you control
		// an ability target only the focus seat controls: the focus seat
		// itself activates the placed carrier (its copy-ability play targets
		// only an ability it controls, "Activated.YouCtrl")
		{card: "Lithoform Engine", baitForm: "abilitySelf", key: "f0/a0"},
	} {
		t.Run(c.card, func(t *testing.T) {
			key := c.key
			if key == "" {
				key = "cast"
			}
			res, err := Generate(reg, b, c.card, key, Options{Prefer: "start"})
			if err != nil {
				t.Fatal(err)
			}
			if res.Path != PathStart || res.Start == nil {
				t.Fatalf("path %q reason %q, want a start file", res.Path, res.Reason)
			}
			f := res.Start
			if f.Focus == nil {
				t.Fatalf("no focus, want a scripted bait")
			}
			switch c.baitForm {
			case "", "cast":
				if f.Focus.Bait == "" || f.Focus.BaitSelf != "" || f.Focus.BaitAbility != "" || f.Focus.BaitTarget != "" {
					t.Fatalf("focus %+v, want a scripted opponent bait cast", f.Focus)
				}
				if !slices.Contains(f.Setup.P1.Hand, f.Focus.Bait) {
					t.Errorf("p1 hand %v does not list the bait %q", f.Setup.P1.Hand, f.Focus.Bait)
				}
				bc, ok := reg.Lookup(f.Focus.Bait)
				if !ok || len(bc.Faces) == 0 {
					t.Fatalf("bait %q is not a card", f.Focus.Bait)
				}
				bait := bc.Faces[0]
				if bait.IsLand() {
					t.Errorf("bait %q is a land, which is not a spell", f.Focus.Bait)
				}
				if c.creature && !bait.IsCreature() {
					t.Errorf("bait %q is not a creature spell for a creature-spell target", f.Focus.Bait)
				}
				if c.creature && f.Active != 1 {
					// a creature spell can only be cast on its controller's main
					// phase, so the bait seat is the active one
					t.Errorf("active %d, want the bait seat 1", f.Active)
				}
				if col := firstColour(bait.ManaCost); col != "" && !slices.Contains(f.Setup.P1.Battlefield, basicOf[col]) {
					t.Errorf("p1 battlefield %v has no %s to pay the bait", f.Setup.P1.Battlefield, basicOf[col])
				}
			case "ability", "abilitySelf":
				if f.Focus.BaitAbility == "" || f.Focus.Bait != "" || f.Focus.BaitSelf != "" {
					t.Fatalf("focus %+v, want a scripted bait ability activation", f.Focus)
				}
				if (c.baitForm == "abilitySelf") != f.Focus.BaitAbilitySelf {
					t.Fatalf("focus %+v, want baitAbilitySelf %v", f.Focus, c.baitForm == "abilitySelf")
				}
				ac, ok := reg.Lookup(f.Focus.BaitAbility)
				if !ok || len(ac.Faces) == 0 {
					t.Fatalf("bait ability %q is not a card", f.Focus.BaitAbility)
				}
				act := ac.Faces[0]
				if !act.IsPermanent() || act.IsLand() {
					t.Errorf("bait ability %q is not a nonland permanent", f.Focus.BaitAbility)
				}
				sa := baitAbilityOf(act)
				if sa == nil {
					t.Fatalf("%q has no activated ability", f.Focus.BaitAbility)
				}
				mana, why := costTokens(sa.Params["Cost"])
				if why != "" {
					t.Fatalf("%q ability cost %q is not placeable: %s", f.Focus.BaitAbility, sa.Params["Cost"], why)
				}
				bf := f.Setup.P1.Battlefield
				if f.Focus.BaitAbilitySelf {
					bf = f.Setup.P0.Battlefield
				}
				if !slices.Contains(bf, f.Focus.BaitAbility) {
					t.Errorf("battlefield %v does not list the bait ability %q (self %v)", bf, f.Focus.BaitAbility, f.Focus.BaitAbilitySelf)
				}
				for _, l := range landsFor(mana, basicFor(sa.Params["Cost"])) {
					if !slices.Contains(bf, l) {
						t.Errorf("bait battlefield %v has no %s to pay the ability cost", bf, l)
					}
				}
			case "targeted":
				if f.Focus.Bait == "" || f.Focus.BaitTarget == "" || f.Focus.BaitSelf != "" || f.Focus.BaitAbility != "" {
					t.Fatalf("focus %+v, want a targeted bait cast with its own target", f.Focus)
				}
				bc, ok := reg.Lookup(f.Focus.Bait)
				if !ok || len(bc.Faces) == 0 {
					t.Fatalf("bait %q is not a card", f.Focus.Bait)
				}
				bait := bc.Faces[0]
				if !bait.IsInstant() {
					t.Errorf("bait %q is not an instant, which the bait seat could not cast at main 1", f.Focus.Bait)
				}
				if len(bait.Abilities) != 1 || bait.Abilities[0].Params["ValidTgts"] == "" {
					t.Fatalf("bait %q has no one targeted ability", f.Focus.Bait)
				}
				if !slices.Contains(f.Setup.P1.Hand, f.Focus.Bait) {
					t.Errorf("p1 hand %v does not list the bait %q", f.Setup.P1.Hand, f.Focus.Bait)
				}
				if !slices.Contains(f.Setup.P1.Battlefield, f.Focus.BaitTarget) &&
					!slices.Contains(f.Setup.P0.Battlefield, f.Focus.BaitTarget) {
					t.Errorf("battlefields %v / %v do not list the bait's target %q", f.Setup.P0.Battlefield, f.Setup.P1.Battlefield, f.Focus.BaitTarget)
				}
			case "self":
				if f.Focus.BaitSelf == "" || f.Focus.Bait != "" || f.Focus.BaitAbility != "" || f.Focus.BaitTarget != "" {
					t.Fatalf("focus %+v, want a focus-seat self bait cast", f.Focus)
				}
				if !slices.Contains(f.Setup.P0.Hand, f.Focus.BaitSelf) {
					t.Errorf("p0 hand %v does not list the self bait %q", f.Setup.P0.Hand, f.Focus.BaitSelf)
				}
				sc, ok := reg.Lookup(f.Focus.BaitSelf)
				if !ok || len(sc.Faces) == 0 {
					t.Fatalf("self bait %q is not a card", f.Focus.BaitSelf)
				}
				self := sc.Faces[0]
				if c.creature && !self.IsCreature() {
					t.Errorf("self bait %q is not a creature spell for a creature-spell-you-control target", f.Focus.BaitSelf)
				}
				if !c.creature && !self.IsInstant() && !self.IsSorcery() {
					t.Errorf("self bait %q is not an instant or sorcery spell", f.Focus.BaitSelf)
				}
				if col := firstColour(self.ManaCost); col != "" && !slices.Contains(f.Setup.P0.Battlefield, basicOf[col]) {
					t.Errorf("p0 battlefield %v has no %s to pay the self bait", f.Setup.P0.Battlefield, basicOf[col])
				}
			}
		})
	}
}

// TestStartstaticother: statics whose effect is observed through a cost
// reduction, a computed board count, an offered grant, or a zone other than
// the battlefield get a start that exercises them, instead of a
// no-generator refusal.
func TestStartstaticother(t *testing.T) {
	reg, b := testBuilder(t)
	gen := func(t *testing.T, card, key string) *StartFile {
		t.Helper()
		res, err := Generate(reg, b, card, key, Options{Prefer: "start"})
		if err != nil {
			t.Fatal(err)
		}
		if res.Path != PathStart || res.Start == nil {
			t.Fatalf("path %q reason %q, want a start file", res.Path, res.Reason)
		}
		return res.Start
	}
	face := func(t *testing.T, name string) *cards.Face {
		t.Helper()
		c, ok := reg.Lookup(name)
		if !ok || len(c.Faces) == 0 {
			t.Fatalf("%q is not in the corpus", name)
		}
		return c.Faces[0]
	}
	refused := func(t *testing.T, card, key, reason string) {
		t.Helper()
		res, err := Generate(reg, b, card, key, Options{Prefer: "start"})
		if err != nil {
			t.Fatal(err)
		}
		if res.Path != PathRefused || res.Reason != reason {
			t.Errorf("path %q reason %q, want refused %q", res.Path, res.Reason, reason)
		}
	}

	// A gated self grant: the STATION counters make the granted ability live,
	// the mana and target make it offered, and the offered comparison reads it.
	t.Run("Adagia, Windswept Bastion", func(t *testing.T) {
		f := gen(t, "Adagia, Windswept Bastion", "static#0.0")
		listIs(t, "p0 battlefield", f.Setup.P0.Battlefield,
			[]string{"Adagia, Windswept Bastion", "Plains", "Plains", "Plains", "Plains", "Memnite"})
		want := []StartCounter{{Card: "Adagia, Windswept Bastion", Kind: "CHARGE", N: 12}}
		if !reflect.DeepEqual(f.Setup.P0.Counters, want) {
			t.Errorf("p0 counters %+v, want %+v", f.Setup.P0.Counters, want)
		}
		listIs(t, "compare", f.Compare, []string{"offered"})
	})

	// A computed pump: Count$Valid Equipment.YouCtrl is made non-zero by one
	// Equipment, so the printed P/T is off its base without extra compare.
	t.Run("Adelbert Steiner", func(t *testing.T) {
		f := gen(t, "Adelbert Steiner", "static#0.0")
		if len(f.Setup.P0.Battlefield) != 2 {
			t.Fatalf("p0 battlefield %v, want the card and one Equipment", f.Setup.P0.Battlefield)
		}
		if !face(t, f.Setup.P0.Battlefield[1]).HasTypeWord("Equipment") {
			t.Errorf("%q is not an Equipment, so the computed amount stays zero", f.Setup.P0.Battlefield[1])
		}
		listIs(t, "compare", f.Compare, nil)
	})

	// A play permission from the graveyard: the card is placed there and the
	// focus casts it, with the extra card the Discard RaiseCost needs.
	t.Run("Alien Symbiosis", func(t *testing.T) {
		f := gen(t, "Alien Symbiosis", "static#0.1")
		listIs(t, "p0 graveyard", f.Setup.P0.Graveyard, []string{"Alien Symbiosis"})
		focusIs(t, f, 0, "Alien Symbiosis", "cast", 0)
		listIs(t, "compare", f.Compare, []string{"offered"})
		if len(f.Setup.P0.Hand) != 1 {
			t.Errorf("p0 hand %v, want one card to discard for RaiseCost", f.Setup.P0.Hand)
		}
	})

	// A cost reduction: the focus spell's printed cost is one generic more than
	// the one land placed, so only the reduction makes it affordable.
	t.Run("Archmage of Runes", func(t *testing.T) {
		f := gen(t, "Archmage of Runes", "static#0.0")
		if len(f.Setup.P0.Hand) != 1 {
			t.Fatalf("p0 hand %v, want one focus spell", f.Setup.P0.Hand)
		}
		spell := f.Setup.P0.Hand[0]
		sf := face(t, spell)
		if !sf.IsInstant() && !sf.IsSorcery() {
			t.Errorf("focus spell %q is not an instant or sorcery", spell)
		}
		if genericCount(sf.ManaCost) != 1 || len(f.Setup.P0.Battlefield) != 2 {
			t.Errorf("focus spell %q cost %q, battlefield %v: want one generic and one land",
				spell, sf.ManaCost, f.Setup.P0.Battlefield)
		}
		focusIs(t, f, 0, spell, "cast", 0)
		if f.Setup.P0.Battlefield[0] != "Archmage of Runes" {
			t.Errorf("p0 battlefield %v, want the source first", f.Setup.P0.Battlefield)
		}
	})

	// A granted mana ability on another permanent: the carrier's offered
	// activate option is the effect.
	t.Run("A Realm Reborn", func(t *testing.T) {
		f := gen(t, "A Realm Reborn", "static#0.0")
		listIs(t, "p0 battlefield", f.Setup.P0.Battlefield, []string{"A Realm Reborn", "Memnite"})
		listIs(t, "compare", f.Compare, []string{"offered"})
	})

	// A zone static with a granted ability: the source sits in the graveyard,
	// a land it grants the ability to is on the battlefield.
	t.Run("Riftstone Portal", func(t *testing.T) {
		f := gen(t, "Riftstone Portal", "static#0.0")
		listIs(t, "p0 graveyard", f.Setup.P0.Graveyard, []string{"Riftstone Portal"})
		if len(f.Setup.P0.Battlefield) != 1 || !face(t, f.Setup.P0.Battlefield[0]).IsLand() {
			t.Errorf("p0 battlefield %v, want a land the grant applies to", f.Setup.P0.Battlefield)
		}
		listIs(t, "compare", f.Compare, []string{"offered"})
	})

	// A computed pump over a card class: one enchantment makes X one.
	t.Run("Yavimaya Enchantress", func(t *testing.T) {
		f := gen(t, "Yavimaya Enchantress", "static#0.0")
		if len(f.Setup.P0.Battlefield) != 2 || !face(t, f.Setup.P0.Battlefield[1]).IsEnchantment() {
			t.Errorf("p0 battlefield %v, want the card and one enchantment", f.Setup.P0.Battlefield)
		}
	})

	// A computed pump over the card's own name: a second copy makes X one.
	t.Run("Relentless Rats", func(t *testing.T) {
		f := gen(t, "Relentless Rats", "static#0.0")
		listIs(t, "p0 battlefield", f.Setup.P0.Battlefield, []string{"Relentless Rats", "Relentless Rats"})
	})

	// A play permission whose affected cards are in hand: one card is placed
	// there and the focus casts it, with no mana because the permission also
	// waives the cost.
	t.Run("Omniscience", func(t *testing.T) {
		f := gen(t, "Omniscience", "static#0.0")
		listIs(t, "p0 battlefield", f.Setup.P0.Battlefield, []string{"Omniscience"})
		if len(f.Setup.P0.Hand) != 1 {
			t.Fatalf("p0 hand %v, want one card the permission plays", f.Setup.P0.Hand)
		}
		focusIs(t, f, 0, f.Setup.P0.Hand[0], "cast", 0)
		listIs(t, "compare", f.Compare, []string{"offered"})
	})

	// A computed amount over a graveyard count: one instant or sorcery in the
	// graveyard makes the characteristic-defining power non-zero.
	t.Run("Enigma Drake", func(t *testing.T) {
		f := gen(t, "Enigma Drake", "static#0.0")
		listIs(t, "p0 battlefield", f.Setup.P0.Battlefield, []string{"Enigma Drake"})
		if len(f.Setup.P0.Graveyard) != 1 {
			t.Fatalf("p0 graveyard %v, want one instant or sorcery", f.Setup.P0.Graveyard)
		}
		gf := face(t, f.Setup.P0.Graveyard[0])
		if !gf.IsInstant() && !gf.IsSorcery() {
			t.Errorf("%q in the graveyard is not an instant or sorcery", gf.Name)
		}
	})

	// A CantBlockBy static whose card is the attacker it names: the card
	// attacks and a blocker the static forbids waits on the other seat.
	t.Run("Amrou Kithkin", func(t *testing.T) {
		f := gen(t, "Amrou Kithkin", "static#0.0")
		listIs(t, "p0 battlefield", f.Setup.P0.Battlefield, []string{"Amrou Kithkin"})
		if len(f.Setup.P1.Battlefield) != 1 {
			t.Fatalf("p1 battlefield %v, want one blocker", f.Setup.P1.Battlefield)
		}
		blocker := face(t, f.Setup.P1.Battlefield[0])
		if blocker.Power() < 3 {
			t.Errorf("blocker %q power %d, want one the static forbids (power 3+)", blocker.Name, blocker.Power())
		}
		focusIs(t, f, 0, "Amrou Kithkin", "attack", 0)
	})

	// A grant whose abilities come from cards exiled with the source is a
	// linkage a start cannot place: the key keeps its named refusal.
	t.Run("Agatha's Soul Cauldron", func(t *testing.T) {
		res, err := Generate(reg, b, "Agatha's Soul Cauldron", "static#0.1", Options{Prefer: "start"})
		if err != nil {
			t.Fatal(err)
		}
		if res.Path != PathRefused || res.Reason != "no-generator:static:unobserved" {
			t.Errorf("path %q reason %q, want refused no-generator:static:unobserved", res.Path, res.Reason)
		}
	})

	// A battlefield count whose only matching object is an Aura: an unattached
	// Aura cannot be listed on the battlefield (the whole start file would be
	// refused at replay), so the count stays zero and the key keeps its refusal.
	t.Run("Aura Gnarlid", func(t *testing.T) {
		refused(t, "Aura Gnarlid", "static#0.1", "no-generator:static:computed")
	})

	// A reduction on an activated ability (Forge writes it as Type$ Ability or
	// ValidSpell$ Activated.*): a plain spell cast is not discounted, so a spell
	// start would be unaffordable and never exercised.
	t.Run("Boom Scholar", func(t *testing.T) {
		refused(t, "Boom Scholar", "static#0.0", "no-generator:static:ReduceCost")
	})
	t.Run("Dragonkin Berserker", func(t *testing.T) {
		refused(t, "Dragonkin Berserker", "static#0.0", "no-generator:static:ReduceCost")
	})

	// A self reduction gated on the spell's target or on another seat casting
	// it: a start's plain cast has no target and is by the focus seat, so the
	// reduction does not apply.
	t.Run("Bitter Downfall", func(t *testing.T) {
		refused(t, "Bitter Downfall", "static#0.0", "no-generator:static:ReduceCost")
	})
	t.Run("Accursed Witch", func(t *testing.T) {
		refused(t, "Accursed Witch", "static#0.0", "no-generator:static:ReduceCost")
	})

	// A grant whose activation cost removes loyalty: the carrier gets the
	// counters the [-10] needs, so the ability is offered (and the key is
	// exercised) instead of credited unexercised.
	t.Run("Avatar of Burgeoning Echoes", func(t *testing.T) {
		f := gen(t, "Avatar of Burgeoning Echoes", "static#0.0")
		spent := int32(0)
		for _, c := range f.Setup.P0.Counters {
			if c.Card != "Avatar of Burgeoning Echoes" && c.Kind == "LOYALTY" {
				spent = c.N
			}
		}
		if spent < 1 {
			t.Errorf("p0 counters %+v, want loyalty counters on the granted planeswalker", f.Setup.P0.Counters)
		}
	})

	// A grant whose source has its own upkeep trigger: the lowering refuses a
	// battlefield card whose upkeep trigger fires at the cut (playing the start
	// gave exercised=0), so the key keeps a named refusal rather than a start
	// that is never exercised.
	t.Run("Sporoloth Ancient", func(t *testing.T) {
		refused(t, "Sporoloth Ancient", "static#0.0", "no-generator:static:upkeep-card")
	})

	// A computed amount over the source's own counters reads one counter kind
	// once: two fields (AddPower, AddToughness) naming the same count place one
	// counter row, since a start binds each row to a distinct copy.
	t.Run("Boon of the Spirit Realm", func(t *testing.T) {
		f := gen(t, "Boon of the Spirit Realm", "static#0.0")
		seen := map[string]int{}
		for _, c := range f.Setup.P0.Counters {
			seen[c.Card+"/"+c.Kind]++
		}
		for k, n := range seen {
			if n > 1 {
				t.Errorf("counter row %s appears %d times in %+v", k, n, f.Setup.P0.Counters)
			}
		}
		if len(seen) == 0 {
			t.Errorf("p0 counters empty, want a counter on the source")
		}
	})

	// A wildcard counter kind is no counter a start can place.
	t.Run("Hancock, Ghoulish Mayor", func(t *testing.T) {
		refused(t, "Hancock, Ghoulish Mayor", "static#0.0", "no-generator:static:computed")
	})

	// A counted filter that carries a counter gate gets the counters on its
	// fixture: without them the count reads zero and the cost is not reduced.
	for _, c := range []struct{ card, key, kind string }{
		{"Cinderslash Ravager", "static#0.0", "OIL"},
		{"Hamza, Guardian of Arashin", "static#0.0", "P1P1"},
		{"Eluge, the Shoreless Sea", "static#0.1", "FLOOD"},
	} {
		t.Run(c.card, func(t *testing.T) {
			f := gen(t, c.card, c.key)
			found := false
			for _, k := range f.Setup.P0.Counters {
				if k.Card != c.card && k.Kind == c.kind && k.N >= 1 {
					found = true
				}
			}
			if !found {
				t.Errorf("p0 counters %+v, want a %s counter on the counted fixture", f.Setup.P0.Counters, c.kind)
			}
		})
	}

	// A grant whose activation cost discards a card: the start holds fodder.
	t.Run("Hollowhead Sliver", func(t *testing.T) {
		f := gen(t, "Hollowhead Sliver", "static#0.0")
		if len(f.Setup.P0.Hand) == 0 {
			t.Errorf("p0 hand %v, want a card to discard for the granted ability", f.Setup.P0.Hand)
		}
	})
}

// TestStarttokensteps is the xproof repro for the steps-needed:token class: a
// card whose needs include a token used to refuse in placeNeeds, so Forge,
// XMage and gorge never played the key. The start places the token itself
// (rules.StartSeat.Tokens), so every example builds a start file.
func TestStarttokensteps(t *testing.T) {
	reg, b := testBuilder(t)
	examples := []struct{ card, key string }{
		{"Abyssal Harvester", "cast"},
		{"Academy Instructor", "f0/t0"},
		{"Agent Venom", "cast"},
		{"Anafenza, Unyielding Lineage", "cast"},
		{"Audience with Trostani", "cast"},
		{"Baron Bertram Graywater", "cast"},
	}
	for _, tc := range examples {
		t.Run(tc.card, func(t *testing.T) {
			res, err := Generate(reg, b, tc.card, tc.key, Options{Prefer: "start"})
			if err != nil {
				t.Fatal(err)
			}
			if res.Path == PathRefused {
				t.Fatalf("%s %s refused %q, want a start file", tc.card, tc.key, res.Reason)
			}
			if res.Start == nil {
				t.Fatalf("%s %s path %q with no start file", tc.card, tc.key, res.Path)
			}
			tokens := res.Start.Setup.P0.Tokens
			if len(tokens) == 0 {
				t.Fatalf("%s %s start places no token", tc.card, tc.key)
			}
			for _, tok := range tokens {
				if _, ok := reg.Token(tok.Script); !ok {
					t.Errorf("%s %s places unknown token script %q", tc.card, tc.key, tok.Script)
				}
			}
			raw, err := res.Start.Marshal()
			if err != nil {
				t.Fatal(err)
			}
			back, err := DecodeStartFile(raw)
			if err != nil {
				t.Fatalf("%s %s: the token start does not decode: %v\n%s", tc.card, tc.key, err, raw)
			}
			if !reflect.DeepEqual(back, res.Start) {
				t.Errorf("%s %s: token start round trip differs:\n%+v\n%+v", tc.card, tc.key, back, res.Start)
			}
		})
	}
}

// TestStartStackTargetTargetingRefused: a stack target with a
// TargetValidTargeting$ restriction (Keep Safe's "target spell that targets a
// permanent you control") is not met by any inert bait, because an inert bait
// has no target of its own. The start generator used to accept the first
// no-target instant anyway and wrote a start whose focus play was never
// offered (xengine play refused it as start-not-exercised). Each of these keys
// must keep the clean start:needs-stack-target refusal instead.
func TestStartStackTargetTargetingRefused(t *testing.T) {
	reg, b := testBuilder(t)
	for _, c := range []struct{ card, key string }{
		{"Keep Safe", "cast"},          // Permanent.YouCtrl+inRealZoneBattlefield
		{"Outwit", "cast"},             // Player
		{"Intervene", "cast"},          // Creature.inRealZoneBattlefield
		{"Avoid Fate", "cast"},         // Permanent.YouCtrl+inRealZoneBattlefield
		{"Hindering Light", "cast"},    // Permanent.YouCtrl+inRealZoneBattlefield,You
		{"Confound", "cast"},           // Creature.inRealZoneBattlefield
		{"Psychic Rebuttal", "cast"},   // You
		{"Turn Aside", "cast"},         // Permanent.YouCtrl+inRealZoneBattlefield
		{"Rebuff the Wicked", "cast"},  // Permanent.YouCtrl+inRealZoneBattlefield
		{"Not of This World", "cast"},  // Permanent.YouCtrl+inRealZoneBattlefield
		{"Teferi's Response", "cast"},  // Land.YouCtrl+inRealZoneBattlefield
		{"Ring of Immortals", "f0/a0"}, // ability: Permanent.YouCtrl+inRealZoneBattlefield
	} {
		t.Run(c.card, func(t *testing.T) {
			res, err := Generate(reg, b, c.card, c.key, Options{Prefer: "start"})
			if err != nil {
				t.Fatal(err)
			}
			if res.Path != PathRefused || res.Reason != reasonNeedsStackTarget {
				t.Fatalf("path %q reason %q, want refused %q (start %+v)", res.Path, res.Reason, reasonNeedsStackTarget, res.Start)
			}
		})
	}
}

// TestStartStackTargetAbilityRefused: an ability-target bait activation is not
// any activated ability. Reroute asks for an activated ability with a single
// target (Activated.singleTarget), which an inert activation never is, and
// Rimewind Cryomancer's focus ability is gated on four snow permanents, which
// the start does not place. Both used to be written as starts by accepting the
// first activation bait and were refused by the replay as start-not-exercised;
// they must keep the clean start:needs-stack-target refusal.
func TestStartStackTargetAbilityRefused(t *testing.T) {
	reg, b := testBuilder(t)
	for _, c := range []struct{ card, key string }{
		{"Reroute", "cast"},              // Activated.singleTarget
		{"Rimewind Cryomancer", "f0/a0"}, // Activated, gated on IsPresent snow
	} {
		t.Run(c.card, func(t *testing.T) {
			res, err := Generate(reg, b, c.card, c.key, Options{Prefer: "start"})
			if err != nil {
				t.Fatal(err)
			}
			if res.Path != PathRefused || res.Reason != reasonNeedsStackTarget {
				t.Fatalf("path %q reason %q, want refused %q (start %+v)", res.Path, res.Reason, reasonNeedsStackTarget, res.Start)
			}
		})
	}
}

// TestStartStackTargetUnbaitedCastModeRefused: Wash Away has two cast modes.
// Its default mode targets a spell "that wasn't cast from its owner's hand",
// which no bait cast from the hand can be; the cleave mode can be baited, but
// the start does not script which mode the bot takes. The generator used to
// pick the cleave mode's bait and write a start the bot then played in the
// default mode, so the focus play was never offered (start-not-exercised).
func TestStartStackTargetUnbaitedCastModeRefused(t *testing.T) {
	reg, b := testBuilder(t)
	res, err := Generate(reg, b, "Wash Away", "cast", Options{Prefer: "start"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Path != PathRefused || res.Reason != reasonNeedsStackTarget {
		t.Fatalf("path %q reason %q, want refused %q (start %+v)", res.Path, res.Reason, reasonNeedsStackTarget, res.Start)
	}
}

// TestStackBaitTiming: the bait is on the stack when the focus play is taken,
// so only an instant or a flash card can be the focus (CR 117.1a: a sorcery or
// permanent spell needs an empty stack). The old check keyed off the bait's
// speed, so an instant bait let a sorcery-speed focus through even though it
// could never be cast.
func TestStackBaitTiming(t *testing.T) {
	reg, _ := testBuilder(t)
	for _, tc := range []struct {
		card string
		want bool
	}{
		{"Absorb", true},              // instant
		{"Bone to Ash", true},         // instant
		{"Reach Through Mists", true}, // instant
		{"Ambush Viper", true},        // creature with flash
		{"Grizzly Bears", false},      // creature, no flash
	} {
		c, ok := reg.Lookup(tc.card)
		if !ok || len(c.Faces) == 0 {
			t.Fatalf("%s is not a card", tc.card)
		}
		if got := castableWithBait(c.Faces[0]); got != tc.want {
			t.Errorf("castableWithBait(%s) = %v, want %v", tc.card, got, tc.want)
		}
	}
}

// TestStackBaitTimingAtCallSite drives the timing guard through
// focusPlayRefusal, not castableWithBait alone: the same counterspell as an
// instant is offered a bait, as a sorcery or a non-flash permanent spell it
// keeps the refusal (CR 117.1a: it cannot be cast with the bait on the stack),
// and with Flash it is offered one again. No corpus card is a non-instant that
// needs a stack target, so the face is built from Absorb's abilities.
func TestStackBaitTimingAtCallSite(t *testing.T) {
	reg, b := testBuilder(t)
	res, err := Generate(reg, b, "Absorb", "cast", Options{Prefer: "start"})
	if err != nil || res.Path != PathStart || res.Start == nil {
		t.Fatalf("Absorb: %+v, %v, want a start file", res, err)
	}
	c, ok := reg.Lookup("Absorb")
	if !ok || len(c.Faces) == 0 {
		t.Fatal("Absorb is not a card")
	}
	base := c.Faces[0]
	for _, tc := range []struct {
		name     string
		types    []string
		keywords []string
		want     string
	}{
		{"instant", []string{"Instant"}, nil, ""},
		{"sorcery", []string{"Sorcery"}, nil, reasonNeedsStackTarget},
		{"creature", []string{"Creature"}, nil, reasonNeedsStackTarget},
		{"flash creature", []string{"Creature"}, []string{"Flash"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			face := &cards.Face{Name: base.Name, ManaCost: base.ManaCost, Types: tc.types,
				Keywords: tc.keywords, Abilities: base.Abilities, SVars: base.SVars}
			got, err := focusPlayRefusal(reg, b, face, nil, "Absorb", res.Start)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("focusPlayRefusal(%s Absorb) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

// TestTokenStartRefusesUnmappedKind: a token kind XMage's setup token table
// cannot place is refused at generation with the clean steps-needed:token
// refusal, not written as a start that lowers to token_unmapped under -xmage.
// Bloodtithe Harvester reads a Blood token; Abyssal Harvester's generic token
// is placed (TestStarttokensteps).
func TestTokenStartRefusesUnmappedKind(t *testing.T) {
	reg, b := testBuilder(t)
	res, err := Generate(reg, b, "Bloodtithe Harvester", "cast", Options{Prefer: "start"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Path != PathRefused || res.Reason != "steps-needed:token" {
		t.Errorf("Bloodtithe Harvester: path %q reason %q, want %q", res.Path, res.Reason, "steps-needed:token")
	}
}

// TestTokenScriptKinds: a Token need arg maps to the canonical token
// definition ("<kind> Token") only when the XMage setup token table maps that
// script too. A start names a token both followers must replay, so a kind only
// Forge could place keeps the clean steps-needed:token refusal instead of
// writing a start that refuses at lowering time. A generic token need is an
// artifact creature token the table maps.
func TestTokenScriptKinds(t *testing.T) {
	reg, _ := testBuilder(t)
	for _, tc := range []struct {
		kind string
		want bool
	}{
		{"treasure", true},
		{"food", true},
		{"clue", true},
		{"blood", false},
		{"powerstone", false},
		{"map", false},
		{"junk", false},
	} {
		script, ok := tokenScript(reg, tc.kind)
		if ok != tc.want {
			t.Errorf("tokenScript(%q) ok = %v, want %v (script %q)", tc.kind, ok, tc.want, script)
			continue
		}
		if !ok {
			continue
		}
		if !lower.SetupTokenScript(script) {
			t.Errorf("tokenScript(%q) = %q, which XMage's setup token table cannot place", tc.kind, script)
		}
		c, _ := reg.Token(script)
		if c == nil || len(c.Faces) == 0 {
			t.Errorf("tokenScript(%q) = %q, not a token definition", tc.kind, script)
			continue
		}
		if !strings.EqualFold(c.Faces[0].Name, tc.kind+" Token") {
			t.Errorf("tokenScript(%q) = %q (%q), want the %s Token", tc.kind, script, c.Faces[0].Name, tc.kind)
		}
	}
	script, ok := tokenScript(reg, "")
	if !ok {
		t.Fatal("tokenScript(generic) found no definition")
	}
	if !lower.SetupTokenScript(script) {
		t.Errorf("tokenScript(generic) = %q, which XMage's setup token table cannot place", script)
	}
	if c, _ := reg.Token(script); c == nil || len(c.Faces) == 0 || !c.Faces[0].IsCreature() || !c.Faces[0].IsArtifact() {
		t.Errorf("tokenScript(generic) = %q, want an artifact creature token", script)
	}
	if _, ok := reg.Token(genericToken); ok && script != genericToken {
		t.Errorf("tokenScript(generic) = %q, want the corpus's %s", script, genericToken)
	}
	if _, ok := tokenScript(reg, "no-such-token-kind"); ok {
		t.Error("tokenScript(no-such-token-kind) found a definition")
	}
}

// TestLandsForSlashlessHybrid: Forge writes hybrid, two-generic hybrid and
// Phyrexian symbols without a slash; each still takes one basic of its first
// colour, as the slashed form does.
func TestLandsForSlashlessHybrid(t *testing.T) {
	for _, c := range []struct {
		cost string
		want []string
	}{
		{"2 UB", []string{"Island", "Island", "Island"}},
		{"2 U/B", []string{"Island", "Island", "Island"}},
		{"1 GW GW", []string{"Forest", "Forest", "Forest"}},
		{"2W 2W 2W", []string{"Plains", "Plains", "Plains"}},
		{"UP", []string{"Island"}},
		{"1 RWP", []string{"Mountain", "Mountain"}},
		{"3 R", []string{"Mountain", "Mountain", "Mountain", "Mountain"}},
	} {
		if got := landsFor(splitCost(c.cost), basicFor(c.cost)); !slices.Equal(got, c.want) {
			t.Errorf("landsFor(%q) = %v, want %v", c.cost, got, c.want)
		}
	}
	if !hasShard("2 WB WB", 'B') || hasShard("2 WB WB", 'U') {
		t.Error("hasShard misreads a slashless hybrid")
	}
}

// TestStartstaticattached: a static whose effect reads the bearer of an Aura or
// Equipment, or a condition the start can place, is a start and not a refusal:
// the Aura is cast on a bearer, the Equipment is equipped to one, and a life
// total or counted permanents meet the condition.
func TestStartstaticattached(t *testing.T) {
	t.Run("start", func(t *testing.T) {
		checkStartRows(t, []startRow{
			{card: "Airtight Alibi", key: "static#0.0", maxTurns: 1, p0Hand: []string{"Airtight Alibi"},
				p0BF: []string{"Forest", "Forest", "Forest", "Wall of Stone"}, focusCard: "Airtight Alibi", focusKind: "cast"},
			{card: "Alien Symbiosis", key: "static#0.0", maxTurns: 1, p0Hand: []string{"Alien Symbiosis"},
				p0BF: []string{"Swamp", "Swamp", "Wall of Stone"}, focusCard: "Alien Symbiosis", focusKind: "cast"},
			{card: "Holy Strength", key: "static#0.0", maxTurns: 1, p0Hand: []string{"Holy Strength"},
				p0BF: []string{"Plains", "Wall of Stone"}, focusCard: "Holy Strength", focusKind: "cast"},
			// an Equipment is on the battlefield with its bearer and the lands of its equip cost
			{card: "Bonesplitter", key: "static#0.0", maxTurns: 1,
				p0BF: []string{"Bonesplitter", "Plains", "Wall of Stone"}, focusCard: "Bonesplitter", focusKind: "ability"},
			// the bearer is the permanent the condition counts: no second legal target
			{card: "Temur Runemark", key: "static#0.1", maxTurns: 1, p0Hand: []string{"Temur Runemark"},
				p0BF: []string{"Forest", "Forest", "Forest", "Wall of Stone"}, focusCard: "Temur Runemark", focusKind: "cast"},
			// a life-total condition is the placed life; no action is forced
			{card: "Angel of Vitality", key: "static#0.0", maxTurns: 1, p0BF: []string{"Angel of Vitality"}},
			// an effect that changes the bearer shows on it: a control Aura's bearer is the
			// other seat's, as the caster's own is theirs already
			{card: "Control Magic", key: "static#0.0", maxTurns: 1, p0Hand: []string{"Control Magic"},
				p0BF: rep("Island", 4), p1BF: []string{"Wall of Stone"}, focusCard: "Control Magic", focusKind: "cast"},
			{card: "Mind Control", key: "static#0.0", maxTurns: 1, p0Hand: []string{"Mind Control"},
				p0BF: rep("Island", 5), p1BF: []string{"Wall of Stone"}, focusCard: "Mind Control", focusKind: "cast"},
			// a land Aura whose cost lands are also what it can enchant: they show the effect too
			{card: "Consecrate Land", key: "static#0.0", maxTurns: 1, p0Hand: []string{"Consecrate Land"},
				p0BF: rep("Plains", 2), focusCard: "Consecrate Land", focusKind: "cast"},
		})
	})
	// an effect that does not show on the bearer, or that the cast could put
	// on a permanent it does not show on, is refused and not credited unplayed
	t.Run("unseen", func(t *testing.T) {
		checkStartRows(t, []startRow{
			// the land types are set to Island/Swamp: the cost lands already are those
			{card: "Spreading Seas", key: "static#0.0", reason: "no-generator:static:bearer"},
			{card: "Lingering Mirage", key: "static#0.0", reason: "no-generator:static:bearer"},
			{card: "Evil Presence", key: "static#0.0", reason: "no-generator:static:bearer"},
			{card: "Contaminated Ground", key: "static#0.0", reason: "no-generator:static:bearer"},
			// control of a land: the cost lands are the caster's own already
			{card: "Annex", key: "static#0.0", reason: "no-generator:static:bearer"},
			{card: "Conquer", key: "static#0.0", reason: "no-generator:static:bearer"},
			// the card types are replaced: a cost land that is a Land already shows nothing
			{card: "Song of the Dryads", key: "static#0.0", reason: "no-generator:static:bearer"},
			{card: "Imprisoned in the Moon", key: "static#0.0", reason: "no-generator:static:bearer"},
			// the IsPresent permanent is a second creature the cast could target
			{card: "Abzan Runemark", key: "static#0.1", reason: "no-generator:static:bearer"},
			{card: "Multiclass Baldric", key: "static#0.0", reason: "no-generator:static:bearer"},
		})
	})
	// counted permanents are copies of one carrier, which the legend rule would
	// thin out if it were legendary: no seat has two of one legendary name
	t.Run("no legendary copies", func(t *testing.T) {
		reg, b := testBuilder(t)
		for _, r := range []struct{ card, key, reason string }{
			// three legendary creatures, and no carrier that is not legendary
			{"Kolvori, God of Kinship", "static#0.0", "no-generator:static:conditional"},
			{"Temur Runemark", "static#0.1", ""},
		} {
			res, err := Generate(reg, b, r.card, r.key, Options{Prefer: "start"})
			if r.reason != "" {
				if err != nil || res.Start != nil || res.Reason != r.reason {
					t.Errorf("%s: %v path %q reason %q, want refused %q", r.card, err, res.Path, res.Reason, r.reason)
				}
				continue
			}
			if err != nil || res.Start == nil {
				t.Fatalf("%s: %v path %q reason %q, want a start", r.card, err, res.Path, res.Reason)
			}
			for _, bf := range [][]string{res.Start.Setup.P0.Battlefield, res.Start.Setup.P1.Battlefield} {
				seen := map[string]bool{}
				for _, n := range bf {
					c, ok := reg.Lookup(n)
					if ok && c.Faces[0].IsLegendary() && n != r.card && seen[n] {
						t.Errorf("%s: %s is placed twice on one battlefield %v", r.card, n, bf)
					}
					seen[n] = true
				}
			}
		}
	})
	// the same for an unattached static: the carrier it affects shows the effect
	t.Run("carrier shows", func(t *testing.T) {
		checkStartRows(t, []startRow{
			// "each land is a Swamp" is set on a Plains, not on a Swamp; a creature
			// that loses a keyword has it
			{card: "Blanket of Night", key: "static#0.0", maxTurns: 1, p0BF: []string{"Blanket of Night", "Plains"}},
			{card: "Archetype of Courage", key: "static#0.1", maxTurns: 1,
				p0BF: []string{"Archetype of Courage"}, p1BF: []string{"Boros Recruit"}},
		})
	})
	// the bearer meets the static's Affected filter, not only what the Aura
	// enchants or the equip targets; a static no bearer can meet, or whose
	// attachment is negated or qualified, is refused and never a false start
	t.Run("affected", func(t *testing.T) {
		reg, b := testBuilder(t)
		for _, r := range []struct {
			card, key, bearerType string
		}{
			{"Equestrian Skill", "static#0.1", "Human"},
			{"Bladed Bracers", "static#0.1", ""},
		} {
			res, err := Generate(reg, b, r.card, r.key, Options{Prefer: "start"})
			if err != nil || res.Start == nil {
				t.Fatalf("%s: %v path %q reason %q", r.card, err, res.Path, res.Reason)
			}
			var bearer string
			for _, n := range append(res.Start.Setup.P0.Battlefield, res.Start.Setup.P1.Battlefield...) {
				if c, ok := reg.Lookup(n); ok && n != r.card && c.Faces[0].IsCreature() {
					bearer = n
				}
			}
			c, _ := reg.Lookup(bearer)
			if bearer == "" || r.bearerType == "" && !(c.Faces[0].HasTypeWord("Human") || c.Faces[0].HasTypeWord("Angel")) ||
				r.bearerType != "" && !c.Faces[0].HasTypeWord(r.bearerType) {
				t.Errorf("%s: bearer %q does not meet the static's Affected filter (%v)", r.card, bearer, res.Start.Setup)
			}
		}
	})
	t.Run("refused", func(t *testing.T) {
		reg, b := testBuilder(t)
		for _, r := range []struct{ card, key string }{
			{"Vampirism", "static#0.1"},        // Creature.YouCtrl+!EnchantedBy: the bearer is excluded
			{"Face of Divinity", "static#0.1"}, // Creature.EnchantedBy Aura.Other: one Aura only
		} {
			res, err := Generate(reg, b, r.card, r.key, Options{Prefer: "start"})
			if err != nil {
				t.Fatal(err)
			}
			if res.Start != nil || res.Path != PathRefused {
				t.Errorf("%s %s: path %q reason %q, want a refusal", r.card, r.key, res.Path, res.Reason)
			}
		}
	})
	t.Run("life", func(t *testing.T) {
		reg, b := testBuilder(t)
		res, err := Generate(reg, b, "Angel of Vitality", "static#0.0", Options{Prefer: "start"})
		if err != nil || res.Start == nil {
			t.Fatalf("%v path %q reason %q", err, res.Path, res.Reason)
		}
		if l := res.Start.Setup.P0.Life; l == nil || *l != 25 {
			t.Errorf("p0 life %v, want 25", l)
		}
	})
}

// TestStartfacestarts: a key of a double-faced or modal card's back face is
// started from the card on the battlefield on its back face (BackFace), the
// focus play the back face's own; a face gorge cannot place there (flip, meld,
// specialize, split) is refused by the layout, not by the old catch-all
// "no-generator:face". A Transformed trigger is the exception: a BackFace
// placement is an enter, not a transform (CR 701.27), so its start stands the
// card front-up and transforms it on the line (TestStartTransformedTrigger).
func TestStartfacestarts(t *testing.T) {
	reg, b := testBuilder(t)
	const (
		aclazotz = "Aclazotz, Deepest Betrayal // Temple of the Dead"
		arlinn   = "Arlinn Kord // Arlinn, Embraced by the Moon"
		wolfkin  = "Wolfkin Outcast // Wedding Crasher"
	)
	for _, tc := range []struct {
		card, key, kind string
		ability         int
		front           bool
	}{
		{aclazotz, "f1/a0", "ability", 0, false},         // a modal back face (a land)
		{arlinn, "f1/a1", "ability", 1, false},           // a transformed planeswalker's loyalty ability
		{wolfkin, "f1/t0", "", 0, false},                 // a transformed back face's trigger
		{"Voldaren Pariah", "f1/t0", "ability", 0, true}, // a Transformed trigger: front face, own transform ability
		{"Kindly Stranger", "f1/t0", "cast", 0, true},    // a Transformed trigger: front face, one-card enabler
	} {
		if _, ok := reg.Lookup(tc.card); !ok {
			t.Fatalf("%s is not in the corpus", tc.card)
		}
		res, err := Generate(reg, b, tc.card, tc.key, Options{Prefer: "start"})
		if err != nil {
			t.Fatalf("%s %s: %v", tc.card, tc.key, err)
		}
		if res.Start == nil {
			t.Errorf("%s %s: %s %q, want a start", tc.card, tc.key, res.Path, res.Reason)
			continue
		}
		f := res.Start
		seats := [2]StartSeat{f.Setup.P0, f.Setup.P1}
		backed := 0
		for _, sd := range seats {
			for _, n := range sd.BackFace {
				backed++
				if n != tc.card || !slices.Contains(sd.Battlefield, n) {
					t.Errorf("%s %s: back_face %q is not a battlefield card of its seat %v", tc.card, tc.key, n, sd.Battlefield)
				}
			}
		}
		if tc.front {
			if backed != 0 {
				t.Errorf("%s %s: %d back_face placements, want none on a transformed start", tc.card, tc.key, backed)
			}
			if !slices.Contains(f.Setup.P0.Battlefield, tc.card) {
				t.Errorf("%s %s: p0 battlefield %v, want the card front-up", tc.card, tc.key, f.Setup.P0.Battlefield)
			}
		} else if backed != 1 {
			t.Errorf("%s %s: %d back_face placements, want 1", tc.card, tc.key, backed)
		}
		if tc.kind != "" {
			if f.Focus == nil {
				t.Errorf("%s %s: focus missing, want %s ability %d", tc.card, tc.key, tc.kind, tc.ability)
			} else if tc.front {
				// a transformed start's focus is the front face's transform
				// ability, or the one-card enabler that stands in for it
				if tc.kind == "ability" && (f.Focus.Card != tc.card || f.Focus.Kind != tc.kind || f.Focus.Ability != tc.ability) {
					t.Errorf("%s %s: focus %+v, want the front face's transform ability %d", tc.card, tc.key, f.Focus, tc.ability)
				}
				if tc.kind == "cast" && f.Focus.Kind != "cast" {
					t.Errorf("%s %s: focus %+v, want the one-card enabler cast", tc.card, tc.key, f.Focus)
				}
			} else if f.Focus.Card != tc.card || f.Focus.Kind != tc.kind || f.Focus.Ability != tc.ability {
				t.Errorf("%s %s: focus %+v, want %s ability %d on the card", tc.card, tc.key, f.Focus, tc.kind, tc.ability)
			}
		}
	}
	for _, tc := range []struct{ card, key, reason string }{
		{"Wyll, Pact-Bound Duelist", "f1/t0", "no-generator:face:specialize"},
		{"Walk-In Closet // Forgotten Cellar", "f1/t0", "no-generator:face:split"},
		{"Orochi Eggwatcher // Shidako, Broodmistress", "f1/a0", "no-generator:face:flip"},
		{"Titania, Voice of Gaea // Titania, Gaea Incarnate", "f1/a0", "no-generator:face:meld"},
	} {
		res, err := Generate(reg, b, tc.card, tc.key, Options{Prefer: "start"})
		if err != nil {
			t.Fatalf("%s %s: %v", tc.card, tc.key, err)
		}
		if res.Path != PathRefused || res.Reason != tc.reason {
			t.Errorf("%s %s: %s %q, want refused %q", tc.card, tc.key, res.Path, res.Reason, tc.reason)
		}
	}
}

// TestStartcombattriggers: a combat trigger (blocks, becomes blocked, combat
// damage dealt or taken) is one scripted combat with no forced play: the active
// seat attacks with exactly Attack, the other blocks with exactly Block (or not
// at all), the card beside them. A trigger on an Aura's or Equipment's bearer
// has no start file to attach with, and stays refused.
func TestStartcombattriggers(t *testing.T) {
	reg, b := testBuilder(t)
	rows := []struct {
		card, key     string
		active        int
		p0BF, p1BF    []string
		attack, block string
	}{
		// the card attacks and is blocked by the fixture
		{card: "Acolyte of the Inferno", key: "f0/t0", p0BF: []string{"Acolyte of the Inferno"}, p1BF: []string{"Grizzly Bears"},
			attack: "Acolyte of the Inferno", block: "Grizzly Bears"},
		{card: "Ammit Eternal", key: "f0/t2", p0BF: []string{"Ammit Eternal"}, p1BF: []string{"Grizzly Bears"},
			attack: "Ammit Eternal", block: "Grizzly Bears"},
		// the card blocks the fixture's attack
		{card: "Aether Membrane", key: "f0/t0", active: 1, p0BF: []string{"Aether Membrane"}, p1BF: []string{"Grizzly Bears"},
			attack: "Grizzly Bears", block: "Aether Membrane"},
		// the card connects: nothing blocks
		{card: "Akki Lavarunner // Tok-Tok, Volcano Born", key: "f0/t0", p0BF: []string{"Akki Lavarunner // Tok-Tok, Volcano Born"},
			attack: "Akki Lavarunner // Tok-Tok, Volcano Born"},
		// the card is dealt the damage
		{card: "Pious Warrior", key: "f0/t0", active: 1, p0BF: []string{"Pious Warrior"}, p1BF: []string{"Grizzly Bears"},
			attack: "Grizzly Bears", block: "Pious Warrior"},
	}
	for _, r := range rows {
		t.Run(r.card+"/"+r.key, func(t *testing.T) {
			res, err := Generate(reg, b, r.card, r.key, Options{Prefer: "start"})
			if err != nil {
				t.Fatal(err)
			}
			if res.Start == nil {
				t.Fatalf("path %q reason %q, want a start file", res.Path, res.Reason)
			}
			f := res.Start
			if f.Active != r.active {
				t.Errorf("active %d, want %d", f.Active, r.active)
			}
			listIs(t, "p0 battlefield", f.Setup.P0.Battlefield, r.p0BF)
			listIs(t, "p1 battlefield", f.Setup.P1.Battlefield, r.p1BF)
			fo := f.Focus
			if fo == nil || fo.Kind != "" || fo.Attack != r.attack || fo.Block != r.block {
				t.Errorf("focus %+v, want kind \"\" attack %q block %q", fo, r.attack, r.block)
			}
			raw, err := f.Marshal()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := DecodeStartFile(raw); err != nil {
				t.Errorf("start file does not decode: %v", err)
			}
		})
	}
	res, err := Generate(reg, b, "Aqueous Form", "f0/t0", Options{Prefer: "start"})
	if err != nil || res.Path != PathRefused || res.Reason != "no-generator:trigger:Attacks:filter" {
		t.Errorf("Aqueous Form: %+v %v, want refused no-generator:trigger:Attacks:filter", res, err)
	}
}

// TestStartcombattriggersRefused: a combat start that cannot make its trigger
// fire is a refusal, not a start the census credits: a damage source with no
// power deals no damage (CR 120.8), a trigger condition (a monarch, an empty
// hand, a damage amount) is a state the start does not place, an attacker with
// menace is not blocked by the one blocker the line scripts (CR 702.111b), and a
// card certdeck cannot build keeps a named refusal.
func TestStartcombattriggersRefused(t *testing.T) {
	reg, b := testBuilder(t)
	for _, r := range []struct{ card, key, reason string }{
		{"Surge Mare", "f0/t0", "no-generator:trigger:DamageDone:power"},                        // a 0-power card deals the damage
		{"Tamiyo, Upriser Crowned", "f0/t1", "no-generator:trigger:DamageDoneOnce:condition"},   // CheckDefinedPlayer: monarch
		{"Kiyomaro, First to Stand", "f0/t0", "no-generator:trigger:DamageDealtOnce:condition"}, // CheckSVar: hand size
		{"Euru, Acorn Scrounger", "f0/t1", "no-generator:trigger:DamageDoneOnce:unsupported"},   // certdeck: MakeCard
		{"Labyrinth Raptor", "f0/t0", "no-generator:trigger:AttackerBlocked:filter"},            // the only carrier has menace
	} {
		t.Run(r.card+"/"+r.key, func(t *testing.T) {
			res, err := Generate(reg, b, r.card, r.key, Options{Prefer: "start"})
			if err != nil {
				t.Fatal(err)
			}
			if res.Path != PathRefused || res.Reason != r.reason {
				t.Errorf("path %q reason %q, want refused %s", res.Path, res.Reason, r.reason)
			}
		})
	}
}

// TestStartcombattriggersDealer: the creature that deals the damage has power:
// Vulture, Feathered Fiend ("creatures you control with flying deal combat
// damage") once got Ornithopter (0/2) as the flier. No carrier or fixture the
// shape picks for a damage source may have no power or menace.
func TestStartcombattriggersDealer(t *testing.T) {
	reg, b := testBuilder(t)
	for _, r := range []struct{ card, key string }{
		{"Vulture, Feathered Fiend", "f0/t0"},
		{"Kastral, the Windcrested", "f0/t0"},
		{"Mu Yanling, Wind Rider", "f0/t1"},
		{"Heroes in a Half Shell", "f0/t0"},
	} {
		t.Run(r.card, func(t *testing.T) {
			res, err := Generate(reg, b, r.card, r.key, Options{Prefer: "start"})
			if err != nil {
				t.Fatal(err)
			}
			if res.Start == nil || res.Start.Focus == nil || res.Start.Focus.Attack == "" {
				t.Fatalf("path %q reason %q, want a combat start", res.Path, res.Reason)
			}
			c, ok := reg.Lookup(res.Start.Focus.Attack)
			if !ok {
				t.Fatalf("attacker %q not in the corpus", res.Start.Focus.Attack)
			}
			if f := c.Faces[0]; f.Power() <= 0 || f.HasKeyword("Menace") {
				t.Errorf("attacker %s has power %d (menace %v)", f.Name, f.Power(), f.HasKeyword("Menace"))
			}
		})
	}
}

// TestCombatTrigStartSeatRequired: a seat the mode requires is kept: a card that
// deals the damage to "You" (its controller) is on the other seat's side, which
// the card cannot be on, so the start is refused and not seated on seat 0.
func TestCombatTrigStartSeatRequired(t *testing.T) {
	reg, b := testBuilder(t)
	c, ok := reg.Lookup("Grizzly Bears")
	if !ok {
		t.Fatal("Grizzly Bears is not in the corpus")
	}
	trig := &cards.Trigger{Mode: "DamageDone", Params: map[string]string{"ValidSource": "Card.Self", "ValidTarget": "You", "CombatDamage": "True"}}
	f := &StartFile{}
	if why := combatTrigStart(reg, b, c.Faces[0], "Grizzly Bears", trig, f); why == "" || f.Focus != nil {
		t.Errorf("combatTrigStart = %q focus %+v, want a refusal", why, f.Focus)
	}
}

// TestStartTransformedTrigger: a back-face "when this transforms into
// CARDNAME" trigger (Mode Transformed) fires only on the transform INTO that
// face (CR 701.27), so its start stands the card front-up and transforms it
// on the line: with the front face's own activated transform ability where
// it has one, else with a one-card enabler (Moonmist for a Human). It must
// never list the card as BackFace: that placement is an enter, not a
// transform, so the trigger never fires. A front face with no placeable cause
// is refused by a narrower reason: a day/night border (Howlpack Piper), a
// battlefield upkeep/draw trigger (Huntmaster of the Fells, Afflicted
// Deserter, Ulrich of the Krallenhorde), an attack or conditional phase
// trigger, or no cause at all; a front-face Transformed trigger or a watcher
// is not this recipe's.
func TestStartTransformedTrigger(t *testing.T) {
	checkStartRows(t, []startRow{
		// own activated transform ability
		{card: "Voldaren Pariah", key: "f1/t0", maxTurns: 1,
			p0BF:      cat([]string{"Voldaren Pariah"}, rep("Memnite", 3)),
			focusCard: "Voldaren Pariah", focusKind: "ability"},
		{card: "Blightreaper Thallid", key: "f1/t0", maxTurns: 1,
			p0BF:      cat([]string{"Blightreaper Thallid"}, rep("Swamp", 3), []string{"Forest"}),
			focusCard: "Blightreaper Thallid", focusKind: "ability"},
		// one-card enabler: Moonmist for a Human, transformed by its resolution
		{card: "Kindly Stranger", key: "f1/t0", maxTurns: 1,
			p0Hand: []string{"Moonmist"}, p0BF: []string{"Kindly Stranger", "Forest", "Forest"},
			p1BF:      []string{"Shield Sphere"},
			focusCard: "Moonmist", focusKind: "cast"},
		{card: "Ultimecia, Time Sorceress", key: "f1/t0", maxTurns: 1,
			p0Hand: []string{"Moonmist"}, p0BF: []string{"Ultimecia, Time Sorceress", "Forest", "Forest"},
			p1BF:      []string{"Shield Sphere"},
			focusCard: "Moonmist", focusKind: "cast"},
		// no placeable one-line cause: refused by name, not as the old catch-all
		{card: "Thing in the Ice", key: "f1/t0", reason: "no-generator:trigger:Transformed:no-cause"},
		{card: "Alluring Suitor", key: "f1/t0", reason: "no-generator:trigger:Transformed:attack"},
		{card: "Archangel Avacyn", key: "f1/t0", reason: "no-generator:trigger:Transformed:no-cause"},
		// a daybound front cannot be transformed by Moonmist (CR 702.145b)
		{card: "Howlpack Piper", key: "f1/t1", reason: "no-generator:trigger:Transformed:day-night"},
		// a battlefield upkeep transform refuses the lowering's cut
		{card: "Huntmaster of the Fells", key: "f1/t0", reason: "no-generator:trigger:Transformed:condition"},
		{card: "Afflicted Deserter", key: "f1/t0", reason: "no-generator:trigger:Transformed:condition"},
		{card: "Ulrich of the Krallenhorde", key: "f1/t0", reason: "no-generator:trigger:Transformed:condition"},
		{card: "Hawkins National Laboratory", key: "f1/t0", reason: "no-generator:trigger:Transformed:condition"},
		{card: "Ashling, Rekindled", key: "f1/t0", reason: "no-generator:trigger:Transformed:condition"},
		{card: "Sidequest: Raise a Chocobo", key: "f1/t0", reason: "no-generator:trigger:Transformed:condition"},
		// a front-face trigger needs the back face placed and transformed back
		{card: "Ashling, Rekindled", key: "f0/t1", reason: "no-generator:trigger:Transformed:front"},
		// a watcher is not this card's transform
		{card: "Corruption of Towashi", key: "f0/t1", reason: "no-generator:trigger:Transformed:not-self"},
	})
	reg, b := testBuilder(t)
	for _, tc := range []struct{ card, key string }{
		{"Voldaren Pariah", "f1/t0"},
		{"Blightreaper Thallid", "f1/t0"},
		{"Kindly Stranger", "f1/t0"},
		{"Ultimecia, Time Sorceress", "f1/t0"},
	} {
		res, err := Generate(reg, b, tc.card, tc.key, Options{Prefer: "start"})
		if err != nil || res.Start == nil {
			t.Fatalf("%s %s: %+v %v", tc.card, tc.key, res, err)
		}
		f := res.Start
		backed := len(f.Setup.P0.BackFace) + len(f.Setup.P1.BackFace)
		if backed != 0 {
			t.Errorf("%s %s: %d back_face placements, want none: a BackFace enter is not a transform", tc.card, tc.key, backed)
		}
		if !slices.Contains(f.Setup.P0.Battlefield, tc.card) {
			t.Errorf("%s %s: p0 battlefield %v, want the card front-up", tc.card, tc.key, f.Setup.P0.Battlefield)
		}
	}
}
