package main

import (
	"bytes"
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/adjudicate"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/classify"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/cov"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/lower"
	"github.com/adams-shaun/gorge/engine/cards"
	"github.com/adams-shaun/gorge/engine/rules"
)

// DefaultClasses is the committed class register.
const DefaultClasses = "oracle/xengine/diagnosed-classes.tsv"

// keyproofArgs is `xengine keyproof`'s input; the XRows/FRows/PRows defaults
// (<play>/xmage.jsonl ...) are filled by runKeyproof, not keyproof.
type keyproofArgs struct {
	Play, XDiff, FDiff, PDiff     string
	XRows, FRows, PRows           string
	Adjudicate, Classes, Cards    string
	Measured, GorgeSHA, CorpusPin string
	XRef, FRef                    string
	Out                           string
}

func runKeyproof(args []string) error {
	fs := flag.NewFlagSet("keyproof", flag.ExitOnError)
	var a keyproofArgs
	fs.StringVar(&a.Play, "play", "", "play output dir (required)")
	fs.StringVar(&a.XDiff, "xmage", "", "xengine diff dir of the XMage follower (required)")
	fs.StringVar(&a.FDiff, "forge", "", "xengine diff dir of the Forge follower (required)")
	fs.StringVar(&a.Adjudicate, "adjudicate", "", "xengine adjudicate output dir of the same play (required)")
	fs.StringVar(&a.PDiff, "phase", "", "xengine diff dir of the phase.rs follower: a phase agreement adds +P to the refs")
	fs.StringVar(&a.XRows, "xmage-rows", "", "XMage rows (default <play>/xmage.jsonl)")
	fs.StringVar(&a.FRows, "forge-rows", "", "Forge rows (default <play>/forge.jsonl)")
	fs.StringVar(&a.PRows, "phase-rows", "", "phase.rs rows (default <play>/phase.jsonl)")
	fs.StringVar(&a.Classes, "classes", DefaultClasses, "the diagnosed-classes register")
	fs.StringVar(&a.Cards, "cards", ".cards", "corpus dir (the registry, and each card script's script_sha)")
	fs.StringVar(&a.Measured, "measured", "", "measurement date YYYY-MM-DD (required: no clock is read)")
	fs.StringVar(&a.GorgeSHA, "gorge-sha", "", "gorge commit stamped on rows (default the first 12 of GORGE_PIN)")
	fs.StringVar(&a.CorpusPin, "corpus-pin", "", "corpus pin stamped on rows")
	fs.StringVar(&a.XRef, "xmage-ref", "", "XMage ref stamped on rows")
	fs.StringVar(&a.FRef, "forge-ref", "", "Forge ref stamped on rows")
	fs.StringVar(&a.Out, "out", "", "output dir, outside the repository (required)")
	fs.Parse(args)
	if a.Play == "" || a.XDiff == "" || a.FDiff == "" || a.Adjudicate == "" || a.Out == "" || a.Measured == "" {
		return fmt.Errorf("keyproof needs -play, -xmage, -forge, -adjudicate, -measured and -out")
	}
	if a.XRows == "" {
		a.XRows = filepath.Join(a.Play, "xmage.jsonl")
	}
	if a.FRows == "" {
		a.FRows = filepath.Join(a.Play, "forge.jsonl")
	}
	if a.PRows == "" {
		a.PRows = filepath.Join(a.Play, "phase.jsonl")
	}
	if a.GorgeSHA == "" {
		if b, err := os.ReadFile("GORGE_PIN"); err == nil {
			a.GorgeSHA = strings.TrimSpace(string(b))
			a.GorgeSHA = a.GorgeSHA[:min(12, len(a.GorgeSHA))]
		}
	}
	reg, err := loadReg(a.Cards)
	if err != nil {
		return err
	}
	return keyproof(reg, a)
}

// ---- the play's lowering: tapes, step ranges, a diverging step's keys ----

// playKeys reads a play dir's lower.jsonl and tapes on demand.
type playKeys struct {
	reg   *cards.Registry
	play  string
	lower map[string]lowerRow
	tapes map[string]*game.Tape
}

func newPlayKeys(reg *cards.Registry, play string) (*playKeys, error) {
	p := &playKeys{reg: reg, play: play, lower: map[string]lowerRow{}, tapes: map[string]*game.Tape{}}
	err := readLines(filepath.Join(play, "lower.jsonl"), func(b []byte) error {
		var r lowerRow
		if err := json.Unmarshal(b, &r); err != nil {
			return err
		}
		p.lower[r.ID] = r
		return nil
	})
	return p, err
}

// tape is the tape of lower_ok item id (nil, false: it has none).
func (p *playKeys) tape(id string) (*game.Tape, lowerRow, bool, error) {
	l, ok := p.lower[id]
	if !ok || l.Status != lower.StatusOK || l.Tape == "" {
		return nil, l, false, nil
	}
	if tp, ok := p.tapes[id]; ok {
		return tp, l, true, nil
	}
	b, err := os.ReadFile(filepath.Join(p.play, l.Tape))
	if err != nil {
		return nil, l, false, err
	}
	tp, err := game.ReadTape(bytes.NewReader(b))
	if err != nil {
		return nil, l, false, fmt.Errorf("%s: %w", l.Tape, err)
	}
	p.tapes[id] = tp
	return tp, l, true, nil
}

// credited is the keys an item that carried out its whole lowered range
// exercised (the range diff credits to compared.tsv).
func (p *playKeys) credited(it oraclegen.Item, id string) ([]classify.Use, bool, error) {
	tp, l, ok, err := p.tape(id)
	if !ok || err != nil {
		return nil, false, err
	}
	lo, hi := classify.LoweredRange(l.lowered(it), tp)
	return classify.Sorted(classify.Uses(p.reg, tp, lo, hi, true)), true, nil
}

// stepKeys is the signature input of finding f and every key its diverging
// step exercised: the entries after the previous step's last entry up to this
// step's own last entry (lowerRow.StepEntry), with the pre-first-decision mana
// abilities when the step is the first or setup. The first key names Family,
// Desc and Cost.
func (p *playKeys) stepKeys(f adjudicate.Finding) (adjudicate.SigInput, []classify.Use, error) {
	step := f.Diverging.Step
	op := "setup"
	if step >= 0 && step < len(f.Item.Steps) {
		op = f.Item.Steps[step].Op
	}
	tp, l, ok, err := p.tape(f.ID)
	if err != nil {
		return adjudicate.SigInput{}, nil, err
	}
	if !ok {
		return adjudicate.SigInput{}, nil, fmt.Errorf("%s: a finding without a lowered tape in %s", f.ID, p.play)
	}
	lo, hi := 0, len(tp.Entries)-1
	for k := min(step, len(l.StepEntry)) - 1; k >= 0; k-- {
		if l.StepEntry[k] >= 0 {
			lo = l.StepEntry[k] + 1
			break
		}
	}
	for j := max(step, 0); j < len(l.StepEntry); j++ {
		if l.StepEntry[j] >= 0 {
			hi = l.StepEntry[j]
			break
		}
	}
	_, lhi := classify.LoweredRange(l.lowered(f.Item), tp)
	hi = min(hi, lhi)
	uses := classify.Sorted(classify.Uses(p.reg, tp, lo, hi, step <= 0))
	k := adjudicate.NoKey
	if len(uses) > 0 {
		k = p.describe(uses[0])
	}
	return adjudicate.InputOf(f, k, op), uses, nil
}

var manaWord = regexp.MustCompile(`^[0-9WUBRGCXSP/]+$`)

// describe is the Family, Desc and Cost of a coverage key.
func (p *playKeys) describe(u classify.Use) adjudicate.KeyDesc {
	k := adjudicate.KeyDesc{Desc: "-", Cost: "-"}
	c, ok := p.reg.Lookup(u.Card)
	if !ok {
		k.Family = "none"
		return k
	}
	for _, ck := range cov.Keys(c) {
		if ck.ID == u.Key {
			k.Desc = ck.Desc
		}
	}
	switch {
	case u.Key == "cast" || u.Key == "land":
		k.Family = u.Key
	case strings.Contains(u.Key, "/t"):
		k.Family = "trigger"
	default:
		k.Family = "activate"
		var face, i int
		if _, err := fmt.Sscanf(u.Key, "f%d/a%d", &face, &i); err == nil && face < len(c.Faces) && c.Faces[face] != nil && i < len(c.Faces[face].Abilities) {
			if a := c.Faces[face].Abilities[i]; a != nil {
				k.Cost = costWords(a.Params["Cost"])
			}
		}
	}
	return k
}

// costWords is an ability cost without its mana: the cost words, each stripped
// of its <...> argument, sorted, deduplicated and joined by "+".
func costWords(cost string) string {
	seen := map[string]bool{}
	for _, w := range strings.Fields(cost) {
		if manaWord.MatchString(w) {
			continue
		}
		if i := strings.IndexByte(w, '<'); i >= 0 {
			w = w[:i]
		}
		if w != "" {
			seen[w] = true
		}
	}
	if len(seen) == 0 {
		return "-"
	}
	ws := make([]string, 0, len(seen))
	for w := range seen {
		ws = append(ws, w)
	}
	sort.Strings(ws)
	return strings.Join(ws, "+")
}

// diagnoseFindings is runAdjudicate's use of the class register.
func diagnoseFindings(reg *cards.Registry, play string, res *adjudicate.Result, rows []adjudicate.ClassRow) error {
	p, err := newPlayKeys(reg, play)
	if err != nil {
		return err
	}
	var first error
	adjudicate.Diagnose(res, rows, func(f adjudicate.Finding) adjudicate.SigInput {
		s, _, err := p.stepKeys(f)
		if err != nil && first == nil {
			first = err
		}
		return s
	})
	return first
}

// ---- keyproof ----

type kpDiv struct {
	sig, cite, ticket string
	diagnosed         bool
}

// kpKey is everything the items said about one (card, key).
type kpKey struct {
	agree map[string]bool
	div   map[string]kpDiv
	owed  map[string]string
}

type kpUse struct {
	Card string `json:"card"`
	Key  string `json:"key"`
}

type kpWitness struct {
	Format   string             `json:"format"`
	ID       string             `json:"id"`
	Keys     []kpUse            `json:"keys"`
	Refs     string             `json:"refs"`
	GorgeSHA string             `json:"gorge_sha"`
	Item     oraclegen.Item     `json:"item"`
	Gorge    rules.OracleResult `json:"gorge"`
}

// A signature's divergent items, for handoff.tsv and sigs/<sig>.tsv.
type kpSig struct {
	input adjudicate.SigInput
	items []kpSigItem
}

type kpSigItem struct {
	id, card string
	steps    int
	keys     []classify.Use
}

const keyproofHeader = "card\toracle_id\tkey\tdesc\tstate\treason\trefs\tsource\tagree_items\tdiff_items\twitness\tsig\tcite\tticket\tgorge_sha\tcorpus_pin\tscript_sha\txmage_ref\tforge_ref\tmeasured\twriter"

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func keyproof(reg *cards.Registry, a keyproofArgs) error {
	if err := classify.RefuseRepoPath(a.Out); err != nil {
		return err
	}
	in, err := adjudicate.Load(a.Play, a.XDiff, a.FDiff, a.XRows, a.FRows)
	if err != nil {
		return err
	}
	if a.PDiff != "" {
		if err := adjudicate.LoadPhase(&in, a.PDiff, a.PRows); err != nil {
			return err
		}
	}
	classes, err := adjudicate.ReadClasses(a.Classes)
	if err != nil {
		return err
	}
	diverging := map[string]adjudicate.Row{}
	if err := readLines(filepath.Join(a.Adjudicate, "findings.jsonl"), func(b []byte) error {
		var r adjudicate.Row
		if err := json.Unmarshal(b, &r); err != nil {
			return err
		}
		diverging[r.ID] = r
		return nil
	}); err != nil {
		return err
	}
	p, err := newPlayKeys(reg, a.Play)
	if err != nil {
		return err
	}

	var ids []string
	for id := range in.XFind {
		if _, ok := in.FFind[id]; ok {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)

	keys := map[classify.Use]*kpKey{}
	keyOf := func(u classify.Use) *kpKey {
		k := keys[u]
		if k == nil {
			k = &kpKey{agree: map[string]bool{}, div: map[string]kpDiv{}, owed: map[string]string{}}
			keys[u] = k
		}
		return k
	}
	sigs := map[string]*kpSig{}
	var witnesses []kpWitness
	// itemRefs of the agreeing items, for the witness.
	type engines struct{ x, f, p bool }
	who := map[string]engines{}

	for _, id := range ids {
		item, ok := in.Items[id]
		if !ok {
			continue
		}
		xc, fc := in.XFind[id].Class, in.FFind[id].Class
		if row, isDiv := diverging[id]; isDiv {
			f, ok := adjudicate.Rebuild(in, row)
			if !ok {
				return fmt.Errorf("%s: in findings.jsonl but not in the play's items", id)
			}
			si, uses, err := p.stepKeys(f)
			if err != nil {
				return err
			}
			sig := adjudicate.KeySignature(si)
			d := kpDiv{sig: sig}
			if c, hit := adjudicate.Match(classes, si); hit {
				d.diagnosed, d.cite, d.ticket = true, cmp.Or(onlyIf(c.CR), c.Upstream), c.Ticket
			}
			for _, u := range uses {
				keyOf(u).div[id] = d
			}
			if !d.diagnosed {
				s := sigs[sig]
				if s == nil {
					s = &kpSig{input: si}
					sigs[sig] = s
				}
				s.items = append(s.items, kpSigItem{id: id, card: row.Card, steps: f.Steps, keys: uses})
			}
			continue
		}
		uses, ok, err := p.credited(item, id)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		refs, xa, fa := "", false, false
		switch {
		case xc == classify.ClassAgree && fc == classify.ClassAgree:
			refs, xa, fa = "FX", true, true
		case xc == classify.ClassAgree && (fc == classify.ClassHarness || fc == classify.ClassTurnCap || fc == classify.ClassForgeSkipsDamageStep):
			refs, xa = "X", true
		case xc == classify.ClassXMageLacks && fc == classify.ClassAgree:
			refs, fa = "F", true
		}
		if refs != "" {
			pa := in.PFind != nil && in.PFind[id].Class == classify.ClassAgree
			if pa {
				refs += "+P"
			}
			for _, u := range uses {
				keyOf(u).agree[id] = true
			}
			who[id] = engines{xa, fa, pa}
			w := kpWitness{Format: "xproof-witness-1", ID: id, Refs: refs, GorgeSHA: dash(a.GorgeSHA), Item: item, Gorge: in.Gorge[id], Keys: []kpUse{}}
			for _, u := range uses {
				w.Keys = append(w.Keys, kpUse{u.Card, u.Key})
			}
			witnesses = append(witnesses, w)
			continue
		}
		reason := "harness"
		switch {
		case xc == classify.ClassRNG || fc == classify.ClassRNG:
			reason = "rng"
		case xc == classify.ClassNotSent && fc == classify.ClassAgree:
			reason = "not-sent"
		}
		for _, u := range uses {
			keyOf(u).owed[id] = reason
		}
	}

	// one row per key
	var useList []classify.Use
	for u := range keys {
		useList = append(useList, u)
	}
	sort.Slice(useList, func(i, j int) bool {
		if useList[i].Card != useList[j].Card {
			return useList[i].Card < useList[j].Card
		}
		return useList[i].Key < useList[j].Key
	})
	shas := map[string]string{}
	scriptSHA := func(card string) string {
		if s, ok := shas[card]; ok {
			return s
		}
		s := "-"
		if c, ok := reg.Lookup(card); ok {
			for _, path := range []string{c.Path, filepath.Join(a.Cards, "cardsfolder", c.Path), filepath.Join(a.Cards, c.Path)} {
				if b, err := os.ReadFile(path); err == nil {
					h := sha256.Sum256(b)
					s = hex.EncodeToString(h[:])[:12]
					break
				}
			}
		}
		shas[card] = s
		return s
	}
	var tsv bytes.Buffer
	tsv.WriteString(keyproofHeader + "\n")
	hist := map[string]int{}
	for _, u := range useList {
		k := keys[u]
		desc := "-"
		if c, ok := reg.Lookup(u.Card); ok {
			for _, ck := range cov.Keys(c) {
				if ck.ID == u.Key {
					desc = ck.Desc
				}
			}
		}
		state, reason, refs, witness, sig, cite, ticket := "", "-", "-", "-", "-", "-", "-"
		var deciding string
		var divIDs, diagIDs []string
		for id, d := range k.div {
			if d.diagnosed {
				diagIDs = append(diagIDs, id)
			} else {
				divIDs = append(divIDs, id)
			}
		}
		sort.Strings(divIDs)
		sort.Strings(diagIDs)
		switch {
		case len(divIDs) > 0:
			state, deciding = "diverged", divIDs[0]
			sig = k.div[deciding].sig
		case len(k.agree) > 0:
			state = "agreed"
			var x, f, ph []string
			for id := range k.agree {
				w := who[id]
				if w.x {
					x = append(x, id)
				}
				if w.f {
					f = append(f, id)
				}
				if w.p {
					ph = append(ph, id)
				}
			}
			sort.Strings(x)
			sort.Strings(f)
			sort.Strings(ph)
			refs = map[[2]bool]string{{true, true}: "FX", {true, false}: "X", {false, true}: "F"}[[2]bool{len(x) > 0, len(f) > 0}]
			var parts []string
			if len(x) > 0 {
				parts = append(parts, "x:"+x[0])
			}
			if len(f) > 0 {
				parts = append(parts, "f:"+f[0])
			}
			if len(ph) > 0 {
				refs += "+P"
				parts = append(parts, "p:"+ph[0])
			}
			witness = strings.Join(parts, ",")
			var all []string
			for id := range k.agree {
				all = append(all, id)
			}
			sort.Strings(all)
			deciding = all[0]
		case len(diagIDs) > 0:
			state, deciding = "diagnosed", diagIDs[0]
			d := k.div[deciding]
			sig, cite, ticket = d.sig, d.cite, d.ticket
		default:
			state = "owed"
			count := map[string]int{}
			var all []string
			for id, r := range k.owed {
				count[r]++
				all = append(all, id)
			}
			sort.Strings(all)
			deciding = all[0]
			best := ""
			for r, n := range count {
				if best == "" || n > count[best] || (n == count[best] && r < best) {
					best = r
				}
			}
			reason = best
		}
		hist[state]++
		source := "steps"
		switch {
		case strings.HasPrefix(deciding, "xs:"):
			source = "start"
		case strings.HasPrefix(deciding, "xe:"):
			source = "game"
		}
		cells := []string{u.Card, "-", u.Key, desc, state, reason, refs, source,
			fmt.Sprint(len(k.agree)), fmt.Sprint(len(k.div)), witness, sig, cite, ticket,
			dash(a.GorgeSHA), dash(a.CorpusPin), scriptSHA(u.Card), dash(a.XRef), dash(a.FRef), a.Measured, "xproof"}
		tsv.WriteString(strings.Join(cells, "\t") + "\n")
	}

	// handoff.tsv and sigs/
	type handoff struct {
		sig, pattern, class string
		keys, cards, items  int
		prio                string
	}
	var hand []handoff
	files := map[string][]byte{}
	for sig, s := range sigs {
		keySet, cardSet := map[classify.Use]bool{}, map[string]bool{}
		slices.SortFunc(s.items, func(x, y kpSigItem) int { return cmp.Or(cmp.Compare(x.steps, y.steps), cmp.Compare(x.id, y.id)) })
		var sb bytes.Buffer
		sb.WriteString("card\tkey\titem\tsteps\n")
		for _, it := range s.items {
			if it.card != "" {
				cardSet[it.card] = true
			}
			if len(it.keys) == 0 {
				fmt.Fprintf(&sb, "%s\t-\t%s\t%d\n", dash(it.card), it.id, it.steps)
			}
			for _, u := range it.keys {
				keySet[u] = true
				fmt.Fprintf(&sb, "%s\t%s\t%s\t%d\n", u.Card, u.Key, it.id, it.steps)
			}
		}
		files[filepath.Join("sigs", sig+".tsv")] = sb.Bytes()
		h := handoff{sig: sig, pattern: s.input.Pattern, class: s.input.Follower, keys: len(keySet), cards: len(cardSet), items: len(s.items), prio: "P2"}
		if s.input.Pattern == "FX|G" {
			h.prio = "P1"
		}
		hand = append(hand, h)
	}
	sort.Slice(hand, func(i, j int) bool {
		x, y := hand[i], hand[j]
		if x.prio != y.prio {
			return x.prio < y.prio
		}
		if x.keys != y.keys {
			return x.keys > y.keys
		}
		return x.sig < y.sig
	})
	var hb bytes.Buffer
	hb.WriteString("sig\tpattern\tclass\tkeys\tcards\titems\tprio\tcite\n")
	for _, h := range hand {
		fmt.Fprintf(&hb, "%s\t%s\t%s\t%d\t%d\t%d\t%s\t-\n", h.sig, h.pattern, h.class, h.keys, h.cards, h.items, h.prio)
	}
	var wb bytes.Buffer
	sort.Slice(witnesses, func(i, j int) bool { return witnesses[i].ID < witnesses[j].ID })
	for _, w := range witnesses {
		b, err := jsonl(w)
		if err != nil {
			return err
		}
		wb.Write(b)
	}
	files["keyproof.tsv"] = tsv.Bytes()
	files["handoff.tsv"] = hb.Bytes()
	files["witness.jsonl"] = wb.Bytes()
	var names []string
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if err := writeAtomic(filepath.Join(a.Out, n), files[n]); err != nil {
			return err
		}
	}
	fmt.Printf("keyproof: %d key(s) into %s:", len(useList), a.Out)
	for _, s := range []string{"agreed", "diverged", "diagnosed", "owed"} {
		fmt.Printf(" %s=%d", s, hist[s])
	}
	fmt.Printf("; %d signature(s) to hand off, %d witness(es)\n", len(hand), len(witnesses))
	return nil
}

// onlyIf is "" for the "-" a register cell uses for nothing.
func onlyIf(s string) string {
	if s == "-" {
		return ""
	}
	return s
}
