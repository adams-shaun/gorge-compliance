// Package cov is the xengine coverage ledger: which cards, and which of their
// abilities, a cross-engine sweep exercised in a gorge game and compared in a
// lowered scenario every follower replayed.
package cov

import (
	"fmt"
	"slices"
	"strconv"
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
			name := keywordName(kw)
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

// keywordName is a keyword string without its ":"-separated parameters.
func keywordName(kw string) string {
	name, _, _ := strings.Cut(kw, ":")
	return strings.TrimSpace(name)
}

// multiplayerTokens are the case-sensitive substrings that mark a script value
// as reading something that differs with more than two players. Sorted.
var multiplayerTokens = []string{
	"ChoosePlayer", "Commander", "DefendingPlayer", "Goad", "Initiative", "Monarch",
	"OppCtrl", "OppOwn", "Opponent", "Player.Other", "Vote",
}

// multiplayerKeywords are the keywords that mark a key by name. Sorted.
var multiplayerKeywords = []string{"Dethrone", "Encore", "Melee", "Myriad"}

// MultiplayerTokens is every listed token followed by every listed keyword name,
// the list docs/oracles.md prints. The slice is the caller's.
func MultiplayerTokens() []string { return slices.Concat(multiplayerTokens, multiplayerKeywords) }

// Multiplayer reports whether key id of c reads something that differs with more
// than two players. It judges the key's own script text only, never the card's
// name: the effect names and parameter values of the key's IR node and of what
// it executes (a spell mode: the raw SVar its choice names; a keyword: the
// keyword string). A value holding a token of MultiplayerTokens flags the key,
// as does a Defined or ValidPlayers of exactly "Player" (each player) and a
// listed keyword, either unexpanded (a kw# key) or named by the Keyword param
// of the entry the loader expanded it into. A combat# key is never flagged, nor
// is an id the card does not hold.
func Multiplayer(c *cards.Card, id string) bool {
	if c == nil {
		return false
	}
	switch Family(id) {
	case "cast", "land":
		if f := faceAt(c, 0); f != nil {
			for _, a := range f.Abilities {
				if a != nil && a.Kind == "SP" && multiplayerChain(a) {
					return true
				}
			}
		}
	case "activated":
		if f, n, ok := keyEntry(c, strings.TrimPrefix(id, "f"), "/a", 2); ok && n[1] < len(f.Abilities) {
			return multiplayerChain(f.Abilities[n[1]])
		}
	case "trigger":
		if f, n, ok := keyEntry(c, strings.TrimPrefix(id, "f"), "/t", 2); ok && n[1] < len(f.Triggers) {
			t := f.Triggers[n[1]]
			return multiplayerParams(t.Params) || multiplayerChain(t.Effect)
		}
	case "mode":
		f, n, ok := keyEntry(c, strings.TrimPrefix(id, "mode#"), ".", 3)
		if !ok || n[1] >= len(f.Abilities) || f.Abilities[n[1]] == nil {
			return false
		}
		choices := strings.Split(f.Abilities[n[1]].Params["Choices"], ",")
		if n[2] < len(choices) {
			return multiplayerRaw(f.SVars[strings.TrimSpace(choices[n[2]])])
		}
	case "static":
		if f, n, ok := keyEntry(c, strings.TrimPrefix(id, "static#"), ".", 2); ok && n[1] < len(f.Statics) {
			return multiplayerParams(f.Statics[n[1]].Params)
		}
	case "repl":
		if f, n, ok := keyEntry(c, strings.TrimPrefix(id, "repl#"), ".", 2); ok && n[1] < len(f.Repls) {
			r := f.Repls[n[1]]
			return multiplayerParams(r.Params) || multiplayerChain(r.With)
		}
	case "keyword":
		if f, n, ok := keyEntry(c, strings.TrimPrefix(id, "kw#"), ".", 2); ok && n[1] < len(f.Keywords) {
			kw := f.Keywords[n[1]]
			return hasMultiplayerToken(kw) || slices.Contains(multiplayerKeywords, keywordName(kw))
		}
	}
	return false
}

// faceAt is c's face i, or nil when the card has none.
func faceAt(c *cards.Card, i int) *cards.Face {
	if i >= len(c.Faces) {
		return nil
	}
	return c.Faces[i]
}

// keyEntry parses the want sep-separated indices of a key id (its prefix already
// removed), the first of them a face of c. ok is false for a malformed id or a
// face the card does not have; the caller bounds the remaining indices.
func keyEntry(c *cards.Card, s, sep string, want int) (*cards.Face, []int, bool) {
	parts := strings.Split(s, sep)
	if len(parts) != want {
		return nil, nil, false
	}
	n := make([]int, want)
	for i, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil || v < 0 {
			return nil, nil, false
		}
		n[i] = v
	}
	f := faceAt(c, n[0])
	return f, n, f != nil
}

// maxSubChain stops a cyclic Sub walk; it is past any chain the loader links.
const maxSubChain = 64

// multiplayerChain reads sa and its Sub chain. The effect name ("DB$ Goad") is
// a script value too; the loader keeps it in API rather than Params.
func multiplayerChain(sa *cards.SA) bool {
	for d := 0; sa != nil && d < maxSubChain; d, sa = d+1, sa.Sub {
		if hasMultiplayerToken(sa.API) || multiplayerParams(sa.Params) {
			return true
		}
	}
	return false
}

// multiplayerParams reports whether any one parameter flags its key. The result
// does not depend on the map's order.
func multiplayerParams(params map[string]string) bool {
	for name, v := range params {
		if multiplayerParam(name, v) {
			return true
		}
	}
	return false
}

func multiplayerParam(name, v string) bool {
	switch name {
	case "Defined", "ValidPlayers":
		if strings.TrimSpace(v) == "Player" {
			return true
		}
	case "Keyword":
		// An expanded keyword has no kw# key: its entry carries the name here.
		if slices.Contains(multiplayerKeywords, keywordName(v)) {
			return true
		}
	}
	return hasMultiplayerToken(v)
}

// multiplayerRaw judges an unparsed "Key$ value | ..." script body.
func multiplayerRaw(body string) bool {
	if hasMultiplayerToken(body) {
		return true
	}
	for seg := range strings.SplitSeq(body, "|") {
		if name, v, ok := strings.Cut(seg, "$"); ok && multiplayerParam(strings.TrimSpace(name), strings.TrimSpace(v)) {
			return true
		}
	}
	return false
}

func hasMultiplayerToken(v string) bool {
	return slices.ContainsFunc(multiplayerTokens, func(t string) bool { return strings.Contains(v, t) })
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
