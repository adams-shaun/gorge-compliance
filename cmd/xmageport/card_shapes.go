package main

import (
	"slices"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
)

// removeCardTypesSubtypeRefusal covers a contested expectation, not a proven
// gorge bug: a card whose effect both adds a type/subtype pair and removes
// card types (Vraska, the Silencer's "It's a Treasure artifact ... and it
// loses all other card types") sets the artifact subtype set to Treasure
// under CR 205.1a, while the XMage test still expects the old artifact
// subtype (Clue). Until the two readings are reconciled the runner cannot
// state which one the asserts mean, so the failing shape is refused.
func (t *translator) removeCardTypesSubtypeRefusal() *refusal {
	for p := 0; p < 2; p++ {
		for _, ac := range t.setup[p] {
			c, ok := t.lookup(ac.name)
			if !ok {
				continue
			}
			for _, f := range c.Faces {
				if f == nil {
					continue
				}
				names := make([]string, 0, len(f.SVars))
				for name := range f.SVars {
					names = append(names, name)
				}
				sort.Strings(names)
				for _, name := range names {
					st, ok := cards.ParseStaticLine(f.SVars[name])
					if !ok || !strings.EqualFold(st.Params["RemoveCardTypes"], "True") {
						continue
					}
					added := addedSubtypeWords(st.Params["AddType"])
					if len(added) == 0 {
						continue
					}
					for _, c := range t.asserts {
						if c.Name != "assertSubtype" || len(c.Args) < 2 {
							continue
						}
						sub := titleEnum(c.Args[1].s)
						if sub == "" || slices.Contains(added, sub) {
							continue
						}
						return refuse("removecardtypes-subtype-contested", "%s sets the artifact subtype set to %s; the test expects the old subtype %s retained", ac.name, strings.Join(added, "/"), sub)
					}
				}
			}
		}
	}
	return nil
}

// addedSubtypeWords returns the subtype words of an AddType$ value, dropping
// the card-type words that name the type half of the change.
func addedSubtypeWords(s string) []string {
	var out []string
	for _, w := range strings.FieldsFunc(s, func(r rune) bool { return r == '&' || r == ',' || r == '+' }) {
		w = strings.TrimSpace(w)
		switch strings.ToLower(w) {
		case "", "artifact", "creature", "land", "enchantment", "instant", "sorcery", "planeswalker", "battle", "kindred", "tribal":
			continue
		}
		out = append(out, w)
	}
	return out
}
