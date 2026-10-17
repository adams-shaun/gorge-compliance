package oraclediff

import (
	"sort"
	"strings"
)

// The hexproof-from opt-in (CompareKeywordsHexproofFrom). Section 2.3 of the
// level-B spec keeps "hexproof from <quality>" outside both keyword
// vocabularies because the two engines spell the quality differently. The
// colour qualities are the exception: XMage's driver emits one rule-text
// keyword per colour ("hexproof from green", HexproofFromGreenAbility and its
// siblings), and gorge's keyword line names the colour either directly
// ("Hexproof:Green", "Hexproof:Card.White:white") or as the permanent's own
// colours ("Hexproof:CardColors", Tam, Mindful First-Year's "hexproof from
// each of its colors"), which the fold expands against the permanent's
// colours, the field both engines already agree on. Only the five colour
// words fold; every other quality stays outside the comparison, so an item
// that opts in is compared on exactly the qualities both engines name alike.

// hexproofFromColours maps a colour letter to the word both engines use.
var hexproofFromColours = map[byte]string{'W': "white", 'U': "blue", 'B': "black", 'R': "red", 'G': "green"}

// hexproofFromWord reports whether w is one of the five colour words.
func hexproofFromWord(w string) bool {
	for _, c := range hexproofFromColours {
		if c == w {
			return true
		}
	}
	return false
}

// hexproofFromFolds returns the "hexproof from <colour>" entries a keyword
// carries, nil for any other keyword.
func hexproofFromFolds(kw, colors string) []string {
	f := foldKeyword(kw)
	if rest, ok := strings.CutPrefix(f, "hexproof from "); ok {
		if hexproofFromWord(rest) {
			return []string{f}
		}
		return nil
	}
	rest, ok := strings.CutPrefix(f, "hexproof:")
	if !ok {
		return nil
	}
	if rest == "cardcolors" {
		var out []string
		for i := 0; i < len(colors); i++ {
			if w, ok := hexproofFromColours[colors[i]]; ok {
				out = append(out, "hexproof from "+w)
			}
		}
		return out
	}
	q := rest
	if i := strings.LastIndexByte(rest, ':'); i >= 0 {
		q = rest[i+1:]
	}
	if hexproofFromWord(q) {
		return []string{"hexproof from " + q}
	}
	return nil
}

// ComparedKeywordsHexproofFrom is the opt-in's view of a permanent's
// keywords: the named vocabulary plus its colour hexproof-from entries,
// sorted and comma-joined, "" when none.
func ComparedKeywordsHexproofFrom(keywords []string, colors string) string {
	seen := map[string]bool{}
	if named := namedList(keywords); named != "" {
		for _, k := range strings.Split(named, ",") {
			seen[k] = true
		}
	}
	for _, kw := range keywords {
		for _, h := range hexproofFromFolds(kw, colors) {
			seen[h] = true
		}
	}
	if len(seen) == 0 {
		return ""
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}
