package oraclegen

// Resolving a granted or gained activated ability for the follower exporters.
//
// A lowered step that activates a granted (AddAbilities$/AddAbility$ SVar),
// gained (GainsAbilitiesOf$) or keyword-granted ability carries the engine's
// offered option label in Step.Ability beside its AbilityIndex: the pick's
// Ability index is not the card's printed index there (it aliases printed
// index 0 for an SVar grant and is -1 for a keyword grant), so resolving the
// index against the printed Face.Abilities exports the WRONG line -- Dalakos,
// Crafter of Wonders's {T} mana ability for Dragon Mantle's granted {R} pump,
// the farm's xe:RAND:1:4:w14/w22 divergence.
//
// The label is exact engine text: "<recipient face name>: <SpellDescription>"
// (the granted-ability offer loops in gorge's legal walk). ResolveActivatedAbility
// finds the SA the label names by scanning the cards the item's setup and the
// replay's snapshot for that step may put in play -- the recipient itself and
// every potential grantor: printed abilities (a gained ability is a printed SA
// on a foreign card's face), each face's SVar table (an SVar grant's body) and
// each AddKeyword$ static (a derived keyword line). Matching the label, not a
// list of grantor card names, covers the next granted ability without a new
// selector; the label is the engine's own, so the match is exact, not a
// heuristic.
//
// This file lives in oraclegen, not in a follower: forge-export, the neutral
// exporter and xprep all need the same answer, and a copy in one of them
// would be the next place to forget a grant kind.

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
)

// GrantedAbilityLabel is the label the engine offers sa under on a recipient
// whose face is named recipient: the granted-ability offer loops label a
// granted, gained or keyword-granted ability
// "<face name>: <SpellDescription>".
func GrantedAbilityLabel(recipient string, sa *cards.SA) string {
	return recipient + ": " + sa.ParamStr(cards.PKSpellDescription)
}

// ResolveActivatedAbility returns the granted/gained/keyword-granted ability
// activate step i names, or false when the step is a printed ability (or names
// no ability). A step lowered before the grant identity existed carries no
// label; its recorded activate pick in g.Decisions still names the offered
// option, so the same resolution covers a pre-fix item without a re-lowering.
//
// Two candidates with the same label but different script lines are
// ambiguous and count as none, so an export never depends on scan order.
func ResolveActivatedAbility(reg *cards.Registry, it Item, i int, st Step, g rules.OracleResult) (*cards.SA, bool) {
	if st.Op != "activate" || st.Card == "" {
		return nil, false
	}
	label := st.Ability
	if label == "" {
		// The pre-fix recovery reads a recorded pick for an index-named
		// step; a label-only step with neither anchor names no ability.
		if st.AbilityIndex == nil {
			return nil, false
		}
		label = recordedActivateLabel(g, i, st.Card)
		if label == "" {
			return nil, false
		}
		// A pre-fix printed step's recorded pick is the printed ability's own
		// label. Treat it as printed, not as a grant that happens to carry the
		// same text, so neutral identities keep their ordinal.
		if sa, ok := printedStepAbility(reg, it, i, st, g); ok &&
			GrantedAbilityLabel(stepLabelName(reg, it, i, st, g), sa) == label {
			return nil, false
		}
	}
	recipient := stepLabelName(reg, it, i, st, g)
	var found []*cards.SA
	seen := map[string]bool{}
	add := func(sa *cards.SA) {
		if sa == nil || !sa.IsActivated() || seen[sa.Line] {
			return
		}
		if GrantedAbilityLabel(recipient, sa) != label {
			return
		}
		seen[sa.Line] = true
		found = append(found, sa)
	}
	for _, c := range grantedSourceCards(reg, it, i, st.Card, g) {
		for _, f := range c.Faces {
			if f == nil {
				continue
			}
			for _, ab := range f.Abilities {
				add(ab)
			}
			names := make([]string, 0, len(f.SVars))
			for sv := range f.SVars {
				names = append(names, sv)
			}
			sort.Strings(names)
			for _, sv := range names {
				add(cards.ResolveSVar(f.SVars, sv))
			}
			for _, st := range f.Statics {
				if line := strings.TrimSpace(st.ParamStr(cards.PKAddKeyword)); line != "" {
					add(cards.GrantedKeywordAbility(line))
				}
				if lines := strings.TrimSpace(st.ParamStr(cards.PKAddKeywords)); lines != "" {
					for _, line := range strings.Split(lines, ",") {
						if line = strings.TrimSpace(line); line != "" {
							add(cards.GrantedKeywordAbility(line))
						}
					}
				}
			}
		}
	}
	if len(found) != 1 {
		return nil, false
	}
	return found[0], true
}

// recordedActivateLabel is the label of the option gorge's runner picked for
// activate step i, or "" when the result records none. A priority action's
// pick is not an OracleDecision (the runner filters KPriority), so this reads
// the run's own transcript line ("step activate ..." then
// "[activate] pN priority -> [\"label\"]"), which is the runner's exact pick.
// A transcript is absent only in a synthetic OracleResult; the entering
// snapshot's Offered list is then the fallback, and a single offered label
// for the source is still exact.
func recordedActivateLabel(g rules.OracleResult, i int, ref string) string {
	if label, ok := transcriptActivateLabel(g.Transcript, i); ok {
		return label
	}
	if i >= 0 && i < len(g.Snapshots) {
		var labels []string
		for _, o := range g.Snapshots[i].Offered {
			if o.Source == ref && o.Label != "" {
				labels = append(labels, o.Label)
			}
		}
		if len(labels) == 1 {
			return labels[0]
		}
	}
	return ""
}

// transcriptActivateLabel extracts step i's activate pick from the runner's
// transcript. Each scenario step opens one "step <op> <card>" line; the
// priority submit for the op follows it as
// "  [activate] p0 priority -> [\"label\"]". The label list is Go's %q of a
// []string, which is JSON for the strings it carries.
func transcriptActivateLabel(lines []string, step int) (string, bool) {
	idx := -1
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "step ") {
			idx++
			continue
		}
		if idx != step || !strings.Contains(line, "[activate]") ||
			!strings.Contains(line, "priority -> [") {
			continue
		}
		start := strings.Index(line, " -> [")
		if start < 0 {
			continue
		}
		var labels []string
		if err := json.Unmarshal([]byte(line[start+4:]), &labels); err != nil || len(labels) != 1 {
			continue
		}
		return labels[0], true
	}
	return "", false
}

// stepLabelName is the face name the engine prefixes an offered ability label
// with: the live snapshot permanent's name (or "Face-down creature", CR 708.8)
// when the step names one, else the ref's card / token face name.
func stepLabelName(reg *cards.Registry, it Item, i int, st Step, g rules.OracleResult) string {
	if perm, ok := stepSnapshotPerm(st.Card, i, g); ok {
		if perm.FaceDown {
			return "Face-down creature"
		}
		if perm.Name != "" {
			return perm.Name
		}
	}
	return oraclediffRefName(st.Card)
}

// printedStepAbility is the ability Face.Abilities[*st.AbilityIndex] the step
// names when the setup or the entering snapshot resolves the recipient face.
// It is nil for a token whose face has no such index or a step with no index.
func printedStepAbility(reg *cards.Registry, it Item, i int, st Step, g rules.OracleResult) (*cards.SA, bool) {
	if st.AbilityIndex == nil {
		return nil, false
	}
	k := *st.AbilityIndex
	if k < 0 {
		return nil, false
	}
	perm, found := stepSnapshotPerm(st.Card, i, g)
	c := refCard(reg, it, st.Card, perm, found)
	if c == nil {
		return nil, false
	}
	face := c.Faces[0]
	if found && perm.Name != "" {
		for _, f := range c.Faces {
			if f.Name == perm.Name {
				face = f
				break
			}
		}
	} else if seat, _, has := strings.Cut(st.Card, ":"); has {
		for _, name := range it.Setup[seat].BackFace {
			if name == oraclediffRefName(st.Card) && len(c.Faces) > 1 {
				face = c.Faces[1]
				break
			}
		}
	}
	if k >= len(face.Abilities) || face.Abilities[k] == nil {
		return nil, false
	}
	return face.Abilities[k], true
}

func stepSnapshotPerm(ref string, i int, g rules.OracleResult) (rules.OracleSnapPerm, bool) {
	if i < 0 || i >= len(g.Snapshots) {
		return rules.OracleSnapPerm{}, false
	}
	for _, p := range g.Snapshots[i].Permanents {
		if p.Ref == ref {
			return p, true
		}
	}
	return rules.OracleSnapPerm{}, false
}

// grantedSourceCards is every card the item's setup or the entering snapshot
// may put a grantor or the step's recipient on: the recipient itself, each
// seat's battlefield and other zones in sorted seat order, the setup tokens,
// then the snapshot's permanents and each seat's visible zones. The snapshot
// is what covers a grantor that entered or moved during the window (an Aura
// cast on the recipient, a land played, a card moved to a graveyard).
func grantedSourceCards(reg *cards.Registry, it Item, i int, ref string, g rules.OracleResult) []*cards.Card {
	var out []*cards.Card
	seen := map[*cards.Card]bool{}
	add := func(c *cards.Card) {
		if c == nil || len(c.Faces) == 0 || seen[c] {
			return
		}
		seen[c] = true
		out = append(out, c)
	}
	perm, found := stepSnapshotPerm(ref, i, g)
	add(refCard(reg, it, ref, perm, found))

	seats := make([]string, 0, len(it.Setup))
	for seat := range it.Setup {
		seats = append(seats, seat)
	}
	sort.Strings(seats)
	for _, seat := range seats {
		s := it.Setup[seat]
		for _, name := range s.Battlefield {
			if c, ok := reg.Lookup(name); ok {
				add(c)
			}
		}
		for _, name := range s.BackFace {
			if c, ok := reg.Lookup(name); ok {
				add(c)
			}
		}
		for _, zone := range [][]string{s.Hand, s.Graveyard, s.Exile, s.Command, s.Library, s.LibraryTop} {
			for _, name := range zone {
				if c, ok := reg.Lookup(name); ok {
					add(c)
				}
			}
		}
		for _, tok := range s.Tokens {
			add(reg.Tokens[tok.Script])
		}
	}
	if i >= 0 && i < len(g.Snapshots) {
		snap := g.Snapshots[i]
		for _, p := range snap.Permanents {
			add(refCard(reg, it, p.Ref, p, true))
		}
		for _, s := range snap.Players {
			for _, zone := range [][]string{s.Hand, s.Graveyard, s.Exile, s.Command, s.LibraryTop} {
				for _, name := range zone {
					if c, ok := reg.Lookup(name); ok {
						add(c)
					}
				}
			}
		}
		for _, s := range snap.Stack {
			if p, ok := stepSnapshotPerm(s.Source, i, g); ok {
				add(refCard(reg, it, s.Source, p, true))
			}
		}
	}
	return out
}

// refCard resolves a scenario ref to its card: a token script by seat and face
// name (narrowed by the snapshot when several scripts share the name), else
// the printed-name registry. Deterministic: setup tokens are scanned in slice
// order and token stems in sorted order; a remaining tie is no match.
func refCard(reg *cards.Registry, it Item, ref string, perm rules.OracleSnapPerm, found bool) *cards.Card {
	name := oraclediffRefName(ref)
	if _, isToken := strings.CutPrefix(oraclediffRefNameUnmarked(ref), "token:"); isToken {
		return grantedTokenCard(reg, it, ref, name, perm, found)
	}
	if c, ok := reg.Lookup(name); ok && len(c.Faces) > 0 {
		return c
	}
	return nil
}

// oraclediffRefNameUnmarked is oraclediffRefName without stripping the
// "token:" marker, so refCard can tell a token ref from a printed card whose
// name happens to be a token's.
func oraclediffRefNameUnmarked(ref string) string {
	n := ref
	if i := strings.IndexByte(n, ':'); i >= 0 && strings.HasPrefix(n, "p") {
		n = n[i+1:]
	}
	if j := strings.LastIndexByte(n, '#'); j >= 0 && j+1 < len(n) && strings.Trim(n[j+1:], "0123456789") == "" {
		n = n[:j]
	}
	return n
}

func grantedTokenCard(reg *cards.Registry, it Item, ref, name string, perm rules.OracleSnapPerm, found bool) *cards.Card {
	if seat, _, has := strings.Cut(ref, ":"); has {
		for _, t := range it.Setup[seat].Tokens {
			if c := reg.Tokens[t.Script]; c != nil && len(c.Faces) > 0 && c.Faces[0].Name == name {
				return c
			}
		}
	}
	if c, ok := reg.Lookup(name); ok && len(c.Faces) > 0 {
		return c
	}
	stems := make([]string, 0, len(reg.Tokens))
	for stem := range reg.Tokens {
		stems = append(stems, stem)
	}
	sort.Strings(stems)
	var cands []*cards.Card
	for _, stem := range stems {
		c := reg.Tokens[stem]
		if c != nil && len(c.Faces) > 0 && c.Faces[0].Name == name {
			cands = append(cands, c)
		}
	}
	if len(cands) == 1 {
		return cands[0]
	}
	if len(cands) == 0 || !found {
		return nil
	}
	cands = filterGrantedCards(cands, func(c *cards.Card) bool {
		return stringSetSubset(c.Faces[0].Keywords, perm.Keywords)
	})
	if len(cands) == 1 {
		return cands[0]
	}
	if len(cands) == 0 {
		return nil
	}
	cands = filterGrantedCards(cands, func(c *cards.Card) bool {
		return stringSetEqual(c.Faces[0].Types, perm.Types)
	})
	if len(cands) == 1 {
		return cands[0]
	}
	if len(cands) == 0 {
		return nil
	}
	cands = filterGrantedCards(cands, func(c *cards.Card) bool {
		return c.Faces[0].PT == perm.PT
	})
	if len(cands) == 1 {
		return cands[0]
	}
	return nil
}

func filterGrantedCards(in []*cards.Card, keep func(*cards.Card) bool) []*cards.Card {
	out := make([]*cards.Card, 0, len(in))
	for _, c := range in {
		if keep(c) {
			out = append(out, c)
		}
	}
	return out
}

func stringSetSubset(a, b []string) bool {
	set := make(map[string]bool, len(b))
	for _, s := range b {
		set[s] = true
	}
	for _, s := range a {
		if !set[s] {
			return false
		}
	}
	return true
}

func stringSetEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	return stringSetSubset(a, b)
}
