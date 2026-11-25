package main

import (
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/classify"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/scen"
	"github.com/adams-shaun/gorge/engine/cards"
)

// declaredKey is the coverage key an item's id says the item was built for:
// xs:<set>:<slug>:<keyid>:<ordinal> (scen.StartID, scen.StepsID). The card is
// the item's own Card; an item of another id family, or one whose producer did
// not name its card, declares nothing.
//
// This is the credit source a tape cannot supply: classify.Uses reads entry
// Picks, Mana and Triggers, so it sees cast, land and f<face>/a|t keys but
// never a level-B static#F.S or combat#F.attack|block key -- no tape entry
// names those. The id names any key family the item can be built for, so a
// family added to scen's ids is credited without touching a list here.
func declaredKey(it oraclegen.Item) (classify.Use, bool) {
	_, _, key, _, ok := scen.ParseID(it.ID)
	if !ok || it.Card == "" {
		return classify.Use{}, false
	}
	return classify.Use{Card: it.Card, Key: key}, true
}

// credit is every coverage key item credits over the tape's entries lo..hi:
// classify.Uses, plus the key the item's id declares (declaredKey) when the
// tape has not already credited it. Counts are not inflated: the declared key
// joins at 1 only when absent.
func credit(reg *cards.Registry, it oraclegen.Item, t *game.Tape, lo, hi int, genesis bool) map[classify.Use]int {
	out := classify.Uses(reg, t, lo, hi, genesis)
	if u, ok := declaredKey(it); ok {
		if _, seen := out[u]; !seen {
			out[u] = 1
		}
	}
	return out
}
