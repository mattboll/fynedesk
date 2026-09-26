package locale

import (
	"testing"
	"time"
)

func TestT_KnownKey(t *testing.T) {
	SetLanguage("en")
	got := T("about.title")
	if got != "About Tyde" {
		t.Errorf("T(\"about.title\") = %q, want %q", got, "About Tyde")
	}
}

func TestT_UnknownKey(t *testing.T) {
	SetLanguage("en")
	got := T("nonexistent.key.xyz")
	if got != "nonexistent.key.xyz" {
		t.Errorf("T(unknown key) = %q, want the key itself", got)
	}
}

func TestSetLanguage(t *testing.T) {
	SetLanguage("fr")
	if Language() != "fr" {
		t.Errorf("Language() = %q after SetLanguage(\"fr\"), want \"fr\"", Language())
	}

	// French translation should be returned
	got := T("cal.jan")
	if got == "January" {
		t.Error("T(\"cal.jan\") returned English after switching to French")
	}

	// Reset to English
	SetLanguage("en")
	if Language() != "en" {
		t.Errorf("Language() = %q after SetLanguage(\"en\"), want \"en\"", Language())
	}
}

func TestSetLanguage_InvalidIgnored(t *testing.T) {
	SetLanguage("en")
	SetLanguage("zz_invalid")
	if Language() != "en" {
		t.Errorf("Language() = %q after invalid SetLanguage, want \"en\"", Language())
	}
}

func TestLanguages(t *testing.T) {
	langs := Languages()
	if len(langs) < 2 {
		t.Fatalf("Languages() returned %d languages, want at least 2", len(langs))
	}
	if langs[0] != "en" {
		t.Errorf("Languages()[0] = %q, want \"en\" first", langs[0])
	}
}

func TestLanguageLabel(t *testing.T) {
	if got := LanguageLabel("en"); got != "English" {
		t.Errorf("LanguageLabel(\"en\") = %q, want \"English\"", got)
	}
	if got := LanguageLabel("fr"); got != "Français" {
		t.Errorf("LanguageLabel(\"fr\") = %q, want \"Français\"", got)
	}
	if got := LanguageLabel("zz"); got != "zz" {
		t.Errorf("LanguageLabel(\"zz\") = %q, want \"zz\"", got)
	}
}

func TestTf(t *testing.T) {
	SetLanguage("en")
	got := Tf("theme.active", "MyTheme")
	want := "Active: MyTheme"
	if got != want {
		t.Errorf("Tf(\"theme.active\", \"MyTheme\") = %q, want %q", got, want)
	}
}

func TestMonthName(t *testing.T) {
	SetLanguage("en")
	if got := MonthName(1); got != "January" {
		t.Errorf("MonthName(1) = %q, want \"January\"", got)
	}
	if got := MonthName(12); got != "December" {
		t.Errorf("MonthName(12) = %q, want \"December\"", got)
	}
	if got := MonthName(0); got != "" {
		t.Errorf("MonthName(0) = %q, want \"\"", got)
	}
	if got := MonthName(13); got != "" {
		t.Errorf("MonthName(13) = %q, want \"\"", got)
	}
}

func TestDates(t *testing.T) {
	day := time.Date(2026, time.September, 28, 9, 0, 0, 0, time.UTC) // a Monday
	for lang, want := range map[string][3]string{
		"en": {"Monday, 28 September", "Mon 28 Sep", "28 Sep"},
		"fr": {"lundi 28 septembre", "lun. 28 sept.", "28 sept."},
	} {
		SetLanguage(lang)
		got := [3]string{DateLong(day), DateShort(day), DayMonth(day)}
		if got != want {
			t.Errorf("%s: got %q, want %q", lang, got, want)
		}
	}
	SetLanguage("en")
}

func TestClock(t *testing.T) {
	at := time.Date(2026, time.September, 28, 21, 5, 9, 0, time.UTC)
	for _, c := range []struct {
		format  string
		seconds bool
		want    string
	}{
		{"12h", false, "9:05pm"},
		{"12h", true, "9:05:09pm"},
		{"24h", false, "21:05"},
		{"24h", true, "21:05:09"},
	} {
		if got := Clock(at, c.format, c.seconds); got != c.want {
			t.Errorf("Clock(%q, %v) = %q, want %q", c.format, c.seconds, got, c.want)
		}
	}
}
