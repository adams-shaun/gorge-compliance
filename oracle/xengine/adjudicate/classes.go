package adjudicate

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// ClassHeader is the first line of oracle/xengine/diagnosed-classes.tsv.
const ClassHeader = "sig\tpattern\tfield\tdesc\tcost\tcr\tupstream\tticket"

// ClassRow is one diagnosed class: a card-independent signature (or a matcher
// over its columns) a person has explained once, by a CR rule or an upstream
// bug. Pattern, Field, Desc ("Family:Desc") and Cost are the matcher; "*"
// matches anything. Sig is the signature that motivated the row.
type ClassRow struct{ Sig, Pattern, Field, Desc, Cost, CR, Upstream, Ticket string }

var (
	sigRE = regexp.MustCompile(`^[0-9a-f]{12}$`)
	crRE  = regexp.MustCompile(`^\d{3}(\.\d+[a-z]?)?$`)
)

// ReadClasses reads a class register; a missing file is no rows. A bad header,
// column count, sig, cr, a row citing neither a cr nor an upstream bug, or an
// empty ticket is an error naming the line.
func ReadClasses(path string) ([]ClassRow, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var rows []ClassRow
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimRight(sc.Text(), "\r")
		if n == 1 {
			if line != ClassHeader {
				return nil, fmt.Errorf("%s:1: header %q, want %q", path, line, ClassHeader)
			}
			continue
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		c := strings.Split(line, "\t")
		if len(c) != 8 {
			return nil, fmt.Errorf("%s:%d: %d columns, want 8", path, n, len(c))
		}
		r := ClassRow{c[0], c[1], c[2], c[3], c[4], c[5], c[6], c[7]}
		switch {
		case !sigRE.MatchString(r.Sig):
			return nil, fmt.Errorf("%s:%d: sig %q is not 12 lowercase hex digits", path, n, r.Sig)
		case r.Pattern == "" || r.Field == "" || r.Desc == "" || r.Cost == "":
			return nil, fmt.Errorf("%s:%d: an empty matcher column (use * for anything)", path, n)
		case r.CR != "-" && !crRE.MatchString(r.CR):
			return nil, fmt.Errorf("%s:%d: cr %q is neither - nor a CR rule like 702.4b", path, n, r.CR)
		case r.Upstream == "":
			return nil, fmt.Errorf("%s:%d: empty upstream (use -)", path, n)
		case r.CR == "-" && r.Upstream == "-":
			return nil, fmt.Errorf("%s:%d: neither cr nor upstream: a class needs a reason", path, n)
		case strings.TrimSpace(r.Ticket) == "":
			return nil, fmt.Errorf("%s:%d: empty ticket", path, n)
		}
		rows = append(rows, r)
	}
	return rows, sc.Err()
}

// Match is the first row, in file order, that matches s: its Sig is s's
// signature, or each of its four matcher columns is "*" or s's value.
func Match(rows []ClassRow, s SigInput) (ClassRow, bool) {
	sig := KeySignature(s)
	any := func(want, have string) bool { return want == "*" || want == have }
	for _, r := range rows {
		if r.Sig == sig ||
			(any(r.Pattern, s.Pattern) && any(r.Field, s.Field) && any(r.Desc, s.Family+":"+s.Desc) && any(r.Cost, s.Cost)) {
			return r, true
		}
	}
	return ClassRow{}, false
}

// Diagnose marks every finding of res that a class row matches, and its row,
// with the matching row's Sig. sigOf builds a finding's signature input.
func Diagnose(res *Result, rows []ClassRow, sigOf func(Finding) SigInput) {
	if len(rows) == 0 {
		return
	}
	for i := range res.Findings {
		if r, ok := Match(rows, sigOf(res.Findings[i])); ok {
			res.Findings[i].Diagnosed = r.Sig
			res.Rows[i].Diagnosed = r.Sig
		}
	}
}
