// Package gorgedir finds the gorge module's source directory (the sibling
// checkout, a go.work member or the module cache) for tests and tools that
// read gorge's own files, such as its deck lists or the residual hand-authored
// oracle scenarios gorge keeps (engine/rules/testdata/oracle).
//
// gorge-compliance's own files are read relative to its checkout; only files
// that gorge owns (they exist in gorge and not here) are resolved through
// this package.
package gorgedir

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

const module = "github.com/adams-shaun/gorge"

var (
	once sync.Once
	dir  string
	err  error
)

// Dir is `go list -m -f {{.Dir}} github.com/adams-shaun/gorge`.
func Dir() (string, error) {
	once.Do(func() {
		var out []byte
		out, err = exec.Command("go", "list", "-m", "-f", "{{.Dir}}", module).Output()
		if err != nil {
			var ee *exec.ExitError
			if errors.As(err, &ee) && len(ee.Stderr) > 0 {
				err = fmt.Errorf("gorgedir: go list -m %s: %v: %s", module, err, strings.TrimSpace(string(ee.Stderr)))
			} else {
				err = fmt.Errorf("gorgedir: go list -m %s: %w", module, err)
			}
			return
		}
		dir = strings.TrimSpace(string(out))
		if dir == "" {
			err = fmt.Errorf("gorgedir: go list -m %s reported no source directory", module)
		}
	})
	return dir, err
}

// Join is filepath.Join(Dir(), elem...): the path of a gorge-owned file.
func Join(elem ...string) (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(append([]string{d}, elem...)...), nil
}

// TB is the part of testing.TB that Path uses; it is declared here so that
// production binaries importing this package do not link package testing.
type TB interface {
	Helper()
	Fatal(args ...any)
}

// Path is Join for tests: it fails t (t.Fatal) when gorge cannot be located,
// so a test that needs gorge's files never silently reads an empty directory.
// Pass a *testing.T or *testing.B.
func Path(t TB, elem ...string) string {
	t.Helper()
	p, err := Join(elem...)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// Default is Join for a command-line flag default: the gorge-owned path when
// gorge can be located, else fallback (a flag default cannot fail; the
// command reports a missing directory when it reads it).
func Default(fallback string, elem ...string) string {
	if p, err := Join(elem...); err == nil {
		return p
	}
	return fallback
}
