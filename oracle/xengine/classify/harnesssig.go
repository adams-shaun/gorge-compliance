package classify

import (
	"strings"
)

// HarnessSignature names why an owed item has no agreeing follower, so the
// keyproof `harness` reason splits into one row group per cause:
//
//	x=<class>[~<slug>]+f=<class>[~<slug>]
//
// x is the XMage follower's finding and f the Forge follower's. The slug is the
// finding's message cut down to what stays the same from item to item (digits,
// quoted names and the engine prefix are dropped), so every item that dies at
// the same driver limit, unanswered decision or missing scripted answer lands
// in one signature. A follower with no finding is class "none". The result
// has only [a-z0-9_.~=+-] and is a pure function of its two arguments.
func HarnessSignature(x, f Finding) string {
	return "x=" + engineSig(x) + "+f=" + engineSig(f)
}

func engineSig(f Finding) string {
	if f.ID == "" && f.Class == "" {
		return "none"
	}
	c := f.Class
	if c == "" {
		c = "none"
	}
	if s := msgSlug(f.Msg); s != "" {
		return c + "~" + s
	}
	return c
}

// slugWords is how many words of a message the slug keeps.
const slugWords = 6

// msgSlug lower-cases msg, drops a leading "<engine>: ", quoted text, digits
// and anything after the first colon past the prefix, and joins the first
// slugWords words with '-'.
func msgSlug(msg string) string {
	msg = strings.ToLower(strings.TrimSpace(msg))
	if i := strings.Index(msg, ": "); i >= 0 && !strings.ContainsAny(msg[:i], " ") {
		msg = msg[i+2:] // "forge: ..." / "xmage: ..."
	}
	if i := strings.Index(msg, ":"); i >= 0 {
		msg = msg[:i]
	}
	var b strings.Builder
	var words []string
	flush := func() {
		if b.Len() > 0 {
			words = append(words, b.String())
			b.Reset()
		}
	}
	quote := rune(0)
	for _, r := range msg {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '"' || r == '\'' || r == '`':
			flush()
			quote = r
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		default: // digits, spaces and punctuation end a word
			flush()
		}
	}
	flush()
	if len(words) > slugWords {
		words = words[:slugWords]
	}
	return strings.Join(words, "-")
}
