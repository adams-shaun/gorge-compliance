package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclediff"
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
	cardFolder := filepath.Join(cardsDir, "cardsfolder")
	tokenFolder := filepath.Join(cardsDir, "tokenscripts")
	names, err := diagnosedCardNames(cardsDir)
	if err != nil {
		return 0, err
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return 0, err
	}
	badRows := map[int]bool{}
	report := func(row int, format string, args ...any) {
		fmt.Fprintf(w, "%s:%d: %s\n", file, row, fmt.Sprintf(format, args...))
		badRows[row] = true
	}

	lines := strings.Split(string(b), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) == "" {
		report(1, "header must be %q", diagnosedHeader)
		return len(badRows), nil
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
			report(row, "unknown card %q (no Name: under %s or %s)", card, cardFolder, tokenFolder)
		}
		if upstream == "" && ticket == "" {
			report(row, "needs an upstream URL or a ticket id")
		}
	}
	return len(badRows), nil
}

// diagnosedCardNames indexes the names a register row may use. A row normally
// names a card (cardsfolder, including alternate faces). It may also name a
// token object with the farm's "token:" spelling ("token:Blood Token"): the
// farm attributes a primary difference to the object its last step named, and
// that object is a token when the step activated or attacked with one. Both a
// token script's own name and a copied card's name can be that object, so every
// indexed name gets the token spelling too.
//
// It also accepts the engine names of sourceless triggered abilities (the CR
// 702.179 speed trigger and the CR 725.2 monarch trigger): the farm's
// report.py can attribute such a stack row to the designation the follower
// prints, and no card carries that name.
func diagnosedCardNames(cardsDir string) (map[string]bool, error) {
	cards, err := diagnosedNames(filepath.Join(cardsDir, "cardsfolder"), false)
	if err != nil {
		return nil, err
	}
	tokens, err := diagnosedNames(filepath.Join(cardsDir, "tokenscripts"), true)
	if err != nil {
		return nil, err
	}
	names := map[string]bool{}
	for name := range cards {
		names[name] = true
		names["token:"+name] = true
	}
	for name := range tokens {
		names["token:"+name] = true
	}
	for _, name := range oraclediff.SourcelessTriggerNames() {
		names[name] = true
	}
	return names, nil
}

// diagnosedNames indexes the Name: values under folder, including alternate
// faces. A missing folder is empty, not an error, only when optional is set:
// a corpus may legitimately have no tokenscripts directory. A missing
// cardsfolder is always an error, so a wrong -cards path stays a corpus error
// instead of turning every row into an "unknown card".
func diagnosedNames(folder string, optional bool) (map[string]bool, error) {
	names := map[string]bool{}
	if _, err := os.Stat(folder); err != nil {
		if optional && errors.Is(err, fs.ErrNotExist) {
			return names, nil
		}
		return nil, fmt.Errorf("cards: %w", err)
	}
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
