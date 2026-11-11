package main

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/xengine/follower"
)

// TestWriteFollowers pins the TSV scripts/xengine-sweep.sh reads: the header,
// the column order, and the xmage and forge rows (their runner, prep and the
// variable names the script read before the registry).
func TestWriteFollowers(t *testing.T) {
	var b strings.Builder
	if err := writeFollowers(&b, follower.All()); err != nil {
		t.Fatal(err)
	}
	want := "#name\trunner\tprep\tjvm\trunner_env\tmem_env\twait_env\n" +
		"xmage\tscripts/xmage-oracle-run.sh\txprep\ttrue\tXENGINE_XMAGE_RUNNER\tXENGINE_XMAGE_MEM\tXENGINE_XMAGE_WAIT\n" +
		"forge\tscripts/forge-oracle-run.sh\tforge-export\ttrue\tXENGINE_FORGE_RUNNER\tXENGINE_FORGE_MEM\tXENGINE_FORGE_WAIT\n"
	if got := b.String(); got != want {
		t.Errorf("xengine followers =\n%s\nwant\n%s", got, want)
	}
}

func TestWriteFollowersNonJVM(t *testing.T) {
	var b strings.Builder
	f := follower.Follower{Name: "phase", Runner: "scripts/phase-oracle-run.sh", Prep: "follower-export"}
	if err := writeFollowers(&b, []follower.Follower{f}); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(b.String(), "\n"), "\n")
	want := "phase\tscripts/phase-oracle-run.sh\tfollower-export\tfalse\tXENGINE_PHASE_RUNNER\tXENGINE_PHASE_MEM\tXENGINE_PHASE_WAIT"
	if len(lines) != 2 || lines[1] != want {
		t.Errorf("rows = %q, want header + %q", lines, want)
	}
}

func TestWriteFollowersRefusesBadField(t *testing.T) {
	for _, f := range []follower.Follower{
		{Name: "a", Runner: "", Prep: "xprep"},
		{Name: "a\tb", Runner: "r", Prep: "xprep"},
	} {
		var b strings.Builder
		if err := writeFollowers(&b, []follower.Follower{f}); err == nil {
			t.Errorf("%+v: no error", f)
		}
	}
}

func TestRunFollowersRefusesArgs(t *testing.T) {
	if err := runFollowers([]string{"extra"}); err == nil {
		t.Error("runFollowers(extra): no error")
	}
}
