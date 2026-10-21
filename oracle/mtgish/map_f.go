package mtgish

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
)

// Unit F: T3 (targets). The fixes here make the two sides render the same
// target slot for the same printed target, so a T3 row is a real
// difference in what a card targets.

// --- mtgish side ---

// targetGroupSuffixes mark a Target variant whose count is per player (or
// per colour/card type): "for each opponent, up to one target creature that
// player controls". The player is the grouping, not a target, and the
// count is not a literal (scripts write TargetMax$ OneEach).
var targetGroupSuffixes = []string{
	"EachPlayerControls", "InEachPlayersGraveyard", "OfEachColor", "OfEachCardType",
}

// targetChooserSuffixes name who picks the target ("target creature of an
// opponent's choice", "chosen at random"): not a player target.
// "TargetPlayerControls" names a player target walk adds as its own slot.
var targetChooserSuffixes = []string{
	"TargetPlayerControls",
	"OfAPlayersChoiceTheyControl", "OfAPlayersChoice", "OfPlayersChoice",
	"ChosenAtRandom", "AtRandom",
}

// targetVariantBase strips the grouping and chooser suffixes from a Target
// variant name; perGroup reports a per-player (per-group) count.
func targetVariantBase(v string) (base string, perGroup bool) {
	base = v
	for _, s := range targetGroupSuffixes {
		if strings.HasSuffix(base, s) {
			base = strings.TrimSuffix(base, s)
			perGroup = true
			break
		}
	}
	for _, s := range targetChooserSuffixes {
		if strings.HasSuffix(base, s) {
			base = strings.TrimSuffix(base, s)
			break
		}
	}
	return base, perGroup
}

// perGroupCount turns a per-group count into the slot's total, which is not
// a literal: one each is "*", up to one each is "0..*".
func perGroupCount(count string) string {
	switch count {
	case "1":
		return "*"
	case "0..1":
		return "0..*"
	}
	return count
}

// targetedModal summarises Targeted_Modal, "target spell or permanent"
// written as alternative Targeted actions of which one runs: the slots
// every alternative shares stay as they are, and the one slot left in each
// alternative becomes one slot whose class is the union of theirs (scripts
// write one ValidTgts$ list). The alternatives' effects merge as modes do.
func (e *effects) targetedModal(alts []any) {
	var subs []*effects
	for _, b := range alts {
		sub := &effects{}
		sub.walk(b)
		subs = append(subs, sub)
	}
	if len(subs) == 0 {
		return
	}
	// The multiset of slots common to every alternative.
	common := append([]string(nil), subs[0].targets...)
	for _, s := range subs[1:] {
		left := append([]string(nil), s.targets...)
		var keep []string
		for _, t := range common {
			for k, u := range left {
				if u == t {
					keep = append(keep, t)
					left = append(left[:k], left[k+1:]...)
					break
				}
			}
		}
		common = keep
	}
	var residual [][]string
	for _, s := range subs {
		left := append([]string(nil), s.targets...)
		for _, t := range common {
			for k, u := range left {
				if u == t {
					left = append(left[:k], left[k+1:]...)
					break
				}
			}
		}
		if len(left) != 1 {
			e.merge(subs)
			return
		}
		residual = append(residual, left)
	}
	var cls []string
	count := ""
	for _, r := range residual {
		c, n := splitTargetToken(r[0])
		cls = append(cls, strings.Split(c, "|")...)
		if count == "" || count == "1" {
			count = n
		}
	}
	for _, s := range subs {
		s.targets = nil
	}
	e.merge(subs)
	e.targets = append(e.targets, common...)
	e.targets = append(e.targets, targetToken(narrowPermanent(cls), count))
}

// splitTargetToken splits a target token into its class list and count
// ("1" when it carries none).
func splitTargetToken(t string) (cls, count string) {
	for i := len(t) - 1; i > 0; i-- {
		if t[i] == 'x' && i+1 < len(t) && (t[i+1] >= '0' && t[i+1] <= '9' || t[i+1] == '*') {
			return t[:i], t[i+1:]
		}
	}
	return t, "1"
}

// awakenPlainBranch picks, of SpellActions_Awaken's two action branches,
// the one cast without awaken: the awakened branch adds only the keyword's
// reminder effect (counters on target land, it becomes a creature), and a
// keyword's expansion is summarised by its T1 keyword token on both sides
// (gorge skips its Awaken expansion).
func awakenPlainBranch(branches []any) []any {
	for _, b := range branches {
		awakened := false
		walk(b, func(_ node, enum, variant string) bool {
			if enum == "_Action" && variant == "AwakenPermanent" {
				awakened = true
			}
			return !awakened
		})
		if !awakened {
			return []any{b}
		}
	}
	return branches
}

// mergeThisOrAnother drops the second of two Trigger rules that encode one
// printed "Whenever this or another <filter> enters / dies / leaves / is
// turned face up" (or "this or enchanted creature attacks") ability: mtgish
// writes one rule per subject, the card prints one ability and the script
// writes one T: line whose ValidCard$ lists both. The pair must have the
// same event class and identical actions; the first subject is this
// permanent and the second another permanent (Other(ThisPermanent)) or the
// permanent it is attached to (HostPermanent).
func mergeThisOrAnother(rs []any) []any {
	out := make([]any, 0, len(rs))
	drop := map[int]bool{}
	for i, r := range rs {
		if drop[i] {
			continue
		}
		out = append(out, r)
		v := variantOf(r)
		if !strings.HasPrefix(v, "Trigger") || v == "TriggerModalA" {
			continue
		}
		a := argsOf(r)
		if len(a) < 2 || !thisSubject(a[0]) {
			continue
		}
		rest := canonicalJSON(a[1:])
		// In the "this" rule, "that permanent" is this one: Pashalik Mons'
		// first rule says "that permanent deals damage", its second "this".
		restThis := thatPermanentRef.ReplaceAllString(rest, `"_Permanent":"ThisPermanent"`)
		ev := strings.Join(dedup(triggerEvents(a[0])), " ")
		for j := i + 1; j < len(rs); j++ {
			if drop[j] || variantOf(rs[j]) != v {
				continue
			}
			b := argsOf(rs[j])
			if len(b) != len(a) {
				continue
			}
			if br := canonicalJSON(b[1:]); br != rest && br != restThis {
				continue
			}
			if strings.Join(dedup(triggerEvents(b[0])), " ") != ev || !otherSubject(b[0]) {
				continue
			}
			drop[j] = true
			break
		}
	}
	return out
}

// thisSubject reports a trigger whose subject is exactly this permanent.
func thisSubject(t any) bool {
	for _, a := range argsOf(t) {
		if variantOf(a) == "SinglePermanent" && variantOf(rawArgs(a)) == "ThisPermanent" {
			return true
		}
	}
	return false
}

// otherSubject reports a trigger whose subject is another permanent
// (Other(ThisPermanent) inside its filter) or the permanent this one is
// attached to.
func otherSubject(t any) bool {
	found := false
	walk(t, func(n node, enum, variant string) bool {
		switch {
		case enum == "_Permanents" && variant == "Other" && variantOf(rawArgs(n)) == "ThisPermanent":
			found = true
		case enum == "_Permanent" && variant == "HostPermanent":
			found = true
		}
		return !found
	})
	return found
}

// thatPermanentRef matches a reference to the triggering permanent.
var thatPermanentRef = regexp.MustCompile(`"_Permanent":"(Trigger_ThatPermanent|ThatEnteringPermanent|ThatDyingPermanent|ThatPermanent)"`)

func canonicalJSON(x any) string {
	b, _ := json.Marshal(x) // map keys are marshalled sorted
	return string(b)
}

// playerOrBattleEvents folds "deals combat damage to a player or battle"
// (an Or of the to-a-player and to-a-permanent variants, the permanent a
// battle) into the one event a script's DamageDone line with
// ValidTarget$ Player,Battle is classed as.
func playerOrBattleEvents(subs []any) (string, bool) {
	if len(subs) != 2 {
		return "", false
	}
	var player, battle bool
	for _, s := range subs {
		v := variantOf(s)
		switch {
		case strings.HasSuffix(v, "CombatDamageToAPlayer"):
			player = true
		case strings.HasSuffix(v, "CombatDamageToAPermanent") || strings.HasSuffix(v, "CombatDamageToABattle"):
			cls := ""
			for _, a := range argsOf(s) {
				if enumOf(a) == "Permanents" && !strings.Contains(canonicalJSON(a), "ThisPermanent") {
					cls = strings.Join(permanentsClasses(a), "|")
				}
			}
			battle = cls == "battle" || strings.HasSuffix(v, "ABattle")
		}
	}
	if player && battle {
		return "combat-damage-player", true
	}
	return "", false
}

// typeAlts reads a Permanents filter as alternatives of card-type sets: Or
// is a union of alternatives, And an intersection ("artifact creature" is
// one alternative needing both types). nil means the filter names no type.
func typeAlts(x any) [][]string {
	switch variantOf(x) {
	case "IsCardtype":
		if s, ok := rawArgs(x).(string); ok {
			if w := typeWord(s); permanentTypes[w] {
				return [][]string{{w}}
			}
		}
	case "IsCreatureType", "IsCreatureTypeVariable":
		return [][]string{{"creature"}}
	case "IsArtifactType":
		return [][]string{{"artifact"}}
	case "IsLandType":
		return [][]string{{"land"}}
	case "IsEnchantmentType":
		return [][]string{{"enchantment"}}
	case "Or":
		var out [][]string
		for _, e := range list(rawArgs(x)) {
			out = append(out, typeAlts(e)...)
		}
		return out
	case "And":
		var out [][]string
		for _, e := range list(rawArgs(x)) {
			sub := typeAlts(e)
			if sub == nil {
				continue
			}
			if out == nil {
				out = sub
				continue
			}
			var cross [][]string
			for _, a := range out {
				for _, b := range sub {
					cross = append(cross, append(append([]string(nil), a...), b...))
				}
			}
			out = cross
		}
		return out
	}
	return nil
}

// intersectionClass renders one alternative's card types: "creature", or
// "artifact&creature" for a target that must have both.
func intersectionClass(types []string) string { return strings.Join(dedup(types), "&") }

// classesOfAlts renders typeAlts' result as target classes.
func classesOfAlts(alts [][]string) []string {
	var out []string
	for _, a := range alts {
		if c := intersectionClass(a); c != "" {
			out = append(out, c)
		}
	}
	return dedup(out)
}

// narrowPermanent drops "permanent" alongside a narrower permanent class
// (a type or an intersection of types): a target-kind comparison can use
// nothing more from it. Next to a non-permanent class (spell, player, a
// card in a zone) it is a real alternative and stays.
func narrowPermanent(cls []string) []string {
	ds := dedup(cls)
	narrower := false
	for _, c := range ds {
		if permanentClass(c) && c != clsPermanent {
			narrower = true
		}
	}
	if !narrower {
		return ds
	}
	out := ds[:0:0]
	for _, c := range ds {
		if c != clsPermanent {
			out = append(out, c)
		}
	}
	return out
}

// permanentClass reports a class naming permanents on the battlefield.
func permanentClass(c string) bool {
	if c == clsPermanent {
		return true
	}
	for _, w := range strings.Split(c, "&") {
		if !permanentTypes[w] {
			return false
		}
	}
	return true
}

// --- gorge side ---

// gorgePermanentTypeProps are the Forge filter properties that name a card
// type the target must also have ("Creature.Artifact": an artifact
// creature).
var gorgePermanentTypeProps = map[string]string{
	"Artifact": "artifact", "Creature": "creature", "Enchantment": "enchantment",
	"Land": "land", "Planeswalker": "planeswalker", "Battle": "battle",
}

// altZones picks the zones one ValidTgts$ alternative targets in when the
// SA targets in several ("Permanent.Red,Card.inZoneStack+Red" with
// TgtZone$ Battlefield,Stack): its inZone property; else the battlefield
// for a "Permanent" head; else every zone listed ("" is the battlefield):
// "Creature" with TgtZone$ Battlefield,Graveyard is a creature or a
// creature card in a graveyard.
func altZones(alt string, zones []string) []string {
	if len(zones) == 1 {
		return zones
	}
	for _, z := range []string{"Battlefield", "Stack", "Graveyard", "Exile", "Hand", "Library"} {
		if strings.Contains(alt, "inZone"+z) || strings.Contains(alt, "inRealZone"+z) {
			if z == "Battlefield" {
				return []string{""}
			}
			return []string{z}
		}
	}
	head := alt
	if i := strings.IndexAny(head, ".+"); i >= 0 {
		head = head[:i]
	}
	var out []string
	for _, z := range zones {
		if z == "Battlefield" {
			z = ""
		}
		if head == "Permanent" && z != "" {
			continue
		}
		out = append(out, z)
	}
	if len(out) == 0 {
		return zones[:1]
	}
	return out
}

// altTypes reads one ValidTgts$ alternative's card types: its head (a type,
// or a subtype through the index) and every type-naming property.
func (idx TypeIndex) altTypes(alt string) (types []string, generic bool) {
	head, props := alt, ""
	if i := strings.IndexAny(alt, ".+"); i >= 0 {
		head, props = alt[:i], alt[i+1:]
	}
	w := typeWord(head)
	switch {
	case permanentTypes[w]:
		types = append(types, w)
	case idx[w] != "":
		types = append(types, idx[w])
	case w == "":
	default:
		generic = true
	}
	for _, p := range strings.FieldsFunc(props, func(r rune) bool { return r == '+' || r == '.' }) {
		if t, ok := gorgePermanentTypeProps[p]; ok {
			types = append(types, t)
		} else if p[0] >= 'A' && p[0] <= 'Z' && idx[typeWord(p)] != "" {
			// A subtype property ("Permanent.Goblin", "Card.Wizard").
			types = append(types, idx[typeWord(p)])
		}
	}
	return types, generic
}

// stackTargetTypes maps a TargetType$ list to the stack classes it allows
// (nil when it names none).
func stackTargetTypes(tt string) []string {
	var cls []string
	for _, t := range strings.Split(tt, ",") {
		t = strings.TrimSpace(t)
		if i := strings.IndexByte(t, '.'); i >= 0 {
			t = t[:i]
		}
		switch t {
		case "Spell", "Instant", "Sorcery":
			cls = append(cls, clsSpell)
		case "Activated", "Triggered":
			cls = append(cls, clsAbility)
		case "SpellAbility":
			cls = append(cls, clsSpell, clsAbility)
		}
	}
	return cls
}

// anyTargetClasses folds a class list naming every kind "any target"
// covers (creature, planeswalker, battle, player: CR 115.4) into "any" (a
// script spells "any other target" out as that list), and drops those
// kinds next to "any", which already covers them.
func anyTargetClasses(cs []string) []string {
	need := map[string]bool{"creature": true, "planeswalker": true, "battle": true, clsPlayer: true}
	n, hasAny := 0, false
	for _, c := range cs {
		if need[c] {
			n++
		}
		hasAny = hasAny || c == clsAny
	}
	if n < len(need) && !hasAny {
		return cs
	}
	out := []string{clsAny}
	for _, c := range cs {
		if !need[c] && c != clsAny {
			out = append(out, c)
		}
	}
	return out
}

// expandExactCounts renders a slot of exactly N targets (N a literal, 2 or
// more) as N one-target slots: "two target creatures" and "target creature
// and another target creature" choose the same targets, and scripts and
// mtgish pick either spelling.
//
// A slot of M..N targets with a literal minimum M of 1 or more is likewise
// M one-target slots and one slot of 0..N-M: "target artifact and up to one
// other target artifact" and "one or two target artifacts" choose the same.
func expandExactCounts(ts []string) []string {
	var out []string
	for _, t := range ts {
		cls, count := splitTargetToken(t)
		lo, hi, ranged := strings.Cut(count, "..")
		n, err := strconv.Atoi(lo)
		if err != nil || n > 20 || (!ranged && n < 2) || (ranged && n < 1) {
			out = append(out, t)
			continue
		}
		for i := 0; i < n; i++ {
			out = append(out, cls)
		}
		if ranged {
			rest := "*"
			if h, err := strconv.Atoi(hi); err == nil {
				rest = strconv.Itoa(h - n)
			}
			if rest != "0" {
				out = append(out, targetToken([]string{cls}, "0.."+rest))
			}
		}
	}
	return out
}

// selfGrantStatic summarises a static through which a script writes a
// printed ability that is live only at a threshold: a Station threshold
// ("STATION 8+": Affected$ Card.Self+counters_GE8_CHARGE), Max speed
// (Condition$ MaxSpeed) and a Level up level (IsPresent$ ... LEVEL
// counters). The card prints the abilities themselves, and mtgish encodes
// them as abilities under the threshold, so the granted abilities are
// summarised as abilities (with their targets), not as "grant:ability".
// Granted keywords follow mtgish's shape: under Station they are keywords
// (StationCharged nests plain rules), otherwise a static granting them.
// Whatever else the static does stays a static. It reports false for any
// other static.
func (g *gorgeNorm) selfGrantStatic(st *cards.Static) bool {
	p := st.Params
	if st.Mode != "Continuous" || !(p["Affected"] == "Card.Self" || strings.HasPrefix(p["Affected"], "Card.Self+")) {
		return false
	}
	station := strings.Contains(p["Affected"], "_CHARGE") && g.f.HasKeyword("Station")
	speed := p["Condition"] == "MaxSpeed"
	level := strings.Contains(p["IsPresent"], "_LEVEL") && g.f.HasKeyword("Level up")
	if !station && !speed && !level {
		return false
	}
	rest := make(map[string]string, len(p))
	for k, v := range p {
		rest[k] = v
	}
	for _, name := range strings.Split(p["AddAbility"], "&") {
		if a := cards.ResolveSVar(g.svars(), strings.TrimSpace(name)); a != nil {
			g.sa(a)
		}
	}
	for _, name := range strings.Split(p["AddTrigger"], "&") {
		if body, ok := g.svars()[strings.TrimSpace(name)]; ok {
			if t, ok := cards.ParseTriggerLine(body); ok {
				g.trigger(&t)
			}
		}
	}
	var folded []string
	for _, name := range strings.Split(p["AddStaticAbility"], "&") {
		if body, ok := g.svars()[strings.TrimSpace(name)]; ok {
			if s, ok := cards.ParseStaticLine(body); ok {
				if level {
					// mtgish's levelStatic folds a level's static
					// abilities into the level's one static.
					folded = append(folded, p2cGorgeStaticEffects(g, &s)...)
				} else {
					g.static(&s)
				}
			}
		}
	}
	delete(rest, "AddAbility")
	delete(rest, "AddTrigger")
	delete(rest, "AddStaticAbility")
	if station {
		for _, k := range cards.SplitKeywordList(p["AddKeyword"]) {
			g.keyword(k)
		}
		delete(rest, "AddKeyword")
	}
	if station {
		// A Spacecraft's "becomes an artifact creature" at its threshold
		// is the card's station structure (mtgish keeps it in
		// StationChargedAnimate), not a printed ability. A level's P/T box
		// stays: mtgish's levelStatic writes each level as one static
		// with its setpt.
		for _, k := range []string{"SetPower", "SetToughness", "AddType", "AddTypes"} {
			delete(rest, k)
		}
	}
	eff := gorgeStaticEffects(st.Mode, rest)
	if len(eff) == 1 && eff[0] == unmapped("Static.Continuous") {
		// Nothing but the granted abilities.
		eff = nil
	}
	eff = append(eff, folded...)
	if eff != nil {
		g.ability(Ability{Kind: "static", Effects: eff})
	}
	return true
}

// targetedDistributed summarises TargetedDistributed (targets, the
// amount distributed, the actions): a slot of up to as many targets as the
// amount ("divide 4 damage among any number of targets", written
// AnyNumberOf or UptoNumber(4) by mtgish) reads "0..*", as scripts' divided
// slots do (gorgeTargets).
func (e *effects) targetedDistributed(n node) {
	a := argsOf(n)
	if len(a) < 3 {
		e.walk(a)
		return
	}
	amount := gameNumber(rawArgs(a[1]))
	for _, t := range list(a[0]) {
		if tn, ok := t.(node); ok {
			e.targets = append(e.targets, distributedCount(mtgishTarget(tn), amount))
		}
	}
	e.walk(a[2:])
}

// distributedCount opens a divided slot's maximum when it is the amount
// divided (or any number) and its minimum is 0.
func distributedCount(t, amount string) string {
	cls, count := splitTargetToken(t)
	if count == "0..*" || (count == "0.."+amount && amount != "*") {
		return cls + "x0..*"
	}
	return t
}

// cleaveAlternative reports the spell ability a script writes for a Cleave
// cost: the same spell without the bracketed words, cast for the cleave
// cost (NonBasicSpell$ True, PrecostDesc$ Cleave). The card prints one
// spell ability and the keyword, as mtgish's SpellActions_Cleave merges
// the two versions; the alternative is summarised as the keyword only.
func cleaveAlternative(a *cards.SA) bool {
	return a.Kind == "SP" && a.Params["NonBasicSpell"] == "True" && strings.TrimSpace(a.Params["PrecostDesc"]) == "Cleave"
}
