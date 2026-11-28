package scen

import (
	"regexp"
	"slices"
	"strconv"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/certdeck"
	"github.com/adams-shaun/gorge/engine/cards"
)

// replKeyRE matches a replacement key of cov.Universe: repl#<face>.<index>,
// the index into the face's Repls.
var replKeyRE = regexp.MustCompile(`^repl#(\d+)\.(\d+)$`)

// selfEntryTap reports a replacement that taps its own card as it enters the
// battlefield: Event "Moved", Params["ValidCard"] == "Card.Self",
// Params["Destination"] == "Battlefield", With != nil, With.API == "Tap",
// With.Params["ETB"] == "True" and With.Params["Defined"] in {"Self",
// "ReplacedCard"}. Condition parameters on With (ConditionPresent$ ...) are
// allowed: the start exercises the replacement whichever way the condition
// reads.
func selfEntryTap(r *cards.Repl) bool {
	if r == nil || r.Event != "Moved" || r.Params["ValidCard"] != "Card.Self" || r.Params["Destination"] != "Battlefield" {
		return false
	}
	w := r.With
	if w == nil || w.API != "Tap" || w.Params["ETB"] != "True" {
		return false
	}
	return w.Params["Defined"] == "Self" || w.Params["Defined"] == "ReplacedCard"
}

// replKeyStart fills f for a replacement key of c; "" when built, else the
// refusal reason. Only a front face's self-entry tap replacement (the
// enter-tapped lands and creatures) is built: a land is played from hand (the
// land family's placement), any other card is cast (castStart). The
// cross-engine comparison then reads the card's tapped state after it enters
// (OracleSnapshot's tapped); no Compare field is needed. Every other
// replacement is refused by its event, so the census buckets the remaining
// work by event.
func replKeyStart(reg *cards.Registry, b *certdeck.Builder, c *cards.Card, card, key, generic string, f *StartFile) string {
	m := replKeyRE.FindStringSubmatch(key)
	if m == nil {
		return "no-generator:repl"
	}
	fi, _ := strconv.Atoi(m[1])
	idx, _ := strconv.Atoi(m[2])
	if fi >= len(c.Faces) || c.Faces[fi] == nil || idx >= len(c.Faces[fi].Repls) {
		return "no-generator:repl"
	}
	if fi > 0 {
		return "no-generator:repl:back-face"
	}
	face := c.Faces[fi]
	r := &face.Repls[idx]
	if !selfEntryTap(r) {
		return "no-generator:repl:" + r.Event
	}
	if r.With.Params["UnlessCost"] != "" {
		// a shock land asks its controller whether to pay
		return "no-generator:repl:unless-cost"
	}
	if slices.Contains(face.Types, "Land") {
		f.Setup.P0.Hand = []string{card}
		f.Focus = &StartFocus{Seat: seat(0), Card: card, Kind: "land"}
		return ""
	}
	return castStart(reg, b, face, card, generic, f)
}
