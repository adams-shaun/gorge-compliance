// Package ptcounter canonicalises the two spellings a power/toughness counter
// takes in the compliance digests: gorge's snapshot vocabulary -- two
// sign-letter/number pairs, "P1P1" is +1/+1 and "M0M1" is -0/-1 (CR 122.1a;
// engine/state.CounterPTDelta is the engine's parser) -- and the printed
// "+N/+M" form the follower runners emit. A +X/+Y counter and a -X/-Y counter
// are the same marker apart from sign, so every such kind renders as one
// canonical P/M label: the sign letter carries the sign, a zero component is
// always "P0" ("+0" and "-0" are the same counter), and the digits are the
// magnitude. Any other counter kind -- CHARGE, LOYALTY, DEATHTOUCHED, a
// keyword counter -- reports ok=false, so the caller keeps its own fallback
// for the kinds this vocabulary does not cover.
package ptcounter

import (
	"regexp"
	"strconv"
	"strings"
)

// gorgeKind matches the engine's P/M vocabulary, mixed signs accepted
// ("P1M1" is +1/-1); printedKind matches the followers' "+N/+M".
var (
	gorgeKind   = regexp.MustCompile(`^([pm])(\d+)([pm])(\d+)$`)
	printedKind = regexp.MustCompile(`^([+-])(\d+)/([+-])(\d+)$`)
)

// Canonical reports the canonical P/M label of a power/toughness counter kind
// and whether kind is one at all. Case and interior spaces are ignored, as
// the engine's own counter-kind normalisation folds them.
func Canonical(kind string) (string, bool) {
	k := strings.ReplaceAll(strings.ToLower(kind), " ", "")
	var pow, tough pair
	if m := gorgeKind.FindStringSubmatch(k); m != nil {
		pow, tough = parse(m[1], m[2]), parse(m[3], m[4])
	} else if m := printedKind.FindStringSubmatch(k); m != nil {
		pow, tough = parse(m[1], m[2]), parse(m[3], m[4])
	} else {
		return kind, false
	}
	return pow.label() + tough.label(), true
}

// pair is one signed counter component: its magnitude and whether the sign
// (as written) was negative.
type pair struct {
	neg bool
	n   int
}

func parse(sign, digits string) pair {
	n, _ := strconv.Atoi(digits)
	return pair{neg: sign == "m" || sign == "-", n: n}
}

// label renders the component; a zero is "P0" whichever sign was written.
func (p pair) label() string {
	if p.n == 0 {
		return "P0"
	}
	if p.neg {
		return "M" + strconv.Itoa(p.n)
	}
	return "P" + strconv.Itoa(p.n)
}
