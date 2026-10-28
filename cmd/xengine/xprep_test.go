package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/xprep"
)

// TestXprepWindowOnly: without -tapes a tape item is refused with
// xmage_window_only before Prepare sees it (it lands in xprep.jsonl and not in
// xitems); with -tapes it is not refused for that reason.
func TestXprepWindowOnly(t *testing.T) {
	reg, played := playTwo(t)
	sp, err := decks.LoadSpellings(manifestDir)
	if err != nil {
		t.Fatal(err)
	}
	// Two items of the played games: the first becomes a window, the second
	// stays the tape it is.
	var ids []string
	if err := readLines(filepath.Join(played, "items.jsonl"), func(b []byte) error {
		var it struct{ ID string }
		if err := json.Unmarshal(b, &it); err != nil {
			return err
		}
		ids = append(ids, it.ID)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var tape string
	for _, id := range ids {
		if strings.HasSuffix(id, ":tape") {
			tape = id
			break
		}
	}
	if tape == "" {
		t.Fatalf("play wrote no tape item among %v", ids)
	}
	window := tape[:strings.Index(tape, ":tape")] + ":w1"
	dir := t.TempDir()
	for _, name := range []string{"items.jsonl", "gorge.jsonl", "lower.jsonl"} {
		raw, err := os.ReadFile(filepath.Join(played, name))
		if err != nil {
			t.Fatal(err)
		}
		var keep [][]byte
		for _, line := range bytes.Split(raw, []byte("\n")) {
			if bytes.Contains(line, []byte(`"`+tape+`"`)) {
				keep = append(keep, line, bytes.ReplaceAll(line, []byte(`"`+tape+`"`), []byte(`"`+window+`"`)))
			}
		}
		if len(keep) != 2 {
			t.Fatalf("%s: %d lines for %s, want 1", name, len(keep)/2, tape)
		}
		if err := os.WriteFile(filepath.Join(dir, name), append(bytes.Join(keep, []byte("\n")), '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run := func(tapes bool) (xitems, refused []byte) {
		out := filepath.Join(t.TempDir(), "xitems.jsonl")
		if err := xprepPlay(reg, sp, dir, out, tapes); err != nil {
			t.Fatal(err)
		}
		xitems, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		refused, err = os.ReadFile(filepath.Join(filepath.Dir(out), xprepRefusalsFile))
		if err != nil {
			t.Fatal(err)
		}
		return xitems, refused
	}
	lines := func(b []byte) int { return bytes.Count(b, []byte("\n")) }

	xitems, refused := run(false)
	if lines(xitems) != 1 || lines(refused) != 1 {
		t.Fatalf("window-only: %d xitems and %d refusals, want 1 and 1\nxitems: %s\nrefused: %s", lines(xitems), lines(refused), xitems, refused)
	}
	var r xprep.Refusal
	if err := json.Unmarshal(bytes.TrimSpace(refused), &r); err != nil {
		t.Fatal(err)
	}
	if r.ID != tape || r.Reason != xprep.ReasonWindowOnly {
		t.Errorf("refusal %+v, want %s refused as %s", r, tape, xprep.ReasonWindowOnly)
	}
	if !bytes.Contains(xitems, []byte(`"`+window+`"`)) {
		t.Errorf("the window item is not in xitems: %s", xitems)
	}

	_, refused = run(true)
	if bytes.Contains(refused, []byte(xprep.ReasonWindowOnly)) {
		t.Errorf("-tapes still refuses a tape as window-only: %s", refused)
	}
}
