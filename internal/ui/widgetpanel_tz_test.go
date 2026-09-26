package ui

import "testing"

func TestGetOffsetFollowsTZ(t *testing.T) {
	t.Setenv("TZ", "Asia/Tokyo")
	if got := getOffset(); got != 9*60 {
		t.Errorf("TZ=Asia/Tokyo: offset %d, want 540", got)
	}
	t.Setenv("TZ", "UTC")
	if got := getOffset(); got != 0 {
		t.Errorf("TZ=UTC: offset %d, want 0", got)
	}
}
