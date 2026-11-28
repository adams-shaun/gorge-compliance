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
// the replay re-derives the same designation from the start's own placements
// and the turn-1 untap check.
const reasonDayNight = "start:day-night"

// entryDayNight reports the designation a Moved DayTime replacement sets as
// the face enters the battlefield from nowhere. A daybound or nightbound
// keyword is NOT read here: CR 702.145d/g applies those from a state-based
// action, which runs after the turn-1 untap check, so a placement's keyword
// can never make the game day before that check. A Moved replacement is a
// replacement, so it does run as the permanent enters during genesis.
//
// gated says the replacement only sets the designation while the game is at
// neither ("if it's neither day nor night, it becomes day as CARDNAME
// enters"); an ungated replacement sets it whoever entered before. ok is
// false when the face has no such replacement, and value is "unknown" when it
// has one whose DayTime value is not day or night.
func entryDayNight(f *cards.Face) (value string, gated, ok bool) {
	if f == nil {
		return "", false, false
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
			return "unknown", false, true
		}
		return value, r.Params["DayTime"] == "Neither", true
	}
	return "", false, false
}

// keywordDayNight is the CR 702.145 state-based designation the face asks for
// while it is on the battlefield: day for daybound, night for nightbound, and
// "" for neither. It is applied only when the designation is still neither
// after the turn's untap check (CR 702.145d/g), not as the permanent enters.
func keywordDayNight(f *cards.Face) string {
	if f == nil {
		return ""
	}
	switch {
	case f.HasKeyword("Daybound"):
		return DayNightDay
	case f.HasKeyword("Nightbound"):
		return DayNightNight
	}
	return ""
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
// battlefield has at turn 1's first-main cut, and returns reasonDayNight when
// an entry value cannot be read. A focus cast declares nothing here: the card
// is still in hand at the cut.
//
// The model is the engine's own genesis-to-cut order:
//
//  1. Placements run p0's cards then p1's, in battlefield order. Only a Moved
//     DayTime replacement changes the designation here (CR 614 replaces the
//     entry). A gated one ("if it's neither day nor night") changes nothing
//     when an earlier placement already set the designation; an ungated one
//     overrides. Daybound/nightbound keywords are not read yet (CR 702.145d/g
//     is a state-based action, below).
//  2. Turn 1's untap step runs CR 731.2a before the cut. PrevTurnSpells is
//     zero there because no turn has completed, so a game that entered the
//     step at day becomes night.
//  3. With the designation still neither after the untap check, the
//     state-based action makes it day for a daybound permanent, else night for
//     a nightbound one (CR 702.145d/g). A designation step 1 set is left as
//     step 2 left it, so a daybound permanent cannot re-make it day.
func dayNightStart(reg *cards.Registry, f *StartFile) string {
	f.DayNight = ""
	state := ""
	var faces []*cards.Face
	for _, s := range []*StartSeat{&f.Setup.P0, &f.Setup.P1} {
		backs := s.BackFaceCopies()
		for i, n := range s.Battlefield {
			c, ok := reg.Lookup(n)
			if !ok {
				continue
			}
			face := placedFace(c, backs[i])
			faces = append(faces, face)
			value, gated, ok := entryDayNight(face)
			if !ok {
				continue
			}
			if value == "unknown" {
				return reasonDayNight
			}
			if gated && state != "" {
				continue
			}
			state = value
		}
	}
	// CR 731.2a at turn 1: the previous turn (there is none) cast no spells, so
	// a day designation becomes night before the first priority.
	if state == DayNightDay {
		state = DayNightNight
	}
	// CR 702.145d/g after the untap check: day wins over night when both a
	// daybound and a nightbound permanent are present.
	if state == "" {
		for _, face := range faces {
			if keywordDayNight(face) == DayNightDay {
				state = DayNightDay
				break
			}
		}
		if state == "" {
			for _, face := range faces {
				if keywordDayNight(face) == DayNightNight {
					state = DayNightNight
					break
				}
			}
		}
	}
	f.DayNight = state
	return ""
}
