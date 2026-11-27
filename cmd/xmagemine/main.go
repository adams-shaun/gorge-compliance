// Command xmagemine sizes XMage's card source as a possible third typed
// reading, without a JVM or an XMage build.
//
//	go run ./cmd/xmagemine -src <XMage checkout>
//	go run ./cmd/xmagemine -src <XMage checkout> -top 50
//	go run ./cmd/xmagemine -src <XMage checkout> -cards names.txt
//
// A card is one *.java file under Mage.Sets/src/mage/cards/<dir>/. It is
// "shared only" when the file declares no class or enum other than the card
// class itself; those cards can be read from the shared mage.* classes they
// import. The class rows count cards importing each shared class.
//
// The output is tab-separated, sorted, and deterministic: two runs over the
// same checkout print identical bytes.
package main

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type cardFile struct {
	Class   string   // the public class, from the file name
	Private []string // other class or enum declarations in the file
	Imports []string // imports starting with "mage." that are not under "mage.cards."
}

// classDecl matches a class or enum declaration at the start of a line after
// the comment and string stripper has run. Local declarations are included:
// they are still a card-private shape.
var classDecl = regexp.MustCompile(`(?m)^[ \t]*(?:(?:static|final|abstract|private|public|protected)[ \t]+)*(class|enum)[ \t]+(\w+)`)

func main() {
	src := flag.String("src", "", "XMage checkout root (the directory containing Mage.Sets)")
	top := flag.Int("top", 200, "number of class rows to print")
	cardsPath := flag.String("cards", "", "restrict the count to card names listed one per line")
	flag.Parse()

	if *src == "" {
		fmt.Fprintln(os.Stderr, "usage: xmagemine -src DIR [-top N] [-cards FILE]")
		os.Exit(2)
	}

	root := filepath.Join(*src, "Mage.Sets", "src", "mage", "cards")
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		fmt.Fprintf(os.Stderr, "xmagemine: %s: not a directory\n", root)
		os.Exit(1)
	}

	files, err := scanTree(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "xmagemine: %v\n", err)
		os.Exit(1)
	}

	if *cardsPath != "" {
		want, err := readCardNames(*cardsPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "xmagemine: %v\n", err)
			os.Exit(1)
		}
		kept := files[:0]
		for _, f := range files {
			if want[f.Class] {
				kept = append(kept, f)
			}
		}
		files = kept
	}

	for _, line := range census(files, *top) {
		fmt.Println(line)
	}
}

// scanFile returns the card class, the private helper declarations, and the
// shared mage.* imports of one Java source file.
func scanFile(path string, src []byte) (cardFile, error) {
	class := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	clean := stripCommentsAndStrings(src)

	var imports []string
	for _, line := range strings.Split(string(clean), "\n") {
		line = strings.TrimSpace(line)
		if line != "import" && !strings.HasPrefix(line, "import ") && !strings.HasPrefix(line, "import\t") {
			continue
		}
		rest := strings.TrimSpace(line[len("import"):])
		if strings.HasPrefix(rest, "static ") || strings.HasPrefix(rest, "static\t") {
			continue
		}
		if !strings.HasSuffix(rest, ";") {
			continue
		}
		name := strings.TrimSpace(strings.TrimSuffix(rest, ";"))
		if !strings.HasPrefix(name, "mage.") || strings.HasPrefix(name, "mage.cards.") {
			continue
		}
		imports = append(imports, name)
	}

	var private []string
	for _, m := range classDecl.FindAllSubmatch(clean, -1) {
		name := string(m[2])
		if name != class {
			private = append(private, name)
		}
	}

	return cardFile{Class: class, Private: private, Imports: imports}, nil
}

// stripCommentsAndStrings replaces // and /* */ comments and string, char and
// text-block literals with spaces. Newlines are kept so line-anchored
// declarations and imports stay on their original lines.
func stripCommentsAndStrings(src []byte) []byte {
	out := make([]byte, len(src))
	copy(out, src)

	const (
		normal = iota
		lineComment
		blockComment
		str
		char
		textBlock
	)

	state := normal
	for i := 0; i < len(src); i++ {
		c := src[i]
		switch state {
		case normal:
			switch c {
			case '/':
				if i+1 < len(src) && src[i+1] == '/' {
					out[i], out[i+1] = ' ', ' '
					i++
					state = lineComment
				} else if i+1 < len(src) && src[i+1] == '*' {
					out[i], out[i+1] = ' ', ' '
					i++
					state = blockComment
				}
			case '"':
				if i+2 < len(src) && src[i+1] == '"' && src[i+2] == '"' {
					out[i], out[i+1], out[i+2] = ' ', ' ', ' '
					i += 2
					state = textBlock
				} else {
					out[i] = ' '
					state = str
				}
			case '\'':
				out[i] = ' '
				state = char
			}
		case lineComment:
			if c == '\n' {
				state = normal
			} else {
				out[i] = ' '
			}
		case blockComment:
			if c == '*' && i+1 < len(src) && src[i+1] == '/' {
				out[i], out[i+1] = ' ', ' '
				i++
				state = normal
			} else if c != '\n' {
				out[i] = ' '
			}
		case str:
			if c == '\\' && i+1 < len(src) {
				out[i] = ' '
				if src[i+1] != '\n' {
					out[i+1] = ' '
				}
				i++
			} else if c == '"' {
				out[i] = ' '
				state = normal
			} else if c != '\n' {
				out[i] = ' '
			}
		case char:
			if c == '\\' && i+1 < len(src) {
				out[i] = ' '
				if src[i+1] != '\n' {
					out[i+1] = ' '
				}
				i++
			} else if c == '\'' {
				out[i] = ' '
				state = normal
			} else if c != '\n' {
				out[i] = ' '
			}
		case textBlock:
			if c == '\\' && i+1 < len(src) {
				out[i] = ' '
				if src[i+1] != '\n' {
					out[i+1] = ' '
				}
				i++
			} else if c == '"' && i+2 < len(src) && src[i+1] == '"' && src[i+2] == '"' {
				out[i], out[i+1], out[i+2] = ' ', ' ', ' '
				i += 2
				state = normal
			} else if c != '\n' {
				out[i] = ' '
			}
		}
	}
	return out
}

// scanTree walks root for *.java files in sorted path order and scans each.
func scanTree(root string) ([]cardFile, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".java" {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)

	files := make([]cardFile, 0, len(paths))
	for _, path := range paths {
		src, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		f, err := scanFile(path, src)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, nil
}

// census returns the output lines: four total rows, then up to top class rows
// ordered by card count descending and class name ascending.
func census(files []cardFile, top int) []string {
	sharedOnly := 0
	withHelper := 0
	counts := make(map[string]int)

	for _, f := range files {
		if len(f.Private) == 0 {
			sharedOnly++
		} else {
			withHelper++
		}
		seen := make(map[string]bool, len(f.Imports))
		for _, imp := range f.Imports {
			if seen[imp] {
				continue
			}
			seen[imp] = true
			counts[imp]++
		}
	}

	lines := []string{
		fmt.Sprintf("total\tcards\t%d", len(files)),
		fmt.Sprintf("total\tcards_shared_only\t%d", sharedOnly),
		fmt.Sprintf("total\tcards_with_private_helper\t%d", withHelper),
		fmt.Sprintf("total\tshared_classes\t%d", len(counts)),
	}

	type classRow struct {
		name  string
		count int
	}
	rows := make([]classRow, 0, len(counts))
	for name, count := range counts {
		rows = append(rows, classRow{name: name, count: count})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].count != rows[j].count {
			return rows[i].count > rows[j].count
		}
		return rows[i].name < rows[j].name
	})

	if top < 0 {
		top = 0
	}
	if top > len(rows) {
		top = len(rows)
	}
	for _, r := range rows[:top] {
		lines = append(lines, fmt.Sprintf("class\t%s\t%d", r.name, r.count))
	}
	return lines
}

// readCardNames reads a one-name-per-line file into a set. Blank lines are
// ignored; surrounding whitespace (including CRLF) is trimmed.
func readCardNames(path string) (map[string]bool, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	names := make(map[string]bool)
	for _, line := range strings.Split(string(src), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			names[line] = true
		}
	}
	return names, nil
}
