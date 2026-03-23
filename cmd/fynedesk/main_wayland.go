//go:build wayland && (linux || openbsd || freebsd || netbsd)

package main

import "fyshos.com/fynedesk/internal/wayland/compositor"

func main() {
	compositor.Run()
}
