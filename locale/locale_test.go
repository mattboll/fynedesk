package locale

import (
	"testing"
)

func TestT_KnownKey(t *testing.T) {
	SetLanguage("en")
	got := T("about.title")
	if got != "About FyneDesk" {
		t.Errorf("T(\"about.title\") = %q, want %q", got, "About FyneDesk")
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
