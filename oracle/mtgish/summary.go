package mtgish

import (
	"sort"
	"strconv"
	"strings"
)

// Summary is the one common shape both normalizers produce for a card face.
// Every field is made of short canonical TOKENS (never Oracle or script
// text), so a summary and a diff of two summaries can be committed.
//
// Anything a normalizer cannot map becomes an explicit "unmapped:<name>"
// token in the field it would have filled; the differ counts a field whose
// difference involves one as a normalizer gap, never as a disagreement, and
// never drops it.
type Summary struct {
	Card string // the card's lookup name
	Face string // this face's printed name

	// T1: characteristics.
	ManaCost string   // "{2}{W}{W}", "" for none
	Types    []string // supertypes, card types and subtypes, lower case, sorted
	PT       string   // "2/3", "*/4", "" for none
	Loyalty  string   // "4", "X", "" for none
	Keywords []string // canonical keyword tokens ("flying", "ward:{2}"), sorted

	// T2-T4: one entry per printed (non-keyword) ability.
	Abilities []Ability
}

// Ability is one printed ability's shape.
type Ability struct {
	Kind    string   // static | triggered | activated | spell | replacement | pregame
	Event   string   // trigger event class, triggered abilities only
	Targets []string // target slot tokens ("creature", "player", "permanentx0..1")
	Effects []string // effect verb tokens ("damage:3", "draw:1", "destroy")
}

// Shape is the T2 token: kind, plus the event class for a trigger.
func (a Ability) Shape() string {
	if a.Event != "" {
		return a.Kind + ":" + a.Event
	}
	return a.Kind
}

// Tier names a comparison tier.
type Tier int

const (
	T1 Tier = 1 + iota
	T2
	T3
	T4
)

// Tiers in order.
var Tiers = []Tier{T1, T2, T3, T4}

func (t Tier) String() string { return "T" + string(rune('0'+int(t))) }

// Field is one compared field of a summary: its tier, its name and its
// tokens (a multiset; scalars are one-token lists).
type Field struct {
	Tier   Tier
	Name   string
	Tokens []string
}

// Fields flattens a summary into its compared fields, in a fixed order.
func (s *Summary) Fields() []Field {
	one := func(v string) []string {
		if v == "" {
			return nil
		}
		return []string{v}
	}
	var shapes, targets, effects []string
	for _, a := range s.Abilities {
		shapes = append(shapes, a.Shape())
		if len(a.Targets) > 0 {
			t := append([]string(nil), a.Targets...)
			sort.Strings(t)
			targets = append(targets, strings.Join(t, "+"))
		}
		effects = append(effects, a.Effects...)
	}
	return []Field{
		{T1, "mana_cost", one(s.ManaCost)},
		{T1, "types", sorted(s.Types)},
		{T1, "pt", one(s.PT)},
		{T1, "loyalty", one(s.Loyalty)},
		{T1, "keywords", sorted(s.Keywords)},
		{T2, "abilities", sorted(shapes)},
		{T3, "targets", sorted(targets)},
		{T4, "effects", sorted(effects)},
	}
}

func sorted(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

// Render prints a summary for triage, one field per line.
func (s *Summary) Render() string {
	var b strings.Builder
	b.WriteString(s.Face + "\n")
	for _, f := range s.Fields() {
		b.WriteString("  " + f.Tier.String() + " " + f.Name + ": " + joinTokens(f.Tokens) + "\n")
	}
	for i, a := range s.Abilities {
		b.WriteString("  ability " + strconv.Itoa(i) + ": " + a.Shape())
		if len(a.Targets) > 0 {
			b.WriteString("  targets=" + strings.Join(sorted(a.Targets), "+"))
		}
		if len(a.Effects) > 0 {
			b.WriteString("  effects=" + strings.Join(sorted(a.Effects), ","))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// joinTokens renders a token multiset; "-" stands for the empty set.
func joinTokens(t []string) string {
	if len(t) == 0 {
		return "-"
	}
	return strings.Join(t, " ")
}

// unmapped is the explicit gap token.
func unmapped(name string) string { return "unmapped:" + name }

// IsUnmapped reports whether tok is a gap token.
func IsUnmapped(tok string) bool { return strings.HasPrefix(tok, "unmapped:") }
