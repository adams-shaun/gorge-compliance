// Package game plays one gorge game between two explore-policy bots and
// records it as a Tape: every committed decision with its answer, the mana
// abilities each one fired, the randomness it consumed, and a state Digest
// taken just before it. The lowering steps (xe-4, xe-5) replay the tape in
// XMage and Forge and self-check against the digests.
package game

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/engine/rules"
	"github.com/adams-shaun/gorge/engine/state"
)

// digestLibraryTop is how many library cards a digest names, top first
// (rules' oracleLibraryTopN).
const digestLibraryTop = 5

// Digest is the engine-neutral slice of game state the tape self-checks
// against. It is defined once, here, and computed from either a live engine
// (DigestOfGame) or a scenario snapshot (DigestOfSnapshot); the two agree for
// the same state. It carries no object ids, timestamps or library order below
// the top five cards.
type Digest struct {
	Turn int32  `json:"turn"`
	Step string `json:"step"`
	// Active is the active seat; Priority the seat holding priority (the
	// snapshot reports it at every checkpoint, so the digest does too).
	Active   int            `json:"active"`
	Priority int            `json:"priority"`
	Players  []PlayerDigest `json:"players"`
	Field    []PermDigest   `json:"field"`
}

// PlayerDigest is one seat. Hand and Exile are sorted names, Graveyard is in
// zone order, LibraryTop is top first.
type PlayerDigest struct {
	Seat         int              `json:"seat"`
	Life         int32            `json:"life"`
	Counters     map[string]int32 `json:"counters,omitempty"`
	Hand         []string         `json:"hand"`
	Graveyard    []string         `json:"graveyard"`
	Exile        []string         `json:"exile"`
	Command      []string         `json:"command,omitempty"` // sorted; Commander games
	LibraryCount int              `json:"library_count"`
	LibraryTop   []string         `json:"library_top"`
	Pool         string           `json:"pool"` // WUBRGC letters
	// CommanderCasts and CommanderDamage are the snapshot's commander tallies
	// (CR 903.8, 903.10a): commander front name -> times cast from the command
	// zone, and "pN:<front name>" (N the commander's owner) -> combat damage
	// this seat has taken from it. Non-zero entries only, nil when none.
	CommanderCasts  map[string]int32 `json:"commander_casts,omitempty"`
	CommanderDamage map[string]int32 `json:"commander_damage,omitempty"`
}

// PermDigest is one battlefield object. Field is ordered by controller, then
// name, then the object's creation order (the snapshot's ref ordinal).
type PermDigest struct {
	Name       string           `json:"name"`
	Controller int              `json:"controller"`
	Owner      int              `json:"owner"`
	Token      bool             `json:"token,omitempty"`
	Tapped     bool             `json:"tapped,omitempty"`
	PT         string           `json:"pt,omitempty"` // creatures only
	Damage     int32            `json:"damage,omitempty"`
	Counters   map[string]int32 `json:"counters,omitempty"`
	AttachedTo string           `json:"attached_to,omitempty"` // name, or "pN" for a player
}

// normCounter is rules.normCounter (oracle_run.go): the snapshot's counter
// vocabulary. The table is copied because the function is unexported.
func normCounter(k string) string {
	switch strings.ReplaceAll(strings.ToLower(k), " ", "") {
	case "+1/+1", "p1p1":
		return "P1P1"
	case "-1/-1", "m1m1":
		return "M1M1"
	}
	return strings.ToUpper(k)
}

// poolString is rules.poolString: the pool as WUBRGC letters.
func poolString(m state.Mana) string {
	var b strings.Builder
	for i, c := range "WUBRGC" {
		b.WriteString(strings.Repeat(string(c), int(m[i])))
	}
	return b.String()
}

// counterMap is the live object's counters as a digest map. A zero amount is
// not a counter: an object keeps a zero entry after its last counter is
// removed (a stun counter spent by the untap step), while the runner's fresh
// placement never had one. Dropping zeros keeps the two digests comparable.
func counterMap(cs []state.Counter) map[string]int32 {
	if len(cs) == 0 {
		return nil
	}
	m := map[string]int32{}
	for _, c := range cs {
		m[normCounter(c.Kind)] += c.N
	}
	for k, n := range m {
		if n == 0 {
			delete(m, k)
		}
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

func hasType(types []string, t string) bool {
	for _, x := range types {
		if strings.EqualFold(x, t) {
			return true
		}
	}
	return false
}

// DigestOfGame is the digest of a live engine. It only reads.
func DigestOfGame(e *rules.Engine) Digest {
	g := e.G
	// faceName is the oracle snapshot's objName: the printed face name.
	faceName := func(o *state.Object) string {
		if o == nil || o.Face() == nil {
			return ""
		}
		return o.Face().Name
	}
	zone := func(z state.Zone, p state.PlayerID, sorted bool) []string {
		ids := g.Zone(z, p)
		out := make([]string, 0, len(ids))
		for _, id := range ids {
			// A nameless object is an ability the engine parked in exile, not a card.
			if n := faceName(g.Obj(id)); n != "" {
				out = append(out, n)
			}
		}
		if sorted {
			sort.Strings(out)
		}
		return out
	}
	// fieldName is the snapshot's fieldName: a face-down permanent has no
	// name, a bound layer-3 name wins, otherwise the printed face name.
	fieldName := func(o *state.Object) string {
		if o == nil || o.Face() == nil {
			return ""
		}
		if o.FaceDown {
			return ""
		}
		if c := e.Derived(o.ID); c.NameSet {
			return c.Name
		}
		return o.Face().Name
	}
	d := Digest{
		Turn: g.Turn, Step: g.Step.String(), Active: int(g.Active), Priority: int(g.Priority),
		Players: []PlayerDigest{}, Field: []PermDigest{},
	}
	for i := range g.Players {
		p := state.PlayerID(i)
		lib := g.Zone(state.ZLibrary, p)
		top := make([]string, 0, digestLibraryTop)
		for j := 0; j < len(lib) && j < digestLibraryTop; j++ {
			top = append(top, faceName(g.Obj(lib[j])))
		}
		casts, damage := cmdTallies(g, i)
		d.Players = append(d.Players, PlayerDigest{
			CommanderCasts: casts, CommanderDamage: damage,
			Seat: i, Life: g.Players[i].Life, Counters: counterMap(g.Players[i].Counters),
			Hand: zone(state.ZHand, p, true), Graveyard: zone(state.ZGraveyard, p, false),
			Exile: zone(state.ZExile, p, true), Command: nilIfEmpty(zone(state.ZCommand, p, true)), LibraryCount: len(lib), LibraryTop: top,
			Pool: poolString(g.Players[i].Pool),
		})
	}
	var field []state.ObjID
	for i := range g.Players {
		field = append(field, g.Zone(state.ZBattlefield, state.PlayerID(i))...)
	}
	sort.Slice(field, func(a, b int) bool { return field[a] < field[b] })
	for _, id := range field {
		o := g.Obj(id)
		pd := PermDigest{
			Name: fieldName(o), Controller: int(o.Controller), Owner: int(o.Owner),
			Token: o.IsToken, Tapped: o.Tapped, Damage: o.Damage, Counters: counterMap(o.Counters),
		}
		// The snapshot decides "creature" from the derived type list.
		if hasType(e.Derived(id).Types, "Creature") {
			pd.PT = fmt.Sprintf("%d/%d", e.Power(id), e.Toughness(id))
		}
		switch {
		case o.AttachedTo != 0:
			pd.AttachedTo = faceName(g.Obj(o.AttachedTo))
		case o.HasAttachedPlayer:
			pd.AttachedTo = fmt.Sprintf("p%d", o.AttachedPlayer)
		}
		d.Field = append(d.Field, pd)
	}
	sortField(d.Field)
	return d
}

// sortField orders the battlefield by controller, then name, then by the
// permanent's remaining fields. Object ids and creation order are not part of
// a digest (the runner mints a scenario's objects in setup order, the live
// game in deck order), so two same-named permanents of one controller are
// told apart by what they show, never by which was made first.
func sortField(f []PermDigest) {
	key := func(p PermDigest) string {
		d := Digest{Field: []PermDigest{p}}
		var b strings.Builder
		for _, kv := range d.flatten() {
			if strings.HasPrefix(kv[0], "field[0].") {
				b.WriteString(kv[1])
				b.WriteByte(0)
			}
		}
		return b.String()
	}
	keys := make([]string, len(f))
	for i := range f {
		keys[i] = key(f[i])
	}
	idx := make([]int, len(f))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool {
		x, y := f[idx[a]], f[idx[b]]
		if x.Controller != y.Controller {
			return x.Controller < y.Controller
		}
		if x.Name != y.Name {
			return x.Name < y.Name
		}
		return keys[idx[a]] < keys[idx[b]]
	})
	out := make([]PermDigest, len(f))
	for i, j := range idx {
		out[i] = f[j]
	}
	copy(f, out)
}

// DigestOfSnapshot is the digest of an oracle snapshot: the same struct
// DigestOfGame builds for the state the snapshot was taken of.
func DigestOfSnapshot(s rules.OracleSnapshot) Digest {
	d := Digest{
		Turn: s.Turn, Step: s.Step, Active: s.Active, Priority: s.Priority,
		Players: []PlayerDigest{}, Field: []PermDigest{},
	}
	// nonNil copies x without its nameless entries: the engine parks resolved
	// abilities in exile, which DigestOfGame leaves out too.
	nonNil := func(x []string) []string {
		out := []string{}
		for _, n := range x {
			if n != "" {
				out = append(out, n)
			}
		}
		return out
	}
	for _, p := range s.Players {
		top := nonNil(p.LibraryTop)
		if len(top) > digestLibraryTop {
			top = top[:digestLibraryTop]
		}
		d.Players = append(d.Players, PlayerDigest{
			Seat: p.Seat, Life: p.Life, Counters: copyCounters(p.Counters),
			Hand: nonNil(p.Hand), Graveyard: nonNil(p.Graveyard), Exile: nonNil(p.Exile),
			Command:        sortedNonEmpty(nonNil(p.Command)),
			CommanderCasts: copyCounters(p.CommanderCasts), CommanderDamage: copyCounters(p.CommanderDamage),
			LibraryCount: p.LibraryCount, LibraryTop: top, Pool: p.Pool,
		})
	}
	for _, p := range s.Permanents {
		d.Field = append(d.Field, PermDigest{
			Name: p.Name, Controller: p.Controller, Owner: p.Owner, Token: p.Token, Tapped: p.Tapped,
			PT: p.PT, Damage: p.Damage, Counters: copyCounters(p.Counters), AttachedTo: refName(p.AttachedTo),
		})
	}
	sortField(d.Field)
	return d
}

// cmdTallies is seat i's commander tax counts and commander damage as the
// snapshot reports them (rules.cmdTallies): front-face names, damage keyed
// "pN:<name>" of the commander's owner, in gorge's dense commander order
// (seat order, then each seat's Commanders order). Non-zero entries only.
func cmdTallies(g *state.Game, i int) (casts, damage map[string]int32) {
	front := func(id state.ObjID) string {
		if o := g.Obj(id); o != nil && o.Card != nil && len(o.Card.Faces) > 0 {
			return o.Card.Faces[0].Name
		}
		return ""
	}
	pl := &g.Players[i]
	for k, id := range pl.Commanders {
		if k >= len(pl.CmdCasts) {
			break
		}
		if n := pl.CmdCasts[k]; n != 0 {
			if casts == nil {
				casts = map[string]int32{}
			}
			casts[front(id)] = n
		}
	}
	dense := 0
	for s := range g.Players {
		for _, id := range g.Players[s].Commanders {
			if dense < len(pl.CmdDamage) {
				if n := pl.CmdDamage[dense]; n != 0 {
					if damage == nil {
						damage = map[string]int32{}
					}
					damage[fmt.Sprintf("p%d:%s", s, front(id))] = n
				}
			}
			dense++
		}
	}
	return casts, damage
}

// nilIfEmpty is x, or nil when x is empty: an empty command zone is no field.
func nilIfEmpty(x []string) []string {
	if len(x) == 0 {
		return nil
	}
	return x
}

// sortedNonEmpty is x sorted, or nil when x is empty.
func sortedNonEmpty(x []string) []string {
	if len(x) == 0 {
		return nil
	}
	sort.Strings(x)
	return x
}

// copyCounters copies a snapshot's counter map, dropping zero amounts: a zero
// is no counter, and the live digest drops them too (counterMap).
func copyCounters(m map[string]int32) map[string]int32 {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]int32, len(m))
	for k, v := range m {
		if v != 0 {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// refName is the object name inside a scenario ref ("p0:Name", "p1:token:Name#2"),
// or the ref itself for a player ("p1") or the empty ref.
func refName(ref string) string {
	i := strings.IndexByte(ref, ':')
	if i < 0 {
		return ref
	}
	name := strings.TrimPrefix(ref[i+1:], "token:")
	if j := strings.LastIndexByte(name, '#'); j >= 0 {
		if _, err := strconv.Atoi(name[j+1:]); err == nil {
			name = name[:j]
		}
	}
	return name
}

// Diff lists where o differs from d, in the order Digest's fields are
// defined, each as "path: d-value != o-value" with paths like players[1].life
// or field[0].tapped. Nil means equal.
func (d Digest) Diff(o Digest) []string {
	a, b := d.flatten(), o.flatten()
	bv := make(map[string]string, len(b))
	for _, kv := range b {
		bv[kv[0]] = kv[1]
	}
	seen := make(map[string]bool, len(a))
	var out []string
	for _, kv := range a {
		seen[kv[0]] = true
		if v, ok := bv[kv[0]]; !ok {
			out = append(out, fmt.Sprintf("%s: %s != <absent>", kv[0], kv[1]))
		} else if v != kv[1] {
			out = append(out, fmt.Sprintf("%s: %s != %s", kv[0], kv[1], v))
		}
	}
	for _, kv := range b {
		if !seen[kv[0]] {
			out = append(out, fmt.Sprintf("%s: <absent> != %s", kv[0], kv[1]))
		}
	}
	return out
}

// flatten lists every digest value as an ordered (path, value) pair.
func (d Digest) flatten() [][2]string {
	var out [][2]string
	add := func(path string, v any) { out = append(out, [2]string{path, fmt.Sprintf("%v", v)}) }
	list := func(path string, xs []string) {
		add(path+".len", len(xs))
		for i, x := range xs {
			add(fmt.Sprintf("%s[%d]", path, i), strconv.Quote(x))
		}
	}
	counters := func(path string, m map[string]int32) {
		ks := make([]string, 0, len(m))
		for k := range m {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		for _, k := range ks {
			add(path+"."+k, m[k])
		}
	}
	add("turn", d.Turn)
	add("step", strconv.Quote(d.Step))
	add("active", d.Active)
	add("priority", d.Priority)
	add("players.len", len(d.Players))
	for i, p := range d.Players {
		pre := fmt.Sprintf("players[%d]", i)
		add(pre+".seat", p.Seat)
		add(pre+".life", p.Life)
		counters(pre+".counters", p.Counters)
		list(pre+".hand", p.Hand)
		list(pre+".graveyard", p.Graveyard)
		list(pre+".exile", p.Exile)
		if len(p.Command) > 0 {
			list(pre+".command", p.Command)
		}
		if len(p.CommanderCasts) > 0 {
			counters(pre+".commander_casts", p.CommanderCasts)
		}
		if len(p.CommanderDamage) > 0 {
			counters(pre+".commander_damage", p.CommanderDamage)
		}
		add(pre+".library_count", p.LibraryCount)
		list(pre+".library_top", p.LibraryTop)
		add(pre+".pool", strconv.Quote(p.Pool))
	}
	add("field.len", len(d.Field))
	for i, p := range d.Field {
		pre := fmt.Sprintf("field[%d]", i)
		add(pre+".name", strconv.Quote(p.Name))
		add(pre+".controller", p.Controller)
		add(pre+".owner", p.Owner)
		add(pre+".token", p.Token)
		add(pre+".tapped", p.Tapped)
		add(pre+".pt", strconv.Quote(p.PT))
		add(pre+".damage", p.Damage)
		counters(pre+".counters", p.Counters)
		add(pre+".attached_to", strconv.Quote(p.AttachedTo))
	}
	return out
}

// Swapped is the digest with seats 0 and 1 exchanged: the seat numbers,
// the player list, the controllers and owners, the "pN" attachment names and
// the "pN:" prefix of a commander damage key.
// The lowering relabels a tape whose starting player is seat 1 this way.
func (d Digest) Swapped() Digest {
	flip := func(i int) int { return 1 - i }
	out := d
	out.Active, out.Priority = flip(d.Active), flip(d.Priority)
	out.Players = make([]PlayerDigest, len(d.Players))
	for i, p := range d.Players {
		p.Seat = flip(p.Seat)
		if p.CommanderDamage != nil {
			sw := make(map[string]int32, len(p.CommanderDamage))
			for k, n := range p.CommanderDamage {
				sw[swapSeatRef(k)] = n
			}
			p.CommanderDamage = sw
		}
		out.Players[flip(i)] = p
	}
	out.Field = make([]PermDigest, len(d.Field))
	for i, p := range d.Field {
		p.Controller, p.Owner = flip(p.Controller), flip(p.Owner)
		if n, err := strconv.Atoi(strings.TrimPrefix(p.AttachedTo, "p")); err == nil && strings.HasPrefix(p.AttachedTo, "p") {
			p.AttachedTo = fmt.Sprintf("p%d", flip(n))
		}
		out.Field[i] = p
	}
	sortField(out.Field)
	return out
}

// swapSeatRef exchanges seats 0 and 1 in a "pN:<name>" ref key.
func swapSeatRef(k string) string {
	i := strings.IndexByte(k, ':')
	if i < 2 || k[0] != 'p' {
		return k
	}
	n, err := strconv.Atoi(k[1:i])
	if err != nil || n > 1 {
		return k
	}
	return fmt.Sprintf("p%d%s", 1-n, k[i:])
}
