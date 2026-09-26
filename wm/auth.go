package wm

import (
	"bytes"
	"fmt"
	"image/color"
	"log"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"sync"

	"fyshos.com/tyde"
	"fyshos.com/tyde/locale"
	wmTheme "fyshos.com/tyde/theme"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/godbus/dbus/v5"
)

type subj struct {
	Kind    string
	Details map[string]dbus.Variant
}

type auth struct {
	mu      sync.Mutex
	dialogs map[string]func() // cookie -> dismiss the modal and end the session
}

func (a *auth) register() {
	conn2, err := dbus.SystemBus()
	if err != nil {
		fyne.LogError("Could not connect to DBus for authentication events", err)
		return
	}

	err = conn2.ExportAll(a, "/AuthenticationAgent", "org.freedesktop.PolicyKit1.AuthenticationAgent")
	if err != nil {
		fyne.LogError("Could not start auth agent server", err)
	}

	session, err := sessionID(conn2)
	if err != nil {
		fyne.LogError("Could not determine our login session, auth agent not registered", err)
		return
	}

	obj := conn2.Object("org.freedesktop.PolicyKit1", "/org/freedesktop/PolicyKit1/Authority")
	call := obj.Call("org.freedesktop.PolicyKit1.Authority.RegisterAuthenticationAgent", 0,

		&subj{"unix-session", map[string]dbus.Variant{
			"session-id": dbus.MakeVariant(session),
		}}, "en_US",
		"/AuthenticationAgent")
	if call.Err != nil {
		fyne.LogError("Failed to register auth agent", call.Err)
	}
}

// sessionID reports the logind session this process belongs to.
//
// polkit requires an authentication agent to register for the session it is
// actually running in: registering for any other is rejected outright with
// "Passed session and the session the caller is in differs. They must be equal
// for now." That leaves the desktop with no agent at all, so password prompts
// fall back to a terminal (or fail entirely) and responses are refused with
// "No session for cookie".
func sessionID(conn *dbus.Conn) (string, error) {
	if id := os.Getenv("XDG_SESSION_ID"); id != "" {
		return id, nil
	}

	// No environment hint, so ask logind which session owns this process.
	var path dbus.ObjectPath
	obj := conn.Object("org.freedesktop.login1", "/org/freedesktop/login1")
	err := obj.Call("org.freedesktop.login1.Manager.GetSessionByPID", 0, uint32(os.Getpid())).Store(&path)
	if err != nil {
		return "", err
	}

	prop, err := conn.Object("org.freedesktop.login1", path).
		GetProperty("org.freedesktop.login1.Session.Id")
	if err != nil {
		return "", err
	}
	id, ok := prop.Value().(string)
	if !ok {
		return "", fmt.Errorf("unexpected session id type %T", prop.Value())
	}
	return id, nil
}

type ident struct {
	ID      string
	Details map[string]dbus.Variant
}

// BeginAuthentication asks the user for the password polkit wants, and
// returns once they gave it or cancelled. The password is never kept: each
// request asks again (polkit itself remembers an authorisation for a while
// where its policy says so).
func (a *auth) BeginAuthentication(actionID, message, iconName string, details map[string]string, cookie string, ids []ident, sender dbus.Sender) (err *dbus.Error) {
	username, err2 := a.resolveUser(ids)
	if err2 != nil {
		fyne.LogError("Failed to look up user", err2)
	}

	done := make(chan struct{})
	var once sync.Once
	var closeModal func()
	// dismiss tears down the modal and ends the auth session. It is guarded so the
	// buttons and a CancelAuthentication call cannot double-close.
	dismiss := func() {
		once.Do(func() {
			fyne.Do(func() {
				if closeModal != nil {
					closeModal()
				}
			})
			a.mu.Lock()
			delete(a.dialogs, cookie)
			a.mu.Unlock()
			close(done)
		})
	}
	a.mu.Lock()
	a.dialogs[cookie] = dismiss
	a.mu.Unlock()

	fyne.DoAndWait(func() {
		closeModal = a.showDialog(username, message, cookie, dismiss)
	})
	<-done
	return nil
}

// showDialog shows the password dialog as a modal; it returns what closes it.
// Fyne thread.
func (a *auth) showDialog(username, message, cookie string, dismiss func()) func() {
	pass := widget.NewPasswordEntry()
	f := widget.NewForm(
		widget.NewFormItem(locale.T("auth.user"), widget.NewLabel(username)),
		widget.NewFormItem(locale.T("auth.password"), pass),
	)

	var authBtn *widget.Button
	authBtn = widget.NewButton(locale.T("auth.authorize"), func() {
		authBtn.Disable()
		password := pass.Text
		go func() { // the helper takes its time (and waits on a wrong password)
			err := a.reply(username, cookie, password)
			fyne.Do(func() {
				if err != nil {
					log.Println("Auth err", err)
					pass.SetText("")
					authBtn.Enable()
					return
				}
				dismiss()
			})
		}()
	})
	authBtn.Importance = widget.HighImportance
	cancel := widget.NewButton(locale.T("auth.cancel"), dismiss)
	pass.OnSubmitted = func(string) {
		if !authBtn.Disabled() {
			authBtn.OnTapped()
		}
	}

	header := widget.NewRichTextFromMarkdown(fmt.Sprintf("### %s\n\n```%s```", locale.T("auth.title"), message))
	header.Wrapping = fyne.TextWrapBreak
	header.Refresh()
	bottomPad := canvas.NewRectangle(color.Transparent)
	bottomPad.SetMinSize(fyne.NewSquareSize(10))
	content := container.NewBorder(
		header,
		container.NewVBox(
			container.NewHBox(layout.NewSpacer(),
				container.NewGridWithColumns(2, cancel, authBtn),
				layout.NewSpacer()), bottomPad,
		),
		nil, nil, f,
	)

	r, g, b, _ := theme.Color(theme.ColorNameOverlayBackground).RGBA()
	bgCol := &color.NRGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: 230}

	bg := canvas.NewRectangle(bgCol)
	icon := canvas.NewImageFromResource(wmTheme.LockIcon)
	iconBox := container.NewWithoutLayout(icon)
	icon.Resize(fyne.NewSize(92, 92))
	icon.Move(fyne.NewPos(300-92-theme.Padding(), theme.Padding()))
	dialog := container.NewStack(
		iconBox, bg,
		container.NewPadded(content),
	)

	// Show as a modal: the desktop centres the dialog over a blurred backdrop that
	// does not dismiss on tap or mouse-out, so it stays until a button calls dismiss.
	closeModal := tyde.Instance().ShowModal(dialog, fyne.NewSize(340, 220))
	tyde.Instance().Root().Canvas().Focus(pass)
	return closeModal
}

// resolveUser picks which identity to authenticate as from those polkit offers.
// It prefers the current user when they are eligible - so people type their own
// password rather than an unexpected admin's - and otherwise falls back to the
// first offered user.
func (a *auth) resolveUser(ids []ident) (string, error) {
	current, _ := user.Current()

	if current != nil {
		for _, id := range ids {
			if id.ID == "unix-user" && identUID(id) == current.Uid {
				return current.Username, nil
			}
		}
	}

	for _, id := range ids {
		if id.ID != "unix-user" {
			continue
		}
		if uid := identUID(id); uid != "" {
			if usr, err := user.LookupId(uid); err == nil {
				return usr.Username, nil
			}
		}
	}

	if current != nil {
		return current.Username, nil
	}
	return "", fmt.Errorf("no user identity offered to authenticate")
}

// identUID reads the numeric uid from a polkit unix-user identity, whose "uid"
// detail is a uint32 variant. Returns "" when absent or the wrong type.
func identUID(id ident) string {
	v, ok := id.Details["uid"]
	if !ok {
		return ""
	}
	switch n := v.Value().(type) {
	case uint32:
		return strconv.FormatUint(uint64(n), 10)
	case int32:
		return strconv.FormatInt(int64(n), 10)
	case uint64:
		return strconv.FormatUint(n, 10)
	case int64:
		return strconv.FormatInt(n, 10)
	}
	return ""
}

// polkitHelpers are where distributions install polkit's helper.
var polkitHelpers = []string{"/usr/lib/polkit-1/polkit-agent-helper-1", "/usr/libexec/polkit-agent-helper-1"}

func (a *auth) reply(username string, cookie string, pass string) error {
	helper := polkitHelpers[0]
	for _, h := range polkitHelpers {
		if _, err := os.Stat(h); err == nil {
			helper = h
			break
		}
	}
	cmd := exec.Command(helper, username)

	buffer := bytes.Buffer{}
	buffer.Write([]byte(cookie + "\n"))
	buffer.Write([]byte(pass + "\n"))
	cmd.Stdin = &buffer

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (a *auth) CancelAuthentication(cookie string, sender dbus.Sender) (err *dbus.Error) {
	a.mu.Lock()
	dismiss, ok := a.dialogs[cookie]
	a.mu.Unlock()
	if ok {
		dismiss() // tears down the modal and ends the session
	}
	return nil
}

// StartAuthAgent asks our policy kit agent to start listening for auth requests.
func StartAuthAgent() {
	a := &auth{dialogs: make(map[string]func())}
	go a.register()
}
