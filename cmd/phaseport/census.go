package main

import (
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// Classes of a phase.rs test, by how it populates the GameScenario builder.
const (
	classRealOnly    = "real-only"    // every object is add_real_card
	classRealVanilla = "real+vanilla" // real cards plus add_creature(P, name, p, t) bodies only
	classSynthetic   = "synthetic"    // no real card, or any Oracle-text / other synthetic object
	classUnparsed    = "unparsed"     // the builder is built or driven outside the test body
)

var classOrder = []string{classRealOnly, classRealVanilla, classSynthetic, classUnparsed}

// testRow is one census row: one #[test] fn.
type testRow struct {
	File    string
	Name    string
	Class   string
	Cards   []string // real cards, source order, de-duplicated
	GorgeOK bool     // at least one real card and every real card is supported by gorge
	Calls   []string // sorted unique builder / runner calls
	Asserts []string // sorted unique assert kinds
	CRs     []string // sorted unique CR citations
}

// cardSupport reports whether gorge has the card (found) and every one of its
// effects is implemented (supported).
type cardSupport func(name string) (supported, found bool)

// maskRust returns src with comments blanked and string/char literal
// interiors blanked (delimiters kept, newlines kept, byte length preserved),
// so brace and paren matching and call scanning cannot be fooled by text.
// Offsets in the result index the original source directly.
func maskRust(src string) string {
	b := []byte(src)
	blank := func(from, to int) {
		for k := from; k < to && k < len(b); k++ {
			if b[k] != '\n' {
				b[k] = ' '
			}
		}
	}
	isIdent := func(c byte) bool {
		return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
	}
	n := len(src)
	for i := 0; i < n; i++ {
		c := src[i]
		switch {
		case c == '/' && i+1 < n && src[i+1] == '/':
			j := i
			for j < n && src[j] != '\n' {
				j++
			}
			blank(i, j)
			i = j - 1
		case c == '/' && i+1 < n && src[i+1] == '*':
			depth, j := 1, i+2
			for j < n && depth > 0 {
				switch {
				case src[j] == '/' && j+1 < n && src[j+1] == '*':
					depth++
					j += 2
				case src[j] == '*' && j+1 < n && src[j+1] == '/':
					depth--
					j += 2
				default:
					j++
				}
			}
			blank(i, j)
			i = j - 1
		case (c == 'r' || c == 'b') && (i == 0 || !isIdent(src[i-1])):
			// raw string: r"..", r#".."#, br#".."#
			j := i
			if c == 'b' && j+1 < n && src[j+1] == 'r' {
				j++
			}
			if src[j] != 'r' {
				continue
			}
			k := j + 1
			for k < n && src[k] == '#' {
				k++
			}
			if k >= n || src[k] != '"' {
				continue
			}
			hashes := k - (j + 1)
			end := strings.Index(src[k+1:], `"`+strings.Repeat("#", hashes))
			if end < 0 {
				blank(k+1, n)
				i = n
				break
			}
			blank(k+1, k+1+end)
			i = k + 1 + end + hashes
		case c == '"':
			j := i + 1
			for j < n && src[j] != '"' {
				if src[j] == '\\' {
					j++
				}
				j++
			}
			if j > n {
				j = n
			}
			blank(i+1, j)
			i = j
		case c == '\'':
			// char literal, unless it is a lifetime ('a, 'static).
			if i+1 < n && src[i+1] == '\\' {
				j := i + 2
				for j < n && src[j] != '\'' {
					j++
				}
				blank(i+1, j)
				i = j
			} else if i+1 < n {
				_, sz := utf8.DecodeRuneInString(src[i+1:])
				if i+1+sz < n && src[i+1+sz] == '\'' {
					blank(i+1, i+1+sz)
					i = i + 1 + sz
				}
			}
		}
	}
	return string(b)
}

// matchClose returns the index of the byte closing the bracket at s[open].
func matchClose(s string, open int) int {
	o := s[open]
	var cl byte
	switch o {
	case '(':
		cl = ')'
	case '{':
		cl = '}'
	case '[':
		cl = ']'
	default:
		return -1
	}
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case o:
			depth++
		case cl:
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// matchOpen is matchClose backwards: the bracket opening the one at s[cl].
func matchOpen(s string, cl int) int {
	c := s[cl]
	var o byte
	switch c {
	case ')':
		o = '('
	case '}':
		o = '{'
	case ']':
		o = '['
	default:
		return -1
	}
	depth := 0
	for i := cl; i >= 0; i-- {
		switch s[i] {
		case c:
			depth++
		case o:
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// splitArgs splits the text between a call's parens at top-level commas. It
// scans the masked copy (strings cannot unbalance it) and slices the original.
func splitArgs(masked, s string) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(s); i++ {
		switch masked[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	if t := strings.TrimSpace(s[start:]); t != "" {
		out = append(out, t)
	}
	return out
}

var (
	reTestAttr     = regexp.MustCompile(`#\[test\]`)
	reFn           = regexp.MustCompile(`\bfn\s+([A-Za-z_][A-Za-z0-9_]*)`)
	reCall         = regexp.MustCompile(`\.\s*([A-Za-z_][A-Za-z0-9_]*)\s*(?:::\s*<[^()]*>)?\s*\(`)
	reCtor         = regexp.MustCompile(`\bGameScenario\s*::\s*([A-Za-z_][A-Za-z0-9_]*)\s*(?:::\s*<[^()]*>)?\s*\(`)
	reLetCtor      = regexp.MustCompile(`let\s+(?:mut\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*(?::[^=;]+)?=\s*GameScenario\s*::`)
	reLetBuild     = regexp.MustCompile(`let\s+(?:mut\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*(?::[^=;]+)?=\s*([A-Za-z_][A-Za-z0-9_]*)\s*\.\s*build\s*\(`)
	reAssert       = regexp.MustCompile(`\b(assert_eq|assert_ne|assert|debug_assert_eq|debug_assert_ne|debug_assert|matches)\s*!\s*\(`)
	reCR           = regexp.MustCompile(`\bCR\s*(\d{3}(?:\.\d+[a-z]?)?)`)
	reFreeCall     = regexp.MustCompile(`(?:^|[^.\w:])([a-z_][A-Za-z0-9_]*)\s*\(`)
	reParamBuilder = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)\s*:\s*&\s*(?:mut\s+)?GameScenario\b`)
	reParamRunner  = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)\s*:\s*&\s*(?:mut\s+)?GameRunner\b`)
	reLetRunner    = regexp.MustCompile(`let\s+(?:mut\s+)?([A-Za-z_]*runner[A-Za-z0-9_]*)\b`)
	reConstStr     = regexp.MustCompile(`(?:const|static)\s+([A-Za-z_][A-Za-z0-9_]*)\s*:\s*&\s*(?:'static\s+)?str\s*=\s*"((?:[^"\\]|\\.)*)"`)
	reIdent        = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)
	reLeadLetter   = regexp.MustCompile(`^[a-z_][A-Za-z0-9_]*$`)
)

// notObjects are builder `add_*` calls that place no object.
var notObjects = map[string]bool{
	"add_transient_continuous_effect":               true,
	"add_transient_continuous_effect_with_bindings": true,
	"add_mana_to_pool":                              true,
	"add_player_counters":                           true,
	"add_priority_yield":                            true,
}

// fileCtx is one source file plus the bodies of its non-test fns, so a test
// that builds its scenario in a same-file helper is read through the helper.
type fileCtx struct {
	src, masked string
	helpers     map[string]*fnSpan // nil value: the name is defined more than once
	consts      map[string]string  // file-level const NAME: &str = ".."
}

type fnSpan struct {
	open, cl  int
	retRunner bool // the signature returns a GameRunner
	isTestFn  bool
}

// censusFile classifies every #[test] fn in one Rust source file.
func censusFile(file, src string, supp cardSupport) []testRow {
	ctx := &fileCtx{src: src, masked: maskRust(src), helpers: map[string]*fnSpan{}, consts: map[string]string{}}
	for _, m := range reConstStr.FindAllStringSubmatch(src, -1) {
		if v, ok := rustString(`"` + m[2] + `"`); ok {
			ctx.consts[m[1]] = v
		}
	}
	type testFn struct {
		name string
		attr int
		span fnSpan
	}
	var tests []testFn
	testAttrs := reTestAttr.FindAllStringIndex(ctx.masked, -1)
	for _, fm := range reFn.FindAllStringSubmatchIndex(ctx.masked, -1) {
		name := ctx.masked[fm[2]:fm[3]]
		nameEnd := fm[3]
		po := strings.IndexByte(ctx.masked[nameEnd:], '(')
		if po < 0 {
			continue
		}
		pc := matchClose(ctx.masked, nameEnd+po)
		if pc < 0 {
			continue
		}
		bo := strings.IndexAny(ctx.masked[pc:], "{;")
		if bo < 0 || ctx.masked[pc+bo] != '{' {
			continue
		}
		open := pc + bo
		cl := matchClose(ctx.masked, open)
		if cl < 0 {
			continue
		}
		sp := fnSpan{open: open, cl: cl, retRunner: strings.Contains(ctx.masked[pc:open], "GameRunner")}
		// a #[test] attribute directly before this fn (only attributes between).
		attr := -1
		for _, ta := range testAttrs {
			if ta[1] <= fm[0] && strings.TrimSpace(stripAttrs(ctx.masked[ta[1]:fm[0]])) == "" {
				attr = ta[0]
			}
		}
		if attr >= 0 {
			tests = append(tests, testFn{name, attr, sp})
			continue
		}
		if _, dup := ctx.helpers[name]; dup {
			ctx.helpers[name] = nil
		} else {
			ctx.helpers[name] = &sp
		}
	}
	var rows []testRow
	for _, t := range tests {
		row := ctx.census(t.span, supp)
		row.File, row.Name = file, t.name
		row.CRs = appendCRs(row.CRs, src[leadingComments(src, t.attr):t.span.cl+1])
		row.CRs = sortedUnique(row.CRs)
		rows = append(rows, row)
	}
	return rows
}

// stripAttrs removes `#[...]` attributes and `pub`/`async`/`unsafe` modifiers.
func stripAttrs(s string) string {
	for {
		i := strings.Index(s, "#[")
		if i < 0 {
			break
		}
		j := matchClose(s, i+1)
		if j < 0 {
			break
		}
		s = s[:i] + " " + s[j+1:]
	}
	for _, w := range []string{"pub(crate)", "pub", "async", "unsafe"} {
		s = strings.ReplaceAll(s, w, "")
	}
	return s
}

// leadingComments returns the start of the comment/attribute block directly
// above the byte offset at.
func leadingComments(src string, at int) int {
	start := at
	for start > 0 {
		ls := strings.LastIndexByte(src[:start-1], '\n') + 1
		line := strings.TrimSpace(src[ls : start-1])
		if !strings.HasPrefix(line, "//") && !strings.HasPrefix(line, "#[") {
			break
		}
		start = ls
	}
	return start
}

func appendCRs(dst []string, text string) []string {
	for _, m := range reCR.FindAllStringSubmatch(text, -1) {
		dst = append(dst, m[1])
	}
	return dst
}

func sortedUnique(xs []string) []string {
	if len(xs) == 0 {
		return nil
	}
	sort.Strings(xs)
	out := xs[:1]
	for _, x := range xs[1:] {
		if x != out[len(out)-1] {
			out = append(out, x)
		}
	}
	return out
}

// bodyInfo is what one fn body (plus the helpers it calls) puts on the scenario.
type bodyInfo struct {
	cards              []string // real cards, source order, de-duplicated
	vanilla, synthetic int
	calls, asserts     map[string]bool
	hasCtor, escaped   bool
}

func (b *bodyInfo) addCard(c string) {
	for _, x := range b.cards {
		if x == c {
			return
		}
	}
	b.cards = append(b.cards, c)
}

func (b *bodyInfo) merge(o *bodyInfo) {
	for _, c := range o.cards {
		b.addCard(c)
	}
	b.vanilla += o.vanilla
	b.synthetic += o.synthetic
	for k := range o.calls {
		b.calls[k] = true
	}
	for k := range o.asserts {
		b.asserts[k] = true
	}
	b.hasCtor = b.hasCtor || o.hasCtor
	b.escaped = b.escaped || o.escaped
}

func (c *fileCtx) census(sp fnSpan, supp cardSupport) testRow {
	info := c.analyse(sp, map[int]bool{})
	row := testRow{Cards: info.cards, Calls: sortedKeys(info.calls), Asserts: sortedKeys(info.asserts)}
	switch {
	case !info.hasCtor || info.escaped:
		row.Class = classUnparsed
	case len(info.cards) == 0 || info.synthetic > 0:
		row.Class = classSynthetic
	case info.vanilla == 0:
		row.Class = classRealOnly
	default:
		row.Class = classRealVanilla
	}
	if len(info.cards) > 0 {
		row.GorgeOK = true
		for _, card := range info.cards {
			ok, found := false, false
			if card != "<dynamic>" {
				ok, found = supp(card)
			}
			if !ok || !found {
				row.GorgeOK = false
			}
		}
	}
	row.CRs = appendCRs(nil, c.src[sp.open:sp.cl+1])
	return row
}

var rustWords = map[string]bool{"if": true, "while": true, "for": true, "match": true, "fn": true, "return": true,
	"loop": true, "in": true, "as": true, "let": true, "Some": true, "Ok": true, "Err": true, "Box": true, "move": true}

// analyse reads the fn body sp, following calls to same-file helpers once each
// (visited breaks cycles and double-counting).
func (c *fileCtx) analyse(sp fnSpan, visited map[int]bool) *bodyInfo {
	visited[sp.open] = true
	body := c.masked[sp.open : sp.cl+1]
	obody := c.src[sp.open : sp.cl+1]
	sig := c.masked[max(0, sp.open-400):sp.open]
	if k := strings.LastIndex(sig, "fn "); k >= 0 {
		sig = sig[k:]
	}

	info := &bodyInfo{calls: map[string]bool{}, asserts: map[string]bool{}}
	builders := map[string]bool{}
	for _, m := range reLetCtor.FindAllStringSubmatch(body, -1) {
		builders[m[1]] = true
	}
	for _, m := range reParamBuilder.FindAllStringSubmatch(sig, -1) {
		builders[m[1]] = true
	}
	info.hasCtor = reCtor.MatchString(body)
	runners := map[string]bool{}
	for _, m := range reParamRunner.FindAllStringSubmatch(sig, -1) {
		runners[m[1]] = true
	}
	for _, m := range reLetBuild.FindAllStringSubmatch(body, -1) {
		if builders[m[2]] {
			runners[m[1]] = true
		}
	}
	for _, m := range reLetRunner.FindAllStringSubmatch(body, -1) {
		runners[m[1]] = true
	}

	// chain[i] = kind of the call whose closing paren is at body[i]: 'b' keeps
	// a builder chain going, 'r' marks the runner a build() returned.
	chain := map[int]byte{}
	for _, m := range reCtor.FindAllStringSubmatchIndex(body, -1) {
		info.calls["GameScenario::"+body[m[2]:m[3]]] = true
		if cl := matchClose(body, m[1]-1); cl >= 0 {
			chain[cl] = 'b'
		}
	}

	// Free calls: a same-file helper is read through; any other fn handed the
	// builder is opaque, so the test cannot be read from here.
	for _, m := range reFreeCall.FindAllStringSubmatchIndex(body, -1) {
		name := body[m[2]:m[3]]
		if rustWords[name] {
			continue
		}
		openP := m[1] - 1
		closeP := matchClose(body, openP)
		if closeP < 0 {
			continue
		}
		if h, ok := c.helpers[name]; ok {
			if h != nil && !visited[h.open] {
				info.merge(c.analyse(*h, visited))
			}
			// the helper's returned runner is driven by this test.
			if h != nil && h.retRunner {
				lm := reLetFromHelper(name).FindStringSubmatch(body)
				if lm != nil {
					for _, id := range reIdent.FindAllString(lm[1], -1) {
						if strings.Contains(id, "runner") {
							runners[id] = true
						}
					}
				}
			}
			continue
		}
		for _, a := range splitArgs(body[openP+1:closeP], body[openP+1:closeP]) {
			a = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(a), "&")), "mut "))
			if builders[a] {
				info.escaped = true
			}
		}
	}

	for _, m := range reCall.FindAllStringSubmatchIndex(body, -1) {
		callName := body[m[2]:m[3]]
		recv := receiver(body, m[0], chain)
		var kind byte
		switch {
		case recv == "chain-b", builders[recv]:
			kind = 'b'
		case recv == "chain-r", runners[recv]:
			kind = 'r'
		}
		if kind == 0 {
			continue
		}
		openP := m[1] - 1
		closeP := matchClose(body, openP)
		if kind == 'r' {
			info.calls["runner."+callName] = true
			continue
		}
		info.calls[callName] = true
		if closeP >= 0 {
			if callName == "build" {
				chain[closeP] = 'r'
			} else {
				chain[closeP] = 'b'
			}
		}
		if !strings.HasPrefix(callName, "add_") || notObjects[callName] || closeP < 0 {
			continue
		}
		args := splitArgs(body[openP+1:closeP], obody[openP+1:closeP])
		switch {
		case callName == "add_real_card":
			card := "<dynamic>"
			if len(args) > 1 {
				if s, ok := rustString(args[1]); ok {
					card = s
				} else if v, ok := c.consts[strings.TrimSpace(args[1])]; ok {
					card = v
				}
			}
			info.addCard(card)
		case callName == "add_creature" && len(args) == 4:
			info.vanilla++
		default:
			info.synthetic++
		}
	}

	for _, m := range reAssert.FindAllStringSubmatchIndex(body, -1) {
		openP := m[1] - 1
		closeP := matchClose(body, openP)
		if closeP < 0 {
			continue
		}
		info.asserts[assertKind(body[m[2]:m[3]], splitArgs(body[openP+1:closeP], obody[openP+1:closeP]))] = true
	}
	if info.escaped {
		info.calls["<builder-escapes>"] = true
	}
	return info
}

// receiver names what a `.call(` at body[at] is invoked on: an identifier, or
// "chain-b"/"chain-r" when it continues a builder call / a build() result.
func receiver(body string, at int, chain map[int]byte) string {
	i := at - 1
	for i >= 0 && (body[i] == ' ' || body[i] == '\t' || body[i] == '\n' || body[i] == '\r' || body[i] == '?') {
		i--
	}
	if i < 0 {
		return ""
	}
	if body[i] == ')' {
		switch chain[i] {
		case 'b':
			return "chain-b"
		case 'r':
			return "chain-r"
		}
		return ""
	}
	end := i + 1
	for i >= 0 && (body[i] == '_' || body[i] >= '0' && body[i] <= '9' || body[i] >= 'a' && body[i] <= 'z' || body[i] >= 'A' && body[i] <= 'Z') {
		i--
	}
	if end-(i+1) == 0 {
		return ""
	}
	// a field (x.scenario.add_..) is not the builder variable.
	j := i
	for j >= 0 && (body[j] == ' ' || body[j] == '\n' || body[j] == '\t') {
		j--
	}
	if j >= 0 && (body[j] == '.' || body[j] == ':') {
		return ""
	}
	return body[i+1 : end]
}

// rustString decodes a Rust string literal argument.
func rustString(arg string) (string, bool) {
	arg = strings.TrimSpace(arg)
	if len(arg) >= 2 && arg[0] == '"' && arg[len(arg)-1] == '"' {
		s := arg[1 : len(arg)-1]
		var b strings.Builder
		for i := 0; i < len(s); i++ {
			if s[i] == '\\' && i+1 < len(s) {
				i++
				switch s[i] {
				case 'n':
					b.WriteByte('\n')
				case 't':
					b.WriteByte('\t')
				default:
					b.WriteByte(s[i])
				}
				continue
			}
			b.WriteByte(s[i])
		}
		return b.String(), true
	}
	if strings.HasPrefix(arg, "r") { // raw string
		t := strings.TrimLeft(arg[1:], "#")
		if len(t) >= 2 && t[0] == '"' {
			t = strings.TrimRight(t, "#")
			if len(t) >= 2 && t[len(t)-1] == '"' {
				return t[1 : len(t)-1], true
			}
		}
	}
	return "", false
}

// assertKind names an assertion by macro and the field or method its subject
// ends in: assert_eq!(runner.state().players[0].life, 20) is "assert_eq:life".
func assertKind(macro string, args []string) string {
	if len(args) == 0 {
		return macro
	}
	if macro == "matches" {
		if len(args) < 2 {
			return macro
		}
		p := args[1]
		if k := strings.IndexAny(p, "({ |"); k > 0 {
			p = p[:k]
		}
		return "matches:" + p
	}
	if n := subjectName(args[0]); reLeadLetter.MatchString(n) {
		return macro + ":" + n
	}
	if strings.HasSuffix(macro, "_eq") || strings.HasSuffix(macro, "_ne") {
		if len(args) > 1 {
			if n := subjectName(args[1]); reLeadLetter.MatchString(n) {
				return macro + ":" + n
			}
		}
	}
	if n := subjectName(args[0]); n != "" {
		return macro + ":" + n
	}
	return macro + ":expr"
}

// subjectName is the last field/method/path segment of the leading operand.
func subjectName(expr string) string {
	// cut at the first top-level comparison/boolean operator
	depth := 0
	for i := 0; i < len(expr); i++ {
		switch expr[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case '=', '!', '<', '>', '&', '|':
			if depth == 0 && i+1 < len(expr) && (expr[i+1] == '=' || expr[i] == '&' && expr[i+1] == '&' || expr[i] == '|' && expr[i+1] == '|') && i > 0 {
				expr = expr[:i]
				i = len(expr)
			}
		}
	}
	expr = strings.TrimSpace(expr)
	expr = strings.TrimLeft(expr, "!&*- ")
	for {
		expr = strings.TrimSpace(expr)
		if expr == "" {
			return ""
		}
		last := expr[len(expr)-1]
		if last == ')' || last == ']' {
			o := matchOpen(expr, len(expr)-1)
			if o < 0 {
				return ""
			}
			expr = expr[:o]
			continue
		}
		if last == '?' {
			expr = expr[:len(expr)-1]
			continue
		}
		break
	}
	k := strings.LastIndexAny(expr, ".:")
	return strings.TrimSpace(expr[k+1:])
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// reLetFromHelper matches `let <pattern> = name(`, capturing the pattern.
func reLetFromHelper(name string) *regexp.Regexp {
	return regexp.MustCompile(`let\s+([^=;]+?)\s*(?::[^=;]+)?=\s*` + regexp.QuoteMeta(name) + `\s*\(`)
}
