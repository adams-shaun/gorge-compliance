package main

// A deliberately small reader for XMage Mage.Tests Java files. It does not
// parse Java: it strips comments, finds the class's string/int constants and
// its @Test methods, and splits each method body into top-level statements.
// Every statement the translator understands is a single DSL call whose
// arguments are literals, constants, enum references or small arithmetic.
// Anything else is reported as a refusal by the translator.

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type javaClass struct {
	Name      string
	Extends   string
	Consts    map[string]value
	Methods   []javaMethod
	Helpers   map[string]bool       // every method name declared in the class
	Defs      map[string]*helperDef // inlinable non-@Test methods by name
	Path      string
	Rel       string
	ParseNote string
}

type javaMethod struct {
	Name    string
	Ignored bool
	Expects bool // @Test(expected = ...)
	Body    string
	Stmts   []string
}

// stripComments removes // and /* */ comments, keeping string and char
// literals intact.
func stripComments(src string) string {
	var b strings.Builder
	for i := 0; i < len(src); i++ {
		c := src[i]
		switch {
		case c == '"' || c == '\'':
			j := i + 1
			for j < len(src) && src[j] != c {
				if src[j] == '\\' {
					j++
				}
				j++
			}
			if j >= len(src) {
				j = len(src) - 1
			}
			b.WriteString(src[i : j+1])
			i = j
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			for i < len(src) && src[i] != '\n' {
				i++
			}
			b.WriteByte('\n')
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			j := strings.Index(src[i+2:], "*/")
			if j < 0 {
				return b.String()
			}
			i = i + 2 + j + 1
			b.WriteByte(' ')
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// matchBrace returns the index of the brace closing the one at open.
func matchBrace(s string, open int) int {
	depth := 0
	for i := open; i < len(s); i++ {
		c := s[i]
		if c == '"' || c == '\'' {
			j := i + 1
			for j < len(s) && s[j] != c {
				if s[j] == '\\' {
					j++
				}
				j++
			}
			i = j
			continue
		}
		if c == '{' {
			depth++
		} else if c == '}' {
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

var (
	reClass    = regexp.MustCompile(`(?m)^\s*(?:public\s+)?(?:abstract\s+)?(?:final\s+)?class\s+(\w+)(?:<[^>]*>)?(?:\s+extends\s+([\w.]+))?`)
	reConst    = regexp.MustCompile(`(?s)(?:private|public|protected)?\s*(?:static\s+)?(?:final\s+)?(String|int)\s+(\w+)\s*=\s*([^;]+);`)
	reMethod   = regexp.MustCompile(`(?m)(?:public|private|protected)?\s*(?:static\s+)?(?:void|[\w<>\[\]]+)\s+(\w+)\s*\(([^)]*)\)\s*(?:throws\s+[\w., ]+)?\s*\{`)
	reTestAnno = regexp.MustCompile(`@Test\b(\s*\([^)]*\))?`)
)

func parseJava(path, rel, src string) (*javaClass, error) {
	src = stripComments(src)
	m := reClass.FindStringSubmatch(src)
	if m == nil {
		return nil, fmt.Errorf("no class")
	}
	jc := &javaClass{Name: m[1], Extends: m[2], Consts: map[string]value{}, Helpers: map[string]bool{}, Defs: map[string]*helperDef{}, Path: path, Rel: rel}
	// Constants first (resolved in source order so one may use another).
	for _, cm := range reConst.FindAllStringSubmatch(src, -1) {
		v := evalExpr(strings.TrimSpace(cm[3]), jc.Consts)
		if cm[1] == "String" && v.kind == vStr || cm[1] == "int" && v.kind == vInt {
			jc.Consts[cm[2]] = v
		}
	}
	for _, mm := range reMethod.FindAllStringSubmatchIndex(src, -1) {
		name := src[mm[2]:mm[3]]
		switch name {
		case "if", "for", "while", "switch", "catch", "synchronized", "return", "new":
			continue
		}
		jc.Helpers[name] = true
		if d := parseHelperDef(src, mm); d != nil {
			if _, dup := jc.Defs[name]; dup {
				jc.Defs[name] = nil // overloaded: not inlinable
			} else {
				jc.Defs[name] = d
			}
		}
	}
	// @Test methods.
	locs := reTestAnno.FindAllStringSubmatchIndex(src, -1)
	for _, loc := range locs {
		rest := src[loc[1]:]
		mm := reMethod.FindStringSubmatchIndex(rest)
		if mm == nil {
			continue
		}
		annos := src[loc[0]:loc[1]] + rest[:mm[0]]
		// look back for @Ignore directly before @Test too
		before := src[max(0, loc[0]-200):loc[0]]
		if k := strings.LastIndex(before, "}"); k >= 0 {
			before = before[k:]
		}
		if k := strings.LastIndex(before, ";"); k >= 0 {
			before = before[k:]
		}
		name := rest[mm[2]:mm[3]]
		open := loc[1] + mm[1] - 1
		closeIdx := matchBrace(src, open)
		if closeIdx < 0 {
			continue
		}
		body := src[open+1 : closeIdx]
		jm := javaMethod{
			Name:    name,
			Ignored: strings.Contains(annos, "@Ignore") || strings.Contains(before, "@Ignore"),
			Expects: loc[2] >= 0 && strings.Contains(src[loc[2]:loc[3]], "expected"),
			Body:    body,
			Stmts:   splitStatements(body),
		}
		jc.Methods = append(jc.Methods, jm)
	}
	return jc, nil
}

// splitStatements splits a method body at ';' and at block braces at depth 0.
// A brace-block statement (if/for/try/lambda body) is kept whole so the
// translator can refuse it.
func splitStatements(body string) []string {
	var out []string
	depthP, depthB := 0, 0
	start := 0
	for i := 0; i < len(body); i++ {
		c := body[i]
		if c == '"' || c == '\'' {
			j := i + 1
			for j < len(body) && body[j] != c {
				if body[j] == '\\' {
					j++
				}
				j++
			}
			i = j
			continue
		}
		switch c {
		case '(':
			depthP++
		case ')':
			depthP--
		case '{':
			depthB++
		case '}':
			depthB--
			if depthB == 0 && depthP == 0 {
				// end of a block statement unless followed by ; or ) etc.
				rest := strings.TrimSpace(body[i+1:])
				if !strings.HasPrefix(rest, ";") && !strings.HasPrefix(rest, ")") && !strings.HasPrefix(rest, ",") && !strings.HasPrefix(rest, "else") && !strings.HasPrefix(rest, "catch") && !strings.HasPrefix(rest, "finally") {
					s := strings.TrimSpace(body[start : i+1])
					if s != "" {
						out = append(out, s)
					}
					start = i + 1
				}
			}
		case ';':
			if depthP == 0 && depthB == 0 {
				s := strings.TrimSpace(body[start:i])
				if s != "" {
					out = append(out, s)
				}
				start = i + 1
			}
		}
	}
	if s := strings.TrimSpace(body[start:]); s != "" {
		out = append(out, s)
	}
	return out
}

// ---- values ----

type vkind int

const (
	vNone vkind = iota
	vStr
	vInt
	vBool
	vIdent // identifier or dotted reference: playerA, PhaseStep.END_TURN
	vAbility
	vExpr // anything else, kept raw
)

type value struct {
	kind vkind
	s    string
	n    int
	b    bool
}

func (v value) String() string {
	switch v.kind {
	case vStr:
		return strconv.Quote(v.s)
	case vInt:
		return strconv.Itoa(v.n)
	case vBool:
		return strconv.FormatBool(v.b)
	}
	return v.s
}

type call struct {
	Recv string // qualifier before the name, if any
	Name string
	Args []value
	Raw  string
}

var reCallHead = regexp.MustCompile(`^(?:([A-Za-z_][\w.]*)\.)?([A-Za-z_]\w*)\s*\(`)

// parseCall parses a statement that is exactly one method call.
func parseCall(stmt string, consts map[string]value) (call, bool) {
	stmt = strings.TrimSpace(stmt)
	m := reCallHead.FindStringSubmatchIndex(stmt)
	if m == nil {
		return call{}, false
	}
	open := m[1] - 1
	end := matchParen(stmt, open)
	if end != len(stmt)-1 {
		return call{}, false
	}
	c := call{Name: stmt[m[4]:m[5]], Raw: stmt}
	if m[2] >= 0 {
		c.Recv = stmt[m[2]:m[3]]
	}
	for _, a := range splitArgs(stmt[open+1 : end]) {
		c.Args = append(c.Args, evalExpr(a, consts))
	}
	return c, true
}

func matchParen(s string, open int) int {
	depth := 0
	for i := open; i < len(s); i++ {
		c := s[i]
		if c == '"' || c == '\'' {
			j := i + 1
			for j < len(s) && s[j] != c {
				if s[j] == '\\' {
					j++
				}
				j++
			}
			i = j
			continue
		}
		if c == '(' {
			depth++
		} else if c == ')' {
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func splitArgs(s string) []string {
	var out []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '"' || c == '\'' {
			j := i + 1
			for j < len(s) && s[j] != c {
				if s[j] == '\\' {
					j++
				}
				j++
			}
			i = j
			continue
		}
		switch c {
		case '(', '{', '[':
			depth++
		case ')', '}', ']':
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
	reIdent   = regexp.MustCompile(`^[A-Za-z_][\w.]*$`)
	reAbility = regexp.MustCompile(`^(?:new\s+)?(\w+Ability)(?:\.getInstance\(\)|\(\))$`)
)

// evalExpr evaluates a literal expression: string concatenation, integer
// + - * arithmetic, booleans, identifiers and constants.
func evalExpr(e string, consts map[string]value) value {
	e = strings.TrimSpace(e)
	for strings.HasPrefix(e, "(") && matchParen(e, 0) == len(e)-1 {
		e = strings.TrimSpace(e[1 : len(e)-1])
	}
	if e == "" {
		return value{kind: vNone}
	}
	if m := reAbility.FindStringSubmatch(e); m != nil {
		return value{kind: vAbility, s: m[1]}
	}
	// split top-level + and - operators
	parts, ops := splitOps(e)
	if len(parts) > 1 {
		vals := make([]value, len(parts))
		allInt, anyStr := true, false
		for i, p := range parts {
			vals[i] = evalExpr(p, consts)
			if vals[i].kind != vInt {
				allInt = false
			}
			if vals[i].kind == vStr {
				anyStr = true
			}
		}
		if allInt {
			// * binds tighter than + and -: fold products into terms first.
			terms := []int{vals[0].n}
			var signs []byte
			for i, op := range ops {
				if op == '*' {
					terms[len(terms)-1] *= vals[i+1].n
					continue
				}
				terms = append(terms, vals[i+1].n)
				signs = append(signs, op)
			}
			n := terms[0]
			for i, op := range signs {
				if op == '+' {
					n += terms[i+1]
				} else {
					n -= terms[i+1]
				}
			}
			return value{kind: vInt, n: n}
		}
		if anyStr {
			var b strings.Builder
			for i, v := range vals {
				if i > 0 && ops[i-1] != '+' {
					return value{kind: vExpr, s: e}
				}
				switch v.kind {
				case vStr:
					b.WriteString(v.s)
				case vInt:
					b.WriteString(strconv.Itoa(v.n))
				default:
					return value{kind: vExpr, s: e}
				}
			}
			return value{kind: vStr, s: b.String()}
		}
		return value{kind: vExpr, s: e}
	}
	if strings.HasPrefix(e, "\"") && strings.HasSuffix(e, "\"") && len(e) >= 2 {
		s, err := strconv.Unquote(e)
		if err != nil {
			return value{kind: vExpr, s: e}
		}
		return value{kind: vStr, s: s}
	}
	if n, err := strconv.Atoi(e); err == nil {
		return value{kind: vInt, n: n}
	}
	if strings.HasPrefix(e, "-") {
		if v := evalExpr(e[1:], consts); v.kind == vInt {
			return value{kind: vInt, n: -v.n}
		}
	}
	if e == "true" || e == "false" {
		return value{kind: vBool, b: e == "true"}
	}
	if v, ok := consts[e]; ok {
		return v
	}
	if reIdent.MatchString(e) {
		return value{kind: vIdent, s: e}
	}
	return value{kind: vExpr, s: e}
}

func splitOps(e string) ([]string, []byte) {
	var parts []string
	var ops []byte
	depth := 0
	start := 0
	for i := 0; i < len(e); i++ {
		c := e[i]
		if c == '"' || c == '\'' {
			j := i + 1
			for j < len(e) && e[j] != c {
				if e[j] == '\\' {
					j++
				}
				j++
			}
			i = j
			continue
		}
		switch c {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case '+', '-', '*':
			if depth != 0 || i == 0 {
				continue
			}
			// unary minus after an operator
			prev := strings.TrimSpace(e[start:i])
			if prev == "" {
				continue
			}
			parts = append(parts, prev)
			ops = append(ops, c)
			start = i + 1
		}
	}
	parts = append(parts, strings.TrimSpace(e[start:]))
	return parts, ops
}

// helperDef is a class method the translator may inline at a statement call
// site: its parameter names (the last may be a String... vararg) and body
// statements.
type helperDef struct {
	params []string
	vararg bool
	stmts  []string
}

var reParam = regexp.MustCompile(`^(?:final\s+)?([\w.<>\[\]]+?)(\.\.\.)?\s+(\w+)$`)

func parseHelperDef(src string, mm []int) *helperDef {
	open := mm[1] - 1
	closeIdx := matchBrace(src, open)
	if closeIdx < 0 {
		return nil
	}
	d := &helperDef{stmts: splitStatements(src[open+1 : closeIdx])}
	if ps := strings.TrimSpace(src[mm[4]:mm[5]]); ps != "" {
		for i, p := range splitArgs(ps) {
			m := reParam.FindStringSubmatch(strings.TrimSpace(p))
			if m == nil {
				return nil
			}
			if m[2] != "" {
				if i != len(splitArgs(ps))-1 {
					return nil
				}
				d.vararg = true
			}
			d.params = append(d.params, m[3])
		}
	}
	return d
}

var reForEach = regexp.MustCompile(`(?s)^for\s*\(\s*(?:final\s+)?[\w.<>]+\s+(\w+)\s*:\s*(\w+)\s*\)\s*\{(.*)\}$`)

// inlineHelpers expands every statement that is a call to an inlinable
// helper into the helper's body, with each parameter replaced by the call
// site's argument text. A for-each over the vararg parameter is unrolled.
// Statements it cannot expand are returned unchanged (the translator then
// refuses them with the usual reason).
func inlineHelpers(stmts []string, defs map[string]*helperDef, depth int) []string {
	if depth > 4 {
		return stmts
	}
	var out []string
	for _, st := range stmts {
		exp, ok := inlineOne(st, defs)
		if !ok {
			out = append(out, st)
			continue
		}
		out = append(out, inlineHelpers(exp, defs, depth+1)...)
	}
	return out
}

func inlineOne(st string, defs map[string]*helperDef) ([]string, bool) {
	m := reCallHead.FindStringSubmatchIndex(st)
	if m == nil || m[2] >= 0 && st[m[2]:m[3]] != "this" {
		return nil, false
	}
	d := defs[st[m[4]:m[5]]]
	if d == nil {
		return nil, false
	}
	open := m[1] - 1
	if matchParen(st, open) != len(st)-1 {
		return nil, false
	}
	args := splitArgs(st[open+1 : len(st)-1])
	bind := map[string]string{}
	var rest []string
	n := len(d.params)
	if d.vararg {
		if len(args) < n-1 {
			return nil, false
		}
		rest = args[n-1:]
		n--
	} else if len(args) != n {
		return nil, false
	}
	for i := 0; i < n; i++ {
		bind[d.params[i]] = "(" + args[i] + ")"
	}
	var out []string
	for _, hs := range d.stmts {
		if d.vararg {
			if fm := reForEach.FindStringSubmatch(hs); fm != nil && fm[2] == d.params[len(d.params)-1] {
				body := splitStatements(fm[3])
				for _, a := range rest {
					b2 := map[string]string{fm[1]: "(" + a + ")"}
					for _, bs := range body {
						out = append(out, substIdents(substIdents(bs, b2), bind))
					}
				}
				continue
			}
			if strings.Contains(hs, d.params[len(d.params)-1]) {
				return nil, false
			}
		}
		out = append(out, substIdents(hs, bind))
	}
	return out, true
}

// substIdents replaces whole identifiers outside string literals.
func substIdents(s string, bind map[string]string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '"' || c == '\'' {
			j := i + 1
			for j < len(s) && s[j] != c {
				if s[j] == '\\' {
					j++
				}
				j++
			}
			if j >= len(s) {
				j = len(s) - 1
			}
			b.WriteString(s[i : j+1])
			i = j
			continue
		}
		if isIdentByte(c) && !(c >= '0' && c <= '9') && (i == 0 || !isIdentByte(s[i-1]) && s[i-1] != '.') {
			j := i
			for j < len(s) && isIdentByte(s[j]) {
				j++
			}
			if r, ok := bind[s[i:j]]; ok {
				b.WriteString(r)
			} else {
				b.WriteString(s[i:j])
			}
			i = j - 1
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

func isIdentByte(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}
