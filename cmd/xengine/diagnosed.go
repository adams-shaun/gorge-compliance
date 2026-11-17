package main

import (
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// diagnosedHeader is the exact column order the register and the farm's
// report.py share: one row per diagnosed (kind, set, card, field) blame
// verdict.
const diagnosedHeader = "kind\tset\tcard\tfield\twrong\tcr\tevidence\tupstream\tticket"

var (
	diagnosedKinds = []string{"FX|G", "G|F|X", "GF|X", "GX|F", "G|F|–", "diverge"}
	diagnosedWrong = []string{"X", "F", "G", "harness"}
	diagnosedCR    = regexp.MustCompile(`^\d{3}(\.\d+[a-z]?)?$`)
)

func runDiagnosed(args []string) error {
	fl := flag.NewFlagSet("diagnosed", flag.ExitOnError)
	check := fl.Bool("check", false, "validate every row of the register")
	file := fl.String("file", "oracle/xengine/diagnosed.tsv", "register TSV")
	cards := fl.String("cards", ".cards", "corpus dir")
	fl.Parse(args)
	if !*check {
		return fmt.Errorf("diagnosed needs -check")
	}
	bad, err := checkDiagnosed(*file, *cards, os.Stdout)
	if err != nil {
		return err
	}
	if bad > 0 {
		return fmt.Errorf("%s: %d invalid row(s)", *file, bad)
	}
	return nil
}

// checkDiagnosed validates file against the register rules, printing one
// "<file>:<row>: <reason>" line per error to w. It returns the number of bad
// rows; a read or corpus error is returned separately.
func checkDiagnosed(file, cardsDir string, w io.Writer) (int, error) {
	folder := filepath.Join(cardsDir, "cardsfolder")
	names, err := diagnosedCardNames(folder)
	if err != nil {
		return 0, err
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return 0, err
	}
	bad := 0
	report := func(row int, format string, args ...any) {
		fmt.Fprintf(w, "%s:%d: %s\n", file, row, fmt.Sprintf(format, args...))
		bad++
	}

	lines := strings.Split(string(b), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) == "" {
		report(1, "header must be %q", diagnosedHeader)
		return bad, nil
	}
	if got := strings.TrimSuffix(lines[0], "\r"); got != diagnosedHeader {
		report(1, "header must be %q", diagnosedHeader)
	}

	seen := map[string]bool{}
	for i, line := range lines[1:] {
		row := i + 2
		line = strings.TrimSuffix(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) != 9 {
			report(row, "want 9 columns, got %d", len(cols))
			continue
		}
		kind, set, card, field := strings.TrimSpace(cols[0]), strings.TrimSpace(cols[1]), strings.TrimSpace(cols[2]), strings.TrimSpace(cols[3])
		wrong, cr := strings.TrimSpace(cols[4]), strings.TrimSpace(cols[5])
		upstream, ticket := strings.TrimSpace(cols[7]), strings.TrimSpace(cols[8])

		key := strings.Join([]string{kind, set, card, field}, "\x00")
		if seen[key] {
			report(row, "duplicate (kind, set, card, field)")
		}
		seen[key] = true

		if !contains(diagnosedKinds, kind) {
			report(row, "unknown kind %q (want %s)", kind, strings.Join(diagnosedKinds, ", "))
		}
		if !contains(diagnosedWrong, wrong) {
			report(row, "unknown wrong %q (want %s)", wrong, strings.Join(diagnosedWrong, ", "))
		}
		if !diagnosedCR.MatchString(cr) {
			report(row, "bad cr %q (want NNN or NNN.NNx)", cr)
		}
		if !names[card] {
			report(row, "unknown card %q (no Name: under %s)", card, folder)
		}
		if upstream == "" && ticket == "" {
			report(row, "needs an upstream URL or a ticket id")
		}
	}
	return bad, nil
}

// diagnosedCardNames walks cardsfolder once and returns every card's Name:
// value, so a register row names a card the corpus actually has.
func diagnosedCardNames(folder string) (map[string]bool, error) {
	names := map[string]bool{}
	err := filepath.WalkDir(folder, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.EqualFold(filepath.Ext(path), ".txt") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, line := range strings.Split(string(b), "\n") {
			if name, ok := strings.CutPrefix(strings.TrimSuffix(line, "\r"), "Name:"); ok {
				names[strings.TrimSpace(name)] = true
				break
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("cards: %w", err)
	}
	return names, nil
}

func contains(all []string, s string) bool {
	for _, v := range all {
		if v == s {
			return true
		}
	}
	return false
}
