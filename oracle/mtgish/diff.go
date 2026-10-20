package mtgish

import (
	"sort"
	"strings"
)

// Row is one field on which the two summaries of a face differ: the tokens
// only gorge's side carries and the tokens only mtgish's side carries. It is
// also the ledger's row shape (Class and Note are the ledger's triage).
type Row struct {
	Card   string
	Tier   Tier
	Field  string
	Gorge  string // gorge-only tokens, space-joined; "-" for none
	Mtgish string // mtgish-only tokens, space-joined; "-" for none
	Class  string
	Note   string
}

// Key identifies a row by everything but its triage.
func (r Row) Key() string {
	return r.Card + "\t" + r.Tier.String() + "\t" + r.Field + "\t" + r.Gorge + "\t" + r.Mtgish
}

// Gap reports whether the difference involves an unmapped token: a
// normalizer gap, not a disagreement between the sources.
func (r Row) Gap() bool {
	for _, side := range []string{r.Gorge, r.Mtgish} {
		for _, t := range strings.Fields(side) {
			if IsUnmapped(t) {
				return true
			}
		}
	}
	return false
}

// Classes are the triage classes a ledger row may carry.
var Classes = []string{"forge-bug", "gorge-compile", "mtgish-bug", "normalizer", "equivalent"}

// ValidClass reports whether c is one of Classes.
func ValidClass(c string) bool {
	for _, k := range Classes {
		if c == k {
			return true
		}
	}
	return false
}

// rowCard is the ledger's card column for a face: the card name, with the
// face name appended when they differ (a back face, a split half).
func rowCard(s *Summary) string {
	if s.Face == "" || s.Face == s.Card {
		return s.Card
	}
	return s.Card + " [" + s.Face + "]"
}

// Diff compares two summaries of the same face and returns one row per
// differing field, in field order. An empty result is full agreement.
func Diff(g, m *Summary) []Row {
	gf, mf := g.Fields(), m.Fields()
	var out []Row
	for i := range gf {
		og, om := multisetDiff(gf[i].Tokens, mf[i].Tokens)
		if len(og) == 0 && len(om) == 0 {
			continue
		}
		out = append(out, Row{
			Card: rowCard(g), Tier: gf[i].Tier, Field: gf[i].Name,
			Gorge: joinTokens(og), Mtgish: joinTokens(om),
		})
	}
	return out
}

// MissingFace is the row for a face only one side has.
func MissingFace(card, face string, gorgeHas bool) Row {
	r := Row{Card: card + " [" + face + "]", Tier: T1, Field: "face", Gorge: "-", Mtgish: "-"}
	if gorgeHas {
		r.Gorge = "face"
	} else {
		r.Mtgish = "face"
	}
	return r
}

// multisetDiff returns the tokens of a not matched in b and of b not matched
// in a, each sorted. Exact matches pair first; then a token whose last
// ':'-segment is "*" (a non-literal amount) pairs with any token that
// agrees on everything before it, so "draw:*" matches "draw:2" and
// "counters:*" matches "counters:coin:1", but "draw:1" never matches
// "draw:2".
func multisetDiff(a, b []string) (onlyA, onlyB []string) {
	ra := append([]string(nil), a...)
	rb := append([]string(nil), b...)
	sort.Strings(ra)
	sort.Strings(rb)
	ra, rb = pairOff(ra, rb, func(x, y string) bool { return x == y })
	ra, rb = pairOff(ra, rb, wildMatch)
	return ra, rb
}

// pairOff removes pairs (x in a, y in b) for which eq holds, greedily in
// sorted order, and returns what is left of each.
func pairOff(a, b []string, eq func(x, y string) bool) ([]string, []string) {
	used := make([]bool, len(b))
	var left []string
	for _, x := range a {
		hit := false
		for j, y := range b {
			if !used[j] && eq(x, y) {
				used[j], hit = true, true
				break
			}
		}
		if !hit {
			left = append(left, x)
		}
	}
	var rest []string
	for j, y := range b {
		if !used[j] {
			rest = append(rest, y)
		}
	}
	return left, rest
}

func wildMatch(x, y string) bool {
	if anyOriginMove(x, y) || anyOriginMove(y, x) {
		return true
	}
	px, lx := splitLast(x)
	py, ly := splitLast(y)
	switch {
	case px == "" || py == "":
		return false
	case px == py:
		return lx == "*" || ly == "*"
	case lx == "*" && strings.HasPrefix(y, px+":"):
		// "counters:*" (type and amount unknown) matches "counters:coin:1".
		return true
	case ly == "*" && strings.HasPrefix(x, py+":"):
		return true
	}
	return false
}

func splitLast(t string) (prefix, last string) {
	i := strings.LastIndexByte(t, ':')
	if i < 0 {
		return "", t
	}
	return t[:i], t[i+1:]
}

// anyOriginMove reports whether x is a move from any zone ("move:*>hand")
// and y the same move from a named zone.
func anyOriginMove(x, y string) bool {
	const pre = "move:*>"
	if !strings.HasPrefix(x, pre) || !strings.HasPrefix(y, "move:") {
		return false
	}
	_, dy, ok := strings.Cut(strings.TrimPrefix(y, "move:"), ">")
	return ok && dy == strings.TrimPrefix(x, pre)
}
