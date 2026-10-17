package audit_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/internal/gorgedir"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/gate"
)

// oracleDir is one directory of hand-authored scenarios and the label its
// files are reported under.
type oracleDir struct {
	path  string // on disk
	label string // prefix in messages: "scenarios/oracle" or "gorge:engine/rules/testdata/oracle"
}

// oracleDirs is the union the audit reads: this repository's own
// scenarios/oracle (the test runs in audit/, so the repo root is "..") and
// gorge's residual directory. Both must exist: a missing directory would
// make the audit pass over nothing.
func oracleDirs(t *testing.T) []oracleDir {
	t.Helper()
	own := filepath.Join("..", gate.HandOracleRel)
	g := gorgedir.Path(t, gate.GorgeHandOracleRel)
	out := []oracleDir{
		{own, filepath.ToSlash(gate.HandOracleRel)},
		{g, "gorge:" + filepath.ToSlash(gate.GorgeHandOracleRel)},
	}
	for _, d := range out {
		if fi, err := os.Stat(d.path); err != nil || !fi.IsDir() {
			t.Fatalf("hand oracle scenario directory %s (%s) is missing", d.path, d.label)
		}
	}
	return out
}

// oracleFile is one per-card scenario file. The scenarios stay raw: gorge's
// runner owns their schema (rules.CheckOracleScenarioJSON decodes them
// strictly), and this package reads only the authoring fields below.
type oracleFile struct {
	Card      string            `json:"card"`
	Family    string            `json:"family"`
	Author    string            `json:"author"`
	OracleSHA string            `json:"oracle_sha,omitempty"`
	Scenarios []json.RawMessage `json:"scenarios"`

	label string // where it was read, for messages
}

// scenarioHead is the part of a scenario the drivers read themselves.
type scenarioHead struct {
	Name   string            `json:"name"`
	Why    string            `json:"why"`
	CR     []string          `json:"cr"`
	Expect []json.RawMessage `json:"expect"`
	Steps  []struct {
		Expect []json.RawMessage `json:"expect"`
	} `json:"steps"`
}

func headOf(t *testing.T, where string, raw json.RawMessage) scenarioHead {
	t.Helper()
	var h scenarioHead
	if err := json.Unmarshal(raw, &h); err != nil {
		t.Fatalf("%s: %v", where, err)
	}
	return h
}

// loadOracleFiles reads every <dir>/<family>/*.json file of every directory,
// keyed by its label path. The file level is decoded strictly.
func loadOracleFiles(t *testing.T) map[string]oracleFile {
	t.Helper()
	out := map[string]oracleFile{}
	for _, d := range oracleDirs(t) {
		paths, err := filepath.Glob(filepath.Join(d.path, "*", "*.json"))
		if err != nil || len(paths) == 0 {
			t.Fatalf("no oracle scenario files under %s: %v", d.label, err)
		}
		for _, p := range paths {
			raw, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			fam := filepath.Base(filepath.Dir(p))
			label := d.label + "/" + fam + "/" + filepath.Base(p)
			dec := json.NewDecoder(bytes.NewReader(raw))
			dec.DisallowUnknownFields()
			var f oracleFile
			if err := dec.Decode(&f); err != nil {
				t.Fatalf("%s: %v", label, err)
			}
			if f.Card == "" || f.Family == "" || len(f.Scenarios) == 0 {
				t.Fatalf("%s: card, family and at least one scenario are required", label)
			}
			if fam != f.Family {
				t.Fatalf("%s: family %q does not match its directory %q", label, f.Family, fam)
			}
			f.label = label
			out[label] = f
		}
	}
	return out
}

// The audit's two ratchets live one file per card per family, never in a
// shared aggregate (gorge spec 2026-10-03-rules-engine-lasagna-design.md W2):
// two audits of two cards touch two files.
//
//   - <dir>/<family>/known-divergent/<slug>.json: a listed scenario must
//     still FAIL (a fixed one is stale and fails the build until its row is
//     deleted); an unlisted failure fails the build. Rows are only ever added
//     by the triage step, never by the scenario author.
//   - <dir>/<family>/known-unconsumed/<slug>.json: a listed scenario must
//     still leave a step `answers` entry no decision matched (the step
//     index, op and answer); a fixed one is stale. Rows are only ever
//     deleted.
//
// Both share the schema {"card": "<Card>", "rows": {"<scenario>": "<reason>"}}.
// A row naming a scenario of another family, or a card with two files in one
// family, fails the build. Both directories of oracleDirs are read; a
// ratchet file sits next to its scenario file.
const (
	oracleDivergentDir  = "known-divergent"
	oracleUnconsumedDir = "known-unconsumed"
)

type oraclePerCard struct {
	Card string            `json:"card"`
	Rows map[string]string `json:"rows"`
}

type oracleRatchetRow struct {
	reason, family, src string
}

// oracleRatchet reads every <dir>/<family>/<kind>/*.json file into
// "<card>/<scenario>" rows. It needs no corpus.
func oracleRatchet(t *testing.T, kind string) map[string]oracleRatchetRow {
	t.Helper()
	out := map[string]oracleRatchetRow{}
	cardFile := map[string]string{}
	for _, d := range oracleDirs(t) {
		paths, err := filepath.Glob(filepath.Join(d.path, "*", kind, "*.json"))
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range paths {
			raw, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			fam := filepath.Base(filepath.Dir(filepath.Dir(p)))
			src := d.label + "/" + fam + "/" + kind + "/" + filepath.Base(p)
			dec := json.NewDecoder(bytes.NewReader(raw))
			dec.DisallowUnknownFields()
			var pc oraclePerCard
			if err := dec.Decode(&pc); err != nil {
				t.Fatalf("%s: want {\"card\": \"<Card>\", \"rows\": {\"<scenario>\": \"<reason>\"}}: %v", src, err)
			}
			if strings.TrimSpace(pc.Card) == "" || len(pc.Rows) == 0 {
				t.Errorf("%s: card and at least one row are required", src)
				continue
			}
			if prev, dup := cardFile[fam+"/"+pc.Card]; dup {
				t.Errorf("%s: card %q already has a %s file at %s", src, pc.Card, kind, prev)
				continue
			}
			cardFile[fam+"/"+pc.Card] = src
			for sc, reason := range pc.Rows {
				if strings.TrimSpace(sc) == "" || strings.TrimSpace(reason) == "" {
					t.Errorf("%s: row %q needs a non-empty scenario and reason", src, sc)
					continue
				}
				k := pc.Card + "/" + sc
				if prev, dup := out[k]; dup {
					t.Errorf("%s: row %q is also listed in %s", src, k, prev.src)
					continue
				}
				out[k] = oracleRatchetRow{reason: reason, family: fam, src: src}
			}
		}
	}
	return out
}

func oracleDivergent(t *testing.T) map[string]oracleRatchetRow {
	t.Helper()
	return oracleRatchet(t, oracleDivergentDir)
}

func oracleUnconsumed(t *testing.T) map[string]oracleRatchetRow {
	t.Helper()
	return oracleRatchet(t, oracleUnconsumedDir)
}
