package debug

import (
	"log"
	"os"
	"strings"
)

// Enabled is true when FYNEDESK_DEBUG=1 is set.
var Enabled = strings.EqualFold(os.Getenv("FYNEDESK_DEBUG"), "1")

// categories holds the set of enabled categories (empty = all).
var categories = parseCategories()

func parseCategories() map[string]bool {
	raw := os.Getenv("FYNEDESK_DEBUG_CATEGORIES")
	if raw == "" {
		return nil
	}
	m := make(map[string]bool)
	for _, cat := range strings.Split(raw, ",") {
		cat = strings.TrimSpace(strings.ToUpper(cat))
		if cat != "" {
			m[cat] = true
		}
	}
	return m
}

// Log prints a debug message if debugging is enabled (optionally filtered by category).
// Usage: debug.Log("PERF", "frame took %v", dur)
func Log(category, format string, args ...interface{}) {
	if !Enabled {
		return
	}
	if categories != nil && !categories[strings.ToUpper(category)] {
		return
	}
	log.Printf("["+category+"] "+format, args...)
}
