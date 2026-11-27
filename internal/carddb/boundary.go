package carddb

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// privateTableNames are tables that are never published, named here as well
// as in the schema so the boundary holds even against a schema that was
// edited to call one public.
var privateTableNames = map[string]bool{
	"text": true, "tags": true, "edhrec": true, "prims": true,
	"values": true, "labels_private": true, "decks": true,
	"cert_events": true, "cert_runs": true, "rulings": true,
}

// forbiddenFieldRe names the fields no public tree may carry, in any table.
var forbiddenFieldRe = regexp.MustCompile(`(?i)^(oracle_text|price.*|prims|tagger|edhrec.*|value.*)$`)

// ForbiddenField reports whether name is a field the public boundary bans.
func ForbiddenField(name string) bool { return forbiddenFieldRe.MatchString(name) }

// forgeLineRe is the shape of a Forge card-script line (Forge is GPL-3.0;
// none may reach a public tree).
var forgeLineRe = regexp.MustCompile(`^A:|^SVar:|^T:Mode\$|^S:Mode\$|\$ [A-Z]`)

// jsonFieldRe finds a banned field used as a JSON key in a .json/.jsonl file.
var jsonFieldRe = regexp.MustCompile(`(?i)"(oracle_text|price[a-z0-9_]*|prims|tagger|edhrec[a-z0-9_]*|value[a-z0-9_]*)"\s*:`)

// Boundary checks that the tree (everything under it but .git) holds nothing
// that must not be published: no private table or column, no column the schema
// does not declare public, no field with a banned name, no Forge-script-shaped
// line in any file. Real schema
// declarations (IsSchemaDecl: a table the trusted schema declares, whose
// whole content equals that declaration) name private tables and columns, so
// they are exempt from the table and field checks (not from the Forge-line
// scan). The one config file, schema/writers.tsv, is allowed but its fixed
// shape is checked; any other file at or under SchemaDir is refused outright.
func Boundary(s *Schema, tree string) ([]Violation, error) {
	files, err := treeFiles(tree)
	if err != nil {
		return nil, err
	}
	var out []Violation
	for _, rel := range files {
		abs := filepath.Join(tree, filepath.FromSlash(rel))
		f, err := os.Open(abs)
		if err != nil {
			return nil, err
		}
		vs, err := boundaryFile(s, rel, f)
		f.Close()
		if err != nil {
			return nil, err
		}
		out = append(out, vs...)
	}
	return out, nil
}

func boundaryFile(s *Schema, rel string, r io.Reader) ([]Violation, error) {
	var out []Violation
	add := func(line int, f string, a ...any) { out = append(out, Violation{rel, line, fmt.Sprintf(f, a...)}) }
	decl := false
	aux := IsSchemaAux(rel)
	if isSchemaPath(rel) {
		b, err := io.ReadAll(r)
		if err != nil {
			return nil, err
		}
		if aux {
			out = append(out, validateWriters(rel, b)...)
		} else {
			// a declaration is judged on its whole first lines, not a prefix
			decl = IsSchemaDecl(s, rel, b)
		}
		r = bytes.NewReader(b)
	}
	br := bufio.NewReaderSize(r, 1<<16)
	if isSchemaPath(rel) && !decl && !aux {
		add(0, notDeclMsg)
	}
	data := !decl
	isTSV := strings.HasSuffix(rel, ".tsv")
	isJSON := strings.HasSuffix(rel, ".json") || strings.HasSuffix(rel, ".jsonl")
	if data {
		for _, comp := range strings.Split(rel, "/") {
			// a component is judged on its stem, whatever the extension
			stem := strings.TrimSuffix(comp, filepath.Ext(comp))
			for _, n := range []string{stem, comp} {
				if privateTableNames[n] {
					add(0, "path names private table %q", n)
					break
				} else if ForbiddenField(n) {
					add(0, "path names forbidden field %q", n)
					break
				}
			}
		}
	}
	table, colsAt := "", 0
	for ln := 1; ; ln++ {
		line, err := br.ReadString('\n')
		if len(line) > 0 {
			line = strings.TrimRight(line, "\r\n")
			if forgeLineRe.MatchString(line) {
				add(ln, "Forge-script-shaped line (pattern %s)", forgeLineRe)
			}
			if decl && rel == SchemaDir+"/VERSION" && (ln > 1 || !schemaVersionRe.MatchString(line)) {
				add(ln, "schema/VERSION must hold only the version number")
			}
			// a .tsv is a shard from its first line; any other file is one
			// from the first line that opens like a shard header
			if data && !aux && ((isTSV && ln == 1) || (!isTSV && strings.HasPrefix(line, shardPrefix))) {
				table, colsAt = headerTable(s, ln, line, add), ln+1
			} else if data && ln == colsAt {
				columnViolations(s, table, line, func(f string, a ...any) { add(ln, f, a...) })
			}
			if data && isJSON {
				if m := jsonFieldRe.FindStringSubmatch(line); m != nil {
					add(ln, "field %q is forbidden in a public tree", m[1])
				}
			}
		}
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
	}
}

// headerTable checks the `#carddb` line of a data shard and returns the
// table it names ("" when the line is not a shard header).
func headerTable(s *Schema, ln int, line string, add func(int, string, ...any)) string {
	table, _, ok := ParseShardHeader(line)
	if !ok {
		add(ln, "not a carddb shard: first line %q is not `#carddb <table> v<N>`", clip(line))
		return ""
	}
	if privateTableNames[table] {
		add(ln, "private table %q", table)
	} else if t := s.Tables[table]; t != nil && t.IsPrivate() {
		add(ln, "private table %q", table)
	}
	return table
}

// columnViolations checks a shard's column-header line: no banned field name,
// no column the schema declares private for the table, and no column the
// schema does not declare public (a public shard may not grow a column, such
// as `comment` on ruling_refs, to carry the private data of its table).
func columnViolations(s *Schema, table, line string, add func(string, ...any)) {
	var t *Table
	if table != "" {
		t = s.Tables[table]
	}
	for _, name := range strings.Split(line, "\t") {
		if ForbiddenField(name) {
			add("field %q is forbidden in a public tree", name)
			continue
		}
		if t == nil {
			continue
		}
		declared, private := false, false
		for _, c := range t.Columns {
			if c.Name == name {
				declared, private = true, c.Visibility == Private
				break
			}
		}
		if private {
			add("column %q of table %q is private", name, table)
		} else if !declared {
			add("column %q is not declared public in table %q", name, table)
		}
	}
}
