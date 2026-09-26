package systray

import "testing"

func TestPixelsToImageRejectsBadIcons(t *testing.T) {
	type pix = struct {
		V0 int32
		V1 int32
		V2 []byte
	}
	good := pix{2, 2, make([]byte, 2*2*4)}
	if pixelsToImage(good) == nil {
		t.Error("a well-formed icon is refused")
	}
	for name, bad := range map[string]pix{
		"data too short": {4, 4, make([]byte, 10)},
		"zero size":      {0, 3, nil},
		"negative":       {-1, 2, make([]byte, 8)},
		"too big":        {5000, 1, make([]byte, 5000*4)},
	} {
		if pixelsToImage(bad) != nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if firstImage([]pix{{9, 9, nil}, good}) == nil {
		t.Error("the good icon after a bad one is not used")
	}
}
