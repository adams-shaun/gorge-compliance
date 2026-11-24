package main

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

func TestPlayStartThornvault(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	out := t.TempDir()
	if err := playStart(reg, []string{"testdata/start"}, "BLB", out, false); err != nil {
		t.Fatal(err)
	}
	var ids []string
	activated := map[string]bool{}
	if err := readLines(filepath.Join(out, "items.jsonl"), func(b []byte) error {
		var it oraclegen.Item
		if err := json.Unmarshal(b, &it); err != nil {
			return err
		}
		ids = append(ids, it.ID)
		bf := it.Setup["p0"].Battlefield
		forests := 0
		for _, n := range bf {
			if n == "Forest" {
				forests++
			}
		}
		if !slices.Contains(bf, "Thornvault Forager") || forests != 4 {
			t.Errorf("%s: p0 battlefield %v, want Thornvault Forager and 4 Forest", it.ID, bf)
		}
		for _, s := range it.Steps {
			// not focus evidence: the explore bot activates Forager on its own in this
			// fixture; the focus is covered by oracle/xengine/game TestFocus*
			if s.Op == "activate" && strings.HasSuffix(s.Card, ":Thornvault Forager") {
				activated[it.ID] = true
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	want := []string{"xs:BLB:thornvault-forager:f0a2:s1", "xs:BLB:thornvault-forager:f0a2:s2"}
	if !slices.Equal(ids, want) {
		t.Fatalf("items %v, want %v", ids, want)
	}
	for _, id := range ids {
		if !activated[id] {
			t.Errorf("%s: no step activates Thornvault Forager", id)
		}
	}
	n := 0
	if err := readLines(filepath.Join(out, "lower.jsonl"), func(b []byte) error {
		var r lowerRow
		if err := json.Unmarshal(b, &r); err != nil {
			return err
		}
		n++
		if r.Status != "lower_ok" {
			t.Errorf("%s: status %s %v, want lower_ok", r.ID, r.Status, r.Reasons)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("lower.jsonl has %d rows, want 2", n)
	}
	xprepPlayDir(t, reg, out)
}

func TestPlayStartFlagsExclusive(t *testing.T) {
	for _, f := range []string{"-windows", "-random=BLB", "-pool=x", "-decks=x", "-dirac", "-tape-turns=1", "-format=commander", "-forge-only=x"} {
		err := runPlay([]string{"-set", "BLB", "-start", "testdata/start", "-out", t.TempDir(), f})
		if err == nil || !strings.Contains(err.Error(), "-start") {
			t.Errorf("-start with %s: err %v, want a -start error", f, err)
		}
	}
	if err := runPlay([]string{"-start", "testdata/start", "-out", t.TempDir()}); err == nil {
		t.Error("-start without -set: no error")
	}
}

// TestPlayStartBezaSetup plays the Beza start (scen's output since
// xp-setup-extra-hand-card: Path to Exile and Taste of Blood in hand) and
// holds the lowered setup to the start file: each seat's hand as listed and
// 40 cards in all, and the lowering agrees with gorge's own runner.
func TestPlayStartBezaSetup(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	out := t.TempDir()
	if err := playStart(reg, []string{"testdata/start-beza"}, "XP", out, false); err != nil {
		t.Fatal(err)
	}
	n := 0
	if err := readLines(filepath.Join(out, "items.jsonl"), func(b []byte) error {
		var it oraclegen.Item
		if err := json.Unmarshal(b, &it); err != nil {
			return err
		}
		n++
		p0, p1 := it.Setup["p0"], it.Setup["p1"]
		if want := []string{"Beza, the Bounding Spring", "Path to Exile"}; !slices.Equal(p0.Hand, want) {
			t.Errorf("%s: p0 hand %v, want %v", it.ID, p0.Hand, want)
		}
		if want := []string{"Taste of Blood"}; !slices.Equal(p1.Hand, want) {
			t.Errorf("%s: p1 hand %v, want %v", it.ID, p1.Hand, want)
		}
		if len(p0.Battlefield) != 5 || len(p0.LibraryTop) != 33 {
			t.Errorf("%s: p0 battlefield %d library %d, want 5 and 33", it.ID, len(p0.Battlefield), len(p0.LibraryTop))
		}
		if len(p1.Battlefield) != 0 || len(p1.LibraryTop) != 39 {
			t.Errorf("%s: p1 battlefield %v library %d, want none and 39", it.ID, p1.Battlefield, len(p1.LibraryTop))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("items.jsonl has %d rows, want 1", n)
	}
	if err := readLines(filepath.Join(out, "lower.jsonl"), func(b []byte) error {
		var r lowerRow
		if err := json.Unmarshal(b, &r); err != nil {
			return err
		}
		if r.Status != "lower_ok" {
			t.Errorf("%s: status %s %v, want lower_ok", r.ID, r.Status, r.Reasons)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// TestStartDeckRefusesFieldBlocked: a start file listing an instant, a
// sorcery or an Aura on the battlefield is refused (scen.FieldBlock), since
// gorge and Forge place it differently and the setup could never agree.
func TestStartDeckRefusesFieldBlocked(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, n := range []string{"Path to Exile", "Taste of Blood", "Strange Augmentation"} {
		_, _, err := startDeck(reg, "start-p0", startSeat{Battlefield: []string{"Plains", n}})
		if err == nil || !strings.Contains(err.Error(), "cannot be listed on the battlefield") {
			t.Errorf("%s on the battlefield: err %v, want a refusal", n, err)
		}
		if _, _, err := startDeck(reg, "start-p0", startSeat{Hand: []string{n}}); err != nil {
			t.Errorf("%s in hand: %v", n, err)
		}
	}
}
