package systray

import "testing"

func TestSlackRedDot(t *testing.T) {
	for tip, red := range map[string]bool{
		"You have 1 notification":   true,
		"You have 12 notifications": true,
		"Vous avez 2 notifications": true,
		"You have unread messages":  false, // the blue dot
		"No unread messages":        false,
		"":                          false,
	} {
		if got := slackRedDot.MatchString(tip); got != red {
			t.Errorf("%q: red dot = %v, want %v", tip, got, red)
		}
	}
}
