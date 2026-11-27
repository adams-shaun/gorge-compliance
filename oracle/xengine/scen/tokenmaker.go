package scen

import (
	"sort"
	"strings"
	"sync"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/certdeck"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/lower"
	"github.com/adams-shaun/gorge/engine/cards"
)

// A token maker is the card a start casts when the trigger's filter demands a
// token entering (Anointer Priest's "a creature token you control enters"): a
// one-token instant or sorcery, so the token enters as the focus play's own
// event. A two-token maker is not used: a ChangesZoneAll trigger fires once
// per token in gorge, and a trigger-order ask puts neither on the stack.
type tokenMaker struct {
	name string
	face *cards.Face // the token the maker mints
}

// tokenMakers holds each registry's ranked one-token makers, built once.
var tokenMakers sync.Map // *cards.Registry -> []tokenMaker

// tokenMakerPool is every one-token instant or sorcery in the corpus, with
// the token's face, sorted by name: deterministic and small enough to test
// the trigger's own filter against each token.
func tokenMakerPool(reg *cards.Registry, b *certdeck.Builder) []tokenMaker {
	if v, ok := tokenMakers.Load(reg); ok {
		return v.([]tokenMaker)
	}
	var out []tokenMaker
	for _, c := range reg.AllCards() {
		if len(c.Faces) != 1 || !b.Playable(c) {
			continue
		}
		f := c.Faces[0]
		if f.IsPermanent() || f.ManaCost == "" || f.ManaCost == "no cost" {
			continue
		}
		var tok *cards.SA
		for i := range f.Abilities {
			a := f.Abilities[i]
			if a.Kind != "SP" || a.API != "Token" {
				continue
			}
			if tok != nil {
				tok = nil // more than one token ability: not one event
				break
			}
			tok = a
		}
		if tok == nil || tok.Sub != nil || tok.Params["ValidTgts"] != "" ||
			tok.Params["Condition"] != "" || tok.Params["UnlessCost"] != "" ||
			tok.Params["SubAbility"] != "" {
			continue
		}
		script := tok.Params["TokenScript"]
		if script == "" || strings.Contains(script, ",") {
			continue
		}
		if amount := tok.Params["TokenAmount"]; amount != "" && amount != "1" {
			continue
		}
		if owner := tok.Params["TokenOwner"]; owner != "" && owner != "You" && owner != "Controller" {
			continue
		}
		tc, ok := reg.Tokens[script]
		if !ok || len(tc.Faces) != 1 {
			continue
		}
		mana, why := costTokens(f.ManaCost)
		if why != "" {
			continue
		}
		simple := true
		for _, t := range mana {
			if t == "X" || t == "S" || t == "C" {
				simple = false
			}
		}
		if !simple {
			continue
		}
		out = append(out, tokenMaker{name: f.Name, face: tc.Faces[0]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	tokenMakers.Store(reg, out)
	return out
}

// focusCastsToken reports whether the start's focus is the token maker the
// trigger's own filter chose (tokenCarrier): a token need Derive reads off the
// card is then met by that cast. Any other token spell is not the selected
// maker, and skipping its need would drop a real placement.
func focusCastsToken(reg *cards.Registry, b *certdeck.Builder, f *StartFile, card, filter string) bool {
	if f.Focus == nil || f.Focus.Kind != "cast" {
		return false
	}
	name, _ := tokenCarrier(reg, b, card, filter)
	return name != "" && name == f.Focus.Card
}

// tokenCarrier is the first one-token maker whose token the filter takes, and
// the seat that must cast it: the seat the filter's controller names, with the
// maker instant-speed when that is the opponent (it casts on the card's
// controller's turn).
func tokenCarrier(reg *cards.Registry, b *certdeck.Builder, card, filter string) (name string, seat int) {
	for _, a := range parseFilter(filter) {
		if !a.known || a.self || a.token != 1 {
			continue
		}
		for _, tm := range tokenMakerPool(reg, b) {
			if tm.name == card || !a.matches(tm.face, true) {
				continue
			}
			if a.ctrl == ctrlOpp {
				c, ok := reg.Lookup(tm.name)
				if !ok || len(c.Faces) == 0 || !c.Faces[0].IsInstant() && !c.Faces[0].HasKeyword("Flash") {
					continue
				}
				return tm.name, 1
			}
			return tm.name, 0
		}
	}
	return "", 0
}

// pickTargetToken is the token script a "target token" filter (Replicator's
// "target token you control", Dogged Hunter's "target creature token") is met
// by: the corpus's inert Servo when the filter takes it, else the first sorted
// script the filter takes, that XMage's setup token table can also replay
// (lower.SetupTokenScript), and whose token survives setup. Only a token the
// filter takes is minted; a token filter no script meets is no placement, not
// a card standing in for a token.
func pickTargetToken(reg *cards.Registry, a filterAlt, tapped bool) (string, bool) {
	matches := func(k string) bool {
		if !lower.SetupTokenScript(k) {
			return false
		}
		c := reg.Tokens[k]
		if c == nil || len(c.Faces) == 0 {
			return false
		}
		f := c.Faces[0]
		return !deadOnArrival(f) && a.matchesState(f, true, tapped)
	}
	if matches(genericToken) {
		return genericToken, true
	}
	keys := make([]string, 0, len(reg.Tokens))
	for k := range reg.Tokens {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if matches(k) {
			return k, true
		}
	}
	return "", false
}
