package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestReadChoices(t *testing.T) {
	in := "Monitor: DP-1 Dell Inc. U3419W\n" +
		"Monitor: eDP-1\n" +
		"Window: Meet – Code-Troopers (firefox)\n" +
		"Window: notes (draft) (kitty)\n" +
		"Window: untitled\n" +
		"\n" +
		"Region: 0,0 10x10\n"

	choices := readChoices(strings.NewReader(in))
	assert.Equal(t, []choice{
		{line: "Monitor: DP-1 Dell Inc. U3419W", kind: choiceScreen, title: "DP-1", sub: "Dell Inc. U3419W"},
		{line: "Monitor: eDP-1", kind: choiceScreen, title: "eDP-1"},
		{line: "Window: Meet – Code-Troopers (firefox)", kind: choiceWindow, title: "Meet – Code-Troopers", sub: "firefox"},
		{line: "Window: notes (draft) (kitty)", kind: choiceWindow, title: "notes (draft)", sub: "kitty"},
		{line: "Window: untitled", kind: choiceWindow, title: "untitled"},
	}, choices)
}
