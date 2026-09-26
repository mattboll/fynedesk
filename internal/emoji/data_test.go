package emoji

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// wantGroups is the Unicode group order, which the generated table must
// preserve.
var wantGroups = []string{
	"Smileys & Emotion",
	"People & Body",
	"Animals & Nature",
	"Food & Drink",
	"Travel & Places",
	"Activities",
	"Objects",
	"Symbols",
	"Flags",
}

func TestGroups_OrderAndContent(t *testing.T) {
	gs := Groups()
	assert.Equal(t, len(wantGroups), len(gs), "every group should be present exactly once")

	for i, g := range gs {
		assert.Equal(t, wantGroups[i], g.Name)
		assert.NotEmpty(t, g.Items)
	}
}

func TestGroups_LeadWithTheObviousMembers(t *testing.T) {
	// The first thing anyone sees when they open a page. Any attempt to sort the
	// table by code point breaks these.
	want := map[string]string{
		"Smileys & Emotion": "😀",
		"People & Body":     "👋",
		"Animals & Nature":  "🐵",
		"Food & Drink":      "🍇",
		"Activities":        "🎃",
	}
	for _, g := range Groups() {
		if char, ok := want[g.Name]; ok {
			assert.Equal(t, char, g.Items[0].Character, "%s opens on the wrong emoji", g.Name)
		}
	}
}

func TestTable_NoSkinToneVariants(t *testing.T) {
	for _, e := range All() {
		for _, r := range e.Character {
			assert.NotContains(t, []rune{'🏻', '🏼', '🏽', '🏾', '🏿'}, r,
				"skin tone variant leaked in: %q", e.Character)
		}
	}
}

func TestTable_NamesAreClean(t *testing.T) {
	for _, e := range All() {
		assert.NotEmpty(t, e.Name, "%q has no name", e.Character)
		assert.False(t, strings.HasPrefix(e.Name, "E") && strings.Contains(e.Name, "."),
			"version marker left in name: %q", e.Name)
		assert.Equal(t, strings.TrimSpace(e.Name), e.Name)
	}
}

func TestTable_NoDuplicateCharacters(t *testing.T) {
	seen := map[string]bool{}
	for _, e := range All() {
		assert.False(t, seen[e.Character], "duplicate emoji %q", e.Character)
		seen[e.Character] = true
	}
}

func TestTable_GroupsAreContiguous(t *testing.T) {
	// Groups() relies on this: a group appearing twice would make two pages.
	seen := map[string]bool{}
	last := ""
	for _, e := range All() {
		if e.Group != last {
			assert.False(t, seen[e.Group], "group %q is split across the table", e.Group)
			seen[e.Group] = true
			last = e.Group
		}
	}
}

func TestTable_KnownEmojiPresent(t *testing.T) {
	// A spread of eras up to the E3.0 cap, including the family sequence that
	// proves joined emoji survive the filter when the font can ligate them.
	want := []string{"😀", "🚀", "🎂", "🤣", "👨‍👩‍👦"}

	present := map[string]bool{}
	for _, e := range All() {
		present[e.Character] = true
	}
	for _, char := range want {
		assert.True(t, present[char], "expected %s in the table", char)
	}
}

func TestTable_UnrenderableEmojiAbsent(t *testing.T) {
	// The table is capped at E3.0, the extent of Fyne's bundled emoji font.
	// Anything newer draws as a box, and the gendered sequences fall apart into
	// their parts, so the generator leaves them out.
	want := []string{
		"👮‍♀️", // E4.0 gendered sequence: no ligature, shows as three glyphs
		"🥰",    // E11.0
		"🫠",    // E14.0
		"🫶",    // E14.0
	}

	present := map[string]bool{}
	for _, e := range All() {
		present[e.Character] = true
	}
	for _, char := range want {
		assert.False(t, present[char], "expected %s to be filtered out", char)
	}
}

func TestSearch(t *testing.T) {
	results := Search("rocket")
	if assert.NotEmpty(t, results) {
		assert.Equal(t, "🚀", results[0].Character, "the exact name should rank first")
	}

	assert.Empty(t, Search(""))
	assert.Empty(t, Search("   "))
	assert.Empty(t, Search("zzzzznotanemoji"))
}

func TestSearch_AllTermsMustMatch(t *testing.T) {
	results := Search("grinning squinting")
	if assert.NotEmpty(t, results) {
		for _, e := range results {
			name := strings.ToLower(e.Name)
			assert.Contains(t, name, "grinning")
			assert.Contains(t, name, "squinting")
		}
	}
}

func TestSearch_ExactNameBeatsLongerMatch(t *testing.T) {
	// "cat" is the exact name of 🐈; 🐱 is "cat face" and the rest merely contain
	// the word. The exact match has to win or the obvious answer is buried.
	results := Search("cat")
	if assert.NotEmpty(t, results) {
		assert.Equal(t, "🐈", results[0].Character)
	}
}

func TestSearch_PrefixBeatsMidNameMatch(t *testing.T) {
	results := Search("cat f")
	if assert.NotEmpty(t, results) {
		assert.Equal(t, "🐱", results[0].Character, "a name starting with the query ranks above one merely containing it")
	}
}

func TestSearch_KeepsTableOrder(t *testing.T) {
	// Equal-ranked matches should come back in the order the picker shows them.
	results := Search("face")
	assert.Greater(t, len(results), 10)

	index := map[string]int{}
	for i, e := range All() {
		index[e.Character] = i
	}
	for i := 1; i < len(results); i++ {
		if matchRank(strings.ToLower(results[i-1].Name), []string{"face"}) !=
			matchRank(strings.ToLower(results[i].Name), []string{"face"}) {
			continue // ranks differ, order is by rank
		}
		assert.Less(t, index[results[i-1].Character], index[results[i].Character])
	}
}

func TestUnicodeVersion(t *testing.T) {
	assert.NotEmpty(t, unicodeVersion)
	assert.NotEqual(t, "unknown", unicodeVersion, "the generator could not read the data version")
}

func TestPages(t *testing.T) {
	pages := Pages()
	assert.Equal(t, RecentPage, pages[0].Name)
	assert.Equal(t, RecentIcon, pages[0].Icon)
	assert.Len(t, pages, len(wantGroups)+1)
	for _, p := range pages[1:] {
		assert.NotEmpty(t, GroupItems(p.Name), p.Name)
		assert.NotEqual(t, p.Name, p.Icon, "%s has no icon", p.Name)
	}
	assert.Nil(t, GroupItems("no such group"))
}

func TestFromCharacters(t *testing.T) {
	got := FromCharacters([]string{"🚀", "not an emoji", "", "😀"})
	if assert.Len(t, got, 2) {
		assert.Equal(t, "rocket", got[0].Name)
		assert.Equal(t, "😀", got[1].Character)
	}
}
