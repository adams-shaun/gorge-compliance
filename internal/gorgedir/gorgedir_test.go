package gorgedir

import (
	"os"
	"testing"
)

// TestPathFindsGorge: the helper resolves to a gorge checkout (its go.mod
// names the gorge module), not to an empty string joined onto elem.
func TestPathFindsGorge(t *testing.T) {
	raw, err := os.ReadFile(Path(t, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "module " + module; len(raw) < len(want) || string(raw[:len(want)]) != want {
		t.Fatalf("gorge go.mod starts %q, want %q", raw[:min(len(raw), 60)], want)
	}
}
