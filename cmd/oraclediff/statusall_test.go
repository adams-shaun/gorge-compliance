package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/adopt"
)

// The workflow publishes a complete measurement on exit 3 and nothing on any
// other failure, so a ratchet failure has to stay distinguishable.
func TestRatchetFailureIsARatchetError(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll(filepath.Dir(adopt.RatchetFile), 0o755); err != nil {
		t.Fatal(err)
	}
	floor := 1
	if err := writeRatchetFile(map[string]adopt.RatchetEntry{"FDN": {OutstandingB: &floor}}); err != nil {
		t.Fatal(err)
	}
	var ratchet ratchetError
	if err := finishRatchetB([]adopt.SetStatus{{Set: "FDN", Outstanding: 2}}, false, nil); !errors.As(err, &ratchet) {
		t.Errorf("over the floor: got %v, want a ratchetError", err)
	}
	if err := finishRatchetB([]adopt.SetStatus{{Set: "FDN", Outstanding: 1}}, false, nil); err != nil {
		t.Errorf("at the floor: %v", err)
	}
}
