package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/cov"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/decks"
	"github.com/adams-shaun/gorge/engine/cards"
)

// poolFlagErr is the error of a -pool run whose other flags make no sense with
// it, or nil. -pool replaces the deck source: -random's cross-set union, its
// -forge-only extras and -dirac's per-set table draw (a dirac table holds one
// set's cards; a pool is not a set) all pick the cards another way. -set
// still names the run in the item ids, as under -random. -pool-filter only
// narrows a pool.
func poolFlagErr(pool, filter, random, forgeOnly string, dirac bool) error {
	if pool == "" {
		if filter != "" {
			return fmt.Errorf("-pool-filter needs -pool")
		}
		return nil
	}
	var bad []string
	if random != "" {
		bad = append(bad, "-random")
	}
	if forgeOnly != "" {
		bad = append(bad, "-forge-only")
	}
	if dirac {
		bad = append(bad, "-dirac")
	}
	if len(bad) > 0 {
		return fmt.Errorf("-pool is the whole deck source: it cannot be combined with %s", strings.Join(bad, ", "))
	}
	return nil
}

// parseNameList is the card names of a pool or filter file: one per line,
// trimmed; everything from a '#' on is a comment (no card name holds one) and
// blank lines are skipped. A name listed twice is kept once.
func parseNameList(b []byte) []string {
	var out []string
	seen := map[string]bool{}
	for _, l := range strings.Split(string(b), "\n") {
		if i := strings.IndexByte(l, '#'); i >= 0 {
			l = l[:i]
		}
		if l = strings.TrimSpace(l); l == "" || seen[l] {
			continue
		}
		seen[l] = true
		out = append(out, l)
	}
	return out
}

// listFile is a name list read from disk and the sha256 of its bytes.
type listFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Names  int    `json:"names"`
	names  []string
}

func readListFile(path string) (listFile, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return listFile{}, err
	}
	sum := sha256.Sum256(b)
	names := parseNameList(b)
	return listFile{Path: path, SHA256: hex.EncodeToString(sum[:]), Names: len(names), names: names}, nil
}

// poolInfo is DIR/pool.json of a -pool run: which pool and filter the decks
// came from, and how each narrowed it.
type poolInfo struct {
	Pool       listFile       `json:"pool"`
	Filter     *listFile      `json:"filter,omitempty"`
	Candidates int            `json:"candidates"`
	Skipped    map[string]int `json:"skipped,omitempty"` // Skip reason (its first word for "unsupported [...]") -> count
	// Filtered is how many candidates the filter dropped; FilterUnmatched the
	// filter's names the corpus does not know (a filter may be shared by pools,
	// so a name the pool lacks is not an error).
	Filtered        int      `json:"filtered,omitempty"`
	FilterUnmatched []string `json:"filter_unmatched,omitempty"`
}

// poolCandidates is the decks' candidates of a -pool run: the pool's names
// through the same gorge-supported filter as a set (decks.CandidatesNamed),
// minus the names the filter lists. With xmage, a card XMage spells other than
// the corpus (any manifest's spelling, decks.Spellings) is left out too, as
// -set -xmage leaves it out. A pool name the corpus does not know is an error
// naming every one: a pool is a committed list, and a misspelling must not
// shrink it in silence.
func poolCandidates(reg *cards.Registry, manifests, poolPath, filterPath string, xmage bool) ([]decks.Card, poolInfo, error) {
	pool, err := readListFile(poolPath)
	if err != nil {
		return nil, poolInfo{}, err
	}
	info := poolInfo{Pool: pool, Skipped: map[string]int{}}
	cands, skips := decks.CandidatesNamed(reg, pool.names, decks.Options{XMage: xmage})
	var unknown []string
	for _, s := range skips {
		if s.Reason == "not in corpus" {
			unknown = append(unknown, s.Card)
		}
		r, _, _ := strings.Cut(s.Reason, " ")
		info.Skipped[r]++
	}
	if len(unknown) > 0 {
		return nil, info, fmt.Errorf("-pool %s: %d name(s) not in the corpus (use the Forge spelling): %s", poolPath, len(unknown), strings.Join(unknown, "; "))
	}
	if xmage {
		sp, err := decks.LoadSpellings(manifests)
		if err != nil {
			return nil, info, err
		}
		kept := cands[:0:0]
		for _, c := range cands {
			if sp.Differs(reg, c.Name) {
				info.Skipped[decks.ReasonXMageSpelling]++
				continue
			}
			kept = append(kept, c)
		}
		cands = kept
	}
	if filterPath != "" {
		f, err := readListFile(filterPath)
		if err != nil {
			return nil, info, err
		}
		info.Filter = &f
		drop := map[string]bool{}
		has := func(n string) bool { _, ok := reg.Lookup(n); return ok }
		folded := compliance.FoldedNames(reg)
		for _, n := range f.names {
			name, ok := compliance.CorpusNameFold(has, folded, n)
			c, found := reg.Lookup(name)
			if !ok || !found || len(c.Faces) == 0 {
				info.FilterUnmatched = append(info.FilterUnmatched, n)
				continue
			}
			drop[c.Faces[0].Name] = true
		}
		kept := cands[:0:0]
		for _, c := range cands {
			if drop[c.Name] {
				info.Filtered++
				continue
			}
			kept = append(kept, c)
		}
		cands = kept
	}
	info.Candidates = len(cands)
	if err := decks.Pairable(cands); err != nil {
		return nil, info, fmt.Errorf("-pool %s: %w", poolPath, err)
	}
	return cands, info, nil
}

// poolPicker draws every game from the pool's candidates against the ledger,
// as setPicker does from a set's: -pool FILE over a set's manifest names deals
// what -set deals.
func poolPicker(cands []decks.Card, led *cov.Ledger) picker {
	return func(int, uint64) ([]decks.Card, *cov.Ledger, string) { return cands, led, "" }
}

// String is the one-line summary play prints.
func (p poolInfo) String() string {
	s := fmt.Sprintf("pool %s (sha256 %.12s): %d names, %d candidates", p.Pool.Path, p.Pool.SHA256, p.Pool.Names, p.Candidates)
	keys := make([]string, 0, len(p.Skipped))
	for k := range p.Skipped {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		s += fmt.Sprintf(", %d %s", p.Skipped[k], k)
	}
	if p.Filter != nil {
		s += fmt.Sprintf("; filter %s (sha256 %.12s) dropped %d", p.Filter.Path, p.Filter.SHA256, p.Filtered)
		if n := len(p.FilterUnmatched); n > 0 {
			s += fmt.Sprintf(", %d filter name(s) not in the corpus", n)
		}
	}
	return s
}

// runPool writes a pool file: the names -set SET draws from (the manifest's
// cards minus its unfinished ones, each in the corpus spelling of its front
// face, once, in manifest order). With -legal FORMAT and -mtgjson FILE (an
// MTGJSON set file, .json or .json.gz) only the cards MTGJSON marks Legal in
// FORMAT are kept. The header records the inputs, so a pool regenerates
// byte for byte (oracle/xengine/pools/README.md).
func runPool(args []string) error {
	fs := flag.NewFlagSet("pool", flag.ExitOnError)
	dir := fs.String("cards", ".cards", "corpus dir")
	manifests := fs.String("manifests", "oracle/compliance/manifests", "set manifests dir")
	set := fs.String("set", "", "set code (required)")
	legal := fs.String("legal", "", "with -mtgjson, keep only the cards legal in this MTGJSON format (e.g. pauper)")
	mtgjson := fs.String("mtgjson", "", "MTGJSON set file (<SET>.json or .json.gz) giving -legal")
	out := fs.String("out", "", "output file (default stdout)")
	fs.Parse(args)
	if *set == "" {
		return fmt.Errorf("pool needs -set")
	}
	if (*legal == "") != (*mtgjson == "") {
		return fmt.Errorf("pool: -legal and -mtgjson go together")
	}
	m, err := compliance.LoadManifest(*manifests, *set)
	if err != nil {
		return err
	}
	reg, err := loadReg(*dir)
	if err != nil {
		return err
	}
	var hdr bytes.Buffer
	fmt.Fprintf(&hdr, "# xengine pool: %s (%s) manifest, xmage_ref %s, minus unfinished; corpus spelling of each front face.\n", m.Code, m.Name, m.XMageRef)
	keep := func(string) bool { return true }
	if *mtgjson != "" {
		legalNames, ver, sum, err := mtgjsonLegal(*mtgjson, *legal)
		if err != nil {
			return err
		}
		keep = func(n string) bool { return legalNames[compliance.FoldName(n)] }
		fmt.Fprintf(&hdr, "# Kept: MTGJSON legalities.%s == Legal, from MTGJSON %s set file %s (version %s, sha256 %s).\n", *legal, *set, baseName(*mtgjson), ver, sum)
	}
	fmt.Fprintf(&hdr, "# Regenerate: go run ./cmd/xengine pool -set %s", *set)
	if *mtgjson != "" {
		fmt.Fprintf(&hdr, " -legal %s -mtgjson %s.json.gz", *legal, *set)
	}
	hdr.WriteString(" -out <this file>\n")
	body, n, missing := poolNames(reg, m, keep)
	fmt.Fprintf(&hdr, "# %d names.\n", n)
	for _, x := range missing {
		fmt.Fprintf(&hdr, "# not in corpus, left out: %s\n", x)
	}
	b := append(hdr.Bytes(), body...)
	if *out == "" {
		_, err = os.Stdout.Write(b)
		return err
	}
	return writeAtomic(*out, b)
}

func baseName(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}

// poolNames is the lines of m's pool: each kept card's corpus front-face name,
// once, in manifest order, the count, and the manifest names the corpus lacks.
func poolNames(reg *cards.Registry, m compliance.Manifest, keep func(string) bool) ([]byte, int, []string) {
	has := func(n string) bool { _, ok := reg.Lookup(n); return ok }
	folded := compliance.FoldedNames(reg)
	unfinished := map[string]bool{}
	for _, u := range m.Unfinished {
		unfinished[compliance.FoldName(u)] = true
	}
	var b bytes.Buffer
	var missing []string
	seen := map[string]bool{}
	n := 0
	for _, mc := range m.Cards {
		if unfinished[compliance.FoldName(mc.Name)] || !keep(mc.Name) {
			continue
		}
		name, ok := compliance.CorpusNameFold(has, folded, mc.Name)
		c, found := reg.Lookup(name)
		if !ok || !found || len(c.Faces) == 0 {
			missing = append(missing, mc.Name)
			continue
		}
		front := c.Faces[0].Name
		if seen[front] {
			continue
		}
		seen[front] = true
		b.WriteString(front + "\n")
		n++
	}
	return b.Bytes(), n, missing
}

// mtgjsonLegal reads an MTGJSON set file and returns the folded names (the
// card's name, each " // " part of it, and its faceName) of every card whose
// legalities[format] is "Legal", the file's MTGJSON version and its sha256.
func mtgjsonLegal(path, format string) (map[string]bool, string, string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, "", "", err
	}
	sum := sha256.Sum256(raw)
	var r io.Reader = bytes.NewReader(raw)
	if strings.HasSuffix(path, ".gz") {
		if r, err = gzip.NewReader(r); err != nil {
			return nil, "", "", err
		}
	}
	var doc struct {
		Meta struct {
			Version string `json:"version"`
		} `json:"meta"`
		Data struct {
			Cards []struct {
				Name       string            `json:"name"`
				FaceName   string            `json:"faceName"`
				Legalities map[string]string `json:"legalities"`
			} `json:"cards"`
		} `json:"data"`
	}
	if err := json.NewDecoder(r).Decode(&doc); err != nil {
		return nil, "", "", fmt.Errorf("%s: %w", path, err)
	}
	if len(doc.Data.Cards) == 0 {
		return nil, "", "", fmt.Errorf("%s: no cards (want an MTGJSON set file)", path)
	}
	out := map[string]bool{}
	for _, c := range doc.Data.Cards {
		if c.Legalities[format] != "Legal" {
			continue
		}
		out[compliance.FoldName(c.Name)] = true
		for _, part := range strings.Split(c.Name, " // ") {
			out[compliance.FoldName(part)] = true
		}
		if c.FaceName != "" {
			out[compliance.FoldName(c.FaceName)] = true
		}
	}
	return out, doc.Meta.Version, hex.EncodeToString(sum[:]), nil
}
