// Package cov is the xengine coverage ledger: which cards, and which of their
// abilities, a cross-engine sweep exercised in a gorge game and compared in a
// lowered scenario every follower replayed.
package cov

import (
	"fmt"
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
)

// Key is one coverage key of a card. ID is stable for a given corpus pin:
// "cast", "land", or "f<face>/a<i>" / "f<face>/t<i>" indexing the compiled
// Face.Abilities / Face.Triggers slices. Desc is a human label (the API or
// trigger mode). Mana marks a mana ability.
type Key struct {
	ID, Desc string
	Mana     bool
}

// Keys is the card's full key set: "cast" (or "land" for a land, judged on the
// front face) followed by Inventory(c).
func Keys(c *cards.Card) []Key {
	id, desc := "cast", "cast"
	if len(c.Faces) > 0 && c.Faces[0] != nil && hasType(c.Faces[0].Types, "Land") {
		id, desc = "land", "land"
	}
	return append([]Key{{ID: id, Desc: desc}}, Inventory(c)...)
}

func hasType(types []string, t string) bool {
	for _, x := range types {
		if x == t {
			return true
		}
	}
	return false
}

func isManaAPI(api string) bool { return api == "Mana" || api == "ManaReflected" }

// Inventory lists c's usable abilities in face, then abilities-before-triggers,
// then index order: every activated ability (an AB entry of Face.Abilities,
// mana abilities included) and every non-bookkeeping triggered ability, on
// every face. Keyword-expanded abilities count (the loader compiles Cycling,
// Equip, Ward ... into ordinary entries). The spell ability, statics,
// replacements and unexpanded keywords are not keys.
//
// Port of abilityInventory in gorge cmd/cardfuzz/abilities.go (package main,
// not importable) at gorge pin 141c85d7edd1. Delete when gorge exports it.
func Inventory(c *cards.Card) []Key {
	var out []Key
	for fi, f := range c.Faces {
		if f == nil {
			continue
		}
		for i, a := range f.Abilities {
			if a == nil || a.Kind != "AB" {
				continue
			}
			d := a.API
			if kw := a.Params["Keyword"]; kw != "" {
				d += "/" + kw
			}
			out = append(out, Key{ID: fmt.Sprintf("f%d/a%d", fi, i), Desc: d, Mana: isManaAPI(a.API)})
		}
		for i, t := range f.Triggers {
			if t.Effect == nil || bookkeepingTrigger(t) {
				continue
			}
			d := t.Mode
			if kw := t.Params["Keyword"]; kw != "" {
				d += "/" + kw
			}
			out = append(out, Key{ID: fmt.Sprintf("f%d/t%d", fi, i), Desc: d})
		}
	}
	return out
}

// bookkeepingTrigger reports a script's internal state-tracking trigger: a
// Static$ True line with no TriggerDescription$ (the Forget/Cleanup riders
// that tidy a Remembered card). It is no printed ability. A Static$ True
// TapsForMana trigger is a real triggered mana ability and stays.
//
// Port of bookkeepingTrigger in gorge cmd/cardfuzz/abilities.go at gorge pin
// 141c85d7edd1.
func bookkeepingTrigger(t cards.Trigger) bool {
	return strings.EqualFold(strings.TrimSpace(t.Params["Static"]), "True") &&
		strings.TrimSpace(t.Params["TriggerDescription"]) == "" && t.Mode != "TapsForMana"
}
