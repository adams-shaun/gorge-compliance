package adopt

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// statusChildEnv makes TestStatusChild run the gate for these sets (a
// comma list) and print their lines: TestCertificationRatchet's child
// process (see ChildMaxCards for why the gate runs in children).
const statusChildEnv = "GORGE_ADOPT_STATUS_SETS"

// ratchetChildSem bounds TestCertificationRatchet's child processes to the
// four StatusChunked allows one test, at ~0.6 GiB each. (The level-B ratchet
// shared it while it also ran in children; its chunks now run in process.)
var ratchetChildSem = make(chan struct{}, 4)

func TestStatusChild(t *testing.T) {
	sets := os.Getenv(statusChildEnv)
	if sets == "" {
		return // only meaningful as TestCertificationRatchet's child
	}
	reg := testutil.CorpusRegistry(t)
	cs, err := Build(reg, root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := cs.Status(reg, root, "A", strings.Split(sets, ",")...)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range got {
		b, _ := json.Marshal(s)
		os.Stdout.WriteString("STATUS " + string(b) + "\n")
	}
}

func runStatusChild(batch []string) ([]SetStatus, error) {
	ratchetChildSem <- struct{}{}
	defer func() { <-ratchetChildSem }()
	cmd := exec.Command(os.Args[0], "-test.run=^TestStatusChild$", "-test.count=1")
	cmd.Env = append(os.Environ(), statusChildEnv+"="+strings.Join(batch, ","))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, &childError{err, string(out)}
	}
	var sets []SetStatus
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		line, ok := strings.CutPrefix(sc.Text(), "STATUS ")
		if !ok {
			continue
		}
		var s SetStatus
		if err := json.Unmarshal([]byte(line), &s); err != nil {
			return nil, err
		}
		sets = append(sets, s)
	}
	return sets, sc.Err()
}

type childError struct {
	err error
	out string
}

func (e *childError) Error() string { return e.err.Error() + ": " + e.out }

// TestCertificationRatchet is section 11.3 C8's ratchet, modelled on
// knownUnsupported: oracle/compliance/ratchet.json records every committed set's
// declared level (a floor: a declared set or level only grows) and its
// outstanding count at level A (a ceiling: it only shrinks). Every set
// must be in the file and every declaration recorded in it. The gate is
// re-run here for the first target format's sets and every declared set
// (where the verdicts live); `make compliance-status` re-runs it for all
// of them. A set that improved passes and is logged: record it with
// `go run ./cmd/oraclediff status -all -sets <SETS> -write-ratchet`.
func TestCertificationRatchet(t *testing.T) {
	t.Parallel()
	if os.Getenv(statusChildEnv) != "" {
		return
	}
	cs := census(t)
	ratchet, err := LoadRatchet(root)
	if err != nil {
		t.Fatal(err)
	}
	declared, err := compliance.LoadDeclared(filepath.Join(root, "oracle", "compliance", "declared.json"))
	if err != nil {
		t.Fatal(err)
	}
	scope := cs.FormatSets(cs.Config.Formats[0].Name)
	for s := range declared {
		if !contains(scope, s) {
			scope = append(scope, s)
		}
	}
	// 4 concurrent children (the cap StatusChunked allows): the 20-set
	// Standard scope is 3 batches of ChildMaxCards, and at 2 they ran in two
	// rounds, each child re-paying the corpus load. Measured 2026-10-09
	// (4 vCPU budget): 16.4s -> 9.1s; four children peak ~2.4 GiB RSS
	// (~0.6 GiB each, status.go) plus this process, inside the doubled
	// 2026-10-09 per-test budget.
	measured, err := cs.StatusChunked(scope, 4, runStatusChild)
	if err != nil {
		t.Fatal(err)
	}
	if len(measured) != len(scope) {
		t.Fatalf("measured %d of %d sets", len(measured), len(scope))
	}
	fails, slack := CheckRatchet(cs.SetCodes(), measured, declared, ratchet)
	// The per-ticket gate's stale-only classifier reads this line (see
	// ops/scripts/gate_affected.sh): the ratchet's outstanding count includes stale
	// verdicts, so a ratchet that only grew on stale problems is a host XMage
	// replay's to clear, not a defect to fix. Emitted on every run (go test
	// prints it only for a failing test), so a non-stale problem leaves no
	// stale-only verdict and the gate fails closed.
	t.Logf("GORGE-CERT-RESULT %s %s", t.Name(), ratchetVerdict(measured, fails))
	for _, f := range fails {
		t.Error(f)
	}
	if len(slack) > 0 {
		t.Logf("improved past the ratchet (record with -write-ratchet): %s", strings.Join(slack, "; "))
	}
	for _, r := range cs.Rollups(measured) {
		if r.Format == cs.Config.Formats[0].Name {
			t.Logf("ratchet: %d sets measured; %s", len(measured), r.Label)
		}
	}
}

// ratchetVerdict classifies TestCertificationRatchet's failure for the
// per-ticket gate's stale-only classifier (ops/scripts/gate_affected.sh): "clean"
// (no fail), "stale-only" (every fail is a set's outstanding GROWTH and that
// set's problems are ALL stale verdicts, a moved scenario sha a host XMage
// replay clears), or "mixed" (any other fail -- a set no longer committed, a
// declared level that shrank, a committed set missing from the ratchet, a
// floor not recorded -- or a growth on a set with a non-stale problem: a real
// regression the gate must still fail). A structural ratchet fail must never
// read as stale-only just because the measured sets happen to be stale.
func ratchetVerdict(measured []SetStatus, fails []string) string {
	if len(fails) == 0 {
		return "clean"
	}
	bySet := map[string]SetStatus{}
	for _, s := range measured {
		bySet[s.Set] = s
	}
	for _, f := range fails {
		set, rest, ok := strings.Cut(f, ": ")
		if !ok || !strings.Contains(rest, " outstanding, ratchet ") {
			return "mixed"
		}
		s, ok := bySet[set]
		if !ok || s.Outstanding == 0 || s.Outstanding != s.Buckets[BucketStale] {
			return "mixed"
		}
	}
	return "stale-only"
}
