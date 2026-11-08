// Package rowio reads newline-delimited rows under a per-row size cap.
//
// A bufio.Scanner fails the whole stream on one row over its cap. The xengine
// follower rows are JSONL written by a Java driver, and a runaway transcript
// can produce a row far over any useful cap; failing the stream loses every
// other row in the chunk. Read instead reports an oversized row to an
// overflow callback and moves on, so the caller can turn it into a classified
// harness row. The row's prefix is kept because the drivers write the identity
// fields (name, id) before the snapshots that can run away.
package rowio

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/adams-shaun/gorge-compliance/oracle/compliance/oraclediff"
)

const (
	// DefaultCap is the per-row cap of the xengine follower row readers. A
	// row at most this long is read whole; a longer row is handed to the
	// overflow callback.
	DefaultCap = 256 << 20

	// maxPrefix is how much of an oversized row is kept for the overflow
	// callback. The drivers write name and id first, so this is ample even
	// for a row whose snapshots run to gigabytes.
	maxPrefix = 1 << 20
)

// Oversized is one row longer than the reader's cap.
type Oversized struct {
	// Prefix is the first min(Size, maxPrefix, Cap) bytes of the row, without
	// the newline: enough to recover the fields a driver writes first.
	Prefix []byte
	// Size is the row's length in bytes, without the newline.
	Size int
	// Cap is the limit Size exceeded.
	Cap int
}

// Reason is the harness message an oversized follower row is classified with.
func (o Oversized) Reason() string {
	return fmt.Sprintf("row is %d bytes, over the %d-byte cap", o.Size, o.Cap)
}

// Meta is the identity a row carries before any large payload.
type Meta struct {
	ID, Name, Engine string
}

// Meta decodes the identity fields from o's prefix. Fields are decoded in
// order and decoding stops at the first incomplete value, so the fields the
// driver writes first are recovered even when the prefix ends inside a later
// string.
func (o Oversized) Meta() Meta {
	var m Meta
	dec := json.NewDecoder(bytes.NewReader(o.Prefix))
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		return m
	}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			break
		}
		key, ok := kt.(string)
		if !ok {
			break
		}
		switch key {
		case "id", "name", "engine":
			var s string
			if err := dec.Decode(&s); err != nil {
				return m
			}
			switch key {
			case "id":
				m.ID = s
			case "name":
				m.Name = s
			case "engine":
				m.Engine = s
			}
		default:
			var skip json.RawMessage
			if err := dec.Decode(&skip); err != nil {
				return m
			}
		}
	}
	return m
}

// FollowerOversized is the harness result an xengine follower row reader
// records for an oversized row. It errors when the prefix names another engine
// (the rows file was passed to the wrong diff) or carries no id (there is no
// item to classify the row against).
func FollowerOversized(o Oversized, engine string) (oraclediff.XResult, error) {
	m := o.Meta()
	if m.Engine != "" && m.Engine != engine {
		return oraclediff.XResult{}, fmt.Errorf("oversized row %q is a %s row, not %s", m.ID, m.Engine, engine)
	}
	if m.ID == "" {
		return oraclediff.XResult{}, fmt.Errorf("oversized row (%s) has no id in its first %d bytes", o.Reason(), len(o.Prefix))
	}
	return oraclediff.XResult{ID: m.ID, Name: m.Name, Harness: o.Reason()}, nil
}

// Read calls each with every non-empty row of path whose length is at most
// cap. A longer row is skipped and passed to over; Read then goes on to the
// next row. With over nil an oversized row is an error, so callers with no
// classified-row alternative keep the old fail-loud behaviour.
func Read(path string, cap int, each func([]byte) error, over func(Oversized) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if cap <= 0 {
		cap = DefaultCap
	}
	br := bufio.NewReaderSize(f, 64<<10)
	var (
		line      []byte // the current row, at most cap bytes while not oversized
		prefix    []byte // the first bytes of an oversized row
		size      int    // bytes of the current row seen so far, without the newline
		oversized bool
	)
	finish := func() error {
		defer func() {
			line = line[:0]
			prefix = nil
			size = 0
			oversized = false
		}()
		if oversized {
			if over == nil {
				return fmt.Errorf("%s: row is %d bytes, over the %d-byte cap", path, size, cap)
			}
			return over(Oversized{Prefix: append([]byte(nil), prefix...), Size: size, Cap: cap})
		}
		if len(line) == 0 {
			return nil
		}
		return each(append([]byte(nil), line...))
	}
	for {
		frag, err := br.ReadSlice('\n')
		content := frag
		final := false
		switch err {
		case nil:
			content = frag[:len(frag)-1]
		case bufio.ErrBufferFull:
			// A fragment of a row still being read.
		case io.EOF:
			final = true
		default:
			return err
		}
		if !oversized {
			room := cap - size
			if len(content) > room {
				if room < 0 {
					room = 0
				}
				line = append(line, content[:room]...)
				keep := min(len(line), maxPrefix)
				prefix = append([]byte(nil), line[:keep]...)
				// The rest of this row is discarded: it is already over cap,
				// and the callback only needs the prefix.
				line = nil
				oversized = true
			} else {
				line = append(line, content...)
			}
		}
		size += len(content)
		if final {
			if size == 0 && !oversized && len(line) == 0 {
				return nil
			}
			return finish()
		}
		if err == nil {
			if err := finish(); err != nil {
				return err
			}
		}
	}
}
