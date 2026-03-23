package locale

import (
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"path"
	"sync"
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
	langs := make([]string, 0, len(catalogs))
	for k := range catalogs {
		langs = append(langs, k)
	}
	// Stable order: en first, then alphabetical
	sorted := make([]string, 0, len(langs))
	for _, l := range langs {
		if l == "en" {
			continue
		}
		sorted = append(sorted, l)
	}
	return append([]string{"en"}, sorted...)
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

// MonthName returns the localized name of a month (1-12).
func MonthName(month int) string {
	keys := []string{"", "cal.jan", "cal.feb", "cal.mar", "cal.apr", "cal.may", "cal.jun",
		"cal.jul", "cal.aug", "cal.sep", "cal.oct", "cal.nov", "cal.dec"}
	if month >= 1 && month <= 12 {
		return T(keys[month])
	}
	return ""
}
