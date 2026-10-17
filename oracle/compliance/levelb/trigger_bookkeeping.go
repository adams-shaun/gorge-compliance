package levelb

import (
	"strings"

	"github.com/adams-shaun/gorge/engine/cards"
)

// BookkeepingSub is the level-B sub-family of a "bookkeeping" trigger: a
// Static$ True line (it never uses the stack, CR 603.2 does not apply to it
// as a stack object) whose whole effect records or clears a fact another
// trigger on the same face reads -- a stored SVar, the card's remembered or
// imprinted set. Spider-Man 2099's "you've played a land from anywhere other
// than your hand this turn" (a LandPlayed StoreSVar and its Cleanup-step
// reset), Desert Were-Worm's "for the first time each turn" switch (reset at
// TurnBegin), Planetarium of Wan Shi Tong's "Do this only once each turn"
// (Remembered, cleared at TurnBegin and when it leaves), Oblivious
// Bookworm's "you turned a permanent face up this turn" (Imprinted, cleared
// at upkeep), Superior Foes of Spider-Man's exile link (Imprinted, cleared
// when it leaves).
//
// Such a trigger has no observable consequence of its own; it is observed
// through its CONSUMER, the sibling trigger that reads the fact. The
// template stages the bookkeeping trigger's cause, lets it run, then reaches
// the consumer -- on the same turn for a record, on p0's next turn for a
// turn-boundary reset, after a flicker of the source for a leaves-the-
// battlefield cleanup -- and compares that observation with XMage.
const BookkeepingSub = "trigger.bookkeeping"

// Bookkeeping kinds: how the template reaches the consumer.
const (
	// BookkeepingRecord writes a fact (a land played from a non-hand zone,
	// a permanent turned face up) the consumer reads later the same turn.
	BookkeepingRecord = "record"
	// BookkeepingReset clears a fact at a turn boundary (TurnBegin, a Phase
	// step): the consumer is observed again on p0's next turn.
	BookkeepingReset = "reset"
	// BookkeepingLeaves clears a fact when the source leaves the battlefield:
	// the consumer is observed again after the source is flickered (CR 400.7,
	// the returned permanent is a new object).
	BookkeepingLeaves = "ltb"
)

// Bookkeeping is one bookkeeping trigger's plan.
type Bookkeeping struct {
	Kind string
	// Key names the fact: "svar:<name>", "remembered" or "imprinted".
	Key string
	// Consumer is the index into Face.Triggers of the stack-using trigger
	// that reads Key.
	Consumer int
	// Recorder is the index of the trigger that writes Key before a reset
	// or a leaves cleanup can be observed: a sibling Static$ record line, or
	// Consumer itself when its own resolution writes the fact (Desert
	// Were-Worm's switch, Planetarium's RememberPlayed$). It equals the
	// bookkeeping trigger's own index for a record.
	Recorder int
}

// BookkeepingOf returns the plan of f.Triggers[idx], or false when the line
// is not a bookkeeping trigger the template can observe.
func BookkeepingOf(f *cards.Face, idx int) (Bookkeeping, bool) {
	if f == nil || idx < 0 || idx >= len(f.Triggers) {
		return Bookkeeping{}, false
	}
	t := &f.Triggers[idx]
	if !strings.EqualFold(t.ParamStr(cards.PKStatic), "True") {
		return Bookkeeping{}, false
	}
	writes, clears, ok := bookkeepingEffect(f, t.ParamStr(cards.PKExecute))
	if !ok || len(writes)+len(clears) != 1 {
		return Bookkeeping{}, false
	}
	var b Bookkeeping
	switch {
	case t.ModeKind() == cards.TriggerChangesZone && strings.EqualFold(t.ParamStr(cards.PKDestination), "Battlefield"):
		// A self-ETB reset is settled by the cast-resolve scenario (or is
		// some other enters line): not this family.
		return Bookkeeping{}, false
	case selfLeavesBattlefield(t):
		b.Kind = BookkeepingLeaves
	case t.ModeKind() == cards.TriggerTurnBegin || t.ModeKind() == cards.TriggerPhase:
		b.Kind = BookkeepingReset
	default:
		b.Kind = BookkeepingRecord
	}
	if b.Kind == BookkeepingRecord {
		if len(writes) != 1 {
			return Bookkeeping{}, false
		}
		b.Key = writes[0]
	} else {
		// A reset or cleanup line either clears the set or stores the SVar's
		// initial value back.
		if len(clears) == 1 {
			b.Key = clears[0]
		} else {
			b.Key = writes[0]
		}
	}
	b.Consumer = -1
	for i := range f.Triggers {
		c := &f.Triggers[i]
		if i == idx || strings.EqualFold(c.ParamStr(cards.PKStatic), "True") {
			continue
		}
		if bookkeepingReads(f, c, b.Key) {
			b.Consumer = i
			break
		}
	}
	if b.Consumer < 0 {
		return Bookkeeping{}, false
	}
	switch b.Kind {
	case BookkeepingRecord:
		b.Recorder = idx
	default:
		b.Recorder = -1
		for i := range f.Triggers {
			r := &f.Triggers[i]
			if i == idx || !strings.EqualFold(r.ParamStr(cards.PKStatic), "True") {
				continue
			}
			if key, ok := recordKey(f, r); ok && key == b.Key {
				b.Recorder = i
				break
			}
		}
		if b.Recorder < 0 && containsString(bookkeepingChainWrites(f, f.Triggers[b.Consumer].ParamStr(cards.PKExecute)), b.Key) {
			b.Recorder = b.Consumer
		}
		if b.Recorder < 0 {
			return Bookkeeping{}, false
		}
	}
	return b, true
}

// recordKey returns the single fact a Static$ record line writes (its mode is
// neither a turn boundary nor a self zone change). It does not look for a
// consumer, so a reset's recorder search cannot recurse.
func recordKey(f *cards.Face, t *cards.Trigger) (string, bool) {
	if !strings.EqualFold(t.ParamStr(cards.PKStatic), "True") || t.ModeKind() == cards.TriggerChangesZone ||
		t.ModeKind() == cards.TriggerTurnBegin || t.ModeKind() == cards.TriggerPhase {
		return "", false
	}
	writes, clears, ok := bookkeepingEffect(f, t.ParamStr(cards.PKExecute))
	if !ok || len(writes) != 1 || len(clears) != 0 {
		return "", false
	}
	return writes[0], true
}

// classifyBookkeeping names a bookkeeping trigger with a plan.
func classifyBookkeeping(f *cards.Face, t *cards.Trigger) (string, bool) {
	if !strings.EqualFold(t.ParamStr(cards.PKStatic), "True") {
		return "", false
	}
	for i := range f.Triggers {
		if &f.Triggers[i] != t {
			continue
		}
		if _, ok := BookkeepingOf(f, i); ok {
			return BookkeepingSub, true
		}
	}
	return "", false
}

// bookkeepingEffect walks an Execute$ chain and reports the facts it writes
// and clears. ok is false when any link does more than bookkeeping.
func bookkeepingEffect(f *cards.Face, execute string) (writes, clears []string, ok bool) {
	seen := map[string]bool{}
	for name := strings.TrimSpace(execute); name != ""; {
		if seen[name] {
			return nil, nil, false
		}
		seen[name] = true
		body, found := f.SVars[name]
		if !found {
			return nil, nil, false
		}
		p := bookkeepingParams(body)
		switch {
		case p["DB"] == "StoreSVar" || p["AB"] == "StoreSVar":
			v := strings.TrimSpace(p["SVar"])
			if v == "" {
				return nil, nil, false
			}
			writes = append(writes, "svar:"+v)
		case p["DB"] == "Cleanup":
			for k, key := range map[string]string{"ClearRemembered": "remembered", "ClearImprinted": "imprinted"} {
				if strings.EqualFold(p[k], "True") {
					clears = append(clears, key)
				}
			}
			for k := range p {
				switch k {
				case "DB", "ClearRemembered", "ClearImprinted", "SubAbility":
				default:
					return nil, nil, false
				}
			}
		case p["DB"] == "Pump":
			for k := range p {
				switch k {
				case "DB", "SubAbility", "Defined":
				case "ImprintCards":
					writes = append(writes, "imprinted")
				case "RememberObjects":
					writes = append(writes, "remembered")
				default:
					return nil, nil, false
				}
			}
		default:
			return nil, nil, false
		}
		name = strings.TrimSpace(p["SubAbility"])
	}
	sortStrings(writes)
	sortStrings(clears)
	return writes, clears, len(writes)+len(clears) > 0
}

// bookkeepingChainWrites lists the facts a stack-using Execute$ chain writes
// as a side effect of its resolution (the consumer that records itself).
func bookkeepingChainWrites(f *cards.Face, execute string) []string {
	var out []string
	seen := map[string]bool{}
	for name := strings.TrimSpace(execute); name != "" && !seen[name]; {
		seen[name] = true
		p := bookkeepingParams(f.SVars[name])
		if (p["DB"] == "StoreSVar" || p["AB"] == "StoreSVar") && p["SVar"] != "" {
			out = append(out, "svar:"+strings.TrimSpace(p["SVar"]))
		}
		for k, v := range p {
			if !strings.EqualFold(strings.TrimSpace(v), "True") {
				continue
			}
			switch {
			case strings.HasPrefix(k, "Remember"):
				out = append(out, "remembered")
			case strings.HasPrefix(k, "Imprint"):
				out = append(out, "imprinted")
			}
		}
		name = strings.TrimSpace(p["SubAbility"])
	}
	sortStrings(out)
	return out
}

// bookkeepingReads reports whether trigger c's own parameters or Execute$
// chain, with every SVar they name expanded, read key.
func bookkeepingReads(f *cards.Face, c *cards.Trigger, key string) bool {
	var text strings.Builder
	for _, k := range sortedKeys(c.Params) {
		text.WriteString(k + "$ " + c.Params[k] + " | ")
	}
	seen := map[string]bool{}
	frontier := []string{text.String()}
	for depth := 0; depth < 6 && len(frontier) > 0; depth++ {
		var next []string
		for _, body := range frontier {
			text.WriteString(body + " | ")
			for _, name := range sortedKeys(f.SVars) {
				if seen[name] || !containsToken(body, name) {
					continue
				}
				seen[name] = true
				next = append(next, f.SVars[name])
			}
		}
		frontier = next
	}
	all := text.String()
	switch {
	case strings.HasPrefix(key, "svar:"):
		return containsToken(all, strings.TrimPrefix(key, "svar:"))
	case key == "remembered":
		return strings.Contains(all, "Defined$ Remembered") || strings.Contains(all, "RememberedSize")
	case key == "imprinted":
		return strings.Contains(all, "Defined$ Imprinted") || strings.Contains(all, "ImprintedSize")
	}
	return false
}

// bookkeepingParams splits one SVar body ("DB$ Cleanup | ClearRemembered$
// True") into its parameters.
func bookkeepingParams(body string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.Split(body, "|") {
		k, v, ok := strings.Cut(part, "$")
		if !ok {
			continue
		}
		out[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return out
}

// containsToken reports whether name occurs in s as a whole identifier.
func containsToken(s, name string) bool {
	if name == "" {
		return false
	}
	for i := 0; ; {
		j := strings.Index(s[i:], name)
		if j < 0 {
			return false
		}
		j += i
		end := j + len(name)
		if (j == 0 || !identByte(s[j-1])) && (end == len(s) || !identByte(s[end])) {
			return true
		}
		i = j + 1
	}
}

func identByte(b byte) bool {
	return b == '_' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sortStrings(out)
	return out
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
