package calendar

import (
	"errors"
	"net/url"
	"os/exec"
)

// OpenLink opens the link of an event (its meeting, its page) in the
// browser, with xdg-open, without waiting for it. Anyone who can send an
// invitation writes those links, so only https ones are opened: a file://
// link, or the scheme of some installed handler, is refused.
func OpenLink(target string) error {
	if !IsWebLink(target) {
		return errors.New("not an https link")
	}
	cmd := exec.Command("xdg-open", target)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// IsWebLink reports whether target is an https link to a host.
func IsWebLink(target string) bool {
	u, err := url.Parse(target)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil
}
