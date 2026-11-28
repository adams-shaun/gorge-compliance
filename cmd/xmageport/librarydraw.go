package main

import (
	"regexp"
	"strings"
)

// reDrawEffect finds a draw in printed Oracle text ("draw a card", "draws two
// cards", "draw eight cards"); the group is the amount word.
var reDrawEffect = regexp.MustCompile(`\bdraws?\s+(an additional|additional|a|an|one|two|three|four|five|six|seven|eight|nine|ten|x|\d+|that many|cards?|half|up to)\b`)

var drawWords = map[string]int{"a": 1, "an": 1, "an additional": 1, "additional": 1, "one": 1, "two": 2,
	"three": 3, "four": 4, "five": 5, "six": 6, "seven": 7, "eight": 8, "nine": 9, "ten": 10}

// reMillEffect and reTopCards find the other ways printed text reaches into
// the library: "mills ten cards", "the top three cards of your library",
// "exile the top card". The group is the amount word.
var reMillEffect = regexp.MustCompile(`\bmills?\s+(a|an|one|two|three|four|five|six|seven|eight|nine|ten|x|\d+|that many|cards?|half)\b`)
var reTopCards = regexp.MustCompile(`\btop\s+(a|an|one|two|three|four|five|six|seven|eight|nine|ten|x|\d+|that many|card|cards|half)\b`)

// reUnboundedAmount follows a draw amount the text does not fix: "draw a card
// for each creature you control", "draw cards equal to its power", "draw a
// number of cards". The amount word may be followed by the "card(s)" it
// counts.
var reUnboundedAmount = regexp.MustCompile(`^\s*(cards?\s+)?(for each|equal to|number of cards)\b`)

// reRepeatable marks a triggered ability that can resolve more than once
// before the scenario stops: "Whenever ...", "each time ...", or a
// beginning-of-step trigger on every turn.
var reRepeatable = regexp.MustCompile(`\b(whenever|each time|at the beginning of (each|your|every))\b`)

// reDeepLibrary marks text that reads a library of a size the text does not
// state: half of it, until a card is found, cascade, ripple, a search (which
// looks at every card), scry, surveil, explore.
var reDeepLibrary = regexp.MustCompile(`\bsearch(es)?\b.*\blibrary\b|\bscry\b|\bsurveil\b|\bexplores?\b|\bcascade\b|\bripple\b|\bclash\b|\bhalf (of )?(your|their|that player's|its owner's) library|(until|each card|all cards|the rest) .*\blibrary\b|\blibrary\b.* until\b`)

// unknownDraws stands for a draw whose size the text does not fix (X, "that
// many", "for each"): larger than any library a test adds.
const unknownDraws = 99

// effectDepth is how many library cards one matched draw/mill/look clause can
// reach. "For each"/"equal to" and a repeatable draw effect reach as many
// cards as the game gives them, so both are unknownDraws. Only draw effects
// repeat here: a triggered "exile the top card" fires once per stated event,
// and the Oracle text does not say how many events the test creates.
func effectDepth(text string, m []int, amount string, repeatable bool) int {
	if reUnboundedAmount.MatchString(text[m[1]:]) || (repeatable && clauseRepeats(text, m[0])) {
		return unknownDraws
	}
	if w, ok := drawWords[amount]; ok {
		return w
	}
	if amount == "card" {
		return 1
	}
	return unknownDraws
}

// clauseRepeats reports whether the draw at pos is the effect of a trigger
// that can resolve more than once. The clause before the first comma is the
// trigger condition itself, so a draw there ("Whenever you draw a card, ...")
// does not repeat.
func clauseRepeats(text string, pos int) bool {
	start := strings.LastIndexAny(text[:pos], ".\n") + 1
	end := len(text)
	if i := strings.IndexAny(text[pos:], ".\n"); i >= 0 {
		end = pos + i
	}
	clause := text[start:end]
	if !reRepeatable.MatchString(clause) {
		return false
	}
	comma := strings.Index(clause, ",")
	return comma < 0 || pos-start >= comma
}

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
		if f != nil {
			n += textDepth(strings.ToLower(f.Oracle))
		}
	}
	return n
}

// textDepth is how many library cards the (lowercased) text can draw, mill or
// look at: the sum of every matched clause, unknownDraws for a deep read.
func textDepth(text string) int {
	n := 0
	for _, ef := range []struct {
		re         *regexp.Regexp
		repeatable bool
	}{
		{reDrawEffect, true},
		{reMillEffect, false},
		{reTopCards, false},
	} {
		for _, m := range ef.re.FindAllStringSubmatchIndex(text, -1) {
			n += effectDepth(text, m, text[m[2]:m[3]], ef.repeatable)
		}
	}
	if reDeepLibrary.MatchString(text) {
		n += unknownDraws
	}
	return n
}

// drawStepsBy counts the draw steps seat p's player takes from turn 1 up to
// the stop: seat p is active on the turns t.active gives it. In a two-seat
// game the starting player skips turn 1's draw; with more seats it draws
// (rule 103.8a; gorge's multiplayer runner does the same). The stop step is
// played before XMage pauses, so the draw step of the stop turn counts when
// the stop is at or past it.
func (t *translator) drawStepsBy(p int) int {
	n := 0
	for turn := 1; turn <= t.stop[0]; turn++ {
		if (turn == 1 && t.seats() == 2) || t.active(turn) != p {
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
// names, once per use of the card (copies in play or hand, casts, activations).
func (t *translator) checkClearedLibraries() *refusal {
	for p := 0; p < t.seats(); p++ {
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
		// uses[name] is how many times the test can play the card: at least
		// one per copy it puts in play, hand or graveyard, and one per time
		// it names the card in a cast or activation. A draw spell cast twice
		// draws twice.
		uses := map[string]int{}
		for q := 0; q < t.seats(); q++ {
			for _, ac := range t.setup[q] {
				if ac.zone != "library_top" {
					uses[ac.name] += ac.count
				}
			}
		}
		for name := range t.allNames {
			if t.nameUses[name] > uses[name] {
				uses[name] = t.nameUses[name]
			}
			if uses[name] < 1 {
				uses[name] = 1
			}
		}
		for name, n := range uses { // an integer sum: map order cannot reach the output
			if d := t.oracleDraws(name); d > 0 {
				draws += min(d*n, unknownDraws)
			}
		}
		// An activateAbility names the ability text, not the card, so the
		// card's own text (counted once per copy above) misses repeat
		// activations: each activation past the first adds that text's depth.
		for text, n := range t.abilityUses { // an integer sum: map order cannot reach the output
			if n > 1 {
				if d := textDepth(strings.ToLower(text)); d > 0 {
					draws += min(d*(n-1), unknownDraws)
				}
			}
		}
		if draws > added {
			return refuse("library-size", "%s library cleared with %d card(s) added, %d draw(s)", pref(p), added, draws)
		}
	}
	return nil
}
