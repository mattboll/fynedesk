package status

import "testing"

func TestParseRfkillWlan(t *testing.T) {
	out := "0 bluetooth unblocked unblocked\n1 wlan      blocked   unblocked\n"
	id, blocked, err := parseRfkillWlan(out)
	if err != nil || id != "1" || !blocked {
		t.Errorf("got %q %v %v", id, blocked, err)
	}
	if _, blocked, _ := parseRfkillWlan("2 wlan unblocked blocked\n"); !blocked {
		t.Error("a hard block is a block")
	}
	if _, _, err := parseRfkillWlan("0 bluetooth unblocked unblocked\n"); err == nil {
		t.Error("no wlan, no error")
	}
}
