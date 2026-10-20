package mtgish

import (
	"sync"

	"github.com/adams-shaun/gorge/engine/cards"
)

// TypeIndex maps a subtype word (typeWord spelling) to the card type it
// belongs to ("goblin" -> "creature", "food" -> "artifact", "gate" ->
// "land"). A script filter names a subtype bare ("Goblin.YouCtrl") where
// mtgish names its kind (IsCreatureType); the index lets both reduce to the
// same target class.
type TypeIndex map[string]string

// subtypeOwners ranks the card types a subtype is attributed to when it is
// printed on faces of several (a Kindred card's creature type).
var subtypeOwners = []string{"creature", "artifact", "land", "enchantment", "planeswalker", "battle"}

var nonSubtypes = map[string]bool{
	"legendary": true, "basic": true, "snow": true, "world": true, "ongoing": true,
	"creature": true, "artifact": true, "enchantment": true, "land": true,
	"planeswalker": true, "instant": true, "sorcery": true, "battle": true,
	"kindred": true, "tribal": true,
}

var typeIndexes struct {
	sync.Mutex
	m map[*cards.Registry]TypeIndex
}

// TypeIndexFor builds (once per registry) the subtype index from every
// printed face in the corpus.
func TypeIndexFor(reg *cards.Registry) TypeIndex {
	typeIndexes.Lock()
	defer typeIndexes.Unlock()
	if idx, ok := typeIndexes.m[reg]; ok {
		return idx
	}
	// votes[subtype][type] counts the faces printing the subtype with the
	// type; the most printed wins (Equipment is an artifact type even
	// though a few Kindred Equipment exist).
	votes := map[string]map[string]int{}
	add := func(f *cards.Face) {
		owners := map[string]bool{}
		for _, t := range f.Types {
			w := typeWord(t)
			if w == "kindred" || w == "tribal" {
				owners["creature"] = true
			} else if permanentTypes[w] {
				owners[w] = true
			}
		}
		for _, t := range f.Types {
			w := typeWord(t)
			if nonSubtypes[w] {
				continue
			}
			if votes[w] == nil {
				votes[w] = map[string]int{}
			}
			for o := range owners {
				votes[w][o]++
			}
		}
	}
	for _, c := range reg.AllCards() {
		for _, f := range c.Faces {
			if f != nil {
				add(f)
			}
		}
	}
	for _, c := range reg.Tokens {
		for _, f := range c.Faces {
			if f != nil {
				add(f)
			}
		}
	}
	idx := TypeIndex{}
	for w, vs := range votes {
		best, bestN := "", 0
		for _, o := range subtypeOwners {
			if vs[o] > bestN {
				best, bestN = o, vs[o]
			}
		}
		if best != "" {
			idx[w] = best
		}
	}
	if typeIndexes.m == nil {
		typeIndexes.m = map[*cards.Registry]TypeIndex{}
	}
	typeIndexes.m[reg] = idx
	return idx
}
