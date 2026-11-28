// Package scen turns one owed ability key (a card and a cov key) into a
// scenario: a scripted-steps item (the level-B shape every follower replays)
// where a level-B template serves the key, else an xproof-start-1 file
// (`xengine play -start`), else a named refusal. Generator reach, the share of
// owed keys some recipe can set up, is xproof's first throughput limit; Census
// measures it.
package scen

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/levelb"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen/templates"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/certdeck"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/cov"
	"github.com/adams-shaun/gorge/engine/cards"
)

// Result paths.
const (
	PathSteps   = "steps"
	PathStart   = "start"
	PathRefused = "refused"
)

// Options tunes Generate.
type Options struct {
	Prefer string // "auto" (default) or "start"
	Set    string // names ids; default "SCEN"
}

// Result is what Generate built: exactly one of Steps, Start or a Reason.
type Result struct {
	Path   string          // "steps", "start" or "refused"
	Reason string          // refusal reason
	Start  *StartFile      // the xproof-start-1 document
	Steps  *oraclegen.Item // the scripted-steps item
}

func refuse(reason string) (Result, error) { return Result{Path: PathRefused, Reason: reason}, nil }

var keyRE = regexp.MustCompile(`^f(\d+)/([at])(\d+)$`)

// historyTokens are the parameter values placement cannot fake: the game state
// they read is history, not position. Earlier entries win (historyToken), so a
// narrower cast history (cast from a zone, cast web-slinging) is named before
// the plain "wascast" it contains.
var historyTokens = []string{"kicked", "castfrom", "websling", "wascast", "thisturnentered", "enteredthisturn"}

// castHistory are the history tokens the start's own line produces: the focus
// casting the card itself from hand makes it "cast" (wascast, wasCastByYou),
// and casting it kicked makes it "kicked" (recipe checks the focus is that cast).
var castHistory = map[string]bool{"wascast": true, "kicked": true}

// Keys is every key Generate accepts for c: cov.Keys plus the level-B
// static and combat requirement keys, then the replacement keys (repl#F.I) of
// cov.Universe.
func Keys(c *cards.Card) []string {
	var out []string
	for _, k := range cov.Keys(c) {
		out = append(out, k.ID)
	}
	for _, r := range levelb.Requirements(c) {
		if r.Family == "static" || r.Family == "combat" {
			out = append(out, r.Key)
		}
	}
	for _, k := range cov.Universe(c) {
		if cov.Family(k.ID) == "repl" {
			out = append(out, k.ID)
		}
	}
	return out
}

// Family names the recipe family of key: cast, land, activate, trigger,
// static, combat or repl.
func Family(key string) string {
	switch {
	case key == "cast" || key == "land":
		return key
	case strings.HasPrefix(key, "static#"):
		return "static"
	case strings.HasPrefix(key, "combat#"):
		return "combat"
	case strings.HasPrefix(key, "repl#"):
		return "repl"
	}
	if m := keyRE.FindStringSubmatch(key); m != nil {
		if m[2] == "a" {
			return "activate"
		}
		return "trigger"
	}
	return "?"
}

// Generate builds the scenario that exercises key of card.
func Generate(reg *cards.Registry, b *certdeck.Builder, card, key string, opt Options) (Result, error) {
	if opt.Prefer == "" {
		opt.Prefer = "auto"
	}
	if opt.Prefer != "auto" && opt.Prefer != "start" {
		return Result{}, fmt.Errorf("scen: prefer %q, want auto or start", opt.Prefer)
	}
	if opt.Set == "" {
		opt.Set = "SCEN"
	}
	c, ok := reg.Lookup(card)
	if !ok || len(c.Faces) == 0 {
		return Result{}, fmt.Errorf("scen: %q is not in the corpus", card)
	}
	// A keyword or spell-mode key of the universe is an owed row with no
	// generator yet: a named refusal, so the proof loop counts it.
	if fam := cov.Family(key); fam == "keyword" || fam == "mode" {
		for _, k := range cov.Universe(c) {
			if k.ID == key {
				return refuse("no-generator:" + fam)
			}
		}
	}
	known := false
	for _, k := range Keys(c) {
		if k == key {
			known = true
		}
	}
	if !known {
		return Result{}, fmt.Errorf("scen: %q has no key %q", card, key)
	}
	// A Forge main face whose Oracle is the `<Unsupported Variant>` placeholder
	// (Un-cards such as Garbage Elemental: the real card is one of the Variant:
	// faces gorge never parses) is an empty shell gorge plays as a 0/0 that
	// dies to state-based actions, while Forge lacks the card and XMage plays a
	// variant: no engine comparison is meaningful. Refuse rather than generate.
	for _, f := range c.Faces {
		if strings.Contains(f.Oracle, "<Unsupported Variant>") {
			return refuse("variant_only")
		}
	}
	// A card XMage does not list is still generated: Forge is the reference
	// oracle (operator 11-24), the XMage leg marks the item not_sent, and
	// keyproof credits a Forge agreement with XMage silent (refs F). Refusing
	// it here owed about a fifth of every xproof cycle's keys.
	fam := Family(key)
	m := keyRE.FindStringSubmatch(key)
	var face *cards.Face
	var params map[string]string
	var sa *cards.SA
	var trig *cards.Trigger
	if m != nil {
		fi, _ := strconv.Atoi(m[1])
		idx, _ := strconv.Atoi(m[3])
		if fi >= len(c.Faces) {
			return Result{}, fmt.Errorf("scen: %q key %q: no face %d", card, key, fi)
		}
		face = c.Faces[fi]
		if m[2] == "a" {
			if idx >= len(face.Abilities) {
				return Result{}, fmt.Errorf("scen: %q key %q: no such ability", card, key)
			}
			sa = face.Abilities[idx]
			params = sa.Params
		} else {
			if idx >= len(face.Triggers) {
				return Result{}, fmt.Errorf("scen: %q key %q: no such trigger", card, key)
			}
			trig = &face.Triggers[idx]
			params = trig.Params
		}
	}
	hist := historyToken(params)
	if hist != "" && !castHistory[hist] {
		return refuse("history:" + hist)
	}
	if opt.Prefer == "auto" && hist == "" {
		if it, ok := steps(reg, b, c, card, key, m, opt); ok {
			return Result{Path: PathSteps, Steps: it}, nil
		}
	}
	return recipe(reg, b, c, card, key, fam, m, sa, trig, hist, opt)
}

func historyToken(params map[string]string) string {
	for _, tok := range historyTokens {
		for _, v := range params {
			if strings.Contains(strings.ToLower(v), tok) {
				return tok
			}
		}
	}
	return ""
}

// reqKey is the level-B requirement key of a cov key ("" for cast and land).
func reqKey(key string, m []string) string {
	switch {
	case m != nil && m[2] == "a":
		return "activate#" + m[1] + "." + m[3]
	case m != nil:
		return "trigger#" + m[1] + "." + m[3]
	case strings.HasPrefix(key, "static#") || strings.HasPrefix(key, "combat#"):
		return key
	}
	return ""
}

// steps is the template path: the level-B requirement serving key, when a
// template builds it and level A does not already settle it.
func steps(reg *cards.Registry, b *certdeck.Builder, c *cards.Card, card, key string, m []string, opt Options) (*oraclegen.Item, bool) {
	rk := reqKey(key, m)
	if rk == "" {
		return nil, false
	}
	for _, req := range levelb.Requirements(c) {
		if req.Key != rk {
			continue
		}
		if req.Gap != "" || req.CoveredByA {
			return nil, false
		}
		it, skip := templates.GenerateB(reg, card, req)
		if skip != nil {
			return nil, false
		}
		it.ID = StepsID(opt.Set, card, key)
		if xm, ok := b.Sp.XMage(reg, card); ok && xm != card && it.XMageName == "" {
			it.XMageName = xm
		}
		return &it, true
	}
	return nil, false
}
