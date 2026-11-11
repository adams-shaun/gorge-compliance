package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/follower"
)

// followersHeader is the first line `xengine followers` prints: the column
// names, behind a '#' so a reader that skips comment lines reads only rows.
// The columns are fixed: a new one goes at the end, so a reader that names
// the first seven keeps working.
//
//	name        the -engine value and XENGINE_FOLLOWERS word
//	runner      the runner script, relative to the repository root
//	prep        the prep stage: xprep, forge-export (follower.Prep*)
//	jvm         true when the runner starts a JVM (the sweep's JVM slots)
//	runner_env  the variable that overrides runner (XENGINE_<NAME>_RUNNER)
//	mem_env     the variable that sets heavy-lock --mem (XENGINE_<NAME>_MEM)
//	wait_env    the variable that sets heavy-lock --wait (XENGINE_<NAME>_WAIT)
const followersHeader = "#name\trunner\tprep\tjvm\trunner_env\tmem_env\twait_env"

// runFollowers prints the follower registry as TSV for scripts/xengine-sweep.sh.
func runFollowers(args []string) error {
	fs := flag.NewFlagSet("followers", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "usage: xengine followers")
		fmt.Fprintln(fs.Output(), "prints one tab-separated row per registered follower, in registry order, after a header:")
		fmt.Fprintln(fs.Output(), followersHeader)
	}
	fs.Parse(args)
	if fs.NArg() != 0 {
		return fmt.Errorf("followers takes no arguments")
	}
	return writeFollowers(os.Stdout, follower.All())
}

func writeFollowers(w io.Writer, fs []follower.Follower) error {
	var b strings.Builder
	b.WriteString(followersHeader + "\n")
	for _, f := range fs {
		for _, v := range []string{f.Name, f.Runner, f.Prep} {
			if v == "" || strings.ContainsAny(v, "\t\n") {
				return fmt.Errorf("follower %q: field %q is empty or holds a tab or newline", f.Name, v)
			}
		}
		fmt.Fprintf(&b, "%s\t%s\t%s\t%t\t%s\t%s\t%s\n",
			f.Name, f.Runner, f.Prep, f.JVM, f.RunnerEnv(), f.MemEnv(), f.WaitEnv())
	}
	_, err := io.WriteString(w, b.String())
	return err
}
