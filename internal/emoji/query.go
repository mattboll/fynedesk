package emoji

import "strings"

// Category represents an emoji category with display icon.
type Category struct {
	Name string
	Icon string
}

// Categories lists emoji categories in display order.
var Categories = []Category{
	{"recent", "\U0001F553"},      // clock
	{"smileys", "\U0001F600"},     // grinning face
	{"people", "\U0001F44B"},      // waving hand
	{"animals", "\U0001F43E"},     // paw prints
	{"food", "\U0001F354"},        // hamburger
	{"travel", "\u2708\uFE0F"},    // airplane
	{"activities", "\u26BD"},      // soccer ball
	{"objects", "\U0001F4A1"},     // light bulb
	{"symbols", "\u2764\uFE0F"},   // red heart
	{"flags", "\U0001F3F3\uFE0F"}, // white flag
}

// Search returns emojis whose name contains the query (case-insensitive).
// Returns up to 80 results for grid display.
func Search(query string) []Entry {
	query = strings.ToLower(query)
	var results []Entry
	for _, e := range Data {
		if strings.Contains(strings.ToLower(e.Name), query) {
			results = append(results, e)
			if len(results) >= 80 {
				break
			}
		}
	}
	return results
}

// ForCategory returns all emojis in the given category.
func ForCategory(category string) []Entry {
	var results []Entry
	for _, e := range Data {
		if e.Category == category {
			results = append(results, e)
		}
	}
	return results
}

// FindName returns the name of an emoji string, or the emoji itself if not found.
func FindName(emojiStr string) string {
	for _, e := range Data {
		if e.Emoji == emojiStr {
			return e.Name
		}
	}
	return emojiStr
}
