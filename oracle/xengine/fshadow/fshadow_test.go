package fshadow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/decision"
	"github.com/adams-shaun/gorge/engine/state"
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
	if res.Asks+res.SkippedAsks != asks || res.Snaps+res.SkippedSnaps != snaps || res.Snaps == 0 {
		t.Errorf("followed %d asks (+%d skipped) and %d snaps (+%d skipped), the log has %d and %d", res.Asks, res.SkippedAsks, res.Snaps, res.SkippedSnaps, asks, snaps)
	}
	// Recorder item 7: Forge's turn-1 untap priority pass and its untap
	// snaps have no gorge counterpart and are skipped, not compared.
	if res.SkippedAsks != 1 || res.SkippedSnaps == 0 {
		t.Errorf("skipped %d asks and %d snaps, want the turn-1 untap pass and every untap snap", res.SkippedAsks, res.SkippedSnaps)
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
	l, line := doctor(t, func(a *Ask) bool { return a.Kind == KindPriority && a.Answer.Action == "pass" && a.Step != "untap" }, func(a *Ask) {
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

// The recorder reconciliation (adams-shaun/forge PR #2, "Reader changes
// needed"): one test per item.

// Item 1: an attack on a planeswalker or battle is defender -1 with
// defender_card, and maps onto gorge's option of that Battle; one gorge does
// not offer is answer-missing in that key form.
func TestAttackOnAPermanent(t *testing.T) {
	line := `{"t":"ask","n":1,"seat":0,"turn":3,"step":"declare-attackers","kind":"attack","options":[{"card":101,"defender_card":204}],"answer":{"attacks":[{"card":101,"defender":-1,"defender_card":204}]}}`
	a := parseAsk(t, line)
	if a.Answer.Attacks[0].DefenderCard != 204 || a.Options[0].DefenderCard != 204 || a.Options[0].Player != nil {
		t.Fatalf("parsed %+v", a)
	}
	f := &follower{toG: map[int]state.ObjID{101: 2, 204: 14}, toF: map[state.ObjID]int{2: 101, 14: 204}, name: map[int]string{}}
	d := &decision.Decision{Kind: decision.KAttackers, Options: []decision.Option{
		{Index: 0, Kind: "attacker", Obj: 2, Player: 1},
		{Index: 1, Kind: "attacker", Obj: 2, Player: 1, Battle: 14},
	}}
	in, dv := f.attackers(d, a)
	if dv != nil || len(in.Choices) != 1 || in.Choices[0] != 1 {
		t.Fatalf("intent %+v, divergence %+v: want the Battle option", in, dv)
	}
	if got := forgeOptionKeys(a); len(got) != 1 || got[0] != "attack:101:c204" {
		t.Fatalf("forge keys %v", got)
	}
	if got := f.gorgeOptionKeys(d); len(got) != 2 || got[0] != "attack:101:c204" {
		t.Fatalf("gorge keys %v", got)
	}
	d.Options = d.Options[:1]
	if _, dv := f.attackers(d, a); dv == nil || dv.What != WhatAnswerMissing || dv.Forge != "attack:101:c204" {
		t.Fatalf("divergence %+v", dv)
	}
}

// Items 2 and 8: a spell target is {"spell":id}, its host card's object on
// the stack; a target answer names its host as source.
func TestSpellTargetAndSource(t *testing.T) {
	line := `{"t":"ask","n":4,"seat":1,"turn":3,"step":"main1","kind":"target","options":[{"spell":105},{"player":0}],"answer":{"targets":[{"spell":105}],"source":203}}`
	a := parseAsk(t, line)
	if a.Answer.Source != 203 || a.Answer.Targets[0].Spell != 105 {
		t.Fatalf("parsed %+v", a.Answer)
	}
	f := &follower{toG: map[int]state.ObjID{105: 6, 203: 13}, toF: map[state.ObjID]int{6: 105, 13: 203}, name: map[int]string{105: "Shock", 203: "Spellstutter"}}
	d := &decision.Decision{Kind: decision.KTarget, Options: []decision.Option{{Index: 0, Player: 0}, {Index: 1, Obj: 6}}}
	in, dv := f.targets(d, a)
	if dv != nil || len(in.Choices) != 1 || in.Choices[0] != 1 {
		t.Fatalf("intent %+v, divergence %+v", in, dv)
	}
	if got := forgeOptionKeys(a); len(got) != 2 || got[1] != "spell:105" {
		t.Fatalf("forge keys %v", got)
	}
	cs := f.entryCards(Entry{Ask: a})
	if len(cs) != 2 || cs[0].Name != "Spellstutter" || cs[1].Name != "Shock" {
		t.Fatalf("implicated %+v, want the source then the spell", cs)
	}
}

// Item 3: a priority action "other" has no gorge mapping: a harness finding,
// not a crash.
func TestPriorityOtherIsHarness(t *testing.T) {
	l, line := doctor(t, func(a *Ask) bool { return a.Kind == KindPriority && a.Answer.Action == "pass" && a.Step != "untap" }, func(a *Ask) {
		a.Answer = Answer{Action: "other", Card: a.Seat*100 + 100}
	})
	h := follow(t, l).Hard
	if h == nil || h.What != WhatError || h.Harness != HarnessUnmapped || h.Line != line {
		t.Fatalf("divergence %+v, want an answer-unmapped harness finding at line %d", h, line)
	}
}

// Item 4: end.reason is Forge's GameEndReason name; "unfinished" with no
// winner is a game Forge stopped early, which stops the follow clean, and a
// winner that differs is still terminal.
func TestEndReasons(t *testing.T) {
	l := readGolden(t)
	if e := l.Entries[len(l.Entries)-1].End; e == nil || e.Reason != "AllOpponentsLost" {
		t.Fatalf("golden end %+v", e)
	}
	cut := readGolden(t)
	for i, e := range cut.Entries {
		if e.Ask != nil && e.Ask.Turn == 3 {
			cut.Entries = append(cut.Entries[:i:i], Entry{Line: e.Line, T: TEnd, End: &End{T: TEnd, Reason: ReasonUnfinished}})
			break
		}
	}
	if res := follow(t, cut); res.Hard != nil || res.End != ReasonUnfinished {
		t.Fatalf("unfinished: end %q, hard %+v", res.End, res.Hard)
	}
	w := 1 - *l.Entries[len(l.Entries)-1].End.Winner
	l.Entries[len(l.Entries)-1].End.Winner = &w
	if h := follow(t, l).Hard; h == nil || h.What != WhatTerminal || h.Field != "winner" {
		t.Fatalf("other winner: %+v", h)
	}
}

// Item 5: the opening shuffles come before start (the golden's order), and
// a log with start first reads and follows the same.
func TestShufflesBeforeStart(t *testing.T) {
	l := readGolden(t)
	if l.Entries[0].Shuffle == nil || l.Entries[1].Shuffle == nil || l.Entries[2].Start == nil {
		t.Fatalf("golden opens %s %s %s, want shuffle shuffle start", l.Entries[0].T, l.Entries[1].T, l.Entries[2].T)
	}
	l.Entries[0], l.Entries[1], l.Entries[2] = l.Entries[2], l.Entries[0], l.Entries[1]
	if res := follow(t, l); res.Hard != nil || res.End != "end" {
		t.Fatalf("start first: %+v", res.Hard)
	}
}

// Item 6: mulligans is Forge's cap of 7, which gorge takes as its limit; a
// free_mulligan gorge's seat-count rule disagrees with (a 2-seat game) has
// no Config input, so it is a harness finding, not a crash.
func TestFreeMulliganInexpressible(t *testing.T) {
	l := readGolden(t)
	if l.Header.Mulligans != 7 || l.Header.FreeMulligan {
		t.Fatalf("golden header mulligans %d free %v", l.Header.Mulligans, l.Header.FreeMulligan)
	}
	l.Header.FreeMulligan = true
	h := follow(t, l).Hard
	if h == nil || h.Harness != HarnessSetup || h.Field != "free_mulligan" || h.Line != 1 {
		t.Fatalf("divergence %+v, want a setup-inexpressible harness finding at the header", h)
	}
}

// Item 9: rand is coin or die only. A coin's heads (1) is gorge's winning
// draw 0, a die's 1..bound is gorge's 0..bound-1, and any other kind (Forge
// logs no pick) is chance-misaligned.
func TestRandValues(t *testing.T) {
	for _, c := range []struct {
		r     Rand
		bound int
		want  int
		ok    bool
	}{
		{Rand{Kind: "coin", Bound: 2, Value: 1}, 2, 0, true},
		{Rand{Kind: "coin", Bound: 2, Value: 0}, 2, 1, true},
		{Rand{Kind: "die", Bound: 6, Value: 6}, 6, 5, true},
		{Rand{Kind: "die", Bound: 6, Value: 1}, 6, 0, true},
		{Rand{Kind: "die", Bound: 20, Value: 3}, 6, 0, false},
		{Rand{Kind: "pick", Bound: 3, Value: 1}, 3, 0, false},
	} {
		got, err := randValue(&c.r, 0, c.bound)
		if (err == nil) != c.ok || (c.ok && got != c.want) {
			t.Errorf("%+v bound %d: %d, %v", c.r, c.bound, got, err)
		}
	}
	if chanceHarness(fmt.Errorf("hypothetical chance planner draw 3: x")) != HarnessChance {
		t.Error("a planner failure is not chance-misaligned")
	}
}

// Item 10: snapshot checkpoints are "t<turn>:<step>".
func TestSnapCheckpoints(t *testing.T) {
	re := regexp.MustCompile(`^t[0-9]+:[a-z0-9-]+$`)
	n := 0
	for _, e := range readGolden(t).Entries {
		if e.Snap != nil {
			n++
			if !re.MatchString(e.Snap.Snapshot.Checkpoint) {
				t.Fatalf("checkpoint %q", e.Snap.Snapshot.Checkpoint)
			}
		}
	}
	if n == 0 {
		t.Fatal("no snaps")
	}
}

func parseAsk(t *testing.T, line string) *Ask {
	t.Helper()
	l, err := Read(bytes.NewReader([]byte(`{"t":"header","format":"forge-shadow-1","seats":[{"seat":0},{"seat":1}]}` + "\n" + line + "\n")))
	if err != nil {
		t.Fatal(err)
	}
	return l.Entries[0].Ask
}

// TestSplitCardBindsByJoinedName is the 4-seat pod repro (farm cmdr4, seed
// 13000): Forge's header names a split card "Dusk // Dawn" while gorge's
// front face is "Dusk", and setup refused the whole game ("does not bind").
// The joined name binds a split card only; an adventurer binds by its front
// face alone.
func TestSplitCardBindsByJoinedName(t *testing.T) {
	reg := corpus(t)
	split, ok := reg.Lookup("Dusk // Dawn")
	if !ok {
		t.Fatal("no Dusk // Dawn in the corpus")
	}
	adv, ok := reg.Lookup("Beanstalk Giant")
	if !ok {
		t.Fatal("no Beanstalk Giant in the corpus")
	}
	for _, c := range []struct {
		card *cards.Card
		name string
		want bool
	}{
		{split, "Dusk // Dawn", true},
		{split, "dusk // dawn", true},
		{split, "Dusk", true},
		{split, "Dawn", false},
		{adv, "Beanstalk Giant", true},
		{adv, "Beanstalk Giant // Fertile Footsteps", false},
		{nil, "Dusk", false},
	} {
		if got := bindsName(c.card, c.name); got != c.want {
			t.Errorf("bindsName(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}
