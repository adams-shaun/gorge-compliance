package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/adopt"
)

// runTickets emits one ticket draft per finding class. It never files anything:
// without -out it prints the list; with -out it writes <id>.md briefs and
// index.jsonl. Ids are stable across runs.
func runTickets(dir, out string, minCards int, anyIn string, asJSON bool) error {
	reg, err := loadReg(dir)
	if err != nil {
		return err
	}
	cs, err := adopt.Build(reg, ".")
	if err != nil {
		return err
	}
	if anyIn == "" {
		anyIn = cs.Config.Formats[0].Name
	}
	ts, notes, err := cs.Tickets(reg, ".", adopt.TicketOptions{MinCards: minCards, AnyIn: anyIn})
	if err != nil {
		return err
	}
	byClass := map[string]int{}
	for _, t := range ts {
		byClass[t.Class]++
	}
	if out != "" {
		if err := writeTickets(out, ts); err != nil {
			return err
		}
	}
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetEscapeHTML(false)
		for _, t := range ts {
			if err := enc.Encode(t); err != nil {
				return err
			}
		}
		return nil
	}
	for _, t := range ts {
		fmt.Printf("P%d %-10s %4d cards  %-52s %s\n", t.Priority, t.Class, len(t.Cards), t.ID, t.Title)
	}
	classes := make([]string, 0, len(byClass))
	for c := range byClass {
		classes = append(classes, c)
	}
	sort.Strings(classes)
	var parts []string
	for _, c := range classes {
		parts = append(parts, fmt.Sprintf("%s %d", c, byClass[c]))
	}
	for _, n := range notes {
		fmt.Println("not filed:", n)
	}
	fmt.Printf("tickets: %d (%s); primitive scope: %d+ tournament cards, or any card in %s\n", len(ts), strings.Join(parts, ", "), minCards, anyIn)
	if out != "" {
		fmt.Printf("wrote %s/<id>.md and %s/index.jsonl\n", out, out)
	}
	return nil
}

func writeTickets(out string, ts []adopt.Ticket) error {
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	old, _ := filepath.Glob(filepath.Join(out, "compliance-*.md"))
	for _, p := range old {
		if err := os.Remove(p); err != nil {
			return err
		}
	}
	idx, err := os.Create(filepath.Join(out, "index.jsonl"))
	if err != nil {
		return err
	}
	defer idx.Close()
	iw := bufio.NewWriter(idx)
	enc := json.NewEncoder(iw)
	enc.SetEscapeHTML(false)
	for _, t := range ts {
		p := filepath.Join(out, t.ID+".md")
		if err := os.WriteFile(p, []byte(t.Body), 0o644); err != nil {
			return err
		}
		row := struct {
			adopt.Ticket
			Brief string `json:"brief"`
		}{t, t.ID + ".md"}
		if err := enc.Encode(row); err != nil {
			return err
		}
	}
	return iw.Flush()
}
