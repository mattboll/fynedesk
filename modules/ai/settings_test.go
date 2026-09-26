package ai

import (
	"errors"
	"net/url"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

// TestLocalSettingsManaged checks the managed-server option drives the shape of
// the Local AI panel: when tyde runs the server there is no address to set, and
// when it doesn't the user gets a Base URL. The option is only ever read here -
// toggling it for real would start a kronk server.
func TestLocalSettingsManaged(t *testing.T) {
	if !kronkAvailable() {
		t.Skip("kronk is not installed, so the managed panel can't be built")
	}
	a := test.NewApp()
	p := a.Preferences()
	p.SetString(prefProvider, ProviderLocal)

	p.SetBool(prefLocalManaged, true)
	panel := localSettings(p, func() {})
	if !hasCheck(panel, managedLabel()) {
		t.Errorf("managed panel is missing the %q option", managedLabel())
	}
	if hasEntryPlaceholder(panel, ollamaEndpoint) {
		t.Error("managed panel offers a Base URL, but tyde owns the address")
	}

	p.SetBool(prefLocalManaged, false)
	panel = localSettings(p, func() {})
	if !hasCheck(panel, managedLabel()) {
		t.Errorf("user-run panel is missing the %q option", managedLabel())
	}
	if !hasEntryPlaceholder(panel, ollamaEndpoint) {
		t.Error("user-run panel has no Base URL to point at their server")
	}
}

// hasCheck reports whether the tree holds a check with the given label.
func hasCheck(o fyne.CanvasObject, label string) bool {
	return findIn(o, func(c fyne.CanvasObject) bool {
		chk, ok := c.(*widget.Check)
		return ok && chk.Text == label
	})
}

// hasEntryPlaceholder reports whether the tree holds an entry prompting with the given text.
func hasEntryPlaceholder(o fyne.CanvasObject, placeholder string) bool {
	return findIn(o, func(c fyne.CanvasObject) bool {
		e, ok := c.(*widget.Entry)
		return ok && e.PlaceHolder == placeholder
	})
}

// findIn walks a panel looking for an object matching pred, descending through
// containers and into form items (where the entries live).
func findIn(o fyne.CanvasObject, pred func(fyne.CanvasObject) bool) bool {
	if pred(o) {
		return true
	}
	switch c := o.(type) {
	case *fyne.Container:
		for _, k := range c.Objects {
			if findIn(k, pred) {
				return true
			}
		}
	case *widget.Form:
		for _, it := range c.Items {
			if findIn(it.Widget, pred) {
				return true
			}
		}
	}
	return false
}

// TestDescribeProbe pins the contract the Test-connection button relies on: a
// working setup says nothing (the tick carries the message), everything else
// returns just the reason.
func TestDescribeProbe(t *testing.T) {
	refused := errors.New("connect: connection refused")

	for _, tt := range []struct {
		name string
		res  probeResult
		want string
	}{
		{"ready", probeResult{models: []string{"llama3.2:latest"}}, ""},
		{
			"unreachable",
			probeResult{err: &url.Error{Op: "Get", URL: "http://localhost:11434/v1/models", Err: refused}},
			refused.Error(),
		},
		{"no models", probeResult{}, "No models loaded yet - pull 'llama3.2' on the server, or wait for its first-use download."},
		{"other models", probeResult{models: []string{"gemma3:4b"}}, "'llama3.2' isn't available. The server offers: gemma3:4b"},
	} {
		if got := describeProbe(tt.res, "llama3.2"); got != tt.want {
			t.Errorf("%s: describeProbe() = %q, want %q", tt.name, got, tt.want)
		}
	}
}
