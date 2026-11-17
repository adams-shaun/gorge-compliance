package main

import (
	"fmt"
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
)

// anyScopePTConflict refuses a second assertPowerToughness on one name that
// states a different P/T while the seat has two or more cards of that name.
// XMage's default ComparisonScope is Any: "some permanent named X is 2/2" and
// "some permanent named X is 3/3" both hold when the copies differ. The ref
// of an assert names one copy (the first), so the second would be judged
// against the wrong object, and which copy each assert means is the test's
// own knowledge, not the translator's.
func (t *translator) anyScopePTConflict(p int, name, pt string) *refusal {
	if len(t.anyRefs(p, name)) < 2 {
		return nil
	}
	key := fmt.Sprintf("%d|%s", p, name)
	if prev, ok := t.ptAsserted[key]; ok && prev != pt {
		return refuse("any-scope-pt-conflict", "%s asserted %s and %s with %d copies in the setup", name, prev, pt, len(t.anyRefs(p, name)))
	}
	if t.ptAsserted == nil {
		t.ptAsserted = map[string]string{}
	}
	t.ptAsserted[key] = pt
	return nil
}

// tokenCopyAssertRefusal refuses a battlefield assert on a card the test only
// brings back with Eternalize or Embalm. The permanent that results is a
// token copy, whose ref is p:token:Name; the assert's plain name binds the
// card left in the graveyard or exile, so the assert would judge the wrong
// object.
func (t *translator) tokenCopyAssertRefusal(p int, name string) *refusal {
	for _, a := range t.actions {
		if a.kind != "activate" {
			continue
		}
		if ab := strings.ToLower(a.ability); strings.HasPrefix(ab, "eternalize") || strings.HasPrefix(ab, "embalm") {
			if t.allOffBattlefield(p, name) {
				return refuse("token-copy-assert", "%s is only a token copy after %s", name, a.ability)
			}
		}
	}
	return nil
}

// altCastHeads are the keyword heads whose cast is offered beside the plain
// cast: the alternative-cost keywords and the face-down family.
var altCastHeads = map[string]bool{"Morph": true, "Megamorph": true, "Disguise": true}

// printsAltCast reports whether the card prints a cast option other than the
// plain one.
func printsAltCast(c *cards.Card) bool {
	for _, f := range c.Faces {
		if f == nil {
			continue
		}
		for _, k := range f.Keywords {
			head, _, _ := strings.Cut(k, ":")
			if altCostKeyword[head] || altCastHeads[head] {
				return true
			}
		}
	}
	return false
}

// landPool is one mana per land source (its first colour): what XMage's
// checkPlayableAbility counts when it asks whether a cast is payable.
func landPool(srcs [][]byte) string {
	var b []byte
	for _, s := range srcs {
		if len(s) > 0 {
			b = append(b, s[0])
		}
	}
	return string(b)
}

// preparedCopyRef is the ref of the object a prepare spell's cast option is
// on. A creature with the prepared designation casts a COPY of its prepare
// spell from exile (CR 722.3c), and that copy, not the creature, is the cast
// option's object; the copy is named by the spell's face. It exists only
// once the creature has entered prepared, so a check before that, or one
// that the copy must be absent (want false), keeps the card's own ref: a ref
// to an object that does not exist fails the run.
func (t *translator) preparedCopyRef(p, src int, ref, face string) string {
	i := strings.IndexByte(ref, ':')
	if i < 0 {
		return ref
	}
	name := ref[i+1:]
	if j := strings.LastIndexByte(name, '#'); j >= 0 {
		name = name[:j]
	}
	c, ok := t.lookup(name)
	if !ok || c.AlternateMode != "Prepare" || len(c.Faces) < 2 || c.Faces[1] == nil || !sameName(c.Faces[1].Name, face) {
		return ref
	}
	for _, a := range t.actions {
		// the copy exists once the creature has been cast (or starts on the
		// battlefield); before that the check is about the card in hand
		if a.seat == p && (a.kind == "cast" || a.kind == "play") && a.src < src && sameName(a.card, name) {
			return fmt.Sprintf("p%d:%s", p, c.Faces[1].Name)
		}
	}
	if t.placeZone(p, ref) == "battlefield" {
		return fmt.Sprintf("p%d:%s", p, c.Faces[1].Name)
	}
	return ref
}
