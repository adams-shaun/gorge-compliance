package adjudicate

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclediff"
	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclegen"
	"github.com/adams-shaun/gorge-compliance/oracle/xengine/classify"
	"github.com/adams-shaun/gorge/engine/rules"
)

// Input is everything one adjudication reads: the play's items and gorge
// results, both followers' rows, and both followers' xe-6 findings (the
// classes decide which items are adjudicated; the finding of the diverging
// engine gives the step, the values and the repro directory).
type Input struct {
	Items        map[string]oraclegen.Item
	Gorge        map[string]rules.OracleResult
	XRows, FRows map[string]oraclediff.XResult
	XFind, FFind map[string]classify.Finding
	// XDiff and FDiff are the `xengine diff` output dirs the findings came
	// from; a draft quotes the REPRO.md commands found under them.
	XDiff, FDiff string
	// HasAgree reports a committed agree verdict row for the card; a cluster
	// of one finding is drafted only when it does not. Nil: no card has one.
	HasAgree func(card string) bool
}

// Row is one adjudicated item. It holds ids, labels and field names only,
// never a snapshot value: it is what findings.jsonl and the ledger carry.
type Row struct {
	ID      string `json:"id"`
	Card    string `json:"card,omitempty"`
	Pattern string `json:"pattern"`
	Label   string `json:"label,omitempty"`
	XClass  string `json:"xmage_class"`
	FClass  string `json:"forge_class"`
	GXField string `json:"gx_field,omitempty"`
	GFField string `json:"gf_field,omitempty"`
	FXField string `json:"fx_field,omitempty"`
}

// Finding is a Row with the values and repro the ticket draft quotes.
type Finding struct {
	Row
	Item         oraclegen.Item
	Diverging    classify.Finding // the XMage finding, else the Forge one
	XVal, FVal   string           // the follower values at the checkpoint
	Steps        int              // steps of the repro (the item cut at the diverging step)
	XRepro       string           // REPRO.md paths; "" when the engine did not diverge
	FRepro       string
	FieldClassOf string
}

// Cluster groups the gorge_suspect findings of one field class and card.
type Cluster struct {
	Key        string
	SHA12      string
	Card       string
	FieldClass string
	Findings   []Finding // by id
	Smallest   Finding   // fewest steps, then id
}

// Result is one adjudication.
type Result struct {
	Rows     []Row // by id
	Findings []Finding
	Notes    []string // xmage_suspect and forge_suspect remarks, by id
	Clusters []Cluster
	Drafts   []Cluster // the clusters that become a ticket draft
}

var digits = regexp.MustCompile(`\d+`)

// FieldClass is a comparator field with its indices stripped ("p0.life" ->
// "p.life"), so seats and positions do not split a cluster.
func FieldClass(field string) string { return digits.ReplaceAllString(field, "") }

// Run adjudicates every eligible item of in, in id order.
func Run(in Input) Result {
	var ids []string
	for id := range in.XFind {
		if _, ok := in.FFind[id]; ok {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	var res Result
	for _, id := range ids {
		xf, ff := in.XFind[id], in.FFind[id]
		item, haveItem := in.Items[id]
		g, haveG := in.Gorge[id]
		if !Eligible(xf.Class, ff.Class) || !haveItem || !haveG {
			continue
		}
		p, legs := ItemLegs(item, g, in.XRows[id], in.FRows[id])
		row := Row{ID: id, Pattern: string(p), Label: Label(p), XClass: xf.Class, FClass: ff.Class}
		if legs.GX.Status == oraclediff.Diverge {
			row.GXField = legs.GX.Field
		}
		if legs.GF.Status == oraclediff.Diverge {
			row.GFField = legs.GF.Field
		}
		if legs.FX.Status == oraclediff.Diverge {
			row.FXField = legs.FX.Field
		}
		div := xf
		if xf.Class != classify.ClassDiverge {
			div = ff
		}
		row.Card = cardOf(item, div)
		f := Finding{Row: row, Item: item, Diverging: div, XVal: xf.Other, FVal: ff.Other}
		f.Steps = len(classify.Truncate(item, div).Steps)
		if xf.Class == classify.ClassDiverge && in.XDiff != "" {
			f.XRepro = filepath.Join(classify.Dir(in.XDiff, xf), "REPRO.md")
		}
		if ff.Class == classify.ClassDiverge && in.FDiff != "" {
			f.FRepro = filepath.Join(classify.Dir(in.FDiff, ff), "REPRO.md")
		}
		f.FieldClassOf = FieldClass(firstNonEmpty(row.GXField, row.GFField, row.FXField))
		res.Rows = append(res.Rows, row)
		res.Findings = append(res.Findings, f)
		switch row.Label {
		case LabelXMageSuspect:
			res.Notes = append(res.Notes, fmt.Sprintf("%s: %s: gorge and Forge agree, XMage differs at %s; weak evidence (Forge runs gorge's own scripts), read the XMage side for a driver cause before anything else", id, row.Pattern, row.GXField))
		case LabelForgeSuspect:
			res.Notes = append(res.Notes, fmt.Sprintf("%s: %s: gorge and XMage agree, Forge differs at %s; recorded only", id, row.Pattern, row.GFField))
		}
	}
	res.Clusters = clusters(res.Findings)
	for _, c := range res.Clusters {
		if len(c.Findings) >= 2 || in.HasAgree == nil || !in.HasAgree(c.Card) {
			res.Drafts = append(res.Drafts, c)
		}
	}
	return res
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// clusters groups the gorge_suspect findings by (field class, card), by key.
func clusters(fs []Finding) []Cluster {
	by := map[string]*Cluster{}
	for _, f := range fs {
		if f.Label != LabelGorgeSuspect {
			continue
		}
		key := f.FieldClassOf + "\x00" + f.Card
		c := by[key]
		if c == nil {
			c = &Cluster{Key: key, SHA12: sha12(key), Card: f.Card, FieldClass: f.FieldClassOf}
			by[key] = c
		}
		c.Findings = append(c.Findings, f)
	}
	keys := make([]string, 0, len(by))
	for k := range by {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]Cluster, 0, len(keys))
	for _, k := range keys {
		c := by[k]
		sort.Slice(c.Findings, func(i, j int) bool { return c.Findings[i].ID < c.Findings[j].ID })
		c.Smallest = c.Findings[0]
		for _, f := range c.Findings[1:] {
			if f.Steps < c.Smallest.Steps {
				c.Smallest = f
			}
		}
		out = append(out, *c)
	}
	return out
}

var permName = regexp.MustCompile(`c\d+ o\d+ (.+?) \[`)

// cardOf names the card of the diverging step: the card cast or activated.
// A step with no card (a pass during which a trigger fired) falls back to the
// first non-token permanent the diverging values name, the trigger's source.
func cardOf(item oraclegen.Item, f classify.Finding) string {
	if f.Step >= 0 && f.Step < len(item.Steps) && item.Steps[f.Step].Card != "" {
		return item.Steps[f.Step].Card
	}
	for _, v := range []string{f.Gorge, f.Other} {
		for _, m := range permName.FindAllStringSubmatch(v, -1) {
			if m[1] != "token" {
				return m[1]
			}
		}
	}
	return ""
}

// ---- reading a play and its two diffs ----

func readLines(path string, each func([]byte) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 256<<20)
	for sc.Scan() {
		if len(sc.Bytes()) == 0 {
			continue
		}
		if err := each(append([]byte(nil), sc.Bytes()...)); err != nil {
			return err
		}
	}
	return sc.Err()
}

func readRows(path, engine string) (map[string]oraclediff.XResult, error) {
	rows := map[string]oraclediff.XResult{}
	err := readLines(path, func(b []byte) error {
		var r struct {
			oraclediff.XResult
			Engine string `json:"engine"`
		}
		if err := json.Unmarshal(b, &r); err != nil {
			return err
		}
		if r.Engine != "" && r.Engine != engine {
			return fmt.Errorf("row %q is a %s row, not %s", r.ID, r.Engine, engine)
		}
		rows[r.ID] = r.XResult
		return nil
	})
	return rows, err
}

func readFindings(dir string) (map[string]classify.Finding, error) {
	out := map[string]classify.Finding{}
	err := readLines(filepath.Join(dir, "verdicts.jsonl"), func(b []byte) error {
		var f classify.Finding
		if err := json.Unmarshal(b, &f); err != nil {
			return err
		}
		out[f.ID] = f
		return nil
	})
	return out, err
}

// Load reads a play directory, the xmage and forge diff directories of it,
// and the two follower row files.
func Load(playDir, xDiff, fDiff, xRows, fRows string) (Input, error) {
	in := Input{
		Items: map[string]oraclegen.Item{}, Gorge: map[string]rules.OracleResult{},
		XDiff: xDiff, FDiff: fDiff,
	}
	var err error
	if err = readLines(filepath.Join(playDir, "items.jsonl"), func(b []byte) error {
		var it oraclegen.Item
		if err := json.Unmarshal(b, &it); err != nil {
			return err
		}
		in.Items[it.ID] = it
		return nil
	}); err != nil {
		return in, err
	}
	if err = readLines(filepath.Join(playDir, "gorge.jsonl"), func(b []byte) error {
		var r struct {
			ID     string             `json:"id"`
			Result rules.OracleResult `json:"result"`
		}
		if err := json.Unmarshal(b, &r); err != nil {
			return err
		}
		in.Gorge[r.ID] = r.Result
		return nil
	}); err != nil {
		return in, err
	}
	if in.XRows, err = readRows(xRows, "xmage"); err != nil {
		return in, err
	}
	if in.FRows, err = readRows(fRows, "forge"); err != nil {
		return in, err
	}
	if in.XFind, err = readFindings(xDiff); err != nil {
		return in, err
	}
	in.FFind, err = readFindings(fDiff)
	return in, err
}

// CommittedAgree reads the committed verdict rows under dir and returns the
// HasAgree predicate over them (a missing directory has no rows).
func CommittedAgree(dir string) (func(card string) bool, error) {
	all, err := compliance.LoadVerdicts(dir)
	if err != nil {
		return nil, err
	}
	agree := map[string]bool{}
	for card, byTemplate := range all {
		for _, r := range byTemplate {
			if r.Status == compliance.StatusAgree {
				agree[compliance.FoldName(card)] = true
			}
		}
	}
	return func(card string) bool { return card != "" && agree[compliance.FoldName(card)] }, nil
}
