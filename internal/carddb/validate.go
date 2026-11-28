package carddb

import (
	"fmt"
	"io"
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
// itself (declarations, not data). Only real declarations are exempt from the
// data checks (see IsSchemaDecl); any other file at or under it is refused,
// so the directory cannot be used to smuggle private data into a public tree.
const SchemaDir = "schema"

// schemaDeclHeader opens every schema declaration table.
const schemaDeclHeader = "#carddb-schema "

var (
	schemaDeclPathRe = regexp.MustCompile(`^schema/v([0-9]+)/(.+)\.tsv$`)
	schemaVersionRe  = regexp.MustCompile(`^[0-9]+\n?$`)
)

// declHeaderLine is the column line every schema declaration table carries.
var declHeaderLine = strings.Join(schemaHeader, "\t")

// IsSchemaDecl reports whether the file at rel, whose whole content is b, is
// a declaration of the schema s: schema/VERSION holding exactly s.Version, or
// schema/v<s.Version>/<table>.tsv for a table s declares, whose content is
// exactly the declaration s itself holds for that table (the
// `#carddb-schema <table> v<N>` line, the column header, and one row per
// declared column). Anything else (a table s does not declare, another
// version, a free-text line, an added, dropped or altered column row) is not
// a declaration, so a tree's own schema can never advertise a column the
// trusted schema s does not.
func IsSchemaDecl(s *Schema, rel string, b []byte) bool {
	if rel == SchemaDir+"/VERSION" {
		n, err := strconv.Atoi(strings.TrimSuffix(string(b), "\n"))
		return schemaVersionRe.Match(b) && err == nil && n == s.Version
	}
	m := schemaDeclPathRe.FindStringSubmatch(rel)
	if m == nil || m[1] != strconv.Itoa(s.Version) || s.Tables[m[2]] == nil {
		return false
	}
	return strings.TrimSuffix(string(b), "\n") == declText(s.Tables[m[2]], s.Version)
}

// IsSchemaAux reports whether rel is the schema's non-declaration config
// file: schema/writers.tsv, the writer SLA registry freshness reads. It is
// the only file under schema/ that is neither VERSION nor a table
// declaration; keeping it here lets the writer ratchet and the data branch
// share one path without letting arbitrary files under schema/.
func IsSchemaAux(rel string) bool { return rel == SchemaDir+"/writers.tsv" }

var (
	writersHeaderRe = regexp.MustCompile(`^#carddb-writers v[1-9][0-9]*$`)
	// writersColumns is the one fixed shape of schema/writers.tsv. Comparing
	// the header exactly keeps the one non-declaration under schema/ from
	// growing a column (or a column named oracle_text / a private table) to
	// park data the public boundary refuses.
	writersColumns = []string{"writer", "sla_hours", "tables", "doc"}
	// A strict positive int, the same rule query.py's load_writers enforces:
	// no leading +, no leading zero, no value beyond int64.
	writersSLARe = regexp.MustCompile(`^[1-9][0-9]*$`)
)

// validateWriters checks schema/writers.tsv's fixed shape, so the one
// non-declaration allowed under schema/ cannot become a place to park
// arbitrary (possibly private) data.
func validateWriters(rel string, b []byte) []Violation {
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
	if !writersHeaderRe.MatchString(lines[0]) {
		add(1, "first line %q is not `#carddb-writers v<N>`", clip(lines[0]))
		return out
	}
	if len(lines) < 2 {
		add(0, "no column header line")
		return out
	}
	header := strings.Split(lines[1], "\t")
	for _, name := range header {
		if name == "" {
			add(2, "empty column name")
		} else if privateTableNames[name] {
			add(2, "column %q names a private table", name)
		} else if ForbiddenField(name) {
			add(2, "column %q is a forbidden field", name)
		}
	}
	if len(header) != len(writersColumns) {
		add(2, "columns must be exactly %s", strings.Join(writersColumns, ", "))
		return out
	}
	for i := range header {
		if header[i] != writersColumns[i] {
			add(2, "columns must be exactly %s", strings.Join(writersColumns, ", "))
			return out
		}
	}
	w, h := 0, 1
	seen := map[string]int{}
	for i, line := range lines[2:] {
		ln := i + 3
		cells := strings.Split(line, "\t")
		if len(cells) != len(header) {
			add(ln, "%d cells, want %d", len(cells), len(header))
			continue
		}
		writer := cells[w]
		if writer == "" {
			add(ln, "empty writer")
			continue
		}
		if first, ok := seen[writer]; ok {
			add(ln, "duplicate writer %q (first at line %d)", writer, first)
			continue
		}
		seen[writer] = ln
		cell := cells[h]
		if !writersSLARe.MatchString(cell) {
			add(ln, "sla_hours %q is not a strict positive int", clip(cell))
			continue
		}
		if n, err := strconv.ParseInt(cell, 10, 64); err != nil || n < 1 {
			add(ln, "sla_hours %q is out of range", clip(cell))
		}
	}
	return out
}

// declText renders a table's schema declaration file, without the final
// newline: the form parseSchemaFile reads.
func declText(t *Table, version int) string {
	yn := func(b bool) string {
		if b {
			return "yes"
		}
		return "no"
	}
	lines := []string{fmt.Sprintf("%s%s v%d", schemaDeclHeader, t.Name, version), declHeaderLine}
	for _, c := range t.Columns {
		lines = append(lines, strings.Join([]string{c.Name, c.Type, yn(c.Required), yn(c.Key), c.Visibility, c.Doc}, "\t"))
	}
	return strings.Join(lines, "\n")
}

// shardPrefix opens the first line of every data shard (and of any file that
// claims to be one, whatever its extension).
const shardPrefix = "#carddb "

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

const notDeclMsg = "not a schema declaration: only schema/VERSION, schema/v<N>/<table>.tsv (identical to the trusted schema's declaration of that table) and schema/writers.tsv may live at or under schema/"

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
		inSchema := isSchemaPath(rel)
		isTSV := strings.HasSuffix(rel, ".tsv")
		if !inSchema && !isTSV {
			// a shard hidden behind another extension is still a shard
			claims, err := claimsShard(filepath.Join(tree, filepath.FromSlash(rel)))
			if err != nil {
				return nil, err
			}
			if claims {
				out = append(out, Violation{rel, 1, "carddb shard must be a .tsv file"})
			}
			continue
		}
		b, err := os.ReadFile(filepath.Join(tree, filepath.FromSlash(rel)))
		if err != nil {
			return nil, err
		}
		if inSchema {
			if IsSchemaAux(rel) {
				out = append(out, validateWriters(rel, b)...)
				continue
			}
			if !IsSchemaDecl(s, rel, b) {
				out = append(out, Violation{rel, 0, notDeclMsg})
			}
			continue
		}
		out = append(out, validateShard(s, scope, rel, b, keys)...)
	}
	return out, nil
}

// claimsShard reports whether the file's first line opens like a shard header.
func claimsShard(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	buf := make([]byte, len(shardPrefix))
	n, _ := io.ReadFull(f, buf)
	return string(buf[:n]) == shardPrefix, nil
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
		if table == "keyproof" {
			out = append(out, keyproofRowRules(rel, head, cells)...)
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

// The closed value sets of the keyproof ledger. One table-specific hook rather
// than per-column vocabularies: the schema's string columns are open by design,
// but a keyproof row can only mean one of these.
var (
	keyproofLegs       = []string{"xmage", "forge", "lockstep", "scenario", "cite"}
	keyproofStates     = []string{"agreed", "diagnosed", "diverged", "owed", "stale", "excluded"}
	keyproofCiteStates = []string{"diagnosed", "owed", "excluded"}
	keyproofSeats      = []string{"0", "2", "3", "4"}
)

func oneOf(v string, set []string) bool {
	for _, s := range set {
		if v == s {
			return true
		}
	}
	return false
}

// keyproofRowRules reports a keyproof row that cannot mean anything. It runs
// after the generic cell checks and looks every column up by name, so a
// reordered header does not change the verdict. The message names the row's
// key columns (schema order: oracle_id, key, leg) and the rule.
func keyproofRowRules(rel string, head []string, row []string) []Violation {
	cell := make(map[string]string, len(head))
	for i, name := range head {
		if i < len(row) {
			cell[name] = row[i]
		}
	}
	leg, state, cite, seats := cell["leg"], cell["state"], cell["cite"], cell["seats"]
	key := strings.Join([]string{cell["oracle_id"], cell["key"], leg}, " ")
	var out []Violation
	add := func(f string, a ...any) {
		out = append(out, Violation{rel, 0, key + ": " + fmt.Sprintf(f, a...)})
	}
	if !oneOf(leg, keyproofLegs) {
		add("leg '%s' is not xmage, forge, lockstep, scenario or cite", leg)
	}
	if !oneOf(state, keyproofStates) {
		add("state '%s' is not agreed, diagnosed, diverged, owed, stale or excluded", state)
	}
	if state == "diagnosed" && cite == Empty {
		add("diagnosed without a cite")
	}
	if leg == "cite" && !oneOf(state, keyproofCiteStates) {
		add("cite leg must be diagnosed, owed or excluded, not '%s'", state)
	}
	if !oneOf(seats, keyproofSeats) {
		add("seats '%s' is not 0, 2, 3 or 4", seats)
	}
	if state == "agreed" && seats == "0" {
		add("agreed with seats 0")
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
