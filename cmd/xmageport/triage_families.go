package main

import (
	"regexp"
	"strings"
)

// Shapes found triaging the triggers, enchantments, rules, watchers,
// replacement, combat, continuous, copy, prevention, control, requirement and
// planeswalker families. Like the shapes in triage_shapes.go each converts a
// FAILING run only: a test of the same shape that passes stays covered.

var (
	// reCopyNewTargets is Illusionist's Bracers: XMage's setChoice(false) is
	// "no new targets for the copy"; gorge poses the optional trigger itself
	// first, so a no answer declines the copy.
	reCopyNewTargets = regexp.MustCompile(`(?i)may choose new targets for the copy`)
	// reOptionalPrevent is Battletide Alchemist: an optional prevention
	// replacement whose yes/no XMage states as booleans in an order and count
	// gorge's replacement asks do not share.
	reOptionalPrevent = regexp.MustCompile(`(?i)\byou may prevent\b`)
	// reSearchLibrary is a library search.
	reSearchLibrary = regexp.MustCompile(`(?i)\bsearch(?:es)? your library for\b`)
	// reCounterReplacement is a replacement that changes how many counters a
	// permanent gets (Doubling Season, Pir, Hardened Scales).
	reCounterReplacement = regexp.MustCompile(`(?i)counters? would be put on|would put one or more [^.\n]*counters?`)
)

// xmageTokenCap is the number of tokens XMage stops a player at: an
// implementation limit of its engine, not a rule of the game.
const xmageTokenCap = 500

// setupCardWith returns the first setup card (either seat, any zone) one of
// whose faces' Oracle text matches re, or "".
func (t *translator) setupCardWith(re *regexp.Regexp) string {
	for p := 0; p < t.seats(); p++ {
		for _, ac := range t.setup[p] {
			if c, ok := t.lookup(ac.name); ok {
				for _, f := range c.Faces {
					if f != nil && re.MatchString(f.Oracle) {
						return ac.name
					}
				}
			}
		}
	}
	return ""
}

// hasBoolChoice reports whether the test states a yes/no choice, optionally
// only the given answer ("" for either).
func (t *translator) hasBoolChoice(answer string) bool {
	for _, ch := range t.choiceList {
		if ch.target || (ch.pick != "yes" && ch.pick != "no") {
			continue
		}
		if answer == "" || ch.pick == answer {
			return true
		}
	}
	return false
}

// familyShapes returns the first shape refusal of those above, or nil.
func (t *translator) familyShapes() *refusal {
	if n := t.setupCardWith(reCopyNewTargets); n != "" && t.hasBoolChoice("no") {
		return refuse("new-target-copy-boolean", "%s's copy may take new targets; the test's false is that choice, gorge asks whether to copy first", n)
	}
	if n := t.setupCardWith(reOptionalPrevent); n != "" && t.hasBoolChoice("") {
		return refuse("optional-prevention-boolean", "%s's optional prevention is answered by booleans gorge's replacement asks do not take in that order", n)
	}
	if t.searchesDefaultLibrary() {
		return refuse("search-default-library", "the test searches a library it never stocks; XMage's default library has the cards, the runner's is filler")
	}
	// Mycosynth Lattice + March of the Machines + Humility: XMage's expected
	// end state is a layer-dependency ruling this repository's hand-ported
	// layer_tests.json deliberately does not adopt (CR 613.7 timestamps leave
	// the three permanents 4/4, 4/4 and 6/6, so the port asserts only the lands
	// dying). The generated scenario would assert the opposite without a
	// ruling, so it is refused rather than ratcheted as a gorge bug.
	if t.setupHasName("Mycosynth Lattice") && t.setupHasName("March of the Machines") && t.setupHasName("Humility") {
		return refuse("layer-dependency-hand-port-ruling", "the expected end state of Mycosynth Lattice + March of the Machines + Humility is disputed by the hand-ported layer scenario; the generated row is not a proven gorge gap")
	}
	return nil
}

// searchesDefaultLibrary reports whether a seat that casts, activates or
// plays something has a card searching its library and the test seeds no
// library card for that seat.
func (t *translator) searchesDefaultLibrary() bool {
	acts := [maxSeats]bool{}
	for _, a := range t.actions {
		if a.kind == "cast" || a.kind == "activate" || a.kind == "play" {
			acts[a.seat] = true
		}
	}
	for p := 0; p < t.seats(); p++ {
		if !acts[p] {
			continue
		}
		stocked := false
		for _, ac := range t.setup[p] {
			if strings.HasPrefix(ac.zone, "library") {
				stocked = true
			}
		}
		if stocked {
			continue
		}
		for _, ac := range t.setup[p] {
			if c, ok := t.lookup(ac.name); ok {
				for _, f := range c.Faces {
					if f != nil && reSearchLibrary.MatchString(f.Oracle) {
						return true
					}
				}
			}
		}
	}
	return false
}

// counterReplacedEntry notes the shape of a setup permanent that enters with
// counters (a planeswalker's loyalty, a Saga's lore) while another setup card
// replaces how many counters it gets. XMage runs addCard(BATTLEFIELD) through
// the game's entry, so Doubling Season and Pir apply, and picks their order
// from a setChoice; the runner places the permanent directly, and its setup
// has no decision to state an order in.
func (t *translator) counterReplacedEntry(name, kind string) {
	if kind != "LOYALTY" && kind != "LORE" {
		return
	}
	if n := t.setupCardWith(reCounterReplacement); n != "" && !sameName(n, name) {
		t.noteShape(refuse("setup-etb-counter-replacement", "%s enters in the setup with %s counters under %s; the runner's setup applies no entry replacement", name, strings.ToLower(kind), n))
	}
}

// tokenCapShape notes the shape of a count at XMage's token cap.
func (t *translator) tokenCapShape(zone string, n int) {
	if zone == "battlefield" && n >= xmageTokenCap {
		t.noteShape(refuse("xmage-token-cap", "%d permanents asserted; XMage stops a player at %d tokens, which is its engine's limit, not a rule", n, xmageTokenCap))
	}
}
