package locale

import (
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"path"
	"slices"
	"strings"
	"sync"
	"time"
)

//go:embed *.json
var localeFS embed.FS

var (
	currentLang = "en"
	mu          sync.RWMutex
	catalogs    = map[string]map[string]string{}
)

func init() {
	entries, _ := localeFS.ReadDir(".")
	for _, e := range entries {
		name := e.Name()
		if path.Ext(name) != ".json" {
			continue
		}
		lang := name[:len(name)-5] // strip ".json"
		data, err := localeFS.ReadFile(name)
		if err != nil {
			continue
		}
		var cat map[string]string
		if err := json.Unmarshal(data, &cat); err != nil {
			log.Printf("locale: failed to parse %s: %v", name, err)
			continue
		}
		catalogs[lang] = cat
	}
}

// SetLanguage changes the active locale.
func SetLanguage(lang string) {
	mu.Lock()
	defer mu.Unlock()
	if _, ok := catalogs[lang]; ok {
		currentLang = lang
	}
}

// Language returns the current locale code.
func Language() string {
	mu.RLock()
	defer mu.RUnlock()
	return currentLang
}

// Languages returns the list of available locale codes.
func Languages() []string {
	mu.RLock()
	defer mu.RUnlock()
	// en first, then the others in alphabetical order.
	langs := make([]string, 0, len(catalogs))
	for k := range catalogs {
		if k != "en" {
			langs = append(langs, k)
		}
	}
	slices.Sort(langs)
	return append([]string{"en"}, langs...)
}

// LanguageLabel returns a human-readable label for a locale code.
func LanguageLabel(code string) string {
	labels := map[string]string{
		"en": "English",
		"fr": "Français",
	}
	if l, ok := labels[code]; ok {
		return l
	}
	return code
}

// T returns the translation of key in the current language.
// Falls back to English, then to the key itself.
func T(key string) string {
	mu.RLock()
	lang := currentLang
	mu.RUnlock()

	if cat, ok := catalogs[lang]; ok {
		if v, ok := cat[key]; ok {
			return v
		}
	}
	if cat, ok := catalogs["en"]; ok {
		if v, ok := cat[key]; ok {
			return v
		}
	}
	return key
}

// Tf returns a formatted translation (like fmt.Sprintf).
func Tf(key string, args ...any) string {
	return fmt.Sprintf(T(key), args...)
}

// WeekdayName returns the localized name of a weekday, as it reads in a
// date ("Monday", "lundi").
func WeekdayName(d time.Weekday) string {
	return T("weekday." + strings.ToLower(d.String()))
}

// WeekdayShort returns the abbreviated name of a weekday ("Mon", "lun.").
func WeekdayShort(d time.Weekday) string {
	return T("weekday.short." + strings.ToLower(d.String()))
}

// monthInDate returns the name of a month as it reads in a date ("January",
// "janvier"), full or abbreviated.
func monthInDate(m time.Month, short bool) string {
	if short {
		return T("month.short." + strings.ToLower(m.String()))
	}
	return T("month." + strings.ToLower(m.String()))
}

// Clock formats the time of day of t in a clock format of the settings:
// "12h" ("3:04pm") or else 24 hours ("15:04").
func Clock(t time.Time, format string, seconds bool) string {
	switch {
	case format == "12h" && seconds:
		return t.Format("3:04:05pm")
	case format == "12h":
		return t.Format("3:04pm")
	case seconds:
		return t.Format("15:04:05")
	}
	return t.Format("15:04")
}

// DateLong formats the day of t in full: "Monday, 2 January".
func DateLong(t time.Time) string {
	return Tf("date.long", WeekdayName(t.Weekday()), t.Day(), monthInDate(t.Month(), false))
}

// DateShort formats the day of t briefly: "Mon 2 Jan".
func DateShort(t time.Time) string {
	return Tf("date.short", WeekdayShort(t.Weekday()), t.Day(), monthInDate(t.Month(), true))
}

// DayMonth formats the day and month of t: "2 Jan".
func DayMonth(t time.Time) string {
	return Tf("date.dayMonth", t.Day(), monthInDate(t.Month(), true))
}

// MonthName returns the localized name of a month (1-12).
func MonthName(month int) string {
	keys := []string{
		"", "cal.jan", "cal.feb", "cal.mar", "cal.apr", "cal.may", "cal.jun",
		"cal.jul", "cal.aug", "cal.sep", "cal.oct", "cal.nov", "cal.dec",
	}
	if month >= 1 && month <= 12 {
		return T(keys[month])
	}
	return ""
}
