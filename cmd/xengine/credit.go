package main

import (
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/classify"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/scen"
	"github.com/adams-shaun/gorge/engine/cards"
)

// canonicalCard is name as the coverage ledger keys it: the card's front-face
// name, which is what startDeck deals (a start may name a double-faced card by
// its joined "A // B" spelling, or by its back face). classify.Uses credits the
// tape's front-face names, so a declared key built from the file's spelling
// would be a second, invisible key for the same card. A name the corpus does
// not know passes through: the caller's later lookup refuses or normalises it.
func canonicalCard(reg *cards.Registry, name string) string {
	if c, ok := reg.Lookup(name); ok && len(c.Faces) > 0 && c.Faces[0] != nil {
		return c.Faces[0].Name
	}
	return name
}

// declaredKey is the coverage key an item's id says the item was built for:
// xs:<set>:<slug>:<keyid>:<ordinal> (scen.StartID, scen.StepsID). The card is
// the item's own Card, canonicalised to the corpus front-face spelling; an item
// of another id family, or one whose producer did not name its card, declares
// nothing.
//
// This is the credit source a tape cannot supply: classify.Uses reads entry
// Picks, Mana and Triggers, so it sees cast, land and f<face>/a|t keys but
// never a level-B static#F.S or combat#F.attack|block key -- no tape entry
// names those. The id names any key family the item can be built for, so a
// family added to scen's ids is credited without touching a list here.
func declaredKey(reg *cards.Registry, it oraclegen.Item) (classify.Use, bool) {
	_, _, key, _, ok := scen.ParseID(it.ID)
	if !ok || it.Card == "" {
		return classify.Use{}, false
	}
	return classify.Use{Card: canonicalCard(reg, it.Card), Key: key}, true
}

// creditDeclared is whether credit joins declaredKey. It is always true outside
// a test, which clears it to read what the tape alone proves a start played.
var creditDeclared = true

// credit is every coverage key item credits over the tape's entries lo..hi:
// classify.Uses, plus the key the item's id declares (declaredKey) when the
// tape has not already credited it. Counts are not inflated: the declared key
// joins at 1 only when absent. Every credited card is the corpus front-face
// name, so a joined "A // B" start cannot credit a second spelling of a key
// the tape already credited.
func credit(reg *cards.Registry, it oraclegen.Item, t *game.Tape, lo, hi int, genesis bool) map[classify.Use]int {
	out := classify.Uses(reg, t, lo, hi, genesis)
	if !creditDeclared {
		return out
	}
	if u, ok := declaredKey(reg, it); ok {
		if _, seen := out[u]; !seen {
			out[u] = 1
		}
	}
	return out
}
