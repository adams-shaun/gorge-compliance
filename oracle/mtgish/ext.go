package mtgish

import "github.com/adams-shaun/gorge/engine/cards"

// Extension tables. Each normaliser switch consults its table just before
// it falls back to an unmapped token, so a family of new mappings can live
// in its own file (registered from init) instead of growing the switches.
// A table is only ever looked up by key, never ranged, so map order cannot
// reach a summary.
var (
	// mtgish side.
	extAction       = map[string]func(e *effects, n node){}   // Action variant
	extLayer        = map[string]func(l any) []string{}       // LayerEffect variant
	extEnter        = map[string]func(x any) []string{}       // ReplacementActionWouldEnter variant
	extRule         = map[string]func(m *mtgishNorm, r any){} // Rule variant
	extTriggerClass = map[string]string{}                     // Trigger variant -> event class

	// gorge side.
	extAPI      = map[string]func(a *cards.SA, svars map[string]string) []string{} // SA API
	extEnterAPI = map[string]func(a *cards.SA) []string{}                          // as-enters replacement API
	extStatic   = map[string]func(p map[string]string) []string{}                  // S: mode
	extTrigger  = map[string]func(p map[string]string) string{}                    // T: mode -> event class
	extRepl     = map[string]func(r *cards.Repl) []string{}                        // R: event
	extKeyword  = map[string]func(g *gorgeNorm, param string){}                    // K: head
)
