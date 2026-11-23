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
