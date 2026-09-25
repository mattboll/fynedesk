package compositor

import (
	"testing"
	"time"
)

func TestNextIdleTimeout(t *testing.T) {
	tests := []struct {
		name  string
		steps []idleStep
		want  time.Duration
		found bool
	}{
		{"none", nil, 0, false},
		{"disabled", []idleStep{{0, true}}, 0, false},
		{"lock first", []idleStep{{5, true}, {6, true}}, 5 * time.Minute, true},
		{"lock done: blank next", []idleStep{{5, false}, {6, true}}, 6 * time.Minute, true},
		{"all done", []idleStep{{5, false}, {6, false}, {30, false}}, 0, false},
		{"suspend sooner", []idleStep{{10, true}, {0, true}, {3, true}}, 3 * time.Minute, true},
	}
	for _, tt := range tests {
		got, found := nextIdleTimeout(tt.steps...)
		if got != tt.want || found != tt.found {
			t.Errorf("%s: got %v, %v; want %v, %v", tt.name, got, found, tt.want, tt.found)
		}
	}
}

func TestCurtainAlpha(t *testing.T) {
	if a := curtainAlpha(curtainLead); a != 0 {
		t.Errorf("before the lead: %v, want 0", a)
	}
	if a := curtainAlpha(time.Minute); a != 0 {
		t.Errorf("a minute away: %v, want 0", a)
	}
	if a := curtainAlpha(0); a != curtainDarkest {
		t.Errorf("at the action: %v, want %v", a, curtainDarkest)
	}
	if a := curtainAlpha(-time.Second); a != curtainDarkest {
		t.Errorf("past the action: %v, want %v", a, curtainDarkest)
	}
	prev := 0.0
	for r := curtainLead; r >= 0; r -= time.Second {
		a := curtainAlpha(r)
		if a < prev {
			t.Fatalf("lighter at %v (%v) than a second before (%v)", r, a, prev)
		}
		prev = a
	}
	// Slow at first: halfway through, the curtain is far from half down.
	if a := curtainAlpha(curtainLead / 2); a > curtainDarkest/2 {
		t.Errorf("halfway: %v, want at most %v", a, curtainDarkest/2)
	}
}
