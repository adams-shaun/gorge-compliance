package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// podDir writes the four Commander deck files of a pod (the three fixtures and
// a copy of the first) to a directory.
func podDir(t *testing.T, stems ...string) string {
	t.Helper()
	src := t.TempDir()
	for i, s := range stems {
		f, err := testutil.LoadRepoDeckFile(s)
		if err != nil {
			t.Fatal(err)
		}
		b, err := json.Marshal(f)
		if err != nil {
			t.Fatal(err)
		}
		name := s
		if i >= 3 {
			name = "copy-" + s
		}
		if err := os.WriteFile(filepath.Join(src, name+".json"), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return src
}

func TestXenginePlayFourSeats(t *testing.T) {
	cmdr := []string{"foundations-tramplesaurus-rex", "foundations-wretched-ranks", "foundations-reign-of-dragons"}
	src := podDir(t, append(cmdr, cmdr[0])...)
	out := t.TempDir()
	if err := runPlay([]string{"-cards", "../../.cards", "-set", "CMDR", "-chunk", "0", "-games", "1", "-seed", "1",
		"-windows", "-tape-turns", "1,2", "-format", "commander", "-commander-decks", src, "-seats", "4", "-out", out}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(out, "lower.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, l := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var r lowerRow
		if err := json.Unmarshal([]byte(l), &r); err != nil {
			t.Fatal(err)
		}
		if r.Status != statusRefused || !reflect.DeepEqual(r.Reasons, []string{"seats:4"}) {
			t.Errorf("row %s: status %q reasons %v, want refused seats:4", r.ID, r.Status, r.Reasons)
		}
		ids[r.ID] = true
	}
	for _, id := range []string{"xe:CMDR:0:0:tape", "xe:CMDR:0:0:tape:t1", "xe:CMDR:0:0:tape:t2"} {
		if !ids[id] {
			t.Errorf("no refused row for %s (rows %v)", id, ids)
		}
	}
	if len(ids) < 4 {
		t.Errorf("no refused window rows: %v", ids)
	}
	if b, err := os.ReadFile(filepath.Join(out, "items.jsonl")); err != nil || len(b) != 0 {
		t.Errorf("items.jsonl: %d bytes, err %v; a pod lowers to nothing", len(b), err)
	}
	tapes, _ := filepath.Glob(filepath.Join(out, "tapes", "*.json"))
	if len(tapes) != 0 {
		t.Errorf("refused games wrote tapes %v", tapes)
	}
}

func TestPlaySeatsFlag(t *testing.T) {
	two := podDir(t, "foundations-tramplesaurus-rex", "foundations-wretched-ranks")
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"-seats", "5", "-format", "commander", "-commander-decks", two}, "-seats between 2 and 4"},
		{[]string{"-seats", "1", "-format", "commander", "-commander-decks", two}, "-seats between 2 and 4"},
		{[]string{"-seats", "3"}, "needs -format commander"},
		{[]string{"-seats", "3", "-format", "commander", "-commander-decks", two}, "need 3"},
	} {
		args := append([]string{"-cards", "../../.cards", "-set", "CMDR", "-out", t.TempDir()}, c.args...)
		if err := runPlay(args); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v: err = %v, want %q", c.args, err, c.want)
		}
	}
}

func TestCommanderPodDeal(t *testing.T) {
	var ds []decks.Deck
	for _, n := range []string{"a", "b", "c", "d", "e"} {
		ds = append(ds, decks.Deck{Name: n, Cards: []string{n}, Commander: n})
	}
	for g := 0; g < 20; g++ {
		two, _ := commanderDealer(ds, 2, "S", 0, 1, false)(g)
		if want := decks.CommanderPair(ds, "S", 0, g, 1); !reflect.DeepEqual(two, want[:]) {
			t.Fatalf("game %d: 2-seat deal %v differs from CommanderPair %v", g, two, want)
		}
		for n := 3; n <= 4; n++ {
			pod, _ := commanderDealer(ds, n, "S", 0, 1, false)(g)
			if len(pod) != n {
				t.Fatalf("game %d: %d decks, want %d", g, len(pod), n)
			}
			seen := map[string]bool{}
			for seat, d := range pod {
				stem := d.Cards[0]
				if seen[stem] || !strings.HasPrefix(d.Name, fmt.Sprintf("S-c0-g%d-%d-", g, seat)) {
					t.Fatalf("game %d: seat %d deck %v repeats or is misnamed (%v)", g, seat, d.Name, pod)
				}
				seen[stem] = true
			}
		}
	}
	a, _ := commanderDealer(ds, 4, "S", 0, 1, false)(7)
	b, _ := commanderDealer(ds, 4, "S", 0, 1, false)(7)
	if !reflect.DeepEqual(a, b) {
		t.Error("the deal is not a pure function of its arguments")
	}
}
