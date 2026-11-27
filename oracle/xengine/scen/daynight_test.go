package scen

import (
	"testing"

	"github.com/adams-shaun/gorge/engine/cards"
)

// TestScenStartDayNightDeclared: a start whose initial battlefield has a
// day/night designation (CR 731) declares it instead of refusing the key.
// xproof 11260225 refused every such start by name (92 keys, 84 cards) because
// the lowered digest does not carry Game.DayNight; the replay re-derives the
// designation from the start's own placements and the turn-1 untap check, so
// the start file states which one it is and the lower gate reads the placement
// rather than the digest field.
func TestScenStartDayNightDeclared(t *testing.T) {
	reg, b := testBuilder(t)
	for _, tc := range []struct{ card, key, day string }{
		// a fixture the start places (Lunar Rejection's werewolf target)
		{"Lunar Rejection", "cast", DayNightDay},
		// the focus card itself, front (daybound) and back (nightbound)
		{"Weaver of Blossoms", "f0/a0", DayNightDay},
		{"Weaver of Blossoms", "f1/a0", DayNightNight},
		// a focus cast is still in hand at the cut: its entry has not happened,
		// so the start declares nothing and the cast itself sets the state
		{"Wolfkin Outcast", "cast", ""},
		{"Volatile Arsonist", "cast", ""},
	} {
		res, err := Generate(reg, b, tc.card, tc.key, Options{Prefer: "start"})
		if err != nil {
			t.Fatalf("%s %s: %v", tc.card, tc.key, err)
		}
		if res.Path != PathStart || res.Start == nil {
			t.Errorf("%s %s: path %q reason %q, want a start", tc.card, tc.key, res.Path, res.Reason)
			continue
		}
		if res.Start.DayNight != tc.day {
			t.Errorf("%s %s: day_night %q, want %q", tc.card, tc.key, res.Start.DayNight, tc.day)
		}
	}
}

// TestScenStartDayNightUnrelatedUnchanged: cards with no day/night cause keep the
// start or the named refusal they had before the check existed, and a card
// that only reads the designation sets none.
func TestScenStartDayNightUnrelatedUnchanged(t *testing.T) {
	reg, b := testBuilder(t)
	for _, tc := range []struct{ card, key string }{
		{"Lightning Bolt", "cast"},
		{"Voldaren Pariah", "f1/t0"},
		{"Blightreaper Thallid", "f1/t0"},
	} {
		res, err := Generate(reg, b, tc.card, tc.key, Options{Prefer: "start"})
		if err != nil || res.Path != PathStart || res.Start == nil {
			t.Errorf("%s %s: %+v %v, want a start", tc.card, tc.key, res, err)
		}
	}
	for _, tc := range []struct{ card, key, reason string }{
		{"Thing in the Ice", "f1/t0", "no-generator:trigger:Transformed:no-cause"},
		{"Alluring Suitor", "f1/t0", "no-generator:trigger:Transformed:attack"},
		{"Howlpack Piper", "f1/t1", "no-generator:trigger:Transformed:day-night"},
	} {
		res, err := Generate(reg, b, tc.card, tc.key, Options{Prefer: "start"})
		if err != nil || res.Path != PathRefused || res.Reason != tc.reason {
			t.Errorf("%s %s: %+v %v, want refused %q", tc.card, tc.key, res, err, tc.reason)
		}
	}
	for _, name := range []string{"Lightning Bolt", "Own the Day"} {
		c, ok := reg.Lookup(name)
		if !ok || len(c.Faces) == 0 {
			continue
		}
		if got, _, ok := entryDayNight(c.Faces[0]); got != "" || ok {
			t.Errorf("%s: entryDayNight = %q, %v, want none", name, got, ok)
		}
		if got := keywordDayNight(c.Faces[0]); got != "" {
			t.Errorf("%s: keywordDayNight = %q, want none", name, got)
		}
	}
}

// TestScenStartDayNightTurnOneCut: the declaration is the designation after
// turn 1's CR 731.2a untap check, not the genesis placement. A gated Moved
// DayTime replacement (Brimstone Vandal, Obsessive Astronomer) makes the game
// day as it enters during genesis; the first untap step sees the previous turn
// cast no spells and makes it night, so the cut is night. A daybound
// permanent's CR 702.145d state-based action runs after that check, so a
// lone daybound Weaver of Blossoms stays day; a daybound permanent cannot undo
// a day set by an entry replacement before the check.
func TestScenStartDayNightTurnOneCut(t *testing.T) {
	reg, _ := testBuilder(t)
	for _, tc := range []struct {
		name string
		p0   []string
		p1   []string
		back []string
		want string
	}{
		{"gated day entry", []string{"Brimstone Vandal"}, nil, nil, DayNightNight},
		{"gated day entry, second card", []string{"Obsessive Astronomer"}, nil, nil, DayNightNight},
		{"daybound alone", []string{"Weaver of Blossoms"}, nil, nil, DayNightDay},
		{"nightbound alone", []string{"Weaver of Blossoms"}, nil, []string{"Weaver of Blossoms"}, DayNightNight},
		{"gated day entry plus daybound", []string{"Brimstone Vandal", "Weaver of Blossoms"}, nil, nil, DayNightNight},
		{"gated day entry after daybound", []string{"Weaver of Blossoms"}, []string{"Brimstone Vandal"}, nil, DayNightNight},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &StartFile{Setup: StartSetup{
				P0: StartSeat{Battlefield: tc.p0, BackFace: tc.back},
				P1: StartSeat{Battlefield: tc.p1},
			}}
			if why := dayNightStart(reg, f); why != "" {
				t.Fatalf("dayNightStart refused %q", why)
			}
			if f.DayNight != tc.want {
				t.Errorf("day_night %q, want %q", f.DayNight, tc.want)
			}
		})
	}
}

// TestScenStartDayNightKeywordSchedule: a daybound/nightbound keyword is a
// state-based action after the turn's untap check, so it only applies when no
// Moved DayTime replacement set a designation during genesis. The synthetic
// registry keeps the two shapes independent of the corpus.
func TestScenStartDayNightKeywordSchedule(t *testing.T) {
	reg := cards.NewRegistry()
	addEntry := func(name, value, gate string) {
		reg.Add(&cards.Card{Faces: []*cards.Face{{
			Name: name,
			Repls: []cards.Repl{{
				Event:  "Moved",
				Params: map[string]string{"ValidCard": "Card.Self", "Destination": "Battlefield", "DayTime": gate},
				With:   &cards.SA{API: "DayTime", Params: map[string]string{"Value": value}},
			}},
		}}})
	}
	addKeyword := func(name, keyword string) {
		reg.Add(&cards.Card{Faces: []*cards.Face{{Name: name, Keywords: []string{keyword}}}})
	}
	addEntry("Gated Day", "Day", "Neither")
	addEntry("Ungated Day", "Day", "")
	addEntry("Ungated Night", "Night", "")
	addKeyword("Daybound", "Daybound")
	addKeyword("Nightbound", "Nightbound")
	for _, tc := range []struct {
		name string
		p0   []string
		p1   []string
		want string
	}{
		{"gated day then daybound", []string{"Gated Day", "Daybound"}, nil, DayNightNight},
		{"ungated day then daybound", []string{"Ungated Day", "Daybound"}, nil, DayNightNight},
		{"daybound then gated day", []string{"Daybound"}, []string{"Gated Day"}, DayNightNight},
		{"ungated night then daybound", []string{"Ungated Night", "Daybound"}, nil, DayNightNight},
		{"daybound alone", []string{"Daybound"}, nil, DayNightDay},
		{"nightbound alone", []string{"Nightbound"}, nil, DayNightNight},
		{"daybound then nightbound", []string{"Daybound", "Nightbound"}, nil, DayNightDay},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &StartFile{Setup: StartSetup{
				P0: StartSeat{Battlefield: tc.p0},
				P1: StartSeat{Battlefield: tc.p1},
			}}
			if why := dayNightStart(reg, f); why != "" {
				t.Fatalf("dayNightStart refused %q", why)
			}
			if f.DayNight != tc.want {
				t.Errorf("day_night %q, want %q", f.DayNight, tc.want)
			}
		})
	}
}

// TestScenStartDayNightUngatedSetter: within genesis an ungated Moved DayTime
// replacement sets the designation whoever entered before, while a gated one
// changes nothing once a designation exists. The untap check after genesis
// makes a day result night, so both orders here end at night.
func TestScenStartDayNightUngatedSetter(t *testing.T) {
	reg := cards.NewRegistry()
	add := func(name, value, gate string) {
		reg.Add(&cards.Card{Faces: []*cards.Face{{
			Name: name,
			Repls: []cards.Repl{{
				Event:  "Moved",
				Params: map[string]string{"ValidCard": "Card.Self", "Destination": "Battlefield", "DayTime": gate},
				With:   &cards.SA{API: "DayTime", Params: map[string]string{"Value": value}},
			}},
		}}})
	}
	add("Gated Day", "Day", "Neither")
	add("Ungated Night", "Night", "")
	for _, tc := range []struct {
		name string
		p0   []string
		p1   []string
		want string
	}{
		{"gated then ungated", []string{"Gated Day"}, []string{"Ungated Night"}, DayNightNight},
		{"ungated then gated", []string{"Ungated Night"}, []string{"Gated Day"}, DayNightNight},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &StartFile{Setup: StartSetup{
				P0: StartSeat{Battlefield: tc.p0},
				P1: StartSeat{Battlefield: tc.p1},
			}}
			if why := dayNightStart(reg, f); why != "" {
				t.Fatalf("dayNightStart refused %q", why)
			}
			if f.DayNight != tc.want {
				t.Errorf("day_night %q, want %q", f.DayNight, tc.want)
			}
		})
	}
}
