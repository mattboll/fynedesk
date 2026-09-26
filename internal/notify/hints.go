package notify

import "github.com/godbus/dbus/v5"

// Hints are the hints of a D-Bus notification (org.freedesktop.Notifications)
// that the desktop acts on.
type Hints struct {
	// Tag asks for the notification to replace the previous one of its
	// application with the same tag (volume or progress popups, a chat…).
	Tag string
	// Urgency is "low", "critical", or "" for normal.
	Urgency      string
	Transient    bool   // show it, keep no trace in the history
	Category     string // e.g. "email.arrived"
	DesktopEntry string // the .desktop file of the application, without suffix
}

// stackTagHints name the hints carrying a stack tag, by the servers that
// brought them in.
var stackTagHints = []string{"x-canonical-private-synchronous", "x-dunst-stack-tag", "synchronous"}

// StackTagHints are the capabilities to advertise for the stack tags (the
// last hint is not one).
func StackTagHints() []string {
	return append([]string{}, stackTagHints[:2]...)
}

// ParseHints reads the hints of a notification, ignoring those of the wrong
// type.
func ParseHints(hints map[string]dbus.Variant) Hints {
	h := Hints{
		Category:     hintString(hints, "category"),
		DesktopEntry: hintString(hints, "desktop-entry"),
		Urgency:      hintUrgency(hints),
	}
	for _, key := range stackTagHints {
		if h.Tag = hintString(hints, key); h.Tag != "" {
			break
		}
	}
	if v, ok := hints["transient"]; ok {
		switch t := v.Value().(type) {
		case bool:
			h.Transient = t
		case int32:
			h.Transient = t != 0
		}
	}
	return h
}

func hintString(hints map[string]dbus.Variant, key string) string {
	if v, ok := hints[key]; ok {
		if str, ok := v.Value().(string); ok {
			return str
		}
	}
	return ""
}

func hintUrgency(hints map[string]dbus.Variant) string {
	v, ok := hints["urgency"]
	if !ok {
		return ""
	}
	var level int
	switch u := v.Value().(type) {
	case byte:
		level = int(u)
	case int32:
		level = int(u)
	case uint32:
		level = int(u)
	default:
		return ""
	}
	switch level {
	case 0:
		return "low"
	case 2:
		return "critical"
	}
	return ""
}
