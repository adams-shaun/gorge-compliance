package cov

import (
	"bufio"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
)

// Counts are one key's counters. Exercised: used in a gorge game. Compared:
// used inside a lowered scenario every follower replayed to a non-harness
// verdict. Offered: offered by the engine and declined by the pilot.
type Counts struct{ Exercised, Compared, Offered int }

// Ledger maps card name -> key id -> counters.
type Ledger struct {
	m map[string]map[string]*Counts
}

// New returns an empty ledger.
func New() *Ledger { return &Ledger{m: map[string]map[string]*Counts{}} }

func (l *Ledger) at(card, key string) *Counts {
	if l.m == nil {
		l.m = map[string]map[string]*Counts{}
	}
	km := l.m[card]
	if km == nil {
		km = map[string]*Counts{}
		l.m[card] = km
	}
	c := km[key]
	if c == nil {
		c = &Counts{}
		km[key] = c
	}
	return c
}

// Seed adds a zero row for every key of c, so Report can tell a card with an
// unexercised key from a card whose keys are all compared. name is the corpus
// spelling the ledger is keyed by.
func (l *Ledger) Seed(name string, c *cards.Card) {
	for _, k := range Keys(c) {
		l.at(name, k.ID)
	}
}

// Add adds d to the counters of card's key.
func (l *Ledger) Add(card, key string, d Counts) {
	c := l.at(card, key)
	c.Exercised += d.Exercised
	c.Compared += d.Compared
	c.Offered += d.Offered
}

// Get returns the counters of card's key (zero when absent).
func (l *Ledger) Get(card, key string) Counts {
	if c := l.m[card][key]; c != nil {
		return *c
	}
	return Counts{}
}

// Uses is the sum of card's Exercised counters over all its keys.
func (l *Ledger) Uses(card string) int {
	n := 0
	for _, c := range l.m[card] {
		n += c.Exercised
	}
	return n
}

// Merge adds every counter of o into l.
func (l *Ledger) Merge(o *Ledger) {
	for card, km := range o.m {
		for key, c := range km {
			l.Add(card, key, *c)
		}
	}
}

const tsvHeader = "card\tkey\texercised\tcompared\toffered"

// WriteTSV writes the ledger sorted by card then key, under a header line.
func (l *Ledger) WriteTSV(w io.Writer) error {
	bw := bufio.NewWriter(w)
	fmt.Fprintln(bw, tsvHeader)
	cardsSorted := make([]string, 0, len(l.m))
	for c := range l.m {
		cardsSorted = append(cardsSorted, c)
	}
	sort.Strings(cardsSorted)
	for _, card := range cardsSorted {
		keys := make([]string, 0, len(l.m[card]))
		for k := range l.m[card] {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			c := l.m[card][k]
			fmt.Fprintf(bw, "%s\t%s\t%d\t%d\t%d\n", card, k, c.Exercised, c.Compared, c.Offered)
		}
	}
	return bw.Flush()
}

// ReadTSV parses WriteTSV's output. Duplicate rows add.
func ReadTSV(r io.Reader) (*Ledger, error) {
	l := New()
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	line := 0
	for sc.Scan() {
		line++
		s := sc.Text()
		if line == 1 {
			if s != tsvHeader {
				return nil, fmt.Errorf("coverage tsv: header %q, want %q", s, tsvHeader)
			}
			continue
		}
		if s == "" {
			continue
		}
		f := strings.Split(s, "\t")
		if len(f) != 5 {
			return nil, fmt.Errorf("coverage tsv line %d: %d fields, want 5", line, len(f))
		}
		var n [3]int
		for i := range n {
			v, err := strconv.Atoi(f[2+i])
			if err != nil || v < 0 {
				return nil, fmt.Errorf("coverage tsv line %d: bad count %q", line, f[2+i])
			}
			n[i] = v
		}
		l.Add(f[0], f[1], Counts{Exercised: n[0], Compared: n[1], Offered: n[2]})
	}
	return l, sc.Err()
}

// Report prints, per set code (sorted), the candidates, the cards exercised
// (their cast/land key used in a game), the cards fully compared (cast/land
// key and ALL ability keys compared) and the never-exercised cards. sets maps
// a set code to its candidate card names (corpus spelling). A card with no
// ledger rows counts as never exercised; "fully compared" needs the card's
// keys seeded (Ledger.Seed), else it reads only the keys that were hit.
func (l *Ledger) Report(w io.Writer, sets map[string][]string) {
	codes := make([]string, 0, len(sets))
	for c := range sets {
		codes = append(codes, c)
	}
	sort.Strings(codes)
	for _, code := range codes {
		names := append([]string(nil), sets[code]...)
		sort.Strings(names)
		exercised, compared := 0, 0
		var never []string
		for _, n := range names {
			km := l.m[n]
			head := km["cast"]
			if head == nil {
				head = km["land"]
			}
			if head == nil || head.Exercised == 0 {
				never = append(never, n)
			} else {
				exercised++
			}
			if head != nil && allCompared(km) {
				compared++
			}
		}
		fmt.Fprintf(w, "%s: candidates %d, exercised %d, fully compared %d, never exercised %d\n",
			code, len(names), exercised, compared, len(never))
		for _, n := range never {
			fmt.Fprintf(w, "  never exercised: %s\n", n)
		}
	}
}

func allCompared(km map[string]*Counts) bool {
	for _, c := range km {
		if c.Compared == 0 {
			return false
		}
	}
	return len(km) > 0
}
