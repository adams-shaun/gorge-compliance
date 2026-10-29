// Package xprep makes a lowered xengine item something the XMage driver can
// replay. The driver runs in strict choose mode: every decision the replay
// poses needs a scripted answer (Item.XAnswers), every activate step the rule
// text XMage selects the ability by (Item.XAbility), and a card XMage spells
// differently needs Item.XMageName. The single-card generator fills those per
// card; a lowered window or game holds many cards, so Prepare assembles the
// same pieces per item, from the result gorge's own runner gave the item.
//
// An item Prepare cannot serve is refused for XMage with a reason from the
// closed Reasons list; it is never guessed at.
package xprep

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/lower"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
)

// Refusal reasons: a closed list.
const (
	// ReasonSpelling: the item holds a card XMage spells differently
	// (Item.XMageName names one card only).
	ReasonSpelling = decks.ReasonXMageSpelling
	// ReasonModeCollision: two cards that are cast or activated in the item
	// give one mode label different numbers; the answer map is keyed by label.
	ReasonModeCollision = "xmage_mode_collision"
	// ReasonAbilityUnknown: an activate step has no XMage rule-text prefix (a
	// token or a card the corpus does not know, an ambiguous face, a step that
	// names no ability index).
	ReasonAbilityUnknown = "xmage_ability_unknown"
	// ReasonRetarget: rewriting the cast steps' targets from gorge's own target
	// decisions changed what gorge does with the item.
	ReasonRetarget = "xmage_retarget_diverges"
	// ReasonNoResult: there is no gorge result to script the answers from.
	ReasonNoResult = "xmage_no_result"
	// ReasonWindowOnly: a tape item; XMage replays windows only (design O4:
	// the driver's turn model cannot follow a tape, so tapes go to Forge and
	// the kernel).
	ReasonWindowOnly = "xmage_window_only"
)

// Reasons is every reason Prepare refuses with.
var Reasons = []string{ReasonSpelling, ReasonModeCollision, ReasonAbilityUnknown, ReasonRetarget, ReasonNoResult, ReasonWindowOnly}

// tapeID matches the id of a tape item: "...:tape" or a prefix "...:tape:t<N>".
var tapeID = regexp.MustCompile(`:tape(:t\d+)?$`)

// IsTape reports whether id names a tape item (a whole tape or one of its
// turn prefixes) rather than a window.
func IsTape(id string) bool { return tapeID.MatchString(id) }

// Refusal is an item that was not prepared, and why.
type Refusal struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
	Detail string `json:"detail,omitempty"`
}

func refuse(id, reason, format string, a ...any) *Refusal {
	return &Refusal{ID: id, Reason: reason, Detail: fmt.Sprintf(format, a...)}
}

// Prepare is l's item with the XMage fields filled from g, gorge's own result
// for it (SelfCheck's Check.Result), or the Refusal that says why it cannot.
// The result is a pure function of its arguments.
//
//   - Answers: the cast and activate steps' targets are rewritten to gorge's
//     own target picks (oraclegen.ChooseTargets) and the rest of the decisions
//     are scripted by oraclegen.XAnswersForScenario, which also restores a
//     divided spell's shares (oraclegen.CastDivisions). The mode numbers are the
//     union over every face of every card the item casts or activates.
//   - Ability prefixes: oraclegen.XMageAbility of the step card's face.
//   - Spellings: an item holding a card XMage spells differently is refused.
//
// Item.XTargetSkips stays unset: the generator derives it from the fixture's
// omitted optional slots, and a lowered item has no fixture, so the driver
// keeps its legacy trailing skip.
func Prepare(reg *cards.Registry, sp *decks.Spellings, l lower.Lowered, g rules.OracleResult) (oraclegen.Item, *Refusal) {
	it := l.Item
	if bad := differing(reg, sp, it); len(bad) > 0 {
		return it, refuse(it.ID, ReasonSpelling, "%s", strings.Join(bad, "; "))
	}
	if len(g.Snapshots) == 0 {
		return it, refuse(it.ID, ReasonNoResult, "gorge result has no snapshots")
	}
	modes, why := modeNumbers(reg, it)
	if why != "" {
		return it, refuse(it.ID, ReasonModeCollision, "%s", why)
	}
	abilities, ref := prefixes(reg, it)
	if ref != nil {
		return it, ref
	}
	sc, castSteps := oraclegen.ChooseTargets(it.Scenario, g.Decisions)
	if why := sameOutcome(reg, sc, g); why != "" {
		return it, refuse(it.ID, ReasonRetarget, "%s", why)
	}
	it.Scenario = sc
	g.Decisions = playStepUnlessPays(sc.Steps, g.Decisions)
	it.XAnswers = oraclegen.XAnswersForScenario(reg, g, sc, modes, castSteps)
	if len(it.XAnswers) == 0 {
		// Nothing to script: still one (empty) entry per step, as the
		// generator's activate and combat templates do.
		it.XAnswers = make([][]oraclegen.XAnswer, len(sc.Steps))
	}
	it.XAbility = abilities
	it.XPos = passPositions(sc.Steps, g.Snapshots)
	return it, nil
}

// playStepUnlessPays rewrites the paid as-enters "unless you pay" decision of a
// land's play step to the yes/no shape XMage's TapSourceUnlessPaysEffect asks.
// Gorge records it as a two-option mode decision, so the generic router emits a
// numeric mode answer; the driver puts that on the MODE queue while the
// as-enters ask reads the CHOICE queue (strict miss "Missing CHOICE def"). A
// paid pass-step unless cost keeps its numeric mode answer, and a decline stays
// a mode decision the generic router already scripts "choice no" for.
func playStepUnlessPays(steps []oraclegen.Step, ds []rules.OracleDecision) []rules.OracleDecision {
	out := append([]rules.OracleDecision(nil), ds...)
	for i := range out {
		d := &out[i]
		if d.Step < 0 || d.Step >= len(steps) || steps[d.Step].Op != "play" ||
			d.Kind != "mode" || d.Resume != "unless_pay" || d.Options < 2 ||
			len(d.Picks) == 0 || !paidUnlessLabel(d.Picks[0]) {
			continue
		}
		// xanswersSetup's yesno branch emits choice "yes" for a "Pay ..." pick.
		d.Kind = "yesno"
	}
	return out
}

// paidUnlessLabel is whether an unless-pay pick's label names paying the cost
// rather than declining it. It mirrors oraclegen's unexported unlessPolarity;
// the labels are the engine's, so the two must stay in sync.
func paidUnlessLabel(label string) bool {
	l := strings.ToLower(strings.TrimSpace(label))
	if l == "" {
		return false
	}
	for _, neg := range []string{"don't", "do not", "decline", "no", "refuse"} {
		if strings.HasPrefix(l, neg) {
			return false
		}
	}
	return true
}

// passPositions is Item.XPos: for each pass step i, where gorge stood in
// snaps[i+1] (the checkpoint after step i; snaps[0] is the setup one), and
// whether that pass moved the turn or step or removed an object from the
// stack. Nil when the snapshots do not line up with the steps, which leaves
// the driver on the pass shapes it knows.
func passPositions(steps []oraclegen.Step, snaps []rules.OracleSnapshot) []*oraclegen.XPos {
	if len(snaps) != len(steps)+1 {
		return nil
	}
	var out []*oraclegen.XPos
	any := false
	for i, st := range steps {
		if st.Op != "pass" {
			out = append(out, nil)
			continue
		}
		any = true
		before, after := snaps[i], snaps[i+1]
		out = append(out, &oraclegen.XPos{
			Turn: after.Turn, Step: after.Step, Active: after.Active,
			Moved:    before.Turn != after.Turn || before.Step != after.Step,
			Resolves: stackLost(before.Stack, after.Stack),
		})
	}
	if !any {
		return nil
	}
	return out
}

// stackLost is whether after lacks an object before holds (compared as a
// multiset of kind, source and controller): a pass never removes an object
// but by resolving it, and a trigger that resolution puts on does not hide it.
func stackLost(before, after []rules.OracleSnapStack) bool {
	left := map[rules.OracleSnapStack]int{}
	for _, o := range after {
		left[o]++
	}
	for _, o := range before {
		if left[o] == 0 {
			return true
		}
		left[o]--
	}
	return false
}

// sameOutcome is "" when gorge's runner, given sc, ends in the snapshots g
// holds, else what differs.
func sameOutcome(reg *cards.Registry, sc oraclegen.Scenario, g rules.OracleResult) string {
	b, err := json.Marshal(sc)
	if err != nil {
		return err.Error()
	}
	res, err := rules.RunOracleScenarioJSON(reg, b)
	switch {
	case err != nil:
		return "rerun: " + err.Error()
	case len(res.Fails) > 0:
		return "rerun fails: " + res.Fails[0]
	case !reflect.DeepEqual(res.Snapshots, g.Snapshots):
		return "rerun snapshots differ from the item's result"
	}
	return ""
}

// refName is the card name a scenario ref ("p0:Forest#2") names; ok is false
// for a token, an ability, a player or anything else that is no corpus card.
func refName(ref string) (string, bool) {
	n := ref
	if i := strings.IndexByte(n, ':'); i >= 2 && n[0] == 'p' && strings.Trim(n[1:i], "0123456789") == "" {
		n = n[i+1:]
	} else if i >= 0 {
		return "", false
	}
	if strings.HasPrefix(n, "token:") || strings.HasPrefix(n, "ability:") {
		return "", false
	}
	if j := strings.LastIndexByte(n, '#'); j >= 0 && j+1 < len(n) && strings.Trim(n[j+1:], "0123456789") == "" {
		n = n[:j]
	}
	return n, n != ""
}

// cardNames is every card name the item's setup and steps hold, sorted.
func cardNames(it oraclegen.Item) []string {
	seen := map[string]bool{}
	add := func(names ...string) {
		for _, n := range names {
			if n != "" {
				seen[n] = true
			}
		}
	}
	addRef := func(refs ...string) {
		for _, r := range refs {
			if n, ok := refName(r); ok {
				add(n)
			}
		}
	}
	for _, s := range it.Setup {
		add(s.Battlefield...)
		add(s.Tapped...)
		add(s.BackFace...)
		add(s.Hand...)
		add(s.Graveyard...)
		add(s.Exile...)
		add(s.Library...)
		add(s.LibraryTop...)
		for n := range s.Counters {
			add(n)
		}
	}
	for _, st := range it.Steps {
		addRef(st.Card, st.AttachedTo)
		addRef(st.Targets...)
		addRef(st.Attackers...)
		for _, b := range st.Blocks {
			addRef(b[0], b[1])
		}
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// differing is the item's cards XMage spells differently, as "Corpus -> XMage".
func differing(reg *cards.Registry, sp *decks.Spellings, it oraclegen.Item) []string {
	var out []string
	for _, n := range cardNames(it) {
		if xm, ok := sp.XMage(reg, n); ok && xm != n {
			out = append(out, n+" -> "+xm)
		}
	}
	return out
}

// stepCard is the corpus card and face index a cast or activate step names;
// ok is false for a token, an ability or a card the corpus does not know. A
// permanent the seat's setup places on its back face is face 1, as the Forge
// export reads it.
func stepCard(reg *cards.Registry, it oraclegen.Item, st oraclegen.Step) (*cards.Card, int, bool) {
	n, ok := refName(st.Card)
	if !ok {
		return nil, 0, false
	}
	c, ok := reg.Lookup(n)
	if !ok || len(c.Faces) == 0 {
		return nil, 0, false
	}
	face := 0
	for i, f := range c.Faces {
		if f.Name == n {
			face = i
			break
		}
	}
	if seat, _, has := strings.Cut(st.Card, ":"); has {
		for _, b := range it.Setup[seat].BackFace {
			if b == n && len(c.Faces) > 1 {
				face = 1
			}
		}
	}
	return c, face, true
}

// modeNumbers is the union of oraclegen.ModeNumbers over every face of every
// card the item casts or activates, or why two cards disagree on a label.
func modeNumbers(reg *cards.Registry, it oraclegen.Item) (map[string]int, string) {
	out := map[string]int{}
	owner := map[string]string{}
	done := map[string]bool{}
	for _, st := range it.Steps {
		if st.Op != "cast" && st.Op != "activate" {
			continue
		}
		n, ok := refName(st.Card)
		if !ok || done[n] {
			continue
		}
		done[n] = true
		c, ok := reg.Lookup(n)
		if !ok {
			continue
		}
		for _, f := range c.Faces {
			m := oraclegen.ModeNumbers(f)
			labels := make([]string, 0, len(m))
			for k := range m {
				labels = append(labels, k)
			}
			sort.Strings(labels)
			for _, k := range labels {
				if prev, seen := out[k]; seen && prev != m[k] {
					return nil, fmt.Sprintf("mode %q is %d on %s and %d on %s", k, prev, owner[k], m[k], n)
				}
				if _, seen := out[k]; !seen {
					out[k], owner[k] = m[k], n
				}
			}
		}
	}
	return out, ""
}

// prefixes is Item.XAbility: parallel to the steps, the XMage rule-text prefix
// of every activate step's ability and "" elsewhere (nil when no step
// activates). An activate step whose prefix is unknown is a Refusal.
func prefixes(reg *cards.Registry, it oraclegen.Item) ([]string, *Refusal) {
	type key struct {
		card string
		face int
	}
	cache := map[key]map[int]string{}
	cached := map[key]string{}
	var out []string
	for i, st := range it.Steps {
		if st.Op != "activate" {
			continue
		}
		if st.AbilityIndex == nil {
			return nil, refuse(it.ID, ReasonAbilityUnknown, "step %d activates %q by label %q, which names no ability index", i, st.Card, st.Ability)
		}
		c, face, ok := stepCard(reg, it, st)
		if !ok {
			return nil, refuse(it.ID, ReasonAbilityUnknown, "step %d activates %q, which is no corpus card", i, st.Card)
		}
		k := key{c.Faces[0].Name, face}
		m, have := cache[k]
		if !have {
			m, cached[k] = oraclegen.XMageAbility(c.Faces[face])
			cache[k] = m
		}
		if why := cached[k]; why != "" {
			return nil, refuse(it.ID, ReasonAbilityUnknown, "step %d: %s: %s", i, c.Faces[face].Name, why)
		}
		p := m[*st.AbilityIndex]
		if p == "" {
			return nil, refuse(it.ID, ReasonAbilityUnknown, "step %d: %s has no XMage text for ability %d", i, c.Faces[face].Name, *st.AbilityIndex)
		}
		if out == nil {
			out = make([]string, len(it.Steps))
		}
		out[i] = p
	}
	return out, nil
}
