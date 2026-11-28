package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/classify"
)

// legRowsOf runs keyproof on fx into out (classes "" or a register) and returns
// keyproof-legs.tsv's rows by key and leg, after checking the row rules every
// leg row must pass and that every keyproof.tsv key has one xmage and one
// forge row.
func (fx *kpFixture) legRowsOf(t *testing.T, out, classes string) map[classify.Use]map[string]map[string]string {
	t.Helper()
	if err := keyproof(fx.reg, fx.args(out, classes)); err != nil {
		t.Fatal(err)
	}
	_, rows := readTSV(t, filepath.Join(out, "keyproof-legs.tsv"))
	byKey := map[classify.Use]map[string]map[string]string{}
	for _, r := range rows {
		u := classify.Use{Card: r["card"], Key: r["key"]}
		if byKey[u] == nil {
			byKey[u] = map[string]map[string]string{}
		}
		if _, dup := byKey[u][r["leg"]]; dup {
			t.Errorf("%v: two %s rows", u, r["leg"])
		}
		byKey[u][r["leg"]] = r
		if !slices.Contains([]string{"xmage", "forge", "phase", "cite", "seed"}, r["leg"]) {
			t.Errorf("%v: leg %q", u, r["leg"])
		}
		if !slices.Contains([]string{"agreed", "diagnosed", "diverged", "owed", "stale", "excluded"}, r["state"]) {
			t.Errorf("%v %s: state %q", u, r["leg"], r["state"])
		}
		if r["state"] == "diagnosed" && (r["cite"] == "" || r["cite"] == "-") {
			t.Errorf("%v %s: diagnosed without a cite: %v", u, r["leg"], r)
		}
		if r["leg"] == "cite" && !slices.Contains([]string{"diagnosed", "owed", "excluded"}, r["state"]) {
			t.Errorf("%v: cite row in state %s", u, r["state"])
		}
		if !slices.Contains([]string{"2", "3", "4"}, r["seats"]) {
			t.Errorf("%v %s: seats %q", u, r["leg"], r["seats"])
		}
		if r["seats"] != "2" {
			t.Errorf("%v %s: seats %s, want 2 in a two-seat fixture", u, r["leg"], r["seats"])
		}
		if r["oracle_id"] != "-" || r["writer"] != "xproof" || r["multiplayer"] != "false" || r["replayed"] != "-" || r["measured"] != "2026-11-23" {
			t.Errorf("%v %s: stamp cells %v", u, r["leg"], r)
		}
	}
	_, legacy := readTSV(t, filepath.Join(out, "keyproof.tsv"))
	if len(legacy) != len(byKey) {
		t.Errorf("keyproof-legs.tsv has %d keys, keyproof.tsv %d", len(byKey), len(legacy))
	}
	for _, r := range legacy {
		u := classify.Use{Card: r["card"], Key: r["key"]}
		if byKey[u]["xmage"] == nil || byKey[u]["forge"] == nil {
			t.Errorf("%v: legs %v, want an xmage and a forge row", u, byKey[u])
		}
	}
	return byKey
}

func TestKeyproofLegsHeaderMatchesSchema(t *testing.T) {
	t.Parallel()
	fx := newKpFixture(t)
	out := filepath.Join(fx.tmp, "out")
	if err := keyproof(fx.reg, fx.args(out, "")); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(out, "keyproof-legs.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := os.ReadFile("../../carddb/schema/v1/keyproof.tsv")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"card"}
	for i, ln := range strings.Split(strings.TrimRight(string(s), "\n"), "\n") {
		if i < 2 {
			continue
		}
		want = append(want, strings.SplitN(ln, "\t", 2)[0])
	}
	got, _, _ := strings.Cut(string(b), "\n")
	if got != strings.Join(want, "\t") {
		t.Errorf("header\n got %q\nwant %q", got, strings.Join(want, "\t"))
	}
}

func TestKeyproofLegsPerEngine(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		mode kpMode
	}{{"both", kpBoth}, {"xmage-only", kpXMageOnly}, {"forge-only", kpForgeOnly}, {"xmage-quiet", kpXMageQuiet}} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			fx := newKpFixtureMode(t, c.mode)
			legs := fx.legRowsOf(t, filepath.Join(fx.tmp, "out"), "")
			// The second item alone decides these keys' legs.
			only := fx.onlySecond(t)
			switch c.mode {
			case kpBoth:
				for u := range fx.stepUses(t) {
					for _, leg := range []string{"xmage", "forge"} {
						if r := legs[u][leg]; r["state"] != "diverged" || !hex12.MatchString(r["sig"]) {
							t.Errorf("%v %s: %v, want diverged with a signature", u, leg, r)
						}
					}
				}
			case kpXMageOnly:
				for _, u := range only {
					if r := legs[u]["xmage"]; r["state"] != "diverged" || !hex12.MatchString(r["sig"]) {
						t.Errorf("%v xmage: %v, want diverged", u, r)
					}
					if r := legs[u]["forge"]; r["state"] != "agreed" || r["refs"] != "F" || r["witness"] != fx.ids[1] {
						t.Errorf("%v forge: %v, want agreed F witnessed by %s", u, r, fx.ids[1])
					}
				}
			case kpForgeOnly:
				for _, u := range only {
					if r := legs[u]["forge"]; r["state"] != "diverged" || !hex12.MatchString(r["sig"]) {
						t.Errorf("%v forge: %v, want diverged", u, r)
					}
					if r := legs[u]["xmage"]; r["state"] != "owed" || r["reason"] != "forge-differs" {
						t.Errorf("%v xmage: %v, want owed forge-differs", u, r)
					}
				}
			case kpXMageQuiet:
				for _, u := range only {
					if r := legs[u]["forge"]; r["state"] != "agreed" || r["refs"] != "F" {
						t.Errorf("%v forge: %v, want agreed F", u, r)
					}
					if r := legs[u]["xmage"]; r["state"] != "owed" || !strings.HasPrefix(r["reason"], "harness:") {
						t.Errorf("%v xmage: %v, want owed harness:...", u, r)
					}
				}
			}
		})
	}
}

func TestKeyproofLegsDiagnosedCite(t *testing.T) {
	t.Parallel()
	fx := newKpFixture(t)
	first := filepath.Join(fx.tmp, "first")
	if err := keyproof(fx.reg, fx.args(first, "")); err != nil {
		t.Fatal(err)
	}
	_, hand := readTSV(t, filepath.Join(first, "handoff.tsv"))
	if len(hand) != 1 {
		t.Fatalf("handoff rows %v", hand)
	}
	classes := filepath.Join(fx.tmp, "classes.tsv")
	reg := "sig\tpattern\tfield\tdesc\tcost\tcr\tupstream\tticket\n" +
		hand[0]["sig"] + "\tFX|G\t*\t*\t*\t702.4b\t-\tdiag-1\n"
	if err := os.WriteFile(classes, []byte(reg), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(fx.tmp, "second")
	legs := fx.legRowsOf(t, out, classes)
	_, rows := readTSV(t, filepath.Join(out, "keyproof.tsv"))
	n := 0
	for _, r := range rows {
		if r["state"] != "diagnosed" {
			continue
		}
		n++
		u := classify.Use{Card: r["card"], Key: r["key"]}
		c := legs[u]["cite"]
		if c == nil || c["state"] != "diagnosed" || c["cite"] != "702.4b" || c["ticket"] != "diag-1" || c["sig"] != hand[0]["sig"] {
			t.Errorf("%v cite: %v, want diagnosed 702.4b diag-1", u, c)
		}
		for _, leg := range []string{"xmage", "forge"} {
			if l := legs[u][leg]; l["state"] != "owed" || l["reason"] != "diagnosed" {
				t.Errorf("%v %s: %v, want owed diagnosed", u, leg, l)
			}
		}
	}
	if n == 0 {
		t.Error("no key turned diagnosed")
	}
}
