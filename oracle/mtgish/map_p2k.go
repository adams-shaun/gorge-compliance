package mtgish

import "github.com/adams-shaun/gorge/engine/cards"

// Normaliser fixes for two mechanics a Forge corpus bump added or respelled.
// Each is a place where both encodings say the same thing and the two
// normalisers rendered it differently.
func init() {
	// "Foretelling cards from your hand can be done on any player's turn"
	// (Cosmos Charger): Forge now writes Mode$ ForetellAnyTurn where it once
	// granted a Foretell keyword; mtgish's CanForetellCardsDuringEachPlayersTurn
	// already maps to the same token.
	extStatic["ForetellAnyTurn"] = func(map[string]string) []string {
		return []string{"player:foretellonanyplayersturn"}
	}
	// Assimilate (Borg Queen, Perfection Manifest): the Star Trek keyword
	// action, spelled DB$ Assimilate in a script and Action.Assimilate in mtgish.
	extAPI["Assimilate"] = func(*cards.SA, map[string]string) []string { return []string{"assimilate"} }
	extAction["Assimilate"] = func(e *effects, _ node) { e.add("assimilate") }
}
