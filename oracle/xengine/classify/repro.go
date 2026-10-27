package classify

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclediff"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/lower"
	"github.com/adams-shaun/gorge/engine/rules"
)

// Origin names where a finding's inputs live, for REPRO.md: the play
// directory the item came from and the follower rows file it was compared
// with. Empty fields print as placeholders.
type Origin struct{ Play, Rows string }

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// Sanitise is id as a directory name ("xe:FDN:0:1:tape" -> "xe_FDN_0_1_tape").
func Sanitise(id string) string { return unsafeName.ReplaceAllString(id, "_") }

// Dir is the repro directory of f under out: <out>/finds/<class>/<id>.
func Dir(out string, f Finding) string {
	return filepath.Join(out, "finds", f.Class, Sanitise(f.ID))
}

// RefuseRepoPath rejects a path inside the module (the nearest ancestor of the
// working directory holding go.mod): follower output is never committed.
// (cmd/oraclediff has the same guard as package main, so it cannot be imported.)
func RefuseRepoPath(path string) error {
	root := repoRoot()
	if root == "" {
		return nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	abs, root = resolveExisting(abs), resolveExisting(root)
	if rel, err := filepath.Rel(root, abs); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("refusing to write %s: it is inside the repository %s (follower output and finds stay off-repo)", path, root)
	}
	return nil
}

func repoRoot() string {
	d, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d
		}
		p := filepath.Dir(d)
		if p == d {
			return ""
		}
		d = p
	}
}

// resolveExisting resolves symlinks in the longest existing prefix of p.
func resolveExisting(p string) string {
	rest := ""
	for {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return filepath.Join(r, rest)
		}
		parent := filepath.Dir(p)
		if parent == p {
			return filepath.Join(p, rest)
		}
		rest = filepath.Join(filepath.Base(p), rest)
		p = parent
	}
}

// WriteRepro writes the repro directory of f into dir (which Dir names):
// scenario.json (the item cut after the diverging step, ids unchanged),
// input.jsonl (that scenario as the one-line follower input), gorge.json and
// <engine>.json (both results cut to the checkpoints the scenario has),
// verdict.json and REPRO.md. A lower_gap finding has no follower and no
// gorge result: it gets the scenario, the verdict and REPRO.md only.
func WriteRepro(dir string, f Finding, item lower.Lowered, g rules.OracleResult, x oraclediff.XResult) error {
	return WriteReproFrom(dir, f, item, g, x, Origin{})
}

// WriteReproFrom is WriteRepro with the origin REPRO.md's commands name.
func WriteReproFrom(dir string, f Finding, item lower.Lowered, g rules.OracleResult, x oraclediff.XResult, o Origin) error {
	if err := RefuseRepoPath(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	cut := Truncate(item.Item, f)
	files := map[string]any{"scenario.json": cut, "verdict.json": f}
	if f.Class != ClassLowerGap {
		files["gorge.json"] = cutResult(g, f)
		files[f.Engine+".json"] = cutX(x, f)
	}
	for name, v := range files {
		b, err := json.MarshalIndent(v, "", " ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, name), append(b, '\n'), 0o644); err != nil {
			return err
		}
	}
	line, err := json.Marshal(cut)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "input.jsonl"), append(line, '\n'), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "REPRO.md"), []byte(reproMD(dir, f, o)), 0o644)
}

// Truncate cuts the item to steps [0, f.Step]. A finding with no checkpoint,
// and one whose step is not known, keeps every step; the setup checkpoint
// keeps the first step (a scenario needs one).
func Truncate(it oraclegen.Item, f Finding) oraclegen.Item {
	n := len(it.Steps)
	if f.Checkpoint != "" {
		n = min(max(f.Step+1, 1), n)
	}
	it.Steps = append([]oraclegen.Step{}, it.Steps[:n]...)
	if len(it.XAnswers) > n {
		it.XAnswers = it.XAnswers[:n]
	}
	if len(it.XAbility) > n {
		it.XAbility = it.XAbility[:n]
	}
	if len(it.XPos) > n {
		it.XPos = it.XPos[:n]
	}
	if len(it.XTargetSkips) > n {
		it.XTargetSkips = it.XTargetSkips[:n]
	}
	return it
}

// keepSnapshots is how many leading snapshots f's checkpoint ends at (all of
// them when it has none or none is named so).
func keepSnapshots(snaps []rules.OracleSnapshot, f Finding) int {
	if f.Checkpoint != "" {
		for i, s := range snaps {
			if s.Checkpoint == f.Checkpoint {
				return i + 1
			}
		}
	}
	return len(snaps)
}

func cutResult(g rules.OracleResult, f Finding) rules.OracleResult {
	if f.Checkpoint == "" {
		return g
	}
	g.Snapshots = g.Snapshots[:keepSnapshots(g.Snapshots, f)]
	var fl []rules.OracleFlip
	for _, x := range g.Flips {
		if x.Step <= f.Step {
			fl = append(fl, x)
		}
	}
	g.Flips = fl
	var ds []rules.OracleDecision
	for _, d := range g.Decisions {
		if d.Step <= f.Step {
			ds = append(ds, d)
		}
	}
	g.Decisions = ds
	return g
}

func cutX(x oraclediff.XResult, f Finding) oraclediff.XResult {
	if f.Checkpoint != "" {
		x.Snapshots = x.Snapshots[:keepSnapshots(x.Snapshots, f)]
	}
	return x
}

func reproMD(dir string, f Finding, o Origin) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	play, rows := o.Play, o.Rows
	if play == "" {
		play = "<PLAY_DIR>"
	}
	if rows == "" {
		rows = "<ROWS.jsonl>"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s: %s", f.Class, f.ID)
	if f.Engine != "" {
		fmt.Fprintf(&b, " (%s)", f.Engine)
	}
	b.WriteString("\n\n")
	if f.Checkpoint != "" {
		fmt.Fprintf(&b, "First difference at `%s`, field `%s`:\n\n- gorge: `%s`\n- %s: `%s`\n\n", f.Checkpoint, f.Field, f.Gorge, f.Engine, f.Other)
	}
	if f.Msg != "" {
		fmt.Fprintf(&b, "%s\n\n", f.Msg)
	}
	if f.Class == ClassLowerGap {
		fmt.Fprintf(&b, "The item never reached a follower: the lowering disagrees with gorge's own runner (a bug here, not in an engine). Re-run the play that made it:\n\n```\ngo run -trimpath ./cmd/xengine play ... -out <new dir>\n```\n\nand read its `lower.jsonl` row for `%s`.\n", f.ID)
		return b.String()
	}
	run := map[string]string{"xmage": "scripts/xmage-oracle-run.sh", "forge": "scripts/forge-oracle-run.sh"}[f.Engine]
	in := filepath.Join(abs, "input.jsonl")
	rerun := filepath.Join(abs, f.Engine+".rerun.jsonl")
	b.WriteString("Re-run the follower on this one scenario (the item cut after the diverging step):\n\n```\n")
	if f.Engine == "forge" {
		// forge-oracle-run.sh reads Forge requests, which carry gorge's decisions.
		req := filepath.Join(abs, "forge.request.jsonl")
		fmt.Fprintf(&b, "go run -trimpath ./cmd/oraclediff forge-export -scenarios %s -out %s\n", in, req)
		in = req
	}
	fmt.Fprintf(&b, "%s %s %s\n```\n\nThen classify it again against the play's gorge result (cut to the rows' checkpoints):\n\n```\n", run, in, rerun)
	fmt.Fprintf(&b, "go run -trimpath ./cmd/xengine diff -play %s -engine %s -rows %s -only %s -out <out dir outside the repository>\n```\n", play, f.Engine, rerun, f.ID)
	fmt.Fprintf(&b, "\nThe original follower rows were read from `%s`.\n", rows)
	return b.String()
}
