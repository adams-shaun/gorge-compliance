package certdeck

// The pool must be one the Forge card DB can seat. Forge builds its StaticData
// with loadNonLegalCards=false: every card NAMED in a Type=Funny or
// Border=Silver edition outside that edition's [eternal] (legal reprint) section
// is filtered, basic lands excepted (forge/StaticData.java,
// CardEdition.Type.FUNNY / BorderColor.SILVER). The pinned cardsfolder still
// holds those scripts, so a card that is filtered here and not there fails at
// SetupBuilder.create with "Couldn't find a card" -- Indicate, Mox Poison and
// Gobland are the Mystery Booster 2 playtest rows that did.
//
// The edition files are Forge's (GPL), not gorge's corpus, so they are read
// from the out-of-tree Forge clone at run time and never committed. A card
// whose only printings are in a Funny, playtest or digital-only edition is
// also out of the pool for operator policy (11-17: deprioritise digital and
// non-tournament cards); a legal reprint in an ordinary paper edition keeps it
// in.

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
)

// forgeEditions is the exclusion set read from one pinned Forge clone's
// forge-gui/res/editions. Names are the raw edition-row names, compared
// exactly: Forge puts the raw EditionEntry.name in its filter set and compares
// the raw card name (StaticData.java), and the preflight's funny_has is an
// exact grep -qxF. Normalising would conflate the Unknown Event row "Lava,
// Axe" with the paper card "Lava Axe", or "Bind // Liberate" with "Bind".
type forgeEditions struct {
	filtered map[string]bool // named in a Funny/Silver edition outside [eternal]
	printed  map[string]bool // named in any edition's card section
	paper    map[string]bool // named in a tournament-legal paper printing
}

var (
	// forgeCollector is the collector-number shape Forge edition rows use. The
	// preflight awk (scripts/forge-oracle-run.sh) accepts the same shape, with
	// F-prefixed playtest numbers (F501) and letter-suffixed numbers (CAa).
	forgeCollector = regexp.MustCompile(`^.?[0-9A-Z-]+[^ ]*$`)
	forgeRarities  = map[string]bool{"S": true, "C": true, "U": true, "R": true, "M": true, "L": true}
	// forgeCardSections is the section set the preflight reads: every place a
	// card name appears in an edition. [eternal] is handled separately as the
	// legal-reprint list.
	forgeCardSections = map[string]bool{
		"cards": true, "eternal": true, "special slot": true, "precon product": true,
		"borderless": true, "etched": true, "showcase": true, "full art": true,
		"extended art": true, "alternate art": true, "retro frame": true,
		"buy a box": true, "promo": true, "prerelease promo": true, "bundle": true,
		"box topper": true, "jumpstart": true, "rebalanced": true, "conjured": true,
		"scheme": true, "printsheets": true,
	}
	// forgeNonPaperSections are card sections that are not a paper printing
	// even in an otherwise paper edition (Alchemy rebalances, conjured cards,
	// scheme cards).
	forgeNonPaperSections = map[string]bool{"rebalanced": true, "conjured": true, "scheme": true}
	forgeBasicLands       = map[string]bool{
		"Plains": true, "Island": true, "Swamp": true, "Mountain": true, "Forest": true, "Wastes": true,
		"Snow-Covered Plains": true, "Snow-Covered Island": true, "Snow-Covered Swamp": true,
		"Snow-Covered Mountain": true, "Snow-Covered Forest": true,
	}
)

// loadForgeEditions reads every *.txt under dir in name order, so the result
// does not depend on directory order.
func loadForgeEditions(dir string) (*forgeEditions, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("certdeck: Forge editions %s: %v", dir, err)
	}
	var files []string
	for _, e := range ents {
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".txt") {
			files = append(files, e.Name())
		}
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("certdeck: Forge editions %s: no *.txt edition files", dir)
	}
	sort.Strings(files)
	e := &forgeEditions{filtered: map[string]bool{}, printed: map[string]bool{}, paper: map[string]bool{}}
	for _, name := range files {
		if err := e.read(filepath.Join(dir, name)); err != nil {
			return nil, err
		}
	}
	return e, nil
}

// read adds one edition file's card names to e.
func (e *forgeEditions) read(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("certdeck: Forge edition %s: %v", path, err)
	}
	defer f.Close()
	type row struct{ name, section string }
	var rows []row
	section := ""
	funny, digital := false, false
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSuffix(sc.Text(), "\r")
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			section = strings.ToLower(strings.TrimSpace(trimmed[1 : len(trimmed)-1]))
			continue
		}
		switch section {
		case "metadata":
			k, v, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			k = strings.ToLower(strings.TrimSpace(k))
			v = strings.ToLower(strings.TrimSpace(v))
			switch k {
			case "type":
				if v == "funny" {
					funny = true
				}
				if v == "online" {
					digital = true
				}
			case "border":
				if v == "silver" {
					funny = true
				}
			}
		default:
			if !forgeCardSections[section] {
				continue
			}
			name, ok := forgeEditionName(line)
			if !ok {
				continue
			}
			rows = append(rows, row{name, section})
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("certdeck: Forge edition %s: %v", path, err)
	}
	for _, r := range rows {
		name := r.name
		e.printed[name] = true
		if r.section == "eternal" || forgeBasicLands[name] {
			e.paper[name] = true
			continue
		}
		if funny {
			e.filtered[name] = true
			continue
		}
		if !digital && !forgeNonPaperSections[r.section] {
			e.paper[name] = true
		}
	}
	return nil
}

// forgeEditionName is the card name of one edition row: the line without its
// trailing artist/parameter, then without its "N R " / "R " collector-number
// and rarity prefix. It mirrors the preflight's awk pass.
func forgeEditionName(line string) (string, bool) {
	line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
	if line == "" || strings.HasPrefix(line, "#") {
		return "", false
	}
	if i := strings.Index(line, " @"); i >= 0 {
		line = line[:i]
	}
	if i := strings.Index(line, " ${"); i >= 0 {
		line = line[:i]
	}
	fields := strings.Fields(line)
	switch {
	case len(fields) >= 3 && forgeRarities[fields[1]] && forgeCollector.MatchString(fields[0]):
		line = strings.Join(fields[2:], " ")
	case len(fields) >= 2 && forgeRarities[fields[0]]:
		line = strings.Join(fields[1:], " ")
	}
	line = strings.TrimSpace(line)
	return line, line != ""
}

// excluded reports whether name is out of the pool: Forge's card DB filters it,
// or it has no tournament-legal paper printing.
func (e *forgeEditions) excluded(name string) bool {
	return e.filtered[name] || (e.printed[name] && !e.paper[name])
}

// UseForgeEditions applies the pinned Forge editions under dir to the builder's
// pool. It is what keeps an enabler, bearer or filler card the Forge card DB
// will not load out of a certification deck; the focus card itself is not
// filtered.
func (b *Builder) UseForgeEditions(dir string) error {
	e, err := loadForgeEditions(dir)
	if err != nil {
		return err
	}
	b.forge = e
	// A Playable answer cached before the filter was installed is unfiltered.
	b.ok = map[*cards.Card]bool{}
	return nil
}

// FindForgeEditions returns the pinned Forge editions directory, or "" when
// none is found. FORGE_ORACLE_EDITIONS wins; then FORGE_ORACLE_SRC (a
// forge-oracle worktree, whose parent holds forge-gui/res/editions);
// then FORGE_ORACLE_CLONE (the clone path the Forge scripts honour), then
// FORGE_ORACLE_DIR and the gitignored .oracle-build/forge layout that
// scripts/forge-oracle-setup.sh writes. It only reads the directory, so it
// works before the Java build exists.
func FindForgeEditions() string {
	var roots []string
	add := func(d string) {
		if d != "" {
			roots = append(roots, d)
		}
	}
	if d := os.Getenv("FORGE_ORACLE_EDITIONS"); d != "" {
		if isForgeEditions(d) {
			return d
		}
		add(d)
	}
	if d := os.Getenv("FORGE_ORACLE_SRC"); d != "" {
		add(filepath.Dir(d))
		add(d)
	}
	if d := os.Getenv("FORGE_ORACLE_CLONE"); d != "" {
		add(d)
	}
	if d := os.Getenv("FORGE_ORACLE_DIR"); d != "" {
		add(filepath.Join(d, "forge"))
		add(d)
	}
	if wd, err := os.Getwd(); err == nil {
		for dir := wd; ; {
			add(filepath.Join(dir, ".oracle-build", "forge", "forge"))
			add(filepath.Join(dir, ".oracle-build", "forge"))
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	for _, r := range roots {
		for _, cand := range []string{r, filepath.Join(r, "forge-gui", "res", "editions")} {
			if isForgeEditions(cand) {
				return cand
			}
		}
	}
	return ""
}

// isForgeEditions reports whether dir holds at least one edition *.txt.
func isForgeEditions(dir string) bool {
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() {
		return false
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range ents {
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".txt") {
			return true
		}
	}
	return false
}
