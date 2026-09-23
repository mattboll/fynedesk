package wallpaper

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// TimeSlot maps a time range to a wallpaper image path.
type TimeSlot struct {
	Name string // e.g. "morning", "day", "evening", "night"
	Hour int    // Start hour (0-23)
	Path string // Image file path
}

// DefaultSlots returns the standard 4-period time-of-day schedule.
func DefaultSlots() []TimeSlot {
	return []TimeSlot{
		{Name: "night", Hour: 0},
		{Name: "morning", Hour: 6},
		{Name: "day", Hour: 10},
		{Name: "evening", Hour: 18},
		{Name: "night", Hour: 22},
	}
}

// CurrentSlotName returns the name of the time slot for the given hour.
func CurrentSlotName(hour int) string {
	slots := DefaultSlots()
	name := slots[0].Name
	for _, s := range slots {
		if hour >= s.Hour {
			name = s.Name
		}
	}
	return name
}

// imageExtensions is the set of file extensions we recognize as wallpaper images.
var imageExtensions = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true,
}

// ResolveDynamicWallpaper returns the wallpaper path for the current time.
// dir should be a directory containing images named by time slot
// (e.g. morning.jpg, day.png, evening.jpg, night.jpg).
//
// If only 2 images exist (e.g. "day" and "night"), those periods are used.
// Falls back to the first image found if no slot matches.
func ResolveDynamicWallpaper(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("read wallpaper dir: %w", err)
	}

	// Build map: slot name → file path
	slotFiles := make(map[string]string)
	var allImages []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if !imageExtensions[ext] {
			continue
		}
		base := strings.TrimSuffix(e.Name(), ext)
		base = strings.ToLower(base)
		fullPath := filepath.Join(dir, e.Name())
		slotFiles[base] = fullPath
		allImages = append(allImages, fullPath)
	}

	if len(allImages) == 0 {
		return "", fmt.Errorf("no images found in %s", dir)
	}

	// Sort for deterministic fallback
	sort.Strings(allImages)

	slotName := CurrentSlotName(time.Now().Hour())

	// Exact match
	if p, ok := slotFiles[slotName]; ok {
		return p, nil
	}

	// Fallback: try adjacent slots
	fallbackOrder := []string{"day", "morning", "evening", "night"}
	for _, name := range fallbackOrder {
		if p, ok := slotFiles[name]; ok {
			return p, nil
		}
	}

	// Last resort: first image alphabetically
	return allImages[0], nil
}
