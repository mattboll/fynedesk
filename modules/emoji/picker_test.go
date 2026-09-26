package emoji

import (
	"os"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"fyshos.com/tyde"
	emojidata "fyshos.com/tyde/internal/emoji"
	wmTest "fyshos.com/tyde/test"

	"github.com/stretchr/testify/assert"
)

func TestMain(m *testing.M) {
	test.NewApp()

	os.Exit(m.Run())
}

// newTestDesktop is the in-memory desktop the overlay calls are made against.
func newTestDesktop() *wmTest.Desktop {
	return wmTest.NewDesktop()
}

// newTestPicker builds a picker with clean preferences.
func newTestPicker(t *testing.T) *picker {
	t.Helper()

	fyne.CurrentApp().Preferences().SetString(prefRecent, "")
	t.Cleanup(func() { fyne.CurrentApp().Preferences().SetString(prefRecent, "") })

	p := &picker{}
	p.build()
	return p
}

func TestPicker_ShowAndHide(t *testing.T) {
	tyde.SetInstance(newTestDesktop())
	t.Cleanup(func() { tyde.SetInstance(nil) })

	p := newTestPicker(t)

	p.show()
	assert.True(t, p.shown)

	p.hide()
	assert.False(t, p.shown)
}

func TestPicker_Toggle(t *testing.T) {
	tyde.SetInstance(newTestDesktop())
	t.Cleanup(func() { tyde.SetInstance(nil) })

	p := newTestPicker(t)
	p.toggle()
	assert.True(t, p.shown)
	p.toggle()
	assert.False(t, p.shown)
}

func TestPicker_PickCopiesToClipboard(t *testing.T) {
	p := newTestPicker(t)

	p.pick(findEmoji(t, "😀"))

	assert.Equal(t, "😀", fyne.CurrentApp().Clipboard().Content())
	if assert.Len(t, p.recent(), 1) {
		assert.Equal(t, "😀", p.recent()[0].Character, "a pick is remembered")
	}
}

func TestPicker_PickClosesThePicker(t *testing.T) {
	tyde.SetInstance(newTestDesktop())
	t.Cleanup(func() { tyde.SetInstance(nil) })

	p := newTestPicker(t)
	p.show()

	p.pick(findEmoji(t, "🚀"))

	assert.False(t, p.shown, "the picker closes so the emoji can be pasted")
	assert.Equal(t, "🚀", fyne.CurrentApp().Clipboard().Content())
}

func TestPicker_RecentsMostRecentFirst(t *testing.T) {
	p := newTestPicker(t)

	p.remember(findEmoji(t, "😀"))
	p.remember(findEmoji(t, "🚀"))

	recent := p.recent()
	if assert.Len(t, recent, 2) {
		assert.Equal(t, "🚀", recent[0].Character)
		assert.Equal(t, "😀", recent[1].Character)
	}
}

func TestPicker_RecentsDeduplicate(t *testing.T) {
	p := newTestPicker(t)

	p.remember(findEmoji(t, "😀"))
	p.remember(findEmoji(t, "🚀"))
	p.remember(findEmoji(t, "😀"))

	recent := p.recent()
	if assert.Len(t, recent, 2, "re-picking should reorder, not duplicate") {
		assert.Equal(t, "😀", recent[0].Character)
		assert.Equal(t, "🚀", recent[1].Character)
	}
}

func TestPicker_RecentsCapped(t *testing.T) {
	p := newTestPicker(t)

	for _, e := range emojidata.All()[:maxRecent+10] {
		p.remember(e)
	}
	assert.Len(t, p.recent(), maxRecent)
}

func TestPicker_RecentsIgnoreUnknownCharacters(t *testing.T) {
	p := newTestPicker(t)
	fyne.CurrentApp().Preferences().SetString(prefRecent, strings.Join([]string{"😀", "not-an-emoji"}, recentSep))

	recent := p.recent()
	if assert.Len(t, recent, 1) {
		assert.Equal(t, "😀", recent[0].Character)
	}
}

func TestPicker_RecentPageAppearsAfterFirstPick(t *testing.T) {
	p := newTestPicker(t)
	assert.NotContains(t, p.pageNames(), recentPage, "nothing picked yet")
	assert.Equal(t, firstGroup(), p.defaultPage())

	p.remember(findEmoji(t, "🚀"))

	assert.Contains(t, p.pageNames(), recentPage)
	assert.Equal(t, recentPage, p.defaultPage())
	assert.Len(t, p.pages.Objects, len(p.pageNames()), "the button row gains its Recent page")
}

func TestPicker_ShowPage(t *testing.T) {
	p := newTestPicker(t)

	p.showPage("Flags")
	assert.Equal(t, "Flags", p.page)
	assert.NotEmpty(t, p.items)
	for _, e := range p.items {
		assert.Equal(t, "Flags", e.Group)
	}
}

func TestPicker_ShowPageFallsBackWhenNoRecents(t *testing.T) {
	p := newTestPicker(t)

	p.showPage(recentPage)
	assert.Equal(t, firstGroup(), p.page, "an empty Recent page falls through to the first group")
}

func TestPicker_SearchFiltersAndRestores(t *testing.T) {
	p := newTestPicker(t)
	p.showPage("Flags")
	flagCount := len(p.items)

	p.search("rocket")
	if assert.NotEmpty(t, p.items) {
		assert.Equal(t, "🚀", p.items[0].Character)
	}

	p.search("  ")
	assert.Len(t, p.items, flagCount, "clearing the box returns to the selected page")
}

func TestPicker_SubmitPicksTopMatch(t *testing.T) {
	p := newTestPicker(t)

	p.entry.SetText("rocket")
	p.entry.OnSubmitted("rocket")

	assert.Equal(t, "🚀", fyne.CurrentApp().Clipboard().Content())
}

func TestPicker_SubmitWithNoMatchDoesNothing(t *testing.T) {
	p := newTestPicker(t)
	fyne.CurrentApp().Clipboard().SetContent("untouched")

	p.entry.SetText("zzzzznotanemoji")
	p.entry.OnSubmitted("zzzzznotanemoji")

	assert.Equal(t, "untouched", fyne.CurrentApp().Clipboard().Content())
	assert.Empty(t, p.recent())
}

func TestPicker_EscapeHides(t *testing.T) {
	tyde.SetInstance(newTestDesktop())
	t.Cleanup(func() { tyde.SetInstance(nil) })

	p := newTestPicker(t)
	p.show()

	p.entry.TypedKey(&fyne.KeyEvent{Name: fyne.KeyEscape})
	assert.False(t, p.shown)
}

func TestPicker_HoverNamesEmoji(t *testing.T) {
	p := newTestPicker(t)
	e := findEmoji(t, "🚀")

	p.hover(e, true)
	assert.Equal(t, e.Name, p.status.Text)

	p.hover(e, false)
	assert.Equal(t, "", p.status.Text)
}

// findEmoji looks up one emoji by character, failing the test if the table ever
// drops it.
func findEmoji(t *testing.T, char string) emojidata.Emoji {
	t.Helper()

	for _, e := range emojidata.All() {
		if e.Character == char {
			return e
		}
	}
	t.Fatalf("emoji %q missing from the table", char)
	return emojidata.Emoji{}
}
