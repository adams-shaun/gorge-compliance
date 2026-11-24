package oraclegen

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
)

// searchZoneAsk is the XMage ask shape a gorge search_zones election answers:
// the effect must be the library-first SearchLibraryGraveyard* family, and
// whether the library search found a card decides if XMage poses the graveyard
// chooseUse at all.
type searchZoneAsk struct {
	libraryFirst bool
	libraryFound bool
}

// searchZoneAsks resolves every search_zones decision to the XMage effect shape
// it answers. The source card is resolved the way digSourceForStep resolves a
// Dig: the stack snapshot at the decision's step is the authority, with the
// step's own cast/activate card as a fallback. Only an effect whose Oracle text
// states "library and/or graveyard" (the SearchLibraryGraveyard* static text)
// is mapped. A graveyard-first effect (Arachnus Spinner's custom
// "graveyard and/or library" implementation) or an unresolved source is left
// out, so the router scripts nothing instead of answering the wrong prompt.
func searchZoneAsks(reg *cards.Registry, sc Scenario, snaps []rules.OracleSnapshot, ds []rules.OracleDecision) map[int]searchZoneAsk {
	if reg == nil {
		return nil
	}
	out := map[int]searchZoneAsk{}
	for i, d := range ds {
		if d.Resume != "search_zones" {
			continue
		}
		libraryFirst, ok := searchZoneLibraryFirst(reg, sc, snaps, d.Step)
		if !ok || !libraryFirst {
			continue
		}
		out[i] = searchZoneAsk{libraryFirst: true, libraryFound: searchZoneLibraryFound(snaps, ds, i)}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// searchZoneLibraryFirst reports whether the effect that posed a search_zones
// decision at step is the library-first XMage class. ok is false when no
// ChangeZone with Origin Library / OriginAlternative Graveyard resolves, when
// its text does not state the and/or pair, or when the resolved abilities
// disagree.
func searchZoneLibraryFirst(reg *cards.Registry, sc Scenario, snaps []rules.OracleSnapshot, step int) (bool, bool) {
	if reg == nil || step < 0 {
		return false, false
	}
	var orders []bool
	seen := map[*cards.SA]bool{}
	addFace := func(f *cards.Face, sa *cards.SA) {
		if f == nil || sa == nil {
			return
		}
		collectSearchZoneOrders(f, sa, seen, &orders)
	}
	if step < len(snaps) {
		for _, o := range snaps[step].Stack {
			c, ok := reg.Lookup(oraclediffRefName(o.Source))
			if !ok {
				continue
			}
			for _, f := range digFaces(c, o.Source) {
				if f == nil {
					continue
				}
				switch {
				case o.Kind == "ability" && o.Trigger != "":
					if idx, err := strconv.Atoi(o.Trigger); err == nil && idx >= 0 && idx < len(f.Triggers) {
						addFace(f, f.Triggers[idx].Effect)
					}
				case o.Kind == "ability":
					if sa := searchZoneActivatedSA(f, sc, snaps, step, o.Source); sa != nil {
						addFace(f, sa)
					} else {
						searchZoneCardFaces(f, seen, &orders)
					}
				default:
					// A spell's effect lives on its own ability list.
					for _, sa := range f.Abilities {
						addFace(f, sa)
					}
				}
			}
		}
	}
	// A cast/activate step names its own source when the snapshot does not
	// resolve it (an empty or stale snapshot).
	if len(orders) == 0 && step < len(sc.Steps) && sc.Steps[step].Card != "" {
		if c, ok := reg.Lookup(oraclediffRefName(sc.Steps[step].Card)); ok {
			for _, f := range digFaces(c, sc.Steps[step].Card) {
				if f != nil {
					searchZoneCardFaces(f, seen, &orders)
				}
			}
		}
	}
	// A cast-resolve step has no card of its own: the trigger that fires while
	// the spell resolves (Delivery Moogle's enters trigger) is sourced by the
	// most recent cast. That card is only used when it actually carries the
	// and/or search effect, so an unrelated earlier cast resolves nothing.
	if len(orders) == 0 {
		for i := step - 1; i >= 0; i-- {
			st := sc.Steps[i]
			if st.Op != "cast" || st.Card == "" {
				continue
			}
			if c, ok := reg.Lookup(oraclediffRefName(st.Card)); ok {
				for _, f := range digFaces(c, st.Card) {
					if f != nil {
						searchZoneCardFaces(f, seen, &orders)
					}
				}
			}
			break
		}
	}
	if len(orders) == 0 {
		return false, false
	}
	for _, o := range orders[1:] {
		if o != orders[0] {
			return false, false
		}
	}
	return orders[0], true
}

// searchZoneActivatedSA resolves the activated ability a stack object names
// through the most recent activate step for that source, the same activation
// identity digSourceAtActivation uses.
func searchZoneActivatedSA(f *cards.Face, sc Scenario, snaps []rules.OracleSnapshot, step int, source string) *cards.SA {
	for i := step - 1; i >= 0 && i < len(sc.Steps); i-- {
		st := sc.Steps[i]
		if st.Op != "activate" || st.Card != source || st.AbilityIndex == nil {
			continue
		}
		if k := *st.AbilityIndex; k >= 0 && k < len(f.Abilities) {
			return f.Abilities[k]
		}
		return nil
	}
	return nil
}

// searchZoneCardFaces collects the search-zone orders of every ability,
// trigger and SVar body on a face. It is the card-level fallback when no exact
// ability or trigger index resolves.
func searchZoneCardFaces(f *cards.Face, seen map[*cards.SA]bool, orders *[]bool) {
	for _, sa := range f.Abilities {
		collectSearchZoneOrders(f, sa, seen, orders)
	}
	for _, tr := range f.Triggers {
		collectSearchZoneOrders(f, tr.Effect, seen, orders)
	}
	for _, name := range sortedKeys(f.SVars) {
		p := svarParams(f.SVars[name])
		if p["DB"] == "ChangeZone" && isLibraryGraveyardChangeZoneParams(p) {
			if order, ok := searchZoneTextOrder(f.Oracle); ok {
				*orders = append(*orders, order)
			}
		}
	}
}

// collectSearchZoneOrders walks one ability chain, recording the ask order of
// every Origin Library / OriginAlternative Graveyard ChangeZone it carries.
func collectSearchZoneOrders(f *cards.Face, sa *cards.SA, seen map[*cards.SA]bool, orders *[]bool) {
	if sa == nil || seen[sa] {
		return
	}
	seen[sa] = true
	if isLibraryGraveyardChangeZone(sa) {
		if order, ok := searchZoneTextOrder(f.Oracle); ok {
			*orders = append(*orders, order)
		}
	}
	collectSearchZoneOrders(f, sa.Sub, seen, orders)
}

func isLibraryGraveyardChangeZone(sa *cards.SA) bool {
	return sa != nil && sa.API == "ChangeZone" && isLibraryGraveyardChangeZoneParams(sa.Params)
}

func isLibraryGraveyardChangeZoneParams(p map[string]string) bool {
	return strings.EqualFold(strings.TrimSpace(p["Origin"]), "Library") &&
		strings.EqualFold(strings.TrimSpace(p["OriginAlternative"]), "Graveyard")
}

// searchZoneTextOrder reads the zone order from the effect wording. The
// standard XMage SearchLibraryGraveyard* classes print "search your library
// and/or graveyard" and ask the library first; a custom graveyard-first card
// prints "search your graveyard and/or library" (Arachnus Spinner). Text with
// no and/or pair (a forced-both search) is not the mapped shape.
func searchZoneTextOrder(oracle string) (bool, bool) {
	lower := strings.ToLower(oracle)
	start := strings.Index(lower, "search your")
	if start < 0 {
		return false, false
	}
	seg := lower[start:]
	library := strings.Index(seg, "library")
	graveyard := strings.Index(seg, "graveyard")
	if library < 0 || graveyard < 0 || library == graveyard {
		return false, false
	}
	lo, hi := library, graveyard
	if lo > hi {
		lo, hi = hi, lo
	}
	if !strings.Contains(seg[lo:hi], "and/or") {
		return false, false
	}
	return library < graveyard, true
}

// searchZoneLibraryFound reports whether the same step's search found a card
// in the library. The before/after checkpoints are the authority (a library
// pick moves a card out, so the library count falls). A pick that the before
// checkpoint shows in the graveyard is a graveyard find even if an unrelated
// draw in the same step also lowered the library count.
func searchZoneLibraryFound(snaps []rules.OracleSnapshot, ds []rules.OracleDecision, i int) bool {
	if i < 0 || i >= len(ds) {
		return false
	}
	d := ds[i]
	if d.Step < 0 || d.Step+1 >= len(snaps) {
		return false
	}
	before, after := snaps[d.Step], snaps[d.Step+1]
	if d.Seat < 0 || d.Seat >= len(before.Players) || d.Seat >= len(after.Players) {
		return false
	}
	if searchPickInGraveyard(ds, i, before.Players[d.Seat].Graveyard) {
		return false
	}
	return after.Players[d.Seat].LibraryCount < before.Players[d.Seat].LibraryCount
}

// searchPickInGraveyard reports whether the search decision that follows this
// zone election picked a card the before checkpoint already shows in the
// graveyard.
func searchPickInGraveyard(ds []rules.OracleDecision, i int, graveyard []string) bool {
	if i < 0 || i >= len(ds) {
		return false
	}
	grave := make(map[string]bool, len(graveyard))
	for _, ref := range graveyard {
		grave[ref] = true
	}
	for j := i + 1; j < len(ds); j++ {
		n := ds[j]
		if n.Step != ds[i].Step {
			break
		}
		if n.Seat != ds[i].Seat || n.Resume != "search" {
			continue
		}
		for _, ref := range n.PickRefs {
			if grave[ref] {
				return true
			}
		}
		for _, ref := range n.ObjectPicks {
			if grave[ref] {
				return true
			}
		}
	}
	return false
}

// searchZoneRedundantBooleans marks the gorge decisions that have no XMage
// ask when a mapped library-first SearchLibraryGraveyard* effect follows them.
// The XMage effect folds both "you may search" (Forge's OptionalDecider$ on
// the trigger and Optional$ True on the ChangeZone) into its own library
// chooseUse, so the optional-trigger yes and the search_confirm yes would be
// left over (Fang-Druid Summoner: "found [Yes, Yes]"). Only a search_confirm
// on the same step/seat as the mapped election qualifies; an optional trigger
// without one keeps its boolean.
func searchZoneRedundantBooleans(ds []rules.OracleDecision, asks map[int]searchZoneAsk) map[int]bool {
	if len(asks) == 0 {
		return nil
	}
	out := map[int]bool{}
	for i, d := range ds {
		if _, ok := asks[i]; !ok {
			continue
		}
		confirm := -1
		for j := i - 1; j >= 0; j-- {
			p := ds[j]
			if p.Step != d.Step || p.Seat != d.Seat {
				break
			}
			if p.Resume == "search_confirm" {
				confirm = j
				break
			}
			if p.Kind != "yesno" {
				break
			}
		}
		if confirm < 0 {
			continue
		}
		out[confirm] = true
		if confirm > 0 {
			p := ds[confirm-1]
			if p.Step == d.Step && p.Seat == d.Seat &&
				p.Kind == "yesno" && p.GorgeKind == "trigger_optional" {
				out[confirm-1] = true
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
