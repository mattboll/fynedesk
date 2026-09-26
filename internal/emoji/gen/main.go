// Command gen builds the emoji picker's data table from the Unicode
// Consortium's emoji-test.txt.
//
// That file is the only source for the order emoji are meant to be presented in:
// it is hand-curated per subgroup ("face-smiling" before "face-affection",
// monkeys before plants) and cannot be derived from code points, which is why
// the table is generated rather than sorted at runtime.
//
// Run with no arguments to fetch the latest published data:
//
//	go generate ./modules/emoji/...
//
// or pass a path to use a local copy instead:
//
//	go run ./modules/emoji/gen emoji-test.txt
package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
)

const sourceURL = "https://unicode.org/Public/emoji/latest/emoji-test.txt"

const outputFile = "table.go"

// maxEmojiVersion is the newest emoji release the picker will offer. Fyne draws
// emoji with its bundled EmojiOne Color font, whose coverage stops at Emoji 3.0
// - and the boundary is absolute. Shaping every fully-qualified emoji against
// that font gives 1371 of 1371 rendering at or below E3.0, and 1 of 543 above
// it. The rest come out as .notdef boxes, or fall apart into their parts: the
// gendered sequences (👮 + ZWJ + ♀ + VS16) have no ligature to collapse into,
// so they show as a person, a broken joiner and a gender sign.
//
// Raise this when Fyne bundles a newer emoji font, and re-run the generator.
const maxEmojiVersion = 3.0

// skinTones are the modifiers that turn one emoji into five. The picker offers
// only the neutral form, so any sequence carrying one is skipped.
var skinTones = map[string]bool{
	"1F3FB": true, "1F3FC": true, "1F3FD": true, "1F3FE": true, "1F3FF": true,
}

type entry struct {
	character string
	name      string
	group     string
}

func main() {
	source, version, err := read()
	if err != nil {
		log.Fatal(err)
	}

	entries, err := parse(source)
	if err != nil {
		log.Fatal(err)
	}
	if len(entries) == 0 {
		log.Fatal("no emoji parsed - has the file format changed?")
	}

	if err := write(entries, version); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("wrote %s: %d emoji from Unicode %s, capped at E%.1f\n",
		outputFile, len(entries), version, maxEmojiVersion)
}

// read returns the emoji-test.txt contents, from a named file or the published
// URL, along with the emoji version it declares.
func read() (string, string, error) {
	var data []byte
	var err error

	if len(os.Args) > 1 {
		data, err = os.ReadFile(os.Args[1])
		if err != nil {
			return "", "", err
		}
	} else {
		resp, err := http.Get(sourceURL)
		if err != nil {
			return "", "", err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return "", "", fmt.Errorf("fetching %s: %s", sourceURL, resp.Status)
		}
		data, err = io.ReadAll(resp.Body)
		if err != nil {
			return "", "", err
		}
	}

	text := string(data)
	return text, version(text), nil
}

// version pulls the "# Version: 17.0" header out of the file.
func version(text string) string {
	for line := range strings.Lines(text) {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "# Version:"); ok {
			return strings.TrimSpace(rest)
		}
	}
	return "unknown"
}

// parse walks the file in order, keeping the fully-qualified emoji of the
// standard groups. Order is preserved exactly: it is the whole point of using
// this file.
//
// Lines look like:
//
//	# group: Smileys & Emotion
//	1F600 ; fully-qualified     # 😀 E1.0 grinning face
func parse(source string) ([]entry, error) {
	var entries []entry
	group := ""

	scanner := bufio.NewScanner(strings.NewReader(source))
	for scanner.Scan() {
		line := scanner.Text()

		if name, ok := strings.CutPrefix(line, "# group:"); ok {
			group = strings.TrimSpace(name)
			continue
		}
		if strings.HasPrefix(line, "#") || strings.TrimSpace(line) == "" {
			continue
		}
		// "Component" holds the bare skin tone and hair modifiers, which are not
		// emoji anyone would pick on their own.
		if group == "" || group == "Component" {
			continue
		}

		codes, rest, ok := strings.Cut(line, ";")
		if !ok {
			continue
		}
		status, comment, ok := strings.Cut(rest, "#")
		if !ok || strings.TrimSpace(status) != "fully-qualified" {
			continue
		}
		if hasSkinTone(codes) {
			continue
		}

		character, name, emojiVersion := splitComment(comment)
		if character == "" || name == "" || emojiVersion > maxEmojiVersion {
			continue
		}
		entries = append(entries, entry{character: character, name: name, group: group})
	}
	return entries, scanner.Err()
}

// hasSkinTone reports whether a space-separated code point list includes a skin
// tone modifier.
func hasSkinTone(codes string) bool {
	for _, code := range strings.Fields(codes) {
		if skinTones[strings.ToUpper(code)] {
			return true
		}
	}
	return false
}

// splitComment reads the character, name and emoji version out of the trailing
// comment, which is "😀 E1.0 grinning face" - the emoji, its version, then the
// name. An unmarked line reports version 0, so it is never filtered out for
// being too new.
func splitComment(comment string) (string, string, float64) {
	fields := strings.Fields(comment)
	if len(fields) < 3 {
		return "", "", 0
	}

	character := fields[0]
	rest := fields[1:]
	emojiVersion := 0.0
	// The "E1.0" marker is metadata rather than name, so lift it out.
	if v, ok := strings.CutPrefix(rest[0], "E"); ok {
		if parsed, err := strconv.ParseFloat(v, 64); err == nil {
			emojiVersion = parsed
			rest = rest[1:]
		}
	}
	return character, strings.Join(rest, " "), emojiVersion
}

func write(entries []entry, version string) error {
	f, err := os.Create(outputFile)
	if err != nil {
		return err
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	fmt.Fprintf(w, "// Code generated by gen/main.go; DO NOT EDIT.\n")
	fmt.Fprintf(w, "// Source: %s\n\n", sourceURL)
	fmt.Fprintf(w, "package emoji\n\n")
	fmt.Fprintf(w, "// unicodeVersion is the emoji data version this table was built from.\n")
	fmt.Fprintf(w, "const unicodeVersion = %q\n\n", version)
	fmt.Fprintf(w, "// emojiTable is every pickable emoji in the order the Unicode Consortium\n")
	fmt.Fprintf(w, "// presents them, grouped and curated. Do not sort it.\n")
	fmt.Fprintf(w, "//\n")
	fmt.Fprintf(w, "// Emoji newer than E%.1f are left out: the bundled Fyne emoji font cannot draw\n", maxEmojiVersion)
	fmt.Fprintf(w, "// them. See maxEmojiVersion in gen/main.go.\n")
	fmt.Fprintf(w, "var emojiTable = []Emoji{\n")
	for _, e := range entries {
		fmt.Fprintf(w, "\t{%q, %q, %q},\n", e.character, e.name, e.group)
	}
	fmt.Fprintf(w, "}\n")

	return w.Flush()
}
