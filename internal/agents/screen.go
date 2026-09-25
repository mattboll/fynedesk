package agents

import (
	"regexp"
	"strings"
	"unicode"
)

// Choice is an answer an agent offers, typed with its key.
type Choice struct {
	Key   string // "1"
	Label string // "Yes, and don't ask again"
}

// choiceLine matches an option of a question: "❯ 1. Yes", "  2) No".
var choiceLine = regexp.MustCompile(`^\s*(?:[❯›>]\s*)?(\d)[.)]\s+(\S.*?)\s*$`)

// Choices finds the question an agent asks at the bottom of its screen and
// returns its options, in order; nil when it asks nothing that way.
func Choices(screen string) []Choice {
	lines := strings.Split(screen, "\n")
	var block []Choice
	for i := len(lines) - 1; i >= 0; i-- {
		m := choiceLine.FindStringSubmatch(lines[i])
		if m == nil {
			if len(block) > 0 && strings.TrimSpace(lines[i]) != "" && !isContinuation(lines[i]) {
				break // above the options: the question itself
			}
			continue
		}
		block = append([]Choice{{Key: m[1], Label: m[2]}}, block...)
	}
	// The options of one question count up from 1, one by one.
	for i, c := range block {
		if c.Key != string(rune('1'+i)) {
			return nil
		}
	}
	return block
}

// isContinuation reports whether a line only continues an option (an
// indented description under it).
func isContinuation(line string) bool {
	return strings.HasPrefix(line, "     ")
}

// Preview returns the last lines of an agent's screen worth reading: the
// frames, rules and status lines of the terminal interface are left out.
func Preview(screen string, max int) []string {
	var out []string
	for _, line := range strings.Split(screen, "\n") {
		line = strings.TrimRightFunc(line, unicode.IsSpace)
		if isChrome(line) {
			continue
		}
		out = append(out, strings.TrimSpace(line))
	}
	// No blank lines at the ends, no runs of them inside.
	var kept []string
	for _, l := range out {
		if l == "" && (len(kept) == 0 || kept[len(kept)-1] == "") {
			continue
		}
		kept = append(kept, l)
	}
	for len(kept) > 0 && kept[len(kept)-1] == "" {
		kept = kept[:len(kept)-1]
	}
	if len(kept) > max {
		kept = kept[len(kept)-max:]
	}
	return kept
}

// isChrome recognises the lines drawn by a terminal interface rather than
// said by the agent: rules, box borders, the prompt, mode and hint lines.
func isChrome(line string) bool {
	t := strings.TrimSpace(line)
	if t == "" {
		return false
	}
	drawing := 0
	for _, r := range t {
		if (r >= 0x2500 && r <= 0x257F) || r == ' ' {
			drawing++
		}
	}
	if drawing*10 >= len([]rune(t))*8 {
		return true // a rule or a box border
	}
	for _, prefix := range []string{"❯", "⏵", "? for shortcuts", "✔ Update", "esc to"} {
		if strings.HasPrefix(t, prefix) {
			return true
		}
	}
	return false
}
