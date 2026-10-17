// Command xmageport translates XMage Mage.Tests @Test methods into the
// oracle-scenario JSON that audit.TestXMagePortScenarios runs (design:
// docs/design/2026-10-10-xmage-scenario-port-design.md).
//
//	xmageport -mage <Mage.Tests/src/test/java/org/mage/test> -out <dir> \
//	    [-family name=relative/dir ...] [-commit scenarios/xmageport]
//
// It reads each Java file without a Java parser (java.go), maps the
// CardTestPlayerBase DSL onto scenario steps/expects (translate.go,
// build.go), refuses what it cannot express with a reason code, and plays
// every generated scenario on gorge once (run.go): that run only places
// setChoice/addTarget answers on the step that consumes them -- the
// expectations always come from XMage's own asserts. Output:
//
//	<out>/<family>/<Class>.json   every generated scenario, with its result
//	<out>/report.tsv              one row per @Test method
//	<out>/summary.txt             counts by family, status and reason
//
// With -commit, the PASSING scenarios (minus methods already present in a
// hand-ported file) are written to <commit>/<family>/<Class>.json.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/testkit/testutil"
)

var dropLands bool

var knownBugs map[string]string

type familyFlag []string

func (f *familyFlag) String() string     { return strings.Join(*f, ",") }
func (f *familyFlag) Set(s string) error { *f = append(*f, s); return nil }

func main() {
	mage := flag.String("mage", "/mnt/sata/gorge-training/xmageoracle/mage/Mage.Tests/src/test/java/org/mage/test", "XMage test source root")
	out := flag.String("out", "/mnt/sata/gorge-training/xmagetests/gen", "output directory")
	corpus := flag.String("cards", ".cards", "gorge .cards corpus directory")
	commit := flag.String("commit", "", "write passing scenarios under this directory (scenarios/xmageport)")
	xmIn := flag.String("xmage-in", "", "write XMage driver cross-check input (JSONL) for generated scenarios here")
	only := flag.String("only", "", "only this Class or Class#method")
	norun := flag.Bool("norun", false, "translate only; do not play scenarios")
	var fams familyFlag
	flag.Var(&fams, "family", "family=relative/dir (repeatable)")
	kbPath := flag.String("known-bugs", "cmd/xmageport/known_bugs.tsv", "triaged known_bug rows (Class#method TAB reason)")
	flag.BoolVar(&dropLands, "droplands", false, "drop basic lands that only pay for spells")
	flag.BoolVar(&verbose, "v", false, "print each run's scenario, transcript and fails")
	flag.Parse()
	if len(fams) == 0 {
		fams = familyFlag{"continuous=cards/continuous", "replacement=cards/replacement", "copy=cards/copy",
			"triggers=cards/triggers", "combat=combat"}
	}
	knownBugs = loadKnownBugs(*kbPath)
	reg, err := testutil.OpenCorpusRegistry(*corpus)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var results []*result
	for _, f := range fams {
		name, dir, ok := strings.Cut(f, "=")
		if !ok {
			fmt.Fprintln(os.Stderr, "bad -family", f)
			os.Exit(2)
		}
		root := filepath.Join(*mage, dir)
		var files []string
		filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() && strings.HasSuffix(p, ".java") {
				files = append(files, p)
			}
			return nil
		})
		sort.Strings(files)
		for _, p := range files {
			src, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			rel, _ := filepath.Rel(*mage, p)
			jc, err := parseJava(p, rel, string(src))
			if err != nil {
				continue
			}
			for i := range jc.Methods {
				m := &jc.Methods[i]
				if *only != "" && *only != jc.Name && *only != jc.Name+"#"+m.Name {
					continue
				}
				results = append(results, translate(reg, jc, m, name))
			}
		}
	}
	if !*norun {
		runAll(reg, results)
	}
	if err := writeOutputs(*out, results); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *xmIn != "" {
		var b strings.Builder
		for _, r := range results {
			if r.Scenario == nil || r.Status == "fail-harness" {
				continue
			}
			line, err := driverLine(r)
			if err == nil {
				b.Write(line)
				b.WriteByte('\n')
			}
		}
		os.WriteFile(*xmIn, []byte(b.String()), 0o644)
	}
	if *commit != "" {
		if err := writeCommit(*commit, results); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	fmt.Print(summary(results))
}

type genEntry struct {
	portScenario
	Status string   `json:"x_status"`
	Fails  []string `json:"x_fails,omitempty"`
}

func writeOutputs(out string, results []*result) error {
	byFile := map[string][]*result{}
	var tsv strings.Builder
	tsv.WriteString("family\tclass\tmethod\tstatus\tcode\tdetail\tfirst_fail\tunsupported\n")
	for _, r := range results {
		ff := ""
		if len(r.Fails) > 0 {
			ff = strings.ReplaceAll(r.Fails[0], "\n", " ")
			ff = strings.ReplaceAll(ff, "\t", " ")
			if len(ff) > 300 {
				ff = ff[:300]
			}
		}
		fmt.Fprintf(&tsv, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", r.Family, r.Class, r.Method, r.Status, r.Code,
			strings.NewReplacer("\t", " ", "\n", " ").Replace(r.Detail), ff, r.Unsupported)
		if r.Scenario != nil {
			k := filepath.Join(r.Family, r.Class+".json")
			byFile[k] = append(byFile[k], r)
		}
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(out, "report.tsv"), []byte(tsv.String()), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(out, "summary.txt"), []byte(summary(results)), 0o644); err != nil {
		return err
	}
	for k, rs := range byFile {
		var f struct {
			Source    string     `json:"source"`
			Scenarios []genEntry `json:"scenarios"`
		}
		f.Source = "Mage.Tests/src/test/java/org/mage/test/" + rs[0].Rel + " (XMage, MIT)"
		for _, r := range rs {
			f.Scenarios = append(f.Scenarios, genEntry{portScenario: *r.Scenario, Status: r.Status, Fails: r.Fails})
		}
		p := filepath.Join(out, k)
		os.MkdirAll(filepath.Dir(p), 0o755)
		b, _ := json.MarshalIndent(f, "", " ")
		if err := os.WriteFile(p, append(b, '\n'), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// existingMethods lists XMage Class#method already ported by hand (any file
// not written by this tool), so generation never duplicates them.
func existingMethods(dir string) map[string]bool {
	out := map[string]bool{}
	paths, _ := filepath.Glob(filepath.Join(dir, "*", "*.json"))
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var f portFile
		if json.Unmarshal(raw, &f) != nil || strings.Contains(f.Source, "cmd/xmageport") {
			continue
		}
		for _, s := range f.Scenarios {
			out[s.XMage] = true
		}
	}
	return out
}

// loadKnownBugs reads known_bugs.tsv (Class#method TAB reason).
func loadKnownBugs(path string) map[string]string {
	out := map[string]string{}
	raw, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if k, v, ok := strings.Cut(line, "\t"); ok {
			out[k] = v
		}
	}
	return out
}

func writeCommit(dir string, results []*result) error {
	have := existingMethods(dir)
	byFile := map[string][]*result{}
	for _, r := range results {
		if r.Scenario != nil && r.Status != "pass" {
			if kb := knownBugs[r.Scenario.XMage]; kb != "" {
				r.Scenario.KnownBug = kb
			}
		}
		if r.Scenario == nil || (r.Status != "pass" && r.Scenario.KnownBug == "") || have[r.Scenario.XMage] {
			continue
		}
		k := filepath.Join(r.Family, r.Class+".json")
		byFile[k] = append(byFile[k], r)
	}
	for k, rs := range byFile {
		f := portFile{Source: "Mage.Tests/src/test/java/org/mage/test/" + rs[0].Rel + " (XMage, MIT; generated by cmd/xmageport)"}
		for _, r := range rs {
			f.Scenarios = append(f.Scenarios, *r.Scenario)
		}
		p := filepath.Join(dir, k)
		os.MkdirAll(filepath.Dir(p), 0o755)
		b, _ := json.MarshalIndent(f, "", " ")
		if err := os.WriteFile(p, append(b, '\n'), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func summary(results []*result) string {
	var b strings.Builder
	type key struct{ fam, k string }
	cnt := map[key]int{}
	fams := map[string]bool{}
	reasons := map[string]int{}
	for _, r := range results {
		fams[r.Family] = true
		cnt[key{r.Family, "total"}]++
		cnt[key{r.Family, r.Status}]++
		cnt[key{"ALL", "total"}]++
		cnt[key{"ALL", r.Status}]++
		if r.Status == "refused" {
			reasons[r.Code]++
		}
	}
	names := []string{}
	for f := range fams {
		names = append(names, f)
	}
	sort.Strings(names)
	names = append(names, "ALL")
	statuses := []string{"total", "refused", "generated", "pass", "fail-harness", "fail-expect"}
	fmt.Fprintf(&b, "%-12s", "family")
	for _, s := range statuses {
		fmt.Fprintf(&b, " %12s", s)
	}
	b.WriteString("\n")
	for _, f := range names {
		fmt.Fprintf(&b, "%-12s", f)
		for _, s := range statuses {
			v := cnt[key{f, s}]
			if s == "generated" {
				v = cnt[key{f, "total"}] - cnt[key{f, "refused"}]
			}
			fmt.Fprintf(&b, " %12d", v)
		}
		b.WriteString("\n")
	}
	b.WriteString("\nrefusals by reason:\n")
	type kv struct {
		k string
		v int
	}
	var rs []kv
	for k, v := range reasons {
		rs = append(rs, kv{k, v})
	}
	sort.Slice(rs, func(i, j int) bool { return rs[i].v > rs[j].v || rs[i].v == rs[j].v && rs[i].k < rs[j].k })
	for _, x := range rs {
		fmt.Fprintf(&b, "  %-32s %5d\n", x.k, x.v)
	}
	return b.String()
}
