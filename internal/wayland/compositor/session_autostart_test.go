package compositor

import (
	"testing"

	"fyshos.com/tyde/internal/autostart"
)

func TestStartedBySession(t *testing.T) {
	slack := autostart.Entry{ID: "slack_slack.desktop", Args: []string{"env", "HINT=x", "/snap/bin/slack"}}
	kde := autostart.Entry{ID: "org.kde.kdeconnect.daemon.desktop", Args: []string{"/usr/bin/kdeconnectd"}}
	restored := []string{"firefox", "Slack"}
	if !startedBySession(slack, restored) {
		t.Error("Slack was restored, and is started again")
	}
	if startedBySession(kde, restored) {
		t.Error("KDE Connect was not restored, and is left out")
	}
}
