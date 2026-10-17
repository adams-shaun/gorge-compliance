package gate_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/adopt"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/gate"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/levelb"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// TestLevelBProblemsBucketLikeA checks the level-B gate's reasons reuse the
// level-A wording: every ECL problem classifies into one of the level-A
// buckets (ECL keeps Swat Away's explained harness row outstanding; FRA, the
// earlier set, has had no level-B problem since lb5-driver served Codie), and none is the "level not built" stub that Check returned for any
// level but A before this ticket.
func TestLevelBProblemsBucketLikeA(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "..")
	reg := testutil.CorpusRegistry(t)
	probs, err := gate.Check(reg, root, "ECL", "B")
	if err != nil {
		t.Fatal(err)
	}
	if len(probs) == 0 {
		t.Fatal("ECL at level B reported no problems; the claim is not empty, so the walk did nothing")
	}
	known := map[string]bool{}
	for _, b := range adopt.Buckets {
		known[b] = true
	}
	var activate int
	for _, p := range probs {
		b := adopt.Bucket(p)
		if b == adopt.BucketLevel {
			t.Errorf("%s: %q bucketed as %q; level B must not emit the unbuilt-level stub", p.Card, p.Reason, b)
		}
		if !known[b] {
			t.Errorf("%s: %q bucketed as unknown %q", p.Card, p.Reason, b)
		}
		if strings.Contains(p.Reason, "activate#") {
			activate++
		}
	}
	// The bucket walk above must have seen an activate# problem only while one
	// is outstanding: a driver batch that settles every activate# row of the set
	// (2026-10-12 18:34 post-replay gates red) legitimately leaves none. So the
	// precondition is that ECL still HAS activate# requirements to be judged,
	// and a problem naming one is bucketed like any other once present.
	if activate == 0 {
		var reqs int
		printed, err := compliance.LoadPrinted(filepath.Join(root, "oracle", "compliance", "printed"), "ECL")
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range printed.Cards {
			c, ok := reg.Lookup(name)
			if !ok {
				continue
			}
			for _, r := range levelb.Requirements(c) {
				if strings.HasPrefix(r.Key, "activate#") {
					reqs++
				}
			}
		}
		if reqs == 0 {
			t.Error("no ECL card has an activate# level-B requirement; the activate bucket is not exercised")
		}
	}
}

// TestLevelBCoveredByAAddsNoProblem picks a real FRA card whose only level-B
// requirement is covered by the level-A cast-resolve scenario (its sole
// requirement is a face-0 self-ETB trigger) and that passes level A: the
// covered requirement must add no level-B problem.
func TestLevelBCoveredByAAddsNoProblem(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "..")
	reg := testutil.CorpusRegistry(t)
	const card = "Craterclaw Colossus"
	c, ok := reg.Lookup(card)
	if !ok {
		t.Fatalf("%s is not in the corpus; pick another covered-by-A card", card)
	}
	reqs := levelb.Requirements(c)
	if len(reqs) == 0 {
		t.Fatalf("%s has no level-B requirements; the covered-by-A path is not exercised", card)
	}
	for _, r := range reqs {
		if !r.CoveredByA {
			t.Fatalf("%s requirement %s is not covered by A; pick a card whose requirements all are", card, r.Key)
		}
	}
	aProbs, err := gate.Check(reg, root, "FRA", "A")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range aProbs {
		if p.Card == card {
			t.Fatalf("%s has a level-A problem (%s); level B would skip the card entirely", card, p.Reason)
		}
	}
	bProbs, err := gate.Check(reg, root, "FRA", "B")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range bProbs {
		if p.Card == card {
			t.Errorf("%s: covered-by-A requirement added a level-B problem: %s", card, p.Reason)
		}
	}
}
