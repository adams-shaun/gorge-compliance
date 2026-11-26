package scen

import "testing"

// TestStartDayNightRefused: a start whose game sets the day/night designation
// (CR 731) cannot be lowered (Game.DayNight is refused, the follower digests
// do not export it), so play wrote refused for every seed and keyproof read
// the key as start-not-exercised (xproof 11260225: 92 keys, 84 cards). scen
// now refuses it by name, whether the cause is the focus card (Weaver of
// Blossoms is daybound) or a fixture the start placed (Lunar Rejection's
// werewolf target is daybound). Unrelated cards keep their reasons and starts.
func TestStartDayNightRefused(t *testing.T) {
	reg, b := testBuilder(t)
	for _, tc := range []struct{ card, key string }{
		{"Lunar Rejection", "cast"},
		{"Weaver of Blossoms", "f0/a0"},
		{"Weaver of Blossoms", "f1/a0"},
		{"Wolfkin Outcast", "cast"},
		// the back face trigger of a daybound card: TestStartfacestarts held a
		// start here that no follower could replay
		{"Wolfkin Outcast // Wedding Crasher", "f1/t0"},
		{"Volatile Arsonist", "cast"},
	} {
		res, err := Generate(reg, b, tc.card, tc.key, Options{Prefer: "start"})
		if err != nil {
			t.Fatalf("%s %s: %v", tc.card, tc.key, err)
		}
		if res.Path != PathRefused || res.Reason != reasonDayNight {
			t.Errorf("%s %s: path %q reason %q, want refused %q", tc.card, tc.key, res.Path, res.Reason, reasonDayNight)
		}
	}
}

// TestStartDayNightUnrelatedUnchanged: cards with no day/night cause keep the
// start or the named refusal they had before the check existed.
func TestStartDayNightUnrelatedUnchanged(t *testing.T) {
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
	// a card that only reads the designation sets none
	for _, name := range []string{"Lightning Bolt", "Own the Day"} {
		if c, ok := reg.Lookup(name); ok && setsDayNight(c) {
			t.Errorf("%s: setsDayNight, want false", name)
		}
	}
}
