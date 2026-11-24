package scen

import (
	"bytes"
	"encoding/json"
	"reflect"
	"slices"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/certdeck"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
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
// Frog-Squirrels); Crypt Feaster [graveyard opp-blockers] (enabler Shifting
// Wall on p1); Banner of Kinship [counters:FELLOWSHIP]; every other fixture
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
			listIs(t, "p1 battlefield", f.Setup.P1.Battlefield, []string{"Shifting Wall"})
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
		{"Aggressive Mammoth", "static#0.0", want{path: "steps"}, want{path: "refused", reason: "no-generator:static"}},
		{"Magnigoth Sentry", "combat#0.block", want{path: "steps"}, want{path: "refused", reason: "no-generator:combat"}},
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
	for _, k := range append([][2]string{{"Thornvault Forager", "f0/a1"}, {"Banner of Kinship", "static#0.0"}}, fixtureKeys...) {
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
