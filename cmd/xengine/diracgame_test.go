package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/dside"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

func TestDiracGameEvery(t *testing.T) {
	cases := []struct {
		every int
		want  string
	}{{1, "111111"}, {2, "010101"}, {3, "001001"}}
	for _, c := range cases {
		got := ""
		for g := 0; g < 6; g++ {
			if diracGame(g, c.every) {
				got += "1"
			} else {
				got += "0"
			}
		}
		if got != c.want {
			t.Errorf("every %d: games %s, want %s", c.every, got, c.want)
		}
	}
}

// TestPlayDiracSwapsRecorded: a set game whose dealt pair dirac's OTJ table refuses as
// an interaction (seed 1, chunk 0, game 1: Archangel of Tithes beside Bucolic Ranch
// and Magda) is played swapped, swaps.jsonl names each swap, and dirac's row for the
// game is not not_applicable.
func TestPlayDiracSwapsRecorded(t *testing.T) {
	testutil.CorpusRegistry(t) // skips without the corpus
	out := t.TempDir()
	err := runPlay([]string{"-cards", "../../.cards", "-manifests", manifestDir, "-set", "OTJ", "-chunk", "0",
		"-games", "2", "-seed", "1", "-dirac", "-out", out})
	if dside.NoTables(err) {
		t.Skip(err) // CI has the corpus but not the dirac synthetic tables
	}
	if err != nil {
		t.Fatal(err)
	}
	var swaps []swapRow
	if err := readLines(filepath.Join(out, "swaps.jsonl"), func(b []byte) error {
		var r swapRow
		swaps = append(swaps, r)
		return json.Unmarshal(b, &swaps[len(swaps)-1])
	}); err != nil {
		t.Fatal(err)
	}
	if len(swaps) == 0 {
		t.Skip("no OTJ pair of the first two games is refused at this pin")
	}
	swapped := map[string]bool{}
	for _, s := range swaps {
		swapped[s.Game] = true
		if s.Card == "" || s.Basic == "" || !strings.HasPrefix(s.Reason, "interaction ") || s.Count < 1 {
			t.Errorf("swap row %+v", s)
		}
	}
	rows := 0
	if err := readLines(filepath.Join(out, "dirac.jsonl"), func(b []byte) error {
		var r dside.Row
		if err := json.Unmarshal(b, &r); err != nil {
			return err
		}
		rows++
		if swapped[r.Game] && r.Status == dside.StatusNA {
			t.Errorf("swapped game %s is still not_applicable: %s", r.Game, r.Reason)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if rows != 2 {
		t.Errorf("dirac.jsonl has %d rows, want 2", rows)
	}
}
