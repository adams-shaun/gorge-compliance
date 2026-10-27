package lower

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
)

// Check statuses.
const (
	StatusOK  = "lower_ok"
	StatusGap = "lower_gap"
)

// Check is the self-check verdict on a lowered scenario. A lower_gap is a bug
// in the lowering (or a divergence between the live game and gorge's own
// runner), never an engine verdict.
type Check struct {
	Status string
	// Step is the scenario step the first problem is at (-1 for the setup
	// checkpoint); Field is the digest path (Digest.Diff) or "harness",
	// "fail" or "flip"; Gorge and Tape are the two values: what the runner
	// produced and what the live game recorded.
	Step        int
	Field       string
	Gorge, Tape string
	Fails       []string
	// Result is gorge's own result for the scenario (zero when the runner
	// could not run it), kept so a comparison against a follower never
	// replays gorge.
	Result rules.OracleResult
}

var failStep = regexp.MustCompile(`^(?:after )?step (\d+) \(`)

// SelfCheck runs the lowered scenario through gorge's oracle runner and holds
// it to the tape. After every step whose live state is known (StepEntry >= 0),
// the digest of the runner's checkpoint must equal the digest recorded on the
// entry that follows the step's last entry; the setup checkpoint must equal
// the digest of the first entry the scenario starts from. The runner's own
// failures, a harness error and a coin flipped differently from the live game
// are gaps too.
func SelfCheck(reg *cards.Registry, l Lowered, t *game.Tape) (out Check) {
	if len(l.Item.Steps) == 0 {
		why := l.Truncated
		if why == "" {
			why = "no steps"
		}
		return Check{Status: StatusGap, Step: -1, Field: "empty", Gorge: why}
	}
	res, err := rules.RunOracleScenarioJSON(reg, l.Item.Raw())
	defer func() { out.Result = res }()
	if err != nil {
		return Check{Status: StatusGap, Step: -1, Field: "harness", Gorge: err.Error()}
	}
	if len(res.Fails) > 0 {
		step := -1
		if m := failStep.FindStringSubmatch(res.Fails[0]); m != nil {
			step, _ = strconv.Atoi(m[1])
		}
		return Check{Status: StatusGap, Step: step, Field: "fail", Gorge: res.Fails[0], Fails: res.Fails}
	}
	byName := map[string]rules.OracleSnapshot{}
	for _, s := range res.Snapshots {
		byName[s.Checkpoint] = s
	}
	sm := seatMap{swap: l.Swap}
	tapeDigest := func(entry int) game.Digest {
		d := t.Entries[entry].Digest
		if sm.swap {
			d = d.Swapped()
		}
		return d
	}
	compare := func(step int, name string, entry int) *Check {
		snap, ok := byName[name]
		if !ok {
			return &Check{Status: StatusGap, Step: step, Field: "checkpoint", Gorge: "missing " + name}
		}
		diff := game.DigestOfSnapshot(snap).Diff(tapeDigest(entry))
		if diff == nil {
			return nil
		}
		path, rest, _ := strings.Cut(diff[0], ": ")
		g, tp, _ := strings.Cut(rest, " != ")
		return &Check{Status: StatusGap, Step: step, Field: path, Gorge: g, Tape: tp}
	}

	found := compare(-1, "setup", l.FirstEntry)
	lastEntry := -1
	for i := range l.Item.Steps {
		if found != nil {
			break
		}
		e := l.StepEntry[i]
		if e < 0 {
			continue
		}
		lastEntry = e
		if e+1 >= len(t.Entries) {
			continue
		}
		found = compare(i, fmt.Sprintf("step %d (%s)", i, l.Item.Steps[i].Op), e+1)
	}
	if f := flipGap(sm, res.Flips, t, lastEntry); f != nil && (found == nil || f.Step <= found.Step) {
		found = f
	}
	if found != nil {
		return *found
	}
	return Check{Status: StatusOK, Step: -1}
}

// flipGap compares the coin flips the runner made with the ones the live game
// made up to tape entry last.
func flipGap(sm seatMap, got []rules.OracleFlip, t *game.Tape, last int) *Check {
	var want []game.Flip
	for i := 0; i <= last && i < len(t.Entries); i++ {
		for _, f := range t.Entries[i].Flips {
			f.Seat = sm.seat(f.Seat)
			want = append(want, f)
		}
	}
	render := func(fs []game.Flip) string {
		var b strings.Builder
		for _, f := range fs {
			fmt.Fprintf(&b, "p%d:%v ", f.Seat, f.Win)
		}
		return strings.TrimSpace(b.String())
	}
	var have []game.Flip
	for _, f := range got {
		have = append(have, game.Flip{Seat: f.Seat, Win: f.Win})
	}
	for i := 0; i < max(len(have), len(want)); i++ {
		if i < len(have) && i < len(want) && have[i] == want[i] {
			continue
		}
		step := -1
		if i < len(got) {
			step = got[i].Step
		}
		return &Check{Status: StatusGap, Step: step, Field: "flip", Gorge: render(have), Tape: render(want)}
	}
	return nil
}
