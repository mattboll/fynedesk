// Note that you need to have github.com/knightpp/dbus-codegen-go installed
//go:generate dbus-codegen-go -prefix org.freedesktop -package screensaver -output generated/screensaver.go dbus/ScreenSaver.xml

package ui

import (
	"math/rand"
	"os"
	"os/exec"
	"sync/atomic"
	"time"

	screensaver "fyshos.com/tyde/internal/ui/generated"
	"github.com/FyshOS/saver"
	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"

	"fyne.io/fyne/v2"
)

var (
	inhibitCount atomic.Int32
	lastActivity = time.Now()
)

func (l *desktop) startXscreensaver() {
	_, err := exec.LookPath("xscreensaver")
	if err != nil {
		fyne.LogError("xscreensaver command not found", err)
		return
	}
	err = exec.Command("xscreensaver", "-no-splash").Start()
	if err != nil {
		fyne.LogError("Failed to lock screen", err)
	}
}

func (l *desktop) TriggerScreenSaver(delay bool) {
	l.triggerScreenSaver(delay, false)
}

// triggerScreenSaver shows the screen saver, noting whether the machine is on
// its way to sleep as it appears so it can start with its animations stopped.
func (l *desktop) triggerScreenSaver(delay, suspending bool) {
	s := saver.NewScreenSaver(nil)
	s.LockImmediately = !delay
	s.Suspending = suspending
	s.ClockFormat = l.settings.ClockFormatting()
	if l.settings.ScreenSaverClock() {
		s.Label = "(clock)"
	} else {
		s.Label = l.settings.ScreenSaverLabel()
	}
	s.Lock = true

	l.wm.ShowScreensaver(s)
}

func (l *desktop) DelayScreenSaver() {
	lastActivity = time.Now()
}

func (l *desktop) watchScreenActivity() {
	watchDBus()
	idle := false
	to := time.NewTicker(5 * time.Second)

	for range to.C {
		if inhibitCount.Load() == 0 && lastActivity.Add(time.Minute*5).Before(time.Now()) {

			if !idle {
				idle = true

				fyne.Do(func() {
					l.TriggerScreenSaver(true)
				})
			}
		} else {
			idle = false
		}
	}
}

// watchSleep listens for systemd-logind's PrepareForSleep signal and shows the
// screensaver lock just before the machine suspends.
func (l *desktop) watchSleep() {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		fyne.LogError("failed to connect to system bus for sleep events", err)
		return
	}

	if err := conn.AddMatchSignal(
		dbus.WithMatchInterface("org.freedesktop.login1.Manager"),
		dbus.WithMatchMember("PrepareForSleep"),
		dbus.WithMatchObjectPath("/org/freedesktop/login1"),
	); err != nil {
		fyne.LogError("failed to watch for sleep events", err)
		return
	}

	login1 := conn.Object("org.freedesktop.login1", "/org/freedesktop/login1")
	inhibit := func() *os.File {
		var fd dbus.UnixFD
		call := login1.Call("org.freedesktop.login1.Manager.Inhibit", 0,
			"sleep", "Tyde", "Lock screen before sleep", "delay")
		if call.Err != nil {
			fyne.LogError("failed to take sleep inhibitor lock", call.Err)
			return nil
		}
		if err := call.Store(&fd); err != nil {
			fyne.LogError("failed to read sleep inhibitor lock", err)
			return nil
		}
		return os.NewFile(uintptr(fd), "logind-inhibit")
	}

	lock := inhibit()
	ch := make(chan *dbus.Signal, 4)
	conn.Signal(ch)
	for sig := range ch {
		if len(sig.Body) == 0 {
			continue
		}
		sleeping, ok := sig.Body[0].(bool)
		if !ok {
			continue
		}

		if sleeping {
			// About to suspend: show the locker now, give it a moment to map,
			// then release our inhibitor so the suspend can proceed.
			fyne.Do(func() {
				l.triggerScreenSaver(false, true)
			})
			time.Sleep(time.Millisecond * 400)
			if lock != nil {
				_ = lock.Close()
				lock = nil
			}
		} else {
			// Resumed: re-arm the inhibitor for the next sleep.
			lock = inhibit()
		}
	}
}

func watchDBus() {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		fyne.LogError("failed to connect to DBus to watch for screensaver inhibits", err)
		return
	}

	name := "org.freedesktop.ScreenSaver"
	r, err := conn.RequestName(name, dbus.NameFlagDoNotQueue)
	if err != nil || r != dbus.RequestNameReplyPrimaryOwner {
		fyne.LogError("Could not watch DBus screensaver, another is registered", err)
		return
	}

	s := &screenSaverWatcher{}
	path := "/org/freedesktop/ScreenSaver"
	err = conn.ExportAll(s, dbus.ObjectPath(path), "org.freedesktop.ScreenSaver")
	if err != nil {
		fyne.LogError("failed to export inhibits", err)
		return
	}

	node := introspect.Node{
		Name: path,
		Interfaces: []introspect.Interface{
			introspect.IntrospectData,
			prop.IntrospectData,
			screensaver.IntrospectDataScreenSaver,
		},
	}
	err = conn.Export(introspect.NewIntrospectable(&node), dbus.ObjectPath(path),
		"org.freedesktop.DBus.Introspectable")
	if err != nil {
		fyne.LogError("could not export our node data", err)
	}
}

type screenSaverWatcher struct{}

func (s *screenSaverWatcher) Inhibit(_ dbus.Sender, who, why string) (uint, *dbus.Error) {
	id := rand.Uint32()
	inhibitCount.Add(1)

	// TODO also check these are still alive every so often
	return uint(id), nil
}

func (s *screenSaverWatcher) UnInhibit(_ dbus.Sender, cookie uint32) *dbus.Error {
	// TODO compare to the cookies logged
	inhibitCount.Add(-1)
	return nil
}
