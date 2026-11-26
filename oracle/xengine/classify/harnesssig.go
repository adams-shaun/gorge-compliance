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

// msgSlug lower-cases msg and drops the leading "<engine>: " and exception
// type prefixes ("xmage: AssertionError: ...", "strict_miss: AssertionError:
// ..."), so the slug names the cause and not the wrapper. A "leftover:" tag is
// kept as a word. It then keeps the first line only, cuts at its first colon,
// drops quoted text and digits, and joins the first slugWords words with '-'.
func msgSlug(msg string) string {
	msg = strings.ToLower(strings.TrimSpace(msg))
	tag := ""
	for {
		i := colonEnd(msg)
		if i < 0 {
			break
		}
		head := strings.TrimSpace(msg[:i])
		if head == "" || strings.ContainsAny(head, " \t\r\n") {
			break
		}
		if head == "leftover" {
			tag = "leftover "
		}
		msg = strings.TrimSpace(msg[i+1:])
	}
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		msg = msg[:i]
	}
	if i := strings.Index(msg, ":"); i >= 0 {
		msg = msg[:i]
	}
	msg = tag + msg
	var b strings.Builder
	var words []string
	flush := func() {
		if b.Len() > 0 {
			words = append(words, b.String())
			b.Reset()
		}
	}
	quote, prev := rune(0), rune(0)
	for _, r := range msg {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '"' || r == '`' || (r == '\'' && !(prev >= 'a' && prev <= 'z')):
			// an apostrophe inside a word ("can't") is not a quote
			flush()
			quote = r
		case r == '\'':
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		default: // digits, spaces and punctuation end a word
			flush()
		}
		prev = r
	}
	flush()
	if len(words) > slugWords {
		words = words[:slugWords]
	}
	return strings.Join(words, "-")
}

// colonEnd is the index of the first colon of s that ends a token (followed by
// whitespace or the end of s), or -1.
func colonEnd(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == ':' && (i+1 == len(s) || s[i+1] == ' ' || s[i+1] == '\n' || s[i+1] == '\t') {
			return i
		}
	}
	return -1
}
