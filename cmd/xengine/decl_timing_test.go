package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclediff"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

// declTimingOut names a directory holding, per testdata item, a replay output
// "<item file>.out.jsonl" to compare instead of starting the XMage driver.
const declTimingOut = "XMAGE_DECL_TIMING_OUT"

// xmageReplay is the XMage driver's result for the one item (testdata file name) in path, or the
// reason the replay cannot be made here.
func xmageReplay(t *testing.T, name, path string) (oraclediff.XResult, string) {
	t.Helper()
	out := os.Getenv(declTimingOut)
	if out != "" {
		out = filepath.Join(out, name+".out.jsonl")
	} else {
		root := os.Getenv("XMAGE_ORACLE_DIR")
		if root == "" {
			root = "/mnt/sata/gorge-training/xmageoracle"
		}
		if _, err := os.Stat(filepath.Join(root, "mage", "Mage.Tests", "target", "test-classes")); err != nil {
			return oraclediff.XResult{}, "no XMage build at " + root
		}
		if f, err := os.OpenFile(filepath.Join(root, "driver.lock"), os.O_RDWR|os.O_CREATE, 0o644); err != nil {
			return oraclediff.XResult{}, "XMage build is not writable: " + err.Error()
		} else {
			f.Close()
		}
		out = filepath.Join(t.TempDir(), "xmage.jsonl")
		cmd := exec.Command("bash", "../../scripts/xmage-oracle-run.sh", path, out)
		cmd.Env = append(os.Environ(), "XMAGE_ORACLE_MEM=4G", "XMAGE_ORACLE_PRIVATE_DB=1")
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("xmage replay: %v\n%s", err, b)
		}
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var x oraclediff.XResult
	if err := json.Unmarshal(b, &x); err != nil {
		t.Fatalf("replay output: %v", err)
	}
	return x, ""
}

// replayItem runs the item in testdata/name in gorge and in XMage and returns
// the first difference between them. It skips when XMage cannot be run here.
func replayItem(t *testing.T, name string) oraclediff.Verdict {
	t.Helper()
	path := filepath.Join("testdata", name)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var it oraclegen.Item
	if err := json.Unmarshal(raw, &it); err != nil {
		t.Fatal(err)
	}
	// The replay input is one JSON object per line.
	var compact json.RawMessage
	if err := json.Unmarshal(raw, &compact); err != nil {
		t.Fatal(err)
	}
	line, err := json.Marshal(compact)
	if err != nil {
		t.Fatal(err)
	}
	in := filepath.Join(t.TempDir(), "in.jsonl")
	if err := os.WriteFile(in, append(line, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	x, why := xmageReplay(t, name, in)
	if why != "" {
		t.Skip(why)
	}
	reg := testutil.CorpusRegistry(t)
	g, err := rules.RunOracleScenarioJSON(reg, it.Raw())
	return oraclediff.Compare(g, err, x)
}

// TestXmageDeclTiming replays, in XMage, the head of xe:FDN:0:11:w8 (the
// passes up to the attack declaration of Angel of Vitality and one pass past
// it). gorge's checkpoint of the pass that reaches declare-attackers shows the
// Angel untapped and not attacking, since the declaration is the next step.
// XMage declares in the step's beginStep, so the driver takes that checkpoint
// at the last priority before it; it must not diverge on permanents.
func TestXmageDeclTiming(t *testing.T) {
	v := replayItem(t, "decl_timing_item.json")
	if v.Status == oraclediff.Harness {
		t.Fatalf("harness: %s %s", v.Engine, v.Msg)
	}
	if v.Field == "permanents" {
		t.Errorf("permanents diverge at %s:\ngorge: %s\nxmage: %s", v.Checkpoint, v.Gorge, v.XMage)
	}
	if v.Status != oraclediff.Agree {
		t.Errorf("the head of the item should agree: %+v", v)
	}
}

// TestXmageEmptyStackPassPair replays the head of xe:BLB:0:3:w3. gorge's
// Coruscation Mage puts an ability on the stack after the spell resolves and
// the next pass pair resolves it; XMage puts none there, so its stack is empty
// at that pair. The driver used to fail the replay ("pass pair needs a stack
// object"); it must follow the position, leave the stack difference to the
// comparison, and run on.
func TestXmageEmptyStackPassPair(t *testing.T) {
	v := replayItem(t, "empty_stack_pair_item.json")
	if v.Status == oraclediff.Harness {
		t.Fatalf("harness: %s %s", v.Engine, v.Msg)
	}
	if v.Status != oraclediff.Diverge || v.Field != "stack" {
		t.Errorf("want the stack difference of the missing ability, got %+v", v)
	}
}
