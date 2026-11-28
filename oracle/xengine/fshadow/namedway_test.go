package fshadow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclediff"
	"github.com/adams-shaun/gorge/engine/decision"
	"github.com/adams-shaun/gorge/engine/state"
)

// asRecorded rewrites l's last cast line as Forge's recorder writes it: the
// casts of the card it listed, as JSON entries without their card
// ("ability":0, say), and the answer's own fields. The log is written and read
// back, so the follower sees only what a log's text holds. It returns that log
// and the cast's line.
func asRecorded(t *testing.T, l *Log, answer string, listed ...string) (*Log, int) {
	t.Helper()
	a, line := lastAsk(t, l, "cast")
	opts := make([]string, len(listed))
	for i, fields := range listed {
		opts[i] = fmt.Sprintf(`{"action":"cast","card":%d%s}`, a.Answer.Card, fields)
	}
	if err := json.Unmarshal([]byte("["+strings.Join(opts, ",")+"]"), &a.Options); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte("{"+answer+"}"), &a.Answer); err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := Write(&b, l); err != nil {
		t.Fatal(err)
	}
	read, err := Read(&b)
	if err != nil {
		t.Fatal(err)
	}
	return read, line
}

// bestowGame bestows Nighthowler on Walking Corpse tapping four Swamps, which
// also pay its creature cast (farm cmdr4 MSH-s141 pod 14102, line 567). gorge's
// planner plans no bestow, so it offers the cast only once the mana floats.
func bestowGame(t *testing.T) *Log {
	const howler, corpse = "Nighthowler", "Walking Corpse"
	return handGame(t, corpus(t), "", []string{"Swamp", "Swamp", corpse, "Swamp", "Swamp", howler}, []handStep{
		{land: "Swamp"},
		{land: "Swamp"},
		{plan: corpse},
		{land: "Swamp"},
		{land: "Swamp"},
		{use: howler, mode: "bestowed", target: corpse, taps: []string{"Swamp", "Swamp", "Swamp", "Swamp"}},
	})
}

// TestNamedWayIsPaidByHand: a cast whose ability index leaves one of gorge's
// ways is paid by hand in that way, one gorge's planner plans or not. The
// snapshots after the cast hold gorge to the board the way leaves.
func TestNamedWayIsPaidByHand(t *testing.T) {
	// two is a card Forge lists two casts of: its front face's, index 0, and
	// the other one.
	two := []string{`,"ability":0`, `,"ability":1`}
	const second = `"ability":1`
	game := func(library []string, steps ...handStep) func(*testing.T) *Log {
		return func(t *testing.T) *Log { return handGame(t, corpus(t), "", library, steps) }
	}
	// lands plays n lands called name, then casts use in mode tapping them.
	lands := func(name string, n int, use, mode, target string) func(*testing.T) *Log {
		library := append(slices.Repeat([]string{name}, n), use)
		var steps []handStep
		for range n {
			steps = append(steps, handStep{land: name})
		}
		cast := handStep{use: use, mode: mode, target: target, taps: slices.Repeat([]string{name}, n)}
		return game(library, append(steps, cast)...)
	}
	for _, c := range []struct {
		name   string
		game   func(*testing.T) *Log
		answer string
		listed []string
	}{
		{"bestow", bestowGame, second, two},
		{"evoke", lands("Island", 3, "Mulldrifter", "evoked", ""), second, two},
		{"split right half", lands("Island", 2, "Fire", "split_alt", "Island"), second, two},
		{"face down", lands("Island", 3, "Willbender", "morphed", ""), ``, []string{`,"ability":0`, ``}},
		{"modal back face", lands("Mountain", 5, "Birgi, God of Storytelling", "modal_spell", ""), ``, []string{`,"ability":0`, ``}},
		// The front face of a card with a second way: a Signet pays it.
		{"front face", game([]string{"Mountain", "Wastes", "Sol Ring", "Boros Signet", "Bonecrusher Giant"},
			handStep{land: "Mountain"},
			handStep{plan: "Sol Ring"},
			handStep{land: "Wastes"},
			handStep{plan: "Boros Signet"},
			handStep{use: "Bonecrusher Giant", taps: []string{"Mountain", "Wastes", "Boros Signet"}},
		), `"ability":0`, []string{`,"ability":0`, ``}},
	} {
		t.Run(c.name, func(t *testing.T) {
			l, _ := asRecorded(t, c.game(t), c.answer, c.listed...)
			if res := followClean(t, l, 1); res.End != "log" {
				t.Errorf("end %q, want the whole log followed", res.End)
			}
		})
	}
}

// TestHandPaymentOfAnUnnamedWayDiverges: ability indexes that leave gorge two
// ways, or pick none, pay nothing by hand.
func TestHandPaymentOfAnUnnamedWayDiverges(t *testing.T) {
	for name, c := range map[string]struct {
		answer string
		listed []string
	}{
		"no index 0":                    {``, []string{``, ``}},
		"two of index 0":                {`"ability":0`, []string{`,"ability":0`, `,"ability":0`}},
		"more listed than gorge's ways": {`"ability":1`, []string{`,"ability":0`, `,"ability":1`, `,"ability":2`}},
	} {
		t.Run(name, func(t *testing.T) {
			l, line := asRecorded(t, bestowGame(t), c.answer, c.listed...)
			stopsAt(t, l, line, 0)
		})
	}
}

// equipGame equips Bonesplitter on the last of creatures, its activation line
// rewritten as Forge's recorder writes an Equip's: no target.
func equipGame(t *testing.T, creatures ...string) *Log {
	return equips(t, creatures, handStep{target: creatures[len(creatures)-1]})
}

// equips plays creatures and then thens: an equip of Bonesplitter on a step's
// target, or the step's land. Every Equip line is rewritten as Forge's
// recorder writes it, with one empty targeting part and no target.
func equips(t *testing.T, creatures []string, thens ...handStep) *Log {
	const splitter = "Bonesplitter"
	library := []string{"Swamp", splitter, "Swamp", "Swamp", "Swamp"}
	steps := []handStep{{land: "Swamp"}, {plan: splitter}, {land: "Swamp"}}
	for _, c := range creatures {
		library = append(library, c, "Swamp")
		steps = append(steps, handStep{land: "Swamp"}, handStep{plan: c})
	}
	for _, s := range thens {
		if s.land != "" {
			library = append(library, s.land)
		} else {
			s = handStep{use: splitter, activate: true, taps: []string{"Swamp"}, target: s.target}
		}
		steps = append(steps, s)
	}
	l := handGame(t, corpus(t), "", library, steps)
	for _, e := range l.Entries {
		if a := e.Ask; a != nil && a.Answer.Action == "activate" {
			a.Answer.Targets, a.Answer.TargetSlots = nil, []int{0}
		}
	}
	return l
}

// reattach rewrites what Bonesplitter is attached to in every snapshot of l
// after line.
func reattach(l *Log, line int, to func(string) string) {
	for _, e := range l.Entries {
		if e.Snap == nil || e.Line <= line {
			continue
		}
		for i := range e.Snap.Snapshot.Permanents {
			if p := &e.Snap.Snapshot.Permanents[i]; p.Name == "Bonesplitter" && p.AttachedTo != "" {
				p.AttachedTo = to(p.AttachedTo)
			}
		}
	}
}

// TestEquipWithOneBearerFollows: the recorder writes no target on an Equip
// activation (farm cmdr4 MSH-s141 pod 14102, line 573); with one creature to
// equip there is no choice to record.
func TestEquipWithOneBearerFollows(t *testing.T) {
	res := follow(t, equipGame(t, "Walking Corpse"))
	if h := res.Hard; h != nil {
		t.Fatalf("hard divergence at line %d: %s %s %s gorge %q forge %q (%s)", h.Line, h.What, h.Kind, h.Field, h.Gorge, h.Forge, h.Detail)
	}
	if res.End != "log" || res.CastTargets != 1 {
		t.Errorf("end %q cast_targets %d, want the whole log followed and the Equip's one target", res.End, res.CastTargets)
	}
}

// TestEquipWithTwoBearersFollows: the line does not say which was equipped
// (farm cmdr4 MSH-s141 pod 14102, line 572); the next snapshot's attached_to
// does. The bearer is not gorge's first option, the default answer.
func TestEquipWithTwoBearersFollows(t *testing.T) {
	two := []string{"Walking Corpse", "Typhoid Rats"}
	for _, c := range []struct {
		name  string
		slots []int
		bare  bool // the snapshot names the bearer, not its ref
		moved bool // the Equipment is on the other creature first
	}{
		{name: "one empty part", slots: []int{0}},
		{name: "no part", slots: []int{}},
		{name: "the host named bare", slots: []int{0}, bare: true},
		{name: "moved from the other bearer", slots: []int{0}, moved: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			thens, picks := []handStep{{target: two[1]}}, 1
			if c.moved {
				thens, picks = []handStep{{target: two[0]}, {land: "Swamp"}, {target: two[1]}}, 2
			}
			l := equips(t, two, thens...)
			for _, e := range l.Entries {
				if a := e.Ask; a != nil && a.Answer.Action == "activate" {
					a.Answer.TargetSlots = c.slots
				}
			}
			if c.bare {
				reattach(l, 0, oraclediff.RefName)
			}
			res := follow(t, l)
			if h := res.Hard; h != nil {
				t.Fatalf("hard divergence at line %d: %s %s %s gorge %q forge %q (%s)", h.Line, h.What, h.Kind, h.Field, h.Gorge, h.Forge, h.Detail)
			}
			if res.End != "log" || res.CastTargets != picks || res.BearerPicks != picks {
				t.Errorf("end %q cast_targets %d bearer_picks %d, want the whole log followed and %d bearers read", res.End, res.CastTargets, res.BearerPicks, picks)
			}
		})
	}
}

// TestEquipBearerNotShownDiverges: where the log does not tell which of two
// creatures was equipped, the follow stops at the Equip's target ask.
func TestEquipBearerNotShownDiverges(t *testing.T) {
	two := []string{"Walking Corpse", "Typhoid Rats"}
	onRats := []handStep{{target: two[1]}}
	// edit changes l after its first Equip, on line.
	shows := func(host string) func(*testing.T, *Log, int) {
		return func(_ *testing.T, l *Log, line int) { reattach(l, line, func(string) string { return host }) }
	}
	for _, c := range []struct {
		name      string
		creatures []string
		thens     []handStep
		edit      func(*testing.T, *Log, int)
		detail    string
	}{
		{"two bearers of one name", []string{two[0], two[0]}, []handStep{{target: two[0]}}, nil, "gorge offers 2"},
		{"attached to nothing", two, onRats, shows(""), "0 new bearers"},
		{"a bearer gorge does not offer", two, onRats, shows("p0:Grizzly Bears"), "gorge offers 0"},
		{"equipped again first", two, []handStep{{target: two[0]}, {target: two[1]}}, nil, "may move it again"},
		{"targeted first", two, onRats, func(t *testing.T, l *Log, line int) {
			a, _ := lastAsk(t, l, "activate")
			i := slices.IndexFunc(l.Entries, func(e Entry) bool { return e.Line > line && e.Ask != nil })
			l.Entries[i].Ask.Answer.Targets = []Ref{{Card: a.Answer.Card}}
		}, "may move it again"},
		{"no later snapshot", two, onRats, func(_ *testing.T, l *Log, line int) {
			l.Entries = slices.DeleteFunc(l.Entries, func(e Entry) bool { return e.Line > line && e.Snap != nil })
		}, "no later snapshot"},
	} {
		t.Run(c.name, func(t *testing.T) {
			l := equips(t, c.creatures, c.thens...)
			i := slices.IndexFunc(l.Entries, func(e Entry) bool { return e.Ask != nil && e.Ask.Answer.Action == "activate" })
			line := l.Entries[i].Line
			if c.edit != nil {
				c.edit(t, l, line)
			}
			for _, slots := range [][]int{{0}, {}} {
				l.Entries[i].Ask.Answer.TargetSlots = slots
				res := follow(t, l)
				// The ask has no line of its own: the next one is reported.
				if h := res.Hard; h == nil || h.What != WhatAnswerMissing || h.Field != "cast_targets" ||
					h.Line != l.Entries[i+1].Line || !strings.Contains(h.Detail, c.detail) {
					t.Errorf("target_slots %v: divergence %+v, want answer-missing cast_targets (%s) after line %d", slots, res.Hard, c.detail, line)
				}
			}
		})
	}
}

// TestUnrecordedTargets: only an activation's ask that must take every option,
// with no target recorded in any part left, is answered without one.
func TestUnrecordedTargets(t *testing.T) {
	const source, a, b = state.ObjID(97), state.ObjID(57), state.ObjID(138)
	next := Entry{Line: 269, T: TAsk, Ask: parseAsk(t, `{"t":"ask","n":209,"seat":0,"kind":"priority","answer":{"action":"pass"}}`)}
	one := []decision.Option{{Index: 0, Obj: a}}
	both := []decision.Option{{Index: 0, Obj: a}, {Index: 1, Obj: b}}
	const empty = `"targets":[],"target_slots":[0]`
	for _, c := range []struct {
		name, action, targets string
		min, max              int
		opts                  []decision.Option
		want                  []int // nil for a divergence
		left                  int   // parts still unspent
	}{
		{"one option", "activate", empty, 1, 1, one, []int{0}, 0},
		{"every option taken", "activate", empty, 2, 2, both, []int{0, 1}, 0},
		{"one part spent per ask", "activate", `"targets":[],"target_slots":[0,0]`, 1, 1, one, []int{0}, 1},
		{"a choice of two", "activate", empty, 1, 1, both, nil, 0},
		{"a cast's targets are recorded", "cast", empty, 1, 1, one, nil, 0},
		{"a later part holds a target", "activate", `"targets":[{"card":138}],"target_slots":[0,1]`, 1, 1, one, nil, 0},
		{"an optional ask takes nothing", "activate", empty, 0, 1, one, []int{}, 0},
	} {
		f := &follower{log: &Log{}, toG: map[int]state.ObjID{97: source, 57: a, 138: b}}
		f.rememberCastTargets(parseAsk(t, `{"t":"ask","n":208,"seat":0,"kind":"priority","answer":{"action":"`+c.action+`","card":97,`+c.targets+`}}`))
		d := &decision.Decision{Seq: 11, Player: 0, Kind: decision.KTarget, Source: source, Min: c.min, Max: c.max, Options: c.opts}
		in, ok := f.answer(d, next)
		if c.want == nil {
			if h := f.res.Hard; ok || h == nil || h.What != WhatAnswerMissing || h.Field != "cast_targets" {
				t.Errorf("%s: ok %v choices %v hard %+v, want a cast_targets finding", c.name, ok, in.Choices, h)
			}
			continue
		}
		if !ok || f.res.Hard != nil || !reflect.DeepEqual(in.Choices, c.want) || len(f.chain.parts) != c.left {
			t.Errorf("%s: ok %v hard %+v choices %v left %v, want %v and %d parts", c.name, ok, f.res.Hard, in.Choices, f.chain.parts, c.want, c.left)
		}
	}
}
