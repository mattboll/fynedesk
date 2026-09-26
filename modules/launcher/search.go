package launcher

import (
	"net/url"
	"strings"

	"fyne.io/fyne/v2"
	wmTheme "fyshos.com/tyde/theme"

	"fyshos.com/tyde"
	"fyshos.com/tyde/locale"
)

// promptPreviewLen is the number of runes of launcher input shown in a
// suggestion title before it is elided.
const promptPreviewLen = 28

var searchMeta = tyde.ModuleMetadata{
	Name:        "Launcher: Web Search",
	NewInstance: newSearchSuggest,
}

type search struct{}

func (s *search) Destroy() {
}

func (s *search) LaunchSuggestions(input string) []tyde.LaunchSuggestion {
	if input == "" {
		return nil
	}
	return []tyde.LaunchSuggestion{&searchItem{text: input}}
}

func (s *search) Metadata() tyde.ModuleMetadata {
	return searchMeta
}

// newCalcSuggest creates a new module that will show an option to search the web for an string.
func newSearchSuggest() tyde.Module {
	return &search{}
}

type searchItem struct {
	text string
}

func (s *searchItem) Icon() fyne.Resource {
	return wmTheme.InternetIcon
}

func (s *searchItem) Title() string {
	return locale.Tf("launcher.searchWeb", TruncatePrompt(s.text))
}

func (s *searchItem) Launch() {
	enc := url.QueryEscape(s.text)
	u, err := url.Parse("https://duck.com/?q=" + enc)
	if err != nil {
		fyne.LogError("Failed to set up web search", err)
		return
	}

	_ = fyne.CurrentApp().OpenURL(u)
}

// TruncatePrompt shortens launcher input for display in a suggestion title.
func TruncatePrompt(s string) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= promptPreviewLen {
		return s
	}
	return strings.TrimRight(string(r[:promptPreviewLen]), " ") + "…"
}
