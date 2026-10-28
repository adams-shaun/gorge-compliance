package lower

import (
	"reflect"
	"strings"
	"sync"

	"github.com/adams-shaun/gorge/engine/cards"
)

// cardText memoises, per card, every exported string reachable from its faces,
// joined by NUL. A card is immutable once linked, so the text never changes.
var (
	cardTextMu sync.Mutex
	cardText   = map[*cards.Card]string{}
)

// cardMentions is true when any exported string anywhere in c.Faces contains one
// of words. A nil card mentions nothing.
func cardMentions(c *cards.Card, words ...string) bool {
	if c == nil {
		return false
	}
	cardTextMu.Lock()
	text, ok := cardText[c]
	if !ok {
		var b strings.Builder
		collectText(&b, reflect.ValueOf(c.Faces), map[uintptr]bool{})
		text = b.String()
		cardText[c] = text
	}
	cardTextMu.Unlock()
	for _, w := range words {
		if strings.Contains(text, w) {
			return true
		}
	}
	return false
}

// collectText appends every string reachable from v through exported struct
// fields, slices, arrays, maps (keys and values), pointers and interfaces. seen
// guards pointer cycles; only membership matters, so map order cannot reach a
// result.
func collectText(b *strings.Builder, v reflect.Value, seen map[uintptr]bool) {
	switch v.Kind() {
	case reflect.String:
		b.WriteString(v.String())
		b.WriteByte(0)
	case reflect.Pointer:
		if v.IsNil() || seen[v.Pointer()] {
			return
		}
		seen[v.Pointer()] = true
		collectText(b, v.Elem(), seen)
	case reflect.Interface:
		if !v.IsNil() {
			collectText(b, v.Elem(), seen)
		}
	case reflect.Struct:
		t := v.Type()
		for i := 0; i < t.NumField(); i++ {
			if t.Field(i).IsExported() {
				collectText(b, v.Field(i), seen)
			}
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			collectText(b, v.Index(i), seen)
		}
	case reflect.Array:
		for i := 0; i < v.Len(); i++ {
			collectText(b, v.Index(i), seen)
		}
	case reflect.Map:
		it := v.MapRange()
		for it.Next() {
			collectText(b, it.Key(), seen)
			collectText(b, it.Value(), seen)
		}
	}
}

// gameMentions is true when the card of any object of the game, in any zone
// (library and ceased included), mentions any of words. It is memoised per
// capture, per word list.
func (w *walker) gameMentions(words ...string) bool {
	key := strings.Join(words, "\x00")
	if r, ok := w.mention[key]; ok {
		return r
	}
	if w.mention == nil {
		w.mention = map[string]bool{}
	}
	r := false
	for i := range w.g.Objs {
		if cardMentions(w.g.Objs[i].Card, words...) {
			r = true
			break
		}
	}
	w.mention[key] = r
	return r
}
