package scen

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/certdeck"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/cov"
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
		// staticKeyStart refuses Banner's static (no carrier can observe it),
		// so the named static refusal stands; casting the card cannot turn an
		// unobservable static into an exercised key.
		{"Banner of Kinship", "static#0.0", want{path: "steps"}, want{path: "refused", reason: "no-generator:static:affected"}},
		// a ChooseColor card is cast, not placed, so its choice is made by the
		// engines' own decision; a land refuses by name, and a choice that is
		// not state (a player) keeps its kind
		{"Curse of Wizardry", "f0/t0", want{path: "steps"}, want{path: "start", check: asEntersCast("Curse of Wizardry", 5)}},
		{"Jeweled Torque", "f0/t0", want{path: "steps"}, want{path: "start", check: asEntersCast("Jeweled Torque", 5)}},
		{"Lurebound Scarecrow", "f0/t0", want{path: "start", check: asEntersCast("Lurebound Scarecrow", 0)}, want{path: "start", check: asEntersCast("Lurebound Scarecrow", 0)}},
		{"Unclaimed Territory", "f0/a1", want{path: "steps"}, want{path: "refused", reason: "as-enters:land"}},
		{"Big Apple, 3 a.m.", "f0/a1", want{path: "steps"}, want{path: "refused", reason: "as-enters:land"}},
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

// TestScenStartAsEntersPlaced: a card whose as-enters replacement asks a
// state-backed choice is never placed at genesis. The placement would be a
// real move this turn, so the live cut would carry Object.EnteredThisTurn and
// Object.AcqTurn, which the lowering's setup drive cannot restate (it places
// permanents directly, CR 302.6). A static key keeps staticKeyStart's own
// setup (its affected carrier, Compare fields and named refusals) and only the
// card's placement moves to hand, where the focus casts it; an ability or
// BecomesTarget trigger key casts the card as a prelude so its own action is
// still exercised afterwards (a plain cast item would credit the ability or
// trigger without ever taking it). A choice setup cannot read (a player) keeps
// its named refusal, a land -- played, not cast -- keeps "as-enters:land", and
// an event a creature cast this turn cannot produce keeps the placement
// refusal.
func TestScenStartAsEntersPlaced(t *testing.T) {
	reg, b := testBuilder(t)
	for _, tc := range []struct {
		card, key, focusKind string
		ability              int
		prelude              bool
		carrier              string
		compare              []string
	}{
		{"Adaptive Automaton", "static#0.0", "cast", 0, false, "", nil},
		// staticKeyStart's affected carrier and Compare survive the move to
		// hand; a plain cast start would drop both.
		{"Rukarumel, Biologist", "static#0.0", "cast", 0, false, "Memnite", nil},
		{"Kindred Boon", "static#0.0", "cast", 0, false, "Crimson Kobolds", []string{"keywords"}},
		{"Lifecraft Engine", "f0/a0", "ability", 0, true, "", nil},
		{"Roaming Throne", "f0/t0", "cast", 0, true, "", nil}, // the opponent's targeting spell is the focus
	} {
		t.Run(tc.card+"/"+tc.key, func(t *testing.T) {
			res, err := Generate(reg, b, tc.card, tc.key, Options{Prefer: "start"})
			if err != nil {
				t.Fatal(err)
			}
			if res.Path != PathStart || res.Start == nil {
				t.Fatalf("path %q reason %q, want a start", res.Path, res.Reason)
			}
			if !slices.Contains(res.Start.Setup.P0.Hand, tc.card) || slices.Contains(res.Start.Setup.P0.Battlefield, tc.card) {
				t.Errorf("p0 hand %v battlefield %v, want %s in hand and not placed",
					res.Start.Setup.P0.Hand, res.Start.Setup.P0.Battlefield, tc.card)
			}
			if tc.carrier != "" && !slices.Contains(res.Start.Setup.P0.Battlefield, tc.carrier) {
				t.Errorf("p0 battlefield %v, want staticKeyStart's carrier %q", res.Start.Setup.P0.Battlefield, tc.carrier)
			}
			if !slices.Equal(res.Start.Compare, tc.compare) {
				t.Errorf("compare %v, want %v: the static setup's observation fields were dropped", res.Start.Compare, tc.compare)
			}
			if tc.prelude {
				if res.Start.Prelude != tc.card || res.Start.PreludeSeat != 0 {
					t.Errorf("prelude %q seat %d, want %q seat 0", res.Start.Prelude, res.Start.PreludeSeat, tc.card)
				}
				if res.Start.Focus == nil || res.Start.Focus.Kind != tc.focusKind {
					t.Errorf("focus %+v, want a %s action after the prelude", res.Start.Focus, tc.focusKind)
				}
				if res.Start.Focus != nil && res.Start.Focus.Kind == "cast" && res.Start.Focus.Card == tc.card {
					t.Errorf("focus %+v is the prelude cast, not the key's own action", res.Start.Focus)
				}
				return
			}
			if res.Start.Prelude != "" {
				t.Errorf("prelude %q, want none for a plain cast start", res.Start.Prelude)
			}
			focusIs(t, res.Start, 0, tc.card, tc.focusKind, tc.ability)
		})
	}
	for _, tc := range []struct{ card, key, reason string }{
		{"Sower of Discord", "f0/t0", "as-enters:player"},
		{"Sorcerous Spyglass", "static#0.0", "as-enters:player"},
		{"Unclaimed Territory", "f0/a1", "as-enters:land"},
		// A static whose own setup refuses keeps its named refusal: the cast
		// must not replace a key staticKeyStart cannot observe.
		{"Banner of Kinship", "static#0.0", "no-generator:static:affected"},
		{"Iona, Shield of Emeria", "static#0.0", "no-generator:static:CantBeCast"},
		{"Cloud Key", "static#0.0", "no-generator:static:ReduceCost"},
		// A trigger whose event a creature cast this turn cannot produce
		// (an attack, another creature's entry, an upkeep, a chosen-colour
		// spell) keeps the named refusal instead of a cast-only item.
		{"Titan of Littjara", "f0/t1", reasonAsEntersStamp},
		{"Bloodline Pretender", "f0/t0", reasonAsEntersStamp},
		{"Herald's Horn", "f0/t0", reasonAsEntersStamp},
	} {
		t.Run(tc.card+"/"+tc.key, func(t *testing.T) {
			res, err := Generate(reg, b, tc.card, tc.key, Options{Prefer: "start"})
			if err != nil {
				t.Fatal(err)
			}
			if res.Path != PathRefused || res.Start != nil || res.Reason != tc.reason {
				t.Fatalf("path %q reason %q, want refused %q", res.Path, res.Reason, tc.reason)
			}
		})
	}
	// Gathering Stone's self-ETB trigger casts it, so its entry and type
	// choice are on the tape rather than a genesis placement.
	res, err := Generate(reg, b, "Gathering Stone", "f0/t0", Options{Prefer: "start"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Path != PathStart || res.Start == nil {
		t.Fatalf("Gathering Stone: path %q reason %q, want a start", res.Path, res.Reason)
	}
	if !slices.Contains(res.Start.Setup.P0.Hand, "Gathering Stone") {
		t.Errorf("p0 hand %v, want Gathering Stone", res.Start.Setup.P0.Hand)
	}
	focusIs(t, res.Start, 0, "Gathering Stone", "cast", 0)
}

// TestScenFixtureSkipsAsEnters: the same placement stamp the focus card's
// refusal guards applies to a battlefield fixture, so the picker must not
// choose a card whose own entry asks a choice. A fixture that would be the
// only match picks none (the calling recipe then refuses by its own reason)
// rather than emitting a start the lower rejects; an ordinary fixture is
// still picked.
func TestScenFixtureSkipsAsEnters(t *testing.T) {
	reg, b := testBuilder(t)
	if got := pickFixture(reg, b, "", func(f *cards.Face) bool { return f.Name == "Adaptive Automaton" }, nil, true); got != "" {
		t.Errorf("fixture = %q, want none: a placed as-enters permanent would be refused downstream", got)
	}
	if got := pickFixture(reg, b, "", func(f *cards.Face) bool { return f.Name == "Grizzly Bears" }, nil, true); got != "Grizzly Bears" {
		t.Errorf("fixture = %q, want Grizzly Bears", got)
	}
}

// asEntersCast checks a ChooseColor card's start: it is in seat 0's hand with
// carriers one-mana spells of each colour (the pick is not known when the start
// is written), and the focus casts it.
func asEntersCast(card string, carriers int) func(t *testing.T, f *StartFile) {
	return func(t *testing.T, f *StartFile) {
		t.Helper()
		if h := f.Setup.P0.Hand; len(h) != 1+carriers || h[0] != card {
			t.Errorf("p0 hand = %v, want %s and %d carriers", h, card, carriers)
		}
		for _, n := range f.Setup.P0.Battlefield {
			if n == card {
				t.Errorf("%s is placed on the battlefield: it would enter without its choice", card)
			}
		}
		focusIs(t, f, 0, card, "cast", 0)
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

// TestStartPlayerCounterCost: a PayEnergy<N> cost is paid from the seat's
// player counters (CR 107.14, 122.1), so a start seeds ENERGY on the seat
// itself instead of refusing the ability. Aethersquall Ancient's {8} ability
// was the class's first card, but its mandatory upkeep trigger makes the
// lowering's start gate refuse the placed card (Vitaspore Thallid's shape), so
// Whirler Virtuoso carries the energy start and Aethersquall Ancient is the
// named refusal; the start output stays a sorted slice with no card name, and
// a malformed amount keeps the old refusal.
func TestStartPlayerCounterCost(t *testing.T) {
	reg, b := testBuilder(t)
	const card = "Whirler Virtuoso"
	if gate, err := Generate(reg, b, "Aethersquall Ancient", "f0/a0", Options{Prefer: "start"}); err != nil {
		t.Fatal(err)
	} else if gate.Path != PathRefused || gate.Reason != "start:upkeep-trigger" {
		t.Fatalf("Aethersquall Ancient: path %q reason %q, want refused start:upkeep-trigger", gate.Path, gate.Reason)
	}
	res, err := Generate(reg, b, card, "f0/a0", Options{Prefer: "start"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Path != PathStart || res.Start == nil {
		t.Fatalf("path %q reason %q, want a start file", res.Path, res.Reason)
	}
	f := res.Start
	focusIs(t, f, 0, card, "ability", 0)
	if want := []StartCounter{{Kind: "ENERGY", N: 3}}; !reflect.DeepEqual(f.Setup.P0.PlayerCounters, want) {
		t.Fatalf("p0 player counters %+v, want %+v", f.Setup.P0.PlayerCounters, want)
	}
	if len(f.Setup.P1.PlayerCounters) != 0 {
		t.Errorf("p1 player counters %+v, want none", f.Setup.P1.PlayerCounters)
	}
	raw, err := f.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(`"player_counters"`)) || !bytes.Contains(raw, []byte(`"kind": "ENERGY"`)) {
		t.Errorf("start bytes do not name the player counters:\n%s", raw)
	}
	if bytes.Contains(raw, []byte(`"card": ""`)) {
		t.Errorf("a seat player counter serialised an empty card:\n%s", raw)
	}
	c, ok := reg.Lookup(card)
	if !ok || len(c.Faces) == 0 {
		t.Fatalf("%s is not in the corpus", card)
	}
	for _, cost := range []string{"PayEnergy<0>", "PayEnergy<x>"} {
		if _, why := planCost(reg, b, c.Faces[0], card, cost, "U"); why != "steps-needed:cost:PayEnergy" {
			t.Errorf("cost %q: reason %q, want steps-needed:cost:PayEnergy", cost, why)
		}
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

// TestStartattackstriggers is the dispatch's repro spelling (xpg-attacks-
// filter): an Attacks trigger whose ValidCard names the key card's attached
// creature (Card.AttachedBy, Card.EnchantedBy, Creature.EnchantedBy). The
// unattached Aura is placed attached (StartAttach) to the most inert
// attackable creature its Enchant keyword takes that the filter's stripped
// alternative takes (Card.AttachedBy+powerLE2 on Writ of Passage keeps the
// power gate), and that bearer is the focus attack. An Equipment cannot enter
// attached from a start (gorge's StartAttach is the Aura entry), and
// attaching it before the attack needs a Magnetic Theft cast plus a second
// turn the one focus play cannot take, so its keys stay refused :equipped. A
// non-Aura's bearer filter (Kitsune Ace, no attackable Vehicle) and the
// unplaced attack target keep narrower refusals.
func TestStartattackstriggers(t *testing.T) {
	reg, b := testBuilder(t)
	for _, c := range []struct{ card, key string }{
		{"Seismic Tutelage", "f0/t1"},
		{"Contaminated Bond", "f0/t0"},
		{"Coercive Impetus", "f0/t0"},
		{"Primetime Suspect", "f0/t0"},
		{"Writ of Passage", "f0/t0"},
	} {
		t.Run(c.card, func(t *testing.T) {
			res, err := Generate(reg, b, c.card, c.key, Options{Prefer: "start"})
			if err != nil {
				t.Fatal(err)
			}
			if res.Path != PathStart || res.Start == nil {
				t.Fatalf("path %q reason %q, want a start file", res.Path, res.Reason)
			}
			f := res.Start
			if f.Active != 0 {
				t.Errorf("active %d, want 0", f.Active)
			}
			if len(f.Setup.P0.Battlefield) == 0 || f.Setup.P0.Battlefield[0] != c.card {
				t.Errorf("p0 battlefield %v, want %q first", f.Setup.P0.Battlefield, c.card)
			}
			attached(t, f, c.card)
			bearer := attachBearerOf(t, f, c.card)
			if bearer == c.card || !slices.Contains(f.Setup.P0.Battlefield, bearer) {
				t.Fatalf("bearer %q is not a placed creature of the Aura's own", bearer)
			}
			focusIs(t, f, 0, bearer, "attack", 0)
			raw, err := f.Marshal()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := DecodeStartFile(raw); err != nil {
				t.Errorf("start file does not decode: %v", err)
			}
		})
	}
	for _, r := range []struct{ card, key, reason string }{
		{"Spiked Ripsaw", "f0/t0", "no-generator:trigger:Attacks:equipped"},
		{"Pip-Boy 3000", "f0/t0", "no-generator:trigger:Attacks:equipped"},
		{"Adaptive Omnitool", "f0/t0", "no-generator:trigger:Attacks:equipped"},
		{"Strength-Testing Hammer", "f0/t0", "no-generator:trigger:Attacks:equipped"},
		// unrelated refusals, unchanged: the card is not a creature the
		// filter's bearer shape takes, and the attack target is not placed
		{"Kitsune Ace", "f0/t0", "no-generator:trigger:Attacks:filter"}, // no attackable Vehicle
	} {
		t.Run(r.card, func(t *testing.T) {
			res, err := Generate(reg, b, r.card, r.key, Options{Prefer: "start"})
			if err != nil || res.Path != PathRefused || res.Reason != r.reason {
				t.Errorf("%s: %+v %v, want refused %q", r.card, res, err, r.reason)
			}
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

// TestStartCommanderFilter is the dispatch's repro spelling (xpg-commander-
// filter): ChangesZone and Attacks triggers whose ValidCard reads IsCommander
// (Tome of Legends and Search for Dagger, "whenever your commander enters or
// attacks"; Keleth, Sunmane Familiar, "whenever a commander you control
// attacks"). The engine resolves Card.IsCommander through Game.CommanderOf
// list membership, never a printed keyword (CR 903.3: the designation is an
// attribute of the card itself), so a start must do more than pick a carrier
// the face test takes: it must designate that carrier in the file's
// Commanders list, or the trigger never reads the entering object or the
// attacker as the seat's commander and the focus is never exercised. The
// inert carrier pool can never hold a commander-shaped card, so the enters
// arm casts a fixture (entersFallback) and the Attacks lane falls back to
// pickFixture (attackCarrier), each recording the designation
// (designateCommander); a commander filter on the card itself (Keleth) is
// the card's own attack with the card designated. A filter still naming a
// property the reader does not understand keeps its ":filter" refusal
// (Secret Summoning's NamedCard): the fail-closed rule is unchanged.
func TestStartCommanderFilter(t *testing.T) {
	reg, b := testBuilder(t)
	for _, c := range []struct{ card, key, kind string }{
		{"Tome of Legends", "f0/t0", "cast"},
		{"Tome of Legends", "f0/t1", "attack"},
		{"Search for Dagger", "f0/t0", "cast"},
		{"Keleth, Sunmane Familiar", "f0/t0", "attack"},
	} {
		t.Run(c.card+" "+c.key, func(t *testing.T) {
			res, err := Generate(reg, b, c.card, c.key, Options{Prefer: "start"})
			if err != nil {
				t.Fatal(err)
			}
			if res.Path != PathStart || res.Start == nil {
				t.Fatalf("path %q reason %q, want a start file", res.Path, res.Reason)
			}
			f := res.Start
			if f.Focus == nil || f.Focus.Seat == nil {
				t.Fatalf("focus %+v, want a focus with a seat", f.Focus)
			}
			if len(f.Setup.P0.Battlefield) == 0 || f.Setup.P0.Battlefield[0] != c.card {
				t.Errorf("p0 battlefield %v, want %q first", f.Setup.P0.Battlefield, c.card)
			}
			if *f.Focus.Seat != 0 || f.Focus.Kind != c.kind {
				t.Fatalf("focus %+v, want seat 0 kind %q", f.Focus, c.kind)
			}
			seat := f.Setup.P0
			// The trigger reads the focus object as the commander, so the
			// file designates the focus object itself.
			if len(seat.Commanders) != 1 || seat.Commanders[0] != f.Focus.Card {
				t.Fatalf("p0 commanders %v, want the focus carrier %q designated", seat.Commanders, f.Focus.Card)
			}
			// The designated card is placed like any other (CR 903.3: the
			// designation does not move it).
			if c.kind == "cast" {
				if !slices.Contains(seat.Hand, f.Focus.Card) {
					t.Errorf("hand %v does not hold the designated commander %q", seat.Hand, f.Focus.Card)
				}
			} else if !slices.Contains(seat.Battlefield, f.Focus.Card) {
				t.Errorf("battlefield %v does not place the designated commander %q", seat.Battlefield, f.Focus.Card)
			}
			// CR 903.3: the designated card must be a legendary creature.
			cmdr, ok := reg.Lookup(f.Focus.Card)
			if !ok || len(cmdr.Faces) == 0 || !cmdr.Faces[0].IsLegendary() || !cmdr.Faces[0].IsCreature() {
				t.Errorf("designated commander %q is not a legendary creature", f.Focus.Card)
			}
			raw, err := f.Marshal()
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(raw), `"commanders"`) {
				t.Errorf("marshalled file carries no commanders designation:\n%s", raw)
			}
			g, err := DecodeStartFile(raw)
			if err != nil {
				t.Fatalf("start file does not decode: %v", err)
			}
			if got := g.Setup.P0.Commanders; !slices.Equal(got, seat.Commanders) {
				t.Errorf("round-trip p0 commanders %v, want %v", got, seat.Commanders)
			}
		})
	}
	for _, r := range []struct{ card, key, reason string }{
		// unchanged fail-closed: an unread property is not a carrier test,
		// and the enters lane keeps its named refusal rather than a blind
		// pick the engine would reject
		{"Secret Summoning", "f0/t0", "no-generator:trigger:ChangesZone:filter"},
	} {
		t.Run(r.card, func(t *testing.T) {
			res, err := Generate(reg, b, r.card, r.key, Options{Prefer: "start"})
			if err != nil || res.Path != PathRefused || res.Reason != r.reason {
				t.Errorf("%s: %+v %v, want refused %q", r.card, res, err, r.reason)
			}
		})
	}
	// The IsCommander row is opt-in: only the commander-OK readers know it,
	// so every lane that cannot record a designation (targets, counters,
	// tapped state) still refuses it exactly as an unknown property.
	for _, r := range []struct {
		name  string
		alts  []filterAlt
		known bool
	}{
		{"parseFilter", parseFilter("Card.IsCommander+YouOwn"), false},
		{"parseFilterStatic", parseFilterStatic("Card.IsCommander+YouOwn"), false},
		{"parseFilterPlaced", parseFilterPlaced("Card.IsCommander+YouOwn"), false},
		{"parseFilterCommander", parseFilterCommander("Card.IsCommander+YouOwn"), true},
		{"parseFilterStaticCommander", parseFilterStaticCommander("Card.IsCommander+YouOwn"), true},
	} {
		t.Run(r.name, func(t *testing.T) {
			if len(r.alts) != 1 || r.alts[0].known != r.known {
				t.Errorf("%s(IsCommander) = %+v, want known %v", r.name, r.alts, r.known)
			}
			if r.known && (len(r.alts) != 1 || !r.alts[0].commander) {
				t.Errorf("%s(IsCommander) lost the commander flag that drives the fixture fallback", r.name)
			}
		})
	}
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

// TestScenKeyAuraLandBearer: an Aura whose Enchant keyword takes no carrier
// creature (Genju of the Cedars' Enchant:Forest, Pooling Venom's Enchant:land)
// gets the basic land it can legally enchant as its StartAttach bearer. The
// land is inert for the focus event (the Aura's own activation; it can still
// tap for mana), and the start lists it on the Aura's controller's battlefield;
// every aura-unattached refusal of this shape becomes a start, the same way the
// creature-Aura shape does.
func TestScenKeyAuraLandBearer(t *testing.T) {
	reg, b := testBuilder(t)
	for _, r := range []struct{ card, bearer string }{
		{"Genju of the Cedars", "Forest"},   // Enchant:Forest
		{"Genju of the Spires", "Mountain"}, // Enchant:Mountain
		{"Pooling Venom", "Plains"},         // Enchant:land: the first basic the filter takes
	} {
		t.Run(r.card, func(t *testing.T) {
			res, err := Generate(reg, b, r.card, "f0/a0", Options{Prefer: "start"})
			if err != nil {
				t.Fatal(err)
			}
			if res.Path != PathStart || res.Start == nil {
				t.Fatalf("path %q reason %q, want a start file", res.Path, res.Reason)
			}
			f := res.Start
			if len(f.Setup.P0.Attach) != 1 || f.Setup.P0.Attach[0].Card != r.card || f.Setup.P0.Attach[0].To != r.bearer {
				t.Fatalf("p0 attach %+v, want %s on %s", f.Setup.P0.Attach, r.card, r.bearer)
			}
			if !slices.Contains(f.Setup.P0.Battlefield, r.bearer) {
				t.Errorf("p0 battlefield %v does not hold the bearer %s", f.Setup.P0.Battlefield, r.bearer)
			}
			if f.Focus == nil || f.Focus.Card != r.card || f.Focus.Kind != "ability" {
				t.Errorf("focus %+v, want the Aura's ability", f.Focus)
			}
			if _, err := DecodeStartFile(mustMarshalStart(t, f)); err != nil {
				t.Errorf("start file does not decode: %v", err)
			}
		})
	}
}

// TestScenAuraLandBearerRefused: an Aura whose Enchant keyword names a player
// (no card can be its bearer) or a planeswalker (the carrier pool has no inert
// one) keeps the exact refusal, and a land Aura whose own upkeep cost the start
// cannot pay names that cost rather than aura-unattached.
func TestScenAuraLandBearerRefused(t *testing.T) {
	checkStartRows(t, []startRow{
		{card: "Curse of Stalked Prey", key: "f0/t0", reason: "battlefield:aura-unattached"}, // Enchant:Player
		{card: "Vivien's Talent", key: "f0/t0", reason: "battlefield:aura-unattached"},       // Enchant:Planeswalker
		{card: "Tourach's Gate", key: "f0/a0", reason: "start:upkeep-cost"},                  // Enchant:land, cumulative upkeep
		{card: "Wand of Orcus", key: "f0/t1", reason: "no-generator:trigger:Blocks:filter"},  // Equipment: StartAttach enters Auras only
	})
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

// TestStartSpellCastPermanentFilters: a ValidCard filter that names Equipment
// or an Aura is a spell the inert carrier pool never holds. "Aura,Equipment"
// (Armory Paladin, Mister Gutsy) casts the simplest Equipment the filter takes
// (no target to pick); an Aura-only filter casts Rancor at the card, its one
// bearer. The other ValidCard shapes of the SpellCast:filter class (a kicked
// spell, a stat property, hasXCost) have no carrier and keep their refusal.
func TestStartSpellCastPermanentFilters(t *testing.T) {
	reg, b := testBuilder(t)
	for _, card := range []string{"Armory Paladin", "Mister Gutsy"} {
		res, err := Generate(reg, b, card, "f0/t0", Options{Prefer: "start"})
		if err != nil || res.Start == nil {
			t.Fatalf("%s: path %q reason %q %v, want a start", card, res.Path, res.Reason, err)
		}
		f := res.Start
		if f.Focus == nil || f.Focus.Kind != "cast" || f.Active != 0 || !slices.Contains(f.Setup.P0.Hand, f.Focus.Card) || f.Setup.P0.Battlefield[0] != card {
			t.Errorf("%s: focus %+v hand %v battlefield %v, want the card on the battlefield and an Equipment cast from hand", card, f.Focus, f.Setup.P0.Hand, f.Setup.P0.Battlefield)
			continue
		}
		c, _ := reg.Lookup(f.Focus.Card)
		if !c.Faces[0].HasTypeWord("Equipment") || c.Faces[0].IsCreature() || !plainCost(c.Faces[0]) {
			t.Errorf("%s: focus %s is not a plain-cost non-creature Equipment", card, f.Focus.Card)
		}
	}
	c, _ := reg.Lookup("Armory Paladin")
	if got := permanentSpellCarrier(reg, b, c.Faces[0], "Armory Paladin", "Aura.YouCtrl", 0, func(*cards.Face) bool { return true }); got != "Rancor" {
		t.Errorf("an Aura filter's carrier = %q, want Rancor (its bearer is the creature card)", got)
	}
	if got := permanentSpellCarrier(reg, b, c.Faces[0], "Armory Paladin", "Aura", 1, func(*cards.Face) bool { return true }); got != "" {
		t.Errorf("an opponent-cast Aura filter's carrier = %q, want none", got)
	}
	const refused = "no-generator:trigger:SpellCast:filter"
	checkStartRows(t, []startRow{
		{card: "Saproling Infestation", key: "f0/t0", reason: refused},    // Card.YouCtrl+kicked
		{card: "Pollywog Prodigy", key: "f0/t0", reason: refused},         // Card.nonCreature+cmcLTX, an opponent's cast
		{card: "Lattice Library", key: "f0/t1", reason: refused},          // Card.hasXCost
		{card: "Nev, the Practical Dean", key: "f0/t0", reason: refused},  // Card.hasXCost
		{card: "Owlin Spiralmancer", key: "f0/t0", reason: refused},       // Card.hasXCost
		{card: "Zimone, Infinite Analyst", key: "f0/t0", reason: refused}, // Card.hasXCost
	})
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
	// nonIsCommander joins with the IsCommander row: the pin evaluates the
	// positive predicate (the row is on) but not the non form, which stays
	// unknown by the pin and refused by the reader (its negation needs the
	// engine's non form, and only a commanderOK lane reads even the positive).
	unsupported := []string{"Party", "nonParty", "Multicolor", "nonMulticolor", "nonMultiColor", "nonHistoric", "nonMonoColor", "nonIsCommander"}
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

// TestStartCycledTriggers: "When you cycle CARDNAME" (Cycled, Card.Self) is
// the card in hand with exactly the cycling cost's mana (planCost met it,
// as a channel key's cost is met), the focus the cycling activation itself,
// the granted ability index the K:Cycling line expanded to. A watcher
// (ValidCard$ Card or Card.Other: the card elsewhere while another card
// cycles) is the watcher arm: the card where its TriggerZones put it (the
// battlefield, or the graveyard for Benalish Partisan) while the ranked
// cycling carrier (Sandbar Merfolk, K:Cycling {2}) sits in the cycler's hand
// with its {2} of basics placed, the focus its cycling activation. An Aura
// watcher (Withering Hex) enters attached to a bearer. FirstTime (Valiant
// Rescuer) is met trivially: nothing has cycled yet.
func TestStartCycledTriggers(t *testing.T) {
	checkStartRows(t, []startRow{
		{card: "Stir the Sands", key: "f0/t0", maxTurns: 1, p0Hand: []string{"Stir the Sands"},
			p0BF: rep("Swamp", 4), focusCard: "Stir the Sands", focusKind: "ability", focusAbility: 1},
		{card: "Renewed Faith", key: "f0/t0", maxTurns: 1, p0Hand: []string{"Renewed Faith"},
			p0BF: rep("Plains", 2), focusCard: "Renewed Faith", focusKind: "ability", focusAbility: 1},
		// watchers: the carrier cycles on the watcher's controller's seat
		{card: "Warped Researcher", key: "f0/t0", maxTurns: 1, p0Hand: []string{"Sandbar Merfolk"},
			p0BF:      cat([]string{"Warped Researcher"}, rep("Island", 2)),
			focusCard: "Sandbar Merfolk", focusKind: "ability", focusAbility: 0},
		{card: "Lightning Rift", key: "f0/t0", maxTurns: 1, p0Hand: []string{"Sandbar Merfolk"},
			p0BF:      cat([]string{"Lightning Rift"}, rep("Mountain", 2)),
			focusCard: "Sandbar Merfolk", focusKind: "ability", focusAbility: 0},
		{card: "Astral Slide", key: "f0/t0", maxTurns: 1, p0Hand: []string{"Sandbar Merfolk"},
			p0BF: cat([]string{"Astral Slide"}, rep("Plains", 2)), p1BF: []string{"Memnite"},
			focusCard: "Sandbar Merfolk", focusKind: "ability", focusAbility: 0},
		{card: "Astral Drift", key: "f0/t1", maxTurns: 1, p0Hand: []string{"Sandbar Merfolk"},
			p0BF: cat([]string{"Astral Drift"}, rep("Plains", 2)), p1BF: []string{"Ornithopter"},
			focusCard: "Sandbar Merfolk", focusKind: "ability", focusAbility: 0},
		{card: "Valiant Rescuer", key: "f0/t0", maxTurns: 1, p0Hand: []string{"Sandbar Merfolk"},
			p0BF:      cat([]string{"Valiant Rescuer"}, rep("Plains", 2)),
			focusCard: "Sandbar Merfolk", focusKind: "ability", focusAbility: 0},
		{card: "Withering Hex", key: "f0/t0", maxTurns: 1, p0Hand: []string{"Sandbar Merfolk"},
			p0BF: []string{"Withering Hex", "Memnite", "Swamp", "Swamp"}, p1BF: []string{"Kobolds of Kher Keep"},
			focusCard: "Sandbar Merfolk", focusKind: "ability", focusAbility: 0},
		{card: "Benalish Partisan", key: "f0/t0", maxTurns: 1, p0Hand: []string{"Sandbar Merfolk"},
			p0BF:      rep("Plains", 2),
			focusCard: "Sandbar Merfolk", focusKind: "ability", focusAbility: 0},
	})
	// the graveyard watcher: TriggerZones$ Graveyard puts the card in its
	// controller's graveyard, where the trigger reads it while the carrier
	// cycles from the hand
	reg, b := testBuilder(t)
	res, err := Generate(reg, b, "Benalish Partisan", "f0/t0", Options{Prefer: "start"})
	if err != nil || res.Start == nil {
		t.Fatalf("Benalish Partisan: %+v %v", res, err)
	}
	if !slices.Contains(res.Start.Setup.P0.Graveyard, "Benalish Partisan") {
		t.Errorf("Benalish Partisan graveyard %v, want the card in it",
			res.Start.Setup.P0.Graveyard)
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
// mints its tokens (the corpus's Servo, untapped: a cost may tap any
// permanent; TestScenStepsNeededCostClass checks the token placements).
func TestStartCosts(t *testing.T) {
	checkStartRows(t, []startRow{
		{card: "Agency Coroner", key: "f0/a0", maxTurns: 1, p0BF: cat([]string{"Agency Coroner"}, rep("Swamp", 3), []string{"Memnite"}),
			focusCard: "Agency Coroner", focusKind: "ability"},
		{card: "Possessed Goat", key: "f0/a0", maxTurns: 1, p0Hand: []string{"Plains"}, p0BF: cat([]string{"Possessed Goat"}, rep("Plains", 3)),
			focusCard: "Possessed Goat", focusKind: "ability"},
		{card: "Bedhead Beastie", key: "f0/a0", maxTurns: 1, p0Hand: []string{"Bedhead Beastie"}, p0BF: rep("Mountain", 3),
			focusCard: "Bedhead Beastie", focusKind: "ability"},
		{card: "Baylen, the Haymaker", key: "f0/a0", maxTurns: 1, p0BF: []string{"Baylen, the Haymaker"},
			focusCard: "Baylen, the Haymaker", focusKind: "ability"},
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

// TestStartNinjutsu: ninjutsu returns an unblocked attacker, so the start is
// a combat line: the vanilla fixture attacks, nothing blocks, and the ninja
// waits in hand with exactly its ninjutsu mana until the declare blockers step
// (the focus attack scripts the line; the activation is the focus play). Other
// cost refusals stay as they were: the unblocked-attacker Return is read only
// by a focus ability (planCost, which every other caller uses, still refuses
// it), and a Return the generator cannot place or a different cost keeps its
// name.
func TestStartNinjutsu(t *testing.T) {
	reg, b := testBuilder(t)
	for _, r := range []struct {
		card string
		mana []string
	}{
		{"Biting-Palm Ninja", rep("Swamp", 3)},
		{"Dokuchi Shadow-Walker", rep("Swamp", 4)},
		{"Blade-Blizzard Kitsune", rep("Plains", 4)},
	} {
		res, err := Generate(reg, b, r.card, "f0/a0", Options{Prefer: "start"})
		if err != nil || res.Start == nil {
			t.Fatalf("%s: %+v %v", r.card, res, err)
		}
		f := res.Start
		listIs(t, r.card+" p0 hand", f.Setup.P0.Hand, []string{r.card})
		listIs(t, r.card+" p0 battlefield", f.Setup.P0.Battlefield, cat(r.mana, []string{combatFixture}))
		listIs(t, r.card+" p1 battlefield", f.Setup.P1.Battlefield, nil)
		focusIs(t, f, 0, r.card, "ability", 0)
		if f.Focus.Attack != combatFixture || f.Focus.Block != "" || f.Active != 0 {
			t.Errorf("%s: focus attack %q block %q active %d, want %q, no block, seat 0", r.card, f.Focus.Attack, f.Focus.Block, f.Active, combatFixture)
		}
	}
	card, ok := reg.Lookup("Biting-Palm Ninja")
	if !ok {
		t.Fatal("Biting-Palm Ninja missing")
	}
	face := card.Faces[0]
	for _, c := range []struct{ cost, want string }{
		{"2 B Return<1/Creature.YouCtrl+attacking+unblocked>", "steps-needed:cost:Return"}, // not a focus ability: still refused
		{"2 B Return<1/Creature.YouCtrl+attacking>", "steps-needed:cost:Return"},           // no unblocked: no line to script
		{"2 B Return<1/Treasure.token>", "steps-needed:cost:Return"},
		{"2 B Forage", "steps-needed:cost:Forage"},
	} {
		if _, why := planCost(reg, b, face, "Biting-Palm Ninja", c.cost, "B"); why != c.want {
			t.Errorf("planCost(%q) = %q, want %q", c.cost, why, c.want)
		}
	}
}

// TestStartSacCosts: the Sac cost shapes beyond Sac<1/CARDNAME> a start can
// pay with placed state (operator 11-26, the steps-needed:cost:Sac class):
//
//   - an untapped-creature Sac places an untapped vanilla creature beside the
//     card (Betrothed of Fire's "Sacrifice an untapped creature");
//   - an AttachedBy Sac spends the Aura's own bearer, which applyCost places
//     attached (StartAttach), and no separate permanent for the cost;
//   - a named-card Sac places the named card beside the key card (Kyscu
//     Drake's Spitting Drake, Urborg Panther's Feral Shadow and
//     Breathstealer), and keeps the refusal when the named cards cannot be
//     placed (Shadowborn Apostle's six creatures named it);
//   - a token-kind Sac mints the cost's tokens: five Treasures (Kellogg,
//     Dangerous Mind), a Food (Bosco, Just a Bear), a token of its own kind
//     (Arcade Cabinet's, Rat King, Pale Piper's Permanent.token -> the
//     corpus's Servo), and keeps the refusal for a kind the XMage setup token
//     table does not map (Diamond Kaleidoscope's Prism);
//   - a filtered-land Sac places the nonbasic land the filter takes (Glacial
//     Crevasses' snow Mountain: Alpine Meadow, the corpus's only snow
//     Mountain).
func TestStartSacCosts(t *testing.T) {
	reg, b := testBuilder(t)
	gen := func(card, key string) *StartFile {
		t.Helper()
		res, err := Generate(reg, b, card, key, Options{Prefer: "start"})
		if err != nil {
			t.Fatal(err)
		}
		if res.Start == nil {
			t.Fatalf("%s %s: refused %q", card, key, res.Reason)
		}
		return res.Start
	}
	tokenScripts := func(ts []StartToken) []string {
		out := make([]string, 0, len(ts))
		for _, tk := range ts {
			out = append(out, tk.Script)
		}
		return out
	}
	for _, r := range []struct {
		card, key string
		p0BF      []string
		p1BF      []string
		tokens    []string
		attach    []StartAttach
		ability   int
	}{
		{card: "Kyscu Drake", key: "f0/a1", p0BF: []string{"Kyscu Drake", "Spitting Drake"}, ability: 1},
		{card: "Betrothed of Fire", key: "f0/a0", p0BF: []string{"Betrothed of Fire", "Memnite", "Crimson Kobolds", "Shield Sphere"},
			p1BF: []string{"Crimson Kobolds"}, attach: []StartAttach{{Card: "Betrothed of Fire", To: "Memnite"}}},
		{card: "Betrothed of Fire", key: "f0/a1", p0BF: []string{"Betrothed of Fire", "Memnite", "Shield Sphere"},
			p1BF: []string{"Crimson Kobolds"}, attach: []StartAttach{{Card: "Betrothed of Fire", To: "Memnite"}}, ability: 1},
		{card: "Urborg Panther", key: "f0/a1", p0BF: []string{"Urborg Panther", "Feral Shadow", "Breathstealer"},
			p1BF: []string{"Phyrexian Walker"}, ability: 1},
		{card: "Bubbling Cauldron", key: "f0/a1", p0BF: []string{"Bubbling Cauldron", "Plains", "Festering Newt"}, ability: 1},
		{card: "Glacial Crevasses", key: "f0/a0", p0BF: []string{"Glacial Crevasses", "Alpine Meadow", "Karplusan Wolverine"}},
		{card: "Kellogg, Dangerous Mind", key: "f0/a0", p0BF: []string{"Kellogg, Dangerous Mind"},
			p1BF: []string{"Shield Sphere"}, tokens: rep("c_a_treasure_sac", 5)},
		{card: "Bosco, Just a Bear", key: "f0/a0", p0BF: []string{"Bosco, Just a Bear", "Forest", "Forest", "Forest"},
			tokens: []string{"c_a_food_sac"}},
		{card: "Arcade Cabinet", key: "f0/a0", p0BF: []string{"Arcade Cabinet", "Plains", "Plains"},
			p1BF: []string{"Shield Sphere"}, tokens: []string{"c_1_1_a_servo"}},
		{card: "Rat King, Pale Piper", key: "f0/a0", p0BF: []string{"Rat King, Pale Piper", "Swamp", "Swamp"},
			tokens: rep("c_1_1_a_servo", 2)},
	} {
		t.Run(r.card+"/"+r.key, func(t *testing.T) {
			f := gen(r.card, r.key)
			listIs(t, "p0 battlefield", f.Setup.P0.Battlefield, r.p0BF)
			listIs(t, "p1 battlefield", f.Setup.P1.Battlefield, r.p1BF)
			listIs(t, "p0 tokens", tokenScripts(f.Setup.P0.Tokens), r.tokens)
			if !reflect.DeepEqual(f.Setup.P0.Attach, r.attach) {
				t.Errorf("p0 attach %+v, want %+v", f.Setup.P0.Attach, r.attach)
			}
			focusIs(t, f, 0, r.card, "ability", r.ability)
		})
	}
	checkStartRows(t, []startRow{
		// Diamond Kaleidoscope f0/a1: the Prism token script exists, but the
		// XMage setup token table does not map it, so the cost stays refused.
		{card: "Diamond Kaleidoscope", key: "f0/a1", reason: "steps-needed:cost:Sac"},
		// Shadowborn Apostle f0/a0: six creatures named it is beyond the
		// four-copy budget a start's placements hold.
		{card: "Shadowborn Apostle", key: "f0/a0", reason: "steps-needed:cost:Sac"},
		// Phyrexian Scrapyard f0/a2: three Phyrexian Scrapyards (a named land
		// the card itself is) are within the budget but no placeable card.
		{card: "Phyrexian Scrapyard", key: "f0/a2", reason: "steps-needed:cost:Sac"},
	})
	// the filtered land fodder adds no mana: it is placed tapped
	if f := gen("Glacial Crevasses", "f0/a0"); !slices.Contains(f.Setup.P0.Tapped, "Alpine Meadow") {
		t.Errorf("p0 tapped %v, want the Alpine Meadow the cost sacrifices", f.Setup.P0.Tapped)
	}
}

// TestBaitPoolActivation pins the bait pool a stack-target start draws its
// bait activation from: a cost beyond mana, tap and Sac<1/CARDNAME> (a bait
// has no placed permanent for another sacrifice) is not in it, so a change to
// planCost's Sac shapes leaves every bait start's pick alone.
func TestBaitPoolActivation(t *testing.T) {
	reg, b := testBuilder(t)
	pool := baitPoolOf(reg, b)
	if len(pool.activation) == 0 {
		t.Fatal("empty activation bait pool")
	}
	if got := pool.activation[0].Name; got != "Dark Sphere" {
		t.Errorf("top activation bait %q, want Dark Sphere (T, Sac<1/CARDNAME>)", got)
	}
	for _, f := range pool.activation {
		if sa := baitAbilityOf(f); sa != nil {
			if _, why := baitCostTokens(sa.Params["Cost"]); why != "" {
				t.Errorf("%s: bait cost %q in the pool: %s", f.Name, sa.Params["Cost"], why)
			}
		}
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
		// "targeted" (a permanent target), "targetedPlayer" (the focus seat)
		// or "self".
		baitForm string
		// targetSelf is whether the targeted bait's permanent must sit on the
		// focus seat's battlefield ("targets a permanent you control").
		targetSelf bool
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
		// a TargetValidTargeting$ restriction: the bait spell must itself
		// target a permanent the focus seat controls, or the focus seat
		{card: "Avoid Fate", baitForm: "targeted", targetSelf: true},
		{card: "Hydromorph Gull", baitForm: "targeted", targetSelf: true, key: "f0/a0"},
		{card: "Ring of Immortals", baitForm: "targeted", targetSelf: true, key: "f0/a0"},
		{card: "Cerulean Drake", baitForm: "targetedPlayer", key: "f0/a0"},
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
				mana, why := baitCostTokens(sa.Params["Cost"])
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
			case "targeted", "targetedPlayer":
				if f.Focus.Bait == "" || f.Focus.BaitSelf != "" || f.Focus.BaitAbility != "" {
					t.Fatalf("focus %+v, want a targeted bait cast", f.Focus)
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
				if c.baitForm == "targetedPlayer" {
					if !f.Focus.BaitTargetPlayer || f.Focus.BaitTarget != "" {
						t.Fatalf("focus %+v, want the bait to target the focus seat itself", f.Focus)
					}
					return
				}
				if f.Focus.BaitTarget == "" || f.Focus.BaitTargetPlayer {
					t.Fatalf("focus %+v, want a targeted bait cast with its own permanent target", f.Focus)
				}
				bf := f.Setup.P1.Battlefield
				if c.targetSelf {
					bf = f.Setup.P0.Battlefield
				}
				if !slices.Contains(bf, f.Focus.BaitTarget) {
					t.Errorf("battlefield %v does not list the bait's target %q (targetSelf %v)", bf, f.Focus.BaitTarget, c.targetSelf)
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

// TestStartStackTargetTargetingBaited: a stack target with a
// TargetValidTargeting$ restriction (Keep Safe's "target spell that targets a
// permanent you control", Cerulean Drake's "targets you") is not met by an
// inert bait, because an inert bait has no target of its own. The start now
// scripts a targeted bait whose own target meets the restriction: a permanent
// the restriction names, or the focus seat itself for a "You"/player one.
func TestStartStackTargetTargetingBaited(t *testing.T) {
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
		{"Cerulean Drake", "f0/a0"},    // You: counter target spell that targets you
		{"Hydromorph Gull", "f0/a0"},   // Creature.YouCtrl+inRealZoneBattlefield
		{"Ring of Immortals", "f0/a0"}, // ability: Permanent.YouCtrl+inRealZoneBattlefield
	} {
		t.Run(c.card, func(t *testing.T) {
			res, err := Generate(reg, b, c.card, c.key, Options{Prefer: "start"})
			if err != nil {
				t.Fatal(err)
			}
			if res.Path != PathStart || res.Start == nil || res.Start.Focus == nil {
				t.Fatalf("path %q reason %q, want a start with a targeted bait (start %+v)", res.Path, res.Reason, res.Start)
			}
			fc := res.Start.Focus
			if fc.Bait == "" || fc.BaitSelf != "" || fc.BaitAbility != "" {
				t.Fatalf("focus %+v, want an opponent-cast targeted bait", fc)
			}
			if (fc.BaitTarget != "") == fc.BaitTargetPlayer {
				t.Fatalf("focus %+v, want exactly one of the bait's permanent target or the focus player", fc)
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
	)
	for _, tc := range []struct {
		card, key, kind string
		ability         int
		front           bool
	}{
		{aclazotz, "f1/a0", "ability", 0, false},         // a modal back face (a land)
		{arlinn, "f1/a1", "ability", 1, false},           // a transformed planeswalker's loyalty ability
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
// at all), the card beside them. A trigger on an Equipment's bearer has no
// start file to attach with, and stays refused; an Aura's bearer is
// attacksStart's bearer shape (TestStartattackstriggers).
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
	// Aqueous Form's Attacks trigger reads the Aura's bearer (Card.AttachedBy),
	// which attacksStart now serves with its bearer shape: see
	// TestStartattackstriggers.
	res, err := Generate(reg, b, "Aqueous Form", "f0/t0", Options{Prefer: "start"})
	if err != nil || res.Start == nil || res.Start.Focus == nil || res.Start.Focus.Kind != "attack" {
		t.Errorf("Aqueous Form: %+v %v, want a bearer-attack start", res, err)
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

// TestStartDamageDoneFilter: the DamageDone source filters that name an
// attachment or a state. An Aura bearer (Card.AttachedBy) is placed attached to
// a creature that attacks (hurting a player: Sigil of Sleep, Sleeper's Robe,
// Necromantic Thirst; hurting a creature: Charisma, whose fixture blocks it); a
// "modified" source gets a +1/+1 counter on the creature that attacks (Kodama
// of the West Tree). An Equipment cannot enter attached from a start and a
// damage to You from a source you control needs a self-hit, so those keep the
// exact refusal, as do unrelated cards' refusals.
func TestStartDamageDoneFilter(t *testing.T) {
	reg, b := testBuilder(t)
	for _, r := range []struct {
		card         string
		attacksCard  bool // the card is the attacker (else its attached bearer)
		block        bool
		counterOnAtk bool
	}{
		{card: "Sigil of Sleep"}, {card: "Sleeper's Robe"}, {card: "Necromantic Thirst"},
		{card: "Charisma", block: true},
		{card: "Kodama of the West Tree", attacksCard: true, counterOnAtk: true},
	} {
		t.Run(r.card, func(t *testing.T) {
			res, err := Generate(reg, b, r.card, "f0/t0", Options{Prefer: "start"})
			if err != nil {
				t.Fatal(err)
			}
			if res.Start == nil {
				t.Fatalf("path %q reason %q, want a start file", res.Path, res.Reason)
			}
			f := res.Start
			fo := f.Focus
			if fo == nil || f.Active != 0 {
				t.Fatalf("focus %+v active %d, want seat 0's attack", fo, f.Active)
			}
			attacker := fo.Card
			if fo.Attack != "" {
				attacker = fo.Attack
			}
			if r.attacksCard != (attacker == r.card) {
				t.Errorf("attacker %q, card is attacker %v", attacker, r.attacksCard)
			}
			if r.block != (fo.Block != "") {
				t.Errorf("block %q, want a block %v", fo.Block, r.block)
			}
			if !r.attacksCard {
				attached(t, f, r.card)
				if f.Setup.P0.Attach[0].To != attacker {
					t.Errorf("attached to %q, want the attacker %q", f.Setup.P0.Attach[0].To, attacker)
				}
				c, ok := reg.Lookup(attacker)
				if !ok || !attackingBearer(c.Faces[0]) {
					t.Errorf("bearer %q cannot attack and deal damage", attacker)
				}
			}
			if got := slices.Contains(f.Setup.P0.Counters, StartCounter{Card: attacker, Kind: "P1P1", N: 1}); got != r.counterOnAtk {
				t.Errorf("counters %+v, want a +1/+1 counter on the attacker %v", f.Setup.P0.Counters, r.counterOnAtk)
			}
		})
	}
	for _, r := range []struct{ card, key, reason string }{
		{"Daredevil, Fearless Fighter", "f0/t0", "no-generator:trigger:DamageDone:filter"},      // a source you control hurts You
		{"Domesticated Watercourse", "f0/t0", "no-generator:trigger:DamageDone:filter"},         // EquippedBy: no attached Equipment start
		{"Sword of Fire and Ice", "f0/t0", "no-generator:trigger:DamageDone:filter"},            // EquippedBy
		{"Sword of War and Peace", "f0/t0", "no-generator:trigger:DamageDone:filter"},           // EquippedBy
		{"Surge Mare", "f0/t0", "no-generator:trigger:DamageDone:power"},                        // unrelated: a 0-power card
		{"Labyrinth Raptor", "f0/t0", "no-generator:trigger:AttackerBlocked:filter"},            // unrelated: the only carrier has menace
		{"Kiyomaro, First to Stand", "f0/t0", "no-generator:trigger:DamageDealtOnce:condition"}, // unrelated: hand size
	} {
		t.Run("refused/"+r.card, func(t *testing.T) {
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

// TestScenCombatTriggerLegalBlocker: a block trigger whose attacker's own
// CantBlockBy static keeps every blocker without flying or reach from blocking
// it (Sidar Kondo of Jamuraa: creatures with power 2 or less) still starts in
// declare-blockers. The pair is legal when the blocker is one the static lets
// block (a flier), which combatPairBlocked reads from the mode's ValidAttacker
// and ValidBlocker together instead of counting a match on either alone.
func TestScenCombatTriggerLegalBlocker(t *testing.T) {
	reg, b := testBuilder(t)
	res, err := Generate(reg, b, "Sidar Kondo of Jamuraa", "f0/t0", Options{Prefer: "start"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Path != PathStart || res.Start == nil {
		t.Fatalf("path %q reason %q, want a start file", res.Path, res.Reason)
	}
	f := res.Start
	if f.Focus == nil || f.Focus.Attack != "Sidar Kondo of Jamuraa" || f.Focus.Block == "" {
		t.Fatalf("focus %+v, want Sidar attacking and a blocker", f.Focus)
	}
	c, ok := reg.Lookup(f.Focus.Block)
	if !ok || len(c.Faces) == 0 {
		t.Fatalf("blocker %q not in the corpus", f.Focus.Block)
	}
	if blk := c.Faces[0]; !blk.HasKeyword("Flying") && !blk.HasKeyword("Reach") {
		t.Errorf("blocker %s has neither flying nor reach; the attacker's CantBlockBy keeps it from blocking", blk.Name)
	}
	if _, err := DecodeStartFile(mustMarshalStart(t, f)); err != nil {
		t.Errorf("start file does not decode: %v", err)
	}
}

// TestScenCombatTriggerAttackerRestriction: Evil Eye of Urborg's own CantAttack
// record ("non-Eye creatures you control can't attack") is a restriction on
// its own attack, not on the creature blocking it: the Eye may attack a
// non-Eye blocker, so its AttackerBlockedByCreature trigger still starts.
// Reading the record against the blocker was the false positive that refused
// it while a blocker-owned record went unread.
func TestScenCombatTriggerAttackerRestriction(t *testing.T) {
	reg, b := testBuilder(t)
	res, err := Generate(reg, b, "Evil Eye of Urborg", "f0/t0", Options{Prefer: "start"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Path != PathStart || res.Start == nil {
		t.Fatalf("path %q reason %q, want a start file", res.Path, res.Reason)
	}
	f := res.Start
	if f.Focus == nil || f.Focus.Attack != "Evil Eye of Urborg" || f.Focus.Block == "" {
		t.Fatalf("focus %+v, want Evil Eye attacking and a blocker", f.Focus)
	}
	c, ok := reg.Lookup(f.Focus.Block)
	if !ok || len(c.Faces) == 0 {
		t.Fatalf("blocker %q not in the corpus", f.Focus.Block)
	}
	if blk := c.Faces[0]; blk.HasTypeWord("Eye") {
		t.Errorf("blocker %s is an Eye; the test does not pin the non-Eye restriction", blk.Name)
	}
	if _, err := DecodeStartFile(mustMarshalStart(t, f)); err != nil {
		t.Errorf("start file does not decode: %v", err)
	}
}

// TestScenCombatPairBlockedByRole: a pair restriction is read by creature role,
// whichever permanent carries it. Stronghold Zeppelin's CantBlockBy ("can block
// only creatures with flying") names the attacker in ValidAttacker and itself
// in ValidBlocker, so a non-flier attacker is no legal pair while a flier is.
// A blocker's CantAttack is read against the attacker, and its Self
// alternative (the blocker) is not.
func TestScenCombatPairBlockedByRole(t *testing.T) {
	reg, _ := testBuilder(t)
	face := func(name string) *cards.Face {
		c, ok := reg.Lookup(name)
		if !ok || len(c.Faces) == 0 {
			t.Fatalf("%s is not in the corpus", name)
		}
		return c.Faces[0]
	}
	stat := func(mode string, params ...string) cards.Static {
		st := cards.Static{Mode: mode, Params: map[string]string{}}
		for i := 0; i+1 < len(params); i += 2 {
			st.Params[params[i]] = params[i+1]
		}
		return st
	}
	bears := face("Grizzly Bears")
	drake := face("Wind Drake")
	zeppelin := face("Stronghold Zeppelin") // CantBlockBy ValidAttacker$ Creature.withoutFlying, ValidBlocker$ Creature.Self
	if st := combatPairBlocked(bears, zeppelin); st == nil || st.Mode != "CantBlockBy" {
		t.Errorf("non-flier attacker vs Stronghold Zeppelin: static %+v, want its CantBlockBy", st)
	}
	if st := combatPairBlocked(drake, zeppelin); st != nil {
		t.Errorf("flier attacker vs Stronghold Zeppelin: static %+v, want a legal pair", st)
	}
	// Evil Eye of Urborg's CantAttack names non-Eye creatures its controller
	// controls: it does not make the Eye's own attack illegal against a non-Eye
	// blocker.
	if st := combatPairBlocked(face("Evil Eye of Urborg"), bears); st != nil {
		t.Errorf("Evil Eye attacking a non-Eye: static %+v, want a legal pair", st)
	}
	// The same record on the blocker is read against the attacker: gorge
	// matches StatCantAttack's filter against the attacking creature.
	restrictor := &cards.Face{Name: "Eye Wall", Types: []string{"Creature", "Eye"},
		Statics: []cards.Static{stat("CantAttack", "ValidCard", "Creature.nonEye+YouCtrl")}}
	if st := combatPairBlocked(bears, restrictor); st == nil || st.Mode != "CantAttack" {
		t.Errorf("non-Eye attacker vs a blocker-owned CantAttack: static %+v, want it read against the attacker", st)
	}
	// A blocker's CantAttack naming Self restricts the blocker, not the
	// attacker, so the pair stays legal.
	selfRestrictor := &cards.Face{Name: "Wall of Stone", Types: []string{"Creature"},
		Statics: []cards.Static{stat("CantAttack", "ValidCard", "Card.Self")}}
	if st := combatPairBlocked(bears, selfRestrictor); st != nil {
		t.Errorf("self-only CantAttack on the blocker: static %+v, want a legal pair", st)
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

// TestStarttappedtarget: a target whose filter names a battlefield state, not
// a face property ("target tapped creature", Aerial Assault's
// ValidTgts$ Creature.tapped), was read as unknown: the start refused as
// start:needs-target. Placing the carrier without recording its state would
// write a file whose focus play the game never offers (start-not-exercised),
// so the placed target must also be listed in the seat's Tapped. The untapped
// form of the same filter (Asphyxiate) must place an untapped target.
func TestStarttappedtarget(t *testing.T) {
	reg, b := testBuilder(t)
	for _, tc := range []struct {
		card   string
		tapped bool
	}{
		{"Aerial Assault", true}, // ValidTgts$ Creature.tapped
		{"Vengeance", true},      // ValidTgts$ Creature.tapped
		{"Asphyxiate", false},    // ValidTgts$ Creature.untapped
	} {
		t.Run(tc.card, func(t *testing.T) {
			res, err := Generate(reg, b, tc.card, "cast", Options{Prefer: "start"})
			if err != nil {
				t.Fatal(err)
			}
			if res.Start == nil {
				t.Fatalf("path %q reason %q, want a start file", res.Path, res.Reason)
			}
			f := res.Start
			var creatures []string
			for _, name := range f.Setup.P1.Battlefield {
				c, ok := reg.Lookup(name)
				if ok && len(c.Faces) > 0 && c.Faces[0].IsCreature() {
					creatures = append(creatures, name)
				}
			}
			if len(creatures) == 0 {
				t.Fatalf("p1 battlefield %v holds no target creature", f.Setup.P1.Battlefield)
			}
			if tc.tapped {
				if len(f.Setup.P1.Tapped) == 0 {
					t.Errorf("p1 tapped %v is empty, want a tapped target (%v)", f.Setup.P1.Tapped, creatures)
				}
				for _, name := range f.Setup.P1.Tapped {
					if !slices.Contains(creatures, name) {
						t.Errorf("p1 tapped %v names %q, not a target creature (%v)", f.Setup.P1.Tapped, name, creatures)
					}
				}
			} else {
				for _, name := range creatures {
					if slices.Contains(f.Setup.P1.Tapped, name) {
						t.Errorf("%s is tapped, want an untapped target", name)
					}
				}
			}
		})
	}
}

// TestScenTargetShapes: the focus-target shapes that used to refuse as
// start:needs-target because the placement search only knew a card of a
// permanent type:
//
//   - a token target (Replicator's "target token you control", Dogged Hunter's
//     "target creature token") is now a minted start token, because no card
//     can stand in for a token;
//   - a creature subtype no card in the corpus holds (Blinkmoth Nexus's
//     "target Blinkmoth creature") is met by a changeling, which is every
//     creature type (CR 702.73a);
//   - a six-target spell (Hex) is placed target by target past the old
//     four-placement cap;
//   - a monocolored permanent (Vanishing Verse) reads the MonoColor filter
//     property;
//   - a TargetValidTargeting$ stack target (Avoid Fate, Cerulean Drake) gets a
//     bait whose own target meets the restriction.
//
// Cradle to Grave is the residual: its target must have entered this turn, and
// a setup permanent or token is placed before turn 1 and does not count, so it
// keeps a named refusal instead of a start the replay would refuse. Commander's
// Plate's equip and Seasinger's activation name a Forge predicate under the
// Creature head (IsCommander, ControllerControlsIsland), not a creature
// subtype: the changeling shortcut must not match them, so both keep the
// develop baseline's named start:needs-target instead of a start the replay
// refuses as start-not-exercised.
func TestScenTargetShapes(t *testing.T) {
	reg, b := testBuilder(t)
	for _, tc := range []struct {
		card, key, reason string
		check             func(t *testing.T, f *StartFile)
	}{
		{card: "Replicator", key: "f0/a0", check: func(t *testing.T, f *StartFile) {
			if len(f.Setup.P0.Tokens) == 0 {
				t.Fatalf("p0 tokens empty, want a minted token target: %+v", f.Setup.P0)
			}
		}},
		{card: "Dogged Hunter", key: "f0/a0", check: func(t *testing.T, f *StartFile) {
			if len(f.Setup.P0.Tokens)+len(f.Setup.P1.Tokens) == 0 {
				t.Fatalf("no minted token on either battlefield, want a creature-token target: %+v / %+v", f.Setup.P0, f.Setup.P1)
			}
		}},
		{card: "Blinkmoth Nexus", key: "f0/a2", check: func(t *testing.T, f *StartFile) {
			for _, bf := range [][]string{f.Setup.P0.Battlefield, f.Setup.P1.Battlefield} {
				for _, p := range permanents(t, reg, bf) {
					if p.HasKeyword("Changeling") || p.AllCreatureTypesCDA() {
						return
					}
				}
			}
			t.Fatalf("no changeling placed for the Blinkmoth-creature target: %+v / %+v", f.Setup.P0.Battlefield, f.Setup.P1.Battlefield)
		}},
		{card: "Hex", key: "cast", check: func(t *testing.T, f *StartFile) {
			creatures := 0
			for _, bf := range [][]string{f.Setup.P0.Battlefield, f.Setup.P1.Battlefield} {
				for _, p := range permanents(t, reg, bf) {
					if p.IsCreature() {
						creatures++
					}
				}
			}
			if creatures < 6 {
				t.Fatalf("%d creature(s) placed, want six for Hex: %+v / %+v", creatures, f.Setup.P0.Battlefield, f.Setup.P1.Battlefield)
			}
		}},
		{card: "Vanishing Verse", key: "cast", check: func(t *testing.T, f *StartFile) {
			for _, bf := range [][]string{f.Setup.P0.Battlefield, f.Setup.P1.Battlefield} {
				for _, p := range permanents(t, reg, bf) {
					if len(faceColours(p)) == 1 {
						return
					}
				}
			}
			t.Fatalf("no monocolored permanent placed for Vanishing Verse: %+v / %+v", f.Setup.P0.Battlefield, f.Setup.P1.Battlefield)
		}},
		{card: "Avoid Fate", key: "cast", check: func(t *testing.T, f *StartFile) {
			if f.Focus == nil || f.Focus.BaitTarget == "" || f.Focus.BaitTargetPlayer {
				t.Fatalf("focus %+v, want a targeted bait with a permanent target", f.Focus)
			}
			if !slices.Contains(f.Setup.P0.Battlefield, f.Focus.BaitTarget) {
				t.Errorf("p0 battlefield %v does not list the bait target %q: Avoid Fate needs a permanent you control", f.Setup.P0.Battlefield, f.Focus.BaitTarget)
			}
		}},
		{card: "Cerulean Drake", key: "f0/a0", check: func(t *testing.T, f *StartFile) {
			if f.Focus == nil || !f.Focus.BaitTargetPlayer || f.Focus.BaitTarget != "" {
				t.Fatalf("focus %+v, want a bait that targets the focus seat", f.Focus)
			}
		}},
		{card: "Cradle to Grave", key: "cast", reason: "start:target-condition:this-turn-entered"},
		{card: "Commander's Plate", key: "f0/a0", reason: "start:needs-target"},
		{card: "Seasinger", key: "f0/a0", reason: "start:needs-target"},
	} {
		t.Run(tc.card, func(t *testing.T) {
			res, err := Generate(reg, b, tc.card, tc.key, Options{Prefer: "start"})
			if err != nil {
				t.Fatal(err)
			}
			if tc.reason != "" {
				if res.Path != PathRefused || res.Reason != tc.reason {
					t.Fatalf("path %q reason %q, want refused %q", res.Path, res.Reason, tc.reason)
				}
				return
			}
			if res.Path != PathStart || res.Start == nil {
				t.Fatalf("path %q reason %q, want a start file", res.Path, res.Reason)
			}
			tc.check(t, res.Start)
		})
	}
}

// TestScenTypeWordOnlyMatchesRealCreatureSubtypes: the changeling shortcut is
// CR 702.73a ("every creature type"), so it applies to a real creature subtype
// (Blinkmoth) and not to a Forge filter predicate spelled under the Creature
// head (IsCommander, ControllerControlsIsland). Treating the latter as a
// subtype is how the generator once wrote a start with a changeling standing in
// for "target creature you control that is your commander" or "whose controller
// controls an Island", which the replay then refused as start-not-exercised.
func TestScenTypeWordOnlyMatchesRealCreatureSubtypes(t *testing.T) {
	reg, _ := testBuilder(t)
	automaton := permanents(t, reg, []string{"Universal Automaton"})[0]
	if !typeWordTest("Creature", "Blinkmoth")(automaton) {
		t.Fatal("a changeling does not meet the real creature subtype Blinkmoth")
	}
	for _, pred := range []string{"IsCommander", "ControllerControlsIsland"} {
		if typeWordTest("Creature", pred)(automaton) {
			t.Errorf("a changeling meets the filter predicate %q as if it were a creature subtype", pred)
		}
	}
}

// TestScenTargetValidOpponentKeepsRefusal: a TargetValidTargeting$ "Opponent"
// names the bait spell's target as the focus seat's opponent, but the start
// document carries only the boolean BaitTargetPlayer and its bait always
// targets the focus seat. Rather than write a start that targets the wrong
// player, the alternative stays unknown and the play keeps its named refusal.
func TestScenTargetValidOpponentKeepsRefusal(t *testing.T) {
	alts := parseTargetValid("Opponent")
	if len(alts) != 1 || alts[0].known {
		t.Fatalf("parseTargetValid(Opponent) = %+v, want one unknown alternative", alts)
	}
	if _, ok := baitPlayerSeat([]stackTgt{{needsTarget: true, valid: alts}}, 0); ok {
		t.Fatal("an Opponent restriction is served by a bait that targets the focus seat")
	}
}

// TestGenerateRefusesVariantOnlyCard covers the Forge variant-only placeholder:
// an Un-card whose main face Oracle is `<Unsupported Variant>` (Garbage
// Elemental: the real card is one of the Variant: faces gorge never parses)
// must be refused, never generated as the empty main face. The variant and
// control scripts are inline strings, not testdata files: a committed file
// whose first line is `Name:` and which carries an `Oracle:` line is a Forge
// card script as the boundary guards define one.
func TestGenerateRefusesVariantOnlyCard(t *testing.T) {
	const variantScript = "Name:Garbage Elemental\nManaCost:4 R\nTypes:Creature Elemental\nOracle:<Unsupported Variant>\n" +
		"\nVariant:C:PT:3/2\nVariant:C:K:Battle cry\n" +
		"Variant:C:T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigRoll\n" +
		"Variant:C:SVar:TrigRoll:DB$ RollDice | ResultSVar$ Result | Sides$ 6 | Amount$ 2\n" +
		"Variant:C:Oracle:When Garbage Elemental enters, roll two six-sided dice. Create a number of 1/1 red Goblin creature tokens equal to the difference between those results.\n"
	controlScript := strings.Replace(variantScript, "Oracle:<Unsupported Variant>", "Oracle:When Garbage Elemental enters, roll two six-sided dice.", 1)

	reg := cards.NewRegistry()
	controlReg := cards.NewRegistry()
	addScript := func(r *cards.Registry, name, src string) {
		t.Helper()
		c, diags := cards.ParseBytes(name+".txt", []byte(src))
		if len(diags) != 0 {
			t.Fatalf("%s: diags %v", name, diags)
		}
		r.Add(c)
	}
	addScript(reg, "Garbage Elemental", variantScript)
	addScript(reg, "Vanilla Ogre", "Name:Vanilla Ogre\nManaCost:2\nTypes:Creature Ogre\nPT:2/2\nOracle:x\n")
	addScript(controlReg, "Garbage Elemental", controlScript)
	// A builder over the small registry: the refusal fires before any
	// certdeck use, but the control and vanilla generations run the recipe,
	// which reads b.Types().
	vb := certdeck.NewBuilder(reg, nil)
	cb := certdeck.NewBuilder(controlReg, nil)

	for _, prefer := range []string{"auto", "start"} {
		res, err := Generate(reg, nil, "Garbage Elemental", "cast", Options{Prefer: prefer})
		if err != nil {
			t.Fatalf("prefer %s: %v", prefer, err)
		}
		if res.Path != PathRefused || res.Reason != "variant_only" {
			t.Errorf("prefer %s: path %q reason %q, want refused/variant_only", prefer, res.Path, res.Reason)
		}
		if res.Start != nil || res.Steps != nil {
			t.Errorf("prefer %s: refused result carries a payload", prefer)
		}
	}

	// A real Oracle on the same face generates (the control proves the check is
	// the placeholder, not the Variant: lines or the card shape).
	res, err := Generate(controlReg, cb, "Garbage Elemental", "cast", Options{Prefer: "start"})
	if err != nil {
		t.Fatalf("control: %v", err)
	}
	if res.Path == PathRefused && strings.HasPrefix(res.Reason, "variant_only") {
		t.Errorf("control refused as %q: the placeholder check over-fires", res.Reason)
	}

	// ... and a card with no placeholder at all is untouched.
	res, err = Generate(reg, vb, "Vanilla Ogre", "cast", Options{Prefer: "start"})
	if err != nil {
		t.Fatalf("vanilla: %v", err)
	}
	if res.Path != PathStart || res.Reason != "" {
		t.Errorf("vanilla: path %q reason %q, want start", res.Path, res.Reason)
	}
}

// countAmong counts name in xs.
func countAmong(xs []string, name string) int {
	n := 0
	for _, x := range xs {
		if x == name {
			n++
		}
	}
	return n
}

// TestScenStepsNeededCostClass is the xproof repro for the steps-needed:cost
// class (xproof refused.tsv, the top cards of the class): a cost whose payment
// needs a prior step is met by placed state, so each builds a start where the
// focus play is the key's line:
//
//   - Baylen, the Haymaker taps any number of untapped tokens
//     (tapXType<N/Permanent.token/token>): the start mints the tokens, and a
//     cost may tap them (summoning sickness never guards a cost's tap);
//   - Fear of Abduction's cast exiles a creature you control
//     (Exile<1/Creature> in the spell's additional cost): an inert creature
//     sits beside the lands, like the fodder a Sac spends; Soul Exchange and
//     Necrotic Fumes are the other two cast Exile shapes;
//   - Ion Storm, Rift Elemental and Quillspike remove a counter from a
//     permanent they do not own (SubCounter<N/Kind/Spec>): an inert creature
//     of the spec carries the counters. The bearer is a name the battlefield
//     holds once and survives the counter kind (a Wall of Stone, not a basic
//     land the mana duplicates nor a 1/1 a -1/-1 counter kills), because the
//     lowering binds a counters entry by card name (counterBindingRefusal);
//   - Migloz, Maze Crusher's minus abilities spend a counter of a personal
//     kind (SubCounter<N/OIL/NICKNAME>): the counters sit on Migloz itself;
//     Charforger, Glistener Seer and Tamiyo's Immobilizer are the CARDNAME
//     spelling of the same arm.
//
// A card the class cannot yet meet keeps its specific refusal: Bloodvial
// Purveyor's Blood token need names a token the setup table does not map
// (steps-needed:token), Sage of Fables's own entry replacement puts counters
// on other permanents (steps-needed:cost:SubCounter), Moxite Refinery removes
// a wildcard counter kind (steps-needed:cost:SubCounter), Food Chain and
// The Soul Stone exile from activated abilities whose starts do not lower
// (steps-needed:cost:Exile), and Rasputin Dreamweaver's personal counters
// need a battlefield placement the start gate refuses (steps-needed:cost:
// SubCounter). The gate's own read then sweeps the finished start itself
// (startGateRefusal): Rasputin's cast and upkeep-trigger keys name the
// startedTheTurnUntapped word the gate stamps on every untapped permanent,
// from hand included (start:started-turn-untapped), and Vitaspore Thallid's
// spore ability places a permanent whose upkeep trigger is mandatory
// (start:upkeep-trigger), starts play lowers to zero items on both seeds.
func TestScenStepsNeededCostClass(t *testing.T) {
	reg, b := testBuilder(t)
	// counterOn is the one counter of kind the start places, proved bindable:
	// the name appears on the battlefield once (the lowering's counters field
	// is by name) and a creature bearer survives the counters placed.
	counterOn := func(t *testing.T, f *StartFile, kind string, n int32) StartCounter {
		t.Helper()
		var got []StartCounter
		for _, c := range f.Setup.P0.Counters {
			if c.Kind == kind {
				got = append(got, c)
			}
		}
		if len(got) != 1 || got[0].N != n {
			t.Fatalf("p0 counters %+v, want one %s=%d", f.Setup.P0.Counters, kind, n)
		}
		c := got[0]
		if c.Card == f.Focus.Card {
			t.Errorf("counter bearer %q is the focus card, want a placed fodder permanent", c.Card)
		}
		if k := countAmong(f.Setup.P0.Battlefield, c.Card); k != 1 {
			t.Errorf("counter bearer %q appears %d times on p0 battlefield %v, want once", c.Card, k, f.Setup.P0.Battlefield)
		}
		card, ok := reg.Lookup(c.Card)
		if !ok || len(card.Faces) == 0 {
			t.Fatalf("counter bearer %q not in the corpus", c.Card)
		}
		if face := card.Faces[0]; face.IsCreature() && kind == "M1M1" && face.Toughness() <= int(n) {
			t.Errorf("counter bearer %q (%s) dies to %d M1M1 counters", c.Card, face.PT, n)
		}
		return c
	}
	// personalCounter is the one counter of kind on the focus card itself: a
	// personal SubCounter kind (NICKNAME/CARDNAME) spends the card's own
	// counters, so the card is its bearer.
	personalCounter := func(t *testing.T, f *StartFile, card, kind string, n int32) {
		t.Helper()
		want := []StartCounter{{Card: card, Kind: kind, N: n}}
		if !reflect.DeepEqual(f.Setup.P0.Counters, want) {
			t.Errorf("p0 counters %+v, want %+v", f.Setup.P0.Counters, want)
		}
	}
	extraCreature := func(t *testing.T, f *StartFile, card string) {
		t.Helper()
		focusIs(t, f, 0, card, "cast", 0)
		found := false
		for _, n := range f.Setup.P0.Battlefield {
			if c, ok := reg.Lookup(n); ok && c.Faces[0].IsCreature() && n != card {
				found = true
			}
		}
		if !found {
			t.Errorf("p0 battlefield %v holds no creature the Exile additional cost spends", f.Setup.P0.Battlefield)
		}
	}
	for _, r := range []struct {
		card, key, want string
		check           func(t *testing.T, f *StartFile)
	}{
		{"Baylen, the Haymaker", "f0/a0", "", func(t *testing.T, f *StartFile) {
			focusIs(t, f, 0, "Baylen, the Haymaker", "ability", 0)
			listIs(t, "p0 battlefield", f.Setup.P0.Battlefield, []string{"Baylen, the Haymaker"})
			if len(f.Setup.P0.Tokens) != 2 {
				t.Errorf("p0 tokens %v, want two minted tokens to tap", f.Setup.P0.Tokens)
			}
			for _, tok := range f.Setup.P0.Tokens {
				if tok.Tapped {
					t.Errorf("token %s placed tapped, want untapped to tap as a cost", tok.Script)
				}
			}
		}},
		{"Baylen, the Haymaker", "f0/a2", "", func(t *testing.T, f *StartFile) {
			focusIs(t, f, 0, "Baylen, the Haymaker", "ability", 2)
			if len(f.Setup.P0.Tokens) != 4 {
				t.Errorf("p0 tokens %v, want four minted tokens to tap", f.Setup.P0.Tokens)
			}
		}},
		{"Fear of Abduction", "cast", "", func(t *testing.T, f *StartFile) {
			extraCreature(t, f, "Fear of Abduction")
			listIs(t, "p0 hand", f.Setup.P0.Hand, []string{"Fear of Abduction"})
		}},
		{"Soul Exchange", "cast", "", func(t *testing.T, f *StartFile) {
			extraCreature(t, f, "Soul Exchange")
		}},
		{"Necrotic Fumes", "cast", "", func(t *testing.T, f *StartFile) {
			extraCreature(t, f, "Necrotic Fumes")
		}},
		{"Ion Storm", "f0/a0", "", func(t *testing.T, f *StartFile) {
			focusIs(t, f, 0, "Ion Storm", "ability", 0)
			counterOn(t, f, "P1P1", 1)
		}},
		{"Ion Storm", "f0/a1", "", func(t *testing.T, f *StartFile) {
			focusIs(t, f, 0, "Ion Storm", "ability", 1)
			counterOn(t, f, "CHARGE", 1)
		}},
		{"Rift Elemental", "f0/a0", "", func(t *testing.T, f *StartFile) {
			focusIs(t, f, 0, "Rift Elemental", "ability", 0)
			counterOn(t, f, "TIME", 1)
		}},
		{"Quillspike", "f0/a0", "", func(t *testing.T, f *StartFile) {
			focusIs(t, f, 0, "Quillspike", "ability", 0)
			counterOn(t, f, "M1M1", 1)
		}},
		{"Migloz, Maze Crusher", "f0/a2", "", func(t *testing.T, f *StartFile) {
			focusIs(t, f, 0, "Migloz, Maze Crusher", "ability", 2)
			want := []StartCounter{{Card: "Migloz, Maze Crusher", Kind: "OIL", N: 3}}
			if !reflect.DeepEqual(f.Setup.P0.Counters, want) {
				t.Errorf("p0 counters %+v, want %+v", f.Setup.P0.Counters, want)
			}
		}},
		{"Charforger", "f0/a0", "", func(t *testing.T, f *StartFile) {
			focusIs(t, f, 0, "Charforger", "ability", 0)
			personalCounter(t, f, "Charforger", "OIL", 3)
		}},
		{"Glistener Seer", "f0/a0", "", func(t *testing.T, f *StartFile) {
			focusIs(t, f, 0, "Glistener Seer", "ability", 0)
			personalCounter(t, f, "Glistener Seer", "OIL", 1)
		}},
		{"Tamiyo's Immobilizer", "f0/a0", "", func(t *testing.T, f *StartFile) {
			focusIs(t, f, 0, "Tamiyo's Immobilizer", "ability", 0)
			personalCounter(t, f, "Tamiyo's Immobilizer", "OIL", 1)
		}},
		// Still-unsupported shapes keep a named refusal rather than an
		// unlowerable start.
		{"Bloodvial Purveyor", "f0/t0", "steps-needed:token", nil},
		{"Sage of Fables", "f0/a0", "steps-needed:cost:SubCounter", nil},
		{"Moxite Refinery", "f0/a0", "steps-needed:cost:SubCounter", nil},
		{"Food Chain", "f0/a0", "steps-needed:cost:Exile", nil},
		{"The Soul Stone", "f0/a1", "steps-needed:cost:Exile", nil},
		// A personal kind's counters ride the card, so the card must be
		// placeable at a start. Rasputin's mandatory upkeep and its
		// startedTheTurnUntapped filter word both refuse that placement: keep
		// the cost refusal rather than a start play writes no item for.
		{"Rasputin Dreamweaver", "f0/a0", "steps-needed:cost:SubCounter", nil},
		{"Rasputin Dreamweaver", "f0/a1", "steps-needed:cost:SubCounter", nil},
		// The same gate read, whole-start: every lane that emits a start is
		// swept through it (startGateRefusal), so a card naming the
		// startedTheTurnUntapped word cannot reach PathStart through a lane
		// with no per-lane guard - cast from hand included, where the gate's
		// gameMentions still stamps the field on every untapped land - and a
		// permanent with a mandatory upkeep trigger is named, not emitted.
		// Play lowers each of these starts to zero items on both seeds.
		{"Rasputin Dreamweaver", "cast", "start:started-turn-untapped", nil},
		{"Rasputin Dreamweaver", "f0/t0", "start:started-turn-untapped", nil},
		{"Vitaspore Thallid", "f0/a1", "start:upkeep-trigger", nil},
		// A Destroy that only reads targets is no self-removal: The Abyss
		// ("destroy all nonartifact creatures you don't control") and
		// Crumbling Ashes ("destroy target creature with a +1/+1 counter on
		// it") are DB$ Destroy with ValidTgts, so the battlefield lane stays
		// their refusal (the sweep itself, over finished StartFiles, is the
		// white-box TestScenStartGateRefusalSweepsFinishedStart); what builds is
		// their cast lane, the card in hand and the play — not the setup
		// drive — firing the trigger after the cut.
		{"The Abyss", "f0/t0", "", func(t *testing.T, f *StartFile) {
			focusIs(t, f, 0, "The Abyss", "cast", 0)
			listIs(t, "p0 hand", f.Setup.P0.Hand, []string{"The Abyss"})
		}},
		{"Crumbling Ashes", "f0/t0", "", func(t *testing.T, f *StartFile) {
			focusIs(t, f, 0, "Crumbling Ashes", "cast", 0)
			listIs(t, "p0 hand", f.Setup.P0.Hand, []string{"Crumbling Ashes"})
		}},
	} {
		res, err := Generate(reg, b, r.card, r.key, Options{Prefer: "start"})
		if err != nil {
			t.Fatalf("%s %s: %v", r.card, r.key, err)
		}
		if r.want != "" {
			if res.Path != PathRefused || res.Reason != r.want {
				t.Errorf("%s %s: path %q reason %q, want refused %q", r.card, r.key, res.Path, res.Reason, r.want)
			}
			continue
		}
		if res.Path != PathStart || res.Start == nil {
			t.Fatalf("%s %s: path %q reason %q, want a start file", r.card, r.key, res.Path, res.Reason)
		}
		r.check(t, res.Start)
	}
}

// TestScenStartGateRefusalSweepsFinishedStart drives the whole-start sweep over
// finished StartFiles, the reviewer's witnesses of the shape develop emitted
// and play lowered to zero items on both seeds:
//   - a mandatory upkeep that only reads targets is not self-removal: The
//     Abyss (ValidTgts Creature.nonArtifact+ControlledBy TriggeredPlayer)
//     and Crumbling Ashes (ValidTgts Creature.counters_GE1_M1M1) are DB$
//     Destroy with targets and stay the gate's refusal, not excused by the
//     echo shape (lower.TriggerRemovesSelf's tightening);
//   - the echo exception is start-aware (upkeepRemovalExcused): Earthlink's
//     {2} over two spare Plains and Thelon's Chant's {G} over a Forest are
//     payable, the drive pays, the card stands at the cut where captureStart
//     refuses — so the sweep refuses by name instead of emitting; the same
//     cards with no payable mana are sacrificed before the cut and stay
//     starts (develop's generated Earthlink and Thelon's Chant starts);
//   - the exception only prices pure mana from basic lands: Magmasaur's
//     UnlessCost SubCounter and Koskun Falls's tapXType are asks the drive
//     can pay, so they are not an empty cost; and a mana source the estimate
//     cannot see (Sol Ring's artifact, Ancient Tomb's two colourless from a
//     non-basic land, an untapped Treasure token, Wild Growth's
//     TapsForMana trigger on a Plains, a static grant of a mana ability or a
//     ManaConvert static) makes the payment provable, so no exception either.
//     All arms are refused by name instead of emitted;
//   - the exception only rests on the proven unconditional self-removal
//     shape (provenUpkeepRemoval): a condition on the trigger or effect
//     (Impending Disaster's seven lands, Defense of the Heart's CheckSVar,
//     fading's counters_EQ0 with placed FADE counters) means the drive does
//     not provably take the card off the battlefield, so it stays refused;
//   - Tapped names bind to copies (TappedCopies, as the lowering binds
//     them): four Plains with Tapped ["Plains"] leave three untapped copies
//     for the startedTheTurnUntapped stamp Rasputin names from hand, and
//     three untapped Plains minus one for a payable echo; all four tapped
//     leaves no stamp anywhere, so no refusal.
//
// Reverting any guard in the sweep — the targeted-Destroy arms, the
// payable-echo half, the unparsed-cost arm, the shape allowlist, the
// non-land/non-basic/trigger/attachment/token mana arms, or the per-copy
// TappedCopies/BackFaceCopies reads — turns one expected refusal back into ""
// (or one expected "" into a refusal) and fails here.
func TestScenStartGateRefusalSweepsFinishedStart(t *testing.T) {
	reg, _ := testBuilder(t)
	start := func(p0 StartSeat) *StartFile {
		f := &StartFile{Format: StartFormat, Players: 2, Seeds: 2}
		f.Setup.P0 = p0
		return f
	}
	for _, r := range []struct {
		name string
		p0   StartSeat
		want string
	}{
		{"targeted destroy, triggered player's creatures", StartSeat{Battlefield: []string{"The Abyss", "Grizzly Bears"}}, "start:upkeep-trigger"},
		{"targeted destroy of a countered creature", StartSeat{Battlefield: []string{"Crumbling Ashes"}}, "start:upkeep-trigger"},
		{"payable echo, spare generic", StartSeat{Battlefield: []string{"Earthlink", "Kami of False Hope", "Plains", "Plains"}}, "start:upkeep-trigger"},
		{"payable echo under a partially tapped name", StartSeat{Battlefield: []string{"Earthlink", "Kami of False Hope", "Plains", "Plains", "Plains"}, Tapped: []string{"Plains"}}, "start:upkeep-trigger"},
		{"unpayable echo (develop's generated start)", StartSeat{Battlefield: []string{"Earthlink", "Kami of False Hope"}}, ""},
		{"payable echo colour", StartSeat{Battlefield: []string{"Thelon's Chant", "Grizzly Bears", "Forest"}}, "start:upkeep-trigger"},
		{"unpayable echo colour (develop's generated start)", StartSeat{Battlefield: []string{"Thelon's Chant", "Grizzly Bears"}}, ""},
		{"payable non-mana unless cost, counter", StartSeat{Battlefield: []string{"Magmasaur"}}, "start:upkeep-trigger"},
		{"payable non-mana unless cost, tap", StartSeat{Battlefield: []string{"Koskun Falls", "Grizzly Bears"}}, "start:upkeep-trigger"},
		{"payable echo from an artifact", StartSeat{Battlefield: []string{"Earthlink", "Kami of False Hope", "Sol Ring"}}, "start:upkeep-trigger"},
		{"payable echo from a multi-mana land", StartSeat{Battlefield: []string{"Earthlink", "Kami of False Hope", "Ancient Tomb"}}, "start:upkeep-trigger"},
		{"payable echo from a mana token", StartSeat{Battlefield: []string{"Earthlink", "Kami of False Hope"}, Tokens: []StartToken{{Script: "c_a_treasure_sac"}}}, "start:upkeep-trigger"},
		{"payable echo from an attached mana trigger", StartSeat{Battlefield: []string{"Earthlink", "Kami of False Hope", "Plains", "Wild Growth"}, Attach: []StartAttach{{Card: "Wild Growth", To: "Plains"}}}, "start:upkeep-trigger"},
		{"payable echo from a granted mana ability", StartSeat{Battlefield: []string{"Earthlink", "Kami of False Hope", "Basal Sliver"}}, "start:upkeep-trigger"},
		{"payable echo by converted colour", StartSeat{Battlefield: []string{"Thelon's Chant", "Grizzly Bears", "Island", "Mycosynth Lattice"}}, "start:upkeep-trigger"},
		{"conditional self-sacrifice, seven lands", StartSeat{Battlefield: []string{"Impending Disaster"}}, "start:upkeep-trigger"},
		{"conditional self-sacrifice, checked creature count", StartSeat{Battlefield: []string{"Defense of the Heart"}}, "start:upkeep-trigger"},
		{"conditional self-sacrifice, fading counters", StartSeat{Battlefield: []string{"Blastoderm"}, Counters: []StartCounter{{Card: "Blastoderm", Kind: "FADE", N: 3}}}, "start:upkeep-trigger"},
		{"mention word with one tapped duplicate", StartSeat{Hand: []string{"Rasputin Dreamweaver"}, Battlefield: []string{"Plains", "Plains", "Plains", "Plains"}, Tapped: []string{"Plains"}}, "start:started-turn-untapped"},
		{"mention word with every copy tapped", StartSeat{Hand: []string{"Rasputin Dreamweaver"}, Battlefield: []string{"Plains", "Plains", "Plains", "Plains"}, Tapped: []string{"Plains", "Plains", "Plains", "Plains"}}, ""},
	} {
		if got := startGateRefusal(reg, start(r.p0)); got != r.want {
			t.Errorf("%s: startGateRefusal %q, want %q", r.name, got, r.want)
		}
	}
}

// TestCounterBindingRefusal: a counters row binds by card name, so a bearer
// whose name appears twice on its seat's battlefield is refused (the lower's
// setup_by_name:counters), not written as a start the followers cannot replay.
// A player counter (Card empty) binds to the seat and needs no permanent.
func TestCounterBindingRefusal(t *testing.T) {
	f := &StartFile{}
	f.Setup.P0.Battlefield = []string{"Mountain", "Mountain"}
	f.Setup.P0.Counters = []StartCounter{{Card: "Mountain", Kind: "P1P1", N: 1}}
	if got := counterBindingRefusal(f); got != "start:counters-by-name" {
		t.Errorf("counter on one of two Mountains: %q, want start:counters-by-name", got)
	}
	f.Setup.P0.Battlefield = append(f.Setup.P0.Battlefield, "Wall of Stone")
	f.Setup.P0.Counters = []StartCounter{{Card: "Wall of Stone", Kind: "P1P1", N: 1}}
	if got := counterBindingRefusal(f); got != "" {
		t.Errorf("counter on the one Wall of Stone: %q, want no refusal", got)
	}
	f.Setup.P0.Counters = []StartCounter{{Kind: "ENERGY", N: 1}}
	if got := counterBindingRefusal(f); got != "" {
		t.Errorf("player counter: %q, want no refusal", got)
	}
}

// TestScenCounterBindingRefusalFromPlan drives the guard through the recipe: two
// SubCounter costs in one ability both pick the same inert bearer, so the
// finished start lists Wall of Stone twice while both counters entries bind
// to that name. The lowering refuses that shape (setup_by_name:counters), so
// the generator must refuse it too instead of emitting an unlowerable start.
// No corpus card produces the shape today, so the double-cost card is
// synthetic; the real corpus fills the certification deck certdeck builds
// around it. Deleting the counterBindingRefusal call from recipe turns this
// refusal into a start and fails here.
func TestScenCounterBindingRefusalFromPlan(t *testing.T) {
	reg := cards.NewRegistry()
	add := func(name, src string) {
		t.Helper()
		c, diags := cards.ParseBytes(name+".txt", []byte(src))
		if len(diags) != 0 {
			t.Fatalf("%s: diags %v", name, diags)
		}
		reg.Add(c)
	}
	add("Wall of Stone", "Name:Wall of Stone\nManaCost:1 R\nTypes:Creature Wall\nPT:0/8\nOracle:Defender\n")
	add("Ion Storm", "Name:Ion Storm\nManaCost:0\nTypes:Artifact\n"+
		"A:AB$ Mana | Cost$ SubCounter<1/CHARGE/Creature> SubCounter<1/P1P1/Creature> | Produced$ C | SpellDescription$ Add {C}.\n"+
		"Oracle:{T}: Add {C}.\n")
	// The real corpus supplies the fillers and lands a deck needs. The two
	// synthetics go in first, so the registry's first-wins name index keeps
	// them over the corpus cards of the same names.
	for _, c := range testutil.CorpusRegistry(t).AllCards() {
		reg.Add(c)
	}
	sp, err := decks.LoadSpellings(manifests)
	if err != nil {
		t.Fatal(err)
	}
	b := certdeck.NewBuilder(reg, sp)
	// The cost plan is the duplicated-name shape itself: both costs ride the
	// one inert bearer, and the start would list it twice.
	card, ok := reg.Lookup("Ion Storm")
	if !ok || len(card.Faces) == 0 {
		t.Fatal("synthetic Ion Storm not in the registry")
	}
	plan, why := planCost(reg, b, card.Faces[0], "Ion Storm",
		"SubCounter<1/CHARGE/Creature> SubCounter<1/P1P1/Creature>", "")
	if why != "" {
		t.Fatalf("planCost refused %q, want the duplicated-bearer plan", why)
	}
	if got := countAmong(plan.field, "Wall of Stone"); got != 2 {
		t.Fatalf("cost plan places Wall of Stone %d times %v, want twice", got, plan.field)
	}
	res, err := Generate(reg, b, "Ion Storm", "f0/a0", Options{Prefer: "start"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Path != PathRefused || res.Reason != "start:counters-by-name" {
		t.Fatalf("Ion Storm f0/a0: path %q reason %q, want refused start:counters-by-name", res.Path, res.Reason)
	}
}

// The refusal reasons of cards unrelated to the harness census are unchanged.
func TestScenRefusalReasonsUnchanged(t *testing.T) {
	reg, b := testBuilder(t)
	for _, r := range []struct{ card, key, reason string }{
		{"Thornvault Forager", "f0/a1", "steps-needed:cost:Forage"},
		{"Sower of Discord", "f0/t0", "as-enters:player"},
		{"Sorcerous Spyglass", "static#0.0", "as-enters:player"},
		{"Unclaimed Territory", "f0/a1", "as-enters:land"},
		// A static whose own setup refuses keeps its named refusal: the cast
		// must not replace a key staticKeyStart cannot observe.
		{"Banner of Kinship", "static#0.0", "no-generator:static:affected"},
		{"Iona, Shield of Emeria", "static#0.0", "no-generator:static:CantBeCast"},
		{"Cloud Key", "static#0.0", "no-generator:static:ReduceCost"},
		{"Spiders-Man, Heroic Horde", "f0/t0", "history:websling"},
		{"Rocket-Powered Goblin Glider", "f0/t0", "history:castfrom"},
	} {
		res, err := Generate(reg, b, r.card, r.key, Options{Prefer: "start"})
		if err != nil {
			t.Fatalf("%s %s: %v", r.card, r.key, err)
		}
		if res.Path != PathRefused || res.Reason != r.reason {
			t.Errorf("%s %s: path %q reason %q, want refused %q", r.card, r.key, res.Path, res.Reason, r.reason)
		}
	}
}

// harness-census.tsv: the signature rows sum to the declared harness total
// and each row's card count matches its names.
func TestHarnessCensus(t *testing.T) {
	b, err := os.ReadFile("testdata/harness-census.tsv")
	if err != nil {
		t.Fatal(err)
	}
	total, sum, rows := -1, 0, 0
	for _, l := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		if v, ok := strings.CutPrefix(l, "# harness-keys "); ok {
			total, _ = strconv.Atoi(v)
			continue
		}
		if strings.HasPrefix(l, "#") || strings.HasPrefix(l, "signature\t") {
			continue
		}
		c := strings.Split(l, "\t")
		if len(c) != 4 || !strings.HasPrefix(c[0], "x=") || !strings.Contains(c[0], "+f=") {
			t.Fatalf("bad census row %q", l)
		}
		keys, _ := strconv.Atoi(c[1])
		cards, _ := strconv.Atoi(c[2])
		if names := strings.Split(c[3], "; "); len(names) != cards || keys < cards {
			t.Errorf("%s: %d keys, %d cards, names %v", c[0], keys, cards, names)
		}
		sum += keys
		rows++
	}
	if total < 0 || rows == 0 || sum != total {
		t.Errorf("census rows sum to %d keys over %d signatures, header says %d", sum, rows, total)
	}
}

// TestStartFilterFixtures: the ShapesFilterFixtures shapes that the stock
// inert-carrier pool could not answer, each once (xproof top-owed
// no-generator:trigger:<mode>:filter, census 11-27):
//
//   - ChangesZone on the bearer an Aura enchants (Card.EnchantedBy) or an
//     Equipment equips (Card.EquippedBy): the pinned gorge reads EquippedBy,
//     EnchantedBy and AttachedBy as one predicate, so the bearer-death start
//     is attachedByStart's (an Aura StartAttach + Murder, an Equipment +
//     Magnetic Theft + an end-step self-sacrifice);
//   - a land the filter takes dies (Land.!token+YouOwn): the removal is
//     Stone Rain, since Murder cannot destroy a land;
//   - an enters trigger whose class no vanilla card holds (Detective) takes
//     the simplest playable permanent the filter takes;
//   - an enters trigger whose class only Auras hold (Cartouche) casts the
//     Aura at a vanilla bearer, naming it, so the Aura enters attached;
//   - "whenever a creature you cast this turn enters" (wasCastByYou) is the
//     carrier cast itself: the stripped filter picks the creature and the
//     cast makes the property true of the entering object;
//   - "a creature you control with a counter on it" (HasCounters) is the
//     generic counter gate: the start places a +1/+1 counter on the creature
//     the trigger reads, whether it dies (The Ozolith) or attacks (Yathan
//     Tombguard's DamageDone dealer).
func TestStartFilterFixtures(t *testing.T) {
	reg, b := testBuilder(t)
	type row struct {
		card, key string
		// focus: seat, card, kind the focus play must have ("" for any).
		fSeat int
		fKind string
		// carrierProp, when set, must hold of the focus's first target (the
		// removal target, the Aura's bearer) or of the focus cast card.
		carrierProp func(*cards.Face) bool
		carrierWhy  string
		// needCounter: the start must place a counter on the named target or
		// the focus attack card.
		needCounter bool
		// propOnFocus: check carrierProp of the focus card (the cast spell),
		// not of a focus target (whose bearer sits in Targets beside it).
		propOnFocus bool
		// focusCard, when set, is the focus cast card the fixture must name
		// (an Equipment cannot enter attached from a start, so the focus is
		// the Magnetic Theft that attaches it).
		focusCard string
		// attach: the focus cast must name the key card first and the equipped
		// creature second, the order Magnetic Theft takes its targets in; a
		// start that decodes but never attaches would pass the shape checks
		// above and fail to fire.
		attach bool
	}
	rows := []row{
		{card: "Necrotic Plague", key: "f0/t0", fSeat: 1, fKind: "cast",
			carrierProp: (*cards.Face).IsCreature, carrierWhy: "bearer"},
		{card: "Skullclamp", key: "f0/t0", fSeat: 0, fKind: "cast",
			carrierProp: (*cards.Face).IsCreature, carrierWhy: "equipped bearer",
			focusCard: "Magnetic Theft", attach: true},
		{card: "Skola Grovedancer", key: "f0/t0", fSeat: 1, fKind: "cast",
			carrierProp: (*cards.Face).IsLand, carrierWhy: "land"},
		{card: "Case of the Pilfered Proof", key: "f0/t0", fSeat: 0, fKind: "cast",
			carrierProp: func(f *cards.Face) bool { return f.HasTypeWord("Detective") }, carrierWhy: "Detective"},
		{card: "Trial of Ambition", key: "f0/t1", fSeat: 0, fKind: "cast", propOnFocus: true,
			carrierProp: func(f *cards.Face) bool { return f.HasTypeWord("Aura") && f.HasTypeWord("Cartouche") }, carrierWhy: "Cartouche Aura"},
		{card: "The Sibsig Ceremony", key: "f0/t0", fSeat: 0, fKind: "cast",
			carrierProp: (*cards.Face).IsCreature, carrierWhy: "cast creature"},
		{card: "The Ozolith", key: "f0/t0", fSeat: 1, fKind: "cast",
			carrierProp: (*cards.Face).IsCreature, carrierWhy: "countered creature", needCounter: true},
		{card: "Yathan Tombguard", key: "f0/t0", fSeat: 0, fKind: "attack",
			carrierProp: (*cards.Face).IsCreature, carrierWhy: "countered attacker", needCounter: true},
		{card: "Elite Scaleguard", key: "f0/t1", fSeat: 0, fKind: "attack",
			carrierProp: (*cards.Face).IsCreature, carrierWhy: "counter-gated attacker", needCounter: true},
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
			if f.Focus == nil || f.Focus.Seat == nil || *f.Focus.Seat != r.fSeat || f.Focus.Kind != r.fKind {
				t.Fatalf("focus %+v, want seat %d kind %q", f.Focus, r.fSeat, r.fKind)
			}
			// The carrier is a focus target that is not the key card (a
			// removal's victim, an Aura's bearer, Magnetic Theft's equipped
			// creature, named second), else the focus cast card itself.
			carrier := f.Focus.Card
			if !r.propOnFocus {
				for _, tname := range f.Focus.Targets {
					if tname != r.card {
						carrier = tname
						break
					}
				}
			}
			if r.focusCard != "" && f.Focus.Card != r.focusCard {
				t.Errorf("focus card %q, want %q", f.Focus.Card, r.focusCard)
			}
			if r.attach {
				// Magnetic Theft targets the Equipment, then the creature it
				// attaches to: naming both is the attach.
				if len(f.Focus.Targets) != 2 || f.Focus.Targets[0] != r.card || f.Focus.Targets[1] != carrier {
					t.Errorf("focus targets %v, want [%q %q] (equipment then equipped creature)", f.Focus.Targets, r.card, carrier)
				}
			}
			c, ok := reg.Lookup(carrier)
			if !ok || len(c.Faces) == 0 {
				t.Fatalf("carrier %q is not in the corpus", carrier)
			}
			if !r.carrierProp(c.Faces[0]) {
				t.Errorf("carrier %q does not meet the filter (%s)", carrier, r.carrierWhy)
			}
			if r.needCounter {
				found := false
				for _, s := range []*StartSeat{&f.Setup.P0, &f.Setup.P1} {
					for _, ct := range s.Counters {
						if ct.Card == carrier && ct.N >= 1 {
							found = true
						}
					}
				}
				if !found {
					t.Errorf("no counter placed on %q: p0 %+v p1 %+v", carrier, f.Setup.P0.Counters, f.Setup.P1.Counters)
				}
			}
			if _, err := DecodeStartFile(mustMarshal(t, f)); err != nil {
				t.Errorf("start file does not decode: %v", err)
			}
		})
	}
}

// TestAttacksCounterGatePlacesCounter: an Attacks filter's counter gate
// ("Creature.YouCtrl+counters_GE1_P1P1") is unknown to parseFilter, which used
// to make attacksStart return a self-attack without the counter and skip
// attackCarrier's placement arm. attacksStart must route the unknown-counter
// case through attackCarrier: the attacker is the card and the counter gate is
// placed in the setup (no recipe/placeNeeds is involved here).
func TestAttacksCounterGatePlacesCounter(t *testing.T) {
	reg, b := testBuilder(t)
	c, ok := reg.Lookup("Elite Scaleguard")
	if !ok || len(c.Faces) == 0 {
		t.Fatal("Elite Scaleguard is not in the corpus")
	}
	face := c.Faces[0]
	var trig *cards.Trigger
	for i := range face.Triggers {
		if face.Triggers[i].Mode == "Attacks" {
			trig = &face.Triggers[i]
			break
		}
	}
	if trig == nil {
		t.Fatal("Elite Scaleguard has no Attacks trigger")
	}
	var f StartFile
	if why := attacksStart(reg, b, face, "Elite Scaleguard", trig, &f); why != "" {
		t.Fatalf("attacksStart refused: %s", why)
	}
	if f.Focus == nil || f.Focus.Card != "Elite Scaleguard" || f.Focus.Kind != "attack" {
		t.Fatalf("focus %+v, want Elite Scaleguard attacking", f.Focus)
	}
	for _, ct := range f.Setup.P0.Counters {
		if ct.Card == "Elite Scaleguard" && ct.N >= 1 {
			return
		}
	}
	t.Errorf("no counter placed on the attacker: %+v", f.Setup.P0.Counters)
}

// TestStartFilterFixtureCounterGateRefused: an enters filter that requires a
// counter on the entering object cannot be served by a cast fixture. A start's
// setup counters have nothing to sit on while the card is still in hand, so
// the carrier the fixture scan could pick would enter without the gate and the
// trigger would never reach the stack (Ichorplate Golem: "whenever a creature
// you control enters, if it has one or more oil counters on it"). The lane
// must keep the ":filter" refusal rather than move the key refused→start.
func TestStartFilterFixtureCounterGateRefused(t *testing.T) {
	reg, b := testBuilder(t)
	for _, c := range []struct{ card, key string }{
		{"Ichorplate Golem", "f0/t0"},
	} {
		res, err := Generate(reg, b, c.card, c.key, Options{Prefer: "start"})
		if err != nil {
			t.Fatalf("%s %s: %v", c.card, c.key, err)
		}
		if res.Path != PathRefused || res.Reason != "no-generator:trigger:ChangesZone:filter" {
			t.Errorf("%s %s: path %q reason %q, want refused no-generator:trigger:ChangesZone:filter",
				c.card, c.key, res.Path, res.Reason)
		}
	}
}

// TestStartFilterFixtureAttachFilterGated: an attachment trigger whose filter
// also gates the bearer on a property must make that property hold of the
// bearer, or refuse. The bearer lane used to route any filter that merely
// contained EnchantedBy/EquippedBy to attachedByStart, which chose the bearer
// from the Aura's Enchant keyword (or the Equipment's end-step sacrifice pool)
// alone:
//
//   - Endless Evil (Card.EnchantedBy+Horror) attached to Memnite, a bearer
//     that is not a Horror, so "when enchanted Horror dies" could never fire.
//     The start may now serve a Horror bearer or refuse: both are honest, and
//     an inert Horror (Vorstclaw) is in the carrier pool;
//   - Forgeborn Phoenix (Creature.EquippedBy,Creature.Self) is itself a
//     creature Equipment, so Magnetic Theft's second target (a creature) can
//     be answered with the Equipment itself and it attaches to itself. That
//     start must stay refused;
//   - Curator's Ward (Card.AttachedBy+Historic) can be served on a historic
//     bearer, but its own static gives that bearer hexproof, so the opponent's
//     Murder is never offered; the lane must refuse rather than attach and
//     never fire.
func TestStartFilterFixtureAttachFilterGated(t *testing.T) {
	reg, b := testBuilder(t)
	const none = "no-generator:trigger:ChangesZone:filter"
	res, err := Generate(reg, b, "Endless Evil", "f0/t1", Options{Prefer: "start"})
	if err != nil {
		t.Fatalf("Endless Evil f0/t1: %v", err)
	}
	switch {
	case res.Path == PathRefused:
		if res.Reason != none {
			t.Errorf("Endless Evil f0/t1 refused with %q, want %q", res.Reason, none)
		}
	case res.Path == PathStart && res.Start != nil && res.Start.Focus != nil:
		// The focus is Murder destroying the Aura's bearer: the first target
		// that is not the Aura itself.
		bearer := ""
		for _, name := range res.Start.Focus.Targets {
			if name != "Endless Evil" {
				bearer = name
				break
			}
		}
		c, ok := reg.Lookup(bearer)
		if !ok || len(c.Faces) == 0 || !c.Faces[0].HasTypeWord("Horror") {
			t.Errorf("Endless Evil f0/t1 served on bearer %q, want a Horror or a refusal", bearer)
		}
	default:
		t.Errorf("Endless Evil f0/t1: path %q reason %q, want a start on a Horror or %q", res.Path, res.Reason, none)
	}
	for _, c := range []struct{ card, key string }{
		{"Forgeborn Phoenix", "f0/t0"},
		{"Curator's Ward", "f0/t0"},
	} {
		res, err := Generate(reg, b, c.card, c.key, Options{Prefer: "start"})
		if err != nil {
			t.Fatalf("%s %s: %v", c.card, c.key, err)
		}
		if res.Path != PathRefused || res.Reason != none {
			t.Errorf("%s %s: path %q reason %q, want refused %q", c.card, c.key, res.Path, res.Reason, none)
		}
	}
}

// TestStartAttachBearerSurvivesAura: an Aura whose own static shrinks its
// bearer (Infectious Rage +2/-1, Screams from Within -1/-1) must not be put
// on a bearer that dies to state-based actions during setup; the start either
// refuses :filter or names a bearer whose toughness stays above zero.
func TestStartAttachBearerSurvivesAura(t *testing.T) {
	reg, b := testBuilder(t)
	const none = "no-generator:trigger:ChangesZone:filter"
	for _, name := range []string{"Infectious Rage", "Screams from Within"} {
		aura, ok := reg.Lookup(name)
		if !ok || len(aura.Faces) == 0 {
			t.Fatalf("%s is not in the registry", name)
		}
		res, err := Generate(reg, b, name, "f0/t0", Options{Prefer: "start"})
		if err != nil {
			t.Fatalf("%s f0/t0: %v", name, err)
		}
		if res.Path == PathRefused {
			if res.Reason != none {
				t.Errorf("%s f0/t0 refused with %q, want %q", name, res.Reason, none)
			}
			continue
		}
		if res.Path != PathStart || res.Start == nil || len(res.Start.Setup.P0.Attach) != 1 {
			t.Fatalf("%s f0/t0: path %q reason %q, want a start with one attachment", name, res.Path, res.Reason)
		}
		bearer := res.Start.Setup.P0.Attach[0].To
		c, ok := reg.Lookup(bearer)
		if !ok || len(c.Faces) == 0 {
			t.Fatalf("%s: bearer %q is not in the registry", name, bearer)
		}
		if !survivesAura(aura.Faces[0], c.Faces[0]) {
			t.Errorf("%s f0/t0: bearer %q (toughness %d) dies to the Aura at setup", name, bearer, c.Faces[0].Toughness())
		}
	}
	// The predicate itself: a 1/1 dies to a -1/-1 or +2/-1 Aura, a 2/2 keeps
	// toughness 1 under both.
	for _, name := range []string{"Infectious Rage", "Screams from Within"} {
		aura, _ := reg.Lookup(name)
		one, _ := reg.Lookup("Memnite")
		two, _ := reg.Lookup("Grizzly Bears")
		if survivesAura(aura.Faces[0], one.Faces[0]) {
			t.Errorf("%s: a 1/1 bearer must not survive", name)
		}
		if !survivesAura(aura.Faces[0], two.Faces[0]) {
			t.Errorf("%s: a 2/2 bearer must survive", name)
		}
	}
}

// TestCastableAlt: the gate castFixtureCarrierIn passes to the fixture
// fallback. A counter gate (counters_*, HasCounters), a token requirement and
// a top-of-library requirement are alternatives a cast cannot establish; a
// non-token requirement and a plain card are.
func TestCastableAlt(t *testing.T) {
	for _, tc := range []struct {
		filter string
		cast   bool
	}{
		{"Creature.YouCtrl", true},
		{"Creature.YouCtrl+!token", true},
		{"Creature.YouCtrl+counters_GE1_OIL", false},
		{"Creature.YouCtrl+HasCounters", false},
		{"Creature.YouCtrl+token", false},
		{"Creature.YouCtrl+TopLibrary", false},
	} {
		t.Run(tc.filter, func(t *testing.T) {
			alts := parseFilterStatic(tc.filter)
			if len(alts) != 1 {
				t.Fatalf("filter %q parsed to %d alternatives", tc.filter, len(alts))
			}
			if !alts[0].known {
				t.Fatalf("filter %q is not understood", tc.filter)
			}
			if got := castableAlt(alts[0]); got != tc.cast {
				t.Errorf("castableAlt(%q) = %v, want %v (counters %v, token %d, topLibrary %v)",
					tc.filter, got, tc.cast, alts[0].counters, alts[0].token, alts[0].topLibrary)
			}
		})
	}
}

// TestStartCombatFilterFixtures: the two combat :filter shapes the fixture
// lanes now answer. "When enchanted creature is dealt damage" (Cracked Skull,
// Sporogenic Infection: ValidTarget Creature.EnchantedBy) is one scripted
// combat with the Aura placed attached (StartAttach) to its bearer, which is
// the blocker that is hurt (gorge reads EnchantedBy as AttachedBy). And a
// filter the fixture cannot pair with (Gustcloak Harrier's own flying means
// Grizzly Bears cannot block it) takes a carrier blocker that can, rather
// than the plain ":filter" refusal.
func TestStartCombatFilterFixtures(t *testing.T) {
	reg, b := testBuilder(t)
	rows := []struct {
		card, key         string
		active            int
		attack, block     string // exact names, "" to skip
		blockProp         func(*cards.Face) bool
		wantAttachKeyCard bool
	}{
		{card: "Cracked Skull", key: "f0/t1", active: 1, blockProp: (*cards.Face).IsCreature, wantAttachKeyCard: true},
		{card: "Sporogenic Infection", key: "f0/t1", active: 1, blockProp: (*cards.Face).IsCreature, wantAttachKeyCard: true},
		{card: "Gustcloak Harrier", key: "f0/t0", active: 0, attack: "Gustcloak Harrier",
			blockProp: func(f *cards.Face) bool { return f.IsCreature() && (f.HasKeyword("Flying") || f.HasKeyword("Reach")) }},
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
			if f.Active != r.active || f.Focus == nil || f.Focus.Attack != r.attack && r.attack != "" {
				t.Fatalf("active %d focus %+v, want active %d attack %q", f.Active, f.Focus, r.active, r.attack)
			}
			if f.Focus.Block == "" {
				t.Fatalf("focus %+v, want a scripted blocker", f.Focus)
			}
			c, ok := reg.Lookup(f.Focus.Block)
			if !ok || len(c.Faces) == 0 || !r.blockProp(c.Faces[0]) {
				t.Fatalf("blocker %q does not meet the pairing", f.Focus.Block)
			}
			if r.wantAttachKeyCard {
				found := false
				for _, a := range f.Setup.P0.Attach {
					if a.Card == r.card && a.To == f.Focus.Block {
						found = true
					}
				}
				if !found {
					t.Errorf("p0 attach %+v, want %q attached to the blocker %q", f.Setup.P0.Attach, r.card, f.Focus.Block)
				}
			}
			if _, err := DecodeStartFile(mustMarshal(t, f)); err != nil {
				t.Errorf("start file does not decode: %v", err)
			}
		})
	}
}

// TestStartReplSelfEntryTap: a self-entry tap replacement (an enter-tapped
// land or creature) starts the card's own entry: a land is played from hand
// (a check land's condition is exercised whichever way it reads), anything
// else is cast.
func TestStartReplSelfEntryTap(t *testing.T) {
	t.Parallel()
	reg, b := testBuilder(t)
	for _, tc := range []struct{ card, kind string }{
		{"Azorius Guildgate", "land"},
		{"Glacial Fortress", "land"},
		{"Wolf Cove Villager", "cast"},
	} {
		t.Run(tc.card, func(t *testing.T) {
			res, err := Generate(reg, b, tc.card, "repl#0.0", Options{Prefer: "start"})
			if err != nil {
				t.Fatal(err)
			}
			if res.Path != PathStart || res.Start == nil {
				t.Fatalf("path %q reason %q, want a start", res.Path, res.Reason)
			}
			focusIs(t, res.Start, 0, tc.card, tc.kind, 0)
			if res.Start.Key != "repl#0.0" {
				t.Errorf("start key %q, want repl#0.0", res.Start.Key)
			}
			listIs(t, "p0 hand", res.Start.Setup.P0.Hand, []string{tc.card})
		})
	}
}

// TestStartReplRefusals: a replacement outside the self-entry tap class is
// refused by name, a shock land's pay-or-tap choice by its own name.
func TestStartReplRefusals(t *testing.T) {
	t.Parallel()
	reg, b := testBuilder(t)
	for _, tc := range []struct{ card, key, reason string }{
		{"Hallowed Fountain", "repl#0.0", "no-generator:repl:unless-cost"},
		{"Kinjalli's Sunwing", "repl#0.0", "no-generator:repl:Moved"},
	} {
		t.Run(tc.card, func(t *testing.T) {
			res, err := Generate(reg, b, tc.card, tc.key, Options{})
			if err != nil {
				t.Fatal(err)
			}
			if res.Path != PathRefused || res.Start != nil || res.Reason != tc.reason {
				t.Fatalf("path %q reason %q, want refused %q", res.Path, res.Reason, tc.reason)
			}
		})
	}
}

// TestStartReplKeyIDRoundTrip: a replacement start's item id names its key,
// so keyproof's declaredKey credits it.
func TestStartReplKeyIDRoundTrip(t *testing.T) {
	t.Parallel()
	id := StartID("SCEN", "Azorius Guildgate", "repl#0.0", 1)
	if id != "xs:SCEN:azorius-guildgate:repl0.0:s1" {
		t.Errorf("StartID = %q, want xs:SCEN:azorius-guildgate:repl0.0:s1", id)
	}
	if _, _, key, _, ok := ParseID(id); !ok || key != "repl#0.0" {
		t.Errorf("ParseID(%q) key %q ok %v, want repl#0.0", id, key, ok)
	}
	if got := Family("repl#0.0"); got != "repl" {
		t.Errorf("Family(repl#0.0) = %q, want repl", got)
	}
}

// TestStartKeywordAndModeRefused: a keyword or spell-mode key of the universe
// is an owed row, so Generate refuses it by name rather than erroring.
func TestStartKeywordAndModeRefused(t *testing.T) {
	t.Parallel()
	reg, b := testBuilder(t)
	for _, tc := range []struct{ card, key, reason string }{
		{"The Eldest Reborn", "kw#0.0", "no-generator:keyword"},
		{"Cryptic Command", "mode#0.0.0", "no-generator:mode"},
	} {
		t.Run(tc.card, func(t *testing.T) {
			c, ok := reg.Lookup(tc.card)
			if !ok {
				t.Fatalf("%q not in the corpus", tc.card)
			}
			if !slices.ContainsFunc(cov.Universe(c), func(k cov.Key) bool { return k.ID == tc.key }) {
				t.Fatalf("%q is not a cov.Universe key of %q", tc.key, tc.card)
			}
			if slices.Contains(Keys(c), tc.key) {
				t.Errorf("Keys(%q) lists %q", tc.card, tc.key)
			}
			res, err := Generate(reg, b, tc.card, tc.key, Options{})
			if err != nil {
				t.Fatal(err)
			}
			if res.Path != PathRefused || res.Reason != tc.reason {
				t.Fatalf("path %q reason %q, want refused %q", res.Path, res.Reason, tc.reason)
			}
		})
	}
}
