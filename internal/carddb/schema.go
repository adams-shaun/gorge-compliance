// Package carddb is the schema loader, shard validator and public-boundary
// check for the carddb data set (carddb/schema, carddb/curated).
//
// A tree is a directory of TSV shards. Every shard is
//
//	#carddb <table> v<N>
//	<column>\t<column>...
//	<row>
//
// where <table> is declared in carddb/schema/v<N>/<table>.tsv and N is
// carddb/schema/VERSION. A table's shards live at <table>.tsv or under
// <table>/ (any file name); the header line, not the path, names the table,
// and the two must agree. Rows are sorted bytewise by their key columns (in
// schema column order) and keys are unique within a table. A cell is never
// empty (`-` is the empty value) and never holds a tab, newline or CR.
package carddb

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Visibility of a column: public data may be published, private data never.
const (
	Public  = "public"
	Private = "private"
)

// Empty is the cell value for "no value".
const Empty = "-"

// Column is one declared column of a table.
type Column struct {
	Name       string
	Type       string // string, int, bool, date, list
	Required   bool
	Key        bool
	Visibility string
	Doc        string
}

// Table is a declared table.
type Table struct {
	Name    string
	Columns []Column
}

// IsPrivate reports whether every column of the table is private (a
// private-only table is never published).
func (t *Table) IsPrivate() bool {
	for _, c := range t.Columns {
		if c.Visibility == Public {
			return false
		}
	}
	return true
}

// Columns returns the columns present in a tree of the given scope: all of
// them for the private scope, the public ones for the public scope.
func (t *Table) ScopeColumns(scope string) []Column {
	if scope == Private {
		return t.Columns
	}
	var out []Column
	for _, c := range t.Columns {
		if c.Visibility == Public {
			out = append(out, c)
		}
	}
	return out
}

// Schema is a loaded schema version.
type Schema struct {
	Version int
	Tables  map[string]*Table
}

// TableNames returns the declared table names, sorted.
func (s *Schema) TableNames() []string {
	var out []string
	for n := range s.Tables {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

var columnTypes = map[string]bool{"string": true, "int": true, "bool": true, "date": true, "list": true}

var schemaHeader = []string{"name", "type", "required", "key", "visibility", "doc"}

// LoadSchema reads <dir>/VERSION and <dir>/v<N>/**/*.tsv.
func LoadSchema(dir string) (*Schema, error) {
	vb, err := os.ReadFile(filepath.Join(dir, "VERSION"))
	if err != nil {
		return nil, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(vb)))
	if err != nil || n < 1 {
		return nil, fmt.Errorf("%s/VERSION: %q is not a schema version", dir, strings.TrimSpace(string(vb)))
	}
	root := filepath.Join(dir, "v"+strconv.Itoa(n))
	s := &Schema{Version: n, Tables: map[string]*Table{}}
	var files []string
	err = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(p, ".tsv") {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	for _, p := range files {
		rel, _ := filepath.Rel(root, p)
		name := strings.TrimSuffix(filepath.ToSlash(rel), ".tsv")
		t, err := parseSchemaFile(p, name, n)
		if err != nil {
			return nil, err
		}
		s.Tables[name] = t
	}
	if len(s.Tables) == 0 {
		return nil, fmt.Errorf("%s: no tables", root)
	}
	return s, nil
}

func parseSchemaFile(path, name string, version int) (*Table, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	fail := func(line int, f string, a ...any) error {
		return fmt.Errorf("%s:%d: %s", path, line, fmt.Sprintf(f, a...))
	}
	lines := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	if want := fmt.Sprintf("#carddb-schema %s v%d", name, version); lines[0] != want {
		return nil, fail(1, "first line is %q, want %q", lines[0], want)
	}
	if len(lines) < 3 || lines[1] != strings.Join(schemaHeader, "\t") {
		return nil, fail(2, "column header must be %q", strings.Join(schemaHeader, "\t"))
	}
	t := &Table{Name: name}
	seen := map[string]bool{}
	anyKey := false
	for i, l := range lines[2:] {
		ln := i + 3
		f := strings.Split(l, "\t")
		if len(f) != len(schemaHeader) {
			return nil, fail(ln, "%d fields, want %d", len(f), len(schemaHeader))
		}
		c := Column{Name: f[0], Type: f[1], Visibility: f[4], Doc: f[5]}
		if !identRe.MatchString(c.Name) {
			return nil, fail(ln, "bad column name %q", c.Name)
		}
		if seen[c.Name] {
			return nil, fail(ln, "duplicate column %q", c.Name)
		}
		seen[c.Name] = true
		if !columnTypes[c.Type] {
			return nil, fail(ln, "unknown type %q", c.Type)
		}
		var ok bool
		if c.Required, ok = parseYesNo(f[2]); !ok {
			return nil, fail(ln, "required must be yes or no, got %q", f[2])
		}
		if c.Key, ok = parseYesNo(f[3]); !ok {
			return nil, fail(ln, "key must be yes or no, got %q", f[3])
		}
		if c.Key && !c.Required {
			return nil, fail(ln, "key column %q must be required", c.Name)
		}
		if c.Visibility != Public && c.Visibility != Private {
			return nil, fail(ln, "visibility must be public or private, got %q", c.Visibility)
		}
		if c.Visibility == Public && ForbiddenField(c.Name) {
			return nil, fail(ln, "public column %q has a name the public boundary forbids", c.Name)
		}
		if c.Doc == "" {
			return nil, fail(ln, "column %q has no doc", c.Name)
		}
		anyKey = anyKey || c.Key
		t.Columns = append(t.Columns, c)
	}
	if !anyKey {
		return nil, fail(1, "table %q declares no key column", name)
	}
	for _, c := range t.Columns {
		if c.Key && c.Visibility != Public && !t.IsPrivate() {
			return nil, fail(1, "table %q: key column %q is private in a table with public columns", name, c.Name)
		}
	}
	return t, nil
}

var identRe = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func parseYesNo(s string) (v, ok bool) {
	switch s {
	case "yes":
		return true, true
	case "no":
		return false, true
	}
	return false, false
}
