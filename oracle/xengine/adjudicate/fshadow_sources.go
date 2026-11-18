package adjudicate

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/rowio"
	"github.com/adams-shaun/gorge/engine/cards"
)

// RegistryPrimitive resolves an implicated ability to its IR primitive from
// the card's IR: the indexed ability's api:<API> or the indexed trigger's
// trig:<Mode>, else the card's one primitive when it has exactly one. A card
// the registry lacks, or an ambiguous one, is "?<card>" (unresolved: the class
// then holds that card alone).
func RegistryPrimitive(reg *cards.Registry) func(Implicated) string {
	return func(c Implicated) string {
		unresolved := "?" + c.signatureName()
		if reg == nil || c.Token {
			return unresolved
		}
		card, ok := lookup(reg, c.Name)
		if !ok || len(card.Faces) == 0 || card.Faces[0] == nil {
			return unresolved
		}
		f := card.Faces[0]
		if c.Ability != nil && *c.Ability >= 0 && *c.Ability < len(f.Abilities) && f.Abilities[*c.Ability] != nil {
			return "api:" + f.Abilities[*c.Ability].API
		}
		if c.Trigger != nil && *c.Trigger >= 0 && *c.Trigger < len(f.Triggers) {
			return "trig:" + f.Triggers[*c.Trigger].Mode
		}
		if ps := card.Primitives(); len(ps) == 1 {
			return ps[0]
		}
		return unresolved
	}
}

// lookup finds name in reg, falling back to an Alchemy A- card's paper name
// when the corpus has no A- script (gorge's edhcover resolves deck names so).
func lookup(reg *cards.Registry, name string) (*cards.Card, bool) {
	if c, ok := reg.Lookup(name); ok {
		return c, true
	}
	if s, ok := strings.CutPrefix(name, "A-"); ok {
		return reg.Lookup(s)
	}
	return nil, false
}

// The paper factors (design section 5's rank).
const (
	PaperCard   = 1.0
	DigitalCard = 0.1
)

// nonPaperTypes are the card types that never see a paper Commander game:
// design section 5's Scheme, Plane and Attraction, and the other
// non-tournament types gorge's edhcover ranks with them.
var nonPaperTypes = map[string]bool{
	"Scheme": true, "Plane": true, "Phenomenon": true, "Attraction": true,
	"Contraption": true, "Conspiracy": true, "Dungeon": true,
}

// RegistryPaper is the paper factor of a card: DigitalCard for an Alchemy A-
// name, a card with a Perpetual effect, a non-tournament type, or a card
// printed only in an Un- set (unOnly, folded names; nil: none); PaperCard
// otherwise, including a card the registry lacks.
func RegistryPaper(reg *cards.Registry, unOnly map[string]bool) func(string) float64 {
	return func(name string) float64 {
		if strings.HasPrefix(name, "A-") || unOnly[compliance.FoldName(name)] {
			return DigitalCard
		}
		if reg == nil {
			return PaperCard
		}
		c, ok := lookup(reg, name)
		if !ok {
			return PaperCard
		}
		for _, f := range c.Faces {
			if f == nil {
				continue
			}
			for _, t := range f.Types {
				if nonPaperTypes[t] {
					return DigitalCard
				}
			}
			if perpetual(f) {
				return DigitalCard
			}
		}
		return PaperCard
	}
}

// perpetual reports an ability of f, at any depth, with Duration$ Perpetual.
func perpetual(f *cards.Face) bool {
	found := false
	var walk func(sa *cards.SA, depth int)
	walk = func(sa *cards.SA, depth int) {
		if sa == nil || depth > 32 || found {
			return
		}
		if sa.Params["Duration"] == "Perpetual" {
			found = true
			return
		}
		walk(sa.Sub, depth+1)
	}
	for _, a := range f.Abilities {
		walk(a, 0)
	}
	for _, t := range f.Triggers {
		walk(t.Effect, 0)
	}
	f.EachSVarAbility(func(sa *cards.SA) { walk(sa, 0) })
	return found
}

// unSets are the Un- sets' codes.
var unSets = map[string]bool{"UGL": true, "UNH": true, "UST": true, "UND": true, "UNF": true, "UNK": true}

// UnOnlyNames reads the committed set manifests under dir and returns the
// folded names of the cards printed in an Un- set and in no other set there.
func UnOnlyNames(dir string) (map[string]bool, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	un, other := map[string]bool{}, map[string]bool{}
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		var m compliance.Manifest
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, fmt.Errorf("%s: %v", p, err)
		}
		into := other
		if unSets[m.Code] {
			into = un
		}
		for _, c := range m.Cards {
			into[compliance.FoldName(c.Name)] = true
		}
	}
	out := map[string]bool{}
	for n := range un {
		if !other[n] {
			out[n] = true
		}
	}
	return out, nil
}

// LoadDeckCounts reads the deck manifest TSV (edh-manifest.tsv: its source and
// id columns) and the stored decklists beside it (edh-decks.jsonl: source,
// id, commanders, cards), and returns how many manifest decks hold each card.
// A decklist the manifest does not name is not counted.
func LoadDeckCounts(manifestTSV, decklists string) (func(string) int, error) {
	f, err := os.Open(manifestTSV)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	in := map[string]bool{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	src, id := -1, -1
	for line := 1; sc.Scan(); line++ {
		cols := strings.Split(sc.Text(), "\t")
		if line == 1 {
			for i, c := range cols {
				switch c {
				case "source":
					src = i
				case "id":
					id = i
				}
			}
			if src < 0 || id < 0 {
				return nil, fmt.Errorf("%s: no source and id columns", manifestTSV)
			}
			continue
		}
		if len(cols) <= max(src, id) {
			continue
		}
		in[cols[src]+"\x00"+cols[id]] = true
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	counts := map[string]int{}
	err = rowio.Read(decklists, rowio.DefaultCap, func(b []byte) error {
		var d struct {
			Source     string   `json:"source"`
			ID         string   `json:"id"`
			Commanders []string `json:"commanders"`
			Cards      []string `json:"cards"`
		}
		if err := json.Unmarshal(b, &d); err != nil {
			return err
		}
		if !in[d.Source+"\x00"+d.ID] {
			return nil
		}
		seen := map[string]bool{}
		for _, n := range append(append([]string(nil), d.Commanders...), d.Cards...) {
			k := cards.NormalizeName(n)
			if k != "" && !seen[k] {
				seen[k] = true
				counts[k]++
			}
		}
		return nil
	}, nil)
	if err != nil {
		return nil, err
	}
	return func(card string) int { return counts[cards.NormalizeName(card)] }, nil
}

var (
	registerCall = regexp.MustCompile(`\bRegister\("([^"]+)",\s*([A-Za-z_][A-Za-z0-9_]*)\)`)
	funcDecl     = regexp.MustCompile(`(?m)^func ([A-Za-z_][A-Za-z0-9_]*)\(`)
)

// HandlerSuspect names the handler of an api: primitive as file:function by
// reading gorge's engine/effects sources under gorgeSrc (the gorge module
// directory, `go list -m -f '{{.Dir}}' github.com/adams-shaun/gorge`): the
// function a Register("<API>", fn) call installs and the file declaring it.
// A primitive it cannot place gets a grep line instead.
func HandlerSuspect(gorgeSrc string) func(string) string {
	handlers := map[string]string{}
	if gorgeSrc != "" {
		dir := filepath.Join(gorgeSrc, "engine", "effects")
		paths, _ := filepath.Glob(filepath.Join(dir, "*.go"))
		sort.Strings(paths)
		fn := map[string]string{}   // API -> function name
		file := map[string]string{} // function name -> file
		for _, p := range paths {
			if strings.HasSuffix(p, "_test.go") {
				continue
			}
			raw, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			rel := filepath.ToSlash(filepath.Join("engine", "effects", filepath.Base(p)))
			for _, m := range registerCall.FindAllStringSubmatch(string(raw), -1) {
				if _, ok := fn[m[1]]; !ok {
					fn[m[1]] = m[2]
				}
			}
			for _, m := range funcDecl.FindAllStringSubmatch(string(raw), -1) {
				if _, ok := file[m[1]]; !ok {
					file[m[1]] = rel
				}
			}
		}
		for api, f := range fn {
			if p, ok := file[f]; ok {
				handlers["api:"+api] = p + ":" + f
			}
		}
	}
	return func(prim string) string {
		if h, ok := handlers[prim]; ok {
			return h
		}
		return genericSuspect(prim)
	}
}

func genericSuspect(prim string) string {
	switch {
	case strings.HasPrefix(prim, "api:"):
		return fmt.Sprintf("the handler engine/effects registers for %s (`grep -n 'Register(\"%s\"' engine/effects/*.go`)", prim, strings.TrimPrefix(prim, "api:"))
	case strings.HasPrefix(prim, "?"):
		return fmt.Sprintf("unresolved: the finding names no ability of %s; read the first difference's source", strings.TrimPrefix(prim, "?"))
	case strings.HasPrefix(prim, "harness:"):
		return "the follower (`xengine fshadow`) for " + strings.TrimPrefix(prim, "harness:")
	}
	return fmt.Sprintf("the rules code that implements %s (`grep -rn 'RegisterNonAPI' engine/` names it)", prim)
}
