package main

// follower-export: forge-export for a follower that is not Forge.
//
//	oraclediff follower-export -engine NAME [-cards .cards] [-runner PATH]
//	                           -scenarios items.jsonl -out req.jsonl
//
// It writes, for every item the follower can replay, exactly the line
// forge-export writes for that item (same id, scenario_sha, item, decisions
// and abilities, the same Forge rewrites) plus one more field, "neutral": the
// engine-neutral identities of the abilities, setup tokens and modes the item
// names, for a follower that does not read Forge scripts. An item that names
// a card the follower lacks is not written to req.jsonl; it goes to
// <dir of -out>/<NAME>-prep.jsonl instead, which `xengine diff -engine NAME`
// reads as class not_sent.
//
// # Preflight protocol
//
// Before it writes anything, follower-export asks the follower once per run
// which cards it lacks:
//
//	RUNNER --preflight NAMES.txt
//
// RUNNER is the registry's Follower.Runner (or -runner). NAMES.txt holds one
// card name per line, UTF-8, sorted, no duplicates: every card name any item
// of the run references (each seat's battlefield, tapped, back_face, hand,
// graveyard, exile, library, library_top, counters and attached cards, and
// the card refs in the steps: card, targets, attackers, blocks, attached_to,
// with the "pN:" owner and "#k" ordinal stripped). Tokens ("pN:token:...")
// and player refs are not cards and are never asked about; names gorge's own
// corpus does not know are dropped too. Names are Forge/gorge spellings.
//
// The runner prints, on stdout, one line per card it LACKS:
//
//	NAME<TAB>REASON
//
// NAME exactly as NAMES.txt gave it, REASON free text without a tab or
// newline (e.g. "not in card-data", "unsupported keyword Foretell"). A card
// it supports gets no line. Blank lines are ignored. Anything on stderr is
// passed through to follower-export's stderr. Exit status 0 means the answer
// is complete; any other exit status (or a line without a tab, or a NAME
// that was not asked about) fails the whole export, since a partial answer
// would send items the follower cannot place.
//
// An item naming at least one refused card becomes the refusal row
//
//	{"id": ID, "reason": "<NAME>_lacks_card", "detail": "Card A: reason; Card B: reason"}
//
// (the refused names sorted), the shape xprep.Refusal has, and no request
// line. Every other item's request is written in input order.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/follower"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/xprep"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/effects"
	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/engine/rules/chars"
)

// followerRequest is one line of a follower-export request file: the Forge
// request for the item, field for field, then Neutral.
type followerRequest struct {
	forgeRequest
	Neutral neutralIDs `json:"neutral"`
}

// neutralIDs are the identities a follower that does not read Forge scripts
// needs to replay an item. Every map is present (possibly empty). An entry,
// or a field of one, that cannot be derived reliably is left out rather than
// guessed; the export's coverage census counts each omission and its reason.
type neutralIDs struct {
	// Ability is keyed like abilities: the decimal index of a step that
	// names an ability by ability_index.
	Ability map[string]neutralAbility `json:"ability"`
	// Token is keyed by tokenscript stem, one entry per stem a seat's setup
	// tokens name.
	Token map[string]neutralToken `json:"token"`
	// Mode is keyed by the decimal index of a modal-choice decision in this
	// request's gorge_decisions (resume cast_modes or modes).
	Mode map[string]neutralMode `json:"mode"`
}

// neutralAbility is one ability by class and ordinal.
type neutralAbility struct {
	// Class is mana (an activated mana ability, CR 605.1a), activated (any
	// other activated ability), loyalty (CR 606) or intrinsic_mana (the
	// ability a basic land type grants, CR 305.6, which no script line holds).
	Class string `json:"class,omitempty"`
	// Ordinal is the ability's 0-based index among the face's abilities of
	// the same class, in script order. Left out for an ability a keyword
	// line mints (Equip, Cycling, ...): its place in the Oracle text is not
	// the script's, so Keyword names it instead.
	Ordinal *int `json:"ordinal,omitempty"`
	// Keyword is the keyword line ("Equip:2") a minted ability comes from.
	Keyword string `json:"keyword,omitempty"`
	// Cost is the activation cost: mana symbols braced and run together
	// ("{2}{W}"), {T}/{Q}, a loyalty cost as "+N"/"-N", any other part in
	// Forge's own spelling with CARDNAME as "~" ("Sac<1/~>"), parts joined
	// by ", " in script order.
	Cost string `json:"cost,omitempty"`
	// Text is the script's SpellDescription with CARDNAME as "~".
	Text string `json:"text,omitempty"`
	// Produces (class mana and intrinsic_mana only) is the set of mana
	// letters an activation can add, in WUBRGC order ("G", "WU" for a
	// choice of white or blue), or "any" for any colour.
	Produces string `json:"produces,omitempty"`
}

// neutralToken is a setup token's characteristics, from gorge's tokenscript.
type neutralToken struct {
	Name       string   `json:"name"`
	Supertypes []string `json:"supertypes,omitempty"`
	Types      []string `json:"types"`
	Subtypes   []string `json:"subtypes,omitempty"`
	// Colours are WUBRG letters; "" is colourless. Left out (with the
	// census counting it) when the script's colour word is not one of the
	// five colours, "all" or "colorless".
	Colours   *string  `json:"colours,omitempty"`
	Power     *int     `json:"power,omitempty"`
	Toughness *int     `json:"toughness,omitempty"`
	Keywords  []string `json:"keywords,omitempty"`
	// Text is the script's Oracle text, lines separated by "\n".
	Text string `json:"text,omitempty"`
}

// neutralMode is one modal choice: the chosen modes, each by its 0-based
// position in the spell's or ability's printed mode list (the Choices$ order),
// and its text with CARDNAME as "~".
type neutralMode struct {
	Step  int               `json:"step"`
	Picks []neutralModePick `json:"picks"`
}

type neutralModePick struct {
	Ordinal int    `json:"ordinal"`
	Text    string `json:"text"`
}

// census counts what follower-export filled and what it left out, by key.
type census map[string]int

func (c census) add(k string) { c[k]++ }

// followerExportStats is what one follower-export run did.
type followerExportStats struct {
	Items, Sent, Refused int
	Asked, Lacking       int
	Census               census
}

func runFollowerExport(args []string) error {
	fs := flag.NewFlagSet("follower-export", flag.ExitOnError)
	engine := fs.String("engine", "", "follower whose runner reads Forge requests (required)")
	dir := fs.String("cards", ".cards", "corpus dir")
	scen := fs.String("scenarios", "", "scenario JSONL (from gen or xengine play)")
	out := fs.String("out", "", "request JSONL (off-repo); refusals go to <dir>/<engine>-prep.jsonl")
	runner := fs.String("runner", "", "preflight runner (default: the registry's runner for -engine)")
	fs.Parse(args)
	fl, ok := follower.Lookup(*engine)
	if !ok || !readsForgeRequests(fl) {
		return fmt.Errorf("follower-export needs -engine naming a follower that reads Forge requests (%s)", strings.Join(forgeRequestFollowers(), "|"))
	}
	if *scen == "" || *out == "" {
		return fmt.Errorf("follower-export needs -scenarios and -out")
	}
	if *runner == "" {
		*runner = fl.Runner
	}
	reg, err := loadReg(*dir)
	if err != nil {
		return err
	}
	st, err := followerExport(reg, fl, *runner, *scen, *out)
	if err != nil {
		return err
	}
	fmt.Printf("wrote %d requests to %s; %d of %d items refused (%d of %d cards lacking) to %s\n",
		st.Sent, *out, st.Refused, st.Items, st.Lacking, st.Asked, followerPrepPath(fl, *out))
	keys := make([]string, 0, len(st.Census))
	for k := range st.Census {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("  %-48s %d\n", k, st.Census[k])
	}
	return nil
}

// followerPrepPath is where follower-export writes fl's refusals: beside the
// request file, as <engine>-prep.jsonl.
func followerPrepPath(fl follower.Follower, out string) string {
	return filepath.Join(filepath.Dir(out), fl.Name+"-prep.jsonl")
}

// followerExport writes fl's requests for the items in scen to out and its
// refusals beside it, asking runner's --preflight which cards fl lacks. The
// follower and runner are parameters so a test can drive a follower the
// registry does not hold.
func followerExport(reg *cards.Registry, fl follower.Follower, runner, scen, out string) (followerExportStats, error) {
	st := followerExportStats{Census: census{}}
	prepPath := followerPrepPath(fl, out)
	for _, p := range []string{out, prepPath} {
		if err := refuseRepoPath(p); err != nil {
			return st, err
		}
	}
	var items []oraclegen.Item
	if err := readLines(scen, func(b []byte) error {
		var it oraclegen.Item
		if err := json.Unmarshal(b, &it); err != nil {
			return err
		}
		items = append(items, it)
		return nil
	}); err != nil {
		return st, err
	}
	st.Items = len(items)
	itemNames := make([][]string, len(items))
	union := map[string]bool{}
	for i, it := range items {
		itemNames[i] = followerCardNames(reg, it)
		for _, n := range itemNames[i] {
			union[n] = true
		}
	}
	names := make([]string, 0, len(union))
	for n := range union {
		names = append(names, n)
	}
	sort.Strings(names)
	st.Asked = len(names)
	lacks, err := runPreflight(runner, names, filepath.Dir(out))
	if err != nil {
		return st, fmt.Errorf("%s preflight: %w", fl.Display, err)
	}
	st.Lacking = len(lacks)

	of, err := os.Create(out)
	if err != nil {
		return st, err
	}
	defer of.Close()
	pf, err := os.Create(prepPath)
	if err != nil {
		return st, err
	}
	defer pf.Close()
	w, pw := bufio.NewWriter(of), bufio.NewWriter(pf)
	for i, it := range items {
		var why []string
		for _, n := range itemNames[i] {
			if r, ok := lacks[n]; ok {
				why = append(why, n+": "+r)
			}
		}
		var line []byte
		if len(why) > 0 {
			st.Refused++
			line, err = json.Marshal(xprep.Refusal{ID: it.ID, Reason: fl.Name + "_lacks_card", Detail: strings.Join(why, "; ")})
			if err != nil {
				return st, err
			}
			pw.Write(line)
			pw.WriteByte('\n')
			continue
		}
		rq, g := forgeExportItem(reg, it)
		line, err = json.Marshal(followerRequest{forgeRequest: rq, Neutral: neutralFor(reg, rq, g, st.Census)})
		if err != nil {
			return st, err
		}
		st.Sent++
		w.Write(line)
		w.WriteByte('\n')
	}
	if err := w.Flush(); err != nil {
		return st, err
	}
	if err := pw.Flush(); err != nil {
		return st, err
	}
	if err := pf.Close(); err != nil {
		return st, err
	}
	return st, of.Close()
}

// playerRef matches a bare player ref ("p0"), which xprep.CardNames passes
// through as if it were a name.
var playerRef = regexp.MustCompile(`^p\d+$`)

// followerCardNames is every card the item references, as the preflight
// protocol lists them: xprep.CardNames plus the attached Auras and the item's
// own card, without player refs and names gorge's corpus does not know.
func followerCardNames(reg *cards.Registry, it oraclegen.Item) []string {
	seen := map[string]bool{}
	add := func(n string) {
		if n == "" || playerRef.MatchString(n) {
			return
		}
		if _, ok := reg.Lookup(n); ok {
			seen[n] = true
		}
	}
	for _, n := range xprep.CardNames(it) {
		add(n)
	}
	add(it.Card)
	for _, s := range it.Setup {
		for _, a := range s.Attached {
			add(a.Card)
			if !strings.Contains(a.To, ":token:") {
				add(refCardName(a.To))
			}
		}
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// runPreflight runs `runner --preflight NAMES` (the protocol in the file
// comment) and returns the lacking cards' reasons by name. The names file is
// written to a temporary file in dir and removed afterwards.
func runPreflight(runner string, names []string, dir string) (map[string]string, error) {
	nf, err := os.CreateTemp(dir, ".preflight-*.txt")
	if err != nil {
		return nil, err
	}
	defer os.Remove(nf.Name())
	for _, n := range names {
		if strings.ContainsAny(n, "\t\n") {
			return nil, fmt.Errorf("card name %q holds a tab or newline", n)
		}
		fmt.Fprintln(nf, n)
	}
	if err := nf.Close(); err != nil {
		return nil, err
	}
	var stdout bytes.Buffer
	cmd := exec.Command(runner, "--preflight", nf.Name())
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return nil, fmt.Errorf("%s --preflight exited %d; nothing was exported", runner, ee.ExitCode())
		}
		return nil, fmt.Errorf("%s --preflight: %w; nothing was exported", runner, err)
	}
	asked := make(map[string]bool, len(names))
	for _, n := range names {
		asked[n] = true
	}
	lacks := map[string]string{}
	for i, l := range strings.Split(stdout.String(), "\n") {
		l = strings.TrimSuffix(l, "\r")
		if strings.TrimSpace(l) == "" {
			continue
		}
		name, reason, ok := strings.Cut(l, "\t")
		if !ok {
			return nil, fmt.Errorf("%s --preflight line %d %q has no tab (want NAME<TAB>REASON)", runner, i+1, l)
		}
		if !asked[name] {
			return nil, fmt.Errorf("%s --preflight line %d names %q, which was not asked about", runner, i+1, name)
		}
		if strings.TrimSpace(reason) == "" {
			reason = "no reason given"
		}
		lacks[name] = reason
	}
	return lacks, nil
}

// neutralFor builds the request's neutral identities from the request as
// forge-export wrote it and gorge's result for the item, counting into c.
func neutralFor(reg *cards.Registry, rq forgeRequest, g rules.OracleResult, c census) neutralIDs {
	n := neutralIDs{Ability: map[string]neutralAbility{}, Token: map[string]neutralToken{}, Mode: map[string]neutralMode{}}
	it := rq.Item
	for i, st := range it.Steps {
		if st.AbilityIndex == nil || st.Card == "" {
			continue
		}
		c.add("ability.steps")
		f := forgeAbilityFace(reg, it, st, g, i)
		if f == nil {
			c.add("ability.omit:face_unresolved")
			continue
		}
		if a, ok := neutralAbilityOf(f, *st.AbilityIndex, c); ok {
			n.Ability[strconv.Itoa(i)] = a
		}
	}
	seats := make([]string, 0, len(it.Setup))
	for s := range it.Setup {
		seats = append(seats, s)
	}
	sort.Strings(seats)
	for _, s := range seats {
		for _, t := range it.Setup[s].Tokens {
			if _, done := n.Token[t.Script]; done {
				continue
			}
			c.add("token.stems")
			tc := reg.Tokens[t.Script]
			if tc == nil || len(tc.Faces) == 0 {
				c.add("token.omit:no_script")
				continue
			}
			n.Token[t.Script] = neutralTokenOf(tc.Faces[0], c)
		}
	}
	for i, d := range rq.GorgeDecisions {
		if d.Kind != "mode" || (d.Resume != "cast_modes" && d.Resume != "modes") {
			continue
		}
		c.add("mode.decisions")
		if m, why := neutralModeOf(reg, d); why != "" {
			c.add("mode.omit:" + why)
		} else {
			c.add("mode.filled")
			n.Mode[strconv.Itoa(i)] = m
		}
	}
	return n
}

// abilityClass is the neutral class of sa, or "" with the reason it has none.
func abilityClass(sa *cards.SA) (class, why string) {
	switch {
	case sa == nil:
		return "", "nil_ability"
	case strings.HasPrefix(sa.Line, "intrinsic:"):
		return "intrinsic_mana", ""
	case !sa.IsActivated():
		return "", "not_activated_" + sa.Kind
	case sa.ParamStr(cards.PKPlaneswalker) == "True":
		return "loyalty", ""
	case cards.IsManaAbilitySA(sa):
		return "mana", ""
	}
	return "activated", ""
}

// neutralAbilityOf is Abilities[k] of f by class, ordinal, cost, text and
// production. ok is false when the ability has no class.
func neutralAbilityOf(f *cards.Face, k int, c census) (neutralAbility, bool) {
	sa := f.Abilities[k]
	class, why := abilityClass(sa)
	if class == "" {
		c.add("ability.omit:" + why)
		return neutralAbility{}, false
	}
	a := neutralAbility{Class: class}
	c.add("ability.class")
	if kw := sa.ParamStr(cards.PKKeywordLine); kw != "" {
		a.Keyword = kw
		c.add("ability.ordinal_omit:keyword_minted")
	} else {
		ord := 0
		for j := 0; j < k; j++ {
			o := f.Abilities[j]
			if o == nil || o.ParamStr(cards.PKKeywordLine) != "" {
				continue
			}
			if oc, _ := abilityClass(o); oc == class {
				ord++
			}
		}
		a.Ordinal = &ord
		c.add("ability.ordinal")
	}
	if cost, ok := neutralCost(sa.ParamStr(cards.PKCost)); ok {
		a.Cost = cost
		c.add("ability.cost")
	} else {
		c.add("ability.cost_omit:unparsed")
	}
	if d := strings.TrimSpace(sa.ParamStr(cards.PKSpellDescription)); d != "" {
		a.Text = tilde(d)
		c.add("ability.text")
	} else if class == "intrinsic_mana" {
		c.add("ability.text_omit:intrinsic")
	} else {
		c.add("ability.text_omit:no_description")
	}
	if class == "mana" || class == "intrinsic_mana" {
		prod := sa
		if !cards.IsManaAbilityAPI(sa.API) {
			prod = cards.ManaChainProduction(sa)
		}
		if p, ok := neutralProduces(prod); ok {
			a.Produces = p
			c.add("ability.produces")
		} else {
			c.add("ability.produces_omit:unmodelled")
		}
	}
	return a, true
}

// tilde replaces Forge's CARDNAME self-reference with "~".
func tilde(s string) string { return strings.ReplaceAll(s, "CARDNAME", "~") }

// manaPart matches one Cost$ part that is a mana symbol: a number, a colour
// or C/S/X, or a hybrid/Phyrexian pair ("W/U", "2/W", "G/P").
var manaPart = regexp.MustCompile(`^([0-9]+|[WUBRGCSX]|[WUBRGC0-9]+/[WUBRGP])$`)

// loyaltyPart matches a loyalty cost: AddCounter<N/LOYALTY> or
// SubCounter<N/LOYALTY>.
var loyaltyPart = regexp.MustCompile(`^(Add|Sub)Counter<([0-9X]+)/LOYALTY>$`)

// neutralCost normalises a Cost$ (the neutralAbility.Cost format). ok is
// false for an empty cost.
func neutralCost(cost string) (string, bool) {
	parts := costParts(cost)
	if len(parts) == 0 {
		return "", false
	}
	var out []string
	mana := ""
	flush := func() {
		if mana != "" {
			out = append(out, mana)
			mana = ""
		}
	}
	for _, p := range parts {
		switch {
		case manaPart.MatchString(p):
			mana += "{" + p + "}"
			continue
		case p == "T" || p == "Q":
			flush()
			out = append(out, "{"+p+"}")
		case loyaltyPart.MatchString(p):
			flush()
			m := loyaltyPart.FindStringSubmatch(p)
			sign := "+"
			if m[1] == "Sub" {
				sign = "-"
			}
			out = append(out, sign+m[2])
		default:
			flush()
			out = append(out, tilde(p))
		}
	}
	flush()
	return strings.Join(out, ", "), true
}

// costParts splits a Cost$ on the spaces outside <...> ("Sac<1/CARDNAME/this
// token>" is one part).
func costParts(cost string) []string {
	var out []string
	depth, start := 0, -1
	for i, r := range cost {
		switch {
		case r == '<':
			depth++
		case r == '>' && depth > 0:
			depth--
		case r == ' ' && depth == 0:
			if start >= 0 {
				out = append(out, cost[start:i])
				start = -1
			}
			continue
		}
		if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		out = append(out, cost[start:])
	}
	return out
}

// neutralProduces is the set of mana letters a mana ability's Produced$ can
// add (neutralAbility.Produces), or "any". ok is false for a production the
// script leaves to a choice the letters cannot state (Chosen, ColorIdentity,
// a Special word, ManaReflected) or a blank one.
func neutralProduces(sa *cards.SA) (string, bool) {
	if sa == nil || sa.API != "Mana" {
		return "", false
	}
	toks := strings.Fields(strings.NewReplacer("{", "", "}", "").Replace(sa.ParamStr(cards.PKProduced)))
	if len(toks) > 0 && toks[0] == "Combo" {
		toks = toks[1:]
	}
	if len(toks) == 1 && toks[0] == "Any" {
		return "any", true
	}
	if len(toks) == 0 {
		return "", false
	}
	has := map[rune]bool{}
	for _, t := range toks {
		if strings.Trim(t, "WUBRGC") != "" {
			return "", false
		}
		for _, r := range t {
			has[r] = true
		}
	}
	var b strings.Builder
	for _, r := range "WUBRGC" {
		if has[r] {
			b.WriteRune(r)
		}
	}
	return b.String(), true
}

// colourWord maps a tokenscript Colors: word to its letter.
var colourWord = map[string]string{"white": "W", "blue": "U", "black": "B", "red": "R", "green": "G"}

// neutralTokenOf is a token face's characteristics.
func neutralTokenOf(f *cards.Face, c census) neutralToken {
	t := neutralToken{Name: f.Name, Types: []string{}, Keywords: f.Keywords}
	c.add("token.filled")
	for _, w := range f.Types {
		switch {
		case chars.IsSupertype(w):
			t.Supertypes = append(t.Supertypes, w)
		case chars.IsCardType(w):
			t.Types = append(t.Types, w)
		default:
			t.Subtypes = append(t.Subtypes, w)
		}
	}
	if col, ok := tokenColours(f.Colors); ok {
		t.Colours = &col
	} else {
		c.add("token.colours_omit:" + f.Colors)
	}
	if f.PT != "" {
		p, perr := strconv.Atoi(strings.TrimSpace(strings.Split(f.PT, "/")[0]))
		parts := strings.Split(f.PT, "/")
		if len(parts) == 2 && perr == nil {
			if q, err := strconv.Atoi(strings.TrimSpace(parts[1])); err == nil {
				t.Power, t.Toughness = &p, &q
			}
		}
		if t.Power == nil {
			c.add("token.pt_omit:not_numeric")
		}
	}
	t.Text = strings.TrimSpace(strings.ReplaceAll(f.Oracle, `\n`, "\n"))
	return t
}

// tokenColours is a Colors: line's WUBRG letters. A blank line is
// colourless, as the token scripts' "no cost" mana cost gives no colour.
func tokenColours(colors string) (string, bool) {
	colors = strings.TrimSpace(strings.ToLower(colors))
	switch colors {
	case "", "colorless":
		return "", true
	case "all":
		return "WUBRG", true
	}
	has := map[string]bool{}
	for _, w := range strings.Split(colors, ",") {
		l, ok := colourWord[strings.TrimSpace(w)]
		if !ok {
			return "", false
		}
		has[l] = true
	}
	var b strings.Builder
	for _, l := range []string{"W", "U", "B", "R", "G"} {
		if has[l] {
			b.WriteString(l)
		}
	}
	return b.String(), true
}

// neutralModeOf places each pick of a modal-choice decision in the printed
// mode list of the card that poses it. why names the reason it cannot.
func neutralModeOf(reg *cards.Registry, d rules.OracleDecision) (neutralMode, string) {
	if len(d.Picks) == 0 {
		return neutralMode{}, "no_picks"
	}
	src := ""
	for _, r := range append(append([]string(nil), d.OptionRefs...), d.PickRefs...) {
		n := refCardName(r)
		if src != "" && n != src {
			return neutralMode{}, "mixed_sources"
		}
		src = n
	}
	if src == "" || strings.HasPrefix(src, "token:") {
		return neutralMode{}, "source_unresolved"
	}
	c, ok := reg.Lookup(src)
	if !ok {
		return neutralMode{}, "source_unresolved"
	}
	ords := charmOrdinals(c)
	if len(ords) == 0 {
		return neutralMode{}, "not_a_charm"
	}
	m := neutralMode{Step: d.Step}
	for _, p := range d.Picks {
		set := ords[p]
		if len(set) != 1 {
			if len(set) == 0 {
				return neutralMode{}, "label_unmatched"
			}
			return neutralMode{}, "label_ambiguous"
		}
		for o := range set {
			m.Picks = append(m.Picks, neutralModePick{Ordinal: o, Text: tilde(p)})
		}
	}
	return m, ""
}

// charmOrdinals maps each mode label of every Charm on c (a spell's SP$
// Charm, or a DB$/AB$ Charm SVar a trigger or ability executes) to the
// 0-based Choices$ positions it holds. A label at two different positions
// (two charms on one card) maps to both, so the caller can refuse it.
func charmOrdinals(c *cards.Card) map[string]map[int]bool {
	out := map[string]map[int]bool{}
	add := func(f *cards.Face, sa *cards.SA) {
		for s, d := sa, 0; s != nil && d < 16; s, d = s.Sub, d+1 {
			if s.API != "Charm" {
				continue
			}
			i := 0
			for _, name := range strings.Split(s.ParamStr(cards.PKChoices), ",") {
				if name = strings.TrimSpace(name); name == "" {
					continue
				}
				label := effects.CharmModeLabel(cards.ResolveSVar(f.SVars, name), name)
				if out[label] == nil {
					out[label] = map[int]bool{}
				}
				out[label][i] = true
				i++
			}
		}
	}
	for _, f := range c.Faces {
		if f == nil {
			continue
		}
		for _, sa := range f.Abilities {
			add(f, sa)
		}
		names := make([]string, 0, len(f.SVars))
		for n := range f.SVars {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			add(f, cards.ResolveSVar(f.SVars, n))
		}
	}
	return out
}
