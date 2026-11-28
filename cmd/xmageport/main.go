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
//
// With -commit and -check it writes nothing there: it reports every committed
// generated file this run would change and exits 1, so a hand edit to a
// generated scenario cannot pass unnoticed.
package main

import (
	"bytes"
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
	mage := flag.String("mage", ".oracle-build/xmage/mage/Mage.Tests/src/test/java/org/mage/test", "XMage test source root")
	out := flag.String("out", ".oracle-build/xmagetests/gen", "output directory")
	corpus := flag.String("cards", ".cards", "gorge .cards corpus directory")
	commit := flag.String("commit", "", "write passing scenarios under this directory (scenarios/xmageport)")
	check := flag.Bool("check", false, "with -commit: write nothing and exit 1 if a committed generated file differs from this run's output")
	xmIn := flag.String("xmage-in", "", "write XMage driver cross-check input (JSONL) for generated scenarios here")
	only := flag.String("only", "", "only this Class or Class#method")
	norun := flag.Bool("norun", false, "translate only; do not play scenarios")
	var fams familyFlag
	flag.Var(&fams, "family", "family=relative/dir (repeatable)")
	kbPath := flag.String("known-bugs", "cmd/xmageport/known_bugs.tsv", "triaged known_bug rows (Class#method TAB reason)")
	flag.StringVar(&droppedRecordPath, "dropped-record", "cmd/xmageport/dropped_choices.tsv", "dropped-choice record (Class#method TAB dropped TAB class TAB cr TAB note)")
	flag.BoolVar(&dropLands, "droplands", false, "drop basic lands that only pay for spells")
	flag.BoolVar(&verbose, "v", false, "print each run's scenario, transcript and fails")
	flag.Parse()
	if len(fams) == 0 {
		fams = familyFlag{"continuous=cards/continuous", "replacement=cards/replacement", "copy=cards/copy",
			"triggers=cards/triggers", "combat=combat",
			"cost=cards/cost", "conditional=cards/conditional", "enchantments=cards/enchantments",
			"watchers=cards/watchers", "mana=cards/mana", "rules=cards/rules",
			"prevention=cards/prevention", "planeswalker=cards/planeswalker", "control=cards/control",
			"filters=cards/filters", "requirement=cards/requirement", "restriction=cards/restriction",
			"damage=cards/damage", "dynamicvalue=cards/dynamicvalue", "asthough=cards/asthough",
			"sba=sba", "protection=cards/protection", "modal=cards/modal", "targets=cards/targets",
			"abilities=cards/abilities", "single=cards/single"}
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
			if r.Scenario == nil || r.Status == "fail-harness" || r.Scenario.Players > 2 {
				// the XMage driver replays the two-player game only
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
	switch {
	case *check && *commit == "":
		fmt.Fprintln(os.Stderr, "-check needs -commit <dir>")
		os.Exit(2)
	case *check:
		drift, err := checkCommit(*commit, results)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if len(drift) > 0 {
			fmt.Fprintf(os.Stderr, "%d committed scenario file(s) differ from this run:\n", len(drift))
			for _, p := range drift {
				fmt.Fprintf(os.Stderr, "  %s\n", p)
			}
			os.Exit(1)
		}
	case *commit != "":
		if err := writeCommit(*commit, results); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		// Keep the dropped-choice record in step with the scenarios just
		// written (measured from disk, so a chunk or -only run that left
		// other scenarios alone keeps their rows).
		if err := syncDroppedRecord(*commit, droppedRecordPath); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	// -commit may have rewritten the record above, so write the summary
	// (including its untriaged count) only now, after any sync.
	sum, err := summary(results)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(filepath.Join(*out, "summary.txt"), []byte(sum), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Print(sum)
}

type genEntry struct {
	portScenario
	Status string   `json:"x_status"`
	Fails  []string `json:"x_fails,omitempty"`
}

func writeOutputs(out string, results []*result) error {
	byFile := map[string][]*result{}
	var tsv strings.Builder
	tsv.WriteString("family\tclass\tmethod\tstatus\tcode\tdetail\tfirst_fail\tunsupported\tdropped\n")
	for _, r := range results {
		ff := ""
		if len(r.Fails) > 0 {
			ff = strings.ReplaceAll(r.Fails[0], "\n", " ")
			ff = strings.ReplaceAll(ff, "\t", " ")
			if len(ff) > 300 {
				ff = ff[:300]
			}
		}
		fmt.Fprintf(&tsv, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%d\n", r.Family, r.Class, r.Method, r.Status, r.Code,
			strings.NewReplacer("\t", " ", "\n", " ").Replace(r.Detail), ff, r.Unsupported, r.Dropped)
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

// writeCommit writes this run's passing scenarios into dir, keeping committed
// scenarios the run did not regenerate.
func writeCommit(dir string, results []*result) error {
	var drift []string
	return commitFiles(dir, results, true, &drift)
}

// checkCommit reports the committed files the same run would change, without
// writing any of them. It is writeCommit's dry run, so a hand-edited
// generated scenario (or a missing or stale one) is named here instead of
// surviving as a canonical artifact.
func checkCommit(dir string, results []*result) ([]string, error) {
	var drift []string
	if err := commitFiles(dir, results, false, &drift); err != nil {
		return nil, err
	}
	return drift, nil
}

func commitFiles(dir string, results []*result, apply bool, drift *[]string) error {
	// put writes one generated file, or in check mode collects it when the
	// committed bytes differ (a missing file differs from every output).
	put := func(p string, f portFile) error {
		if apply {
			return writePortFile(p, f)
		}
		got, err := os.ReadFile(p)
		if err != nil || !bytes.Equal(got, marshalPortFile(f)) {
			*drift = append(*drift, p)
		}
		return nil
	}
	// remove is put's counterpart for a file whose only scenarios a triaged
	// removal deleted.
	remove := func(p string) error {
		if apply {
			return os.Remove(p)
		}
		if _, err := os.Stat(p); err == nil {
			*drift = append(*drift, p)
		}
		return nil
	}
	have := existingMethods(dir)
	byFile := map[string][]*result{}
	files := map[string]bool{}
	status := map[string]string{}
	written := map[string]bool{}
	for _, r := range results {
		// Key by the class/method even when the row refused before a scenario
		// existed: a refused row is a deliberate removal of any old scenario.
		status[r.Class+"#"+r.Method] = r.Status
		files[filepath.Join(r.Family, r.Class+".json")] = true
		if r.Scenario == nil {
			continue
		}
		if r.Status != "pass" && r.Status != "fail-replay" {
			if kb := knownBugs[r.Scenario.XMage]; kb != "" {
				r.Scenario.KnownBug = kb
			}
		}
		// fail-replay is never written, known_bug or not: the audit fails a
		// replay diff before it looks at known_bug.
		if r.Status == "fail-replay" || (r.Status != "pass" && r.Scenario.KnownBug == "") || have[r.Scenario.XMage] {
			continue
		}
		k := filepath.Join(r.Family, r.Class+".json")
		byFile[k] = append(byFile[k], r)
		written[r.Scenario.XMage] = true
	}
	var keys []string
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		rs := byFile[k]
		p := filepath.Join(dir, k)
		// Keep committed scenarios this run did not regenerate. A run is often
		// a chunk or a single -only method, and a method whose translation now
		// fails the harness or is refused has no replacement scenario: writing
		// only this run's results would silently delete coverage that still
		// passes the audit. A refusal is a limit of this translator, not
		// evidence the committed scenario is wrong, so its old scenario stays
		// too. A method is only dropped when this run wrote its replacement
		// (or triaged it fail-replay, which is never written and outranks the
		// old row).
		old, hadOld := readPortFile(p)
		var kept []portScenario
		for _, sc := range old.Scenarios {
			switch status[sc.XMage] {
			case "":
				kept = append(kept, sc)
			case "fail-harness", "fail-expect", "refused":
				if !written[sc.XMage] {
					kept = append(kept, sc)
				}
			}
		}
		if len(rs) == 0 {
			// Nothing to write for this class. Only a file this tool generated
			// may lose scenarios, and only when a removal was triaged.
			if !hadOld || !strings.Contains(old.Source, "cmd/xmageport") || len(kept) == len(old.Scenarios) {
				continue
			}
			if len(kept) == 0 {
				if err := remove(p); err != nil {
					return err
				}
				continue
			}
			old.Scenarios = kept
			if err := put(p, old); err != nil {
				return err
			}
			continue
		}
		f := portFile{Source: "Mage.Tests/src/test/java/org/mage/test/" + rs[0].Rel + " (XMage, MIT; generated by cmd/xmageport)"}
		f.Scenarios = append(f.Scenarios, kept...)
		for _, r := range rs {
			f.Scenarios = append(f.Scenarios, *r.Scenario)
		}
		if err := put(p, f); err != nil {
			return err
		}
	}
	return nil
}

func marshalPortFile(f portFile) []byte {
	b, _ := json.MarshalIndent(f, "", " ")
	return append(b, '\n')
}

func writePortFile(p string, f portFile) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, marshalPortFile(f), 0o644)
}

// readPortFile reads one committed scenario file. An absent or unreadable
// file yields ok=false so writeCommit starts from this run's results.
func readPortFile(path string) (portFile, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return portFile{}, false
	}
	var f portFile
	if json.Unmarshal(raw, &f) != nil {
		return portFile{}, false
	}
	return f, true
}

func summary(results []*result) (string, error) {
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
	// dropped-choice totals: what this run dropped, and how much of the
	// record still argues no reason (classes empty).
	tests, choices := 0, 0
	for _, r := range results {
		if r.Dropped > 0 {
			tests++
			choices += r.Dropped
		}
	}
	// A record that will not parse must fail the run, not report untriaged=0
	// and look clean.
	record, err := loadDroppedRecord(droppedRecordPath)
	if err != nil {
		return "", err
	}
	untriaged := 0
	for _, row := range record {
		if row.Class == "" {
			untriaged++
		}
	}
	fmt.Fprintf(&b, "\ndropped-choice tests=%d choices=%d untriaged=%d\n", tests, choices, untriaged)
	return b.String(), nil
}
