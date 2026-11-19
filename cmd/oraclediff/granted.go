package main

// Resolving a granted or gained activated ability for the follower exporters.
//
// A lowered step that activates a granted (AddAbilities$/AddAbility$ SVar),
// gained (GainsAbilitiesOf$) or keyword-granted ability carries the engine's
// offered option label in Step.Ability beside its AbilityIndex (the lower's
// granted-identity branch): the pick's Ability index is not the card's printed
// index there (it aliases printed index 0 for an SVar grant and is -1 for a
// keyword grant), so the printed-index resolution forgeAbilityFace performs
// would export the WRONG line -- Dalakos, Crafter of Wonders's {T} mana
// ability for Dragon Mantle's granted {R} pump, the farm's
// xe:RAND:1:4:w14/w22 divergence.
//
// The label is exact engine text: "<recipient face name>: <SpellDescription>"
// (legal.go's granted-ability offer loops; "Face-down creature" when the
// recipient is face down). resolveGrantedStepAbility finds the SA the label
// names by scanning the cards the item's setup puts in play -- the recipient
// itself and every potential grantor: printed abilities (a gained ability is
// a printed SA on a foreign card's face) and each face's SVar table (an SVar
// grant's body) -- for the one activated ability whose engine label equals
// the step's label. Matching the label, not a list of grantor card names,
// covers the next granted ability without a new selector; the label is the
// engine's own, so the match is exact, not a heuristic.
//
// Keyword-granted abilities (a layer-6 AddKeyword$ Cycling/Saddle/Crew grant)
// are deliberately NOT resolved here: their lines derive from the live
// layer walk the exporter cannot re-run, so no candidate matches and the
// exporters omit the line or refuse the step (their current behaviour for an
// unresolvable ability) instead of exporting the printed index's ability.
// Such a replay fails loudly in gorge's runner anyway (the keyword pick's
// index is -1), so nothing silently wrong ships.

import (
	"sort"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge/engine/cards"
)

// grantedAbilityLabel is the label the engine offers sa under on a recipient
// whose face is named recipient: legal.go's offer loops label a granted,
// gained or keyword-granted ability "<face name>: <SpellDescription>".
func grantedAbilityLabel(recipient string, sa *cards.SA) string {
	return recipient + ": " + sa.ParamStr(cards.PKSpellDescription)
}

// resolveGrantedStepAbility returns the granted/gained ability an activate
// step whose lowering recorded the engine's offered label names, or false
// when the step names no label or no card the setup may put in play carries
// an activated ability under exactly that label (two candidates with the
// same label are ambiguous and count as none, so a wrong export never
// depends on scan order).
func resolveGrantedStepAbility(reg *cards.Registry, it oraclegen.Item, st oraclegen.Step) (*cards.SA, bool) {
	if st.Ability == "" || st.AbilityIndex == nil {
		return nil, false
	}
	var found []*cards.SA
	seen := map[string]bool{}
	add := func(sa *cards.SA) {
		if sa == nil || !sa.IsActivated() || seen[sa.Line] {
			return
		}
		if grantedAbilityLabel(grantedLabelName(st.Card), sa) != st.Ability {
			return
		}
		seen[sa.Line] = true
		found = append(found, sa)
	}
	for _, c := range grantedSourceCards(reg, it, st.Card) {
		for _, f := range c.Faces {
			if f == nil {
				continue
			}
			for _, ab := range f.Abilities {
				add(ab)
			}
			for sv := range f.SVars {
				add(cards.ResolveSVar(f.SVars, sv))
			}
		}
	}
	if len(found) != 1 {
		return nil, false
	}
	return found[0], true
}

// grantedLabelName is the face name the step's card's offered label is
// prefixed with: the ref's card name, or, for a token ref, the token's own
// face name (the token stem is the script, not the face name).
func grantedLabelName(ref string) string {
	name := refCardName(ref)
	if tokenName, isToken := cutTokenRef(name); isToken {
		return tokenName
	}
	return name
}

// cutTokenRef splits a "token:<name>" ref into its face name.
func cutTokenRef(name string) (string, bool) {
	return strings.CutPrefix(name, "token:")
}

// grantedSourceCards is every card the item's setup may put a grantor or the
// step's recipient on: the recipient itself (a printed card or a setup
// token), then each seat's battlefield (a grantor static's or Aura's
// carrier), back faces, hand, graveyard, exile, command zone and library
// (the off-battlefield grant walk reads those zones too), and the setup
// tokens. Deterministic order: each zone in its slice order; the only map
// iterated is a face's SVar table, whose order never reaches the result (an
// ambiguous label resolves to none).
func grantedSourceCards(reg *cards.Registry, it oraclegen.Item, ref string) []*cards.Card {
	var out []*cards.Card
	seen := map[*cards.Card]bool{}
	add := func(c *cards.Card) {
		if c == nil || len(c.Faces) == 0 || seen[c] {
			return
		}
		seen[c] = true
		out = append(out, c)
	}
	name := refCardName(ref)
	if tokenName, isToken := cutTokenRef(name); isToken {
		add(grantedTokenCard(reg, it, tokenName))
	} else if c, ok := reg.Lookup(name); ok {
		add(c)
	}
	for _, seat := range it.Setup {
		for _, name := range seat.Battlefield {
			if c, ok := reg.Lookup(name); ok {
				add(c)
			}
		}
		for _, name := range seat.BackFace {
			if c, ok := reg.Lookup(name); ok {
				add(c)
			}
		}
		for _, zone := range [][]string{seat.Hand, seat.Graveyard, seat.Exile, seat.Command, seat.Library, seat.LibraryTop} {
			for _, name := range zone {
				if c, ok := reg.Lookup(name); ok {
					add(c)
				}
			}
		}
		for _, tok := range seat.Tokens {
			add(reg.Tokens[tok.Script])
		}
	}
	return out
}

// grantedTokenCard resolves a token face name the way forgeTokenCard's cheap
// paths do: a setup token whose face name matches, else a corpus card, else
// the unique token script whose face name matches (stems in sorted order, so
// the scan is deterministic; a tie is no match).
func grantedTokenCard(reg *cards.Registry, it oraclegen.Item, name string) *cards.Card {
	for _, seat := range it.Setup {
		for _, t := range seat.Tokens {
			if c := reg.Tokens[t.Script]; c != nil && len(c.Faces) > 0 && c.Faces[0].Name == name {
				return c
			}
		}
	}
	if c, ok := reg.Lookup(name); ok && len(c.Faces) > 0 {
		return c
	}
	stems := make([]string, 0, len(reg.Tokens))
	for stem := range reg.Tokens {
		stems = append(stems, stem)
	}
	sort.Strings(stems)
	var cands []*cards.Card
	for _, stem := range stems {
		c := reg.Tokens[stem]
		if c != nil && len(c.Faces) > 0 && c.Faces[0].Name == name {
			cands = append(cands, c)
		}
	}
	if len(cands) == 1 {
		return cands[0]
	}
	return nil
}
