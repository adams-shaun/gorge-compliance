package main

// Triage of the committed record: the group key that gathers rows failing the
// same way, and the defect table, scenarios/padseats-defects.tsv, which
// collects the record's gorge-defect rows into one row per defect class. The
// table is the input for gorge defect tickets, one per row.

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const (
	defectsName   = "padseats-defects.tsv"
	defectsHeader = "defect\tcr\tgroup\trows\trepro\tsummary"
	// defectNotePrefix starts the note of every gorge-defect row, followed by
	// the defect id and a space.
	defectNotePrefix = "defect:"
	// maxSentence bounds a note or a summary.
	maxSentence = 120
	// padUnclassedPer is the decision-day bound on unclassed differs rows:
	// at most one in padUnclassedPer scenarios (2%).
	padUnclassedPer = 50
)

var (
	digitRun = regexp.MustCompile(`[0-9]+`)
	// defectID is a defect id: lowercase letters, digits and hyphens, at most
	// 40 characters.
	defectID = regexp.MustCompile(`^[a-z0-9-]{1,40}$`)
	// ruleID is one comprehensive-rules id, such as 800.4a.
	ruleID = regexp.MustCompile(`^[0-9]{3}(\.[0-9]+[a-z]?)?$`)
)

// padGroupKey is a first_diff with its values dropped: "expect" for an
// expectation failure, else the text before the first ": " with every run of
// digits replaced by "N".
func padGroupKey(firstDiff string) string {
	if strings.HasPrefix(firstDiff, "expect: ") {
		return "expect"
	}
	head, _, _ := strings.Cut(firstDiff, ": ")
	return digitRun.ReplaceAllString(head, "N")
}

// unclassedOverBound reports whether more than one in padUnclassedPer
// scenarios is an unclassed differs row.
func unclassedOverBound(unclassed, scenarios int) bool {
	return unclassed*padUnclassedPer > scenarios
}

// defect is one row of the defect table.
type defect struct {
	ID, CR, Group string
	Rows          int
	Repro         string
	Summary       string
}

// noteDefect returns the defect id a gorge-defect note cites: the note starts
// "defect:<id> ".
func noteDefect(note string) (string, bool) {
	rest, ok := strings.CutPrefix(note, defectNotePrefix)
	if !ok {
		return "", false
	}
	id, _, ok := strings.Cut(rest, " ")
	return id, ok && defectID.MatchString(id)
}

// parseDefects reads the defect table strictly: the header, six columns, ids
// of the id form sorted and unique, one rule id in cr, a positive rows count
// and a one-sentence summary.
func parseDefects(data []byte) ([]defect, error) {
	text, ok := strings.CutSuffix(string(data), "\n")
	lines := strings.Split(text, "\n")
	if !ok || lines[0] != defectsHeader {
		return nil, fmt.Errorf("want the header %q and newline-terminated rows", defectsHeader)
	}
	var out []defect
	var errs []error
	for i, line := range lines[1:] {
		bad := func(format string, args ...any) {
			errs = append(errs, fmt.Errorf("line %d: "+format, append([]any{i + 2}, args...)...))
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			bad("%d columns, want 6", len(f))
			continue
		}
		n, err := strconv.Atoi(f[3])
		if err != nil || n < 1 {
			bad("rows %q, want a positive count", f[3])
		}
		d := defect{ID: f[0], CR: f[1], Group: f[2], Rows: n, Repro: f[4], Summary: f[5]}
		if !defectID.MatchString(d.ID) {
			bad("defect id %q: want lowercase letters, digits and hyphens, at most 40", d.ID)
		}
		if len(out) > 0 && d.ID <= out[len(out)-1].ID {
			bad("%q is not sorted after %q", d.ID, out[len(out)-1].ID)
		}
		if !ruleID.MatchString(d.CR) {
			bad("cr %q: want one rule id", d.CR)
		}
		if d.Group == "" || d.Repro == "" {
			bad("group and repro are required")
		}
		if d.Summary == "" || len(d.Summary) > maxSentence {
			bad("summary: want one sentence of at most %d characters", maxSentence)
		}
		out = append(out, d)
	}
	return out, errors.Join(errs...)
}

// checkDefects lists how the record's gorge-defect rows and the defect table
// depart from each other. steps gives a scenario's step count, for the repro
// (the shortest scenario citing the id); a key it does not know fails.
func checkDefects(record []row, table []defect, steps func(key string) (int, bool)) []error {
	byID := map[string]defect{}
	for _, d := range table {
		byID[d.ID] = d
	}
	citing := map[string][]row{}
	var errs []error
	for _, r := range record {
		if r.Class != classGorgeDefect {
			if _, ok := noteDefect(r.Note); ok {
				errs = append(errs, fmt.Errorf("%s: class %q cites a defect; only %s rows do", r.Scenario, r.Class, classGorgeDefect))
			}
			continue
		}
		if r.CR == "" {
			errs = append(errs, fmt.Errorf("%s: %s row has no cr", r.Scenario, classGorgeDefect))
		}
		id, ok := noteDefect(r.Note)
		if !ok {
			errs = append(errs, fmt.Errorf("%s: note %q does not start %q<id> ", r.Scenario, r.Note, defectNotePrefix))
			continue
		}
		d, ok := byID[id]
		if !ok {
			errs = append(errs, fmt.Errorf("%s: defect %q is not in %s", r.Scenario, id, defectsName))
			continue
		}
		if r.CR != d.CR {
			errs = append(errs, fmt.Errorf("%s: cr %q, defect %s has %q", r.Scenario, r.CR, id, d.CR))
		}
		citing[id] = append(citing[id], r)
	}
	for _, d := range table {
		rows := citing[d.ID]
		if len(rows) != d.Rows {
			errs = append(errs, fmt.Errorf("defect %s: rows %d, the record cites it %d times", d.ID, d.Rows, len(rows)))
		}
		if len(rows) == 0 {
			continue
		}
		shortest, repro := -1, false
		for _, r := range rows {
			if g := padGroupKey(r.FirstDiff); g != d.Group {
				errs = append(errs, fmt.Errorf("defect %s: group %q, %s has %q", d.ID, d.Group, r.Scenario, g))
			}
			n, ok := steps(r.Scenario)
			if !ok {
				errs = append(errs, fmt.Errorf("defect %s: %s names no scenario", d.ID, r.Scenario))
				continue
			}
			if shortest < 0 || n < shortest {
				shortest = n
			}
			repro = repro || r.Scenario == d.Repro
		}
		if !repro {
			errs = append(errs, fmt.Errorf("defect %s: repro %q is not a row citing it", d.ID, d.Repro))
		} else if n, _ := steps(d.Repro); shortest >= 0 && n != shortest {
			errs = append(errs, fmt.Errorf("defect %s: repro %q has %d steps, a citing row has %d", d.ID, d.Repro, n, shortest))
		}
	}
	return errs
}

// scenarioSteps is the number of steps of each scenario, by key.
func scenarioSteps(scs []scenario) (map[string]int, error) {
	out := make(map[string]int, len(scs))
	for _, s := range scs {
		var body struct {
			Steps []json.RawMessage `json:"steps"`
		}
		if err := json.Unmarshal(s.Raw, &body); err != nil {
			return nil, fmt.Errorf("%s: %w", s.Key, err)
		}
		out[s.Key] = len(body.Steps)
	}
	return out, nil
}
