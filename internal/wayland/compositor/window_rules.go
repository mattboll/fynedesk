package compositor

import (
	"encoding/json"
	"log"
	"path/filepath"
	"strings"

	"fyshos.com/fynedesk/wlipc"
)

// matchWindowRule returns the first matching rule for the given app ID.
// Exact app_id match takes priority over glob patterns.
func (s *server) matchWindowRule(appID string) *wlipc.WindowRule {
	if appID == "" || len(s.windowRules) == 0 {
		return nil
	}
	lower := strings.ToLower(appID)

	// First pass: exact match
	for i := range s.windowRules {
		if s.windowRules[i].AppID != "" && strings.ToLower(s.windowRules[i].AppID) == lower {
			return &s.windowRules[i]
		}
	}

	// Second pass: glob pattern match
	for i := range s.windowRules {
		if s.windowRules[i].Pattern != "" {
			matched, _ := filepath.Match(strings.ToLower(s.windowRules[i].Pattern), lower)
			if matched {
				return &s.windowRules[i]
			}
		}
	}

	return nil
}

// applyWindowRuleXdg applies a window rule to an XDG view before positioning.
func (s *server) applyWindowRuleXdg(v *xdgView, rule *wlipc.WindowRule) {
	appID := getXdgToplevelAppID(v.xdgToplevel)

	if rule.Float != nil {
		v.floating = *rule.Float
	}
	if rule.Workspace > 0 && rule.Workspace <= s.numDesks {
		v.desk = rule.Workspace - 1 // Convert 1-based to 0-based
	}
	// Don't apply maximize/resize to dialog windows (they have a parent).
	isDialog := v.parent != nil
	if rule.Maximize != nil && *rule.Maximize && !isDialog {
		v.maximized = true
	}
	if rule.Pinned != nil {
		v.pinned = *rule.Pinned
	}
	if rule.Opacity > 0 && rule.Opacity <= 1.0 {
		v.opacity = rule.Opacity
	}
	if !isDialog {
		if rule.Width > 0 && rule.Height > 0 {
			v.xdgToplevel.SetSize(int32(rule.Width), int32(rule.Height))
		} else if rule.Width > 0 {
			v.xdgToplevel.SetSize(int32(rule.Width), int32(v.xdgToplevel.Current().Height()))
		} else if rule.Height > 0 {
			v.xdgToplevel.SetSize(int32(v.xdgToplevel.Current().Width()), int32(rule.Height))
		}
	}

	log.Printf("[RULE] Applied rule for XDG %q (dialog=%v): float=%v workspace=%d maximize=%v\n",
		appID, isDialog, rule.Float, rule.Workspace, rule.Maximize)
}

// applyWindowRuleXway applies a window rule to an XWayland view before positioning.
func (s *server) applyWindowRuleXway(v *xwayView, rule *wlipc.WindowRule) {
	class := getXwaylandSurfaceClass(v.surface)

	if rule.Float != nil {
		v.floating = *rule.Float
	}
	if rule.Workspace > 0 && rule.Workspace <= s.numDesks {
		v.desk = rule.Workspace - 1
	}
	// Don't apply maximize/resize to dialog windows (they have a parent).
	isDialog := v.parent != nil
	if rule.Maximize != nil && *rule.Maximize && !isDialog {
		v.maximized = true
	}
	if rule.Pinned != nil {
		v.pinned = *rule.Pinned
	}
	if rule.Opacity > 0 && rule.Opacity <= 1.0 {
		v.opacity = rule.Opacity
	}
	if !isDialog {
		if rule.Width > 0 && rule.Height > 0 {
			v.surface.Configure(int16(v.x), int16(v.y), uint16(rule.Width), uint16(rule.Height))
		} else if rule.Width > 0 {
			v.surface.Configure(int16(v.x), int16(v.y), uint16(rule.Width), uint16(v.surface.Height()))
		} else if rule.Height > 0 {
			v.surface.Configure(int16(v.x), int16(v.y), uint16(v.surface.Width()), uint16(rule.Height))
		}
	}

	log.Printf("[RULE] Applied rule for XWayland %q (dialog=%v): float=%v workspace=%d maximize=%v\n",
		class, isDialog, rule.Float, rule.Workspace, rule.Maximize)
}

// loadWindowRules loads window rules from the prefs map.
func (s *server) loadWindowRules(prefs map[string]interface{}) {
	rulesStr, ok := prefs["windowrules"].(string)
	if !ok || rulesStr == "" {
		s.windowRules = nil
		return
	}

	var rules []wlipc.WindowRule
	if err := json.Unmarshal([]byte(rulesStr), &rules); err != nil {
		log.Printf("Warning: could not parse window rules: %v\n", err)
		s.windowRules = nil
		return
	}

	// Validate rules: check pattern syntax and field bounds
	var valid []wlipc.WindowRule
	for _, rule := range rules {
		// Validate glob pattern syntax
		if rule.Pattern != "" {
			if _, err := filepath.Match(rule.Pattern, "test"); err != nil {
				log.Printf("Warning: invalid glob pattern %q in window rule: %v\n", rule.Pattern, err)
				continue
			}
			if len(rule.Pattern) > 256 {
				log.Printf("Warning: window rule pattern too long (%d chars), skipping\n", len(rule.Pattern))
				continue
			}
		}
		// Validate AppID length
		if len(rule.AppID) > 256 {
			log.Printf("Warning: window rule AppID too long (%d chars), skipping\n", len(rule.AppID))
			continue
		}
		// Validate opacity bounds
		if rule.Opacity < 0 || rule.Opacity > 1.0 {
			rule.Opacity = 0 // Reset invalid opacity (0 = don't apply)
		}
		valid = append(valid, rule)
	}

	s.windowRules = valid
	log.Printf("Window rules loaded: %d rules (%d skipped)\n", len(valid), len(rules)-len(valid))
}
