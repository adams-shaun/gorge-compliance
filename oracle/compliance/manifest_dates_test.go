package compliance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestCommittedManifestDatesParse: every committed set manifest's release date is a real
// YYYY-MM-DD date. carddb's cert_sets fold refuses any other value, and a manifest written
// before manifest.go normalized XMage's buildDate kept "2009-00-31" (Conflux) for days.
func TestCommittedManifestDatesParse(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("manifests", "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no manifests found: %v", err)
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var m struct {
			Released string `json:"released"`
		}
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if m.Released == "" {
			continue
		}
		if _, err := time.Parse("2006-01-02", m.Released); err != nil {
			t.Errorf("%s: released %q is not a YYYY-MM-DD date", f, m.Released)
		}
	}
}
