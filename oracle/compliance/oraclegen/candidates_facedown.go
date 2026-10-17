// A face-down permanent as a target fixture (CR 708.2, 702.168).
//
// A filter naming the faceDown qualifier ("Card.faceDown", Hauntwoods
// Shrieker's SetState; "Creature.faceDown", Nosy Goblin; "Creature.YouCtrl+
// faceDown", Skirk Alarmist) is satisfied only by a permanent that entered
// the battlefield face down. A setup placement is always face up and fires no
// face-down state, so the fixture is a prelude cast: p0 casts a plain
// Disguise probe face down for {3} and resolves it. The target ref is the
// probe's own face name -- the runner resolves a ref by face name, and only
// the snapshot name is blanked (CR 708.2a).
package oraclegen

import (
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
)

// FaceDownCastPool is the family-independent {3} a Morph, Megamorph or
// Disguise card is cast face down for (CR 702.37a/702.168a/702.169a).
const FaceDownCastPool = "CCC"

// DisguiseProbes are the plain Disguise cards a scenario casts face down,
// tried in order: Bolrac-Clan Basher (a plain creature, accepted by any
// creature/permanent filter), then a Vampire Detective and an Elf Crocodile
// Detective so a Detective filter has a candidate. None has a TurnFaceUp
// trigger of its own. This is the one list; the templates' face-up probes
// alias it.
var DisguiseProbes = []string{
	"Bolrac-Clan Basher", "Basilica Stalker", "Undercover Crocodelf",
}

// FaceDownProbe returns the first DisguiseProbes card that is not skip, is in
// the corpus with a printed Disguise cost and is known to XMage, or "".
func FaceDownProbe(reg *cards.Registry, skip string) string {
	for _, cand := range DisguiseProbes {
		if cand == skip || !XMageKnown(cand) {
			continue
		}
		c, ok := reg.Lookup(cand)
		if !ok || len(c.Faces) == 0 {
			continue
		}
		if _, ok := c.Faces[0].KeywordParam("Disguise"); !ok {
			continue
		}
		return cand
	}
	return ""
}

// faceDownCandidates serves a filter that demands a face-down permanent. ok
// is false when the filter names no faceDown component (the caller's other
// branches decide); with ok true a nil slice means the shape fails closed.
//
// Only p0 casts on p0's turn, so the probe is p0's: a filter alternative that
// demands another player's permanent (OppCtrl, YouDontCtrl, OppOwn), and one
// whose base the creature probe cannot be (a Land), is never served.
func faceDownCandidates(reg *cards.Registry, filter string) (cs []cand, ok bool) {
	if !filterHasComponent(filter, "faceDown") {
		return nil, false
	}
	for _, alt := range strings.Split(filter, ",") {
		if !filterHasComponent(alt, "faceDown") || demandsOpponentObject(alt) {
			continue
		}
		switch strings.ToLower(strings.SplitN(strings.TrimSpace(alt), ".", 2)[0]) {
		case "card", "permanent", "creature":
		default:
			continue
		}
		probe := FaceDownProbe(reg, "")
		if probe == "" {
			return nil, true
		}
		return []cand{{
			seat: "p0", zone: "battlefield", card: probe, ref: "p0:" + probe,
			pre: []Step{
				{Op: "cast", Seat: 0, Card: "p0:" + probe, Mana: FaceDownCastPool, CastMode: "disguised"},
				{Op: "resolve"},
			},
		}}, true
	}
	return nil, true
}

// demandsOpponentObject reports an alternative whose controller or owner word
// names a player other than the source's controller.
func demandsOpponentObject(alt string) bool {
	for _, q := range []string{"OppCtrl", "YouDontCtrl", "OppOwn", "YouDontOwn"} {
		if strings.Contains(alt, q) {
			return true
		}
	}
	return false
}
