package compositor

import "testing"

func TestOutputIdentity(t *testing.T) {
	tests := []struct{ name, make, model, serial, want string }{
		{"DP-2", "Dell Inc.", "DELL U2723QE", "7X9K1Q3", "Dell Inc. DELL U2723QE 7X9K1Q3"}, // any port
		{"DP-1", "Dell Inc.", "DELL U2723QE", "", "Dell Inc. DELL U2723QE @DP-1"},          // twins told apart by port
		{"HDMI-A-1", "", "", "", "HDMI-A-1"},
	}
	for _, tt := range tests {
		if got := outputIdentity(tt.name, tt.make, tt.model, tt.serial); got != tt.want {
			t.Errorf("outputIdentity(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestSetupKeyIgnoresOrder(t *testing.T) {
	if setupKey([]string{"laptop", "dell"}) != setupKey([]string{"dell", "laptop"}) {
		t.Error("the same screens plugged in another order are another setup")
	}
	if setupKey([]string{"laptop"}) == setupKey([]string{"laptop", "dell"}) {
		t.Error("the laptop alone and with the screen are the same setup")
	}
}

func TestPlaceBookLearnsPerSetup(t *testing.T) {
	var b placeBook
	laptop := setupKey([]string{"laptop"})
	docked := setupKey([]string{"laptop", "dell"})
	max := windowPlace{Output: "laptop", Zone: "max"}
	left := windowPlace{Output: "dell", Zone: "left"}

	if !b.learn(laptop, "firefox", []*windowPlace{&max}) {
		t.Error("a new place is not a change")
	}
	if b.learn(laptop, "firefox", []*windowPlace{&max}) {
		t.Error("the same place again is a change")
	}
	b.learn(docked, "firefox", []*windowPlace{&left})

	if p, ok := b.lookup(laptop, "firefox", 0); !ok || p != max {
		t.Errorf("laptop: got %+v, %v", p, ok)
	}
	if p, ok := b.lookup(docked, "firefox", 0); !ok || p != left {
		t.Errorf("docked: got %+v, %v", p, ok)
	}
	if _, ok := b.lookup(docked, "slack", 0); ok {
		t.Error("an unknown application has a place")
	}
}

func TestPlaceBookKeepsWhatItCannotSee(t *testing.T) {
	var b placeBook
	setup := setupKey([]string{"laptop"})
	first := windowPlace{Output: "laptop", Zone: "left"}
	second := windowPlace{Output: "laptop", Zone: "right"}
	b.learn(setup, "kitty", []*windowPlace{&first, &second})

	// The first window is minimized: its place stays, the second moves.
	moved := windowPlace{Output: "laptop", Zone: "max"}
	b.learn(setup, "kitty", []*windowPlace{nil, &moved})
	if p, _ := b.lookup(setup, "kitty", 0); p != first {
		t.Errorf("the minimized window lost its place: %+v", p)
	}
	if p, _ := b.lookup(setup, "kitty", 1); p != moved {
		t.Errorf("the moved window: %+v", p)
	}

	// Only one window open now: the place of the second one is kept.
	b.learn(setup, "kitty", []*windowPlace{&second})
	if p, ok := b.lookup(setup, "kitty", 1); !ok || p != moved {
		t.Errorf("the closed window lost its place: %+v, %v", p, ok)
	}
}

func TestZoneNames(t *testing.T) {
	for z, name := range zoneNames {
		if zoneFromName(name) != z {
			t.Errorf("%q does not name zone %d back", name, z)
		}
	}
	if zoneFromName("") != snapNone {
		t.Error("a free window has a zone")
	}
}
