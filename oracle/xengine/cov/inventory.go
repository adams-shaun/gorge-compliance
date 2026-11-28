// Package cov is the xengine coverage ledger: which cards, and which of their
// abilities, a cross-engine sweep exercised in a gorge game and compared in a
// lowered scenario every follower replayed.
package cov

import (
	"fmt"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/levelb"
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

// Family is the key family of a Universe key id: one of "cast", "land",
// "activated", "trigger", "mode", "static", "repl", "keyword" or "combat".
// It returns "" for an id outside the key universe.
func Family(id string) string {
	switch {
	case id == "cast" || id == "land":
		return id
	case strings.HasPrefix(id, "mode#"):
		return "mode"
	case strings.HasPrefix(id, "static#"):
		return "static"
	case strings.HasPrefix(id, "repl#"):
		return "repl"
	case strings.HasPrefix(id, "kw#"):
		return "keyword"
	case strings.HasPrefix(id, "combat#"):
		return "combat"
	}
	if strings.HasPrefix(id, "f") {
		if i := strings.IndexByte(id, '/'); i >= 0 {
			switch {
			case strings.HasPrefix(id[i:], "/a"):
				return "activated"
			case strings.HasPrefix(id[i:], "/t"):
				return "trigger"
			}
		}
	}
	return ""
}

// Universe is the card's full key set: Keys(c) plus spell modes, statics,
// replacement effects, plain keywords and combat requirements. Faces are
// visited in order inside each group; a basic land returns only Keys(c), as
// level B gives it no requirements.
func Universe(c *cards.Card) []Key {
	if c == nil {
		return nil
	}
	out := Keys(c)
	if len(c.Faces) == 0 || isBasicLand(c) {
		return out
	}
	// Spell modes: a modal spell ability's choices, in script order.
	for fi, f := range c.Faces {
		if f == nil {
			continue
		}
		for ai, a := range f.Abilities {
			if a == nil || a.Kind != "SP" || a.API != "Charm" {
				continue
			}
			choices := strings.TrimSpace(a.Params["Choices"])
			if choices == "" {
				continue
			}
			for mi, choice := range strings.Split(choices, ",") {
				out = append(out, Key{
					ID:   fmt.Sprintf("mode#%d.%d.%d", fi, ai, mi),
					Desc: "Charm/" + strings.TrimSpace(choice),
				})
			}
		}
	}
	// Statics come from level B so the AIHintOnlyStatic skip and the
	// static#F.I ids stay one rule.
	reqs := levelb.Requirements(c)
	for _, r := range reqs {
		if r.Family == "static" {
			out = append(out, Key{ID: r.Key, Desc: r.Sub})
		}
	}
	// Replacement effects, in face then IR order.
	for fi, f := range c.Faces {
		if f == nil {
			continue
		}
		for i, r := range f.Repls {
			d := r.Event
			if kw := r.Params["Keyword"]; kw != "" {
				d += "/" + kw
			}
			out = append(out, Key{ID: fmt.Sprintf("repl#%d.%d", fi, i), Desc: d})
		}
	}
	// Plain keywords: not expanded into another entry on the face and not a
	// level-B combat keyword (combat requirements carry those).
	for fi, f := range c.Faces {
		if f == nil {
			continue
		}
		for i, kw := range f.Keywords {
			name := strings.TrimSpace(kw)
			if j := strings.IndexByte(name, ':'); j >= 0 {
				name = strings.TrimSpace(name[:j])
			}
			if name == "" || levelb.IsCombatKeyword(name) || keywordExpanded(f, name) {
				continue
			}
			out = append(out, Key{ID: fmt.Sprintf("kw#%d.%d", fi, i), Desc: name})
		}
	}
	for _, r := range reqs {
		if r.Family == "combat" {
			out = append(out, Key{ID: r.Key, Desc: r.Sub})
		}
	}
	return out
}

// keywordExpanded reports whether the loader turned a printed keyword into an
// ability, trigger, static or replacement on the same face: the compiled entry
// carries the printed keyword's name in Params["Keyword"].
func keywordExpanded(f *cards.Face, name string) bool {
	for _, a := range f.Abilities {
		if a != nil && a.Params["Keyword"] == name {
			return true
		}
	}
	for i := range f.Triggers {
		if f.Triggers[i].Params["Keyword"] == name {
			return true
		}
	}
	for i := range f.Statics {
		if f.Statics[i].Params["Keyword"] == name {
			return true
		}
	}
	for i := range f.Repls {
		if f.Repls[i].Params["Keyword"] == name {
			return true
		}
	}
	return false
}

// isBasicLand mirrors levelb's basic-land rule, which is unexported there.
func isBasicLand(c *cards.Card) bool {
	if c == nil || len(c.Faces) == 0 || c.Faces[0] == nil {
		return false
	}
	basic, land := false, false
	for _, t := range c.Faces[0].Types {
		basic = basic || strings.EqualFold(t, "Basic")
		land = land || strings.EqualFold(t, "Land")
	}
	return basic && land
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
