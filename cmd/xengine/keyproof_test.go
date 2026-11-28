package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclediff"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/adjudicate"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/classify"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/game"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/lower"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/scen"
	"github.com/adams-shaun/gorge/engine/cards"
)

// stampRows rewrites a follower row file with "engine":"<engine>" on every row.
func stampRows(t *testing.T, in, out, engine string) {
	t.Helper()
	var stamped []byte
	if err := readLines(in, func(b []byte) error {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(b, &m); err != nil {
			return err
		}
		m["engine"] = json.RawMessage(`"` + engine + `"`)
		nb, _ := json.Marshal(m)
		stamped = append(append(stamped, nb...), '\n')
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, stamped, 0o644); err != nil {
		t.Fatal(err)
	}
}

func readTSV(t *testing.T, path string) (header []string, rows []map[string]string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for i, ln := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		cells := strings.Split(ln, "\t")
		if i == 0 {
			header = cells
			continue
		}
		if len(cells) != len(header) {
			t.Fatalf("%s line %d: %d cells, header has %d", path, i+1, len(cells), len(header))
		}
		m := map[string]string{}
		for j, h := range header {
			m[h] = cells[j]
		}
		rows = append(rows, m)
	}
	return header, rows
}

// kpFixture is a play whose first item agrees and whose diverging item (fx.div:
// the second of a two-item play, else the first item with a lowered step that
// exercises a key the first did not) both followers diverge on identically
// (FX|G), adjudicated, ready for keyproof.
type kpFixture struct {
	reg        *cards.Registry
	play, tmp  string
	ids        []string
	xd, fd, ad string
	xrows      string
	frows      string
	lower      map[string]lowerRow
	div        string // the diverging item (the second of a two-item play)
	step       int    // its diverging step
}

func newKpFixture(t *testing.T) *kpFixture { return newKpFixtureMode(t, kpBoth) }

// How the followers' rows treat the fixture's second item.
type kpMode int

const (
	kpBoth       kpMode = iota // both followers differ from gorge identically: FX|G
	kpXMageOnly                // XMage differs, Forge agrees: GF|X
	kpForgeOnly                // Forge differs, XMage agrees: GX|F
	kpXMageQuiet               // XMage has no answer (a harness row), Forge agrees: no finding
)

func newKpFixtureMode(t *testing.T, mode kpMode) *kpFixture {
	t.Helper()
	reg, play := playTwo(t)
	xprepPlayDir(t, reg, play)
	return newKpFixtureFrom(t, mode, reg, play)
}

// newKpFixtureFrom is the fixture over any play dir whose items diff to the
// mode's verdicts: playTwo's xe: tape items, or lowerKeyStarts' xs: start
// items (the only ones that declare a key, so the only ones that can name a
// key when keyproof drops them).
func newKpFixtureFrom(t *testing.T, mode kpMode, reg *cards.Registry, play string) *kpFixture {
	t.Helper()
	tmp := t.TempDir()
	raw := filepath.Join(tmp, "raw.jsonl")
	ids := goldenRows(t, play, raw, nil)
	if len(ids) < 2 {
		t.Skip("play kept fewer than two lower_ok items")
	}
	fx := &kpFixture{reg: reg, play: play, tmp: tmp, ids: ids, lower: map[string]lowerRow{}, step: -1}
	if err := readLines(filepath.Join(play, "lower.jsonl"), func(b []byte) error {
		var r lowerRow
		if err := json.Unmarshal(b, &r); err != nil {
			return err
		}
		fx.lower[r.ID] = r
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// The diverging item is the first (after the agreeing first item) with a
	// lowered step that exercised a key the first item did not (an agreement
	// outranks a diagnosis, so a class could only clear such a key): the
	// second item of a two-item play, the interesting start of a batch.
	divStep := func(id string) int {
		l := fx.lower[id]
		hi := len(l.StepEntry) - 1
		if l.Truncated != "" {
			hi = 0
			for hi+1 < len(l.StepEntry) && l.StepEntry[hi+1] < l.TruncatedAt {
				hi++
			}
		}
		for s := 1; s <= hi; s++ {
			if l.StepEntry[s] < 0 {
				continue
			}
			for u := range fx.stepUsesAt(t, id, s) {
				if _, shared := fx.full(t, ids[0])[u]; !shared {
					return s
				}
			}
		}
		return -1
	}
	for _, id := range ids[1:] {
		if s := divStep(id); s >= 0 {
			fx.div, fx.step = id, s
			break
		}
	}
	if fx.step < 0 {
		t.Skip("no lowered step of any item but the first exercised a key the first did not")
	}
	differ := func(i int, x *oraclediff.XResult) {
		if ids[i] == fx.div {
			x.Snapshots[fx.step+1].Players[0].Life++
		}
	}
	quiet := func(i int, x *oraclediff.XResult) {
		if ids[i] == fx.div {
			x.Snapshots, x.Harness = nil, "driver: no answer"
		}
	}
	xEdit, fEdit := differ, differ
	switch mode {
	case kpXMageOnly:
		fEdit = nil
	case kpForgeOnly:
		xEdit = nil
	case kpXMageQuiet:
		xEdit, fEdit = quiet, nil
	}
	rawX, rawF := filepath.Join(tmp, "rawx.jsonl"), filepath.Join(tmp, "rawf.jsonl")
	goldenRows(t, play, rawX, xEdit)
	goldenRows(t, play, rawF, fEdit)
	fx.xrows = filepath.Join(tmp, "xrows.jsonl")
	fx.frows = filepath.Join(tmp, "frows.jsonl")
	stampRows(t, rawX, fx.xrows, "xmage")
	stampRows(t, rawF, fx.frows, "forge")
	fx.xd, fx.fd, fx.ad = filepath.Join(tmp, "xd"), filepath.Join(tmp, "fd"), filepath.Join(tmp, "ad")
	if err := diff(reg, play, "xmage", fx.xrows, fx.xd, ""); err != nil {
		t.Fatal(err)
	}
	if err := diff(reg, play, "forge", fx.frows, fx.fd, ""); err != nil {
		t.Fatal(err)
	}
	in, err := adjudicate.Load(play, fx.xd, fx.fd, fx.xrows, fx.frows)
	if err != nil {
		t.Fatal(err)
	}
	res := adjudicate.Run(in)
	want := map[kpMode]oraclediff.Pattern{kpBoth: oraclediff.ForgeXMageG, kpXMageOnly: oraclediff.GorgeForgeX, kpForgeOnly: oraclediff.GorgeXMageF}
	if p, ok := want[mode]; !ok {
		if len(res.Rows) != 0 {
			t.Fatalf("adjudication rows %+v, want none", res.Rows)
		}
	} else if len(res.Rows) != 1 || res.Rows[0].ID != fx.div || res.Rows[0].Pattern != string(p) {
		t.Fatalf("adjudication rows %+v, want one %s row for %s", res.Rows, p, fx.div)
	}
	if err := adjudicate.Write(fx.ad, res); err != nil {
		t.Fatal(err)
	}
	return fx
}

func (fx *kpFixture) args(out, classes string) keyproofArgs {
	return keyproofArgs{
		Play: fx.play, XDiff: fx.xd, FDiff: fx.fd, XRows: fx.xrows, FRows: fx.frows,
		Adjudicate: fx.ad, Classes: classes, Cards: "../../.cards", Measured: "2026-11-23", Out: out,
	}
}

func (fx *kpFixture) tape(t *testing.T, id string) *game.Tape {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(fx.play, fx.lower[id].Tape))
	if err != nil {
		t.Fatal(err)
	}
	tp, err := game.ReadTape(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return tp
}

// full is the credit range of an agreeing item: the whole lowered tape.
func (fx *kpFixture) full(t *testing.T, id string) map[classify.Use]int {
	tp := fx.tape(t, id)
	lo, hi := classify.LoweredRange(fx.lower[id].lowered(oraclegen.Item{}), tp)
	return classify.Uses(fx.reg, tp, lo, hi, true)
}

// stepUses is the uses of the second item's diverging step.
func (fx *kpFixture) stepUses(t *testing.T) map[classify.Use]int {
	return fx.stepUsesAt(t, fx.div, fx.step)
}

// stepUsesAt is the uses of item id's step: the tape entries after the
// previous step's last one up to its own last one.
func (fx *kpFixture) stepUsesAt(t *testing.T, id string, step int) map[classify.Use]int {
	tp, l := fx.tape(t, id), fx.lower[id]
	lo, hi := 0, l.StepEntry[step]
	if step > 0 {
		lo = l.StepEntry[step-1] + 1
	}
	return classify.Uses(fx.reg, tp, lo, hi, step <= 0)
}

var hex12 = regexp.MustCompile(`^[0-9a-f]{12}$`)

func TestKeyproofAgreeAndDiverge(t *testing.T) {
	t.Parallel()
	fx := newKpFixture(t)
	out := filepath.Join(fx.tmp, "out")
	if err := keyproof(fx.reg, fx.args(out, "")); err != nil {
		t.Fatal(err)
	}
	wantHeader := "card\toracle_id\tkey\tdesc\tstate\treason\trefs\tsource\tagree_items\tdiff_items\twitness\tsig\tcite\tticket\tgorge_sha\tcorpus_pin\tscript_sha\txmage_ref\tforge_ref\tmeasured\twriter"
	if b, _ := os.ReadFile(filepath.Join(out, "keyproof.tsv")); !strings.HasPrefix(string(b), wantHeader+"\n") {
		t.Fatalf("keyproof.tsv header:\n%s", strings.SplitN(string(b), "\n", 2)[0])
	}
	_, rows := readTSV(t, filepath.Join(out, "keyproof.tsv"))
	byKey := map[classify.Use]map[string]string{}
	for _, r := range rows {
		byKey[classify.Use{Card: r["card"], Key: r["key"]}] = r
		if r["writer"] != "xproof" || r["measured"] != "2026-11-23" {
			t.Errorf("%s %s: writer %q measured %q", r["card"], r["key"], r["writer"], r["measured"])
		}
	}
	id0, id1 := fx.ids[0], fx.ids[1]
	diverging := fx.stepUses(t)
	uses1 := fx.full(t, id1)

	// agreeing items: the smallest agreeing id per key is the witness.
	smallest := map[classify.Use]string{}
	for _, id := range fx.ids {
		if id == id1 {
			continue
		}
		for u := range fx.full(t, id) {
			if s, ok := smallest[u]; !ok || id < s {
				smallest[u] = id
			}
		}
	}
	if len(fx.full(t, id0)) == 0 {
		t.Fatal("item 0 exercised no key")
	}
	agreedN := 0
	for u := range smallest {
		if _, d := diverging[u]; d {
			continue // a divergence outranks any agreement
		}
		r, ok := byKey[u]
		if !ok || r["state"] != "agreed" || r["refs"] != "FX" {
			t.Errorf("%v: %v, want agreed FX", u, r)
			continue
		}
		agreedN++
		if want := "x:" + smallest[u] + ",f:" + smallest[u]; r["witness"] != want {
			t.Errorf("%v: witness %q, want %q", u, r["witness"], want)
		}
	}
	if agreedN == 0 {
		t.Error("no agreed key")
	}
	var sig string
	for u := range diverging {
		r := byKey[u]
		if r["state"] != "diverged" || !hex12.MatchString(r["sig"]) {
			t.Errorf("%v: %v, want diverged with a 12-hex sig", u, r)
			continue
		}
		if sig != "" && sig != r["sig"] {
			t.Errorf("one diverging item gave two sigs %s, %s", sig, r["sig"])
		}
		sig = r["sig"]
		if r["diff_items"] != "1" {
			t.Errorf("%v: diff_items %q", u, r["diff_items"])
		}
	}
	// Contamination: a diverging item credits nothing outside its diverging step.
	for u := range uses1 {
		if _, d := diverging[u]; d {
			continue
		}
		if _, shared := smallest[u]; shared {
			continue
		}
		if r, ok := byKey[u]; ok {
			t.Errorf("%v: only the diverging item exercised it outside its step, but has row %v", u, r)
		}
	}

	_, hand := readTSV(t, filepath.Join(out, "handoff.tsv"))
	if len(hand) != 1 || hand[0]["pattern"] != "FX|G" || hand[0]["prio"] != "P1" || hand[0]["cite"] != "-" {
		t.Fatalf("handoff.tsv rows %v", hand)
	}
	if len(diverging) > 0 {
		if !hex12.MatchString(hand[0]["sig"]) || hand[0]["sig"] != sig {
			t.Errorf("handoff sig %q, rows' sig %q", hand[0]["sig"], sig)
		}
		if _, err := os.Stat(filepath.Join(out, "sigs", hand[0]["sig"]+".tsv")); err != nil {
			t.Error(err)
		}
	}
	nw := 0
	if err := readLines(filepath.Join(out, "witness.jsonl"), func(b []byte) error {
		var w struct {
			Format string `json:"format"`
			ID     string `json:"id"`
			Refs   string `json:"refs"`
		}
		if err := json.Unmarshal(b, &w); err != nil {
			return err
		}
		if w.Format != "xproof-witness-1" || w.ID == id1 || w.Refs != "FX" {
			t.Errorf("witness line %s", b)
		}
		nw++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if nw != len(fx.ids)-1 {
		t.Errorf("witness.jsonl has %d lines, want one per agreeing item (%d)", nw, len(fx.ids)-1)
	}
}

func TestKeyproofClassClears(t *testing.T) {
	t.Parallel()
	fx := newKpFixture(t)
	first := filepath.Join(fx.tmp, "first")
	if err := keyproof(fx.reg, fx.args(first, "")); err != nil {
		t.Fatal(err)
	}
	_, hand := readTSV(t, filepath.Join(first, "handoff.tsv"))
	if len(hand) != 1 {
		t.Fatalf("handoff rows %v", hand)
	}
	classes := filepath.Join(fx.tmp, "classes.tsv")
	reg := "sig\tpattern\tfield\tdesc\tcost\tcr\tupstream\tticket\n" +
		hand[0]["sig"] + "\tFX|G\t*\t*\t*\t702.4b\t-\tdiag-1\n"
	if err := os.WriteFile(classes, []byte(reg), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(fx.tmp, "second")
	if err := keyproof(fx.reg, fx.args(out, classes)); err != nil {
		t.Fatal(err)
	}
	_, rows := readTSV(t, filepath.Join(out, "keyproof.tsv"))
	n := 0
	other := fx.full(t, fx.ids[0])
	for _, r := range rows {
		u := classify.Use{Card: r["card"], Key: r["key"]}
		if _, shared := other[u]; shared && r["state"] != "agreed" {
			t.Errorf("%v: agreed by another item, state %s", u, r["state"])
		}
		if r["state"] == "diverged" {
			t.Errorf("%s %s still diverged", r["card"], r["key"])
		}
		if r["state"] == "diagnosed" {
			n++
			if r["sig"] != hand[0]["sig"] || r["cite"] != "702.4b" || r["ticket"] != "diag-1" {
				t.Errorf("diagnosed row %v", r)
			}
		}
	}
	if n == 0 {
		t.Error("no key turned diagnosed")
	}
	if b, _ := os.ReadFile(filepath.Join(out, "handoff.tsv")); strings.Count(string(b), "\n") != 1 {
		t.Errorf("handoff.tsv not empty:\n%s", b)
	}
}

func TestKeyproofCostWords(t *testing.T) {
	t.Parallel()
	for cost, want := range map[string]string{
		"T":                       "T",
		"2 G T":                   "T",
		"Sac<1/CARDNAME> T":       "Sac+T",
		"1 B Forage T Forage":     "Forage+T",
		"2 W":                     "-",
		"":                        "-",
		"tapXType<1/Creature> 3U": "tapXType",
	} {
		if got := costWords(cost); got != want {
			t.Errorf("costWords(%q) = %q, want %q", cost, got, want)
		}
	}
}

// adjudicate -classes: a row matching the diverging item's signature labels
// the finding and its row; no rows change nothing.
func TestAdjudicateDiagnoses(t *testing.T) {
	t.Parallel()
	fx := newKpFixture(t)
	in, err := adjudicate.Load(fx.play, fx.xd, fx.fd, fx.xrows, fx.frows)
	if err != nil {
		t.Fatal(err)
	}
	res := adjudicate.Run(in)
	if err := diagnoseFindings(fx.reg, fx.play, &res, nil); err != nil || res.Rows[0].Diagnosed != "" {
		t.Fatalf("no rows: %v, %+v", err, res.Rows[0])
	}
	rows := []adjudicate.ClassRow{{Sig: "0123456789ab", Pattern: "FX|G", Field: "*", Desc: "*", Cost: "*", CR: "702.4b", Upstream: "-", Ticket: "t"}}
	if err := diagnoseFindings(fx.reg, fx.play, &res, rows); err != nil {
		t.Fatal(err)
	}
	if res.Rows[0].Diagnosed != "0123456789ab" || res.Findings[0].Diagnosed != "0123456789ab" {
		t.Errorf("not diagnosed: %+v", res.Rows[0])
	}
}

// keyRows runs keyproof on fx into out and returns its rows by key and the
// handoff rows.
func (fx *kpFixture) keyRows(t *testing.T, out string) (map[classify.Use]map[string]string, []map[string]string) {
	t.Helper()
	if err := keyproof(fx.reg, fx.args(out, "")); err != nil {
		t.Fatal(err)
	}
	_, rows := readTSV(t, filepath.Join(out, "keyproof.tsv"))
	byKey := map[classify.Use]map[string]string{}
	for _, r := range rows {
		byKey[classify.Use{Card: r["card"], Key: r["key"]}] = r
	}
	_, hand := readTSV(t, filepath.Join(out, "handoff.tsv"))
	return byKey, hand
}

// onlySecond is the keys of the second item's diverging step no other item
// exercised: the keys whose state the second item alone decides.
func (fx *kpFixture) onlySecond(t *testing.T) []classify.Use {
	t.Helper()
	var out []classify.Use
	for u := range fx.stepUses(t) {
		shared := false
		for _, id := range fx.ids {
			if _, ok := fx.full(t, id)[u]; ok && id != fx.div {
				shared = true
			}
		}
		if !shared {
			out = append(out, u)
		}
	}
	if len(out) == 0 {
		t.Fatal("no key of the diverging step is the second item's alone")
	}
	return out
}

// Forge leads (operator 11-24): XMage alone differing leaves the key agreed with
// refs F, and its signature is still handed off as a follower-only (GF|X, P2) row.
func TestKeyproofXMageOnlyDiffersCreditsForge(t *testing.T) {
	t.Parallel()
	fx := newKpFixtureMode(t, kpXMageOnly)
	byKey, hand := fx.keyRows(t, filepath.Join(fx.tmp, "out"))
	for _, u := range fx.onlySecond(t) {
		r := byKey[u]
		if r["state"] != "agreed" || r["refs"] != "F" || r["witness"] != "f:"+fx.ids[1] {
			t.Errorf("%v: %v, want agreed F witnessed by %s", u, r, fx.ids[1])
		}
	}
	for u, r := range byKey {
		if r["state"] != "agreed" {
			t.Errorf("%v: state %s, want every key agreed", u, r["state"])
		}
	}
	if len(hand) != 1 || hand[0]["pattern"] != "GF|X" || hand[0]["prio"] != "P2" || !hex12.MatchString(hand[0]["sig"]) {
		t.Fatalf("handoff.tsv rows %v, want one GF|X P2 row", hand)
	}
}

// Forge differing is a gorge-suspect divergence even when XMage agrees with
// gorge: never credited, handed off at P1.
func TestKeyproofForgeOnlyDiffersDiverges(t *testing.T) {
	t.Parallel()
	fx := newKpFixtureMode(t, kpForgeOnly)
	byKey, hand := fx.keyRows(t, filepath.Join(fx.tmp, "out"))
	for u := range fx.stepUses(t) {
		if r := byKey[u]; r["state"] != "diverged" || !hex12.MatchString(r["sig"]) {
			t.Errorf("%v: %v, want diverged", u, r)
		}
	}
	if len(hand) != 1 || hand[0]["pattern"] != "GX|F" || hand[0]["prio"] != "P1" {
		t.Fatalf("handoff.tsv rows %v, want one GX|F P1 row", hand)
	}

	// The same Forge difference on an item adjudicate did not take (XMage
	// could not answer it) is still a divergence: keyproof rebuilds it as G|F|–.
	if err := os.WriteFile(filepath.Join(fx.ad, "findings.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	byKey, hand = fx.keyRows(t, filepath.Join(fx.tmp, "out2"))
	for u := range fx.stepUses(t) {
		if r := byKey[u]; r["state"] != "diverged" {
			t.Errorf("no finding: %v: %v, want diverged", u, r)
		}
	}
	if len(hand) != 1 || hand[0]["pattern"] != "G|F|–" || hand[0]["prio"] != "P1" {
		t.Fatalf("no finding: handoff.tsv rows %v, want one G|F|– P1 row", hand)
	}
}

// XMage with no answer (harness, not_sent, decision_shape ...) and Forge
// agreeing credits the key with refs F; both agreeing stays FX.
func TestKeyproofXMageQuietCreditsForge(t *testing.T) {
	t.Parallel()
	fx := newKpFixtureMode(t, kpXMageQuiet)
	byKey, hand := fx.keyRows(t, filepath.Join(fx.tmp, "out"))
	second := fx.full(t, fx.ids[1])
	first := fx.full(t, fx.ids[0])
	nF := 0
	for u := range second {
		r := byKey[u]
		_, shared := first[u]
		switch {
		case r["state"] != "agreed":
			t.Errorf("%v: %v, want agreed", u, r)
		case !shared && r["refs"] != "F":
			t.Errorf("%v: refs %s, want F (only the XMage-quiet item exercised it)", u, r["refs"])
		case !shared:
			nF++
		}
	}
	for u := range first {
		if r := byKey[u]; r["refs"] != "FX" {
			t.Errorf("%v: refs %s, want FX (both agreed on the first item)", u, r["refs"])
		}
	}
	if nF == 0 {
		t.Error("no key credited F alone")
	}
	if len(hand) != 0 {
		t.Errorf("handoff.tsv rows %v, want none", hand)
	}
}

// An owed item's reason is rng, or harness plus the two followers' signature.
func TestKeyproofOwedReason(t *testing.T) {
	t.Parallel()
	f := func(class, msg string) classify.Finding { return classify.Finding{ID: "i", Class: class, Msg: msg} }
	for _, c := range []struct {
		x, f classify.Finding
		want string
	}{
		{f(classify.ClassRNG, ""), f(classify.ClassHarness, "x"), "rng"},
		{f(classify.ClassHarness, "xmage: a b"), f(classify.ClassRNG, ""), "rng"},
		{f(classify.ClassNotSent, ""), f(classify.ClassAgree, ""), "harness:x=not_sent+f=agree"},
		{classify.Finding{}, f(classify.ClassHarness, "forge: no priority at step 4"), "harness:x=none+f=harness~no-priority-at-step"},
	} {
		if got := owedReason(c.x, c.f); got != c.want {
			t.Errorf("owedReason(%v, %v) = %q, want %q", c.x, c.f, got, c.want)
		}
	}
}

// dropFinding rewrites a diff's verdicts.jsonl without item id's finding, as
// a follower whose driver never answered (nor refused) the item would leave it.
func dropFinding(t *testing.T, path, id string) {
	t.Helper()
	var kept []byte
	if err := readLines(path, func(b []byte) error {
		var f classify.Finding
		if err := json.Unmarshal(b, &f); err != nil {
			return err
		}
		if f.ID == id {
			return nil
		}
		kept = append(append(kept, b...), '\n')
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, kept, 0o644); err != nil {
		t.Fatal(err)
	}
}

// setLowerStatus rewrites a play dir's lower.jsonl row for id with the given
// status and no tape, as a lowering that produced no tape leaves it.
func setLowerStatus(t *testing.T, play, id, status string) {
	t.Helper()
	var out []byte
	if err := readLines(filepath.Join(play, "lower.jsonl"), func(b []byte) error {
		var r lowerRow
		if err := json.Unmarshal(b, &r); err != nil {
			return err
		}
		if r.ID == id {
			r.Status, r.Tape = status, ""
		}
		nb, err := json.Marshal(r)
		if err != nil {
			return err
		}
		out = append(append(out, nb...), '\n')
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(play, "lower.jsonl"), out, 0o644); err != nil {
		t.Fatal(err)
	}
}

// item reads one items.jsonl row of the fixture's play.
func (fx *kpFixture) item(t *testing.T, id string) oraclegen.Item {
	t.Helper()
	var found oraclegen.Item
	if err := readLines(filepath.Join(fx.play, "items.jsonl"), func(b []byte) error {
		var it oraclegen.Item
		if err := json.Unmarshal(b, &it); err != nil {
			return err
		}
		if it.ID == id {
			found = it
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if found.ID == "" {
		t.Fatalf("%s is not in items.jsonl", id)
	}
	return found
}

// coveredElsewhere is the keys another item credits (its whole lowered range)
// or the diverging step exercised: the keys whose row state is not decided by
// the dropped items alone.
func (fx *kpFixture) coveredElsewhere(t *testing.T, except ...string) map[classify.Use]bool {
	t.Helper()
	skip := map[string]bool{}
	for _, id := range except {
		skip[id] = true
	}
	covered := map[classify.Use]bool{}
	for u := range fx.stepUses(t) {
		covered[u] = true
	}
	for _, id := range fx.ids {
		if skip[id] || id == fx.div {
			continue // the diverging item credits nothing outside its step
		}
		for u := range fx.full(t, id) {
			covered[u] = true
		}
	}
	return covered
}

// cardIDs are the fixture's item ids whose item names card.
func (fx *kpFixture) cardIDs(t *testing.T, card string) []string {
	t.Helper()
	var ids []string
	for _, id := range fx.ids {
		if fx.item(t, id).Card == card {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		t.Fatalf("no item of %s (items %v)", card, fx.ids)
	}
	return ids
}

// newKpStartPair is a played start pair over xs: items (playTwo's xe: tape
// items declare no key, so only xs: items can name a key when keyproof drops
// them): every cardA item agrees with both followers, the last cardB item
// diverges from them identically at its last snapshot (a start's early steps
// are inside XMage's comparison cuts, its last checkpoint is compared by both
// followers), adjudicated, ready for keyproof.
func newKpStartPair(t *testing.T, cardA, keyA, cardB, keyB string) *kpFixture {
	t.Helper()
	ks := lowerKeyStarts(t, [][2]string{{cardA, keyA}, {cardB, keyB}})
	xprepPlayDir(t, ks.reg, ks.play)
	reg, play := ks.reg, ks.play
	tmp := t.TempDir()
	ids := goldenRows(t, play, filepath.Join(tmp, "raw.jsonl"), nil)
	fx := &kpFixture{reg: reg, play: play, tmp: tmp, ids: ids, lower: map[string]lowerRow{}, step: -1}
	if err := readLines(filepath.Join(play, "lower.jsonl"), func(b []byte) error {
		var r lowerRow
		if err := json.Unmarshal(b, &r); err != nil {
			return err
		}
		fx.lower[r.ID] = r
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// The diverging item is the last of cardB's two seeds.
	fx.div = ""
	for _, id := range ids {
		if strings.HasPrefix(id, "xs:"+strings.Join([]string{"XP", scen.Slug(cardB)}, ":")) {
			fx.div = id
		}
	}
	if fx.div == "" {
		t.Fatalf("no %s item in %v", cardB, ids)
	}
	differ := func(i int, x *oraclediff.XResult) {
		if ids[i] == fx.div {
			x.Snapshots[len(x.Snapshots)-1].Players[0].Life++
		}
	}
	rawX, rawF := filepath.Join(tmp, "rawx.jsonl"), filepath.Join(tmp, "rawf.jsonl")
	goldenRows(t, play, rawX, differ)
	goldenRows(t, play, rawF, differ)
	fx.xrows = filepath.Join(tmp, "xrows.jsonl")
	fx.frows = filepath.Join(tmp, "frows.jsonl")
	stampRows(t, rawX, fx.xrows, "xmage")
	stampRows(t, rawF, fx.frows, "forge")
	fx.xd, fx.fd, fx.ad = filepath.Join(tmp, "xd"), filepath.Join(tmp, "fd"), filepath.Join(tmp, "ad")
	if err := diff(reg, play, "xmage", fx.xrows, fx.xd, ""); err != nil {
		t.Fatal(err)
	}
	if err := diff(reg, play, "forge", fx.frows, fx.fd, ""); err != nil {
		t.Fatal(err)
	}
	xc := verdictClasses(t, filepath.Join(fx.xd, "verdicts.jsonl"))
	fc := verdictClasses(t, filepath.Join(fx.fd, "verdicts.jsonl"))
	for _, e := range []struct {
		name string
		c    map[string]string
	}{
		{"xmage", xc}, {"forge", fc},
	} {
		if c := e.c[fx.div]; c != classify.ClassDiverge {
			t.Fatalf("%s class of the diverging item %s: %s, want diverge", e.name, fx.div, c)
		}
		for _, id := range ids {
			if id != fx.div && e.c[id] != classify.ClassAgree {
				t.Fatalf("%s class of %s: %s, want agree", e.name, id, e.c[id])
			}
		}
	}
	// The diverging step is the one classify cut the finding at.
	var xf classify.Finding
	if err := readLines(filepath.Join(fx.xd, "verdicts.jsonl"), func(b []byte) error {
		var f classify.Finding
		if err := json.Unmarshal(b, &f); err != nil {
			return err
		}
		if f.ID == fx.div {
			xf = f
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	fx.step = xf.Step
	if fx.step < 0 || fx.step >= len(fx.lower[fx.div].StepEntry) || fx.lower[fx.div].StepEntry[fx.step] < 0 {
		t.Fatalf("diverging step %d outside %s's lowered steps", fx.step, fx.div)
	}
	in, err := adjudicate.Load(play, fx.xd, fx.fd, fx.xrows, fx.frows)
	if err != nil {
		t.Fatal(err)
	}
	res := adjudicate.Run(in)
	if len(res.Rows) != 1 || res.Rows[0].ID != fx.div || res.Rows[0].Pattern != string(oraclediff.ForgeXMageG) {
		t.Fatalf("adjudication rows %+v, want one FX|G row for %s", res.Rows, fx.div)
	}
	if err := adjudicate.Write(fx.ad, res); err != nil {
		t.Fatal(err)
	}
	return fx
}

// owedWant is the reason keyproof must name for a key its dropped item alone
// exercised.
const (
	reasonForgeMissing = "follower-missing:forge:agree"
	reasonNoTape       = "credited-refused:status:refused"
)

// assertOwed checks that every own key no other item credits got an owed row
// with exactly reason, and returns how many did.
func (fx *kpFixture) assertOwed(t *testing.T, byKey map[classify.Use]map[string]string, dropped []string, own map[classify.Use]int, reason string) int {
	t.Helper()
	n := 0
	covered := fx.coveredElsewhere(t, dropped...)
	for u := range own {
		if covered[u] {
			continue
		}
		r := byKey[u]
		if r == nil {
			t.Errorf("%v: no row, want owed %s", u, reason)
			continue
		}
		if r["state"] != "owed" || r["reason"] != reason || r["refs"] != "-" {
			t.Errorf("%v: %v, want owed %s with refs -", u, r, reason)
			continue
		}
		n++
	}
	return n
}

// An item only one follower's diff answered compared nothing, but its keys
// must not vanish: each gets an owed row with the follower-missing reason (the
// absent follower, then the present one's class), not a silent drop.
func TestKeyproofFollowerMissingOwed(t *testing.T) {
	t.Parallel()
	fx := newKpFixture(t)
	dropped := fx.ids[0]
	dropFinding(t, filepath.Join(fx.fd, "verdicts.jsonl"), dropped)
	out := filepath.Join(fx.tmp, "out")
	byKey, _ := fx.keyRows(t, out)

	own := fx.full(t, dropped)
	if len(own) == 0 {
		t.Fatalf("%s exercised no key", dropped)
	}
	if n := fx.assertOwed(t, byKey, []string{dropped}, own, reasonForgeMissing); n == 0 {
		t.Error("no key of the dropped item was its alone: the case covered nothing")
	}
	// The dropped item credits nothing: no witness.
	if b, _ := os.ReadFile(filepath.Join(out, "witness.jsonl")); strings.Contains(string(b), `"id":"`+dropped+`"`) {
		t.Errorf("the follower-missing item wrote a witness")
	}
}

// The same for a start's xs: items (both seeds of one card, dropped together):
// each key they alone exercised -- including the declared key no tape entry
// names -- gets an owed row with the follower-missing reason.
func TestKeyproofFollowerMissingOwedStart(t *testing.T) {
	t.Parallel()
	const card = "Anointer Priest"
	fx := newKpStartPair(t, card, "f0/t0", "Vengeant Vampire", "f0/t0")
	dropped := fx.cardIDs(t, card)
	for _, id := range dropped {
		dropFinding(t, filepath.Join(fx.fd, "verdicts.jsonl"), id)
	}
	byKey, _ := fx.keyRows(t, filepath.Join(fx.tmp, "out"))

	own := map[classify.Use]int{}
	for _, id := range dropped {
		for u, n := range fx.full(t, id) {
			own[u] += n
		}
		u, ok := declaredKey(fx.reg, fx.item(t, id))
		if !ok {
			t.Fatalf("%s declares no key", id)
		}
		own[u]++
	}
	if n := fx.assertOwed(t, byKey, dropped, own, reasonForgeMissing); n == 0 {
		t.Error("no key of the dropped start was its alone: the case covered nothing")
	}
	if r := byKey[classify.Use{Card: card, Key: "f0/t0"}]; r["state"] != "owed" || r["reason"] != reasonForgeMissing {
		t.Errorf("the declared key of the dropped start: %v, want owed %s", r, reasonForgeMissing)
	}
}

// credited refusing an item (its lowering gave no tape) used to drop its keys
// silently; the key its id declares now gets an owed row naming the refusal.
// Once the item is also follower-missing, that reason wins (the item was never
// compared at all), still naming the declared key.
func TestKeyproofCreditedRefusalOwed(t *testing.T) {
	t.Parallel()
	const card = "Anointer Priest"
	fx := newKpStartPair(t, card, "f0/t0", "Vengeant Vampire", "f0/t0")
	dropped := fx.cardIDs(t, card)
	u := classify.Use{Card: card, Key: "f0/t0"}
	if _, ok := declaredKey(fx.reg, fx.item(t, dropped[0])); !ok {
		t.Fatalf("%s declares no key", dropped[0])
	}
	for _, id := range dropped {
		setLowerStatus(t, fx.play, id, statusRefused)
	}
	byKey, _ := fx.keyRows(t, filepath.Join(fx.tmp, "out"))
	if r := byKey[u]; r == nil || r["state"] != "owed" || r["reason"] != reasonNoTape {
		t.Errorf("declared key %v: %v, want owed %s", u, r, reasonNoTape)
	}

	// The same items then lose Forge's findings too: follower-missing is the
	// answer triage needs, and it still names the declared key.
	for _, id := range dropped {
		dropFinding(t, filepath.Join(fx.fd, "verdicts.jsonl"), id)
	}
	byKey, _ = fx.keyRows(t, filepath.Join(fx.tmp, "out2"))
	if r := byKey[u]; r == nil || r["state"] != "owed" || r["reason"] != reasonForgeMissing {
		t.Errorf("follower-missing: declared key %v: %v, want owed %s", u, r, reasonForgeMissing)
	}
}

// The drop reasons bucket by class: follower-missing names the absent follower
// and the present one's class, credited-refusal why there was no tape.
func TestKeyproofDropReasons(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		absent  string
		present classify.Finding
		want    string
	}{
		{"forge", classify.Finding{Class: classify.ClassAgree}, "follower-missing:forge:agree"},
		{"xmage", classify.Finding{Class: classify.ClassHarness}, "follower-missing:xmage:harness"},
		{"xmage", classify.Finding{}, "follower-missing:xmage:none"},
	} {
		if got := owedReasonFollowerMissing(c.absent, c.present); got != c.want {
			t.Errorf("owedReasonFollowerMissing(%q, %+v) = %q, want %q", c.absent, c.present, got, c.want)
		}
	}
	p := &playKeys{lower: map[string]lowerRow{
		"ok-notape": {ID: "ok-notape", Status: lower.StatusOK},
		"gap":       {ID: "gap", Status: "lower_gap"},
		"nostatus":  {ID: "nostatus"},
	}}
	for id, want := range map[string]string{
		"missing":   "credited-refused:no-lower-row",
		"ok-notape": "credited-refused:no-tape",
		"gap":       "credited-refused:status:lower_gap",
		"nostatus":  "credited-refused:status:none",
	} {
		if got := p.creditRefusal(id); got != want {
			t.Errorf("creditRefusal(%q) = %q, want %q", id, got, want)
		}
	}
}

// A key's reason is voted by class first: splitting harness into signatures
// must not move a key to rng.
func TestKeyproofOwedPickClassFirst(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		owed map[string]string
		want string
	}{
		{"harness split three ways beats two rng", map[string]string{"1": "harness:A", "2": "harness:B", "3": "harness:C", "4": "rng", "5": "rng"}, "harness:A"},
		{"two-two tie goes to harness", map[string]string{"1": "harness:B", "2": "harness:B", "3": "rng", "4": "rng"}, "harness:B"},
		{"most common signature inside the class", map[string]string{"1": "harness:B", "2": "harness:A", "3": "harness:B", "4": "rng"}, "harness:B"},
		{"rng majority", map[string]string{"1": "harness:A", "2": "rng", "3": "rng"}, "rng"},
		{"one reason", map[string]string{"1": "rng"}, "rng"},
		{"drop reasons vote by class too", map[string]string{"1": "follower-missing:forge:agree", "2": "credited-refused:status:refused", "3": "follower-missing:xmage:harness"}, "follower-missing:forge:agree"},
	} {
		if got := pickOwedReason(c.owed); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}
