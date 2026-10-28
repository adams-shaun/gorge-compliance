package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/classify"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/lower"
)

// The calibration gate (DESIGN §11 R1): a follower proceeds to a sweep only
// if at least GateWindows of its lower_ok windows were followed (neither
// harness nor decision_shape). The share of 2-turn tape prefixes followed
// decides whether the follower runs whole games (O4): below GateTape it is
// window-only.
const (
	GateWindows = 0.80
	GateTape    = 0.50
	GateTapeN   = 2
	topMessages = 10
)

var (
	windowID = regexp.MustCompile(`:w\d+$`)
	prefixID = regexp.MustCompile(`:tape:t(\d+)$`)
	numbers  = regexp.MustCompile(`#?\d+`)
	// prefixTurn is the ":t<N>" a tape prefix's id adds to its tape's.
	prefixTurn = regexp.MustCompile(`:t\d+$`)
)

// Calib is calib.json: the calibration of one follower on one play directory.
type Calib struct {
	Engine  string       `json:"engine"`
	Windows CalibWindows `json:"windows"`
	Tapes   CalibTapes   `json:"tapes"`
	Cover   CalibCover   `json:"coverage"`
	Gate    CalibGate    `json:"gate"`
}

// CalibWindows counts the one-turn windows: ByClass over every window the play
// lowered (a window whose self-check failed is a lower_gap), and the rates over
// the lower_ok ones.
type CalibWindows struct {
	Total   int            `json:"total"`
	LowerOK int            `json:"lower_ok"`
	ByClass map[string]int `json:"by_class"`
	// Harness is the lower_ok windows classed harness; NotFollowed those classed
	// harness or decision_shape; the rates are shares of LowerOK.
	Harness         int       `json:"harness"`
	NotFollowed     int       `json:"not_followed"`
	HarnessRate     float64   `json:"harness_rate"`
	NotFollowedRate float64   `json:"not_followed_rate"`
	TopHarness      []MsgRate `json:"top_harness"`
}

// MsgRate is a normalised harness message and how many items had it.
type MsgRate struct {
	Msg   string `json:"msg"`
	Count int    `json:"count"`
}

// CalibTapes is the prefix curve: Curve[N] is the share of tape prefixes of N
// turns not classed harness or decision_shape. The whole tape (no prefix) is
// reported under "full". MedianFirstHarness is the median scenario step of the
// earliest harness row of each tape that has one (-1 when there are none).
type CalibTapes struct {
	Curve              map[string]CurvePoint `json:"curve"`
	MedianFirstHarness float64               `json:"median_first_harness_step"`
}

// CurvePoint is one point of the prefix curve.
type CurvePoint struct {
	Items    int     `json:"items"`
	Followed int     `json:"followed"`
	Share    float64 `json:"share"`
}

// CalibCover counts the (card, key) pairs the run touched.
type CalibCover struct {
	Exercised int `json:"exercised"`
	Compared  int `json:"compared"`
}

// CalibGate applies the gate. Tape2 is nil when the run has no 2-turn prefix.
type CalibGate struct {
	Windows bool  `json:"windows"`
	Tape2   *bool `json:"tape2"`
}

func runCalib(args []string) error {
	fs := flag.NewFlagSet("calib", flag.ExitOnError)
	playDir := fs.String("play", "", "play output dir (required)")
	diffDir := fs.String("diff", "", "diff output dir (required); CALIB.md and calib.json are written here")
	engine := fs.String("engine", "", "follower: xmage or forge (required)")
	fs.Parse(args)
	if *playDir == "" || *diffDir == "" || (*engine != "xmage" && *engine != "forge") {
		return fmt.Errorf("calib needs -play, -diff and -engine xmage|forge")
	}
	if err := classify.RefuseRepoPath(*diffDir); err != nil {
		return err
	}
	c, err := calibrate(*playDir, *diffDir, *engine)
	if err != nil {
		return err
	}
	jb, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := writeAtomic(filepath.Join(*diffDir, "calib.json"), append(jb, '\n')); err != nil {
		return err
	}
	var md bytes.Buffer
	c.markdown(&md)
	if err := writeAtomic(filepath.Join(*diffDir, "CALIB.md"), md.Bytes()); err != nil {
		return err
	}
	fmt.Print(md.String())
	return nil
}

// followed is true for a class where the follower replayed the item far enough
// to be compared.
func followed(class string) bool {
	return class != classify.ClassHarness && class != classify.ClassDecisionShape &&
		class != classify.ClassPaymentChoice
}

// normMsg strips object ids and numbers from a harness message.
func normMsg(m string) string {
	return strings.TrimSpace(numbers.ReplaceAllString(m, "N"))
}

// calibrate reads playDir/lower.jsonl, diffDir/verdicts.jsonl and the two
// coverage ledgers, and applies the gate. The result is a pure function of the
// files.
func calibrate(playDir, diffDir, engine string) (Calib, error) {
	status := map[string]string{}
	if err := readLines(filepath.Join(playDir, "lower.jsonl"), func(b []byte) error {
		var r lowerRow
		if err := json.Unmarshal(b, &r); err != nil {
			return err
		}
		status[r.ID] = r.Status
		return nil
	}); err != nil {
		return Calib{}, err
	}
	c := Calib{Engine: engine}
	c.Windows.ByClass = map[string]int{}
	c.Tapes.Curve = map[string]CurvePoint{}
	msgs := map[string]int{}
	first := map[string]int{} // tape -> scenario step of its earliest harness row
	if err := readLines(filepath.Join(diffDir, "verdicts.jsonl"), func(b []byte) error {
		var f classify.Finding
		if err := json.Unmarshal(b, &f); err != nil {
			return err
		}
		switch m := prefixID.FindStringSubmatch(f.ID); {
		case windowID.MatchString(f.ID):
			w := &c.Windows
			w.Total++
			w.ByClass[f.Class]++
			if status[f.ID] != lower.StatusOK {
				return nil
			}
			w.LowerOK++
			if f.Class == classify.ClassHarness {
				w.Harness++
				msgs[normMsg(f.Msg)]++
			}
			if !followed(f.Class) {
				w.NotFollowed++
			}
		case strings.HasSuffix(f.ID, ":tape") || m != nil:
			key := "full"
			if m != nil {
				key = m[1]
			}
			p := c.Tapes.Curve[key]
			p.Items++
			if followed(f.Class) {
				p.Followed++
			}
			c.Tapes.Curve[key] = p
			if f.Class == classify.ClassHarness {
				tape := prefixTurn.ReplaceAllString(f.ID, "")
				if at, seen := first[tape]; !seen || f.Step < at {
					first[tape] = f.Step
				}
			}
		}
		return nil
	}); err != nil {
		return Calib{}, err
	}
	w := &c.Windows
	if w.LowerOK > 0 {
		w.HarnessRate = float64(w.Harness) / float64(w.LowerOK)
		w.NotFollowedRate = float64(w.NotFollowed) / float64(w.LowerOK)
	}
	for _, m := range sortedKeys(msgs) {
		w.TopHarness = append(w.TopHarness, MsgRate{m, msgs[m]})
	}
	sort.SliceStable(w.TopHarness, func(i, j int) bool { return w.TopHarness[i].Count > w.TopHarness[j].Count })
	if len(w.TopHarness) > topMessages {
		w.TopHarness = w.TopHarness[:topMessages]
	}
	for k, p := range c.Tapes.Curve {
		p.Share = float64(p.Followed) / float64(p.Items)
		c.Tapes.Curve[k] = p
	}
	c.Tapes.MedianFirstHarness = -1
	if n := len(first); n > 0 {
		firstHarness := make([]int, 0, n)
		for _, st := range first {
			firstHarness = append(firstHarness, st)
		}
		sort.Ints(firstHarness)
		c.Tapes.MedianFirstHarness = float64(firstHarness[n/2])
		if n%2 == 0 {
			c.Tapes.MedianFirstHarness = float64(firstHarness[n/2-1]+firstHarness[n/2]) / 2
		}
	}
	c.Gate.Windows = w.LowerOK > 0 && 1-w.NotFollowedRate >= GateWindows
	if p, ok := c.Tapes.Curve[strconv.Itoa(GateTapeN)]; ok {
		ok := p.Share >= GateTape
		c.Gate.Tape2 = &ok
	}
	var err error
	if c.Cover.Exercised, err = countKeys(filepath.Join(playDir, "exercised.tsv"), 2); err != nil {
		return Calib{}, err
	}
	if c.Cover.Compared, err = countKeys(filepath.Join(diffDir, "compared.tsv"), 3); err != nil {
		return Calib{}, err
	}
	return c, nil
}

// countKeys is the rows of a coverage ledger TSV (card, key, exercised,
// compared, offered) whose column col is above 0.
func countKeys(path string, col int) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	n := 0
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 256<<20)
	for first := true; sc.Scan(); first = false {
		if first {
			continue
		}
		cols := strings.Split(sc.Text(), "\t")
		if col < len(cols) {
			if v, err := strconv.Atoi(cols[col]); err == nil && v > 0 {
				n++
			}
		}
	}
	return n, sc.Err()
}

func (c Calib) markdown(w *bytes.Buffer) {
	pct := func(f float64) string { return fmt.Sprintf("%.1f%%", 100*f) }
	fmt.Fprintf(w, "# xengine calibration: %s\n\n## Windows\n\n", c.Engine)
	fmt.Fprintf(w, "%d windows, %d lower_ok.\n\n```\n", c.Windows.Total, c.Windows.LowerOK)
	for _, k := range sortedKeys(c.Windows.ByClass) {
		fmt.Fprintf(w, "%-15s %d\n", k, c.Windows.ByClass[k])
	}
	fmt.Fprintf(w, "```\n\nOf the lower_ok windows: harness %s, not followed (harness or decision_shape) %s.\n", pct(c.Windows.HarnessRate), pct(c.Windows.NotFollowedRate))
	if len(c.Windows.TopHarness) > 0 {
		fmt.Fprintf(w, "\nTop harness messages:\n\n")
		for _, m := range c.Windows.TopHarness {
			fmt.Fprintf(w, "- %d x %s\n", m.Count, m.Msg)
		}
	}
	fmt.Fprintf(w, "\n## Tapes\n\nShare of tape prefixes followed, by turns kept:\n\n```\n")
	keys := sortedKeys(c.Tapes.Curve)
	sort.SliceStable(keys, func(i, j int) bool {
		a, ea := strconv.Atoi(keys[i])
		b, eb := strconv.Atoi(keys[j])
		if ea != nil || eb != nil {
			return ea == nil
		}
		return a < b
	})
	for _, k := range keys {
		p := c.Tapes.Curve[k]
		fmt.Fprintf(w, "%-5s %d/%d  %s\n", k, p.Followed, p.Items, pct(p.Share))
	}
	fmt.Fprintf(w, "```\n\nMedian step of the first harness row of a tape: %v\n", c.Tapes.MedianFirstHarness)
	fmt.Fprintf(w, "\n## Coverage\n\n(card, key) pairs exercised by the run: %d; compared against %s: %d.\n", c.Cover.Exercised, c.Engine, c.Cover.Compared)
	fmt.Fprintf(w, "\n## Gate\n\n- %.0f%% of lower_ok windows followed: %v\n", 100*GateWindows, c.Gate.Windows)
	if c.Gate.Tape2 == nil {
		fmt.Fprintf(w, "- %.0f%% of %d-turn tape prefixes followed: no such prefix in this run\n", 100*GateTape, GateTapeN)
	} else {
		fmt.Fprintf(w, "- %.0f%% of %d-turn tape prefixes followed: %v\n", 100*GateTape, GateTapeN, *c.Gate.Tape2)
	}
}
