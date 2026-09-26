package compositor

import "testing"

func TestNotificationID(t *testing.T) {
	n := newNotificationsDBus()

	a, replaces := n.notificationID("app", 0, "")
	if replaces {
		t.Fatal("a new notification replaces nothing")
	}
	if b, _ := n.notificationID("app", 0, ""); b == a {
		t.Fatal("each new notification gets its own id")
	}
	if id, replaces := n.notificationID("app", a, ""); id != a || !replaces {
		t.Fatalf("replaces_id: got %d %v, want %d true", id, replaces, a)
	}

	vol, _ := n.notificationID("volume", 0, "vol")
	if id, replaces := n.notificationID("volume", 0, "vol"); id != vol || !replaces {
		t.Fatalf("same stack tag: got %d %v, want %d true", id, replaces, vol)
	}
	if id, replaces := n.notificationID("other", 0, "vol"); id == vol || replaces {
		t.Fatal("stack tags belong to one application")
	}
}
