package oraclediff

// The N-way extension of the three-way patterns: gorge plus any number of
// followers, each named by one letter (F Forge, X XMage, P phase.rs). It is
// pure, like ThreeWay: the caller supplies which pairs agree, and gets back
// the agreement classes, their pattern string and a label. ThreeWay and its
// patterns are untouched; a caller that has a third follower reports this
// pattern beside the three-way one.

import (
	"sort"
	"strconv"
	"strings"
)

// The marks a follower that did not compare carries in an N-way pattern.
const (
	// MarkUnsupported is a follower that lacks a card the item names (its
	// preflight refused it, or its database missed it at run time). It is an
	// explicit "unsupported", never a disagreement.
	MarkUnsupported = "?"
	// MarkUncompared is a follower that did not compare for any other reason
	// (a harness row, not sent, a turn cap, or it stopped before the point
	// the others were decided at).
	MarkUncompared = "–"
)

// The N-way labels. They read the agreement classes by engine, not by
// family: Forge and Manabrew are not both letters here.
const (
	// NWayGorgeSuspect: gorge alone, and at least two followers agree with
	// each other against it.
	NWayGorgeSuspect = "gorge_suspect"
	// NWayAllDiffer: gorge alone, and no two followers agree.
	NWayAllDiffer = "all_differ"
	// NWaySplit: gorge has company, and more than one follower stands
	// against it (in one class or several).
	NWaySplit = "split"
	// A single follower alone against gorge and everyone else that compared
	// is "<name>_suspect" (NWaySuspect).
)

// AgreeN is the pattern when every engine compared and all agree: "AGREE"
// plus the engine count ("AGREE4" for gorge, Forge, XMage and phase.rs).
func AgreeN(n int) string { return "AGREE" + strconv.Itoa(n) }

// NWayVerdict is one item's N-way classification.
type NWayVerdict struct {
	// Pattern lists the agreement classes of the engines that compared,
	// largest first (a tie puts gorge's class first, then by first letter),
	// each with gorge first and the followers in letter order, joined by "|";
	// then, space-separated, each follower that did not compare with its
	// mark ("FPX|G", "GP|FX", "FX|G P?", "GFX|P", "AGREE4"). With exactly
	// gorge, Forge and XMage compared it spells the three-way patterns' class
	// strings ("FX|G", "GF|X", "G|F|X"); only all-agree differs ("GFX", as
	// AGREE3 is ThreeWay's word).
	Pattern string
	// Label is the pattern's label: NWayGorgeSuspect, NWayAllDiffer,
	// NWaySplit, NWaySuspect(name), or "" (all compared agree, or fewer than
	// two engines compared).
	Label string
	// Classes are the agreement classes, in pattern order.
	Classes [][]string
	// Marks are the followers that did not compare, by letter.
	Marks map[string]string
	// Inconsistent: the pairwise answers are not transitive (a = b, b = c,
	// a != c). Pattern is then "INCONSISTENT" and nothing else is set.
	Inconsistent bool
}

// NWaySuspect is the label of a single follower standing alone against
// gorge and every other engine that compared.
func NWaySuspect(name string) string { return name + "_suspect" }

// NWay classifies one item. letters are the followers in any order ("G" is
// gorge and is implied). marks holds, for each follower that did not compare,
// MarkUnsupported or MarkUncompared; every other follower compared. eq
// reports whether two compared engines agree and whether that pair was
// compared at all (known); NWay asks only pairs of compared engines, and a
// pair that is not known neither joins nor splits them. names maps a letter
// to the engine name its suspect label carries.
func NWay(letters []string, marks map[string]string, eq func(a, b string) (agree, known bool), names map[string]string) NWayVerdict {
	all := []string{"G"}
	for _, l := range letters {
		if _, out := marks[l]; !out && l != "G" {
			all = append(all, l)
		}
	}
	// union-find over the compared engines
	parent := map[string]string{}
	for _, l := range all {
		parent[l] = l
	}
	var find func(string) string
	find = func(a string) string {
		if parent[a] != a {
			parent[a] = find(parent[a])
		}
		return parent[a]
	}
	type pair struct{ a, b string }
	var differ []pair
	for i := range all {
		for j := i + 1; j < len(all); j++ {
			agree, known := eq(all[i], all[j])
			if !known {
				continue
			}
			if agree {
				parent[find(all[i])] = find(all[j])
			} else {
				differ = append(differ, pair{all[i], all[j]})
			}
		}
	}
	for _, d := range differ {
		if find(d.a) == find(d.b) {
			return NWayVerdict{Pattern: string(Inconsistent), Inconsistent: true}
		}
	}
	byRoot := map[string][]string{}
	for _, l := range all {
		r := find(l)
		byRoot[r] = append(byRoot[r], l)
	}
	classes := make([][]string, 0, len(byRoot))
	for _, c := range byRoot {
		sort.Slice(c, func(i, j int) bool { return letterLess(c[i], c[j]) })
		classes = append(classes, c)
	}
	sort.Slice(classes, func(i, j int) bool {
		if len(classes[i]) != len(classes[j]) {
			return len(classes[i]) > len(classes[j])
		}
		return letterLess(classes[i][0], classes[j][0])
	})
	v := NWayVerdict{Classes: classes, Marks: map[string]string{}}
	for l, m := range marks {
		if l != "G" {
			v.Marks[l] = m
		}
	}
	parts := make([]string, len(classes))
	for i, c := range classes {
		parts[i] = strings.Join(c, "")
	}
	if len(classes) == 1 && len(v.Marks) == 0 {
		v.Pattern = AgreeN(len(all))
	} else {
		v.Pattern = strings.Join(parts, "|")
	}
	ms := make([]string, 0, len(v.Marks))
	for l := range v.Marks {
		ms = append(ms, l)
	}
	sort.Strings(ms)
	for _, l := range ms {
		v.Pattern += " " + l + v.Marks[l]
	}
	v.Label = nwayLabel(classes, names)
	return v
}

// letterLess orders engine letters: gorge first, then alphabetically.
func letterLess(a, b string) bool {
	if a == "G" || b == "G" {
		return a == "G" && b != "G"
	}
	return a < b
}

func nwayLabel(classes [][]string, names map[string]string) string {
	if len(classes) < 2 {
		return ""
	}
	var gc []string
	var others [][]string
	for _, c := range classes {
		if c[0] == "G" {
			gc = c
		} else {
			others = append(others, c)
		}
	}
	if len(gc) == 1 {
		for _, c := range others {
			if len(c) >= 2 {
				return NWayGorgeSuspect
			}
		}
		return NWayAllDiffer
	}
	if len(others) == 1 && len(others[0]) == 1 {
		l := others[0][0]
		if n := names[l]; n != "" {
			return NWaySuspect(n)
		}
		return NWaySuspect(l)
	}
	return NWaySplit
}

// The phase.rs vote on one item: where P stands relative to gorge.
const (
	PhaseWithGorge   = "with_gorge"  // P is in gorge's class
	PhaseWithOthers  = "with_others" // P shares a class with another follower, not with gorge
	PhaseAlone       = "alone"       // P differs from every engine that compared
	PhaseUnsupported = "unsupported" // P lacks a card the item names
	PhaseUncompared  = "uncompared"  // P did not compare for another reason
)

// Vote is where follower letter l stands in v (one of the Phase* words).
func (v NWayVerdict) Vote(l string) string {
	if m, ok := v.Marks[l]; ok {
		if m == MarkUnsupported {
			return PhaseUnsupported
		}
		return PhaseUncompared
	}
	for _, c := range v.Classes {
		for _, x := range c {
			if x != l {
				continue
			}
			switch {
			case c[0] == "G":
				return PhaseWithGorge
			case len(c) > 1:
				return PhaseWithOthers
			default:
				return PhaseAlone
			}
		}
	}
	return PhaseUncompared
}
