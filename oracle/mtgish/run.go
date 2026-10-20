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
	Rows  []Row
	// NoGorge and NoMtgish name the cards one side lacks; they are reported,
	// not compared.
	NoGorge, NoMtgish []string
}

// Run compares every named card present on both sides, in name order.
func Run(names []string, reg *cards.Registry, ms map[string]*Card) Result {
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
		p, rows := compareCard(n, gc, mc, idx, func(face string) bool { _, ok := ms[face]; return ok })
		res.Pairs = append(res.Pairs, p)
		res.Rows = append(res.Rows, rows...)
	}
	SortRows(res.Rows)
	return res
}

// TierStats counts, for one tier, the faces that agree, the faces with a
// disagreement, and the faces whose only differences are normalizer gaps.
type TierStats struct {
	Faces, Agree, Disagree, Gap int
}

// Stats tallies a result per tier, face by face. A face with both a gap
// and a disagreement at a tier counts as a disagreement.
func (r Result) Stats() map[Tier]TierStats {
	type key struct {
		card string
		tier Tier
	}
	dis, gap := map[key]bool{}, map[key]bool{}
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
				default:
					s.Agree++
				}
				out[t] = s
			}
		}
	}
	return out
}
