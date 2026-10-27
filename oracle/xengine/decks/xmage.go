package decks

import (
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance"
	"github.com/adams-shaun/gorge/engine/cards"
)

// ReasonXMageSpelling is the Skip reason of a card the -xmage candidate
// filter leaves out, and the refusal reason xprep gives an item holding one:
// XMage stores the card under a different name than the corpus does.
const ReasonXMageSpelling = "xmage_spelling"

// Options select how Candidates builds the list.
type Options struct {
	// XMage leaves out every card XMage spells differently from the corpus.
	// An item names one card per XMage driver field (oraclegen.Item.XMageName),
	// so a whole game of many cards cannot carry the spellings; its decks must
	// avoid them (xprep refuses an item that holds one).
	XMage bool
}

// Spellings is XMage's name for each card, read from the committed XMage set
// manifests (the names the pass and the gate stamp as Item.XMageName).
type Spellings struct {
	byFold map[string]string // compliance.FoldName(name) -> the manifest's spelling
}

// LoadSpellings reads every manifest under manifestDir. A name two sets list
// under spellings that fold equal keeps the first, in manifest file order.
func LoadSpellings(manifestDir string) (*Spellings, error) {
	names, err := compliance.LoadXMageNames(manifestDir)
	if err != nil {
		return nil, err
	}
	s := &Spellings{byFold: make(map[string]string, len(names))}
	for _, n := range names {
		k := compliance.FoldName(n)
		if _, ok := s.byFold[k]; !ok {
			s.byFold[k] = n
		}
	}
	return s, nil
}

// XMage is XMage's spelling of the card the corpus calls name (a front face, a
// back face or the card's own name); ok is false when no manifest lists it.
// A split, adventure or Room card is listed under all its faces ("A // B"), so
// the joined face names are tried too, as oraclediff's xmageSpelling does.
func (s *Spellings) XMage(reg *cards.Registry, name string) (string, bool) {
	if xm, ok := s.byFold[compliance.FoldName(name)]; ok {
		return xm, true
	}
	if c, ok := reg.Lookup(name); ok && len(c.Faces) > 1 {
		faces := make([]string, len(c.Faces))
		for i, f := range c.Faces {
			faces[i] = f.Name
		}
		if xm, ok := s.byFold[compliance.FoldName(strings.Join(faces, " // "))]; ok {
			return xm, true
		}
	}
	return "", false
}

// Differs is true when XMage spells the card other than name. The comparison
// is exact, not folded: "Dáin Ironfoot" and "Dain Ironfoot" fold equal and
// still need Item.XMageName.
func (s *Spellings) Differs(reg *cards.Registry, name string) bool {
	xm, ok := s.XMage(reg, name)
	return ok && xm != name
}

// ExcludedXMage is how many of skips the -xmage filter left out.
func ExcludedXMage(skips []Skip) int {
	n := 0
	for _, s := range skips {
		if s.Reason == ReasonXMageSpelling {
			n++
		}
	}
	return n
}
