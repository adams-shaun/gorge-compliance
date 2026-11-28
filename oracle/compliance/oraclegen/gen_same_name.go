package oraclegen

import (
	"strings"

	"github.com/adams-shaun/gorge/engine/rules"
)

// isScenarioRefShaped reports whether s is a "p<seat>:<name>[#k]" object ref
// (the shape isScenarioRef on the driver accepts), not a bare label.
func isScenarioRefShaped(s string) bool {
	i := strings.IndexByte(s, ':')
	return i > 1 && s[0] == 'p' && strings.Trim(s[1:i], "0123456789") == ""
}

// IsScenarioRefShaped is the exported form of isScenarioRefShaped, for
// template answer writers that must never emit "@" before a non-ref label.
func IsScenarioRefShaped(s string) bool {
	return isScenarioRefShaped(s)
}

// SameNamePick describes one object pick that shares its name with another
// offered object, and the XMage answer that selects it exactly.
//
// Ambiguous is set only when the pick and a sibling are DISTINCT objects
// (their scenario refs differ). Two options that resolve to the same ref are
// the same object -- a repeated trigger source, a dual-mode index -- and
// XMage's name match already selects it, so they are not ambiguous and need
// no discriminator.
//
// At most one of CopyMarker and Alias is set:
//
//   - CopyMarker ("[only copy]"/"[no copy]") is XMage's isCopy() filter. It
//     is used only when it leaves EXACTLY ONE candidate: a token among
//     same-named cards, or a card among same-named tokens. Its spelling
//     carries no space before the bracket; TestPlayer.makeChoose strips
//     exactly 9/11 characters.
//   - Alias ("@<ref>") is the driver's exact-object form
//     (TestPlayer.hasObjectTargetNameOrAlias): a bound alias names one
//     object and no other. It is used when the copy filter is not unique --
//     two cards, two tokens, or a card and a token that share a name -- and
//     the scenario ref is therefore the only thing that separates them.
//     registerAliases binds a setup ref to its object; the driver also binds
//     the ref of a mid-game permanent (a token copy) before the ask.
//
// A pick with neither (a bare name) is NOT uniquely identified and is the
// defect the same-name census counts as unresolved.
type SameNamePick struct {
	Ambiguous  bool
	Distinct   int    // distinct same-name offered refs, pick included
	CopyMarker string // "[only copy]"/"[no copy]" when the copy filter is unique
	Alias      string // "@<ref>" when only the exact ref separates the candidates
	// Unanswerable is set when the copy filter is not unique AND the pick's
	// ref is not an identity XMage can bind (a rank-derived library/hand
	// card). The name-only answer is unsound, so the census counts the item
	// as unproven rather than resolved. A target ask can still carry the
	// ref's exact owner prefix when that prefix separates the candidates
	// (ownerQualifiedRef), and a choice pick as its "@<ref>" alias form.
	Unanswerable bool
}

// pickRefExact reports whether pick k's ref is an identity XMage's alias
// binding reconstructs exactly: a token, or a setup battlefield permanent.
// A rank-derived ref (an anonymous library/hand card) is not. A hand-built
// decision that omits the provenance list is treated as exact (its refs are
// synthesized identity fixtures, not live ranks).
func pickRefExact(d rules.OracleDecision, k int) bool {
	if k >= 0 && k < len(d.PickRefsInexact) {
		return !d.PickRefsInexact[k]
	}
	return true
}

// ownerQualifiedRef returns pick k's ref when its owner prefix alone still
// identifies one object, even though the ref is rank-derived: the ref carries
// no "#k" rank and no other same-named offered ref belongs to the same seat.
// XMage's alias binding scopes a non-token ref to that seat's own objects, so
// a card picked from p1's graveyard ("p1:Dawn-Blessed Pennant") reaches p1's
// card, not the first same-named card in either graveyard (CR 601.2c).
//
// Known limit: only the OFFERED refs are visible here. Gorge's rank-free ref
// is k==1 among the owner's live same-named objects in EVERY zone, while the
// driver's alias binding ranks battlefield and hand before graveyard, so a
// same-named card the owner has on the battlefield or in hand (not offered)
// can still win the driver's binding. A decision cannot rule that out; the
// result is a visible replay mismatch, never a silent pass.
//
// An empty result means the owner does not separate the candidates -- two
// same-named objects of one seat, or a ranked ref whose ObjID rank the
// driver's zone order cannot reproduce -- so the conservative bare-name form
// stays.
func ownerQualifiedRef(d rules.OracleDecision, k int) string {
	if k < 0 || k >= len(d.PickRefs) || pickRefExact(d, k) {
		return ""
	}
	ref := d.PickRefs[k]
	seat, ok := refSeat(ref)
	if !ok || !strings.Contains(ref, ":") || strings.Contains(ref, ":token:") {
		return ""
	}
	if i := strings.LastIndexByte(ref, '#'); i >= 0 && i+1 < len(ref) &&
		strings.Trim(ref[i+1:], "0123456789") == "" {
		return ""
	}
	name := oraclediffRefName(ref)
	for _, candidate := range d.OptionRefs {
		if candidate == ref || !strings.EqualFold(oraclediffRefName(candidate), name) {
			continue
		}
		if s, ok := refSeat(candidate); ok && s == seat {
			return ""
		}
	}
	return ref
}

// ClassifySameName reports how pick k of d is disambiguated.
func ClassifySameName(d rules.OracleDecision, k int) SameNamePick {
	if k >= len(d.PickRefs) || !strings.Contains(d.PickRefs[k], ":") {
		return SameNamePick{}
	}
	ref := d.PickRefs[k]
	name := oraclediffRefName(ref)
	pickToken := strings.Contains(ref, ":token:")
	distinct := map[string]bool{}
	sameKind := 0
	for _, candidate := range d.OptionRefs {
		if !strings.EqualFold(oraclediffRefName(candidate), name) {
			continue
		}
		distinct[candidate] = true
		if strings.Contains(candidate, ":token:") == pickToken {
			sameKind++
		}
	}
	if len(distinct) < 2 {
		// The pick's name is unambiguous, or every same-named option is the
		// same object (a repeated trigger source), which a bare name still
		// selects exactly.
		return SameNamePick{}
	}
	p := SameNamePick{Ambiguous: true, Distinct: len(distinct)}
	if sameKind == 1 {
		// Exactly one candidate passes the copy filter: the token-or-card
		// distinction is unique, so the bare name plus the marker selects it.
		if pickToken {
			p.CopyMarker = "[only copy]"
		} else {
			p.CopyMarker = "[no copy]"
		}
		return p
	}
	// Two or more candidates of the same kind (or a card and a token plus a
	// same-kind sibling): the copy filter leaves more than one. Only the
	// pick's own scenario ref identifies it, and only when XMage can bind
	// that ref by identity. A rank-derived ref would bind the WRONG object
	// (or nothing), so it is not emitted: the item is unanswerable.
	if !pickRefExact(d, k) {
		p.Unanswerable = true
		return p
	}
	p.Alias = "@" + ref
	return p
}

// InNameMatchMechanism reports whether pick k of d reaches an XMage name /
// scenario-ref match at all. Excluded are the mechanisms that do NOT:
//
//   - an arrange order (Surveil/Scry/Dig "put them back"): XMage shows the
//     looked-at cards as an ordering list; two identical basics are
//     interchangeable and the compared snapshot cannot tell which one moved,
//     so disambiguating changes nothing.
//   - a library search (pickKind search, or a dig/hideaway pick): XMage's
//     computer player resolves it and the card's identity among identical
//     same-named basics does not change the compared state.
//
// It is the outer scope of the same-name census. IsNameSelection additionally
// drops a pick no exact answer can identify (an inexact ref whose copy filter
// leaves several same-kind candidates).
func InNameMatchMechanism(d rules.OracleDecision, k int) bool {
	if d.Kind == "order" {
		return false
	}
	if pickKind(d, k) == "search" {
		return false
	}
	switch d.Resume {
	case "dig", "search", "dig_arrange", "hideaway_arrange":
		return false
	}
	return true
}

// IsNameSelection reports whether pick k of d is answered by an object name
// or scenario ref that XMage's own name matching consumes (makeChoose,
// chooseTarget, TargetDiscard) AND an exact answer exists. It is the scope
// of the same-name census: every in-scope ambiguous pick must resolve, and a
// pick with no exact answer (ClassifySameName.Unanswerable) is counted as
// unproven, never as resolved.
func IsNameSelection(d rules.OracleDecision, k int) bool {
	return InNameMatchMechanism(d, k) && !ClassifySameName(d, k).Unanswerable
}

// SameNameAmbiguity is ClassifySameName's boolean summary: whether the pick
// shares its name with a distinct sibling, and the copy marker when that
// filter is unique (empty for an alias pick). It is the compatibility form
// the census used before Alias existed; new callers use ClassifySameName.
func SameNameAmbiguity(d rules.OracleDecision, k int) (ambiguous bool, marker string) {
	p := ClassifySameName(d, k)
	return p.Ambiguous, p.CopyMarker
}

// namesObject reports whether a choice LABEL is an object-name answer (or
// empty, meaning "use the object's name"). An activated ability or mana
// choice carries an action label ("Activate Llanowar Elves for mana") that
// XMage's makeChoose matches literally, exactly like an endure choice
// (TestYesNoRequiresAnEngineBoolean); replacing it with the object's scenario
// ref would answer a different question. The exact-ref alias is an
// object-NAME answer, so it is only correct when the label names the object.
func namesObject(label, ref string) bool {
	if label == "" || label == ref || strings.HasPrefix(label, "@") {
		return true
	}
	return strings.EqualFold(label, oraclediffRefName(ref))
}

// hiddenPickUnanswerable is hiddenPickWithheld without snapshots: a
// shape-only caller cannot prove a copy run interchangeable, so it keeps the
// conservative withhold.
func hiddenPickUnanswerable(d rules.OracleDecision) bool { return hiddenPickWithheld(d, nil) }

// offersOnlyCopiesOf reports whether every object decision d offers is a
// same-named object of pick k's own seat: the candidates differ only in
// identity, never in name or owner.
func offersOnlyCopiesOf(d rules.OracleDecision, k int) bool {
	if k < 0 || k >= len(d.PickRefs) || len(d.OptionRefs) == 0 {
		return false
	}
	name := oraclediffRefName(d.PickRefs[k])
	seat, ok := refSeat(d.PickRefs[k])
	if !ok {
		return false
	}
	for _, candidate := range d.OptionRefs {
		if !strings.EqualFold(oraclediffRefName(candidate), name) || strings.Contains(candidate, ":token:") {
			return false
		}
		if s, ok := refSeat(candidate); !ok || s != seat {
			return false
		}
	}
	return !strings.Contains(d.PickRefs[k], ":token:")
}

// ExactRefAlias is the "@ref" answer (or the ref's exact owner-qualified form)
// that names decision d's pick k when its name alone matches more than one
// offered object, or "" when the name is unambiguous (ClassifySameName). Cost
// paths that script a batch as one answer per pick use it so each single binds
// the exact object, as JoinedChoice does for the joined answer.
func ExactRefAlias(d rules.OracleDecision, k int) string {
	if k >= len(d.Picks) || k >= len(d.PickRefs) {
		return ""
	}
	p := ClassifySameName(d, k)
	names := namesObject(d.Picks[k], d.PickRefs[k])
	if p.Alias != "" && names {
		return p.Alias
	}
	if p.Unanswerable && names {
		if owner := ownerQualifiedRef(d, k); owner != "" {
			return "@" + owner
		}
	}
	return ""
}

// JoinedChoice is the one "^"-joined answer XAnswersForScenario scripts for a
// multi-pick decision that one makeChoose dialog consumes: each pick spelled as
// XMage's dialog lists it (a same-name object by its exact ref, a mana colour
// by its colour name, a bare colour word by its capitalised colour name).
// Callers that drop or match that answer use it, so they see the same spelling
// the scenario carries.
func JoinedChoice(d rules.OracleDecision) string {
	labels := make([]string, 0, len(d.Picks))
	for k, label := range d.Picks {
		if label == "" && k < len(d.PickRefs) {
			label = oraclediffRefName(d.PickRefs[k])
		}
		if colour, ok := bareColourLabel(d, k); ok {
			label = colour
		}
		label = disambiguatedObjectChoice(d, k, label)
		if colour, ok := manaColourLabel(label); ok {
			label = colour
		}
		labels = append(labels, label)
	}
	return strings.Join(labels, "^")
}

// disambiguatedObjectChoice returns the choice-queue answer for pick k: the
// exact-ref alias when the copy marker cannot separate the candidates AND the
// label is an object-name answer, the owner-qualified ref when an inexact ref's
// owner still separates them, else the label with the copy marker appended (no
// space before the bracket).
func disambiguatedObjectChoice(d rules.OracleDecision, k int, label string) string {
	nameIsObject := k < len(d.PickRefs) && namesObject(label, d.PickRefs[k])
	return disambiguatedObjectChoiceNamed(d, k, label, nameIsObject)
}

// targetPickValue is the target-queue answer for pick k of d: the seat ref
// as offered, the exact-ref alias when only the ref separates same-named
// candidates (the driver's targetName maps a bound ref to its @alias), the
// owner-qualified ref when an inexact ref's owner still separates them, else
// the object name with the copy marker appended. XMage's chooseTarget parses
// the same copy marker as makeChoose, so a target among same-named objects
// needs the discriminator too.
func targetPickValue(d rules.OracleDecision, k int, label string) string {
	v := label
	if k < len(d.PickRefs) {
		v = d.PickRefs[k]
	}
	if isSeat(v) {
		return v
	}
	p := ClassifySameName(d, k)
	if p.Alias != "" {
		return d.PickRefs[k]
	}
	if p.Unanswerable {
		// The owner prefix is exact even when the rank is not: a graveyard
		// card picked from p1's graveyard is "p1:<name>", which the driver
		// binds to p1's own object instead of the first same-named card.
		if owner := ownerQualifiedRef(d, k); owner != "" {
			return owner
		}
	}
	v = oraclediffRefName(v)
	_, marker := SameNameAmbiguity(d, k)
	return v + marker
}

// oraclediffRefName strips a scenario ref to the object name.
func oraclediffRefName(ref string) string {
	n := ref
	if i := strings.IndexByte(n, ':'); i >= 0 && strings.HasPrefix(n, "p") {
		n = n[i+1:]
	}
	n = strings.TrimPrefix(n, "token:")
	if j := strings.LastIndexByte(n, '#'); j >= 0 && j+1 < len(n) && strings.Trim(n[j+1:], "0123456789") == "" {
		n = n[:j]
	}
	return n
}
