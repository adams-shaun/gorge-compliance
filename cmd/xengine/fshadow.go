package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/fshadow"
)

// runFShadow follows one forge-shadow-1 log in gorge (oracle/xengine/fshadow)
// and writes OUT/result.json, plus OUT/divergence-1.json and its record
// prefix at the first hard divergence. It exits 0 either way: a divergence is
// a finding, not a failure.
func runFShadow(args []string) error {
	fs := flag.NewFlagSet("fshadow", flag.ExitOnError)
	dir := fs.String("cards", ".cards", "corpus dir")
	logPath := fs.String("log", "", "forge-shadow-1 log (required)")
	out := fs.String("out", DefaultRoot+"/fshadow", "output directory")
	game := fs.String("game", "", "game id (default: the log's base name)")
	maxDec := fs.Int("max-decisions", fshadow.DefaultMaxDecisions, "follower decision cap")
	fs.Parse(args)
	if *logPath == "" {
		return fmt.Errorf("fshadow: -log is required")
	}
	if *game == "" {
		*game = strings.TrimSuffix(filepath.Base(*logPath), filepath.Ext(*logPath))
	}
	f, err := os.Open(*logPath)
	if err != nil {
		return err
	}
	l, err := fshadow.Read(f)
	f.Close()
	if err != nil {
		return err
	}
	reg, err := loadReg(*dir)
	if err != nil {
		return err
	}
	res, err := fshadow.Follow(reg, l, fshadow.Options{MaxDecisions: *maxDec})
	if err != nil {
		return err
	}
	b, err := jsonl(res)
	if err != nil {
		return err
	}
	if err := writeAtomic(filepath.Join(*out, "result.json"), b); err != nil {
		return err
	}
	verdict := fmt.Sprintf("clean (%s)", res.End)
	if res.Hard != nil {
		p, err := fshadow.WriteDivergence(*out, *game, *logPath, res.Hard)
		if err != nil {
			return err
		}
		verdict = fmt.Sprintf("%s %s at line %d -> %s", res.Hard.What, res.Hard.Kind, res.Hard.Line, p)
	}
	fmt.Printf("fshadow %s: %d decisions, %d asks, %d snaps, %d soft: %s\n", *game, res.Decisions, res.Asks, res.Snaps, len(res.Soft), verdict)
	return nil
}
