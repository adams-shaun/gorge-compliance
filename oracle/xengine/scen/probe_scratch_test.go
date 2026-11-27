package scen

import (
	"sort"
	"strings"
	"testing"
)

func TestProbeStepsNeeded(t *testing.T) {
	reg, _ := testBuilder(t)
	var keys []string
	for k := range reg.Tokens {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if len(k) > 30 {
			continue
		}
		c := reg.Tokens[k]
		if c == nil || len(c.Faces) == 0 {
			continue
		}
		if strings.Contains(strings.ToLower(k), "blood") || strings.Contains(strings.ToLower(c.Faces[0].Name), "blood") {
			t.Logf("token %s name=%q types=%v", k, c.Faces[0].Name, c.Faces[0].Types)
		}
	}
	t.Logf("total tokens %d", len(keys))
}
