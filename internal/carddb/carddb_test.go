package carddb

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const realSchema = "../../carddb/schema"

func loadReal(t *testing.T) *Schema {
	t.Helper()
	s, err := LoadSchema(realSchema)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// cellFor builds a legal value of column c for row i (i < 10, so the keys
// sort as written).
func cellFor(c Column, i int) string {
	if !c.Required && i == 0 {
		return Empty
	}
	switch c.Type {
	case "int":
		return "7"
	case "bool":
		return "true"
	case "date":
		return "2026-01-02"
	case "list":
		return "a,b"
	}
	return fmt.Sprintf("%s%d", c.Name, i)
}

// shard renders a legal shard of table with n rows over the scope's columns.
func shard(t *Table, scope string, n int) string {
	cols := t.ScopeColumns(scope)
	var b strings.Builder
	fmt.Fprintf(&b, "#carddb %s v1\n", t.Name)
	names := make([]string, len(cols))
	for i, c := range cols {
		names[i] = c.Name
	}
	b.WriteString(strings.Join(names, "\t") + "\n")
	for i := 0; i < n; i++ {
		cells := make([]string, len(cols))
		for j, c := range cols {
			cells[j] = cellFor(c, i)
		}
		b.WriteString(strings.Join(cells, "\t") + "\n")
	}
	return b.String()
}

// validTree writes one legal shard of every table the scope allows.
func validTree(t *testing.T, s *Schema, scope string) string {
	t.Helper()
	root := t.TempDir()
	for _, name := range s.TableNames() {
		tb := s.Tables[name]
		if scope == Public && tb.IsPrivate() {
			continue
		}
		write(t, root, name+".tsv", shard(tb, scope, 3))
	}
	return root
}

func msgs(vs []Violation) string {
	var b strings.Builder
	for _, v := range vs {
		b.WriteString(v.String() + "\n")
	}
	return b.String()
}

func mustFail(t *testing.T, vs []Violation, err error, want string) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) == 0 {
		t.Fatalf("no violation, want one containing %q", want)
	}
	if !strings.Contains(msgs(vs), want) {
		t.Fatalf("violations %q do not contain %q", msgs(vs), want)
	}
}

func TestValidTreePasses(t *testing.T) {
	s := loadReal(t)
	for _, scope := range []string{Public, Private} {
		root := validTree(t, s, scope)
		vs, err := Validate(s, root, scope)
		if err != nil || len(vs) != 0 {
			t.Fatalf("%s validate: %v %v", scope, err, msgs(vs))
		}
		if scope == Public {
			vs, err = Boundary(s, root)
			if err != nil || len(vs) != 0 {
				t.Fatalf("boundary: %v %v", err, msgs(vs))
			}
		}
	}
}

func TestShardedTablePasses(t *testing.T) {
	s := loadReal(t)
	root := t.TempDir()
	write(t, root, "legality/a.tsv", "#carddb legality v1\noracle_id\tformat\tstatus\nx1\tmodern\tlegal\n")
	write(t, root, "legality/b.tsv", "#carddb legality v1\noracle_id\tformat\tstatus\nx2\tmodern\tlegal\n")
	vs, err := Validate(s, root, Public)
	if err != nil || len(vs) != 0 {
		t.Fatalf("%v %v", err, msgs(vs))
	}
	// A key repeated in another shard of the table is a duplicate.
	write(t, root, "legality/c.tsv", "#carddb legality v1\noracle_id\tformat\tstatus\nx1\tmodern\tbanned\n")
	vs, err = Validate(s, root, Public)
	mustFail(t, vs, err, "duplicate key (x1, modern), first at legality/a.tsv:3")
}

func TestShuffledShardFails(t *testing.T) {
	s := loadReal(t)
	root := validTree(t, s, Public)
	lines := strings.Split(strings.TrimSuffix(read(t, root, "cards.tsv"), "\n"), "\n")
	lines[2], lines[4] = lines[4], lines[2]
	write(t, root, "cards.tsv", strings.Join(lines, "\n")+"\n")
	vs, err := Validate(s, root, Public)
	mustFail(t, vs, err, "rows not sorted")
}

func TestDuplicateKeyFails(t *testing.T) {
	s := loadReal(t)
	root := validTree(t, s, Public)
	lines := strings.Split(strings.TrimSuffix(read(t, root, "sets.tsv"), "\n"), "\n")
	lines[3] = lines[2]
	write(t, root, "sets.tsv", strings.Join(lines, "\n")+"\n")
	vs, err := Validate(s, root, Public)
	mustFail(t, vs, err, "duplicate key")
}

func TestCellSyntaxFails(t *testing.T) {
	s := loadReal(t)
	hdr := "#carddb sets v1\nset_code\tname\trelease_date\tset_type\tparent_code\n"
	for name, tc := range map[string]struct{ row, want string }{
		"empty cell":       {"a\tn\t2026-01-02\tcore\t\n", "empty cell"},
		"raw tab":          {"a\tn\t2026-01-02\tco\tre\t-\n", "6 cells, want 5"},
		"carriage return":  {"a\tn\r\t2026-01-02\tcore\t-\n", "carriage return"},
		"required dash":    {"a\t-\t2026-01-02\tcore\t-\n", "required column has no value"},
		"bad date":         {"a\tn\t2026-13-02\tcore\t-\n", "not a YYYY-MM-DD date"},
		"too few cells":    {"a\tn\n", "2 cells, want 5"},
		"invalid UTF-8":    {"a\tn\xff\t2026-01-02\tcore\t-\n", "not valid UTF-8"},
		"optional dash ok": {"a\tn\t2026-01-02\tcore\t-\n", ""},
	} {
		root := t.TempDir()
		write(t, root, "sets.tsv", hdr+tc.row)
		vs, err := Validate(s, root, Public)
		if tc.want == "" {
			if err != nil || len(vs) != 0 {
				t.Errorf("%s: %v %v", name, err, msgs(vs))
			}
			continue
		}
		if err != nil || !strings.Contains(msgs(vs), tc.want) {
			t.Errorf("%s: %v %q, want %q", name, err, msgs(vs), tc.want)
		}
	}
}

func TestHeaderFails(t *testing.T) {
	s := loadReal(t)
	sets := "set_code\tname\trelease_date\tset_type\tparent_code\n"
	for name, tc := range map[string]struct{ rel, body, want string }{
		"no header line":   {"sets.tsv", sets + "a\tn\t2026-01-02\tcore\t-\n", "is not `#carddb <table> v<N>`"},
		"wrong version":    {"sets.tsv", "#carddb sets v2\n" + sets, "shard is v2, schema is v1"},
		"unknown table":    {"nope.tsv", "#carddb nope v1\nk\n", `table "nope" is not declared`},
		"wrong path":       {"elsewhere.tsv", "#carddb sets v1\n" + sets, "must live at sets.tsv or under sets/"},
		"missing column":   {"sets.tsv", "#carddb sets v1\nset_code\tname\trelease_date\tset_type\n", "missing [parent_code]"},
		"extra column":     {"sets.tsv", "#carddb sets v1\n" + strings.TrimSuffix(sets, "\n") + "\tzzz\n", "extra [zzz]"},
		"reordered column": {"sets.tsv", "#carddb sets v1\nname\tset_code\trelease_date\tset_type\tparent_code\n", "wrong order"},
		"no final newline": {"sets.tsv", "#carddb sets v1\n" + strings.TrimSuffix(sets, "\n"), "does not end with a newline"},
		"empty file":       {"sets.tsv", "", "empty file"},
	} {
		root := t.TempDir()
		write(t, root, tc.rel, tc.body)
		vs, err := Validate(s, root, Public)
		if err != nil || !strings.Contains(msgs(vs), tc.want) {
			t.Errorf("%s: %v %q, want %q", name, err, msgs(vs), tc.want)
		}
	}
}

func TestPrivateTableRefusedInPublicTree(t *testing.T) {
	s := loadReal(t)
	root := validTree(t, s, Public)
	write(t, root, "text.tsv", shard(s.Tables["text"], Private, 2))
	vs, err := Validate(s, root, Public)
	mustFail(t, vs, err, `table "text" is private`)
	vs, err = Boundary(s, root)
	mustFail(t, vs, err, `private table "text"`)
	if vs, err = Validate(s, root, Private); err != nil || len(vs) != 0 {
		t.Fatalf("private scope must accept it: %v %v", err, msgs(vs))
	}
}

func TestRulingsStayPrivate(t *testing.T) {
	s := loadReal(t)

	// The private table itself never reaches a public tree.
	root := validTree(t, s, Public)
	write(t, root, "rulings.tsv", shard(s.Tables["rulings"], Private, 2))
	vs, err := Boundary(s, root)
	mustFail(t, vs, err, `private table "rulings"`)

	// The hard-coded private-name list holds even against a schema edited to
	// call every rulings column public.
	sdir := t.TempDir()
	copyTree(t, realSchema, sdir)
	write(t, sdir, "v1/rulings.tsv", strings.ReplaceAll(read(t, sdir, "v1/rulings.tsv"), "\tprivate\t", "\tpublic\t"))
	s2, err := LoadSchema(sdir)
	if err != nil {
		t.Fatal(err)
	}
	vs, err = Boundary(s2, validTree(t, s2, Public))
	mustFail(t, vs, err, `private table "rulings"`)

	// The public mirror holds ids, dates and sources, and no text.
	root = validTree(t, s, Public)
	write(t, root, "ruling_refs.tsv", shard(s.Tables["ruling_refs"], Public, 2))
	if vs, err = Validate(s, root, Public); err != nil || len(vs) != 0 {
		t.Fatalf("ruling_refs validate: %v %v", err, msgs(vs))
	}
	if vs, err = Boundary(s, root); err != nil || len(vs) != 0 {
		t.Fatalf("ruling_refs boundary: %v %v", err, msgs(vs))
	}

	// A public shard cannot smuggle the text back in as an extra column.
	lines := strings.Split(strings.TrimSuffix(shard(s.Tables["ruling_refs"], Public, 2), "\n"), "\n")
	lines[1] += "\tcomment"
	for i := 2; i < len(lines); i++ {
		lines[i] += "\t-"
	}
	write(t, root, "ruling_refs.tsv", strings.Join(lines, "\n")+"\n")
	vs, err = Boundary(s, root)
	mustFail(t, vs, err, `column "comment" is not declared public in table "ruling_refs"`)
}

func TestPrivateColumnInPublicShardFails(t *testing.T) {
	s := loadReal(t)
	// A banned name on a public table, whatever the schema says.
	root := validTree(t, s, Public)
	body := read(t, root, "cards.tsv")
	lines := strings.Split(body, "\n")
	for i := 1; i < len(lines)-1; i++ {
		if i == 1 {
			lines[i] += "\toracle_text"
		} else {
			lines[i] += "\t-"
		}
	}
	write(t, root, "cards.tsv", strings.Join(lines, "\n"))
	vs, err := Boundary(s, root)
	mustFail(t, vs, err, `field "oracle_text" is forbidden`)
	vs, err = Validate(s, root, Public)
	mustFail(t, vs, err, "extra [oracle_text]")

	// A column the schema declares private in a table with public columns.
	sdir := t.TempDir()
	copyTree(t, realSchema, sdir)
	card := read(t, sdir, "v1/cards.tsv")
	write(t, sdir, "v1/cards.tsv", card+"curator_note\tstring\tno\tno\tprivate\tinternal note\n")
	s2, err := LoadSchema(sdir)
	if err != nil {
		t.Fatal(err)
	}
	root = validTree(t, s2, Private)
	vs, err = Boundary(s2, root)
	mustFail(t, vs, err, `column "curator_note" of table "cards" is private`)
	vs, err = Validate(s2, root, Public)
	mustFail(t, vs, err, "extra [curator_note]")
}

func TestBannedFieldNames(t *testing.T) {
	for _, n := range []string{"oracle_text", "price_usd", "Price", "prims", "tagger", "edhrec_rank", "value", "values", "ValueScore"} {
		if !ForbiddenField(n) {
			t.Errorf("%q should be forbidden", n)
		}
	}
	for _, n := range []string{"oracle_id", "name", "mana_value", "text", "tag", "pricing"} {
		if ForbiddenField(n) {
			t.Errorf("%q should be allowed", n)
		}
	}
	for _, n := range loadReal(t).TableNames() {
		tb := loadReal(t).Tables[n]
		for _, c := range tb.Columns {
			if c.Visibility == Public && ForbiddenField(c.Name) {
				t.Errorf("%s.%s is public but banned", n, c.Name)
			}
		}
	}
}

func TestSchemaRejectsBannedPublicColumn(t *testing.T) {
	sdir := t.TempDir()
	copyTree(t, realSchema, sdir)
	card := read(t, sdir, "v1/cards.tsv")
	write(t, sdir, "v1/cards.tsv", card+"price_usd\tint\tno\tno\tpublic\tprice\n")
	if _, err := LoadSchema(sdir); err == nil || !strings.Contains(err.Error(), "forbids") {
		t.Fatalf("got %v", err)
	}
}

// forgeLines are shaped like Forge card-script lines, built from pieces so
// this file never holds one at a line start.
var forgeLines = []string{
	"A:" + "AB$ Draw | Cost$ T",
	"SVar:" + "X:Count$Valid Creature",
	"T:Mode" + "$ ChangesZone | Origin$ Any",
	"S:Mode" + "$ Continuous | Affected$ Creature",
	"Cost" + "$ B",
	"AB" + "$ Draw",
}

func TestForgeLineFails(t *testing.T) {
	s := loadReal(t)
	for _, fl := range forgeLines {
		root := validTree(t, s, Public)
		// in a data row (still a legal row to the validator)
		write(t, root, "sets.tsv", "#carddb sets v1\nset_code\tname\trelease_date\tset_type\tparent_code\n"+fl+"\tn\t2026-01-02\tcore\t-\n")
		vs, err := Boundary(s, root)
		mustFail(t, vs, err, "sets.tsv:3: Forge-script-shaped line")
		// and in any other file, including the schema directory
		for _, rel := range []string{"README.md", "notes/x.txt", "schema/v1/extra.txt"} {
			root = validTree(t, s, Public)
			write(t, root, rel, "ok\n"+fl+"\n")
			vs, err = Boundary(s, root)
			mustFail(t, vs, err, rel+":2: Forge-script-shaped line")
		}
	}
	// a mid-line `$ X` is the pattern's fifth alternative; prose is clean
	root := validTree(t, s, Public)
	write(t, root, "README.md", "Cost is $5 and A: not at line start. \n  A: indented\n")
	if vs, err := Boundary(s, root); err != nil || len(vs) != 0 {
		t.Fatalf("%v %v", err, msgs(vs))
	}
}

func TestBoundaryOtherShapes(t *testing.T) {
	s := loadReal(t)
	for name, tc := range map[string]struct{ rel, body, want string }{
		"private table by path":   {"prims/a.tsv", "#carddb cards v1\nx\n", `path names private table "prims"`},
		"banned file name, json":  {"oracle_text.json", "{}\n", `path names forbidden field "oracle_text"`},
		"banned file name, md":    {"notes/tagger.md", "x\n", `path names forbidden field "tagger"`},
		"banned file name, prims": {"prims.json", "{}\n", `path names private table "prims"`},
		"banned file name":        {"price_history.tsv", "#carddb cards v1\nx\n", `path names forbidden field "price_history"`},
		"json field":              {"export.jsonl", "{\"name\":\"a\",\"oracle_text\":\"b\"}\n", `field "oracle_text" is forbidden`},
		"json price field":        {"export.json", "  \"price_usd\": 3,\n", `field "price_usd" is forbidden`},
		"tsv without header":      {"plain.tsv", "a\tb\n", "not a carddb shard"},
		"private table by name":   {"x/tags.tsv", "#carddb tags v1\noracle_id\ttag\n", `private table "tags"`},
		// no banned path component, no banned column, no column line: only the
		// `#carddb decks v1` header can catch this
		"private table by header": {"x/data.tsv", "#carddb decks v1\n", `private table "decks"`},
		// a private shard under another extension is still a shard
		"private table, .txt":     {"x/data.txt", "#carddb text v1\noracle_id\toracle_text\n", `private table "text"`},
		"private table, no ext":   {"x/data", "#carddb decks v1\n", `private table "decks"`},
		"private table, .TSV":     {"x/data.TSV", "#carddb decks v1\n", `private table "decks"`},
		"banned column, .txt":     {"x/data.txt", "#carddb unknown v1\noracle_id\tedhrec_rank\n", `field "edhrec_rank" is forbidden`},
		"banned column, no table": {"y.tsv", "#carddb unknown v1\noracle_id\tedhrec_rank\n", `field "edhrec_rank" is forbidden`},
	} {
		root := validTree(t, s, Public)
		write(t, root, tc.rel, tc.body)
		vs, err := Boundary(s, root)
		if err != nil || !strings.Contains(msgs(vs), tc.want) {
			t.Errorf("%s: %v %q, want %q", name, err, msgs(vs), tc.want)
		}
	}
	// the schema directory names private tables and columns and is allowed to
	root := validTree(t, s, Public)
	copyTree(t, realSchema, filepath.Join(root, SchemaDir))
	if vs, err := Boundary(s, root); err != nil || len(vs) != 0 {
		t.Fatalf("schema dir: %v %v", err, msgs(vs))
	}
	if vs, err := Validate(s, root, Public); err != nil || len(vs) != 0 {
		t.Fatalf("schema dir validate: %v %v", err, msgs(vs))
	}
}

// The schema directory is exempt only for real declarations: anything else at
// or under it, or a root file named `schema`, is refused by both commands.
func TestSchemaDirCannotSmuggle(t *testing.T) {
	s := loadReal(t)
	decl6 := "name\ttype\trequired\tkey\tvisibility\tdoc\nfoo\tstring\tyes\tyes\tprivate\tnote\n"
	leak := "#carddb text v1\noracle_id\toracle_text\nabc\tDraw a card.\n"
	for name, tc := range map[string]struct{ rel, body string }{
		"nested shard":           {"schema/notes/leak.tsv", leak},
		"shard under v1":         {"schema/v1/leak.tsv", leak},
		"decl path, no header":   {"schema/v1/text2.tsv", "name\ttype\trequired\tkey\tvisibility\tdoc\n"},
		"non-tsv":                {"schema/v1/notes.txt", "Draw a card.\n"},
		"json":                   {"schema/export.json", "{\"oracle_text\":\"x\"}\n"},
		"VERSION with extras":    {"schema/VERSION", "1\nDraw a card.\n"},
		"root file named schema": {"schema", leak},
		// the same, at a path the schema does declare: the whole declaration is
		// compared with the trusted schema's, so each of these is refused
		"declared path, free-text first line": {"schema/v1/cards.tsv", "#carddb-schema THIS IS THE ORACLE TEXT OF A CARD\n" + decl6},
		"declared path, prefix only":          {"schema/v1/cards.tsv", "#carddb-schema "},
		"declared path, wrong table":          {"schema/v1/cards.tsv", "#carddb-schema sets v1\n" + decl6},
		"declared path, wrong version":        {"schema/v1/cards.tsv", "#carddb-schema cards v2\n" + decl6},
		// a real declaration plus one row the trusted schema does not hold
		"added public banned row": {"schema/v1/cards.tsv", realDecl(t, "cards") + "oracle_text\tstring\tno\tno\tpublic\tOracle rules text\n"},
		"added price row":         {"schema/v1/cards.tsv", realDecl(t, "cards") + "price_usd\tstring\tno\tno\tpublic\tx\n"},
		"added prims row":         {"schema/v1/sets.tsv", realDecl(t, "sets") + "prims\tstring\tno\tno\tpublic\tx\n"},
		"added private row":       {"schema/v1/cards.tsv", realDecl(t, "cards") + "curator_secret\tstring\tno\tno\tprivate\tx\n"},
		"added innocuous public":  {"schema/v1/cards.tsv", realDecl(t, "cards") + "extra\tstring\tno\tno\tpublic\tx\n"},
		"visibility flipped":      {"schema/v1/cards.tsv", strings.Replace(realDecl(t, "cards"), "\tpublic\t", "\tprivate\t", 1)},
		"dropped row":             {"schema/v1/sets.tsv", realDecl(t, "sets")[:strings.LastIndex(strings.TrimSuffix(realDecl(t, "sets"), "\n"), "\n")+1]},
		// the first line is judged whole, not by its prefix
		"prefix only":            {"schema/v1/leak.tsv", "#carddb-schema "},
		"prefix + free text":     {"schema/v1/leak.tsv", "#carddb-schema THIS IS THE ORACLE TEXT OF A CARD\n" + decl6},
		"prefix + wrong table":   {"schema/v1/leak.tsv", "#carddb-schema cards v1\n" + decl6},
		"prefix + wrong version": {"schema/v1/leak.tsv", "#carddb-schema leak v2\n" + decl6},
		// well-formed, but a table the schema does not declare
		"undeclared table":  {"schema/v1/evil.tsv", "#carddb-schema evil v1\n" + decl6},
		"other version dir": {"schema/v2/cards.tsv", "#carddb-schema cards v2\n" + decl6},
		"VERSION mismatch":  {"schema/VERSION", "2\n"},
	} {
		root := validTree(t, s, Public)
		write(t, root, tc.rel, tc.body)
		vs, err := Validate(s, root, Public)
		if err != nil || !strings.Contains(msgs(vs), "not a schema declaration") {
			t.Errorf("%s: validate: %v %q", name, err, msgs(vs))
		}
		vs, err = Boundary(s, root)
		if err != nil || len(vs) == 0 {
			t.Errorf("%s: boundary passed: %v %q", name, err, msgs(vs))
		}
	}
	// Validate refuses a shard hiding behind another extension
	for _, rel := range []string{"x/data.txt", "x/data", "x/data.TSV"} {
		root := validTree(t, s, Public)
		write(t, root, rel, "#carddb text v1\nx\n")
		vs, err := Validate(s, root, Public)
		if err != nil || len(vs) == 0 {
			t.Errorf("validate %s passed: %v %q", rel, err, msgs(vs))
		}
	}
	// a declaration with a free-form line is refused by the boundary
	root := validTree(t, s, Public)
	copyTree(t, realSchema, filepath.Join(root, SchemaDir))
	write(t, root, "schema/v1/sets.tsv", read(t, root, "schema/v1/sets.tsv")+"Draw a card.\n")
	vs, err := Boundary(s, root)
	mustFail(t, vs, err, "not a schema declaration")
	// the unmodified schema directory still passes
	root = validTree(t, s, Public)
	copyTree(t, realSchema, filepath.Join(root, SchemaDir))
	if vs, err := Boundary(s, root); err != nil || len(vs) != 0 {
		t.Fatalf("real schema dir: %v %v", err, msgs(vs))
	}
}

// schema/writers.tsv is the one config file allowed under schema/, and even
// it has a fixed shape so it cannot hold arbitrary data.
func TestWritersConfig(t *testing.T) {
	s := loadReal(t)
	root := validTree(t, s, Public)
	header := "#carddb-writers v1\nwriter\tsla_hours\ttables\tdoc\n"
	write(t, root, "schema/writers.tsv", header+"catalogue\t24\tcards\tfixture\n")
	if vs, err := Validate(s, root, Public); err != nil || len(vs) != 0 {
		t.Fatalf("writers config: %v %v", err, msgs(vs))
	}
	if vs, err := Boundary(s, root); err != nil || len(vs) != 0 {
		t.Fatalf("writers config boundary: %v %v", err, msgs(vs))
	}
	for _, tc := range []struct{ name, body, want string }{
		{"zero", header + "catalogue\tzero\tcards\tfixture\n", "strict positive int"},
		{"plus", header + "catalogue\t+24\tcards\tfixture\n", "strict positive int"},
		{"leading zero", header + "catalogue\t01\tcards\tfixture\n", "strict positive int"},
		{"huge", header + "catalogue\t999999999999999999999\tcards\tfixture\n", "out of range"},
		{"duplicate", header + "catalogue\t24\tcards\ta\ncatalogue\t36\tcards\tb\n", "duplicate writer"},
		{"forbidden column", "#carddb-writers v1\nwriter\tsla_hours\ttables\toracle_text\ncatalogue\t24\tcards\tx\n", "forbidden field"},
		{"private column", "#carddb-writers v1\nwriter\tsla_hours\ttables\ttext\ncatalogue\t24\tcards\tx\n", "private table"},
		{"over wide", "#carddb-writers v1\nwriter\tsla_hours\ttables\tdoc\textra\ncatalogue\t24\tcards\tfixture\tmore\n", "columns must be exactly"},
	} {
		write(t, root, "schema/writers.tsv", tc.body)
		vs, err := Validate(s, root, Public)
		mustFail(t, vs, err, tc.want)
		vs, err = Boundary(s, root)
		mustFail(t, vs, err, tc.want)
	}
}

// realDecl is the committed declaration file of a table.
func realDecl(t *testing.T, table string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(realSchema, "v1", table+".tsv"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The committed curated files may hold seeded rows; an empty one gets one
// example row, and every tree still validates against the schema.
func TestCuratedFiles(t *testing.T) {
	s := loadReal(t)
	cur := "../../carddb/curated"
	if vs, err := Validate(s, "../../carddb", Public); err != nil || len(vs) != 0 {
		t.Fatalf("committed carddb tree: %v %v", err, msgs(vs))
	}
	if vs, err := Boundary(s, "../../carddb"); err != nil || len(vs) != 0 {
		t.Fatalf("committed carddb boundary: %v %v", err, msgs(vs))
	}
	ents, err := os.ReadDir(cur)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"bracket-flags", "exclusions", "game-changers", "labels", "relations"}
	var got []string
	root := t.TempDir()
	for _, e := range ents {
		name := strings.TrimSuffix(e.Name(), ".tsv")
		got = append(got, name)
		b, err := os.ReadFile(filepath.Join(cur, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		body := string(b)
		if n := strings.Count(body, "\n"); n < 2 {
			t.Errorf("%s: %d lines, want at least the header and column line", e.Name(), n)
		} else if n == 2 { // a header-only curated file: add one example row
			body += strings.Split(shard(s.Tables["curated/"+name], Public, 1), "\n")[2] + "\n"
		}
		write(t, root, "curated/"+e.Name(), body)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("curated files %v, want %v", got, want)
	}
	if vs, err := Validate(s, root, Public); err != nil || len(vs) != 0 {
		t.Fatalf("curated + example rows: %v %v", err, msgs(vs))
	}
}

func copyTree(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.WalkDir(from, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(from, p)
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		write(t, to, filepath.ToSlash(rel), string(b))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
