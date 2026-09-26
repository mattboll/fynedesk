package status

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	cal "fyshos.com/tyde/internal/calendar"
)

func TestNextMeetingLabel(t *testing.T) {
	long := cal.Event{Title: "Réunion d'équipe élargie à Genève"}
	got := formatNextMeetingLabel(long, 5*time.Minute)
	if !utf8.ValidString(got) {
		t.Fatalf("label cut inside a character: %q", got)
	}
	if !strings.HasPrefix(got, "Réunion d'équipe élargi…") {
		t.Errorf("title: %q", got)
	}
	if !strings.HasSuffix(got, "5 min") {
		t.Errorf("minutes: %q", got)
	}
}
