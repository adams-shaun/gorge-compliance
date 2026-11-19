// XMage ability-text derivation (see
// docs/design/2026-10-05-compliance-level-b.md section 2.1).
//
// XMage's TestPlayer selects an activated ability by a prefix of its printed
// rule text -- activateAbility(turn, step, player, "{2}, {T}") -- so a level-B
// activate scenario must carry that prefix outside the scenario body (which
// rules' runner decodes strictly). The mapping is kept in its own file so the
// driver side can adjust it without touching the template:
//
//   - Each printed activated-ability line is paired with the non-keyword AB
//     that spells the same printed cost (Cost$) and effect
//     (SpellDescription), never by ordinal position: a face's A:AB$ order may
//     differ from its Oracle line order (Ezuri, Renegade Leader).
//   - A keyword-expanded AB (Equip, Cycling, Station, ...) maps to its
//     keyword line's text ("Equip {2}"), found by the keyword's printed name.
//   - A loyalty cost prints as XMage does ("+1", "-3", "0", "-X"), without
//     the Oracle brackets.
//   - Loyalty, shared-cost and line-less intrinsic mappings must select
//     uniquely under startsWith: conflicting prefixes extend a word at a
//     time in {this}-rewritten text ("+1: Exile" vs "+1: Add", "-1:").
//     Other faces retain their legacy cost-only mappings.
//   - An injected basic-land-type mana ability with no printed line maps to
//     XMage's "{T}: Add {C}." text.
//   - Matching is a bijection: distinct lines map to distinct ABs, so a line
//     left with one unused candidate is forced to it, and a line whose wording
//     derives no candidate is paired when exactly one line and one
//     non-intrinsic AB remain (the only possible pairing). A line that still
//     matches no AB, more than one AB, or that leaves a non-intrinsic AB
//     unmatched is ambiguous and the caller skips the face.
package oraclegen

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
)

// XMageAbility maps every activated ability of f (by index into
// f.Abilities) to the XMage rule-text prefix that selects it, or returns a
// non-empty reason the whole face's mapping is ambiguous. A caller that only
// needs one ability still fails closed for the face, because the pairing is
// derived from the whole face.
func XMageAbility(f *cards.Face) (map[int]string, string) {
	lines := abilityLines(f.Oracle)
	// Keyword-expanded activations can also have colon-bearing printed
	// selector lines (Class: "{cost}: Level N"). They belong to that
	// keyword AB, not to the ordinary activated-ability lines paired below.
	lines = removeKeywordAbilityLines(f, lines)
	pairs, ok := pairAbilityLines(f, lines)
	if !ok {
		return nil, "activate xmage text ambiguous"
	}
	out := make(map[int]string, len(f.Abilities))
	full := make([]string, len(pairs))
	base := make([]string, len(pairs))
	nonKeyword := make([]int, len(pairs))
	mapped := make(map[int]bool, len(pairs))
	for k, i := range pairs {
		nonKeyword[k] = i
		mapped[i] = true
		// full and base live in the same {this}-rewritten text space, so a
		// shared self-referential cost ("{T}, Sacrifice {this}") is seen as
		// shared and an extension into the rule text never copies the printed
		// name.
		full[k] = xmageRuleLine(lines[k], f.Name)
		base[k] = linePrefix(lines[k], f.Name)
		if loyalty := loyaltyCost(f.Abilities[i].ParamStr(cards.PKCost)); loyalty != "" {
			colon := strings.Index(full[k], ": ")
			if colon < 0 {
				return nil, "activate xmage text ambiguous"
			}
			full[k] = loyalty + ": " + strings.TrimSpace(full[k][colon+2:])
			base[k] = loyalty
		}
		// A card class that sets an ability word the Oracle line does not
		// print (Bloom Tender's Vivid) must carry it on BOTH the full line
		// and the base prefix, or the HasPrefix check below rejects the face.
		if hdr := xmageAbilityWordHeader(f.Name, full[k]); hdr != "" {
			full[k] = hdr + full[k]
			base[k] = hdr + base[k]
		}
	}
	// Keyword ABs and line-less intrinsic mana abilities are not in the
	// printed-line pairing; append every selectable synthetic text now.
	// Assemble EVERY selectable line before extending any prefix, including
	// keyword abilities, so insertion order cannot hide a conflict (Murmuring
	// Bosk's printed {T} versus its Forest intrinsic).
	for i, sa := range f.Abilities {
		if !sa.IsActivated() || mapped[i] {
			continue
		}
		var text string
		if intrinsicLandMana(sa) {
			text = intrinsicManaText(sa)
		} else if keyword := sa.ParamStr(cards.PKKeyword); keyword != "" {
			var ok bool
			text, ok = keywordPrefix(f, sa)
			if !ok {
				return nil, "activate xmage text ambiguous"
			}
		} else {
			continue
		}
		nonKeyword = append(nonKeyword, i)
		base = append(base, text)
		full = append(full, text)
	}
	seen := make(map[string]bool, len(nonKeyword))
	for k, i := range nonKeyword {
		prefix := extendPrefix(base[k], full, k)
		if conflictsWithOtherLine(prefix, k, full) || namesShortName(prefix[len(base[k]):], f.Name) {
			return nil, "activate xmage text ambiguous"
		}
		if prefix == "" || seen[prefix] || out[i] != "" {
			return nil, "activate xmage text ambiguous"
		}
		seen[prefix] = true
		out[i] = prefix
	}
	// A unique full-line match must also be unique against every emitted
	// prefix; fail closed if a prefix ever leaves its own line's text space.
	prefixes := make([]string, len(nonKeyword))
	for k, i := range nonKeyword {
		prefixes[k] = out[i]
	}
	for k, prefix := range prefixes {
		if !strings.HasPrefix(full[k], prefix) || conflictsWithOtherLine(prefix, k, prefixes) {
			return nil, "activate xmage text ambiguous"
		}
	}
	return out, ""
}

// pairAbilityLines pairs every printed activated-ability line with the
// non-keyword AB that describes it. A face's A:AB$ order is not the Oracle
// line order, so matching is by the printed cost and effect text, never by
// ordinal position. An intrinsic basic-land mana ability may have no printed
// line and is left for the synthetic-text path; every other unmatched AB, and
// every line that is left ambiguous, makes the face ambiguous.
func pairAbilityLines(f *cards.Face, lines []string) ([]int, bool) {
	candidates := nonKeywordAbilities(f, true)
	sets := make([][]int, len(lines))
	for k, line := range lines {
		sets[k] = abilitiesForLine(f, line, candidates)
	}
	pairs := make([]int, len(lines))
	for k := range pairs {
		pairs[k] = -1
	}
	used := make(map[int]bool, len(lines))
	// Distinct lines cannot claim the same AB, so a line with exactly one
	// unused candidate is forced to it. Repeating the pass is what makes a
	// cost/effect wording gap harmless: once the other lines are fixed, the
	// gap line is the only one left for the remaining AB.
	for progress := true; progress; {
		progress = false
		for k := range lines {
			if pairs[k] >= 0 {
				continue
			}
			ai, n := -1, 0
			for _, c := range sets[k] {
				if !used[c] {
					ai, n = c, n+1
				}
			}
			if n == 1 {
				pairs[k] = ai
				used[ai] = true
				progress = true
			}
		}
	}
	// A line whose wording derives no candidate at all can still be paired
	// when the bijection leaves it alone: one printed line and one
	// non-intrinsic AB remain, so no other pairing exists (Goblin Polka Band's
	// Forge cost spells the per-target {R} as X). This is not extended to a
	// line whose candidates were all claimed by other lines: that is a real
	// conflict, not an elimination.
	last := -1
	for k := range lines {
		if pairs[k] >= 0 {
			continue
		}
		if last >= 0 {
			return nil, false
		}
		last = k
	}
	if last >= 0 {
		var free []int
		for _, ai := range candidates {
			if !used[ai] && !intrinsicLandMana(f.Abilities[ai]) {
				free = append(free, ai)
			}
		}
		if len(free) != 1 || len(sets[last]) != 0 {
			return nil, false
		}
		pairs[last] = free[0]
		used[free[0]] = true
	}
	for _, ai := range candidates {
		if !used[ai] && !intrinsicLandMana(f.Abilities[ai]) {
			return nil, false
		}
	}
	return pairs, true
}

// abilitiesForLine returns the candidates the line can describe. Printed cost
// symbols are the primary key; a shared symbol set is narrowed by the
// non-mana cost's printed words and then by the effect text. A line whose
// symbols match nothing falls back to the effect alone, which serves Oracle
// lines that spell alternative or compound costs in one line. More than one
// remaining candidate is deliberately left ambiguous.
func abilitiesForLine(f *cards.Face, line string, candidates []int) []int {
	prefix := strings.ToLower(line)
	if i := strings.Index(prefix, ": "); i >= 0 {
		prefix = prefix[:i]
	}
	want := oracleCostSymbols(line)
	var matches []int
	for _, ai := range candidates {
		if sameCostSymbols(want, forgeCostSymbols(f.Abilities[ai].ParamStr(cards.PKCost))) {
			matches = append(matches, ai)
		}
	}
	if len(matches) > 1 {
		if narrowed := filterCostFeatures(f, matches, line, prefix); len(narrowed) > 0 {
			matches = narrowed
		}
	}
	if len(matches) > 1 {
		if narrowed := filterEffects(f, matches, line); len(narrowed) > 0 {
			matches = narrowed
		}
	}
	if len(matches) == 0 {
		matches = filterEffects(f, candidates, line)
	}
	return matches
}

// filterCostFeatures keeps the candidates whose non-mana Cost$ words are
// spelled in the line's cost prefix. An empty result leaves the previous
// candidate set in place: a feature spelling this matcher cannot derive must
// not by itself turn a known cost match into a different pairing.
func filterCostFeatures(f *cards.Face, candidates []int, line, prefix string) []int {
	var out []int
	for _, ai := range candidates {
		if costFeaturesMatch(f.Abilities[ai], line, prefix) {
			out = append(out, ai)
		}
	}
	return out
}

// costFeaturesMatch reports whether every non-mana Cost$ token has a word in
// the line's printed cost prefix. A token's printed description (the last
// slash-separated payload part) is required in full; without one, any word of
// its filter is enough. A line that prints non-mana cost text cannot match a
// candidate with no non-mana Cost$ token at all.
func costFeaturesMatch(sa *cards.SA, line, prefix string) bool {
	features := forgeCostFeatures(sa.ParamStr(cards.PKCost))
	if prefixHasCostText(line) && len(features) == 0 {
		return false
	}
	for _, feature := range features {
		head, desc, filter := feature[0], feature[1], feature[2]
		if len(head) > 0 && !anyWordIn(prefix, head) {
			return false
		}
		if len(desc) > 0 {
			for _, word := range desc {
				if !strings.Contains(prefix, word) {
					return false
				}
			}
		} else if len(filter) > 0 && !anyWordIn(prefix, filter) {
			return false
		}
	}
	return true
}

// filterEffects keeps the candidates whose SpellDescription describes the
// line's effect text under effectMatches.
func filterEffects(f *cards.Face, candidates []int, line string) []int {
	var out []int
	for _, ai := range candidates {
		if effectMatches(line, abilityEffectText(f, ai), f.Name) {
			out = append(out, ai)
		}
	}
	return out
}

// abilityEffectText is the ability's printed effect, or the injected mana
// ability's synthetic "Add {C}." text for an intrinsic with no printed line.
func abilityEffectText(f *cards.Face, ai int) string {
	sa := f.Abilities[ai]
	if intrinsicLandMana(sa) {
		return strings.TrimPrefix(intrinsicManaText(sa), "{T}: ")
	}
	return sa.ParamStr(cards.PKSpellDescription)
}

// lineEffectText is the text after the line's first ": ", including any
// second colon a compound cost prints (Rasputin, the Oneiromancer's
// "{T}: Remove ...: Add ...").
func lineEffectText(line string) string {
	i := strings.Index(line, ": ")
	if i < 0 {
		return ""
	}
	return line[i+2:]
}

// effectMatches reports whether desc spells the line's effect. Both sides are
// normalised for {this}, card-name spelling, number words and common Oracle /
// Forge wording differences. Equality, a whole-word suffix (a compound cost
// may precede the effect), and a prefix are accepted; a prefix whose leftover
// begins "or " is rejected because that is one printed line describing two
// different ABs (Jinxed Choker, Cadaverous Bloom), which must stay ambiguous.
func effectMatches(line, desc, name string) bool {
	if desc == "" {
		return true
	}
	want := normalizeXMageText(lineEffectText(line), name)
	if strings.Contains(desc, "NICKNAME") && !strings.Contains(want, "{this}") {
		want = normalizeNickname(want, name)
	}
	got := normalizeXMageText(desc, name)
	if strings.TrimRight(want, ".,;:") == strings.TrimRight(got, ".,;:") {
		return true
	}
	if strings.HasSuffix(want, " "+got) {
		return true
	}
	if strings.HasPrefix(want, got) {
		return !strings.HasPrefix(strings.TrimSpace(want[len(got):]), "or ")
	}
	return false
}

// normalizeXMageText puts an effect in the spelling both sides can share:
// {this} for every printed name or Forge placeholder, lower case, collapsed
// whitespace, number words as digits, and the hand-mapped Oracle/Forge word
// differences. The alternatives in "add {G} or {W}" are sorted, because XMage
// prints the card's own order and Forge may reverse it.
func normalizeXMageText(text, name string) string {
	text = stripReminder(strings.TrimSpace(text))
	text = selfRef(text, name)
	text = strings.ReplaceAll(text, "CARDNAME", "{this}")
	text = strings.ReplaceAll(text, "NICKNAME", "{this}")
	if short, _, ok := strings.Cut(name, ","); ok {
		if short = strings.TrimSpace(short); short != "" {
			text = strings.ReplaceAll(text, short, "{this}")
		}
	}
	norm := strings.ToLower(strings.Join(strings.Fields(text), " "))
	norm = strings.ReplaceAll(norm, "'s", "s")
	norm = " " + norm + " "
	for _, pair := range [][2]string{
		{"gains", "get"}, {"gain", "get"}, {"gets", "get"},
		{"amount", "number"}, {"that card", "it"},
	} {
		norm = strings.ReplaceAll(norm, " "+pair[0]+" ", " "+pair[1]+" ")
	}
	for _, pair := range numberWordPairs {
		norm = strings.ReplaceAll(norm, " "+pair[0]+" ", " "+pair[1]+" ")
	}
	// Forge prints a type-restricted mana effect's list as "Dragon or Omen
	// spell" where the Oracle repeats the noun ("Dragon spell or an Omen
	// spell"). Collapse both spellings to the Forge form.
	norm = spellListRE.ReplaceAllString(norm, "$1 or $2 spell")
	// Forge also keeps a stale sentence period inside a mana alternative
	// ("Add {W}. or {U}.") where the Oracle has none.
	norm = strings.ReplaceAll(norm, ". or ", " or ")
	norm = strings.TrimSpace(norm)
	return sortAddAlternatives(norm)
}

var (
	addOrRE     = regexp.MustCompile(`add (\{[^{}]+\}) or (\{[^{}]+\})`)
	spellListRE = regexp.MustCompile(`\b([a-z]+) spell or (?:an? )?([a-z]+) spell\b`)
)

// sortAddAlternatives orders a two-colour "add {X} or {Y}" alternative so the
// printed order cannot hide the same effect.
func sortAddAlternatives(s string) string {
	return addOrRE.ReplaceAllStringFunc(s, func(m string) string {
		sub := addOrRE.FindStringSubmatch(m)
		if len(sub) != 3 {
			return m
		}
		a, b := sub[1], sub[2]
		if a > b {
			a, b = b, a
		}
		return "add " + a + " or " + b
	})
}

// normalizeNickname rewrites the longest word-prefix of a comma-less card's
// name that appears in text to {this}. Forge uses NICKNAME where the Oracle
// prints a short name that is not before a comma ("Crovax" for "Crovax the
// Cursed"), and selfRef cannot derive it from the full name.
func normalizeNickname(text, name string) string {
	fields := strings.Fields(name)
	for n := len(fields) - 1; n >= 1; n-- {
		prefix := strings.Join(fields[:n], " ")
		if prefix != "" && strings.Contains(strings.ToLower(text), strings.ToLower(prefix)) {
			return selfRef(text, prefix)
		}
	}
	return text
}

// numberWordPairs maps the English number words Forge spells to the digits the
// Oracle uses ("mills ten cards" versus "mills 10 cards"). A slice, not a map,
// so the replacement order is fixed even though the words do not overlap.
var numberWordPairs = [][2]string{
	{"one", "1"}, {"two", "2"}, {"three", "3"}, {"four", "4"},
	{"five", "5"}, {"six", "6"}, {"seven", "7"}, {"eight", "8"},
	{"nine", "9"}, {"ten", "10"}, {"eleven", "11"}, {"twelve", "12"},
	{"thirteen", "13"}, {"fourteen", "14"}, {"fifteen", "15"},
	{"sixteen", "16"}, {"seventeen", "17"}, {"eighteen", "18"},
	{"nineteen", "19"}, {"twenty", "20"}, {"thirty", "30"},
	{"forty", "40"}, {"fifty", "50"}, {"sixty", "60"},
	{"seventy", "70"}, {"eighty", "80"}, {"ninety", "90"},
	{"hundred", "100"},
}

// oracleCostSymbols extracts the mana and loyalty symbols from a line's cost
// prefix. A brace whose content is not a mana symbol ({this}) is ignored.
func oracleCostSymbols(line string) []string {
	prefix := line
	if i := strings.Index(prefix, ": "); i >= 0 {
		prefix = prefix[:i]
	}
	var out []string
	for _, m := range costBraceRE.FindAllStringSubmatch(prefix, -1) {
		if sym, ok := oracleManaSymbol(m[1]); ok {
			out = append(out, sym)
		}
	}
	for _, m := range loyaltyBracketRE.FindAllStringSubmatch(prefix, -1) {
		n := m[1]
		switch {
		case n == "X" || n == "-X":
			out = append(out, "-X")
		case n == "0" || n == "+0" || n == "-0":
			out = append(out, "0")
		case strings.HasPrefix(n, "-"):
			out = append(out, n)
		default:
			out = append(out, "+"+strings.TrimPrefix(n, "+"))
		}
	}
	return out
}

// forgeCostSymbols extracts the mana and loyalty symbols from a Forge Cost$
// string. Non-symbol tokens (Sac<...>, PayLife<...>) are handled by
// forgeCostFeatures; Waterbend<N> is a mana cost of N in XMage's rule text.
func forgeCostSymbols(cost string) []string {
	var out []string
	for _, tok := range forgeCostTokens(cost) {
		if sym, ok := forgeManaSymbol(tok); ok {
			out = append(out, sym)
			continue
		}
		if n, ok := loyaltyTokenCount(tok); ok {
			switch {
			case n == "X":
				out = append(out, "-X")
			case n == "0":
				out = append(out, "0")
			case strings.HasPrefix(tok, "SubCounter<"):
				out = append(out, "-"+n)
			default:
				out = append(out, "+"+n)
			}
			continue
		}
		if strings.HasPrefix(tok, "Waterbend<") {
			out = append(out, "{"+strings.TrimSuffix(strings.TrimPrefix(tok, "Waterbend<"), ">")+"}")
		}
	}
	return out
}

// forgeCostFeatures returns one (head, description, filter) word list per
// non-mana Cost$ token.
func forgeCostFeatures(cost string) [][3][]string {
	var out [][3][]string
	for _, tok := range forgeCostTokens(cost) {
		if _, ok := forgeManaSymbol(tok); ok {
			continue
		}
		if _, ok := loyaltyTokenCount(tok); ok || strings.HasPrefix(tok, "XMin") {
			continue
		}
		head, desc, filter := forgeCostFeatureWords(tok)
		if len(head)+len(desc)+len(filter) > 0 {
			out = append(out, [3][]string{head, desc, filter})
		}
	}
	return out
}

// loyaltyTokenCount returns the counter count of a loyalty Cost$ token.
func loyaltyTokenCount(tok string) (string, bool) {
	for _, spec := range []struct{ prefix, sign string }{{"AddCounter<", "+"}, {"SubCounter<", "-"}} {
		if strings.HasPrefix(tok, spec.prefix) && strings.HasSuffix(tok, "/LOYALTY>") {
			return strings.TrimSuffix(strings.TrimPrefix(tok, spec.prefix), "/LOYALTY>"), true
		}
	}
	return "", false
}

// forgeCostFeatureWords splits a Cost$ token's head and its <count/filter/
// description> payload into the printed words a cost prefix can contain.
func forgeCostFeatureWords(tok string) (head, desc, filter []string) {
	rawHead, payload, _ := strings.Cut(tok, "<")
	payload = strings.TrimSuffix(payload, ">")
	head = costFeatureWords(rawHead)
	parts := strings.Split(payload, "/")
	if len(parts) >= 2 {
		filter = costFeatureWords(parts[1])
	}
	if len(parts) >= 3 {
		desc = costFeatureWords(parts[len(parts)-1])
	}
	return head, desc, filter
}

// costFeatureWords lower-cases a camelCase Forge word or filter and drops the
// grammar and placeholder words that are not printed cost text.
func costFeatureWords(text string) []string {
	var b strings.Builder
	for i, r := range text {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	var out []string
	for _, w := range strings.FieldsFunc(strings.ToLower(b.String()), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	}) {
		if !costFeatureWord(w) {
			continue
		}
		out = append(out, w)
	}
	return out
}

// costFeatureWord filters placeholders, counts and grammar words out of a
// Cost$ token's printed spelling.
func costFeatureWord(w string) bool {
	switch w {
	case "xmin1", "xmin4", "xmin", "x", "cardname", "nickname", "youctrl",
		"ctrl", "p1p1", "one", "two", "three", "four", "five", "six",
		"seven", "eight", "nine", "ten", "s":
		return false
	}
	if len(w) < 3 || strings.Trim(w, "0123456789") == "" {
		return false
	}
	return true
}

// anyWordIn reports whether prefix contains any of words.
func anyWordIn(prefix string, words []string) bool {
	for _, w := range words {
		if strings.Contains(prefix, w) {
			return true
		}
	}
	return false
}

// prefixHasCostText reports whether a line's cost prefix prints words beyond
// its mana and loyalty symbols (a sacrifice, discard, tap or forge cost), so a
// candidate with no corresponding non-mana Cost$ token cannot match it. Dash
// headers ("Metalcraft — ") and pure alternatives ("{3}, {T} or {G}, {T}") do
// not count.
func prefixHasCostText(line string) bool {
	prefix := line
	if i := strings.Index(prefix, ": "); i >= 0 {
		prefix = prefix[:i]
	}
	for _, sep := range []string{" — ", " - "} {
		if i := strings.Index(prefix, sep); i > 0 {
			prefix = prefix[i+len(sep):]
		}
	}
	prefix = costBraceRE.ReplaceAllString(prefix, " ")
	prefix = loyaltyBracketRE.ReplaceAllString(prefix, " ")
	for _, w := range strings.FieldsFunc(strings.ToLower(prefix), func(r rune) bool {
		return !(r >= 'a' && r <= 'z')
	}) {
		switch w {
		case "or", "and", "then", "to":
			continue
		}
		if len(w) >= 3 {
			return true
		}
	}
	return false
}

// sameCostSymbols compares two symbol lists as multisets: Forge and Oracle
// spell the same cost in a different order ("T G" versus "{G}, {T}").
func sameCostSymbols(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	counts := make(map[string]int, len(a))
	for _, s := range a {
		counts[s]++
	}
	for _, s := range b {
		counts[s]--
	}
	for _, n := range counts {
		if n != 0 {
			return false
		}
	}
	return true
}

// forgeCostTokens splits a Forge Cost$ string on whitespace, keeping a
// token's <...> payload (which may itself contain spaces) together.
func forgeCostTokens(cost string) []string {
	var out []string
	depth, start := 0, -1
	for i, r := range cost {
		switch r {
		case '<':
			depth++
		case '>':
			if depth > 0 {
				depth--
			}
		case ' ', '\t':
			if depth == 0 {
				if start >= 0 {
					out = append(out, cost[start:i])
					start = -1
				}
				continue
			}
		}
		if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		out = append(out, cost[start:])
	}
	return out
}

// forgeManaSymbol renders one bare Forge cost token as XMage's braces, or
// reports false for a token that is not a mana symbol. Forge spells a hybrid
// or Phyrexian symbol as two letters ("RW", "GP", "2B").
func forgeManaSymbol(tok string) (string, bool) {
	if tok == "" {
		return "", false
	}
	if strings.Trim(tok, "0123456789") == "" {
		return "{" + tok + "}", true
	}
	if len(tok) == 1 && strings.Contains("WUBRGCSXQTP", tok) {
		return "{" + tok + "}", true
	}
	if len(tok) == 2 {
		switch {
		case strings.Contains("WUBRG", tok[:1]) && strings.Contains("WUBRG", tok[1:]):
			return "{" + tok[:1] + "/" + tok[1:] + "}", true
		case tok[0] == '2' && strings.Contains("WUBRG", tok[1:]):
			return "{2/" + tok[1:] + "}", true
		case strings.Contains("WUBRG", tok[:1]) && tok[1] == 'P':
			return "{" + tok[:1] + "/P}", true
		}
	}
	if strings.Count(tok, "/") == 1 {
		return oracleManaSymbol(tok)
	}
	return "", false
}

// oracleManaSymbol renders a brace-enclosed printed symbol, or reports false
// when the brace is a placeholder ({this}) rather than mana.
func oracleManaSymbol(tok string) (string, bool) {
	if tok == "" {
		return "", false
	}
	if strings.Trim(tok, "0123456789") == "" {
		return "{" + tok + "}", true
	}
	if strings.Count(tok, "/") == 1 {
		left, right, _ := strings.Cut(tok, "/")
		l, lok := oracleManaSide(left)
		r, rok := oracleManaSide(right)
		if lok && rok {
			return "{" + strings.ToUpper(l) + "/" + strings.ToUpper(r) + "}", true
		}
		return "", false
	}
	if len(tok) == 1 && strings.Contains("WUBRGCSXQTP", strings.ToUpper(tok)) {
		return "{" + strings.ToUpper(tok) + "}", true
	}
	return "", false
}

// oracleManaSide validates one half of a hybrid or Phyrexian printed symbol.
func oracleManaSide(s string) (string, bool) {
	if strings.Trim(s, "0123456789") == "" {
		return s, true
	}
	if len(s) == 1 && strings.Contains("WUBRGCSXQTP", strings.ToUpper(s)) {
		return s, true
	}
	return "", false
}

// costBraceRE and loyaltyBracketRE find a line's printed cost symbols.
var (
	costBraceRE      = regexp.MustCompile(`\{([^{}]+)\}`)
	loyaltyBracketRE = regexp.MustCompile(`\[([+-]?(?:[0-9]+|X))\]`)
)

// loyaltyCost renders Forge's bracketed loyalty counter cost as XMage's
// printed cost. Empty means this is not a loyalty cost.
func loyaltyCost(cost string) string {
	for _, part := range strings.Split(cost, ",") {
		part = strings.TrimSpace(part)
		for _, spec := range []struct{ prefix, sign string }{{"AddCounter<", "+"}, {"SubCounter<", "-"}} {
			if !strings.HasPrefix(part, spec.prefix) || !strings.HasSuffix(part, "/LOYALTY>") {
				continue
			}
			n := strings.TrimSuffix(strings.TrimPrefix(part, spec.prefix), "/LOYALTY>")
			if n == "X" {
				return "-X"
			}
			if n == "0" {
				return "0"
			}
			if spec.sign == "-" {
				return "-" + n
			}
			return "+" + n
		}
	}
	return ""
}

// extendPrefix lengthens prefix one word at a time into its own full line
// until no other line starts with it, the longer prefix XMage's startsWith
// selection needs to tell abilities apart, including -1 versus -10 costs.
func extendPrefix(prefix string, full []string, own int) string {
	line := full[own]
	for len(prefix) < len(line) && conflictsWithOtherLine(prefix, own, full) {
		end := len(prefix)
		for end < len(line) && line[end] == ' ' {
			end++
		}
		for end < len(line) && line[end] != ' ' {
			end++
		}
		prefix = line[:end]
	}
	return prefix
}

// conflictsWithOtherLine reports whether prefix would select another ability
// line under XMage's startsWith matching rule.
func conflictsWithOtherLine(prefix string, own int, lines []string) bool {
	for i, line := range lines {
		if i != own && strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

// namesShortName reports whether text mentions the short name of a comma-
// named card ("Chandra" for "Chandra, Torch of Defiance"). The Oracle prints
// it where XMage's rule text has {this}, so an extension through it could not
// match and the mapping fails closed.
func namesShortName(text, name string) bool {
	short, _, ok := strings.Cut(name, ",")
	return ok && short != "" && strings.Contains(strings.ToLower(text), strings.ToLower(short))
}

// nonKeywordAbilities lists the indices of f's activated ABs that are not
// keyword-expanded, in IR order; withIntrinsic keeps the injected basic-land
// mana abilities.
func nonKeywordAbilities(f *cards.Face, withIntrinsic bool) []int {
	var out []int
	for i, sa := range f.Abilities {
		if sa.IsActivated() && sa.ParamStr(cards.PKKeyword) == "" && (withIntrinsic || !intrinsicLandMana(sa)) {
			out = append(out, i)
		}
	}
	return out
}

// intrinsicLandMana identifies the mana ability cards.ApplyIntrinsics injects
// for a basic land subtype (CR 305.6), by the marker the injector stamps on
// it, so a land's own printed "{T}: Add {C}." ability is never mistaken for one.
func intrinsicLandMana(sa *cards.SA) bool {
	return sa.Line == intrinsicManaLine
}

// intrinsicManaLine is the SA.Line cards.IntrinsicManaAbility stamps on the
// ability it builds.
const intrinsicManaLine = "intrinsic: basic land mana"

func intrinsicManaText(sa *cards.SA) string {
	return fmt.Sprintf("{T}: Add {%s}.", strings.TrimSpace(sa.ParamStr(cards.PKProduced)))
}

// oracleLines splits a face's Oracle text into its printed lines. The corpus
// stores the line separators as the two characters "\n" (a backslash and an
// n), not a real newline; a card built directly by a test may carry either,
// so a real newline is normalised to the corpus spelling before splitting.
func oracleLines(oracle string) []string {
	return strings.Split(strings.ReplaceAll(oracle, "\n", `\n`), `\n`)
}

// abilityLines lists the oracle lines that are activated-ability lines, in
// order: a line whose reminder text is stripped still contains ": " and does
// not start with "(". Stripping the trailing parenthetical reminder is what
// keeps a keyword line ("Cycling {2} ({2}, Discard this card: Draw a card.)")
// out of the list; the whole-line parenthetical a conditional mana ability is
// granted under ("({T}: Add {G} or {U}.)") is excluded by the leading "(".
func abilityLines(oracle string) []string {
	var out []string
	gated := false
	for _, raw := range oracleLines(oracle) {
		line := spaceAfterCostColon(strings.TrimSpace(raw))
		if line == "" || strings.HasPrefix(line, "(") {
			continue
		}
		// Forge models the abilities printed under a "LEVEL N-M" / "STATION N+"
		// header (the rest of the face) as SVar grants, and "Max speed —"
		// lines as nothing at all, so neither has an AB to pair with.
		gated = gated || levelHeaderRE.MatchString(line)
		if gated || strings.HasPrefix(line, "Max speed — ") {
			continue
		}
		colon := strings.Index(line, ": ")
		if colon < 0 || !strings.Contains(stripReminder(line), ": ") {
			continue
		}
		// A quoted granted ability ("... and has \"{T}, Sacrifice this
		// creature: ...\"") carries its own ": " inside the quotes; it is
		// reminder text for a different object, not this face's ability line.
		if q := strings.IndexByte(line, '"'); q >= 0 && q < colon {
			continue
		}
		out = append(out, line)
	}
	return out
}

// levelHeaderRE matches the "LEVEL 1-2" / "LEVEL 8+" / "STATION 12+" line
// that opens a leveler's or Spacecraft's granted-ability sections.
var levelHeaderRE = regexp.MustCompile(`^(LEVEL|STATION) \d+(-\d+|\+)$`)

// costColonRE finds the cost/effect colon of a line printed without the
// space after it ("{T}:Draw a card.", "[-3]:You draw"), which a braced cost or
// a bracketed loyalty cost always precedes.
var costColonRE = regexp.MustCompile(`([}\]]):([^ ])`)

// spaceAfterCostColon restores the space after the cost colon, only on the
// first such colon so a colon inside the effect text is left alone.
func spaceAfterCostColon(line string) string {
	loc := costColonRE.FindStringSubmatchIndex(line)
	if loc == nil || strings.Contains(line[:loc[0]], ": ") {
		return line
	}
	return line[:loc[1]-1] + " " + line[loc[1]-1:]
}

// stripReminder removes a line's trailing parenthetical reminder text, the
// "(...)" that follows the printed rules text of a keyword or ability line.
// Only a trailing " (...)" is removed, so a line whose own text carries a
// parenthesis is untouched.
func stripReminder(line string) string {
	for {
		i := strings.LastIndex(line, " (")
		if i < 0 || !strings.HasSuffix(line, ")") {
			return strings.TrimSpace(line)
		}
		line = strings.TrimSpace(line[:i])
	}
}

// linePrefix returns the text before the line's first ": ", the cost prefix
// XMage matches on, with the card's name rewritten by selfRef and the cost spelled as XMage
// renders it (xmageCostText).
func linePrefix(line, sourceName string) string {
	i := strings.Index(line, ": ")
	if i < 0 {
		return ""
	}
	return xmageCostText(selfRef(strings.TrimSpace(line[:i]), sourceName), sourceName)
}

// selfRef rewrites every mention of the card's name in text to {this}:
// XMage's no-argument AbilityImpl.getRule() leaves the source placeholder
// literal, so a self-referential cost or effect is rendered with {this}, not
// the card's printed name.
func selfRef(text, sourceName string) string {
	if sourceName == "" {
		return text
	}
	lowerText, lowerName := strings.ToLower(text), strings.ToLower(sourceName)
	for from := 0; ; {
		rel := strings.Index(lowerText[from:], lowerName)
		if rel < 0 {
			break
		}
		start := from + rel
		text = text[:start] + "{this}" + text[start+len(sourceName):]
		lowerText = lowerText[:start] + "{this}" + lowerText[start+len(sourceName):]
		from = start + len("{this}")
	}
	return text
}

func removeKeywordAbilityLines(f *cards.Face, lines []string) []string {
	owned := make(map[string]bool)
	for _, sa := range f.Abilities {
		if !sa.IsActivated() || sa.ParamStr(cards.PKKeyword) == "" {
			continue
		}
		if line, ok := keywordPrefix(f, sa); ok {
			owned[line] = true
		}
	}
	out := lines[:0]
	for _, line := range lines {
		if !owned[strings.TrimSpace(line)] {
			out = append(out, line)
		}
	}
	return out
}

// keywordPrefix finds the keyword-expanded AB's printed line and returns its
// reminder-stripped text. For ordinary keyword abilities the line starts with
// the keyword's printed name, bounded so "Crew" does not match "Crewmate";
// Class, TypeCycling and job-named Equip use their distinct Oracle shapes.
func keywordPrefix(f *cards.Face, sa *cards.SA) (string, bool) {
	head := sa.ParamStr(cards.PKKeyword)
	var lines []string
	for _, raw := range oracleLines(f.Oracle) {
		lines = append(lines, stripReminder(strings.TrimSpace(raw)))
	}
	var matches []string
	if head == "TypeCycling" {
		// One printed line lists every landcycling ("Swampcycling {2},
		// mountaincycling {2}") and XMage prints each as its own ability.
		matches = keywordMatches(head, sa, commaSegments(lines))
	} else if matches = keywordMatches(head, sa, lines); len(matches) == 0 {
		// A comma-joined keyword list ("Madness {R}, cycling {1}{R}, ...").
		matches = keywordMatches(head, sa, commaSegments(lines))
	}
	// Prose that merely names the keyword ("Equip abilities you activate of
	// other Equipment cost {1} less...") also starts with it, and a face may
	// print the keyword twice ("Equip Detective {1}", "Equip {3}"). Only when
	// that leaves several candidates does the printed line decide: first the
	// line that spells this ability's leading cost symbol (or, for a cost with
	// none, the line whose cost is a dash-led "Equip—Sacrifice ..."), then the
	// line whose cost follows the keyword name directly. Forge's cost spelling
	// is never required to round-trip -- it collapses hybrid, Phyrexian, tap and
	// min-count symbols -- so a narrowing that finds nothing is skipped.
	if len(matches) > 1 {
		if symbol := leadingCostSymbol(sa.ParamStr(cards.PKCost)); symbol != "" {
			matches = narrowMatches(matches, func(line string) bool { return strings.Contains(line, symbol) })
		} else {
			matches = narrowMatches(matches, func(line string) bool { return dashLed(keywordCandidate(line), head) })
		}
	}
	if len(matches) > 1 {
		matches = narrowMatches(matches, func(line string) bool { return costLed(keywordCandidate(line), head) })
	}
	if len(matches) == 0 {
		return printedEquip(sa)
	}
	if len(matches) != 1 {
		return "", false
	}
	return xmageKeywordText(head, sa, matches[0])
}

// keywordMatches returns the lines that print this keyword ability.
func keywordMatches(head string, sa *cards.SA, lines []string) []string {
	var matches []string
	for _, line := range lines {
		switch head {
		case "TypeCycling":
			// Forge's TypeCycling covers basic-landcycling, arbitrary
			// typecycling (for example Halflingcycling) and dotted type
			// specs (Sojourner's Companion's Land.Artifact, printed
			// "Artifact landcycling").
			word := typeCyclingWord(sa.ParamStr(cards.PKChangeType))
			if word != "" && strings.HasPrefix(strings.ToLower(line), strings.ToLower(word+"cycling")) {
				matches = append(matches, line)
			}
		case "Class":
			// A Class level-up AB is printed as "{cost}: Level N", not as
			// a line beginning with the keyword name.
			if strings.HasSuffix(strings.ToLower(line), ": level "+sa.ParamStr(cards.PKLevel)) {
				matches = append(matches, line)
			}
		default:
			// Job-named equipment prints "Job — Equip {N}". Match the
			// keyword after that printed header, while preserving the whole
			// XMage selector text.
			if keywordLineStartsWith(keywordCandidate(line), head) {
				matches = append(matches, line)
			}
		}
	}
	return matches
}

// typeCyclingWord renders the printed word a TypeCycling AB's line begins
// with. "Basic" is printed "Basic land"; a dotted spec (Land.Artifact) is
// printed in English word order, the reverse of the dotted order.
func typeCyclingWord(spec string) string {
	spec = strings.TrimSpace(spec)
	if strings.EqualFold(spec, "Basic") {
		return "Basic land"
	}
	parts := strings.Split(spec, ".")
	if len(parts) < 2 {
		return spec
	}
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return strings.ToLower(strings.Join(parts, " "))
}

// commaSegments splits every line that lists several comma-separated
// abilities into those abilities, first letter upper-cased as XMage prints it.
func commaSegments(lines []string) []string {
	var out []string
	for _, line := range lines {
		for _, seg := range strings.Split(line, ", ") {
			if seg = strings.TrimSpace(seg); seg != "" {
				out = append(out, strings.ToUpper(seg[:1])+seg[1:])
			}
		}
	}
	return out
}

// xmageKeywordText spells the matched printed line as XMage renders the
// ability. A mana cost reads "Cycling {2}" in both; a non-mana cost is printed
// "Equip—Sacrifice a creature." by EquipAbility and CyclingAbility
// ("&mdash;"), and "Eternalize {2}{W}{W}, Discard a card" by EternalizeAbility.
// A compound mana-and-other Equip cost is printed mana-first
// ("Equip—{2}, Pay 2 life.") but rendered non-mana-first
// ("Equip&mdash;Pay 2 life.{2}"), so its arm reorders the printed parts; a
// mana-first Cycling dash line has no rendering (CyclingAbility holds one
// cost), and other keywords' dash-led shapes fail closed.
func xmageKeywordText(head string, sa *cards.SA, line string) (string, bool) {
	candidate := keywordCandidate(line)
	if !keywordLineStartsWith(candidate, head) {
		// Class and TypeCycling lines do not begin with the AB's keyword name.
		return line, true
	}
	header := flavorHeader(line[:len(line)-len(candidate)])
	name, rest := candidate[:len(head)], candidate[len(head):]
	switch {
	case strings.HasPrefix(rest, ":"):
		return header + name + " " + strings.TrimSpace(rest[1:]), true
	case !strings.HasPrefix(rest, "—") && !strings.HasPrefix(rest, "-"):
		return header + candidate, true
	}
	cost := strings.TrimLeft(rest, "—-")
	switch head {
	case "Equip", "Cycling":
		symbol := leadingCostSymbol(sa.ParamStr(cards.PKCost))
		if symbol == "" {
			return header + name + "&mdash;" + cost, true
		}
		if !strings.Contains(cost, symbol) {
			// A dash-led line that does not spell the ability's leading mana
			// symbol belongs to another ability; printedEquip supplies the
			// plain "Equip {N}" text.
			return "", false
		}
		if !strings.HasPrefix(cost, symbol) {
			// Word-led alternate-cost form ("Equip—Pay {3} or discard a
			// card.", Bloodthorn Flail's OrCost): the printed order is
			// XMage's order.
			return header + name + "&mdash;" + cost, true
		}
		if head != "Equip" {
			// CyclingAbility holds one cost field, so a mana-first Cycling
			// dash line has no XMage rendering.
			return "", false
		}
		// A compound mana-plus-non-mana cost is rendered by XMage's
		// EquipAbility with the non-mana part first, so spell that order from
		// the printed line.
		reordered, ok := reorderCompoundEquipCost(cost, symbol)
		if !ok {
			return "", false
		}
		return header + name + "&mdash;" + reordered, true
	case "Eternalize":
		return header + name + " " + strings.TrimSuffix(cost, "."), true
	}
	return "", false
}

// reorderCompoundEquipCost spells a printed mana-first compound Equip cost
// ("{2}, Pay 2 life.") in XMage's EquipAbility order, non-mana text first and
// the mana run last ("Pay 2 life.{2}"), spelled from the printed cost. The
// printed mana run must begin with the ability's leading Forge symbol and be
// nothing but printed mana symbols; anything else fails closed.
func reorderCompoundEquipCost(cost, symbol string) (string, bool) {
	mana, rest, ok := strings.Cut(cost, ", ")
	if !ok || rest == "" || !strings.HasPrefix(mana, symbol) || !manaSymbolRun(mana) {
		return "", false
	}
	return strings.TrimSuffix(rest, ".") + "." + mana, true
}

// manaSymbolRun reports whether s is one or more printed mana symbols
// ("{2}", "{1}{U}").
func manaSymbolRun(s string) bool {
	for s != "" {
		if !strings.HasPrefix(s, "{") {
			return false
		}
		end := strings.Index(s, "}")
		if end < 0 || !isManaCostSymbol(s[1:end]) {
			return false
		}
		s = s[end+1:]
	}
	return true
}

// flavorHeader spells a printed "Job — " header as AbilityImpl.addRulePrefix
// renders a flavor word (CardUtil.italicizeWithEmDash).
func flavorHeader(header string) string {
	word := strings.TrimSuffix(header, " — ")
	if word == header {
		return header
	}
	return "<i>" + word + "</i> &mdash; "
}

// printedEquip spells an Equip ability whose Oracle text prints no Equip line
// at all (Forge omits it from some cards' Oracle: Buster Sword, Glamdring) as
// XMage renders a plain mana cost: "Equip {2}".
func printedEquip(sa *cards.SA) (string, bool) {
	if sa.ParamStr(cards.PKKeyword) != "Equip" {
		return "", false
	}
	fields := strings.Fields(sa.ParamStr(cards.PKCost))
	var text strings.Builder
	for _, field := range fields {
		if !isManaCostSymbol(field) {
			return "", false
		}
		text.WriteString("{" + field + "}")
	}
	if text.Len() == 0 {
		return "", false
	}
	return "Equip " + text.String(), true
}

// dashLed reports whether the keyword's cost is a dash-led non-mana cost
// ("Equip—Sacrifice a creature").
func dashLed(line, display string) bool {
	rest := strings.TrimSpace(line[len(display):])
	return strings.HasPrefix(rest, "—") || strings.HasPrefix(rest, "-")
}

// keywordCandidate strips a Job-style header ("Perseus's Bow — Equip {6}")
// from a printed line, leaving the text that begins with the keyword name.
func keywordCandidate(line string) string {
	if dash := strings.LastIndex(line, " — "); dash >= 0 {
		return strings.TrimSpace(line[dash+len(" — "):])
	}
	return line
}

// costLed reports whether the keyword's cost follows its printed name
// directly ("Equip {1}{R}", "Equip—Sacrifice a creature"), as opposed to prose.
func costLed(line, display string) bool {
	rest := strings.TrimSpace(line[len(display):])
	return rest == "" || strings.HasPrefix(rest, "{") || strings.HasPrefix(rest, "—") || strings.HasPrefix(rest, "-")
}

// leadingCostSymbol renders the first symbol of Forge's cost as a printed
// "{X}", or "" when it is not a plain symbol.
func leadingCostSymbol(cost string) string {
	fields := strings.Fields(cost)
	if len(fields) == 0 || !isManaCostSymbol(fields[0]) {
		return ""
	}
	return "{" + fields[0] + "}"
}

// narrowMatches keeps the lines keep accepts, or all of them when none does.
func narrowMatches(lines []string, keep func(string) bool) []string {
	var out []string
	for _, line := range lines {
		if keep(line) {
			out = append(out, line)
		}
	}
	if len(out) == 0 {
		return lines
	}
	return out
}

func isManaCostSymbol(symbol string) bool {
	if symbol == "" {
		return false
	}
	for _, r := range symbol {
		if !((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '/') {
			return false
		}
	}
	return true
}

// keywordLineStartsWith reports whether line begins with the printed keyword
// name display.
func keywordLineStartsWith(line, display string) bool {
	if len(line) < len(display) || !strings.EqualFold(line[:len(display)], display) {
		return false
	}
	if len(line) == len(display) {
		return true
	}
	switch line[len(display)] {
	case ' ', '{', ':':
		return true
	}
	// "Equip—Sacrifice a creature": the dash is a multi-byte rune.
	return dashLed(line, display)
}
