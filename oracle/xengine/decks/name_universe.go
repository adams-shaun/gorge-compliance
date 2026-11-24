package decks

import "github.com/adams-shaun/gorge/engine/cards"

// NameUniverse is the card-name universe an xengine game over deckCards must
// use so its name asks and gorge's oracle-runner replay agree. It mirrors
// gorge's engine/rules oracleNameUniverse: nil (no universe) unless a seated
// card mentions a card-name choice, else the distinct seated cards.
//
// A game whose NameUniverse is the whole corpus can name any card in the
// corpus, while the runner offers only the cards the lowered scenario seats
// (CR 201.3 allows both, so neither is wrong). A name outside the scenario's
// cards can never be replayed, so both sides use this one.
func NameUniverse(deckCards [][]*cards.Card) *cards.Universe {
	need := false
	seen := map[*cards.Card]bool{}
	var cs []*cards.Card
	for _, deck := range deckCards {
		for _, c := range deck {
			if c == nil || seen[c] {
				continue
			}
			seen[c] = true
			cs = append(cs, c)
			need = need || cards.MentionsNameChoice(c)
		}
	}
	if !need {
		return nil
	}
	return cards.UniverseOf(cs)
}
