package main

// Dropped-choice record (cmd/xmageport/dropped_choices.tsv). When an XMage
// test scripts a setChoice/addTarget that gorge never asks for, the runner
// drops the choice and runResult notes it in the scenario's why ("N XMage
// choice(s) gorge never asks were dropped."). A choice XMage asks and gorge
// does not is a difference between two engines -- sometimes XMage asks
// needlessly, sometimes gorge skips a choice the rules require -- so each
// translated test with dropped choices gets one row here to triage instead
// of living only as prose in why.
//
// Row layout (header xmage\tdropped\tclass\tcr\tnote, sorted by xmage):
//   - xmage:   Class#method, the scenario's xmage field
//   - dropped: the number of dropped choices
//   - class:   empty (untriaged) or one of droppedClasses
//   - cr:      the rule that decides it; required for xmage-extra-ask and
//              gorge-missing-ask
//   - note:    one sentence, no tab

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// droppedRecordPath is where the record lives (flag -dropped-record);
// summary reads it for the untriaged count.
var droppedRecordPath = "cmd/xmageport/dropped_choices.tsv"

type droppedRow struct {
	XMage   string
	Dropped int
	Class   string
	CR      string
	Note    string
}

// droppedClasses is the closed triage set ("" is the untriaged state).
var droppedClasses = map[string]bool{
	"":                  true,
	"xmage-extra-ask":   true, // the rules need no choice there (one legal option, or XMage confirms something the rules make automatic)
	"gorge-missing-ask": true, // the rules give the player a choice gorge makes for them (a gorge defect)
	"setup-trigger":     true, // the choice answers a trigger of a setup permanent before the first step, which the scenario format cannot script
	"harness":           true, // any other translation limit
}

// droppedNeedsCR lists the classes whose claim cites the rule that decides it.
var droppedNeedsCR = map[string]bool{
	"xmage-extra-ask":   true,
	"gorge-missing-ask": true,
}

const droppedHeader = "xmage\tdropped\tclass\tcr\tnote"

// reDroppedWhy matches the sentence runResult appends when r.Dropped > 0.
var reDroppedWhy = regexp.MustCompile(`(\d+) XMage choice\(s\) gorge never asks were dropped\.`)

// droppedFromScenarios walks the committed scenario tree under root and
// returns each scenario's xmage key to the number of choices its why says
// were dropped. A test whose scenario dropped none is absent.
func droppedFromScenarios(root string) (map[string]int, error) {
	out := map[string]int{}
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(p, ".json") {
			return nil
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		var f portFile
		if err := json.Unmarshal(raw, &f); err != nil {
			return fmt.Errorf("%s: %v", p, err)
		}
		for _, sc := range f.Scenarios {
			m := reDroppedWhy.FindStringSubmatch(sc.Why)
			if m == nil || sc.XMage == "" {
				continue
			}
			n, err := strconv.Atoi(m[1])
			if err != nil {
				return fmt.Errorf("%s: %s: bad dropped count %q: %v", p, sc.XMage, m[1], err)
			}
			if prev, ok := out[sc.XMage]; ok && prev != n {
				return fmt.Errorf("%s: %s drops %d choices here but %d elsewhere", p, sc.XMage, n, prev)
			}
			out[sc.XMage] = n
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// mergeDropped rebuilds the record from a measurement: rows for the measured
// tests only, dropped from the measurement, and class/cr/note kept from the
// old row -- except that a row whose count changed returns to untriaged
// (triage argued the old count, so it must be redone). The result is sorted
// by xmage.
func mergeDropped(record []droppedRow, measured map[string]int) []droppedRow {
	old := make(map[string]droppedRow, len(record))
	for _, r := range record {
		old[r.XMage] = r
	}
	keys := make([]string, 0, len(measured))
	for k := range measured {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	rows := make([]droppedRow, 0, len(keys))
	for _, k := range keys {
		n := measured[k]
		row := droppedRow{XMage: k, Dropped: n}
		if o, ok := old[k]; ok && o.Dropped == n {
			row.Class, row.CR, row.Note = o.Class, o.CR, o.Note
		}
		rows = append(rows, row)
	}
	return rows
}

// loadDroppedRecord reads the record; an absent file is the empty record
// (a fresh checkout of a work tree without one, or -commit to a new dir).
func loadDroppedRecord(path string) ([]droppedRow, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []droppedRow
	for i, line := range strings.Split(string(raw), "\n") {
		if line == "" || strings.HasPrefix(line, "#") || line == droppedHeader {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 5 {
			return nil, fmt.Errorf("%s: line %d: want 5 tab-separated fields, got %d", path, i+1, len(f))
		}
		n, err := strconv.Atoi(f[1])
		if err != nil {
			return nil, fmt.Errorf("%s: line %d: bad dropped count %q: %v", path, i+1, f[1], err)
		}
		out = append(out, droppedRow{XMage: f[0], Dropped: n, Class: f[2], CR: f[3], Note: f[4]})
	}
	return out, nil
}

func writeDroppedRecord(path string, rows []droppedRow) error {
	sorted := make([]droppedRow, len(rows))
	copy(sorted, rows)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].XMage < sorted[j].XMage })
	var b strings.Builder
	b.WriteString(droppedHeader + "\n")
	squash := strings.NewReplacer("\t", " ", "\n", " ")
	for _, r := range sorted {
		fmt.Fprintf(&b, "%s\t%d\t%s\t%s\t%s\n", r.XMage, r.Dropped, r.Class, r.CR, squash.Replace(r.Note))
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// syncDroppedRecord rewrites the record from the scenarios committed under
// scenarioDir, preserving the triage of rows whose count did not change.
func syncDroppedRecord(scenarioDir, recordPath string) error {
	measured, err := droppedFromScenarios(scenarioDir)
	if err != nil {
		return err
	}
	record, err := loadDroppedRecord(recordPath)
	if err != nil {
		return err
	}
	return writeDroppedRecord(recordPath, mergeDropped(record, measured))
}
