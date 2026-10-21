package mtgish

import (
	"sort"
	"strconv"
	"strings"
)

// The canonical vocabulary both normalizers emit. Keeping the shared
// spellings here makes the two halves agree by construction.

// manaSymbol is one canonical cost symbol: "W", "X", "W/U", "W/P", "2/W",
// or a generic amount ("3").
type manaCost struct {
	generic int
	hasNum  bool
	syms    []string
}

func (m *manaCost) add(sym string) {
	if n, err := strconv.Atoi(sym); err == nil {
		m.generic += n
		m.hasNum = true
		return
	}
	if strings.Contains(sym, "/") {
		// A hybrid's halves in a fixed order (Forge writes "G/W" where
		// mtgish writes "GW"), with a phyrexian P last.
		var parts []string
		phyrexian := false
		for _, p := range strings.Split(sym, "/") {
			if p == "P" {
				phyrexian = true
			} else {
				parts = append(parts, p)
			}
		}
		sort.Strings(parts)
		if phyrexian {
			parts = append(parts, "P")
		}
		sym = strings.Join(parts, "/")
	}
	m.syms = append(m.syms, sym)
}

// symbolOrder sorts X first, then the colours in WUBRG order, then the rest.
func symbolOrder(s string) string {
	const order = "XWUBRGCS"
	if i := strings.IndexByte(order, s[0]); i >= 0 && len(s) == 1 {
		return string(rune('a'+i)) + s
	}
	return "z" + s
}

func (m *manaCost) String() string {
	if !m.hasNum && len(m.syms) == 0 {
		return ""
	}
	syms := append([]string(nil), m.syms...)
	sort.Slice(syms, func(i, j int) bool { return symbolOrder(syms[i]) < symbolOrder(syms[j]) })
	var b strings.Builder
	xs := 0
	for _, s := range syms {
		if s == "X" {
			b.WriteString("{X}")
			xs++
		}
	}
	if m.generic > 0 || (m.hasNum && len(syms) == 0) {
		b.WriteString("{" + strconv.Itoa(m.generic) + "}")
	}
	for _, s := range syms[xs:] {
		b.WriteString("{" + s + "}")
	}
	return b.String()
}

// typeWord canonicalises a type-line word: lower case, letters only, so
// "Time Lord" and mtgish's "TimeLord" agree.
func typeWord(w string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(w) {
		if r >= 'a' && r <= 'z' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// keywordWord canonicalises a keyword name the same way ("First Strike" and
// "FirstStrike" are "firststrike").
func keywordWord(w string) string { return typeWord(w) }

// signed renders a P/T modifier: "+1", "-2", "+0".
func signed(n int) string {
	if n < 0 {
		return strconv.Itoa(n)
	}
	return "+" + strconv.Itoa(n)
}

// pump is the T4 token for a power/toughness modification; "*" when either
// half is not a literal.
func pump(p, t string) string {
	if p == "*" || t == "*" {
		return "pump:*"
	}
	return "pump:" + p + "/" + t
}

// counterName canonicalises a counter type: "+1/+1", "-1/-1", "stun",
// "flying", "loyalty".
func counterName(s string) string { return strings.ToLower(s) }

// The target classes.
const (
	clsAny       = "any"
	clsPlayer    = "player"
	clsSpell     = "spell"
	clsAbility   = "ability"
	clsGraveyard = "card-graveyard"
	clsExile     = "card-exile"
	clsPermanent = "permanent"
)

// permanentTypes are the card types a permanent target class may name.
var permanentTypes = map[string]bool{
	"artifact": true, "battle": true, "creature": true, "enchantment": true,
	"land": true, "planeswalker": true,
}

// targetToken renders one target slot: the class (alternatives joined by
// "|", sorted) and, unless it is exactly one, the count ("x0..1", "x2",
// "x0..*").
func targetToken(classes []string, count string) string {
	cs := anyTargetClasses(dedup(classes))
	if len(cs) == 0 {
		cs = []string{clsPermanent}
	}
	t := strings.Join(cs, "|")
	// A count that is not a literal ("X target creatures", or a script's
	// gift/kicker-dependent X) is not compared, like a non-literal amount.
	if count != "" && count != "1" && count != "*" {
		t += "x" + count
	}
	return t
}

func dedup(in []string) []string {
	s := sorted(in)
	out := s[:0]
	for i, v := range s {
		if v != "" && (i == 0 || v != s[i-1]) {
			out = append(out, v)
		}
	}
	return out
}

// countRange renders a min/max target count.
func countRange(min, max string) string {
	if min == max {
		return min
	}
	return min + ".." + max
}
