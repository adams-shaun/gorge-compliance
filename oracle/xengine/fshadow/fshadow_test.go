package fshadow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclediff"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/decision"
	"github.com/adams-shaun/gorge/engine/rules"
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

// TestTargetGroupsOneChoicePerGroup is the farm cmdr4 repro (BIG/DFT/WOE
// s148 cmdr4, pod 14801, offset 388): a combined charm target ask (gorge
// KTarget with one Option.Group per target-bearing mode) was answered by
// first-match object, so two Forge targets that were both legal for the
// first mode landed in that one group and Decision.Validate rejected the
// answer with `choices 0 and 1 are mutually exclusive (group
// "charm-mode-0")`. The follower must choose one option per group; the
// no-log-line default answer is bound by the same wire contract.
func TestTargetGroupsOneChoicePerGroup(t *testing.T) {
	// Two modes, each able to target either of the same two permanents, so
	// first-match by object puts both picks in charm-mode-0.
	line := `{"t":"ask","n":388,"seat":0,"turn":5,"step":"main1","kind":"target","options":[{"card":20},{"card":30},{"card":20},{"card":30}],"answer":{"targets":[{"card":20},{"card":30}]}}`
	a := parseAsk(t, line)
	f := &follower{toG: map[int]state.ObjID{20: 2, 30: 3}, toF: map[state.ObjID]int{2: 20, 3: 30}, name: map[int]string{}}
	d := &decision.Decision{Seq: 9, Player: 0, Kind: decision.KTarget, Min: 2, Max: 2,
		Options: []decision.Option{
			{Index: 0, Kind: "card", Obj: 2, Group: "charm-mode-0"},
			{Index: 1, Kind: "card", Obj: 3, Group: "charm-mode-0"},
			{Index: 2, Kind: "card", Obj: 2, Group: "charm-mode-1"},
			{Index: 3, Kind: "card", Obj: 3, Group: "charm-mode-1"},
		}}
	// Pin the fixture to the pod's rejection: first-match choices [0 1] are
	// both charm-mode-0, and the engine's own fence refuses them with the
	// divergence's message.
	naive := decision.Intent{Seq: 9, Player: 0, Choices: []int{0, 1}}
	if err := d.Validate(naive); err == nil || !strings.Contains(err.Error(), `mutually exclusive (group "charm-mode-0")`) {
		t.Fatalf("fixture no longer reproduces the pod rejection: %v", err)
	}
	in, dv := f.targets(d, a)
	if dv != nil {
		t.Fatalf("logged target ask: divergence %+v", dv)
	}
	if len(in.Choices) != 2 || d.Options[in.Choices[0]].Group == d.Options[in.Choices[1]].Group {
		t.Fatalf("intent %+v: want one choice per charm mode group", in)
	}
	if err := d.Validate(in); err != nil {
		t.Errorf("intent %+v rejected by the wire contract: %v", in, err)
	}

	// A target decision with no log line is answered by default: it must obey
	// the same Group contract, not take the first Min options (all of mode 0).
	prio := parseAsk(t, `{"t":"ask","n":389,"seat":0,"turn":5,"step":"main1","kind":"priority","answer":{"action":"pass"}}`)
	f2 := &follower{log: &Log{}}
	din, ok := f2.answer(d, Entry{Line: 389, T: TAsk, Ask: prio})
	if !ok || f2.res.Hard != nil {
		t.Fatalf("default target answer: ok %v hard %+v", ok, f2.res.Hard)
	}
	if len(din.Choices) != 2 || d.Options[din.Choices[0]].Group == d.Options[din.Choices[1]].Group {
		t.Fatalf("default intent %+v: want one choice per charm mode group", din)
	}
	if err := d.Validate(din); err != nil {
		t.Errorf("default intent %+v rejected by the wire contract: %v", din, err)
	}

	// An ungrouped ask is a different shape: no Option.Group means no
	// exclusivity, so the default must stay firstMin's [0 1]. Giving every
	// slot the whole offer list answers [0 0], which Validate rejects as a
	// duplicate choice.
	ungrouped := &decision.Decision{Seq: 10, Player: 0, Kind: decision.KTarget, Min: 2, Max: 2,
		Options: []decision.Option{
			{Index: 0, Kind: "card", Obj: 2},
			{Index: 1, Kind: "card", Obj: 3},
			{Index: 2, Kind: "player", Player: 1},
		}}
	uin, ok := f2.answer(ungrouped, Entry{Line: 390, T: TAsk, Ask: prio})
	if !ok || f2.res.Hard != nil {
		t.Fatalf("ungrouped default target answer: ok %v hard %+v", ok, f2.res.Hard)
	}
	if !reflect.DeepEqual(uin.Choices, []int{0, 1}) {
		t.Fatalf("ungrouped default intent %+v: want first Min choices [0 1]", uin)
	}
	if err := ungrouped.Validate(uin); err != nil {
		t.Errorf("ungrouped default intent %+v rejected by the wire contract: %v", uin, err)
	}
}

// TestInvalidIntentIsHarness: a submit-time wire rejection is a follower
// construction bug, not a gorge class. The farm cmdr4 s148 finding was a
// Decision.Validate refusal (two target options of one charm-mode group),
// which only the follower's mapping could have built; it must carry a
// harness reason so adjudicate files it as the follower's, and a legal
// intent whose submit fails for an engine reason must stay a gorge
// finding. The fence is pure, so no shadow log is needed.
func TestInvalidIntentIsHarness(t *testing.T) {
	d := &decision.Decision{Seq: 7, Player: 0, Kind: decision.KTarget, Min: 2, Max: 2,
		Options: []decision.Option{
			{Index: 0, Kind: "card", Obj: 2, Group: "charm-mode-0"},
			{Index: 1, Kind: "card", Obj: 3, Group: "charm-mode-0"},
			{Index: 2, Kind: "card", Obj: 2, Group: "charm-mode-1"},
			{Index: 3, Kind: "card", Obj: 3, Group: "charm-mode-1"},
		}}
	in := decision.Intent{Seq: 7, Player: 0, Choices: []int{0, 1}}
	err := d.Validate(in)
	if err == nil || !strings.Contains(err.Error(), `mutually exclusive (group "charm-mode-0")`) {
		t.Fatalf("fixture is not wire-invalid: %v", err)
	}
	dv := Divergence{What: WhatError, Detail: "submit: " + err.Error(), Harness: submitHarness(d, in, err)}
	if dv.Harness != HarnessIntent || dv.Harness == "" {
		t.Fatalf("divergence %+v: want harness %q, not a gorge class", dv, HarnessIntent)
	}

	// A legal intent (one option per group) that Submit refuses for an
	// engine-side reason stays a gorge finding.
	ok := decision.Intent{Seq: 7, Player: 0, Choices: []int{0, 2}}
	if err := d.Validate(ok); err != nil {
		t.Fatalf("fixture legal intent rejected: %v", err)
	}
	if got := submitHarness(d, ok, fmt.Errorf("engine broke")); got != "" {
		t.Fatalf("legal intent classified %q, want a gorge finding", got)
	}
	if got := submitHarness(d, ok, fmt.Errorf("hypothetical chance planner draw 3: x")); got != HarnessChance {
		t.Fatalf("planner failure classified %q, want %q", got, HarnessChance)
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

// TestForgeLibraryTopCutToGorgeDepth is the second pod repro (farm cmdr4,
// seeds 13000 and 13002): Forge's SnapshotWriter names 5 library cards and
// gorge's snapshot 3, so the first checkpoint of every real Forge game was a
// p0.library_top snapshot divergence. The Forge side is cut to gorge's depth
// before the compare, and a difference within it still counts.
func TestForgeLibraryTopCutToGorgeDepth(t *testing.T) {
	x := rules.OracleSnapshot{Players: []rules.OracleSnapPlayer{
		{Seat: 0, LibraryTop: []string{"Forest", "Island", "Swamp", "Mountain", "Plains"}},
		{Seat: 1, LibraryTop: []string{"Forest"}},
	}}
	got := sameLibraryDepth(x)
	if want := []string{"Forest", "Island", "Swamp"}; !reflect.DeepEqual(got.Players[0].LibraryTop, want) {
		t.Errorf("seat 0 library_top = %v, want %v", got.Players[0].LibraryTop, want)
	}
	if !reflect.DeepEqual(got.Players[1].LibraryTop, []string{"Forest"}) {
		t.Errorf("seat 1 library_top = %v, want [Forest]", got.Players[1].LibraryTop)
	}
	if len(x.Players[0].LibraryTop) != 5 {
		t.Errorf("the log's snapshot was modified: %v", x.Players[0].LibraryTop)
	}
	g := x
	g.Players = []rules.OracleSnapPlayer{
		{Seat: 0, LibraryTop: []string{"Forest", "Island", "Swamp"}},
		{Seat: 1, LibraryTop: []string{"Forest"}},
	}
	if v := oraclediff.CompareOpts(rules.OracleResult{Snapshots: []rules.OracleSnapshot{g}}, nil, oraclediff.XResult{Snapshots: []rules.OracleSnapshot{got}}, nil); v.Status != oraclediff.Agree {
		t.Errorf("gorge's 3 against Forge's first 3: %v %s", v.Status, v.Field)
	}
	g.Players[0].LibraryTop = []string{"Forest", "Swamp", "Island"}
	if v := oraclediff.CompareOpts(rules.OracleResult{Snapshots: []rules.OracleSnapshot{g}}, nil, oraclediff.XResult{Snapshots: []rules.OracleSnapshot{got}}, nil); v.Status == oraclediff.Agree {
		t.Error("an order difference within gorge's depth agreed")
	}
}

// TestModesWithoutALogLineAnsweredByDefault is the farm cmdr4 repro (seeds
// 13001 and 12901): gorge asks a modal pick where Forge's next log line is
// the seat's priority, because Forge's recorder writes no line for a modal
// answer. The follower used to stop there with kind "modes seat 2" against
// "priority seat 2"; it now answers by default and consumes no line.
func TestModesWithoutALogLineAnsweredByDefault(t *testing.T) {
	prio := parseAsk(t, `{"t":"ask","n":113,"seat":2,"turn":3,"step":"main1","kind":"priority","answer":{"action":"pass"}}`)
	modes := func(min, max int, n int) *decision.Decision {
		d := &decision.Decision{Seq: 5, Player: 2, Kind: decision.KModes, Min: min, Max: max}
		for i := 0; i < n; i++ {
			d.Options = append(d.Options, decision.Option{Index: i, Kind: "mode"})
		}
		return d
	}
	for _, c := range []struct {
		name string
		d    *decision.Decision
		want []int
	}{
		{"choose one", modes(1, 1, 3), []int{0}},
		{"choose two", modes(2, 2, 3), []int{0, 1}},
		{"choose both", modes(2, 2, 2), []int{0, 1}},
		{"up to two takes one", modes(0, 2, 3), []int{0}},
		{"clamped to the offer", modes(3, 3, 2), []int{0, 1}},
		{"repeatable cycles", func() *decision.Decision { d := modes(3, 3, 2); d.Repeatable = true; return d }(), []int{0, 1, 0}},
		{"budget keeps the sum", func() *decision.Decision {
			d := modes(1, 3, 3)
			d.Budgeted, d.MaxSum = true, 2
			d.Options[0].Value, d.Options[1].Value, d.Options[2].Value = 1, 2, 1
			return d
		}(), []int{0}},
	} {
		f := &follower{log: &Log{}}
		in, ok := f.answer(c.d, Entry{Line: 156, T: TAsk, Ask: prio})
		if !ok || f.res.Hard != nil {
			t.Fatalf("%s: hard divergence %+v", c.name, f.res.Hard)
		}
		if !reflect.DeepEqual(in.Choices, c.want) || in.Seq != 5 || in.Player != 2 {
			t.Errorf("%s: intent %+v, want choices %v", c.name, in, c.want)
		}
		if f.pos != 0 || f.res.Modes != 1 || f.res.Asks != 0 {
			t.Errorf("%s: pos %d modes %d asks %d, want the log line left unconsumed", c.name, f.pos, f.res.Modes, f.res.Asks)
		}
	}
}

// TestUnloggedArrangeIsAnsweredByDefault is the farm cmdr4 repro (seeds
// 13000, 13003 and the HOB/OTJ pods): Forge's recorder logs no scry or
// surveil answer (BridgeController.arrangeForScry/arrangeForSurveil take the
// fallback path, no ask line), so a scry land's arrange ask was matched
// against the next priority line and read as a kind divergence ("arrange seat
// 1" vs "priority seat 1"). An arrange the log has no line for is gorge's
// own bookkeeping: the follower keeps the offered cards on top in order,
// consumes no log line, and Forge's real choice shows at the next
// library_top snapshot instead.
func TestUnloggedArrangeIsAnsweredByDefault(t *testing.T) {
	l := readGolden(t)
	const temple = "Temple of Mystery"
	if _, ok := corpus(t).Lookup(temple); !ok {
		t.Skipf("no %s in the corpus", temple)
	}
	// Seat 1's first land play (card 206) is a scry-1 land.
	for i := range l.Header.Seats[1].Cards {
		if l.Header.Seats[1].Cards[i].ID == 206 {
			l.Header.Seats[1].Cards[i].Name = temple
		}
	}
	entries := l.Entries[:0]
	for _, e := range l.Entries {
		if e.Snap == nil { // the doctored deck changes every later snapshot
			entries = append(entries, e)
		}
	}
	l.Entries = entries
	res := follow(t, l)
	// The land play is ask 4; the arrange follows it, before ask 5. The
	// doctored deck may diverge later (the scry trigger gives both seats
	// priority passes the golden log lacks), but not at the arrange.
	if res.Hard != nil && res.Hard.Ask <= 5 {
		b, _ := json.MarshalIndent(res.Hard, "", "  ")
		t.Fatalf("divergence at or before the scry land's arrange:\n%s", b)
	}
	if res.Decisions < 5 {
		t.Errorf("only %d decisions followed; the arrange was not passed (hard %+v)", res.Decisions, res.Hard)
	}
	if res.Arranges == 0 {
		t.Error("no default arrange answered")
	}
}

// TestMulliganBottomWaitsForTheBottomingAsk is the farm cmdr4 repro (EOE and
// OTJ pods, seed 13003, log line 17): Forge bottoms a London mulligan's cards
// right after drawing, so its mull line for a kept hand carries the bottom;
// gorge asks every seat keep-or-mulligan first and the bottoming of every
// kept hand after, in seat order. In a 4-seat pod the next keep ask belongs
// to another seat, and the follower, still holding the first seat's line for
// its bottom, stopped on "mulligan seat: gorge 3, Forge 1". The bottom is
// now remembered by seat and answers the bottoming ask whenever it comes.
func TestMulliganBottomWaitsForTheBottomingAsk(t *testing.T) {
	l, err := Read(strings.NewReader(`{"t":"header","format":"forge-shadow-1","seats":[{"seat":0},{"seat":1},{"seat":2},{"seat":3}]}
{"t":"mull","seat":1,"keep":true,"bottom":[201,202]}
{"t":"mull","seat":3,"keep":true}
{"t":"ask","n":1,"seat":1,"turn":1,"step":"main1","kind":"priority","answer":{"action":"pass"}}
`))
	if err != nil {
		t.Fatal(err)
	}
	f := &follower{log: l, toG: map[int]state.ObjID{201: 11, 202: 12, 203: 13}}
	keepAsk := func(p state.PlayerID) *decision.Decision {
		return &decision.Decision{Seq: 1, Player: p, Kind: decision.KMulligan, Min: 1, Max: 1,
			Options: []decision.Option{{Index: 0, Kind: "keep"}, {Index: 1, Kind: "mulligan"}}}
	}
	bottomAsk := &decision.Decision{Seq: 3, Player: 1, Kind: decision.KMulligan, Min: 2, Max: 2,
		Options: []decision.Option{{Index: 0, Kind: "bottom", Obj: 13}, {Index: 1, Kind: "bottom", Obj: 12}, {Index: 2, Kind: "bottom", Obj: 11}}}
	for _, c := range []struct {
		d    *decision.Decision
		want []int
		pos  int
	}{
		{keepAsk(1), []int{0}, 1},
		{keepAsk(3), []int{0}, 2},
		{bottomAsk, []int{2, 1}, 2},
	} {
		in, ok := f.answer(c.d, l.Entries[f.pos])
		if !ok || f.res.Hard != nil {
			t.Fatalf("%s for seat %d: hard divergence %+v", c.d.Kind, c.d.Player, f.res.Hard)
		}
		if !reflect.DeepEqual(in.Choices, c.want) || f.pos != c.pos {
			t.Errorf("seat %d: choices %v pos %d, want %v pos %d", c.d.Player, in.Choices, f.pos, c.want, c.pos)
		}
	}
}

// mulliganFarmLog is the farm cmdr4 pod-14400 pregame (ECL/OTJ/TLA
// s144-cmdr4, seed 14400) as forge-shadow-1 records it: four 9-card seats,
// seat 0 the random-policy seat. Seat 0 mulligans seven times in a row while
// the other three keep, so its eighth mulligan is line 24, the farm's
// divergence offset. Forge's London rule still offers that mulligan: CR
// 103.5c's free first mulligan of a multiplayer game does not count toward
// the number of mulligans the player may take, and Forge derives its cap from
// the counted ones. gorge's Config.Mulligans counts the free mulligan too
// (gorge's own crcoverage 103.5c row records the deviation), so the follower
// reached line 24 with only "keep" on gorge's list.
//
// withNoOp adds what Forge writes next when the random seat answers
// "mulligan" again with an empty hand: AbstractMulligan.mulligan() returns
// without a redraw or an increment, so Forge logs a state-neutral mull and
// asks again. The keep that follows carries no bottom (the recorder cleared
// the tucked cards on the no-op line) and an empty hand, so the whole hand
// goes to the library bottom.
func mulliganFarmLog(withNoOp bool) *Log {
	const seats, deck = 4, 9
	h := Header{T: THeader, Format: Format, Seed: 14400, ForgeRef: "farm-cmdr4",
		Mulligans: 7, FreeMulligan: true}
	for s := 0; s < seats; s++ {
		seat := Seat{Seat: s, Name: fmt.Sprintf("seat%d", s)}
		for k := 0; k < deck; k++ {
			name := "Forest"
			if k%2 == 1 {
				name = "Grizzly Bears"
			}
			seat.Cards = append(seat.Cards, Card{ID: (s+1)*100 + k, Name: name})
		}
		h.Seats = append(h.Seats, seat)
	}
	order := func(s int) []int {
		ids := make([]int, deck)
		for k := range ids {
			ids[k] = (s+1)*100 + k
		}
		return ids
	}
	l := &Log{Header: h}
	line := 2 // the header is line 1
	add := func(e Entry) {
		e.Line = line
		line++
		l.Entries = append(l.Entries, e)
	}
	for s := 0; s < seats; s++ {
		add(Entry{T: TShuffle, Shuffle: &Shuffle{T: TShuffle, Seat: s, Order: order(s)}})
	}
	add(Entry{T: TStart, Start: &Start{T: TStart, Seat: 0}})
	for m := 1; m <= 8; m++ {
		// Before mulligan m, Forge has bottomed max(0, m-2) cards of its
		// seven (the first mulligan of a multiplayer game is free).
		n := 7
		if m >= 3 {
			n = 9 - m
		}
		hand := append([]int(nil), order(0)[:n]...)
		add(Entry{T: TMull, Mull: &Mull{T: TMull, Seat: 0, Keep: false, Hand: &hand}})
		if m == 1 {
			for s := 1; s < seats; s++ {
				add(Entry{T: TMull, Mull: &Mull{T: TMull, Seat: s, Keep: true}})
			}
		}
		// The redraw shuffle after this mulligan (line 24's is line 25).
		add(Entry{T: TShuffle, Shuffle: &Shuffle{T: TShuffle, Seat: 0, Order: order(0)}})
	}
	if withNoOp {
		add(Entry{T: TMull, Mull: &Mull{T: TMull, Seat: 0, Keep: false, Hand: &[]int{}}})
		add(Entry{T: TMull, Mull: &Mull{T: TMull, Seat: 0, Keep: true, Hand: &[]int{}}})
	} else {
		add(Entry{T: TMull, Mull: &Mull{T: TMull, Seat: 0, Keep: true,
			Hand: &[]int{}, Bottom: append([]int(nil), order(0)[:7]...)}})
	}
	add(Entry{T: TEnd, End: &End{T: TEnd, Reason: ReasonUnfinished}})
	return l
}

// TestMulliganAllowanceCoversTheFreeMulligan is the farm cmdr4 repro
// (ECL/OTJ/TLA s144-cmdr4, pod-14400 seed 14400, log line 24, class
// answer-missing|mulligan): the random-policy seat answers its eighth
// mulligan and gorge's list has only "keep", because the follower handed
// Config.Mulligans Forge's counted cap and gorge counts the free first
// mulligan against the allowance too. The follower must translate the
// header's cap into the allowance gorge's Config actually means (cap plus the
// free mulligan of CR 103.5c), so the eighth mulligan and the keep that
// follows it are answered.
func TestMulliganAllowanceCoversTheFreeMulligan(t *testing.T) {
	l := mulliganFarmLog(false)
	eighth := 0
	seen := 0
	for _, e := range l.Entries {
		if m := e.Mull; m != nil && m.Seat == 0 && !m.Keep {
			seen++
			if seen == 8 {
				eighth = e.Line
			}
		}
	}
	if eighth != 24 {
		t.Fatalf("the eighth mulligan is at line %d, want the farm's line 24", eighth)
	}
	res := follow(t, l)
	if res.Hard != nil {
		b, _ := json.MarshalIndent(res.Hard, "", "  ")
		t.Fatalf("divergence at line %d:\n%.3000s", res.Hard.Line, b)
	}
	// 4 keeps/mulligans in the first pass, seven more mulligans, then the
	// keep that ends the round. The truncated log's unfinished end line stops
	// the follower at the pending bottoming ask, so the keep is the last
	// decision; the bottoming itself is covered by
	// TestMulliganEmptyHandKeepBottomsTheWholeHand.
	if res.Decisions < 12 {
		t.Errorf("followed only %d decisions; the eighth mulligan was not answered", res.Decisions)
	}
	if res.End != ReasonUnfinished {
		t.Errorf("end %q, want the log's unfinished end", res.End)
	}
}

// TestMulliganNoOpAtEmptyHandKeepsForGorge is the tail of the same pod: after
// the eighth mulligan the random seat's hand is empty, and Forge can log one
// or more further "mulligan" answers that do nothing (AbstractMulligan
// returns without a redraw). gorge's allowance is spent, so its only option
// is keep; a no-op line must be skipped, and the eventual keep (whose bottom
// the recorder cleared) bottoms the whole hand.
func TestMulliganNoOpAtEmptyHandKeepsForGorge(t *testing.T) {
	l := mulliganFarmLog(true)
	res := follow(t, l)
	if res.Hard != nil {
		b, _ := json.MarshalIndent(res.Hard, "", "  ")
		t.Fatalf("divergence at line %d:\n%.3000s", res.Hard.Line, b)
	}
	if res.SkippedMulls != 1 {
		t.Errorf("skipped %d empty-hand mulligans, want 1", res.SkippedMulls)
	}
	if res.Decisions < 12 {
		t.Errorf("followed only %d decisions; the keep after the no-op was not answered", res.Decisions)
	}
	if res.End != ReasonUnfinished {
		t.Errorf("end %q, want the log's unfinished end", res.End)
	}
}

// TestMulliganEmptyHandKeepBottomsTheWholeHand drives the keep and bottoming
// answers of the no-op tail directly: a keep line with an empty hand and no
// recorded bottom (the recorder cleared it) must bottom the whole gorge hand
// at the bottoming ask, and an ask that cannot reach an empty hand is a
// genuine answer-missing, not a silent partial bottom.
func TestMulliganEmptyHandKeepBottomsTheWholeHand(t *testing.T) {
	f := &follower{log: &Log{}}
	keep := &decision.Decision{Seq: 1, Player: 0, Kind: decision.KMulligan, Min: 1, Max: 1,
		Options: []decision.Option{{Index: 0, Kind: "keep"}}}
	if in, ok := f.answer(keep, Entry{Line: 27, T: TMull, Mull: &Mull{T: TMull, Seat: 0, Keep: true, Hand: &[]int{}}}); !ok || !reflect.DeepEqual(in.Choices, []int{0}) {
		t.Fatalf("keep: intent %+v ok %v, hard %+v", in, ok, f.res.Hard)
	}
	bottom := func(min int, n int) *decision.Decision {
		d := &decision.Decision{Seq: 2, Player: 0, Kind: decision.KMulligan, Min: min, Max: min}
		for i := 0; i < n; i++ {
			d.Options = append(d.Options, decision.Option{Index: i, Kind: "bottom", Obj: state.ObjID(10 + i)})
		}
		return d
	}
	in, ok := f.answer(bottom(2, 2), Entry{Line: 28, T: TMull})
	if !ok || !reflect.DeepEqual(in.Choices, []int{0, 1}) {
		t.Fatalf("bottoming: intent %+v ok %v, want the whole hand", in, ok)
	}
	if f.mullBottomAll[0] {
		t.Error("the whole-hand marker was not consumed")
	}
	f = &follower{log: &Log{}}
	if in, _ := f.answer(keep, Entry{Line: 27, T: TMull, Mull: &Mull{T: TMull, Seat: 0, Keep: true, Hand: &[]int{}}}); !reflect.DeepEqual(in.Choices, []int{0}) {
		t.Fatalf("second keep: intent %+v", in)
	}
	if _, ok := f.answer(bottom(3, 2), Entry{Line: 28, T: TMull}); ok || f.res.Hard == nil || f.res.Hard.Field != "bottom" {
		t.Fatalf("a 3-card bottom from a 2-card hand: ok %v hard %+v, want answer-missing bottom", ok, f.res.Hard)
	}
}

// TestPriorityActivateMapsToAbilityOption is the farm cmdr4 repro (FDN-s131
// pods 13101 and 13103, Blast Zone and Evolving Wilds): Forge's seat
// activated a land's printed ability and the follower stopped with
// "unsupported priority action activate" (class answer-unmapped). gorge lists
// that as an "ability" option on the same card, which the follower now picks.
func TestPriorityActivateMapsToAbilityOption(t *testing.T) {
	const host, wilds = 301, state.ObjID(41)
	d := &decision.Decision{Seq: 9, Player: 3, Kind: decision.KPriority, Options: []decision.Option{
		{Index: 0, Kind: "pass"},
		{Index: 1, Kind: "ability", Obj: 7, Ability: 0},
		{Index: 2, Kind: "ability", Obj: wilds, Ability: 0},
		{Index: 3, Kind: "ability", Obj: wilds, Ability: 1},
	}}
	f := &follower{log: &Log{}, toG: map[int]state.ObjID{host: wilds, 302: 7}}
	// One ability option on the card: the answer is that option.
	one := *d
	one.Options = d.Options[:3]
	in, dv := f.priority(&one, parseAsk(t, `{"t":"ask","n":254,"seat":3,"turn":5,"step":"main1","kind":"priority","options":[{"action":"activate","card":301,"ability":9}],"answer":{"action":"activate","card":301,"ability":9}}`))
	if dv != nil || !reflect.DeepEqual(in.Choices, []int{2}) {
		t.Fatalf("single ability: intent %+v divergence %+v, want option 2", in, dv)
	}
	// Two: the ability's rank among Forge's activate options picks the option.
	in, dv = f.priority(d, parseAsk(t, `{"t":"ask","n":254,"seat":3,"turn":5,"step":"main1","kind":"priority","options":[{"action":"activate","card":301,"ability":20},{"action":"activate","card":301,"ability":21}],"answer":{"action":"activate","card":301,"ability":21}}`))
	if dv != nil || !reflect.DeepEqual(in.Choices, []int{3}) {
		t.Fatalf("two abilities: intent %+v divergence %+v, want option 3", in, dv)
	}
	// A card gorge offers no ability on is answer-missing, not a harness gap.
	in, dv = f.priority(&decision.Decision{Seq: 9, Player: 3, Kind: decision.KPriority, Options: d.Options[:1]},
		parseAsk(t, `{"t":"ask","n":254,"seat":3,"turn":5,"step":"main1","kind":"priority","answer":{"action":"activate","card":301,"ability":9}}`))
	if dv == nil || dv.What != WhatAnswerMissing || dv.Harness != "" {
		t.Fatalf("no ability offered: %+v, want answer-missing", dv)
	}
}

// TestChooseWithoutALogLineAnsweredByDefault is the farm cmdr4 repro (seeds
// 13100 and 13102): gorge asks a list-pick (KChoose) where Forge's next log
// line is the seat's priority, because Forge's recorder logs only priority,
// attack, block and target asks. The follower used to stop there with kind
// "choose seat 2" against "priority seat 2"; it now answers by default (the
// first Min options) and consumes no line.
func TestChooseWithoutALogLineAnsweredByDefault(t *testing.T) {
	prio := parseAsk(t, `{"t":"ask","n":200,"seat":2,"turn":5,"step":"main1","kind":"priority","answer":{"action":"pass"}}`)
	choose := func(min, max, n int) *decision.Decision {
		d := &decision.Decision{Seq: 7, Player: 2, Kind: decision.KChoose, Min: min, Max: max}
		for i := 0; i < n; i++ {
			d.Options = append(d.Options, decision.Option{Index: i, Kind: "discard"})
		}
		return d
	}
	for _, c := range []struct {
		name string
		d    *decision.Decision
		want []int
	}{
		{"exactly one", choose(1, 1, 3), []int{0}},
		{"discard down", choose(2, 2, 4), []int{0, 1}},
		{"optional picks none", choose(0, 2, 3), []int{}},
		{"clamped to the offer", choose(3, 3, 2), []int{0, 1}},
	} {
		f := &follower{log: &Log{}}
		in, ok := f.answer(c.d, Entry{Line: 292, T: TAsk, Ask: prio})
		if !ok || f.res.Hard != nil {
			t.Fatalf("%s: hard divergence %+v", c.name, f.res.Hard)
		}
		if !reflect.DeepEqual(in.Choices, c.want) || in.Seq != 7 || in.Player != 2 {
			t.Errorf("%s: intent %+v, want choices %v", c.name, in, c.want)
		}
		if f.pos != 0 || f.res.Chooses != 1 || f.res.Asks != 0 {
			t.Errorf("%s: pos %d chooses %d asks %d, want the log line left unconsumed", c.name, f.pos, f.res.Chooses, f.res.Asks)
		}
	}
}

// TestSnapshotAheadOfUnloggedDecisionWaits is the farm cmdr4 repro (EOE and
// OTJ pod 13002, HOB pod 12903): gorge waits on a cleanup discard (turn 2)
// while Forge, which logs no ask for it, has already answered and written the
// next snapshot (turn 3). The follower compared that snapshot against gorge's
// pre-discard state and stopped on "snapshot turn: gorge 2, forge 3". It must
// answer the unlogged decision by default first, and only then compare.
func TestSnapshotAheadOfUnloggedDecisionWaits(t *testing.T) {
	snap := func(turn int, step string) Entry {
		return Entry{Line: 97, T: TSnap, Snap: &Snap{T: TSnap, Snapshot: rules.OracleSnapshot{Turn: int32(turn), Step: step}}}
	}
	cleanup := at(2, "cleanup")
	for _, c := range []struct {
		name string
		kind decision.Kind
		e    Entry
		at   int
		want bool
	}{
		{"discard waits for the next turn's snapshot", decision.KChoose, snap(3, "upkeep"), cleanup, true},
		{"modes", decision.KModes, snap(3, "upkeep"), cleanup, true},
		{"arrange", decision.KArrange, snap(2, "main2"), at(2, "main1"), true},
		{"a snapshot of this very step is compared", decision.KChoose, snap(2, "cleanup"), cleanup, false},
		{"a logged decision still compares", decision.KPriority, snap(3, "upkeep"), cleanup, false},
		{"a mulligan still compares", decision.KMulligan, snap(3, "upkeep"), cleanup, false},
		{"not a snapshot", decision.KChoose, Entry{Line: 3, T: TAsk}, cleanup, false},
	} {
		if got := snapAheadOfUnlogged(c.e, c.at, &decision.Decision{Kind: c.kind}); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// TestFollowAnswersUnloggedChooseBeforeAheadSnapshot drives the real
// follower loop (not just snapAheadOfUnlogged): the golden log is doctored so
// seat 0 keeps its 7 and passes without playing a land on turn 2, so gorge
// asks a cleanup discard (KChoose, no log line) at turn 2's cleanup while the
// log's next entry is a snapshot of turn 3. The follower must answer the
// discard by default first (Chooses == 1) and not stop on the "turn" field
// of that snapshot; with the run-loop deferral removed it stops there.
func TestFollowAnswersUnloggedChooseBeforeAheadSnapshot(t *testing.T) {
	l := readGolden(t)
	var entries []Entry
	mulls := 0
	for _, e := range l.Entries {
		switch {
		case e.Snap != nil || e.End != nil:
			continue
		case e.Mull != nil && e.Mull.Seat == 0:
			mulls++
			if mulls == 1 { // the mulligan: keep the opening seven instead
				e.Mull.Keep = true
			} else {
				continue
			}
		case e.Shuffle != nil && e.Shuffle.Seat == 0 && len(entries) > 0 && mulls > 0:
			continue // the post-mulligan reshuffle
		case e.Ask != nil && e.Ask.Seat == 0 && e.Ask.Turn == 2 && e.Ask.Step == "main1" && e.Ask.Answer.Action == "land":
			continue // no land play: the pass that follows it is the seat's only main1 ask
		}
		if e.Ask != nil && e.Ask.Turn > 2 {
			continue
		}
		entries = append(entries, e)
	}
	last := entries[len(entries)-1].Line
	entries = append(entries, Entry{Line: last + 1, T: TSnap, Snap: &Snap{T: TSnap, Snapshot: rules.OracleSnapshot{Turn: 3, Step: "upkeep"}}})
	l.Entries = entries
	res := follow(t, l)
	if res.Chooses != 1 {
		t.Errorf("chooses %d, want the cleanup discard answered by default once (hard %+v)", res.Chooses, res.Hard)
	}
	if h := res.Hard; h != nil && (h.Field == "turn" || h.Line == last+1 && h.Gorge == "2") {
		t.Errorf("stopped comparing the turn-3 snapshot against gorge's pre-discard state: %+v", h)
	}
}

// TestTargetWithoutALogLineAnsweredByDefault is the farm cmdr4 repro (pod
// 13203, offset 448): gorge asks a target (KTarget) where Forge's next log line
// is the seat's priority ask, because Forge's recorder writes a target ask only
// where its targeting hook is asked. The follower used to stop with kind
// "target seat 2" against "priority seat 2"; it now answers by default (the
// first Min options) and consumes no line. A target ask in the log still maps.
func TestTargetWithoutALogLineAnsweredByDefault(t *testing.T) {
	prio := parseAsk(t, `{"t":"ask","n":349,"seat":2,"turn":5,"step":"main1","kind":"priority","answer":{"action":"pass"}}`)
	d := &decision.Decision{Seq: 9, Player: 2, Kind: decision.KTarget, Min: 1, Max: 1,
		Options: []decision.Option{{Index: 0, Player: 0}, {Index: 1, Obj: 6}}}
	f := &follower{log: &Log{}}
	in, ok := f.answer(d, Entry{Line: 448, T: TAsk, Ask: prio})
	if !ok || f.res.Hard != nil {
		t.Fatalf("hard divergence %+v", f.res.Hard)
	}
	if !reflect.DeepEqual(in.Choices, []int{0}) || in.Seq != 9 || in.Player != 2 {
		t.Errorf("intent %+v, want the first option", in)
	}
	if f.pos != 0 || f.res.Targets != 1 || f.res.Chooses != 0 || f.res.Asks != 0 {
		t.Errorf("pos %d targets %d chooses %d asks %d, want the log line left unconsumed", f.pos, f.res.Targets, f.res.Chooses, f.res.Asks)
	}

	// A logged target ask is still followed, not defaulted.
	ta := parseAsk(t, `{"t":"ask","n":350,"seat":2,"turn":5,"step":"main1","kind":"target","options":[{"player":0}],"answer":{"targets":[{"player":0}]}}`)
	f = &follower{log: &Log{Entries: []Entry{{Line: 449, T: TAsk, Ask: ta}}}}
	if in, ok = f.answer(d, f.log.Entries[0]); !ok || f.pos != 1 || f.res.Targets != 0 || !reflect.DeepEqual(in.Choices, []int{0}) {
		t.Errorf("logged target: ok %v pos %d targets %d intent %+v", ok, f.pos, f.res.Targets, in)
	}
}

// TestTargetFromTheCastAnswer is the farm cmdr4 repro (pod 14201, offset
// 277): seat 0 cast Abrade's "destroy target artifact" mode at seat 1's Arcane
// Signet, and Forge's recorder carried the target on the cast's priority
// answer ("targets":[{"card":138}]) with no target ask. The follower answered
// gorge's target decision by default -- the first offered artifact, seat 0's
// own Sol Ring -- so gorge put Sol Ring in seat 0's graveyard and the next
// snapshot diverged at p0.graveyard. The target now comes from the cast.
func TestTargetFromTheCastAnswer(t *testing.T) {
	cast := parseAsk(t, `{"t":"ask","n":208,"seat":0,"turn":6,"step":"main2","kind":"priority","answer":{"action":"cast","card":97,"tapped":[20,57],"targets":[{"card":138}],"target_slots":[1]}}`)
	next := parseAsk(t, `{"t":"ask","n":209,"seat":0,"turn":6,"step":"main2","kind":"priority","answer":{"action":"pass"}}`)
	const solRing, signet = state.ObjID(57), state.ObjID(138)
	newF := func() *follower {
		return &follower{log: &Log{}, toG: map[int]state.ObjID{57: solRing, 138: signet}}
	}
	d := &decision.Decision{Seq: 11, Player: 0, Kind: decision.KTarget, Min: 1, Max: 1,
		Options: []decision.Option{{Index: 0, Obj: solRing}, {Index: 1, Obj: signet}}}

	f := newF()
	f.rememberCastTargets(cast)
	in, ok := f.answer(d, Entry{Line: 269, T: TAsk, Ask: next})
	if !ok || f.res.Hard != nil {
		t.Fatalf("hard divergence %+v", f.res.Hard)
	}
	if !reflect.DeepEqual(in.Choices, []int{1}) {
		t.Errorf("choices %v, want [1]: Forge's Arcane Signet, not the first offered artifact", in.Choices)
	}
	if f.pos != 0 || f.res.CastTargets != 1 || f.res.Targets != 0 || len(f.castTargets) != 0 {
		t.Errorf("pos %d cast_targets %d targets %d left %v, want one cast target spent and no line consumed", f.pos, f.res.CastTargets, f.res.Targets, f.castTargets)
	}

	// Another seat's target decision does not take seat 0's cast targets.
	f = newF()
	f.rememberCastTargets(cast)
	d1 := *d
	d1.Player = 1
	if in, _ = f.answer(&d1, Entry{Line: 269, T: TAsk, Ask: next}); !reflect.DeepEqual(in.Choices, []int{0}) || f.res.Targets != 1 {
		t.Errorf("seat 1: choices %v targets %d, want the default", in.Choices, f.res.Targets)
	}

	// Several target slots asked one decision each take the refs slot by
	// slot: Explosion's any target (a creature) then its target player.
	expl := parseAsk(t, `{"t":"ask","n":1043,"seat":0,"turn":25,"step":"main1","kind":"priority","answer":{"action":"cast","card":39,"targets":[{"card":138},{"player":0}],"target_slots":[1,1]}}`)
	f = newF()
	f.rememberCastTargets(expl)
	anyT := &decision.Decision{Seq: 12, Player: 0, Kind: decision.KTarget, Min: 1, Max: 1,
		Options: []decision.Option{{Index: 0, Player: 0}, {Index: 1, Player: 1}, {Index: 2, Obj: signet}}}
	if in, _ = f.answer(anyT, Entry{Line: 1258, T: TAsk, Ask: next}); !reflect.DeepEqual(in.Choices, []int{2}) {
		t.Errorf("any target: choices %v, want [2]", in.Choices)
	}
	pl := &decision.Decision{Seq: 13, Player: 0, Kind: decision.KTarget, Min: 1, Max: 1,
		Options: []decision.Option{{Index: 0, Player: 1}, {Index: 1, Player: 0}}}
	if in, _ = f.answer(pl, Entry{Line: 1258, T: TAsk, Ask: next}); !reflect.DeepEqual(in.Choices, []int{1}) || f.res.CastTargets != 2 {
		t.Errorf("target player: choices %v cast_targets %d, want [1] and 2", in.Choices, f.res.CastTargets)
	}

	// Consuming the next log line spends what is left: a later trigger's
	// target is answered by default.
	f = newF()
	f.rememberCastTargets(cast)
	ta := parseAsk(t, `{"t":"ask","n":210,"seat":0,"turn":6,"step":"main2","kind":"target","options":[{"card":57}],"answer":{"targets":[{"card":57}]}}`)
	if in, ok = f.answer(d, Entry{Line: 270, T: TAsk, Ask: ta}); !ok || f.pos != 1 || len(f.castTargets) != 0 || !reflect.DeepEqual(in.Choices, []int{0}) {
		t.Errorf("logged target: ok %v pos %d left %v intent %+v, want the line followed and the cast targets cleared", ok, f.pos, f.castTargets, in)
	}
	if in, _ = f.answer(d, Entry{Line: 271, T: TAsk, Ask: next}); !reflect.DeepEqual(in.Choices, []int{0}) || f.res.Targets != 1 {
		t.Errorf("after a consumed line: choices %v targets %d, want the default", in.Choices, f.res.Targets)
	}
}

// mayShuffleFixture builds a follower on the golden log (seat 1 never mulligans, so it has one shuffle) with one extra
// seat-1 shuffle line appended, carrying the forge ids of seat 1's library
// as it stands after the opening deal, minus the last drop cards. It stops
// before any decision, with the engine past genesis.
func mayShuffleFixture(t *testing.T, drop int) *follower {
	t.Helper()
	probe := &follower{log: readGolden(t), reg: corpus(t), toG: map[int]state.ObjID{}, toF: map[state.ObjID]int{}, name: map[int]string{}, shuffles: map[int][]*Shuffle{}, planned: map[int]int{}, start: -1}
	if err := probe.setup(); err != nil {
		t.Fatal(err)
	}
	if err := probe.e.AdvanceHypothetical(); err != nil {
		t.Fatal(err)
	}
	lib := probe.e.G.Zone(state.ZLibrary, 1)
	if len(lib) <= drop {
		t.Fatalf("library %d, drop %d", len(lib), drop)
	}
	order := make([]int, 0, len(lib))
	for _, id := range lib[:len(lib)-drop] {
		order = append(order, probe.toF[id])
	}
	l := readGolden(t)
	l.Entries = append(l.Entries, Entry{T: TShuffle, Shuffle: &Shuffle{T: TShuffle, Seat: 1, Order: order}})
	f := &follower{log: l, reg: corpus(t), toG: map[int]state.ObjID{}, toF: map[state.ObjID]int{}, name: map[int]string{}, shuffles: map[int][]*Shuffle{}, planned: map[int]int{}, start: -1}
	if err := f.setup(); err != nil {
		t.Fatal(err)
	}
	if err := f.e.AdvanceHypothetical(); err != nil {
		t.Fatal(err)
	}
	// Seat 0's mulligan reshuffle is not yet planned (the mulligan is a
	// decision, not genesis), and its 9-card line would hold seat 0's smaller
	// library: only seat 1's appended line is under test.
	f.planned[0] = len(f.shuffles[0])
	return f
}

func mayShuffleDecision(f *follower) *decision.Decision {
	return &decision.Decision{Player: 0, Kind: decision.KChoose, Min: 1, Max: 1, ResumeKind: "arrange_mayshuffle",
		Options: []decision.Option{{Index: 0, Kind: "yes", Label: "Yes — shuffle"}, {Index: 1, Kind: "no", Label: "No — keep the order"}}}
}

// A "you may shuffle" election has no Forge ask line: Forge's shuffle line
// is the only trace of the answer. harness:chance-misaligned came from
// answering yes by default when Forge declined, so gorge's next planned
// shuffle took a later Forge shuffle of a smaller library ("Forge shuffled
// 90 cards, gorge 91"). The next shuffle line holding gorge's whole library
// is a yes, and the next one being a later, smaller shuffle is a no.
func TestMayShuffleElectionFollowsShuffleLines(t *testing.T) {
	for _, c := range []struct {
		name string
		drop int // cards the next Forge shuffle holds fewer than gorge's library
		want string
	}{
		{"taken", 0, "yes"},
		{"declined", 1, "no"},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := mayShuffleFixture(t, c.drop)
			d := mayShuffleDecision(f)
			in, ok := f.mayShuffleChoose(d)
			if !ok || len(in.Choices) != 1 || d.Options[in.Choices[0]].Kind != c.want {
				t.Fatalf("answered %+v ok %v, want %s", in, ok, c.want)
			}
		})
	}
	f := mayShuffleFixture(t, 0)
	d := mayShuffleDecision(f)
	d.ResumeKind = "cast_modes"
	if _, ok := f.mayShuffleChoose(d); ok {
		t.Error("a non-shuffle choose is answered by mayShuffleChoose")
	}
}

// TestOptionalTriggerWithoutALogLineAnsweredByDefault is the farm cmdr4 repro
// (TLA-s135 pod 13501, offset 233): gorge asks whether an optional trigger is
// put on the stack (KTriggerOptional) where Forge's next log line is the
// seat's priority ask, because Forge's recorder writes no ask for a "may"
// trigger. The follower used to stop with kind "trigger_optional seat 0"
// against "priority seat 0"; it now answers yes and consumes no line.
func TestOptionalTriggerWithoutALogLineAnsweredByDefault(t *testing.T) {
	prio := parseAsk(t, `{"t":"ask","n":178,"seat":0,"turn":3,"step":"main1","kind":"priority","answer":{"action":"pass"}}`)
	d := &decision.Decision{Seq: 4, Player: 0, Kind: decision.KTriggerOptional, Min: 1, Max: 1,
		Options: []decision.Option{{Index: 0, Kind: "yes"}, {Index: 1, Kind: "no"}}}
	f := &follower{log: &Log{}}
	in, ok := f.answer(d, Entry{Line: 233, T: TAsk, Ask: prio})
	if !ok || f.res.Hard != nil {
		t.Fatalf("hard divergence %+v", f.res.Hard)
	}
	if !reflect.DeepEqual(in.Choices, []int{0}) || in.Seq != 4 || in.Player != 0 {
		t.Errorf("intent %+v, want yes", in)
	}
	if f.pos != 0 || f.res.Optionals != 1 || f.res.Asks != 0 {
		t.Errorf("pos %d optionals %d asks %d, want the log line left unconsumed", f.pos, f.res.Optionals, f.res.Asks)
	}
	if !unloggedKind(decision.KTriggerOptional) {
		t.Error("trigger_optional is not an unlogged kind: a snapshot past it would compare gorge's pre-answer state")
	}
}

// TestTriggerOrderWithoutALogLineAnsweredByDefault is the farm cmdr4 repro
// (FRA-s27490217 pod 2749021700, offset 557): gorge asks a seat to order its
// simultaneously triggered abilities (KTriggerOrder, CR 603.3b) where Forge's
// next log line is the next seat's priority ask, because Forge's recorder
// writes no ask for a trigger order (its AI orders the stack itself). The
// follower used to stop with kind "trigger_order seat 2" against "priority
// seat 0"; it now answers the offered order and consumes no line.
func TestTriggerOrderWithoutALogLineAnsweredByDefault(t *testing.T) {
	prio := parseAsk(t, `{"t":"ask","n":442,"seat":0,"turn":5,"step":"main1","kind":"priority","answer":{"action":"pass"}}`)
	d := &decision.Decision{Seq: 9, Player: 2, Kind: decision.KTriggerOrder, Min: 3, Max: 3,
		Options: []decision.Option{{Index: 0, Kind: "trigger"}, {Index: 1, Kind: "trigger"}, {Index: 2, Kind: "trigger"}}}
	f := &follower{log: &Log{}}
	in, ok := f.answer(d, Entry{Line: 557, T: TAsk, Ask: prio})
	if !ok || f.res.Hard != nil {
		t.Fatalf("hard divergence %+v", f.res.Hard)
	}
	if !reflect.DeepEqual(in.Choices, []int{0, 1, 2}) || in.Seq != 9 || in.Player != 2 {
		t.Errorf("intent %+v, want the offered order", in)
	}
	if f.pos != 0 || f.res.Orders != 1 || f.res.Asks != 0 {
		t.Errorf("pos %d orders %d asks %d, want the log line left unconsumed", f.pos, f.res.Orders, f.res.Asks)
	}
	if !unloggedKind(decision.KTriggerOrder) {
		t.Error("trigger_order is not an unlogged kind: a snapshot past it would compare gorge's pre-answer state")
	}
}

// TestKindDivergenceCarriesPlacement: a same-kind seat mismatch (the farm
// cmdr4 pod-43324287302 "priority seat 1" against "priority seat 0") must say
// where Forge's ask and gorge each are, so the next farm run is diagnosable
// from divergence-<n>.json alone.
func TestKindDivergenceCarriesPlacement(t *testing.T) {
	l, line := doctor(t, func(a *Ask) bool { return a.Kind == KindPriority && a.Answer.Action == "pass" && a.Step != "untap" }, func(a *Ask) {
		a.Seat = 1 - a.Seat
	})
	h := follow(t, l).Hard
	if h == nil || h.What != WhatKind || h.Line != line {
		t.Fatalf("divergence %+v, want a kind divergence at line %d", h, line)
	}
	if !strings.Contains(h.Detail, "forge ask t") || !strings.Contains(h.Detail, "gorge t") || !strings.Contains(h.Detail, "stack ") {
		t.Errorf("detail %q does not place the divergence", h.Detail)
	}
	if got := FileOf(h, "g", "", "").Msg; !strings.Contains(got, h.Detail) {
		t.Errorf("msg %q drops the detail", got)
	}
}

// TestCombatSnapCarryingTheAttackPrecedesTheAsk is the farm pod-14800 repro
// (Vampire Hexmage, BIG/DFT/WOE-s148-cmdr4): Forge's recorder writes the
// declare-attackers snapshot after the attacking flag is set but before the
// tap, then the attack ask. gorge is still at its pending KAttackers, so
// comparing stopped on "attacking". Such a snapshot is skipped; a
// pre-declaration one, or one for another step, still compares.
func TestCombatSnapCarryingTheAttackPrecedesTheAsk(t *testing.T) {
	snap := func(step string, attacking, blocking bool) Entry {
		return Entry{Line: 446, T: TSnap, Snap: &Snap{T: TSnap, Snapshot: rules.OracleSnapshot{Turn: 10, Step: step,
			Permanents: []rules.OracleSnapPerm{{Attacking: attacking, Blocking: blocking}}}}}
	}
	ask := func(kind, step string) Entry {
		return Entry{Line: 447, T: TAsk, Ask: &Ask{Kind: kind, Turn: 10, Step: step}}
	}
	atk, blk := &decision.Decision{Kind: decision.KAttackers}, &decision.Decision{Kind: decision.KBlockers}
	for _, c := range []struct {
		name string
		e, n Entry
		at   int
		d    *decision.Decision
		want bool
	}{
		{"attack carried, attack ask next", snap("declare-attackers", true, false), ask(KindAttack, "declare-attackers"), at(10, "declare-attackers"), atk, true},
		{"block carried, block ask next", snap("declare-blockers", false, true), ask(KindBlock, "declare-blockers"), at(10, "declare-blockers"), blk, true},
		{"pre-declaration snapshot compares", snap("declare-attackers", false, false), ask(KindAttack, "declare-attackers"), at(10, "declare-attackers"), atk, false},
		{"next ask is a priority pass", snap("declare-attackers", true, false), ask(KindPriority, "declare-attackers"), at(10, "declare-attackers"), atk, false},
		{"gorge not at the declaration", snap("declare-attackers", true, false), ask(KindAttack, "declare-attackers"), at(10, "declare-attackers"), &decision.Decision{Kind: decision.KPriority}, false},
		{"another turn", snap("declare-attackers", true, false), ask(KindAttack, "declare-attackers"), at(9, "declare-attackers"), atk, false},
	} {
		if got := snapPrecedesOwnDeclaration(c.e, c.n, c.at, c.d); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// searchPickFixture is mayShuffleFixture with the appended seat-1 shuffle
// line holding seat 1's library minus the cards at the given indexes.
func searchPickFixture(t *testing.T, gone ...int) (*follower, []state.ObjID) {
	t.Helper()
	probe := &follower{log: readGolden(t), reg: corpus(t), toG: map[int]state.ObjID{}, toF: map[state.ObjID]int{}, name: map[int]string{}, shuffles: map[int][]*Shuffle{}, planned: map[int]int{}, start: -1}
	if err := probe.setup(); err != nil {
		t.Fatal(err)
	}
	if err := probe.e.AdvanceHypothetical(); err != nil {
		t.Fatal(err)
	}
	lib := slices.Clone(probe.e.G.Zone(state.ZLibrary, 1))
	order := make([]int, 0, len(lib))
	for i, id := range lib {
		if !slices.Contains(gone, i) {
			order = append(order, probe.toF[id])
		}
	}
	l := readGolden(t)
	l.Entries = append(l.Entries, Entry{T: TShuffle, Shuffle: &Shuffle{T: TShuffle, Seat: 1, Order: order}})
	f := &follower{log: l, reg: corpus(t), toG: map[int]state.ObjID{}, toF: map[state.ObjID]int{}, name: map[int]string{}, shuffles: map[int][]*Shuffle{}, planned: map[int]int{}, start: -1}
	if err := f.setup(); err != nil {
		t.Fatal(err)
	}
	if err := f.e.AdvanceHypothetical(); err != nil {
		t.Fatal(err)
	}
	f.planned[0] = len(f.shuffles[0])
	return f, f.e.G.Zone(state.ZLibrary, 1)
}

// TestSearchPickFromTheShuffleLine is the farm cmdr4 repro (BIG-s142 pod
// 14201, Terramorphic Expanse): a library search's "up to one" pick has no
// Forge ask line, and answering it by default (nothing, Min 0) left the land
// in gorge's library, so the search's shuffle met 90 cards against Forge's 89
// (harness:chance-misaligned). The pick is the offered card missing from the
// owner's next shuffle line; a shuffle that misses an unoffered card is a
// later one and leaves the default answer.
func TestSearchPickFromTheShuffleLine(t *testing.T) {
	prio := parseAsk(t, `{"t":"ask","n":231,"seat":1,"turn":5,"step":"main1","kind":"priority","answer":{"action":"pass"}}`)
	search := func(lib []state.ObjID, min, max int, idx ...int) *decision.Decision {
		d := &decision.Decision{Seq: 9, Player: 1, Kind: decision.KChoose, Min: min, Max: max, ResumeKind: "search"}
		for _, i := range idx {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "search", Obj: lib[i], Player: 1})
		}
		return d
	}
	for _, c := range []struct {
		name  string
		gone  []int
		min   int
		max   int
		offer []int
		want  []int
		picks int
	}{
		{"up to one finds the missing card", []int{1}, 0, 1, []int{0, 1}, []int{1}, 1},
		{"up to two finds both", []int{0, 1}, 0, 2, []int{0, 1}, []int{0, 1}, 1},
		{"found nothing", nil, 0, 1, []int{0, 1}, []int{}, 1},
		{"a later shuffle keeps the default", []int{1}, 0, 1, []int{0}, []int{}, 0},
		{"more missing than allowed keeps the default", []int{0, 1}, 0, 1, []int{0, 1}, []int{}, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			f, lib := searchPickFixture(t, c.gone...)
			d := search(lib, c.min, c.max, c.offer...)
			in, ok := f.answer(d, Entry{Line: 300, T: TAsk, Ask: prio})
			if !ok || f.res.Hard != nil {
				t.Fatalf("hard divergence %+v", f.res.Hard)
			}
			if !reflect.DeepEqual(in.Choices, c.want) || f.res.LibraryPicks != c.picks || f.res.Chooses != 1 || f.pos != 0 {
				t.Errorf("choices %v library picks %d chooses %d pos %d, want %v and %d", in.Choices, f.res.LibraryPicks, f.res.Chooses, f.pos, c.want, c.picks)
			}
		})
	}
}
