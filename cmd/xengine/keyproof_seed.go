package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/classify"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/cov"
	"github.com/adams-shaun/gorge/engine/cards"
)

// keyproofSeedColumns is carddb/schema/v1/keyproof.tsv's column order minus the
// legacy leading `card` column: the oracle id comes from the scope file. A
// `carddb put keyproof` reads the seed file's first line as its column names.
const keyproofSeedColumns = "oracle_id\tkey\tleg\tdesc\tstate\treason\trefs\tsource\tagree_items\tdiff_items\t" +
	"witness\tsig\tcite\tticket\tseats\tmultiplayer\tgorge_sha\tcorpus_pin\tscript_sha\txmage_ref\tforge_ref\t" +
	"measured\treplayed\treplayed_sha\twriter"

// seedLegs is the legs a seed row speaks for: the two legs a key can hold
// before any follower has been run (pf-keys-02 fills multiplayer, the proof
// loop overwrites state).
var seedLegs = []string{"xmage", "lockstep"}

// seedRow is one owed ledger row awaiting its first measurement.
type seedRow struct {
	oracleID, key, leg, desc, reason, scriptSHA string
}

// cells renders the row in keyproofSeedColumns' order: everything a seed cannot
// know yet is "-", the proof cells are zero, and the writer says seed.
func (r seedRow) cells(gorgeSHA, measured string) string {
	return strings.Join([]string{r.oracleID, r.key, r.leg, r.desc, "owed", r.reason, "-", "seed",
		"0", "0", "-", "-", "-", "-", "0", "false", gorgeSHA, "-", r.scriptSHA, "-", "-", measured, "-", "-", "seed"},
		"\t")
}

// seedKeyproof writes out/keyproof-seed.tsv: one owed row per key of every
// card of the scope file (tab-separated oracle_id<TAB>name, the output of
// `carddb sql "SELECT oracle_id, name FROM scope_cards ORDER BY 1"`; lines
// starting with oracle_id or # are skipped) and each of seedLegs, so a card
// with no ledger row is visible to `carddb need` as owed work. A name the
// registry does not have gets the two cast rows with reason not-in-corpus.
// `carddb put keyproof --source seed --if-absent` then adds the rows without
// touching a measured one.
func seedKeyproof(reg *cards.Registry, scopePath, cardsDir, measured, gorgeSHA, out string) error {
	if err := classify.RefuseRepoPath(out); err != nil {
		return err
	}
	b, err := os.ReadFile(scopePath)
	if err != nil {
		return err
	}
	// scriptSHA is kpTable's (the corpus dir, the same three path tries and the
	// same per-card cache the proof loop uses).
	tab := newKpTable(reg, keyproofArgs{Cards: cardsDir, GorgeSHA: gorgeSHA, Measured: measured})
	var rows []seedRow
	nCards, nKeys, nMiss := 0, 0, 0
	for i, ln := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		ln = strings.TrimSuffix(ln, "\r")
		if ln == "" || strings.HasPrefix(ln, "#") || strings.HasPrefix(ln, "oracle_id") {
			continue
		}
		oracleID, name, ok := strings.Cut(ln, "\t")
		if !ok || oracleID == "" || name == "" {
			return fmt.Errorf("%s line %d: want oracle_id<TAB>name, got %q", scopePath, i+1, ln)
		}
		nCards++
		c, hit := reg.Lookup(name)
		matched := name
		if !hit {
			// A split card's registry name is its front face's; NormalizeName
			// folds "Front // Back" the same way, but a scope line spelling the
			// whole printed name must still find its card.
			if front, _, cutOK := strings.Cut(name, " // "); cutOK {
				c, hit = reg.Lookup(front)
				matched = front
			}
		}
		if !hit {
			nMiss++
			nKeys++
			for _, leg := range seedLegs {
				rows = append(rows, seedRow{oracleID: oracleID, key: "cast", leg: leg, desc: "cast",
					reason: "not-in-corpus", scriptSHA: "-"})
			}
			continue
		}
		sha := tab.scriptSHA(matched)
		univ := cov.Universe(c)
		nKeys += len(univ)
		for _, k := range univ {
			for _, leg := range seedLegs {
				rows = append(rows, seedRow{oracleID: oracleID, key: k.ID, leg: leg, desc: dash(k.Desc), reason: "untried", scriptSHA: sha})
			}
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.oracleID != b.oracleID {
			return a.oracleID < b.oracleID
		}
		if a.key != b.key {
			return a.key < b.key
		}
		return a.leg < b.leg
	})
	var buf bytes.Buffer
	buf.WriteString(keyproofSeedColumns + "\n")
	for _, r := range rows {
		buf.WriteString(r.cells(gorgeSHA, measured) + "\n")
	}
	if err := writeAtomic(filepath.Join(out, "keyproof-seed.tsv"), buf.Bytes()); err != nil {
		return err
	}
	fmt.Printf("keyproof seed: %d cards, %d keys, %d rows, %d not in corpus\n", nCards, nKeys, len(rows), nMiss)
	return nil
}
