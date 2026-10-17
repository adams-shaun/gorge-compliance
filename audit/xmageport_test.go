package audit_test

// XMage interaction-scenario port (docs/design/2026-10-10-xmage-scenario-port-design.md).
//
// Every file under scenarios/xmageport/<family>/*.json is a translation of
// one or more XMage Mage.Tests @Test methods (XMage is MIT-licensed; each
// file's "source" names the original class#method, see
// scenarios/xmageport/NOTICE). The scenarios use the oracle-audit schema
// unchanged plus two port fields, so they run on gorge's runner exactly like
// scenarios/oracle and could also be replayed by the XMage oracle driver.
// Only card NAMES are written here; no Forge script text (the GPL boundary).
//
// A scenario that exposes a gorge engine bug keeps its XMage expectation and
// carries "known_bug": "<reason>". It is then skipped with that reason, and a
// sibling assertion makes the row stale-proof: if the scenario starts
// passing, the test fails and asks for the row to be removed.

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// xmagePortDir is scenarios/xmageport, from this package's directory.
var xmagePortDir = filepath.Join("..", "scenarios", "xmageport")

type xmagePortFile struct {
	Source    string            `json:"source"`
	Scenarios []json.RawMessage `json:"scenarios"`
}

// xmagePortScenario is one ported scenario: the runner's scenario object
// (Body, the port fields removed) plus the two port fields.
type xmagePortScenario struct {
	Name     string
	CR       []string
	Why      string
	XMage    string // the originating test method (Class#method)
	KnownBug string // the gorge divergence this scenario exposes, if any
	Body     []byte
}

// splitPortScenario separates the port fields from the runner's schema,
// which rejects unknown fields.
func splitPortScenario(raw json.RawMessage) (xmagePortScenario, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return xmagePortScenario{}, err
	}
	var sc xmagePortScenario
	for k, dst := range map[string]*string{"xmage": &sc.XMage, "known_bug": &sc.KnownBug} {
		if v, ok := fields[k]; ok {
			if err := json.Unmarshal(v, dst); err != nil {
				return xmagePortScenario{}, err
			}
			delete(fields, k)
		}
	}
	var h struct {
		Name string   `json:"name"`
		CR   []string `json:"cr"`
		Why  string   `json:"why"`
	}
	if err := json.Unmarshal(raw, &h); err != nil {
		return xmagePortScenario{}, err
	}
	sc.Name, sc.CR, sc.Why = h.Name, h.CR, h.Why
	body, err := json.Marshal(fields)
	if err != nil {
		return xmagePortScenario{}, err
	}
	sc.Body = body
	return sc, nil
}

func loadXMagePortFiles(t *testing.T) map[string][]xmagePortScenario {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(xmagePortDir, "*", "*.json"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no xmageport scenario files under %s: %v", xmagePortDir, err)
	}
	out := map[string][]xmagePortScenario{}
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		var f xmagePortFile
		if err := dec.Decode(&f); err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		if f.Source == "" || len(f.Scenarios) == 0 {
			t.Fatalf("%s: source and at least one scenario are required", p)
		}
		for _, r := range f.Scenarios {
			sc, err := splitPortScenario(r)
			if err != nil {
				t.Fatalf("%s: %v", p, err)
			}
			if sc.XMage == "" || sc.Name == "" {
				t.Fatalf("%s: every scenario needs name and xmage", p)
			}
			if probs := rules.CheckOracleScenarioJSON(sc.Body); len(probs) > 0 {
				t.Fatalf("%s/%s: %s", p, sc.Name, strings.Join(probs, "; "))
			}
			out[p] = append(out[p], sc)
		}
	}
	return out
}

// TestXMagePortScenarios runs every ported scenario in the runner's XMage
// fixture mode: XMage's addCard places setup permanents before the game with
// no zone-change triggers, and cmd/xmageport validates through
// rules.RunOracleScenarioJSON, which runs in the same mode.
func TestXMagePortScenarios(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	files := loadXMagePortFiles(t)
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		fam := filepath.Base(filepath.Dir(p))
		slug := strings.TrimSuffix(filepath.Base(p), ".json")
		for _, sc := range files[p] {
			t.Run(fam+"/"+slug+"/"+sc.Name, func(t *testing.T) {
				t.Parallel()
				res, err := rules.RunOracleAuditScenarioJSON(reg, sc.Body, true)
				if err != nil {
					t.Fatalf("%s: %v", p, err)
				}
				if res.ReplayDiff != "" {
					t.Errorf("log-only replay differs:\n%s", res.ReplayDiff)
				}
				fails := res.Fails
				if sc.KnownBug != "" {
					if len(fails) == 0 {
						t.Fatalf("known_bug row is stale (scenario passes now; delete it): %s", sc.KnownBug)
					}
					t.Skipf("known gorge bug: %s\n  observed: %s", sc.KnownBug, strings.Join(fails, "\n  observed: "))
				}
				if len(fails) > 0 {
					t.Errorf("XMage %s CR %v\n  %s\n  FAIL: %s\n  transcript:\n    %s",
						sc.XMage, sc.CR, sc.Why, strings.Join(fails, "\n  FAIL: "), strings.Join(res.Transcript, "\n    "))
				}
			})
		}
	}
}
