package carddb

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Violation is one finding about a file of a tree. Line is 1-based, 0 when
// the finding is about the file as a whole.
type Violation struct {
	Path string // slash-separated, relative to the tree root
	Line int
	Msg  string
}

func (v Violation) String() string {
	if v.Line > 0 {
		return fmt.Sprintf("%s:%d: %s", v.Path, v.Line, v.Msg)
	}
	return fmt.Sprintf("%s: %s", v.Path, v.Msg)
}

// SchemaDir is the top-level directory of a tree that holds the schema
// itself (declarations, not data): validate skips it, and the boundary check
// does not read its private table declarations as private data.
const SchemaDir = "schema"

// treeFiles lists the regular files of a tree, slash-separated and relative,
// in lexical order. .git is skipped.
func treeFiles(tree string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(tree, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(tree, p)
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	return out, err
}

func isSchemaPath(rel string) bool { return rel == SchemaDir || strings.HasPrefix(rel, SchemaDir+"/") }

// pathHoldsTable reports whether rel is a legal location for a shard of table.
func pathHoldsTable(rel, table string) bool {
	return rel == table+".tsv" || strings.HasPrefix(rel, table+"/")
}

// ParseShardHeader splits a `#carddb <table> v<N>` line.
func ParseShardHeader(line string) (table string, version int, ok bool) {
	f := strings.Split(line, " ")
	if len(f) != 3 || f[0] != "#carddb" || f[1] == "" || !strings.HasPrefix(f[2], "v") {
		return "", 0, false
	}
	n, err := strconv.Atoi(f[2][1:])
	if err != nil || n < 1 || strconv.Itoa(n) != f[2][1:] {
		return "", 0, false
	}
	return f[1], n, true
}

// Validate checks every .tsv shard of tree against s. scope is Public (a
// tree that may be published: private tables are refused and the expected
// columns are the public ones) or Private (every table and column). The
// returned violations are in path order, then line order.
func Validate(s *Schema, tree, scope string) ([]Violation, error) {
	files, err := treeFiles(tree)
	if err != nil {
		return nil, err
	}
	var out []Violation
	keys := map[string]map[string]string{} // table -> key -> "path:line"
	for _, rel := range files {
		if isSchemaPath(rel) || !strings.HasSuffix(rel, ".tsv") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(tree, filepath.FromSlash(rel)))
		if err != nil {
			return nil, err
		}
		out = append(out, validateShard(s, scope, rel, b, keys)...)
	}
	return out, nil
}

func validateShard(s *Schema, scope, rel string, b []byte, keys map[string]map[string]string) []Violation {
	var out []Violation
	add := func(line int, f string, a ...any) { out = append(out, Violation{rel, line, fmt.Sprintf(f, a...)}) }
	if len(b) == 0 {
		add(0, "empty file")
		return out
	}
	if b[len(b)-1] != '\n' {
		add(0, "does not end with a newline")
	}
	lines := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	table, ver, ok := ParseShardHeader(lines[0])
	if !ok {
		add(1, "first line %q is not `#carddb <table> v<N>`", clip(lines[0]))
		return out
	}
	if ver != s.Version {
		add(1, "shard is v%d, schema is v%d", ver, s.Version)
	}
	t := s.Tables[table]
	if t == nil {
		add(1, "table %q is not declared in the schema", table)
		return out
	}
	if !pathHoldsTable(rel, table) {
		add(1, "table %q must live at %s.tsv or under %s/", table, table, table)
	}
	if scope != Private && t.IsPrivate() {
		add(1, "table %q is private and not allowed in a public tree", table)
		return out
	}
	cols := t.ScopeColumns(scope)
	if len(lines) < 2 {
		add(0, "no column header line")
		return out
	}
	head := strings.Split(lines[1], "\t")
	if !sameColumns(head, cols) {
		add(2, "%s", columnDiff(head, cols))
		return out
	}
	var keyIdx []int
	for i, c := range cols {
		if c.Key {
			keyIdx = append(keyIdx, i)
		}
	}
	if keys[table] == nil {
		keys[table] = map[string]string{}
	}
	var prev []string
	for i, l := range lines[2:] {
		ln := i + 3
		if strings.ContainsRune(l, '\r') {
			add(ln, "carriage return in row")
			continue
		}
		cells := strings.Split(l, "\t")
		if len(cells) != len(cols) {
			add(ln, "%d cells, want %d", len(cells), len(cols))
			continue
		}
		bad := false
		for j, c := range cols {
			if msg := checkCell(c, cells[j]); msg != "" {
				add(ln, "%s: %s", c.Name, msg)
				bad = true
			}
		}
		if bad {
			continue
		}
		key := make([]string, len(keyIdx))
		for k, j := range keyIdx {
			key[k] = cells[j]
		}
		joined := strings.Join(key, "\t")
		if at, dup := keys[table][joined]; dup {
			add(ln, "duplicate key (%s), first at %s", strings.Join(key, ", "), at)
		} else {
			keys[table][joined] = rel + ":" + strconv.Itoa(ln)
			if prev != nil && compareKeys(prev, key) > 0 {
				add(ln, "rows not sorted: key (%s) follows (%s)", strings.Join(key, ", "), strings.Join(prev, ", "))
			}
		}
		prev = key
	}
	return out
}

func compareKeys(a, b []string) int {
	for i := range a {
		if c := strings.Compare(a[i], b[i]); c != 0 {
			return c
		}
	}
	return 0
}

func sameColumns(head []string, cols []Column) bool {
	if len(head) != len(cols) {
		return false
	}
	for i := range head {
		if head[i] != cols[i].Name {
			return false
		}
	}
	return true
}

func columnDiff(head []string, cols []Column) string {
	want := map[string]bool{}
	var names []string
	for _, c := range cols {
		want[c.Name] = true
		names = append(names, c.Name)
	}
	have := map[string]bool{}
	var extra, missing []string
	for _, h := range head {
		have[h] = true
		if !want[h] {
			extra = append(extra, h)
		}
	}
	for _, c := range cols {
		if !have[c.Name] {
			missing = append(missing, c.Name)
		}
	}
	sort.Strings(extra)
	switch {
	case len(extra) > 0 || len(missing) > 0:
		return fmt.Sprintf("columns differ from the schema: extra [%s], missing [%s]", strings.Join(extra, " "), strings.Join(missing, " "))
	default:
		return fmt.Sprintf("columns are in the wrong order: want %s", strings.Join(names, "\t"))
	}
}

var intRe = regexp.MustCompile(`^-?(0|[1-9][0-9]*)$`)

// checkCell returns "" when cell is a legal value of column c.
func checkCell(c Column, cell string) string {
	if cell == "" {
		return "empty cell (write - for no value)"
	}
	if !utf8.ValidString(cell) {
		return "not valid UTF-8"
	}
	if cell == Empty {
		if c.Required {
			return "required column has no value"
		}
		return ""
	}
	switch c.Type {
	case "int":
		if !intRe.MatchString(cell) {
			return fmt.Sprintf("%q is not an int", clip(cell))
		}
	case "bool":
		if cell != "true" && cell != "false" {
			return fmt.Sprintf("%q is not true or false", clip(cell))
		}
	case "date":
		if d, err := time.Parse("2006-01-02", cell); err != nil || d.Format("2006-01-02") != cell {
			return fmt.Sprintf("%q is not a YYYY-MM-DD date", clip(cell))
		}
	case "list":
		for _, it := range strings.Split(cell, ",") {
			if it == "" || it == Empty {
				return fmt.Sprintf("%q is not a comma-separated list of non-empty items", clip(cell))
			}
		}
	}
	return ""
}

func clip(s string) string {
	if len(s) > 60 {
		return s[:60] + "..."
	}
	return s
}
