package scen

import (
	"testing"

	"github.com/adams-shaun/gorge/engine/cards"
)

// TestScenStartDayNightDeclared: a start whose initial battlefield sets the
// day/night designation (CR 731) declares it instead of refusing the key.
// xproof 11260225 refused every such start by name (92 keys, 84 cards) because
// the lowered digest does not carry Game.DayNight; the replay re-derives the
// designation from the start's own daybound/nightbound placements, so the
// start file states which one it is and the lower gate reads the placement
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
		if c, ok := reg.Lookup(name); ok && len(c.Faces) > 0 {
			if got, _ := dayNightOfFace(c.Faces[0]); got != "" {
				t.Errorf("%s: dayNightOfFace = %q, want none", name, got)
			}
		}
	}
}

// TestScenStartDayNightUngatedSetter: the corpus's Moved DayTime replacements
// are all gated DayTime$ Neither, so an earlier placement's designation wins.
// An ungated one ("it becomes day as CARDNAME enters") sets the designation
// whoever entered before, so the later placement overrides; both orders are
// checked so the first-setter reading cannot creep back in.
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
