// Command carddb checks a carddb tree (docs: internal/carddb).
//
//	carddb validate [-schema carddb/schema] [-scope public|private] -tree DIR
//	carddb boundary [-schema carddb/schema] -tree DIR
//
// validate checks every .tsv shard under DIR against the schema: header line,
// column set, sorted unique keys, cell syntax. boundary checks that DIR is
// publishable: no private table or column, no undeclared column, no banned
// field name, no Forge card-script line. Output is one violation per line in
// path order.
//
// Exit status: 0 clean, 1 violations found, 2 usage or I/O error.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/adams-shaun/gorge-compliance/internal/carddb"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || (args[0] != "validate" && args[0] != "boundary") {
		fmt.Fprintln(stderr, "usage: carddb validate|boundary [-schema DIR] [-scope public|private] -tree DIR")
		return 2
	}
	cmd := args[0]
	fs := flag.NewFlagSet("carddb "+cmd, flag.ContinueOnError)
	fs.SetOutput(stderr)
	schemaDir := fs.String("schema", "carddb/schema", "schema directory (holds VERSION and v<N>/)")
	tree := fs.String("tree", "", "tree to check")
	scope := fs.String("scope", carddb.Public, "validate only: public (publishable columns) or private (all columns)")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	if *tree == "" || fs.NArg() != 0 {
		fmt.Fprintln(stderr, "carddb: -tree is required")
		return 2
	}
	if *scope != carddb.Public && *scope != carddb.Private {
		fmt.Fprintf(stderr, "carddb: -scope %q: want public or private\n", *scope)
		return 2
	}
	s, err := carddb.LoadSchema(*schemaDir)
	if err != nil {
		fmt.Fprintln(stderr, "carddb:", err)
		return 2
	}
	var vs []carddb.Violation
	if cmd == "validate" {
		vs, err = carddb.Validate(s, *tree, *scope)
	} else {
		vs, err = carddb.Boundary(s, *tree)
	}
	if err != nil {
		fmt.Fprintln(stderr, "carddb:", err)
		return 2
	}
	for _, v := range vs {
		fmt.Fprintln(stdout, v)
	}
	if len(vs) > 0 {
		fmt.Fprintf(stderr, "carddb %s: %d violation(s)\n", cmd, len(vs))
		return 1
	}
	fmt.Fprintf(stdout, "carddb %s: ok (%s)\n", cmd, *tree)
	return 0
}
