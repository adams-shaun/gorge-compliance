package fshadow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

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
	const splitter = "Bonesplitter"
	library := []string{"Swamp", splitter, "Swamp", "Swamp", "Swamp"}
	steps := []handStep{{land: "Swamp"}, {plan: splitter}, {land: "Swamp"}}
	for _, c := range creatures {
		library = append(library, c, "Swamp")
		steps = append(steps, handStep{land: "Swamp"}, handStep{plan: c})
	}
	steps = append(steps, handStep{use: splitter, activate: true, taps: []string{"Swamp"}, target: creatures[len(creatures)-1]})
	l := handGame(t, corpus(t), "", library, steps)
	a, _ := lastAsk(t, l, "activate")
	a.Answer.Targets, a.Answer.TargetSlots = nil, []int{0}
	return l
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

// TestEquipWithTwoBearersDiverges: the line does not say which was equipped.
func TestEquipWithTwoBearersDiverges(t *testing.T) {
	res := follow(t, equipGame(t, "Walking Corpse", "Typhoid Rats"))
	if h := res.Hard; h == nil || h.What != WhatAnswerMissing || h.Field != "cast_targets" {
		t.Fatalf("divergence %+v, want answer-missing cast_targets", res.Hard)
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
