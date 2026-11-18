package fshadow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/adams-shaun/gorge/server/matchrecord"
)

// DivergenceSchema is the schema tag of one divergence-<n>.json, the input
// xengine adjudicate's fshadow census reads.
const DivergenceSchema = "fshadow-divergence-1"

// PairFG is a Forge-hosted game's pair.
const PairFG = "F|G"

// File is one divergence-<n>.json. It mirrors adjudicate.Divergence
// (oracle/xengine/adjudicate/fshadow.go, gorge-compliance PR #221) field for
// field rather than importing it, so this package does not depend on that
// change; TestDivergenceFileFields holds the two to one field list.
type File struct {
	Schema   string       `json:"schema"`
	Game     string       `json:"game"`
	Seed     int64        `json:"seed"`
	Decks    []string     `json:"decks,omitempty"`
	Log      string       `json:"log,omitempty"`
	Offset   int64        `json:"offset"`
	Pair     string       `json:"pair"`
	Resynced int          `json:"resynced,omitempty"`
	What     string       `json:"what"`
	Kind     string       `json:"kind"`
	Field    string       `json:"field,omitempty"`
	Gorge    string       `json:"gorge,omitempty"`
	Other    string       `json:"other,omitempty"`
	Cards    []Implicated `json:"cards,omitempty"`
	Record   string       `json:"record,omitempty"`
	Repro    string       `json:"repro,omitempty"`
	Harness  string       `json:"harness,omitempty"`
	Msg      string       `json:"msg,omitempty"`
}

// Implicated mirrors adjudicate.Implicated: one implicated card and, when
// known, its ability (front-face ability or trigger index in IR order) or an
// explicit primitive.
type Implicated struct {
	Name      string `json:"name"`
	Token     bool   `json:"token,omitempty"`
	Ability   *int   `json:"ability,omitempty"`
	Trigger   *int   `json:"trigger,omitempty"`
	Primitive string `json:"primitive,omitempty"`
}

// FileOf is d's file form for game (an id unique within the run) read from
// logPath. record is the record file's name beside the divergence, "" when
// none was written.
func FileOf(d *Divergence, game, logPath, record string) File {
	msg := d.Detail
	if d.Ask > 0 {
		msg = strings.TrimSpace(fmt.Sprintf("ask %d seat %d. %s", d.Ask, d.Seat, msg))
	}
	f := File{
		Schema: DivergenceSchema, Game: game, Seed: int64(d.Seed), Decks: d.Decks,
		Log: logPath, Offset: int64(d.Line), Pair: PairFG, What: d.What, Kind: d.Kind,
		Field: d.Field, Gorge: d.Gorge, Other: d.Forge, Cards: d.Cards, Record: record,
		Harness: d.Harness, Msg: msg,
	}
	if logPath != "" {
		f.Repro = "go run ./cmd/xengine fshadow -log " + logPath + " -out .oracle-build/xengine/fshadow"
	}
	return f
}

// WriteDivergence writes d as dir/divergence-<n>.json, with its gorge record
// prefix beside it as divergence-<n>.match.json.gz, and returns the
// divergence's path. The record holds every hidden card, so dir is never in
// the repository.
func WriteDivergence(dir, game, logPath string, d *Divergence) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	record := ""
	if d.Record != nil {
		record = fmt.Sprintf("divergence-%d%s", d.N, matchrecord.Ext)
		if err := matchrecord.Write(filepath.Join(dir, record), d.Record); err != nil {
			return "", err
		}
	}
	b, err := json.MarshalIndent(FileOf(d, game, logPath, record), "", "  ")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, fmt.Sprintf("divergence-%d.json", d.N))
	return path, os.WriteFile(path, append(b, '\n'), 0o644)
}
