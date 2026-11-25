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
		{"Holy Strength", "static#0.0", "no-generator:static:attached"},
		// the attack/block is not offered, or the effect is not seen
		{"Magus of the Moat", "combat#0.attack", "no-generator:combat:restricted"},
		{"Frenetic Raptor", "combat#0.block", "no-generator:combat:restricted"},
		{"Blazing Archon", "combat#0.block", "no-generator:combat:restricted"},
		{"Yavimaya Enchantress", "static#0.0", "no-generator:static:computed"},
		{"Relentless Rats", "static#0.0", "no-generator:static:computed"},
		{"Riftstone Portal", "static#0.0", "no-generator:static:zone"},
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
// has a legal target (CR 115.1a).
func TestStartstacktarget(t *testing.T) {
	reg, b := testBuilder(t)
	for _, c := range []struct {
		card string
		// creature is whether the key's target must be a creature spell
		// (Bone to Ash's "Counter target creature spell").
		creature bool
	}{
		{card: "Absorb"},                      // Counter target spell
		{card: "Admiral's Order"},             // Counter target spell
		{card: "Assert Authority"},            // Counter target spell, exile instead
		{card: "Condescend"},                  // Counter target spell unless its controller pays {X}
		{card: "Bone to Ash", creature: true}, // Counter target creature spell
	} {
		t.Run(c.card, func(t *testing.T) {
			res, err := Generate(reg, b, c.card, "cast", Options{Prefer: "start"})
			if err != nil {
				t.Fatal(err)
			}
			if res.Path != PathStart || res.Start == nil {
				t.Fatalf("path %q reason %q, want a start file", res.Path, res.Reason)
			}
			f := res.Start
			if f.Focus == nil || f.Focus.Bait == "" {
				t.Fatalf("focus %+v, want a scripted bait cast", f.Focus)
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
		})
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
