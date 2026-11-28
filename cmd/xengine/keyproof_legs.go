package main

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/classify"
)

// keyproofLegsHeader is keyproof-legs.tsv's header: "card" (the corpus name the
// proof loop folds to an oracle id, as in keyproof.tsv) and then the columns of
// carddb/schema/v1/keyproof.tsv in schema order.
const keyproofLegsHeader = "card\toracle_id\tkey\tleg\tdesc\tstate\treason\trefs\tsource\tagree_items\tdiff_items\twitness\tsig\tcite\tticket\tseats\tmultiplayer\tgorge_sha\tcorpus_pin\tscript_sha\txmage_ref\tforge_ref\tmeasured\treplayed\treplayed_sha\twriter"

// kpLegAcc is what the items said about one (card, key) for one follower.
type kpLegAcc struct {
	agree map[string]bool   // item id -> this follower's agreement credited the key
	div   map[string]string // item id -> signature of this follower's undiagnosed divergence
	owed  map[string]string // item id -> why this follower gave the key nothing
}

// kpLegKey is one (card, key)'s leg evidence.
type kpLegKey struct {
	x, f  kpLegAcc
	cite  map[string]kpDiv // diagnosed items (kpDiv.diagnosed is true)
	seats int              // largest seat count of any item that touched the key
}

func newKpLegKey() *kpLegKey {
	acc := func() kpLegAcc {
		return kpLegAcc{agree: map[string]bool{}, div: map[string]string{}, owed: map[string]string{}}
	}
	return &kpLegKey{x: acc(), f: acc(), cite: map[string]kpDiv{}, seats: 2}
}

// sortedIDs is a map's keys, sorted.
func sortedIDs[V any](m map[string]V) []string {
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// kpSource is the source column of a deciding item id.
func kpSource(id string) string {
	switch {
	case strings.HasPrefix(id, "xs:"):
		return "start"
	case strings.HasPrefix(id, "xe:"):
		return "game"
	}
	return "steps"
}

// legRows is keyproof-legs.tsv: per (card, key) one xmage and one forge row,
// each carrying that follower's own state, and a cite row when a diagnosed
// item touched the key; sorted by card, key, leg.
func legRows(tab *kpTable, legs map[classify.Use]*kpLegKey) []byte {
	uses := make([]classify.Use, 0, len(legs))
	for u := range legs {
		uses = append(uses, u)
	}
	sort.Slice(uses, func(i, j int) bool {
		if uses[i].Card != uses[j].Card {
			return uses[i].Card < uses[j].Card
		}
		return uses[i].Key < uses[j].Key
	})
	var b bytes.Buffer
	b.WriteString(keyproofLegsHeader + "\n")
	for _, u := range uses {
		k := legs[u]
		desc, seats := tab.desc(u), fmt.Sprint(max(2, k.seats))
		row := func(leg, state, reason, refs, source string, agree, diff int, witness, sig, cite, ticket string) {
			cells := []string{u.Card, "-", u.Key, leg, desc, state, reason, refs, source,
				fmt.Sprint(agree), fmt.Sprint(diff), witness, sig, cite, ticket, seats, "false",
				tab.gorgeSHA, tab.corpusPin, tab.scriptSHA(u.Card), tab.xmageRef, tab.forgeRef, tab.measured,
				"-", "-", "xproof"}
			b.WriteString(strings.Join(cells, "\t") + "\n")
		}
		// Legs sort "cite" < "forge" < "xmage".
		if len(k.cite) > 0 {
			ids := sortedIDs(k.cite)
			d := k.cite[ids[0]]
			state, reason := "diagnosed", "-"
			if d.cite == "" {
				state, reason = "owed", "diagnosed-no-cite"
			}
			row("cite", state, reason, "-", kpSource(ids[0]), 0, len(k.cite), "-", dash(d.sig), dash(d.cite), dash(d.ticket))
		}
		for _, e := range []struct {
			leg, letter string
			acc         kpLegAcc
		}{{"forge", "F", k.f}, {"xmage", "X", k.x}} {
			a := e.acc
			switch {
			case len(a.div) > 0:
				ids := sortedIDs(a.div)
				row(e.leg, "diverged", "-", "-", kpSource(ids[0]), len(a.agree), len(a.div), "-", a.div[ids[0]], "-", "-")
			case len(a.agree) > 0:
				ids := sortedIDs(a.agree)
				row(e.leg, "agreed", "-", e.letter, kpSource(ids[0]), len(a.agree), len(a.div), ids[0], "-", "-", "-")
			case len(a.owed) > 0:
				ids := sortedIDs(a.owed)
				row(e.leg, "owed", pickOwedReason(a.owed), "-", kpSource(ids[0]), len(a.agree), len(a.div), "-", "-", "-", "-")
			default:
				row(e.leg, "owed", "no-item", "-", "steps", len(a.agree), len(a.div), "-", "-", "-", "-")
			}
		}
	}
	return b.Bytes()
}
