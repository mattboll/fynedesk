package compositor

import "testing"

func TestShareTracker(t *testing.T) {
	var tr shareTracker

	// A screenshot: a session that ends before it lasted.
	n := tr.started(1)
	if active, changed := tr.ended(1); active || changed {
		t.Fatal("a short session is no share")
	}
	if _, changed := tr.lasted(n); changed {
		t.Fatal("an ended session cannot become a share")
	}

	// A share, and a screenshot taken during it.
	n = tr.started(2)
	if active, changed := tr.lasted(n); !active || !changed {
		t.Fatal("a session that lasts is a share")
	}
	tr.started(3)
	if active, changed := tr.ended(3); !active || changed {
		t.Fatal("a screenshot during a share does not end it")
	}

	// A new session at the address of an ended one is not taken for it.
	tr.started(4)
	old := tr.started(5)
	tr.ended(5)
	tr.started(5)
	if _, changed := tr.lasted(old); changed {
		t.Fatal("the new session at a reused address inherited the share")
	}

	if active, changed := tr.ended(2); active || !changed {
		t.Fatal("the share should end with its session")
	}
}
