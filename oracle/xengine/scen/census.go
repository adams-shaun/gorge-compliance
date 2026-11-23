package scen

import (
	"fmt"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/certdeck"
	"github.com/adams-shaun/gorge/engine/cards"
)

// Row is one key of the census: what Generate does with it.
type Row struct {
	Card, Key, Family, Path, Reason string
}

// Census runs Generate over every key of every named card, in card then key
// order. A card the corpus does not load is skipped; a Generate error is a row
// with path "error".
func Census(reg *cards.Registry, b *certdeck.Builder, names []string, opt Options) []Row {
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	var rows []Row
	for _, n := range sorted {
		c, ok := reg.Lookup(n)
		if !ok || len(c.Faces) == 0 {
			continue
		}
		keys := Keys(c)
		sort.Strings(keys)
		for _, k := range keys {
			r := Row{Card: n, Key: k, Family: Family(k)}
			res, err := Generate(reg, b, n, k, opt)
			switch {
			case err != nil:
				r.Path, r.Reason = "error", strings.ReplaceAll(err.Error(), "\t", " ")
			default:
				r.Path, r.Reason = res.Path, res.Reason
			}
			rows = append(rows, r)
		}
	}
	return rows
}

// SummaryRow counts the keys with one path and reason.
type SummaryRow struct {
	Path, Reason string
	Keys         int
}

// Summarize groups rows by (path, reason), most keys first.
func Summarize(rows []Row) []SummaryRow {
	count := map[[2]string]int{}
	for _, r := range rows {
		count[[2]string{r.Path, r.Reason}]++
	}
	var out []SummaryRow
	for k, n := range count {
		out = append(out, SummaryRow{k[0], k[1], n})
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Keys != b.Keys {
			return a.Keys > b.Keys
		}
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		return a.Reason < b.Reason
	})
	return out
}

// ReachTSV is the census table: card, key, family, path, reason.
func ReachTSV(rows []Row) string {
	var sb strings.Builder
	sb.WriteString("card\tkey\tfamily\tpath\treason\n")
	for _, r := range rows {
		fmt.Fprintf(&sb, "%s\t%s\t%s\t%s\t%s\n", r.Card, r.Key, r.Family, r.Path, r.Reason)
	}
	return sb.String()
}

// SummaryTSV is the reach summary: path, reason, keys.
func SummaryTSV(rows []Row) string {
	var sb strings.Builder
	sb.WriteString("path\treason\tkeys\n")
	for _, s := range Summarize(rows) {
		fmt.Fprintf(&sb, "%s\t%s\t%d\n", s.Path, s.Reason, s.Keys)
	}
	return sb.String()
}
