package main

// oracle-audit compares, card by card, the Oracle line gorge actually compiles
// (cards.Face.Oracle, read from the corpus's Forge scripts) with the current
// Oracle text (the deckdb cards.oracle_text export, one TSV row per card:
// name \t sha256 \t text). A script that lags an erratum shows as diff. It also
// checks every hand scenario's recorded oracle_sha against the corpus and
// reports the stale ones, so a re-derived text never silently re-judges
// scenarios written against text nobody read.
//
//	oraclediff oracle-audit [-cards .cards] -oracle <tsv> -out <tsv> [-scenarios scenarios/oracle] [-stale <tsv>]
//
// Both sides are normalised the same way before comparison: reminder text
// (parenthesised) stripped, the Forge placeholders CARDNAME and ~ mapped to
// the card's name, quote characters folded to ASCII, whitespace folded, and a
// multi-faced card's faces joined in order. Verdicts: ok (equal), diff
// (unequal), missing (the export has no row for the card). Output carries
// names, hashes and verdicts only -- never script or Oracle text (the GPL
// boundary); both digests are the first 12 hex digits of the sha256 of the
// normalised text.

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/cards/oracletext"
)

// reminder re matches one innermost parenthesised span, so the loop below
// also unwinds nesting.
var reminderRe = regexp.MustCompile(`\([^()]*\)`)

// normaliseOracleText strips reminder text, maps the placeholders CARDNAME and
// ~ to the card's name, folds quote characters to ASCII and folds whitespace,
// so a script's Oracle line and the exported current text compare by content,
// not by formatting. Idempotent.
func normaliseOracleText(text, name string) string {
	// deckdb joins a multi-faced card's face texts with "\n//\n" (deckdb.py
	// load_scryfall); Scryfall's own multi-face text separates them with a
	// bare line break. Drop the separator before the general folding, in
	// both its escaped and real-newline forms, so both sources compare.
	t := strings.ReplaceAll(text, `\n//\n`, "\n")
	t = strings.ReplaceAll(t, "\n//\n", "\n")
	t = strings.ReplaceAll(t, `\n`, " ")
	for {
		next := reminderRe.ReplaceAllString(t, " ")
		if next == t {
			break
		}
		t = next
	}
	t = strings.NewReplacer(
		"“", `"`, "”", `"`, "„", `"`, "‟", `"`,
		"‘", `'`, "’", `'`, "‚", `'`, "‛", `'`,
		"CARDNAME", name, "~", name,
	).Replace(t)
	return strings.Join(strings.Fields(t), " ")
}

// digest12 is the first 12 hex digits of the sha256 of s, the same length as
// the scenario oracle_sha convention (oracletext.Digest), over OUR normalised
// text.
func digest12(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:12]
}

// oracleExportRow is one deckdb cards.oracle_text export row.
type oracleExportRow struct {
	name string
	sha  string
	text string
}

// loadOracleExport reads the exported TSV (name \t sha256 \t normalised
// text; the text carries no raw newlines) keyed by normalised name.
func loadOracleExport(path string) (map[string]oracleExportRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	rows := map[string]oracleExportRow{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	line := 0
	for sc.Scan() {
		line++
		text := sc.Text()
		if text == "" {
			continue
		}
		parts := strings.SplitN(text, "\t", 3)
		if len(parts) != 3 || parts[0] == "" {
			return nil, fmt.Errorf("%s:%d: want name\tsha\ttext, got %d fields", path, line, len(parts))
		}
		rows[cards.NormalizeName(parts[0])] = oracleExportRow{name: parts[0], sha: parts[1], text: parts[2]}
	}
	return rows, sc.Err()
}

// scriptDigest is the digest of the corpus card's compiled Oracle text: every
// face's Oracle line normalised against that face's own name, faces joined in
// order.
func scriptDigest(c *cards.Card) (string, string) {
	parts := make([]string, 0, len(c.Faces))
	for _, f := range c.Faces {
		parts = append(parts, normaliseOracleText(oracletext.Text(f), f.Name))
	}
	joined := strings.Join(parts, " ")
	return digest12(joined), joined
}

// exportDigest is the digest of the exported current text. A multi-faced
// card's export is one text (the faces in order, separated by line breaks),
// so it is normalised against the card's primary name.
func exportDigest(row oracleExportRow, name string) (string, string) {
	joined := normaliseOracleText(row.text, name)
	return digest12(joined), joined
}

// oracleAuditRow is one output line: card \t verdict \t script_sha \t
// oracle_sha. Verdict is ok, diff or missing.
type oracleAuditRow struct {
	card      string
	verdict   string
	scriptSHA string
	oracleSHA string
}

// oracleAuditNames is the sorted primary-face names of every card in reg.
func oracleAuditNames(reg *cards.Registry) []string {
	names := make([]string, 0, len(reg.AllCards()))
	for _, c := range reg.AllCards() {
		names = append(names, c.Faces[0].Name)
	}
	sort.Strings(names)
	return names
}

// runOracleAudit writes one audit row per corpus card and reports the stale
// hand scenarios. stalePath empty skips the staleness file (still reported on
// stdout); scenariosDir empty skips the check entirely.
func runOracleAudit(dir, exportPath, outPath, scenariosDir, stalePath string) error {
	if exportPath == "" || outPath == "" {
		return errors.New("oracle-audit needs -oracle and -out")
	}
	reg, err := loadReg(dir)
	if err != nil {
		return err
	}
	export, err := loadOracleExport(exportPath)
	if err != nil {
		return err
	}

	var w io.Writer = os.Stdout
	if outPath != "-" {
		f, err := os.Create(outPath)
		if err != nil {
			return err
		}
		defer f.Close()
		w = bufio.NewWriter(f)
	}

	names := oracleAuditNames(reg)
	rows := make([]oracleAuditRow, 0, len(names))
	nOK, nDiff, nMissing := 0, 0, 0
	for _, name := range names {
		c, ok := reg.Lookup(name)
		if !ok {
			return fmt.Errorf("corpus card %q vanished from the registry", name)
		}
		row := oracleAuditRow{card: name, verdict: "missing", scriptSHA: ""}
		// The export may key a card by its full name (A // B) or by any
		// face's name; try all of them before calling it missing.
		lookup := []string{name}
		if len(c.Faces) > 1 {
			full := make([]string, 0, len(c.Faces))
			for _, f := range c.Faces {
				full = append(full, f.Name)
			}
			lookup = append(lookup, strings.Join(full, " // "))
			lookup = append(lookup, full...)
		}
		for _, key := range lookup {
			if exp, ok := export[cards.NormalizeName(key)]; ok {
				row.scriptSHA, _ = scriptDigest(c)
				row.oracleSHA, _ = exportDigest(exp, name)
				if row.scriptSHA == row.oracleSHA {
					row.verdict = "ok"
					nOK++
				} else {
					row.verdict = "diff"
					nDiff++
				}
				delete(export, cards.NormalizeName(key))
				break
			}
		}
		if row.verdict == "missing" {
			row.scriptSHA, _ = scriptDigest(c)
			nMissing++
		}
		rows = append(rows, row)
	}
	nOrphan := len(export)

	for _, row := range rows {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", row.card, row.verdict, row.scriptSHA, row.oracleSHA)
	}
	if bf, isBuf := w.(*bufio.Writer); isBuf {
		if err := bf.Flush(); err != nil {
			return err
		}
	}

	fmt.Printf("oracle-audit: %d cards: %d ok, %d diff, %d missing, %d export rows no corpus card matched\n",
		len(rows), nOK, nDiff, nMissing, nOrphan)
	if nDiff > 0 {
		var b strings.Builder
		shown := 0
		for _, row := range rows {
			if row.verdict != "diff" || shown == 20 {
				continue
			}
			fmt.Fprintf(&b, "diff %s (script %s, oracle %s)\n", row.card, row.scriptSHA, row.oracleSHA)
			shown++
		}
		if shown == 20 {
			b.WriteString("... (see the -out file for the rest)\n")
		}
		fmt.Print(b.String())
	}

	if scenariosDir == "" {
		return nil
	}
	stale, err := staleOracleScenarios(reg, scenariosDir)
	if err != nil {
		return err
	}
	if stalePath != "" {
		f, err := os.Create(stalePath)
		if err != nil {
			return err
		}
		defer f.Close()
		bw := bufio.NewWriter(f)
		for _, s := range stale {
			fmt.Fprintf(bw, "%s\t%s\t%s\t%s\n", s.card, s.file, s.recorded, s.current)
		}
		if err := bw.Flush(); err != nil {
			return err
		}
	}
	fmt.Printf("oracle-audit: %d scenario files checked, %d stale\n", scenarioFilesChecked, len(stale))
	for _, s := range stale {
		fmt.Printf("stale %s (%s): recorded %s, corpus %s: re-derive the scenarios\n", s.card, s.file, s.recorded, s.current)
	}
	return nil
}

// scenarioFilesChecked counts the scenario files the last staleOracleScenarios
// call read, for the summary line. Package state only because the count is
// reported next to the stale list it belongs to; nothing else reads it.
var scenarioFilesChecked int

// staleScenario is one stale hand-scenario file.
type staleScenario struct {
	card     string
	file     string
	recorded string
	current  string
}

// scenarioOracleHead is the part of a scenario file the staleness check reads.
type scenarioOracleHead struct {
	Card      string `json:"card"`
	OracleSHA string `json:"oracle_sha"`
}

// staleOracleScenarios compares every scenario file's oracle_sha against the
// digest of the card's current corpus text (oracletext.Digest, the same
// ratchet audit's TestOracleAudit applies) and returns the stale ones sorted
// by file.
func staleOracleScenarios(reg *cards.Registry, dir string) ([]staleScenario, error) {
	var stale []staleScenario
	checked := 0
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || filepath.Ext(path) != ".json" {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var head scenarioOracleHead
		if err := json.Unmarshal(raw, &head); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		checked++
		if head.OracleSHA == "" {
			return nil
		}
		c, ok := reg.Lookup(head.Card)
		if !ok {
			return fmt.Errorf("%s: card %q not in the corpus", path, head.Card)
		}
		if current := oracletext.Digest(c); head.OracleSHA != current {
			stale = append(stale, staleScenario{
				card:     head.Card,
				file:     path,
				recorded: head.OracleSHA,
				current:  current,
			})
		}
		return nil
	})
	sort.Slice(stale, func(i, j int) bool { return stale[i].file < stale[j].file })
	scenarioFilesChecked = checked
	return stale, err
}
