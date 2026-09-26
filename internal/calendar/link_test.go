package calendar

import "testing"

func TestIsWebLink(t *testing.T) {
	for link, want := range map[string]bool{
		"https://meet.google.com/abc-defg-hij": true,
		"https://example.zoom.us/j/123?pwd=x":  true,
		"http://meet.example.com/x":            false,
		"file:///etc/passwd":                   false,
		"steam://run/1":                        false,
		"--help":                               false,
		"https://":                             false,
		"https://user:pass@evil.example/meet":  false,
		"":                                     false,
		"javascript:alert(1)":                  false,
		"HTTPS://MEET.GOOGLE.COM/abc-defg-hij": true,
	} {
		if got := IsWebLink(link); got != want {
			t.Errorf("IsWebLink(%q) = %v, want %v", link, got, want)
		}
	}
}
