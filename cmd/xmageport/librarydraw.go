package main

import (
	"regexp"
	"strings"
)

// reDrawEffect finds a draw in printed Oracle text ("draw a card", "draws two
// cards", "draw X cards"); the group is the amount word.
var reDrawEffect = regexp.MustCompile(`\bdraws?\s+(an additional|additional|a|an|one|two|three|four|five|six|seven|x|\d+|that many|cards?|half|up to)\b`)

var drawWords = map[string]int{"a": 1, "an": 1, "an additional": 1, "additional": 1, "one": 1, "two": 2,
	"three": 3, "four": 4, "five": 5, "six": 6, "seven": 7, "eight": 8, "nine": 9, "ten": 10}

// reMillEffect and reTopCards find the other ways printed text reaches into
// the library: "mills ten cards", "the top three cards of your library",
// "exile the top card". The group is the amount word.
var reMillEffect = regexp.MustCompile(`\bmills?\s+(a|an|one|two|three|four|five|six|seven|eight|nine|ten|x|\d+|that many|cards?|half)\b`)
var reTopCards = regexp.MustCompile(`\btop\s+(a|an|one|two|three|four|five|six|seven|eight|nine|ten|x|\d+|that many|card|cards|half)\b`)

// reDeepLibrary marks text that reads a library of a size the text does not
// state: half of it, until a card is found, cascade, ripple.
var reDeepLibrary = regexp.MustCompile(`\bcascade\b|\bripple\b|\bclash\b|\bhalf (of )?(your|their|that player's|its owner's) library|(until|each card|all cards|the rest) .*\blibrary\b|\blibrary\b.* until\b`)

// unknownDraws stands for a draw whose size the text does not fix (X, "that
// many"): larger than any library a test adds.
const unknownDraws = 99

// oracleDraws is how many library cards the named card's Oracle text can draw,
// mill or look at (unknownDraws when the text does not bound it).
func (t *translator) oracleDraws(name string) int {
	if t.reg == nil {
		return 0
	}
	c, ok := t.lookup(name)
	if !ok {
		return 0
	}
	n := 0
	for _, f := range c.Faces {
		if f == nil {
			continue
		}
		text := strings.ToLower(f.Oracle)
		for _, re := range []*regexp.Regexp{reDrawEffect, reMillEffect, reTopCards} {
			for _, m := range re.FindAllStringSubmatch(text, -1) {
				if w, ok := drawWords[m[1]]; ok {
					n += w
				} else if m[1] == "card" {
					n++
				} else {
					n += unknownDraws
				}
			}
		}
		if reDeepLibrary.MatchString(text) {
			n += unknownDraws
		}
	}
	return n
}

// drawStepsBy counts the draw steps seat p's player takes from turn 1 up to
// the stop: playerA is the active player on odd turns and skips turn 1's draw,
// playerB is active on even turns. The stop step is played before XMage pauses,
// so the draw step of the stop turn counts when the stop is at or past it.
func (t *translator) drawStepsBy(p int) int {
	n := 0
	for turn := 1; turn <= t.stop[0]; turn++ {
		if turn == 1 || (turn%2 == 1) != (p == 0) {
			continue
		}
		if turn < t.stop[0] || t.stop[1] >= sDraw {
			n++
		}
	}
	return n
}

// checkClearedLibraries refuses a seat whose library the test emptied
// (removeAllCardsFromLibrary) when the method draws past the cards the test
// put back: XMage then draws from an empty library, where the runner draws its
// filler; the same goes for a mill or a look at the top that goes deeper than
// the added cards. Both are read from the Oracle text of every card the test
// names, counted once per card.
func (t *translator) checkClearedLibraries() *refusal {
	for p := 0; p < 2; p++ {
		if !t.libCleared[p] {
			continue
		}
		added := 0
		for _, ac := range t.setup[p] {
			if ac.zone == "library_top" {
				added += ac.count
			}
		}
		draws := t.drawStepsBy(p)
		seen := map[string]bool{}
		for q := 0; q < 2; q++ {
			for _, ac := range t.setup[q] {
				if ac.zone != "library_top" && !seen[ac.name] {
					seen[ac.name] = true
					draws += t.oracleDraws(ac.name)
				}
			}
		}
		for name := range t.allNames {
			if !seen[name] {
				seen[name] = true
				draws += t.oracleDraws(name)
			}
		}
		if draws > added {
			return refuse("library-size", "%s library cleared with %d card(s) added, %d draw(s)", pref(p), added, draws)
		}
	}
	return nil
}
