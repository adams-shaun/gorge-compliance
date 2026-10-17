package adopt

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/testkit/testutil"
)

// levelBChunks is how many independent tests the level-B ratchet is sharded
// into. TestLevelBRatchet held the sets that carry a level-B floor in
// oracle/compliance/ratchet.json to it by running the whole B scope in ONE child
// process: 23.8 s wall / 30.2 CPU-s alone at 4 vCPU (2026-10-12), 51 s under
// gate contention, ~7 CPU-s of it the child re-paying the corpus load and
// census Build the test binary already holds. Each chunk now runs the gate
// in this process over the process-shared registry and census (census,
// testutil.CorpusRegistry), so the chunks pay that setup once between them
// and with every other corpus test in the package. A level-B child stayed
// near 0.6-0.8 GiB for one set or four (status.go's ChildMaxCards note: no
// per-scenario heap pin is left), which is what makes in-process safe.
//
// Chunk i covers scope[i*n/K : (i+1)*n/K] of the B scope in set-code order,
// so the chunks partition it exactly -- a B claim that grows grows every
// chunk and never drops a set (TestLevelBRatchetChunksPartitionTheScope). Its
// cost grows with the B claim, exactly as TestCertificationRatchet grows with
// the declared claim. Record an improved count with
// `go run ./cmd/oraclediff status -all -level B -sets <SETS> -write-ratchet`.
const levelBChunks = 4

// levelBScope is the B scope: every census set whose ratchet entry carries an
// outstanding_b floor, in census set-code order.
func levelBScope(t *testing.T) ([]string, map[string]RatchetEntry) {
	t.Helper()
	cs := census(t)
	ratchet, err := LoadRatchet(root)
	if err != nil {
		t.Fatal(err)
	}
	var scope []string
	for _, s := range cs.SetCodes() {
		if e, ok := ratchet[s]; ok && e.OutstandingB != nil {
			scope = append(scope, s)
		}
	}
	if len(scope) == 0 {
		t.Fatal("no set carries an outstanding_b floor; record one with `oraclediff status -all -level B -sets BIG,EOE,FDN,FRA -write-ratchet`")
	}
	return scope, ratchet
}

// levelBChunk returns chunk i's set codes.
func levelBChunk(scope []string, i int) []string {
	n := len(scope)
	return scope[i*n/levelBChunks : (i+1)*n/levelBChunks]
}

// runLevelBChunk is one shard of the level-B ratchet: it measures the chunk's
// sets at level B and holds each to its floor.
func runLevelBChunk(t *testing.T, i int) {
	t.Parallel()
	scope, ratchet := levelBScope(t)
	chunk := levelBChunk(scope, i)
	if len(chunk) == 0 {
		t.Logf("B scope of %d sets leaves chunk %d empty", len(scope), i)
		return
	}
	measured, err := census(t).Status(testutil.CorpusRegistry(t), root, "B", chunk...)
	if err != nil {
		t.Fatal(err)
	}
	if len(measured) != len(chunk) {
		t.Fatalf("measured %d of %d sets", len(measured), len(chunk))
	}
	fails, slack := CheckRatchetB(measured, ratchet)
	for _, f := range fails {
		t.Error(f)
	}
	if len(slack) > 0 {
		t.Logf("improved past the level-B floor (record with -write-ratchet): %s", strings.Join(slack, "; "))
	}
	t.Logf("level-B ratchet chunk %d: %d sets measured: %s", i, len(measured), strings.Join(chunk, ","))
}

func TestLevelBRatchetChunk00(t *testing.T) { runLevelBChunk(t, 0) }
func TestLevelBRatchetChunk01(t *testing.T) { runLevelBChunk(t, 1) }
func TestLevelBRatchetChunk02(t *testing.T) { runLevelBChunk(t, 2) }
func TestLevelBRatchetChunk03(t *testing.T) { runLevelBChunk(t, 3) }

// TestLevelBRatchetChunksPartitionTheScope holds the sharding to full
// coverage: one TestLevelBRatchetChunkN per chunk, and the chunks,
// concatenated, are exactly the B scope.
func TestLevelBRatchetChunksPartitionTheScope(t *testing.T) {
	scope, _ := levelBScope(t)
	var joined []string
	for i := 0; i < levelBChunks; i++ {
		joined = append(joined, levelBChunk(scope, i)...)
	}
	if len(joined) != len(scope) {
		t.Fatalf("chunks cover %d of %d B-scope sets", len(joined), len(scope))
	}
	for i := range scope {
		if joined[i] != scope[i] {
			t.Fatalf("chunk concatenation diverges at %d: %q vs %q", i, joined[i], scope[i])
		}
	}
	if tests := len([]func(*testing.T){TestLevelBRatchetChunk00, TestLevelBRatchetChunk01,
		TestLevelBRatchetChunk02, TestLevelBRatchetChunk03}); tests != levelBChunks {
		t.Fatalf("%d chunk tests for %d chunks", tests, levelBChunks)
	}
}
