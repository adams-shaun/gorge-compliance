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
	"sort"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclediff"
)

const (
	// diagnosedHeader is the exact column order every register shard and the
	// farm's report.py share: one row per diagnosed (kind, set, card, field)
	// blame verdict.
	diagnosedHeader = "kind\tset\tcard\tfield\twrong\tcr\tevidence\tupstream\tticket"

	// diagnosedMaxBytes is the repository's committed-file cap (1 MiB). The
	// register is split across shards so every file stays under it; -check
	// enforces the cap, including on the active shard the farm appends to.
	diagnosedMaxBytes = 1 << 20

	// diagnosedShardTarget is the soft size -split packs shards to. It leaves
	// the active shard, which report.py grows, at least half its cap in
	// headroom before the next split.
	diagnosedShardTarget = diagnosedMaxBytes / 2

	// diagnosedActiveName is the shard the farm's report.py appends to and the
	// last shard in register order. The other shards are shard-NNN.tsv.
	diagnosedActiveName = "active.tsv"
)

var (
	diagnosedKinds = []string{"FX|G", "G|F|X", "GF|X", "GX|F", "G|F|–", "diverge"}
	diagnosedWrong = []string{"X", "F", "G", "harness"}
	diagnosedCR    = regexp.MustCompile(`^\d{3}(\.\d+[a-z]?)?$`)
)

func runDiagnosed(args []string) error {
	fl := flag.NewFlagSet("diagnosed", flag.ExitOnError)
	check := fl.Bool("check", false, "validate every row of every shard")
	split := fl.Bool("split", false, "repack the shard directory to the 1 MiB cap")
	file := fl.String("file", "oracle/xengine/diagnosed", "register shard directory, or one TSV")
	cards := fl.String("cards", ".cards", "corpus dir")
	fl.Parse(args)
	switch {
	case *check && *split:
		return fmt.Errorf("diagnosed: -check and -split are exclusive")
	case *split:
		return splitDiagnosed(*file, diagnosedShardTarget)
	case !*check:
		return fmt.Errorf("diagnosed needs -check or -split")
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
// rows; a read or corpus error is returned separately. file is either one TSV
// or a shard directory; every shard must have the header, every shard must fit
// the committed-file cap, and the duplicate key check spans all shards.
func checkDiagnosed(file, cardsDir string, w io.Writer) (int, error) {
	cardFolder := filepath.Join(cardsDir, "cardsfolder")
	tokenFolder := filepath.Join(cardsDir, "tokenscripts")
	names, err := diagnosedCardNames(cardsDir)
	if err != nil {
		return 0, err
	}
	shards, err := diagnosedShards(file)
	if err != nil {
		return 0, err
	}
	badRows := map[string]bool{}
	report := func(shard string, row int, format string, args ...any) {
		fmt.Fprintf(w, "%s:%d: %s\n", shard, row, fmt.Sprintf(format, args...))
		badRows[fmt.Sprintf("%s\x00%d", shard, row)] = true
	}

	seen := map[string]string{}
	for _, shard := range shards {
		b, err := os.ReadFile(shard)
		if err != nil {
			return 0, err
		}
		if len(b) > diagnosedMaxBytes {
			report(shard, 1, "shard is %d bytes, over the %d-byte cap", len(b), diagnosedMaxBytes)
		}
		lines := strings.Split(string(b), "\n")
		if len(lines) == 0 || strings.TrimSpace(lines[0]) == "" {
			report(shard, 1, "header must be %q", diagnosedHeader)
			continue
		}
		if got := strings.TrimSuffix(lines[0], "\r"); got != diagnosedHeader {
			report(shard, 1, "header must be %q", diagnosedHeader)
		}

		for i, line := range lines[1:] {
			row := i + 2
			line = strings.TrimSuffix(line, "\r")
			if strings.TrimSpace(line) == "" {
				continue
			}
			cols := strings.Split(line, "\t")
			if len(cols) != 9 {
				report(shard, row, "want 9 columns, got %d", len(cols))
				continue
			}
			kind, set, card, field := strings.TrimSpace(cols[0]), strings.TrimSpace(cols[1]), strings.TrimSpace(cols[2]), strings.TrimSpace(cols[3])
			wrong, cr := strings.TrimSpace(cols[4]), strings.TrimSpace(cols[5])
			upstream, ticket := strings.TrimSpace(cols[7]), strings.TrimSpace(cols[8])

			key := strings.Join([]string{kind, set, card, field}, "\x00")
			if first, ok := seen[key]; ok {
				report(shard, row, "duplicate (kind, set, card, field) (first at %s)", first)
			} else {
				seen[key] = fmt.Sprintf("%s:%d", shard, row)
			}

			if !contains(diagnosedKinds, kind) {
				report(shard, row, "unknown kind %q (want %s)", kind, strings.Join(diagnosedKinds, ", "))
			}
			if !contains(diagnosedWrong, wrong) {
				report(shard, row, "unknown wrong %q (want %s)", wrong, strings.Join(diagnosedWrong, ", "))
			}
			if !diagnosedCR.MatchString(cr) {
				report(shard, row, "bad cr %q (want NNN or NNN.NNx)", cr)
			}
			if !names[card] {
				report(shard, row, "unknown card %q (no Name: under %s or %s)", card, cardFolder, tokenFolder)
			}
			if upstream == "" && ticket == "" {
				report(shard, row, "needs an upstream URL or a ticket id")
			}
		}
	}
	return len(badRows), nil
}

// diagnosedShards lists the files that make up a register. A path that is one
// file is a one-shard register (the checker's fixtures); a directory is read
// in deterministic shard order, with active.tsv last because that is the shard
// the farm appends to and the one -split repacks last.
func diagnosedShards(file string) ([]string, error) {
	info, err := os.Stat(file)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return []string{file}, nil
	}
	entries, err := os.ReadDir(file)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".tsv") {
			continue
		}
		names = append(names, e.Name())
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("%s: no *.tsv shards", file)
	}
	sort.Slice(names, func(i, j int) bool {
		if (names[i] == diagnosedActiveName) != (names[j] == diagnosedActiveName) {
			return names[j] == diagnosedActiveName // active last
		}
		ni, iok := diagnosedShardIndex(names[i])
		nj, jok := diagnosedShardIndex(names[j])
		switch {
		case iok && jok:
			if ni != nj {
				return ni < nj
			}
		case iok != jok:
			return iok // numbered shards before an unrecognised TSV
		}
		return names[i] < names[j]
	})
	shards := make([]string, len(names))
	for i, name := range names {
		shards[i] = filepath.Join(file, name)
	}
	return shards, nil
}

// diagnosedShardIndex reads the NNN of a shard-NNN.tsv name. The checker sorts
// numbered shards numerically, not by the spelling of the width, so the layout
// keeps its order past shard-999.
func diagnosedShardIndex(name string) (int, bool) {
	if !strings.HasPrefix(name, "shard-") || !strings.HasSuffix(name, ".tsv") {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(name, "shard-"), ".tsv"))
	if err != nil {
		return 0, false
	}
	return n, true
}

// splitDiagnosed rewrites the shards of a register directory so each is at
// most target bytes, header included, preserving the register's row order.
// All but the last shard become shard-NNN.tsv; the last is always active.tsv,
// the farm's append target, so an append grows only that file until the next
// split. Every row must fit one shard under the hard cap, even if it is larger
// than target.
func splitDiagnosed(dir string, target int) error {
	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s: -split needs a shard directory", dir)
	}
	if target <= len(diagnosedHeader)+1 || target > diagnosedMaxBytes {
		return fmt.Errorf("split target %d is outside (%d, %d]", target, len(diagnosedHeader)+1, diagnosedMaxBytes)
	}
	shards, err := diagnosedShards(dir)
	if err != nil {
		return err
	}
	var rows [][]byte
	for _, shard := range shards {
		b, err := os.ReadFile(shard)
		if err != nil {
			return err
		}
		lines := strings.Split(string(b), "\n")
		if len(lines) == 0 || strings.TrimSuffix(lines[0], "\r") != diagnosedHeader {
			return fmt.Errorf("%s:1: header must be %q", shard, diagnosedHeader)
		}
		for _, line := range lines[1:] {
			line = strings.TrimSuffix(line, "\r")
			if strings.TrimSpace(line) == "" {
				continue
			}
			if len(line)+len(diagnosedHeader)+2 > diagnosedMaxBytes {
				return fmt.Errorf("%s: row is %d bytes; the header plus row cannot fit the %d-byte cap", shard, len(line), diagnosedMaxBytes)
			}
			rows = append(rows, []byte(line))
		}
	}

	var packed [][]byte
	cur := []byte(diagnosedHeader + "\n")
	for _, row := range rows {
		if len(cur) > len(diagnosedHeader)+1 && len(cur)+len(row)+1 > target {
			packed = append(packed, cur)
			cur = []byte(diagnosedHeader + "\n")
		}
		cur = append(cur, row...)
		cur = append(cur, '\n')
	}
	packed = append(packed, cur)

	want := map[string]bool{}
	for i, b := range packed {
		name := fmt.Sprintf("shard-%03d.tsv", i)
		if i == len(packed)-1 {
			name = diagnosedActiveName
		}
		want[name] = true
		path := filepath.Join(dir, name)
		if err := writeAtomic(path, b); err != nil {
			return err
		}
		// writeAtomic creates a 0600 temp file; register shards are ordinary
		// committed data, so give them the usual read mode.
		if err := os.Chmod(path, 0o644); err != nil {
			return err
		}
	}
	for _, shard := range shards {
		if want[filepath.Base(shard)] {
			continue
		}
		if err := os.Remove(shard); err != nil {
			return err
		}
	}
	return nil
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
