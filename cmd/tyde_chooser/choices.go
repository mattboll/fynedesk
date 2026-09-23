package main

import (
	"bufio"
	"io"
	"strings"
)

// choiceKind tells a screen from a window.
type choiceKind int

const (
	choiceScreen choiceKind = iota
	choiceWindow
)

// choice is one line offered by xdg-desktop-portal-wlr to a dmenu chooser:
// "Monitor: <output> <description>" or "Window: <title> (<app id>)". The
// chooser answers with the line itself.
type choice struct {
	line  string
	kind  choiceKind
	title string // output name, or window title
	sub   string // output description, or app id
}

// readChoices parses the chooser input, ignoring blank and unknown lines.
func readChoices(r io.Reader) []choice {
	var choices []choice
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		if c, ok := parseChoice(scanner.Text()); ok {
			choices = append(choices, c)
		}
	}
	return choices
}

func parseChoice(line string) (choice, bool) {
	if rest, ok := strings.CutPrefix(line, "Monitor: "); ok {
		name, desc, _ := strings.Cut(rest, " ")
		return choice{line: line, kind: choiceScreen, title: name, sub: strings.TrimSpace(desc)}, name != ""
	}
	if rest, ok := strings.CutPrefix(line, "Window: "); ok {
		title, appID := rest, ""
		if i := strings.LastIndex(rest, " ("); i >= 0 && strings.HasSuffix(rest, ")") {
			title, appID = rest[:i], rest[i+2:len(rest)-1]
		}
		return choice{line: line, kind: choiceWindow, title: title, sub: appID}, true
	}
	return choice{}, false
}
