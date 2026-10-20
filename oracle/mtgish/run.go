package mtgish

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance"
	"github.com/adams-shaun/gorge/engine/cards"
)

// ManifestNames reads a set's committed card list (XMage-derived), sorted.
func ManifestNames(root, set string) ([]string, error) {
	b, err := os.ReadFile(filepath.Join(root, "oracle", "compliance", "manifests", compliance.ManifestFileName(set)))
	if err != nil {
		return nil, err
	}
	var m compliance.Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(m.Cards))
	for _, c := range m.Cards {
		out = append(out, c.Name)
	}
	sort.Strings(out)
	return out, nil
}

// Result is one comparison run over a list of cards.
type Result struct {
	Pairs []Pair
	// Rows are the live differences: what a ledger holds.
	Rows []Row
	// Excluded are the rows all of whose unmapped tokens are excluded
	// tokens (class ClassExcluded): reported apart, never ledgered, not
	// counted as gaps.
	Excluded []Row
	// NoGorge and NoMtgish name the cards one side lacks; they are reported,
	// not compared.
	NoGorge, NoMtgish []string
	// ExcludedCards are the cards a card exclusion kept out of the
	// comparison (both sides had them).
	ExcludedCards []string
}

// Run compares every named card present on both sides, in name order.
func Run(names []string, reg *cards.Registry, ms map[string]*Card) Result {
	return RunWith(names, reg, ms, Exclusions{})
}

// RunWith is Run with exclusions applied: excluded cards are not compared
// and rows whose unmapped tokens are all excluded move to Result.Excluded.
func RunWith(names []string, reg *cards.Registry, ms map[string]*Card, ex Exclusions) Result {
	var res Result
	idx := TypeIndexFor(reg)
	names = append([]string(nil), names...)
	sort.Strings(names)
	for _, n := range names {
		gc, gok := reg.Lookup(n)
		mc, mok := ms[n]
		switch {
		case !gok:
			res.NoGorge = append(res.NoGorge, n)
			continue
		case !mok:
			res.NoMtgish = append(res.NoMtgish, n)
			continue
		}
		if _, ok := ex.Card[n]; ok {
			res.ExcludedCards = append(res.ExcludedCards, n)
			continue
		}
		p, rows := Compare(n, gc, mc, idx)
		res.Pairs = append(res.Pairs, p)
		for _, r := range rows {
			if len(ex.Token) > 0 && ex.RowExcluded(r) {
				r.Class = ClassExcluded
				res.Excluded = append(res.Excluded, r)
				continue
			}
			res.Rows = append(res.Rows, r)
		}
	}
	SortRows(res.Rows)
	SortRows(res.Excluded)
	return res
}

// Subset narrows a result to the named cards (a set's manifest), without
// comparing again. A named card the result neither compared, excluded nor
// found without a gorge card counts as lacking an mtgish card.
func (r Result) Subset(names []string) Result {
	want := map[string]bool{}
	for _, n := range names {
		want[n] = true
	}
	var out Result
	faces := map[string]bool{}
	done := map[string]bool{}
	for _, p := range r.Pairs {
		if !want[p.Card] {
			continue
		}
		out.Pairs = append(out.Pairs, p)
		done[p.Card] = true
		for _, s := range append(append([]*Summary(nil), p.Gorge...), p.Mtgish...) {
			faces[rowCard(s)] = true
		}
		faces[p.Card] = true
	}
	for _, row := range r.Rows {
		if faces[row.Card] {
			out.Rows = append(out.Rows, row)
		}
	}
	for _, row := range r.Excluded {
		if faces[row.Card] {
			out.Excluded = append(out.Excluded, row)
		}
	}
	for _, n := range r.ExcludedCards {
		if want[n] {
			out.ExcludedCards = append(out.ExcludedCards, n)
			done[n] = true
		}
	}
	for _, n := range r.NoGorge {
		if want[n] {
			out.NoGorge = append(out.NoGorge, n)
			done[n] = true
		}
	}
	for _, n := range names {
		if !done[n] {
			out.NoMtgish = append(out.NoMtgish, n)
			done[n] = true
		}
	}
	sort.Strings(out.NoMtgish)
	return out
}

// TierStats counts, for one tier, the faces that agree, the faces with a
// disagreement, the faces whose only differences are normalizer gaps, and
// the faces whose only differences are excluded rows.
type TierStats struct {
	Faces, Agree, Disagree, Gap, Excluded int
}

// Stats tallies a result per tier, face by face. A face with both a gap
// and a disagreement at a tier counts as a disagreement; a face with an
// excluded row counts as excluded only when it has no other row there.
func (r Result) Stats() map[Tier]TierStats {
	type key struct {
		card string
		tier Tier
	}
	dis, gap, exc := map[key]bool{}, map[key]bool{}, map[key]bool{}
	for _, row := range r.Excluded {
		exc[key{row.Card, row.Tier}] = true
	}
	for _, row := range r.Rows {
		k := key{row.Card, row.Tier}
		if row.Gap() {
			gap[k] = true
		} else {
			dis[k] = true
		}
	}
	out := map[Tier]TierStats{}
	for _, p := range r.Pairs {
		for _, g := range p.Gorge {
			for _, t := range Tiers {
				k := key{rowCard(g), t}
				s := out[t]
				s.Faces++
				switch {
				case dis[k]:
					s.Disagree++
				case gap[k]:
					s.Gap++
				case exc[k]:
					s.Excluded++
				default:
					s.Agree++
				}
				out[t] = s
			}
		}
	}
	return out
}
