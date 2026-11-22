package main

// oracle-audit compares, card by card, the Oracle line gorge actually compiles
// (cards.Face.Oracle, read from the corpus's Forge scripts) with the current
// Oracle text (the deckdb cards.oracle_text export, one TSV row per card:
// name \t sha256 \t text). A script that lags an erratum shows as diff. It also
// checks every hand scenario against the current text and reports the stale
// ones: a scenario is stale when its recorded oracle_sha no longer matches the
// corpus script's text, or when the card's script lags the current Oracle text
// (verdict diff), so a re-derived text never silently re-judges scenarios
// written against text nobody read.
//
//	oraclediff oracle-audit [-cards .cards] -oracle <tsv> -out <tsv> [-scenarios scenarios/oracle] [-stale <tsv>]
//
// Both sides are normalised the same way before comparison: reminder text
// (parenthesised) stripped, the Forge placeholders CARDNAME and ~ mapped to
// the card's name, quote characters folded to ASCII, whitespace folded, and a
// multi-faced card's faces joined in order. Verdicts: ok (equal), diff
// (unequal), missing (the export has no row for the card), empty (the export
// row carries no text although the script does: no data, not errata lag). Output carries
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

// oracleExport indexes the export two ways. full is keyed by the exact
// normalised full name ("a // b", every face kept), so a single-faced card
// never shares a key with a same-front-face multi-faced row. short is keyed by
// cards.NormalizeName (front face only); it is the fallback for names the
// export spells differently, and maps to the key of the row that won it.
type oracleExport struct {
	full  map[string]oracleExportRow
	short map[string]string
}

// fullKey is the exact-name key: lower-cased, whitespace folded, the " // "
// separator kept.
func fullKey(name string) string {
	return strings.Join(strings.Fields(strings.ToLower(name)), " ")
}

// better reports whether row a beats row b for one short key: a row with text
// beats an empty one (an "X // X" row with text "\n" must never hide the real
// X), and between two equally empty or non-empty rows the smaller full key
// wins, so the choice never depends on the file's row order.
func (e *oracleExport) better(a, b string) bool {
	ae := strings.TrimSpace(normaliseOracleText(e.full[a].text, "")) == ""
	be := strings.TrimSpace(normaliseOracleText(e.full[b].text, "")) == ""
	if ae != be {
		return !ae
	}
	return a < b
}

// loadOracleExport reads the exported TSV (name \t sha256 \t normalised
// text; the text carries no raw newlines).
func loadOracleExport(path string) (*oracleExport, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	ex := &oracleExport{full: map[string]oracleExportRow{}, short: map[string]string{}}
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
		fk := fullKey(parts[0])
		if old, dup := ex.full[fk]; dup && old.text != parts[2] {
			// The same exact name twice: keep the longer text, ties by
			// text order, independent of file order.
			if len(old.text) > len(parts[2]) || (len(old.text) == len(parts[2]) && old.text < parts[2]) {
				continue
			}
		}
		ex.full[fk] = oracleExportRow{name: parts[0], sha: parts[1], text: parts[2]}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	for fk := range ex.full {
		sk := cards.NormalizeName(ex.full[fk].name)
		if cur, ok := ex.short[sk]; !ok || ex.better(fk, cur) {
			ex.short[sk] = fk
		}
	}
	return ex, nil
}

// find returns the export row for a card, and its full key. Exact full names
// are tried before the front-face fallback, so X never resolves to X // X.
func (e *oracleExport) find(keys []string) (oracleExportRow, string, bool) {
	for _, k := range keys {
		if row, ok := e.full[fullKey(k)]; ok {
			return row, fullKey(k), true
		}
	}
	for _, k := range keys {
		if fk, ok := e.short[cards.NormalizeName(k)]; ok {
			return e.full[fk], fk, true
		}
	}
	return oracleExportRow{}, "", false
}

// selfRefRe matches Scryfall's 2025 self-reference wording.
var selfRefRe = regexp.MustCompile(`(?i)\bthis (creature|card|land|artifact|enchantment|permanent|spell|planeswalker|token|vehicle|saga|equipment|aura|battle|class|siege|case|attraction|object)\b`)

// foldSelfRefs maps every self-reference to one token: each face name, its
// part before a comma ("Zurgo Bellstriker" -> "Zurgo"), and "this <type>".
// Longest names first so a full name is not eaten by its own short form.
func foldSelfRefs(text string, names []string) string {
	var pats []string
	seen := map[string]bool{}
	for _, n := range names {
		short := strings.SplitN(n, ",", 2)[0]
		if short == n {
			short = strings.Fields(n + " ")[0] // "Zurgo Bellstriker" -> "Zurgo"
		}
		for _, v := range []string{n, short} {
			if len(v) >= 4 && !seen[v] {
				seen[v] = true
				pats = append(pats, v)
			}
		}
	}
	sort.Slice(pats, func(i, j int) bool {
		if len(pats[i]) != len(pats[j]) {
			return len(pats[i]) > len(pats[j])
		}
		return pats[i] < pats[j]
	})
	for _, p := range pats {
		text = strings.ReplaceAll(text, p, "SELFREF")
	}
	return strings.Join(strings.Fields(selfRefRe.ReplaceAllString(text, "SELFREF")), " ")
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
// oracle_sha. Verdict is ok, diff, missing or empty.
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
	var outFile *os.File
	if outPath != "-" {
		f, err := os.Create(outPath)
		if err != nil {
			return err
		}
		outFile = f
		w = bufio.NewWriter(f)
	}
	// Summary lines go to stderr when the TSV itself is on stdout.
	var sum io.Writer = os.Stdout
	if outPath == "-" {
		sum = os.Stderr
	}

	names := oracleAuditNames(reg)
	rows := make([]oracleAuditRow, 0, len(names))
	nOK, nDiff, nMissing, nEmpty, nDiffFolded := 0, 0, 0, 0, 0
	matched := map[string]bool{}
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
		if exp, fk, ok := export.find(lookup); ok {
			var scriptText, oracleText string
			row.scriptSHA, scriptText = scriptDigest(c)
			row.oracleSHA, oracleText = exportDigest(exp, name)
			switch {
			case row.scriptSHA == row.oracleSHA:
				row.verdict = "ok"
				nOK++
			case strings.TrimSpace(oracleText) == "":
				row.verdict = "empty"
				nEmpty++
			default:
				row.verdict = "diff"
				nDiff++
				names := make([]string, 0, len(c.Faces))
				for _, f := range c.Faces {
					names = append(names, f.Name)
				}
				if foldSelfRefs(scriptText, names) != foldSelfRefs(oracleText, names) {
					nDiffFolded++
				}
			}
			matched[fk] = true
		}
		if row.verdict == "missing" {
			row.scriptSHA, _ = scriptDigest(c)
			nMissing++
		}
		rows = append(rows, row)
	}
	nOrphan := 0
	for key := range export.full {
		if !matched[key] {
			nOrphan++
		}
	}
	verdicts := make(map[string]oracleAuditRow, len(rows))

	for _, row := range rows {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", row.card, row.verdict, row.scriptSHA, row.oracleSHA)
		verdicts[row.card] = row
	}
	if bf, isBuf := w.(*bufio.Writer); isBuf {
		if err := bf.Flush(); err != nil {
			return err
		}
	}
	if outFile != nil {
		if err := outFile.Close(); err != nil {
			return err
		}
	}

	fmt.Fprintf(sum, "oracle-audit: %d cards: %d ok, %d diff (%d still differ once self-references are folded), %d missing, %d empty, %d export rows no corpus card matched\n",
		len(rows), nOK, nDiff, nDiffFolded, nMissing, nEmpty, nOrphan)
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
		fmt.Fprint(sum, b.String())
	}

	if scenariosDir == "" {
		return nil
	}
	stale, checked, err := staleOracleScenarios(reg, scenariosDir, verdicts)
	if err != nil {
		return err
	}
	if stalePath != "" {
		f, err := os.Create(stalePath)
		if err != nil {
			return err
		}
		bw := bufio.NewWriter(f)
		for _, s := range stale {
			fmt.Fprintf(bw, "%s\t%s\t%s\t%s\t%s\n", s.card, s.file, s.reason, s.recorded, s.current)
		}
		if err := bw.Flush(); err != nil {
			f.Close()
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
	}
	fmt.Fprintf(sum, "oracle-audit: %d scenario files checked, %d stale\n", checked, len(stale))
	for _, s := range stale {
		fmt.Fprintf(sum, "stale %s (%s): %s: recorded %s, now %s: re-derive the scenarios\n", s.card, s.file, s.reason, s.recorded, s.current)
	}
	return nil
}

// staleScenario is one stale hand-scenario file.
type staleScenario struct {
	card     string
	file     string
	reason   string // "script" (oracle_sha != corpus digest) or "export" (script lags the current text)
	recorded string
	current  string
}

// scenarioOracleHead is the part of a scenario file the staleness check reads.
type scenarioOracleHead struct {
	Card      string `json:"card"`
	OracleSHA string `json:"oracle_sha"`
}

// staleOracleScenarios returns, sorted by file, the scenario files written
// against text that is no longer current, and how many files it read. A file
// is stale for one of two reasons:
//
//   - reason "script": its oracle_sha differs from the digest of the card's
//     corpus text (oracletext.Digest, the ratchet TestOracleAudit applies);
//   - reason "export": the sha still matches the corpus script, but the audit
//     verdict for the card is diff, i.e. the script itself lags the current
//     Oracle text, so the scenario was authored against outdated text.
//     recorded is the scenario's oracle_sha, current the export digest.
//
// verdicts is the audit row per primary card name.
func staleOracleScenarios(reg *cards.Registry, dir string, verdicts map[string]oracleAuditRow) ([]staleScenario, int, error) {
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
			stale = append(stale, staleScenario{card: head.Card, file: path, reason: "script", recorded: head.OracleSHA, current: current})
		} else if v, ok := verdicts[c.Faces[0].Name]; ok && v.verdict == "diff" {
			stale = append(stale, staleScenario{card: head.Card, file: path, reason: "export", recorded: head.OracleSHA, current: v.oracleSHA})
		}
		return nil
	})
	sort.Slice(stale, func(i, j int) bool { return stale[i].file < stale[j].file })
	return stale, checked, err
}
