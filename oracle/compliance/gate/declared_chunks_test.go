package gate

import (
	"path/filepath"
	"sort"
	"sync"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// declaredChunks is how many independent tests the compliance claim (spec
// section 8: every set in oracle/compliance/declared.json meets its level at this
// head) is sharded into. Run whole it was the package's pole: 15.3 s alone
// at 4 vCPU and 27-34 s under gate contention (2026-10-12), of which ~5 s is
// the shared corpus, ~1.3 s the root-wide gate tables and ~0.3-0.8 s each
// declared set. Chunk i checks codes[i*n/K : (i+1)*n/K] of the claim in
// set-code order, so the chunks partition the claim exactly -- a claim that
// grows grows every chunk and never drops a set
// (TestDeclaredSetsCompliantChunksPartitionTheClaim). Its cost grows with the
// claim, not the corpus.
//
// Run the whole claim with -run 'TestDeclaredSetsCompliantChunk'.
const declaredChunks = 4

// declaredClaim is what every chunk shares, loaded once per test binary: the
// claim, its set codes in order, the process-shared registry and the
// root-wide gate tables CheckDeclared loads once per call (loadShared), so K
// chunks pay them once, not K times.
var declaredClaim struct {
	sync.Once
	declared compliance.Declared
	codes    []string
	reg      *cards.Registry
	sh       *sharedTables
	fatal    string
}

const declaredRoot = "../../.."

func loadDeclaredClaim(t *testing.T) {
	t.Helper()
	declaredClaim.Do(func() {
		declared, err := compliance.LoadDeclared(filepath.Join(declaredRoot, "oracle", "compliance", "declared.json"))
		if err != nil {
			declaredClaim.fatal = err.Error()
			return
		}
		declaredClaim.declared = declared
		for set := range declared {
			declaredClaim.codes = append(declaredClaim.codes, set)
		}
		sort.Strings(declaredClaim.codes)
		if len(declared) == 0 {
			return
		}
		// SharedCorpus compiles the corpus when the binary's fingerprint-keyed
		// IR cache is absent (a branch whose engine/cards/ sources differ from the one
		// that last compiled it) and caches the result per process. Bare
		// LoadRegistry would instead fail hard on that absent cache, and the
		// gate runs this package in parallel with ./engine/rules, the only other
		// writer: that raced the cache into existence and failed the gate
		// (cli-20261006T024354Z-e5dc4abb). A genuinely missing corpus still
		// errors and fails the test. It is also the process-shared registry: a
		// registry is read-only after open, and a fresh load per test cost
		// ~600 MB and ~1 s.
		corpus, ok := testutil.SharedCorpusDir(".")
		if !ok {
			declaredClaim.fatal = "declared sets need the corpus: could not resolve the repo root"
			return
		}
		reg, err := cards.SharedCorpus(corpus)
		if err != nil {
			declaredClaim.fatal = "declared sets need the corpus (make fetch-cards compile-cards): " + err.Error()
			return
		}
		sh, err := loadShared(reg, declaredRoot)
		if err != nil {
			declaredClaim.fatal = err.Error()
			return
		}
		declaredClaim.reg, declaredClaim.sh = reg, sh
	})
	if declaredClaim.fatal != "" {
		t.Fatal(declaredClaim.fatal)
	}
}

// declaredChunk returns chunk i's set codes.
func declaredChunk(codes []string, i int) []string {
	n := len(codes)
	return codes[i*n/declaredChunks : (i+1)*n/declaredChunks]
}

// runDeclaredChunk is one shard of the compliance claim: CheckDeclared's
// per-set walk (checkLevel over the once-loaded shared tables) over the
// chunk's sets, every problem an error. A missing corpus fails rather than
// skips: a declared set cannot be checked without it.
func runDeclaredChunk(t *testing.T, i int) {
	t.Parallel()
	loadDeclaredClaim(t)
	if len(declaredClaim.declared) == 0 {
		t.Log("no declared sets")
		return
	}
	chunk := declaredChunk(declaredClaim.codes, i)
	if len(chunk) == 0 {
		t.Logf("claim of %d sets leaves chunk %d empty", len(declaredClaim.codes), i)
		return
	}
	var probs []Problem
	for _, set := range chunk {
		level := declaredClaim.declared[set]
		ps, err := checkLevel(declaredClaim.reg, declaredRoot, set, level, declaredClaim.sh)
		if err != nil {
			t.Fatalf("%s:%s: %v", set, level, err)
		}
		probs = append(probs, ps...)
		for _, p := range ps {
			t.Errorf("%s:%s %s: %s", set, level, p.Card, p.Reason)
		}
	}
	// The per-ticket gate's stale-only classifier reads this line (see
	// CertVerdict and ops/scripts/gate_affected.sh): a chunk whose every problem is
	// a stale verdict is a host XMage replay's to clear, not a defect to fix, so
	// the gate requeues it instead of charging a fix round. Emitted on every
	// run (go test prints it only for a failing test), so a non-stale problem,
	// or a t.Fatal before this line, leaves no stale-only verdict and the gate
	// fails closed.
	t.Logf("GORGE-CERT-RESULT %s %s", t.Name(), CertVerdict(probs))
	t.Logf("declared chunk %d: %v", i, chunk)
}

func TestDeclaredSetsCompliantChunk00(t *testing.T) { runDeclaredChunk(t, 0) }
func TestDeclaredSetsCompliantChunk01(t *testing.T) { runDeclaredChunk(t, 1) }
func TestDeclaredSetsCompliantChunk02(t *testing.T) { runDeclaredChunk(t, 2) }
func TestDeclaredSetsCompliantChunk03(t *testing.T) { runDeclaredChunk(t, 3) }

// TestDeclaredSetsCompliantChunksPartitionTheClaim holds the sharding to full
// coverage: one TestDeclaredSetsCompliantChunkN per chunk, and the chunks,
// concatenated, are exactly the declared claim in set-code order.
func TestDeclaredSetsCompliantChunksPartitionTheClaim(t *testing.T) {
	declared, err := compliance.LoadDeclared(filepath.Join(declaredRoot, "oracle", "compliance", "declared.json"))
	if err != nil {
		t.Fatal(err)
	}
	codes := make([]string, 0, len(declared))
	for set := range declared {
		codes = append(codes, set)
	}
	sort.Strings(codes)
	var joined []string
	for i := 0; i < declaredChunks; i++ {
		joined = append(joined, declaredChunk(codes, i)...)
	}
	if len(joined) != len(codes) {
		t.Fatalf("chunks cover %d of %d declared sets", len(joined), len(codes))
	}
	for i := range codes {
		if joined[i] != codes[i] {
			t.Fatalf("chunk concatenation diverges at %d: %q vs %q", i, joined[i], codes[i])
		}
	}
	if tests := len([]func(*testing.T){TestDeclaredSetsCompliantChunk00, TestDeclaredSetsCompliantChunk01,
		TestDeclaredSetsCompliantChunk02, TestDeclaredSetsCompliantChunk03}); tests != declaredChunks {
		t.Fatalf("%d chunk tests for %d chunks", tests, declaredChunks)
	}
}
