// Package ocr reads the text of a picture with tesseract.
package ocr

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"slices"
	"strings"
)

// InstallCommand installs tesseract and the languages Tyde reads.
const InstallCommand = "sudo apt install tesseract-ocr tesseract-ocr-fra tesseract-ocr-eng"

// ErrMissing tells that tesseract, or any language for it, is not installed.
var ErrMissing = errors.New("tesseract is not installed")

// preferred are the languages read when installed, in this order.
var preferred = []string{"fra", "eng"}

// Text returns the text of the picture at path, read in French and English
// (or whatever languages are installed).
func Text(ctx context.Context, path string) (string, error) {
	bin, err := exec.LookPath("tesseract")
	if err != nil {
		return "", ErrMissing
	}
	list, err := exec.CommandContext(ctx, bin, "--list-langs").Output()
	if err != nil {
		return "", fmt.Errorf("tesseract --list-langs: %w", err)
	}
	langs := pickLanguages(parseLanguages(string(list)))
	if len(langs) == 0 {
		return "", ErrMissing
	}
	out, err := exec.CommandContext(ctx, bin, path, "stdout", "-l", strings.Join(langs, "+")).Output()
	if err != nil {
		return "", fmt.Errorf("tesseract: %w", err)
	}
	return cleanup(string(out)), nil
}

// parseLanguages reads the output of tesseract --list-langs: a heading,
// then one language per line.
func parseLanguages(list string) []string {
	var langs []string
	for i, line := range strings.Split(list, "\n") {
		line = strings.TrimSpace(line)
		if i == 0 || line == "" || line == "osd" { // osd is page layout, not a language
			continue
		}
		langs = append(langs, line)
	}
	return langs
}

// pickLanguages keeps the preferred languages that are installed, or else
// all of them.
func pickLanguages(installed []string) []string {
	var langs []string
	for _, l := range preferred {
		if slices.Contains(installed, l) {
			langs = append(langs, l)
		}
	}
	if len(langs) == 0 {
		return installed
	}
	return langs
}

var blankLines = regexp.MustCompile(`\n{3,}`)

// cleanup tidies what tesseract read: no form feed, no trailing spaces,
// at most one blank line in a row, nothing around.
func cleanup(text string) string {
	text = strings.ReplaceAll(text, "\f", "")
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	return strings.TrimSpace(blankLines.ReplaceAllString(strings.Join(lines, "\n"), "\n\n"))
}
