package pinfollow

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// TestGorgePinFollow runs scripts/gorge-pin-follow-test.sh: a pin behind
// gorge main is bumped on auto/gorge-pin off develop, an up-to-date bot branch
// is left alone, a moved develop or a newer main rebuilds it (so a red bump is
// superseded, never blocking), and a pin that is current, past main or off
// main is skipped. It needs git and bash only (local fixture repositories).
func TestGorgePinFollow(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	script, err := filepath.Abs("../../scripts/gorge-pin-follow-test.sh")
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("bash", script).CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v\n%s", script, err, out)
	}
	t.Logf("%s", out)
}
