package templates

import (
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/levelb"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/cards"
)

// namelessBlockerFilter reports whether a CantBlockBy ValidBlocker$ filter
// is satisfied only by a creature with no name (Pompous Gadabout's
// Creature.noName) or only by a face-down one. No vanilla probe is nameless:
// the only nameless creature a scenario can field is a face-down permanent
// (CR 708.2a: it has no name), which setup never places, so the blocker-filter
// probe pairs cannot split such a filter.
func namelessBlockerFilter(filter string) bool {
	for _, part := range strings.FieldsFunc(filter, func(r rune) bool { return r == '.' || r == '+' || r == ',' }) {
		switch strings.ToLower(strings.TrimSpace(part)) {
		case "noname", "facedown":
			return true
		}
	}
	return false
}

// cantBlockByNamelessItem serves "CARDNAME can't be blocked by creatures that
// don't have a name" (CantBlockBy, ValidBlocker$ Creature.noName): p1 casts
// a plain Disguise probe face down for {3} on its turn (CR 702.168a; a
// face-down permanent has no name, CR 708.2a) beside a named vanilla blocker,
// then p0 attacks on turn 3 with the card and the large probe. At p1's
// declare-blockers decision the face-down creature may block the probe but
// not the card, and the named creature may block the card (CR 509.1b). ok is
// false when the filter is not a nameless one or no face-down probe exists,
// leaving the row to the ordinary probe-pair search's skip.
func cantBlockByNamelessItem(reg *cards.Registry, f *cards.Face, name string, req levelb.Requirement, filter string) (oraclegen.Item, *oraclegen.Skip, bool) {
	const mode = "CantBlockBy"
	if !namelessBlockerFilter(filter) {
		return oraclegen.Item{}, nil, false
	}
	probe := oraclegen.FaceDownProbe(reg, name)
	if probe == "" {
		return oraclegen.Item{}, nil, false
	}
	const named = "Grizzly Bears"
	self, spider := cardAt(0, name), cardAt(0, largeAttackerProbe)
	faceDown, namedRef := cardAt(1, probe), cardAt(1, named)
	steps := []oraclegen.Step{
		{Op: "pass_to", Seat: 1, Step: "main1", Active: "p1"},
		{Op: "cast", Seat: 1, Card: faceDown, Mana: oraclegen.FaceDownCastPool, CastMode: "disguised"},
		{Op: "resolve", Seat: 1},
		{Op: "attack", Seat: 0, Defender: "p1", Attackers: []string{self, spider}},
		{Op: "pass_to", Seat: 0, Decision: "blockers", Expect: []oraclegen.Expect{
			canBlockExpect(faceDown, spider, true),
			canBlockExpect(faceDown, self, false),
			canBlockExpect(namedRef, self, true),
		}},
	}
	sc := withBackFace(staticScenario(f, name, []string{name, largeAttackerProbe}, nil, steps), name, req)
	p1 := sc.Setup["p1"]
	p1.Battlefield = []string{named}
	p1.Hand = appendFixtureUnique(p1.Hand, probe)
	sc.Setup["p1"] = p1
	it, skip := finishLegalityItem(reg, f, name, mode, req, sc, []string{"509.1b", "708.2a"})
	return it, skip, true
}
