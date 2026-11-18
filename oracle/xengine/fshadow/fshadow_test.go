package fshadow

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

func corpus(t *testing.T) *cards.Registry {
	t.Helper()
	return testutil.CorpusRegistry(t)
}

func readGolden(t *testing.T) *Log {
	t.Helper()
	b, err := os.ReadFile(filepath.FromSlash(goldenPath))
	if err != nil {
		t.Fatal(err)
	}
	l, err := Read(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func follow(t *testing.T, l *Log) *Result {
	t.Helper()
	res, err := Follow(corpus(t), l, Options{})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// TestGoldenReplaysClean is the follower's repro: the golden 2-seat vanilla
// log (a mulligan, land plays, paid casts, attacks, a block, an
// empty-library loss) replays in gorge with no divergence, every snapshot
// agreeing.
func TestGoldenReplaysClean(t *testing.T) {
	l := readGolden(t)
	res := follow(t, l)
	if res.Hard != nil {
		b, _ := json.MarshalIndent(res.Hard, "", "  ")
		t.Fatalf("hard divergence:\n%.4000s", b)
	}
	if len(res.Soft) != 0 {
		t.Errorf("soft differences: %+v", res.Soft)
	}
	if res.End != "end" {
		t.Errorf("end %q, want the log's end line matched", res.End)
	}
	asks, snaps := 0, 0
	for _, e := range l.Entries {
		if e.Ask != nil {
			asks++
		}
		if e.Snap != nil {
			snaps++
		}
	}
	if res.Asks != asks || res.Snaps != snaps || snaps == 0 {
		t.Errorf("followed %d asks and %d snaps, the log has %d and %d", res.Asks, res.Snaps, asks, snaps)
	}
	t.Logf("fshadow: clean: %d decisions, %d asks, %d snaps", res.Decisions, res.Asks, res.Snaps)
}

// doctor returns the golden log with the first ask matching pick changed by
// edit, and that ask's line.
func doctor(t *testing.T, pick func(*Ask) bool, edit func(*Ask)) (*Log, int) {
	t.Helper()
	l := readGolden(t)
	for i := range l.Entries {
		if a := l.Entries[i].Ask; a != nil && pick(a) {
			edit(a)
			return l, l.Entries[i].Line
		}
	}
	t.Fatal("no ask to doctor")
	return nil, 0
}

// TestDoctoredAnswerReportsAnswerMissing: one land play doctored to a card
// still in the library is not on gorge's list, so the follower stops there
// with answer-missing and writes divergence-1.json.
func TestDoctoredAnswerReportsAnswerMissing(t *testing.T) {
	l, line := doctor(t, func(a *Ask) bool { return a.Answer.Action == "land" }, func(a *Ask) {
		a.Answer.Card = libraryForest(t, a.Seat)
	})
	res := follow(t, l)
	h := res.Hard
	if h == nil {
		t.Fatal("no divergence")
	}
	if h.What != WhatAnswerMissing || h.Kind != "priority" || h.Line != line {
		t.Fatalf("divergence %s %s at line %d, want answer-missing priority at line %d (%+v)", h.What, h.Kind, h.Line, line, h)
	}
	if h.Record == nil || len(h.Record.Chance) == 0 || len(h.Decks) != 2 || h.Seed != 7 {
		t.Errorf("divergence lacks the record prefix, chance or game fields: %+v", h)
	}
	if len(h.Cards) != 1 || h.Cards[0].Name != "Forest" || h.Cards[0].Primitive != "action:land" {
		t.Errorf("implicated %v, want [Forest]", h.Cards)
	}
	dir := t.TempDir()
	path, err := WriteDivergence(dir, "golden-doctored", goldenPath, h)
	if err != nil {
		t.Fatal(err)
	}
	back := readFile(t, path)
	if back.Schema != DivergenceSchema || back.What != WhatAnswerMissing || back.Pair != PairFG || back.Offset != int64(line) || filepath.Base(path) != "divergence-1.json" {
		t.Fatalf("%s: %+v", path, back)
	}
	if _, err := os.Stat(filepath.Join(dir, back.Record)); back.Record == "" || err != nil {
		t.Fatalf("record %q: %v", back.Record, err)
	}
	t.Logf("fshadow: %s %s at line %d: forge %s", h.What, h.Kind, h.Line, h.Forge)
}

// libraryForest is a Forest of seat's deck that no opening hand holds: one
// the opening shuffle left in the library past the seven drawn.
func libraryForest(t *testing.T, seat int) int {
	l := readGolden(t)
	for _, e := range l.Entries {
		if s := e.Shuffle; s != nil && s.Seat == seat {
			for _, id := range s.Order[7:] {
				if l.Header.Seats[seat].Cards[id-100*(seat+1)].Name == "Forest" {
					return id
				}
			}
		}
	}
	t.Fatalf("seat %d has no Forest in its library", seat)
	return 0
}

// TestDoctoredPaymentReportsPayment: a cast whose logged sources gorge has no
// plan for is the payment class of answer-missing.
func TestDoctoredPaymentReportsPayment(t *testing.T) {
	l, line := doctor(t, func(a *Ask) bool { return a.Answer.Action == "cast" }, func(a *Ask) {
		a.Answer.Tapped = a.Answer.Tapped[:1]
	})
	h := follow(t, l).Hard
	if h == nil || h.What != WhatAnswerMissing || h.Field != "payment" || h.Line != line {
		t.Fatalf("divergence %+v, want answer-missing payment at line %d", h, line)
	}
}

// TestDoctoredSnapshotReportsSnapshot: a snapshot whose life total differs
// is a snapshot divergence carrying both states.
func TestDoctoredSnapshotReportsSnapshot(t *testing.T) {
	l := readGolden(t)
	line := 0
	for i := range l.Entries {
		if s := l.Entries[i].Snap; s != nil && s.N == 3 {
			s.Snapshot.Players[0].Life--
			line = l.Entries[i].Line
		}
	}
	h := follow(t, l).Hard
	if h == nil || h.What != WhatSnapshot || h.Field != "p0.life" || h.Line != line || h.GorgeSnapshot == nil || h.ForgeSnapshot == nil {
		t.Fatalf("divergence %+v, want snapshot p0.life at line %d", h, line)
	}
}

// TestOneSidedOptionIsSoft: an option Forge offers and gorge does not, with
// Forge's answer still on gorge's list, is recorded and does not stop.
func TestOneSidedOptionIsSoft(t *testing.T) {
	l, line := doctor(t, func(a *Ask) bool { return a.Kind == KindPriority && a.Answer.Action == "pass" }, func(a *Ask) {
		a.Options = append(a.Options, Option{Action: "cast", Card: 999})
	})
	res := follow(t, l)
	if res.Hard != nil {
		t.Fatalf("hard divergence %+v", res.Hard)
	}
	if len(res.Soft) != 1 || res.Soft[0].Line != line || len(res.Soft[0].OnlyForge) != 1 || res.Soft[0].OnlyForge[0] != "cast:999" {
		t.Fatalf("soft %+v, want one only-Forge cast:999 at line %d", res.Soft, line)
	}
}

func TestReadRoundTrip(t *testing.T) {
	b, err := os.ReadFile(filepath.FromSlash(goldenPath))
	if err != nil {
		t.Fatal(err)
	}
	l, err := Read(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Write(&out, l); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out.Bytes(), b) {
		t.Fatal("Write(Read(golden)) differs from the golden")
	}
	if _, err := Read(bytes.NewReader([]byte(`{"t":"ask","n":1}` + "\n"))); err == nil {
		t.Fatal("a log with no header read")
	}
}

// readFile reads a divergence file strictly: a field outside File's list
// fails.
func readFile(t *testing.T, path string) File {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var f File
	if err := dec.Decode(&f); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return f
}

// adjudicateFields is adjudicate.Divergence's and adjudicate.Implicated's
// JSON field lists (gorge-compliance PR #221,
// oracle/xengine/adjudicate/fshadow.go, schema fshadow-divergence-1), which
// File and Implicated mirror. A change to either side changes this list.
var adjudicateFields = map[string][]string{
	"divergence": {"schema", "game", "seed", "decks", "log", "offset", "pair", "resynced", "what", "kind", "field", "gorge", "other", "cards", "record", "repro", "harness", "msg"},
	"implicated": {"name", "token", "ability", "trigger", "primitive"},
}

func jsonFields(v any) []string {
	var out []string
	rt := reflect.TypeOf(v)
	for i := 0; i < rt.NumField(); i++ {
		tag, _, _ := strings.Cut(rt.Field(i).Tag.Get("json"), ",")
		out = append(out, tag)
	}
	return out
}

// TestDivergenceFileFields round-trips a written divergence file through
// adjudicate's field list: File and Implicated carry exactly those fields,
// and a file of every field set reads back unchanged.
func TestDivergenceFileFields(t *testing.T) {
	if got := jsonFields(File{}); !reflect.DeepEqual(got, adjudicateFields["divergence"]) {
		t.Fatalf("File fields %v, adjudicate's %v", got, adjudicateFields["divergence"])
	}
	if got := jsonFields(Implicated{}); !reflect.DeepEqual(got, adjudicateFields["implicated"]) {
		t.Fatalf("Implicated fields %v, adjudicate's %v", got, adjudicateFields["implicated"])
	}
	k := 2
	d := &Divergence{N: 3, What: WhatSnapshot, Kind: "priority", Decks: []string{"a", "b"}, Seed: 9, Line: 41, Ask: 5, Seat: 1,
		Field: "p0.life", Gorge: "20", Forge: "19", Detail: "x", Cards: []Implicated{{Name: "Grizzly Bears", Ability: &k}}}
	dir := t.TempDir()
	path, err := WriteDivergence(dir, "g1", "run/g1.jsonl", d)
	if err != nil {
		t.Fatal(err)
	}
	f := readFile(t, path)
	want := FileOf(d, "g1", "run/g1.jsonl", "")
	if !reflect.DeepEqual(f, want) {
		t.Fatalf("read back %+v, wrote %+v", f, want)
	}
	raw, _ := os.ReadFile(path)
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{}
	for _, n := range adjudicateFields["divergence"] {
		allowed[n] = true
	}
	for n := range keys {
		if !allowed[n] {
			t.Errorf("field %q is not adjudicate's", n)
		}
	}
	if string(keys["schema"]) != `"`+DivergenceSchema+`"` {
		t.Errorf("schema %s", keys["schema"])
	}
}
