package adopt

import (
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/gate"
)

func TestBucket(t *testing.T) {
	for reason, want := range map[string]string{
		"no printed list (compliance/printed/G00.json): ...": BucketNoPrintedList,
		"not in the corpus":                                                  BucketNotInCorpus,
		"unsupported [kw:Bargain]":                                           BucketUnsupported,
		"gorge_wrong verdict (cast-resolve): levels":                         BucketGorgeWrong,
		"XMage does not implement it; needs a hand-authored oracle scenario": BucketXMageLacks,
		"no generated scenario (no fixture gorge can cast) and no hand":      BucketTemplateGap,
		"no verdict for cast-resolve-v1/Shock":                               BucketNoVerdict,
		"verdict diverge (cast-resolve): step 1":                             BucketDiverge,
		"verdict harness (cast-resolve) [ruling r]: xmage: boom":             BucketHarness,
		"automatic ruling r sampled for review; check it":                    BucketReview,
		"verdict is for an older scenario (x); re-run the XMage pass":        BucketStale,
		"level B has no templates yet":                                       BucketLevel,
		"frozen p0.life changed: want 17, have 20 (x)":                       BucketExpectation,
	} {
		if got := Bucket(gate.Problem{Card: "c", Reason: reason}); got != want {
			t.Errorf("Bucket(%q) = %q, want %q", reason, got, want)
		}
	}
}

func TestRollupsLabelCompleteFormats(t *testing.T) {
	cs := &Census{Config: &Config{Formats: []Format{{Name: "Standard", Sets: []string{"A", "B"}}, {Name: "All", Tournament: true}}}}
	sets := []SetStatus{
		{Set: "A", Formats: []string{"Standard", "All"}, Level: "A", Declared: "A", Cards: 10},
		{Set: "B", Formats: []string{"Standard", "All"}, Level: "A", Cards: 5, Outstanding: 3, NonTournament: 1,
			Buckets: map[string]int{BucketUnsupported: 3}},
	}
	r := cs.Rollups(sets)
	if r[0].Label != "Standard: 1/2 sets at A, 3 of 15 entries outstanding" || r[0].Complete() {
		t.Errorf("Standard rollup %+v", r[0])
	}
	if r[1].Outstanding != 2 {
		t.Errorf("All leaves out non-tournament problems: %+v", r[1])
	}
	sets[1].Declared, sets[1].Outstanding, sets[1].NonTournament = "A", 0, 0
	if r := cs.Rollups(sets); r[0].Label != "Standard:A" || !r[0].Complete() {
		t.Errorf("all declared: %+v", r[0])
	}
}

func TestCheckRatchet(t *testing.T) {
	sets := []SetStatus{{Set: "FRA", Outstanding: 0}, {Set: "FDN", Outstanding: 7}, {Set: "M10", Outstanding: 190}}
	declared := compliance.Declared{"FRA": "A"}
	ratchet := map[string]RatchetEntry{"FRA": {Level: "A"}, "FDN": {Outstanding: 6}, "M10": {Outstanding: 198}}
	committed := []string{"FDN", "FRA", "M10"}
	fails, slack := CheckRatchet(committed, sets, declared, ratchet)
	if len(fails) != 1 || !strings.Contains(fails[0], "FDN: 7 outstanding, ratchet 6") {
		t.Errorf("fails %v, want FDN's growth only", fails)
	}
	if !reflect.DeepEqual(slack, []string{"M10: 198 -> 190"}) {
		t.Errorf("slack %v", slack)
	}
	// Dropping a declaration, or declaring without recording the floor,
	// both fail; so does a set the ratchet does not know.
	fails, _ = CheckRatchet(append(committed, "NEW"), sets, compliance.Declared{"FDN": "A"},
		map[string]RatchetEntry{"FRA": {Level: "A"}, "FDN": {Outstanding: 7}, "M10": {Outstanding: 190}})
	want := []string{"FRA: was declared at A", "NEW: a committed set missing", "FDN: declared A but the ratchet's floor"}
	if len(fails) != len(want) {
		t.Fatalf("fails %v", fails)
	}
	for i, w := range want {
		if !strings.HasPrefix(fails[i], w) {
			t.Errorf("fail %d = %q, want prefix %q", i, fails[i], w)
		}
	}
	round := MarshalRatchet(ratchet)
	if !strings.Contains(string(round), "\"FDN\": {\"outstanding\":6},\n") {
		t.Errorf("one set per line: %s", round)
	}
}

// TestCheckRatchetForgeSplit: Forge-only certification may only grow through
// a recorded write, an XMage-verified count never falls, and either upgrade
// is slack that a -write-ratchet records.
func TestCheckRatchetForgeSplit(t *testing.T) {
	committed := []string{"TRK"}
	declared := compliance.Declared{}
	ten, twoTwenty := 10, 220
	measured := SetStatus{Set: "TRK", Outstanding: 3, XMageVerified: 10, ForgeOnly: 220}

	// (a) A first Forge-only measurement fails against a nil ratchet.
	fails, slack := CheckRatchet(committed, []SetStatus{measured}, declared,
		map[string]RatchetEntry{"TRK": {Outstanding: 223}})
	if len(fails) != 1 || !strings.HasPrefix(fails[0], "TRK: 220 Forge-only cards, ratchet 0") {
		t.Errorf("(a) fails %v, want the Forge-only growth only", fails)
	}
	if !strings.Contains(strings.Join(slack, "\n"), "TRK: 223 -> 3") {
		t.Errorf("(a) slack %v, want the outstanding fall", slack)
	}

	// (b) The recorded split matches: no fail, no slack.
	fails, slack = CheckRatchet(committed, []SetStatus{measured}, declared,
		map[string]RatchetEntry{"TRK": {Outstanding: 3, XMageVerified: &ten, ForgeOnly: &twoTwenty}})
	if len(fails) != 0 || len(slack) != 0 {
		t.Errorf("(b) fails %v slack %v, want neither", fails, slack)
	}

	// (c) Forge-only grew while XMage-verified fell: two fails.
	nine, twoTwentyOne := 9, 221
	grown := measured
	grown.XMageVerified, grown.ForgeOnly = nine, twoTwentyOne
	fails, _ = CheckRatchet(committed, []SetStatus{grown}, declared,
		map[string]RatchetEntry{"TRK": {Outstanding: 3, XMageVerified: &ten, ForgeOnly: &twoTwenty}})
	if len(fails) != 2 ||
		!strings.HasPrefix(fails[0], "TRK: 221 Forge-only cards, ratchet 220") ||
		!strings.HasPrefix(fails[1], "TRK: 9 XMage-verified cards, ratchet 10") {
		t.Errorf("(c) fails %v", fails)
	}

	// (d) XMage superseded a Forge row: both movements are slack.
	eleven, twoNineteen := 11, 219
	upgraded := measured
	upgraded.XMageVerified, upgraded.ForgeOnly = eleven, twoNineteen
	fails, slack = CheckRatchet(committed, []SetStatus{upgraded}, declared,
		map[string]RatchetEntry{"TRK": {Outstanding: 3, XMageVerified: &ten, ForgeOnly: &twoTwenty}})
	if len(fails) != 0 {
		t.Errorf("(d) fails %v, want none", fails)
	}
	all := strings.Join(slack, "\n")
	if !strings.Contains(all, "TRK: forge-only 220 -> 219") || !strings.Contains(all, "TRK: xmage-verified 10 -> 11") {
		t.Errorf("(d) slack %v", slack)
	}
}

// TestRatchetOfRecordsForgeSplitOnlyWhenForgeOnly: a set that has never had
// a Forge-only card keeps its one-line entry; one that has records both
// numbers, 0 included.
func TestRatchetOfRecordsForgeSplitOnlyWhenForgeOnly(t *testing.T) {
	sets := []SetStatus{
		{Set: "FDN", Outstanding: 0, XMageVerified: 300},
		{Set: "TRK", Outstanding: 3, XMageVerified: 10, ForgeOnly: 220},
	}
	r := RatchetOf(sets, map[string]RatchetEntry{})
	fdn := r["FDN"]
	if fdn.XMageVerified != nil || fdn.ForgeOnly != nil {
		t.Errorf("FDN split pointers %v/%v, want nil (never Forge-only)", fdn.XMageVerified, fdn.ForgeOnly)
	}
	trk := r["TRK"]
	if trk.XMageVerified == nil || *trk.XMageVerified != 10 || trk.ForgeOnly == nil || *trk.ForgeOnly != 220 {
		t.Errorf("TRK split %v/%v, want 10/220", trk.XMageVerified, trk.ForgeOnly)
	}
	if got := string(MarshalRatchet(r)); !strings.Contains(got, "\"FDN\": {\"outstanding\":0},\n") {
		t.Errorf("FDN entry moved: %s", got)
	}

	// A set that once had Forge-only cards keeps recording both numbers,
	// even when the measured Forge-only count is now 0.
	five := 5
	prev := map[string]RatchetEntry{"TRK": {ForgeOnly: &five}}
	zero := []SetStatus{{Set: "TRK", Outstanding: 0, XMageVerified: 10, ForgeOnly: 0}}
	r = RatchetOf(zero, prev)
	trk = r["TRK"]
	if trk.ForgeOnly == nil || *trk.ForgeOnly != 0 || trk.XMageVerified == nil || *trk.XMageVerified != 10 {
		t.Errorf("TRK after Forge-only: %v/%v, want 10/0", trk.XMageVerified, trk.ForgeOnly)
	}
}
