package scen

import (
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
)

// Day/night designations (CR 731.1).
const (
	DayNightDay   = "day"
	DayNightNight = "night"
)

// reasonDayNight names a start whose initial battlefield sets a day/night
// designation the generator cannot read (a DayTime replacement whose value is
// not day or night). Every ordinary setter is declared on StartFile instead:
// the replay re-derives the same designation from the start's own daybound or
// nightbound placements.
const reasonDayNight = "start:day-night"

// dayNightOfFace reports the designation face gives the game as it enters the
// battlefield from nowhere, and whether that change is gated on the game being
// at neither. A daybound permanent makes it day and a nightbound one night
// (CR 702.145a); a Moved replacement whose body is a DayTime effect ("if it's
// neither day nor night, it becomes day as CARDNAME enters", gate DayTime$
// Neither) gives the value it sets. An ungated Moved DayTime replacement sets
// the designation whoever enters, so it is reported as conditional=false: it
// overrides a designation an earlier placement set. An ability or trigger that
// changes the designation later (a cast's ETB trigger, an upkeep "it becomes
// night") is not an entry state: at the start's cut it has not happened yet,
// so it is not read here.
func dayNightOfFace(f *cards.Face) (value string, conditional bool) {
	if f == nil {
		return "", false
	}
	if f.HasKeyword("Daybound") {
		return DayNightDay, true
	}
	if f.HasKeyword("Nightbound") {
		return DayNightNight, true
	}
	for _, r := range f.Repls {
		if r.Event != "Moved" || !strings.Contains(r.Params["ValidCard"], "Card.Self") ||
			r.Params["Destination"] != "Battlefield" || r.With == nil || r.With.API != "DayTime" {
			continue
		}
		switch {
		case strings.EqualFold(r.With.Params["Value"], "Day"):
			value = DayNightDay
		case strings.EqualFold(r.With.Params["Value"], "Night"):
			value = DayNightNight
		default:
			return "unknown", false
		}
		return value, r.Params["DayTime"] == "Neither"
	}
	return "", false
}

// placedFace is the face a battlefield placement enters: the back face when
// the seat's BackFace binds that copy, else the front.
func placedFace(c *cards.Card, back bool) *cards.Face {
	if len(c.Faces) == 0 {
		return nil
	}
	if back && len(c.Faces) > 1 {
		return c.Faces[1]
	}
	return c.Faces[0]
}

// dayNightStart declares the designation the finished start's initial
// battlefield sets on either seat, and returns reasonDayNight when an entry
// value cannot be read. Placements run p0's cards then p1's, in battlefield
// order. A gated entry ("if it's neither day nor night") changes nothing when
// an earlier placement already set the designation, so it is skipped then; an
// ungated entry sets it whoever entered before, so it overrides. A focus cast
// declares nothing here: the card is still in hand at the cut.
func dayNightStart(reg *cards.Registry, f *StartFile) string {
	f.DayNight = ""
	state := ""
	for _, s := range []*StartSeat{&f.Setup.P0, &f.Setup.P1} {
		backs := s.BackFaceCopies()
		for i, n := range s.Battlefield {
			c, ok := reg.Lookup(n)
			if !ok {
				continue
			}
			got, conditional := dayNightOfFace(placedFace(c, backs[i]))
			switch got {
			case "":
				continue
			case "unknown":
				return reasonDayNight
			}
			if conditional && state != "" {
				continue
			}
			state = got
		}
	}
	f.DayNight = state
	return ""
}
