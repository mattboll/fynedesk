package notify

import "testing"

func TestIDs(t *testing.T) {
	var ids IDs
	next := uint32(0)
	fresh := func() uint32 { next++; return next }

	a, replaces := ids.ID("app", 0, "", fresh)
	if replaces {
		t.Fatal("a new notification replaces nothing")
	}
	if b, _ := ids.ID("app", 0, "", fresh); b == a {
		t.Fatal("each new notification gets its own id")
	}
	if id, replaces := ids.ID("app", a, "", fresh); id != a || !replaces {
		t.Fatalf("replaces_id: got %d %v, want %d true", id, replaces, a)
	}

	vol, _ := ids.ID("volume", 0, "vol", fresh)
	if id, replaces := ids.ID("volume", 0, "vol", fresh); id != vol || !replaces {
		t.Fatalf("same stack tag: got %d %v, want %d true", id, replaces, vol)
	}
	if id, replaces := ids.ID("other", 0, "vol", fresh); id == vol || replaces {
		t.Fatal("stack tags belong to one application")
	}
}
