package adjudicate

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclediff"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/classify"
	"github.com/adams-shaun/gorge/engine/rules"
)

func finding(id, engine, class, msg string) classify.Finding {
	return classify.Finding{ID: id, Engine: engine, Class: class, Msg: msg, Step: 0}
}

// classOf is the xe-6 class a two-checkpoint row gets against gorge's 18.
func classOf(r oraclediff.XResult) string {
	if r.Harness != "" || len(r.Snapshots) < 2 {
		return classify.ClassHarness
	}
	if r.Snapshots[1].Players[0].Life == 18 {
		return classify.ClassAgree
	}
	return classify.ClassDiverge
}

func TestFourWay(t *testing.T) {
	it := item("i", "Shock")
	g := result(18)
	for _, c := range []struct {
		name          string
		x, f, p       oraclediff.XResult
		pf            *classify.Finding // nil: from the row
		four, label   string
		vote, gpField string
	}{
		{"phase confirms FX|G", row("i", 20, ""), row("i", 20, ""), row("i", 20, ""), nil,
			"FPX|G", "gorge_suspect", oraclediff.PhaseWithOthers, "p0.life"},
		{"phase sides with gorge against FX", row("i", 20, ""), row("i", 20, ""), row("i", 18, ""), nil,
			"GP|FX", "split", oraclediff.PhaseWithGorge, ""},
		{"phase alone where the three agree", row("i", 18, ""), row("i", 18, ""), row("i", 17, ""), nil,
			"GFX|P", "phase_suspect", oraclediff.PhaseAlone, "p0.life"},
		{"phase confirms a GF|X", row("i", 20, ""), row("i", 18, ""), row("i", 18, ""), nil,
			"GFP|X", "xmage_suspect", oraclediff.PhaseWithGorge, ""},
		{"phase lacks the card", row("i", 20, ""), row("i", 20, ""), oraclediff.XResult{}, ptr(finding("i", "phase", classify.ClassNotSent, "phase_lacks_card")),
			"FX|G P?", "gorge_suspect", oraclediff.PhaseUnsupported, ""},
		{"phase lacks a card at run time", row("i", 20, ""), row("i", 20, ""), row("i", 20, "phase lacks card: Shock"), ptr(finding("i", "phase", classify.ClassXMageLacks, "phase lacks card: Shock")),
			"FX|G P?", "gorge_suspect", oraclediff.PhaseUnsupported, ""},
		{"phase harness before any difference", row("i", 20, ""), row("i", 20, ""), oraclediff.XResult{ID: "i", Harness: "driver gap", Snapshots: result(18).Snapshots[:1]}, nil,
			"FX|G P–", "gorge_suspect", oraclediff.PhaseUncompared, ""},
		{"xmage lacks, phase with forge", row("i", 18, "Couldn't find a card: Shock"), row("i", 20, ""), row("i", 20, ""), ptr(finding("i", "phase", classify.ClassDiverge, "")),
			"FP|G X?", "gorge_suspect", oraclediff.PhaseWithOthers, "p0.life"},
	} {
		t.Run(c.name, func(t *testing.T) {
			pf := finding("i", "phase", classOf(c.p), "")
			if c.pf != nil {
				pf = *c.pf
			}
			xc := classOf(c.x)
			if c.x.Harness != "" {
				xc = classify.ClassXMageLacks
			}
			_, r := Four(it, g, c.x, c.f, c.p, finding("i", "xmage", xc, ""), finding("i", "forge", classOf(c.f), ""), pf)
			if r.Four != c.four || r.Label != c.label || r.Phase != c.vote || r.GPField != c.gpField {
				t.Fatalf("got %q/%q/%s/%q, want %q/%q/%s/%q", r.Four, r.Label, r.Phase, r.GPField, c.four, c.label, c.vote, c.gpField)
			}
		})
	}
}

func ptr(f classify.Finding) *classify.Finding { return &f }

// threeCheckpoints is a result with life `a` at step 0 and `b` at step 1.
func threeCheckpoints(a, b int32) rules.OracleResult {
	r := result(a)
	s := r.Snapshots[1]
	s.Checkpoint = "step 1 (pass)"
	s.Players = []rules.OracleSnapPlayer{{Seat: 0, Life: b, LibraryCount: 30}, {Seat: 1, Life: 20, LibraryCount: 30}}
	r.Snapshots = append(r.Snapshots, s)
	return r
}

// A phase row that stopped (harness) after parting from gorge earlier than
// the checkpoint the three-way verdict turned on stands alone; one that stopped
// before it with no difference says nothing.
func TestFourWayPhaseStoppedEarly(t *testing.T) {
	it := oraclegen.Item{ID: "i", Card: "Shock", Scenario: oraclegen.Scenario{Steps: []oraclegen.Step{{Op: "cast", Card: "Shock"}, {Op: "pass"}}}}
	g := threeCheckpoints(18, 15)
	x := oraclediff.XResult{ID: "i", Snapshots: threeCheckpoints(18, 16).Snapshots}
	f := oraclediff.XResult{ID: "i", Snapshots: threeCheckpoints(18, 16).Snapshots}
	parted := oraclediff.XResult{ID: "i", Harness: "stuck", Snapshots: threeCheckpoints(17, 0).Snapshots[:2]}
	_, r := Four(it, g, x, f, parted, finding("i", "xmage", classify.ClassDiverge, ""), finding("i", "forge", classify.ClassDiverge, ""), finding("i", "phase", classify.ClassDiverge, ""))
	if r.Three != "FX|G" || r.Four != "FX|G|P" || r.Phase != oraclediff.PhaseAlone {
		t.Fatalf("parted early: %+v", r)
	}
	quiet := oraclediff.XResult{ID: "i", Harness: "stuck", Snapshots: threeCheckpoints(18, 0).Snapshots[:2]}
	_, r = Four(it, g, x, f, quiet, finding("i", "xmage", classify.ClassDiverge, ""), finding("i", "forge", classify.ClassDiverge, ""), finding("i", "phase", classify.ClassHarness, ""))
	if r.Four != "FX|G P–" || r.Phase != oraclediff.PhaseUncompared {
		t.Fatalf("stopped quiet: %+v", r)
	}
}

func TestEligibleFour(t *testing.T) {
	for _, c := range []struct {
		x, f, p string
		want    bool
	}{
		{classify.ClassAgree, classify.ClassAgree, classify.ClassDiverge, true},
		{classify.ClassDiverge, classify.ClassDiverge, classify.ClassNotSent, true},
		{classify.ClassAgree, classify.ClassAgree, classify.ClassAgree, false},
		{classify.ClassAgree, classify.ClassAgree, classify.ClassNotSent, false},
		{classify.ClassDiverge, classify.ClassAgree, classify.ClassDecisionShape, false},
		{classify.ClassNotSent, classify.ClassAgree, classify.ClassDiverge, true},
		{classify.ClassRNG, classify.ClassAgree, classify.ClassDiverge, false},
	} {
		if got := EligibleFour(c.x, c.f, c.p); got != c.want {
			t.Errorf("EligibleFour(%s, %s, %s) = %v, want %v", c.x, c.f, c.p, got, c.want)
		}
	}
}

// The fourth column adds files and never changes the three-way ones.
func TestFourWayLeavesThreeWayOutputAlone(t *testing.T) {
	base := func() Input {
		return Input{
			Items: map[string]oraclegen.Item{"a": item("a", "Shock"), "b": item("b", "Opt")},
			Gorge: map[string]rules.OracleResult{"a": result(18), "b": result(18)},
			XRows: map[string]oraclediff.XResult{"a": row("a", 20, ""), "b": row("b", 18, "")},
			FRows: map[string]oraclediff.XResult{"a": row("a", 20, ""), "b": row("b", 18, "")},
			XFind: map[string]classify.Finding{"a": finding("a", "xmage", classify.ClassDiverge, ""), "b": finding("b", "xmage", classify.ClassAgree, "")},
			FFind: map[string]classify.Finding{"a": finding("a", "forge", classify.ClassDiverge, ""), "b": finding("b", "forge", classify.ClassAgree, "")},
		}
	}
	three := filepath.Join(t.TempDir(), "three")
	res3 := Run(base())
	if res3.Four != nil {
		t.Fatalf("no phase, yet a four-way table: %+v", res3.Four)
	}
	if err := Write(three, res3); err != nil {
		t.Fatal(err)
	}
	in := base()
	in.PRows = map[string]oraclediff.XResult{"a": row("a", 20, ""), "b": row("b", 17, "")}
	in.PFind = map[string]classify.Finding{"a": finding("a", "phase", classify.ClassDiverge, ""), "b": finding("b", "phase", classify.ClassDiverge, "")}
	four := filepath.Join(t.TempDir(), "four")
	res4 := Run(in)
	if err := Write(four, res4); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(three)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		a, _ := os.ReadFile(filepath.Join(three, e.Name()))
		b, err := os.ReadFile(filepath.Join(four, e.Name()))
		if err != nil || !bytes.Equal(a, b) {
			t.Errorf("%s changed with phase (err %v)", e.Name(), err)
		}
	}
	if len(res4.Four) != 2 || res4.Four[0].Four != "FPX|G" || res4.Four[1].Four != "GFX|P" {
		t.Fatalf("four-way rows: %+v", res4.Four)
	}
	pat, err := os.ReadFile(filepath.Join(four, "patterns4.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "FPX|G\t1\nGFX|P\t1\nphase:alone\t1\nphase:with_others\t1\n"; string(pat) != want {
		t.Fatalf("patterns4.tsv:\n%s\nwant:\n%s", pat, want)
	}
	if _, err := os.Stat(filepath.Join(three, "fourway.jsonl")); err == nil {
		t.Fatal("fourway.jsonl written without phase")
	}
}
