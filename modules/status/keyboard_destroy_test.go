package status

import "testing"

func TestKeyboardDestroyTwice(t *testing.T) {
	k := &keyboardLayout{done: make(chan struct{})}
	k.Destroy()
	k.Destroy() // panicked: close of closed channel
}
