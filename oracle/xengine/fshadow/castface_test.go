package fshadow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
)

// prankster is an adventurer whose creature and Adventure (Free the Fae) both
// cost {1}{U}, so the same two Islands pay either face.
const prankster = "Picklock Prankster"

// recorded is a hand game casting card's face of gorge mode from two Islands,
// its cast line rewritten as Forge's recorder writes it: the casts of the card
// it listed, as JSON entries without their card ("ability":0, say), and the
// answer's own fields. The log is written and read back, so the follower sees
// only what a log's text holds.
func recorded(t *testing.T, card, mode, answer string, listed ...string) (*Log, int) {
	t.Helper()
	l := handGame(t, corpus(t), "", []string{"Island", "Island", card}, []handStep{
		{land: "Island"},
		{land: "Island"},
		{plan: card, mode: mode},
	})
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

// TestCastTakesTheFaceForgesAbilityNames: Forge lists an adventurer's creature
// as ability 0 and its Adventure with no ability, under one label and cost
// (farm cmdr4 FRA-s134 pod 13403, line 303). The follower casts the face the
// answer names, and the snapshots after it hold gorge to that board.
func TestCastTakesTheFaceForgesAbilityNames(t *testing.T) {
	for name, c := range map[string]struct{ mode, answer string }{
		"creature":  {"", `"ability":0`},
		"adventure": {"adventure_alt", ``},
	} {
		t.Run(name, func(t *testing.T) {
			l, _ := recorded(t, prankster, c.mode, c.answer, `,"ability":0`, ``)
			if res := followClean(t, l, 0); res.End != "log" {
				t.Errorf("end %q, want the whole log followed", res.End)
			}
		})
	}
}

// TestCastOfTheOnlyListedFrontFaceFollows: index 0 names gorge's front face
// though Forge listed no second cast of the card.
func TestCastOfTheOnlyListedFrontFaceFollows(t *testing.T) {
	l, _ := recorded(t, prankster, "", `"ability":0`, `,"ability":0`)
	followClean(t, l, 0)
}

// TestCastForgesAbilityDoesNotNameStops: lines whose abilities do not pick a
// face stop at the cast.
func TestCastForgesAbilityDoesNotNameStops(t *testing.T) {
	for name, c := range map[string]struct {
		answer string
		listed []string
	}{
		// A commander's or a may-play's casts carry none.
		"no index 0":     {``, []string{``, ``}},
		"two of index 0": {`"ability":0`, []string{`,"ability":0`, `,"ability":0`}},
		// A third cast Forge lists pairs with no way of gorge's two.
		"more listed than gorge's ways": {``, []string{`,"ability":0`, ``, `,"ability":1`}},
		"the front face alone listed":   {``, []string{`,"ability":0`}},
		// A cast under another mark is not one of the answer's candidates.
		"the other cast is marked": {``, []string{`,"ability":0`, `,"mode":"kicked"`}},
	} {
		t.Run(name, func(t *testing.T) {
			l, line := recorded(t, prankster, "", c.answer, c.listed...)
			unnamedAt(t, l, line, 0)
		})
	}
}

// TestAbilityWays: the rule over the other layouts Forge's logs hold, whose
// non-front casts gorge offers under their own modes.
func TestAbilityWays(t *testing.T) {
	cast := func(mode string) way { return way{kind: "cast", mode: mode} }
	front, free := cast(""), way{kind: "cast", alt: 1}
	for name, c := range map[string]struct {
		ways   []way
		answer string
		listed []string
		want   []way
	}{
		"split right half":      {[]way{cast("split_alt"), front}, `1`, []string{`0`, `1`}, []way{cast("split_alt")}},
		"split left half":       {[]way{cast("split_alt"), front}, `0`, []string{`0`, `1`}, []way{front}},
		"free alternative cost": {[]way{front, free}, ``, []string{`0`, ``}, []way{free}},
		"front over a grant":    {[]way{front, cast("blitz")}, `0`, []string{`0`, ``, `1`}, []way{front}},
		"two others stay": {[]way{front, cast("split_alt"), cast("fuse")}, `2`, []string{`0`, `1`, `2`},
			[]way{cast("split_alt"), cast("fuse")}},
		// gorge asks the variant during the front cast and has no second way.
		"one gorge way":      {[]way{front}, ``, []string{`0`, ``}, []way{front}},
		"one way, one cast":  {[]way{front}, ``, []string{`0`}, []way{front}},
		"no front way":       {[]way{cast("mayplay")}, `0`, []string{`0`}, []way{cast("mayplay")}},
		"evoke alone listed": {[]way{front, cast("evoke")}, `1`, []string{`1`}, []way{front, cast("evoke")}},
	} {
		t.Run(name, func(t *testing.T) {
			ability := func(s string) string {
				if s == "" {
					return ""
				}
				return `,"ability":` + s
			}
			opts := make([]string, len(c.listed))
			for i, s := range c.listed {
				opts[i] = `{"action":"cast","card":5` + ability(s) + `}`
			}
			// Another card's cast and the card's activation are not counted.
			opts = append(opts, `{"action":"cast","card":6,"ability":0}`, `{"action":"activate","card":5,"ability":0}`)
			line := fmt.Sprintf(`{"options":[%s],"answer":{"action":"cast","card":5%s}}`, strings.Join(opts, ","), ability(c.answer))
			var a Ask
			if err := json.Unmarshal([]byte(line), &a); err != nil {
				t.Fatal(err)
			}
			ws := slices.Clone(c.ways)
			if got := abilityWays(ws, &a); !slices.Equal(got, c.want) {
				t.Errorf("ways %+v, want %+v", got, c.want)
			}
			if !slices.Equal(ws, c.ways) {
				t.Errorf("the input ways became %+v", ws)
			}
		})
	}
}

// TestForgeAbilityZeroIsNotNone: an activation listed with no ability is not
// the one listed as index 0.
func TestForgeAbilityZeroIsNotNone(t *testing.T) {
	var a Ask
	line := `{"options":[{"action":"activate","card":5},{"action":"activate","card":5,"ability":0}],"answer":{"action":"activate","card":5,"ability":0}}`
	if err := json.Unmarshal([]byte(line), &a); err != nil {
		t.Fatal(err)
	}
	if got := slices.Index(forgeAbilityIDs(&a), a.Answer.abilityID()); got != 1 {
		t.Errorf("the answered ability ranks %d, want 1", got)
	}
}
