// Command deckcheck reports, for each gorge deck JSON path given, how gorge reads it: names the registry
// cannot resolve, the Commander deck-rule check (ValidateCommander) and each card gorge does not fully
// support (registry Unsupported reasons). It prints one JSON array; scripts/deck-audit.py reads it.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/adams-shaun/gorge/engine/deck"
	"github.com/adams-shaun/gorge/engine/effects"
	_ "github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/testkit/testutil"
)

type res struct {
	Path        string              `json:"path"`
	ParseErr    string              `json:"parse_err,omitempty"`
	NotInReg    []string            `json:"not_in_registry,omitempty"`
	Validate    []string            `json:"validate,omitempty"`
	Unsupported map[string][]string `json:"unsupported,omitempty"`
}

func main() {
	reg, err := testutil.OpenCorpusRegistry(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	sup := effects.Supported()
	var out []res
	for _, p := range os.Args[2:] {
		r := res{Path: p, Unsupported: map[string][]string{}}
		raw, err := os.ReadFile(p)
		if err != nil {
			r.ParseErr = err.Error()
			out = append(out, r)
			continue
		}
		f, err := deck.Parse(raw)
		if err != nil {
			r.ParseErr = err.Error()
			out = append(out, r)
			continue
		}
		for _, e := range f.Cards {
			c, ok := reg.Lookup(e.Name)
			if !ok {
				r.NotInReg = append(r.NotInReg, e.Name)
				continue
			}
			if m := reg.Unsupported(c, sup); len(m) > 0 {
				r.Unsupported[e.Name] = m
			}
		}
		if err := f.ValidateCommander(reg); err != nil {
			for _, l := range strings.Split(err.Error(), "\n") {
				l = strings.TrimSpace(l)
				if l != "" && l != "commander deck invalid:" {
					r.Validate = append(r.Validate, l)
				}
			}
		}
		out = append(out, r)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", " ")
	enc.Encode(out)
}
