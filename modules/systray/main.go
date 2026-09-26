//Note that you need to have github.com/knightpp/dbus-codegen-go installed from "custom" branch
//go:generate dbus-codegen-go -prefix org.kde -package notifier -output generated/notifier/status_notifier_item.go StatusNotifierItem.xml
//go:generate dbus-codegen-go -prefix org.kde -package watcher -output generated/watcher/status_notifier_watcher.go StatusNotifierWatcher.xml
//go:generate dbus-codegen-go -prefix com.canonical -package menu -output generated/menu/dbus_menu.go DbusMenu.xml

// Package systray implements a system tray module using the D-Bus StatusNotifierItem protocol for application indicators.
package systray

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/FyshOS/appie"
	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"

	"fyshos.com/tyde"
	"fyshos.com/tyde/internal/icon"
	"fyshos.com/tyde/modules/systray/generated/menu"
	"fyshos.com/tyde/modules/systray/generated/notifier"
	"fyshos.com/tyde/modules/systray/generated/watcher"
	wmtheme "fyshos.com/tyde/theme"
	"fyshos.com/tyde/wlipc"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	deskDriver "fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const (
	path     = "/StatusNotifierWatcher"
	hostPath = "/StatusNotifierHost"
)

var resourceID = 0

func init() {
	tyde.RegisterModule(trayMeta)
}

var trayMeta = tyde.ModuleMetadata{
	Name:        "SystemTray",
	NewInstance: NewTray,
}

type tray struct {
	conn   *dbus.Conn
	menu   *menu.Dbusmenu
	done   chan struct{} // closed by Destroy: stops the watch on the tray apps
	signal chan *dbus.Signal

	box   *fyne.Container
	lock  sync.Mutex
	nodes map[dbus.Sender]*node
}

type node struct {
	ico *multiButton
	ni  *notifier.StatusNotifierItem
	pid uint32
}

// NewTray creates a new module that will show a system tray in the status area
func NewTray() tyde.Module {
	iconSize := wmtheme.NarrowBarWidth
	grid := container.New(collapsingGridWrap(fyne.NewSize(iconSize, iconSize)))
	t := &tray{box: grid, nodes: make(map[dbus.Sender]*node)}

	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		fyne.LogError("Could not connect to DBus for system tray events", err)
		return t
	}
	t.conn = conn

	err = conn.ExportAll(struct{}{}, hostPath, "org.kde.StatusNotifierHost")
	if err != nil {
		fyne.LogError("Failed to export notifier host", err)
		return t
	}

	err = conn.ExportAll(t, path, "org.kde.StatusNotifierWatcher")
	if err != nil {
		fyne.LogError("Unable to register watcher", err)
		return t
	}

	reply, err := conn.RequestName("org.kde.StatusNotifierWatcher", dbus.NameFlagDoNotQueue)
	if err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		log.Println("Failed to claim notifier watcher name (another tray?)", reply, err)
		return t
	}

	_, err = prop.Export(conn, path, createPropSpec())
	if err != nil {
		log.Printf("Failed to export notifier item properties to bus")
		return t
	}

	node := introspect.Node{
		Name: path,
		Interfaces: []introspect.Interface{
			introspect.IntrospectData,
			prop.IntrospectData,
			watcher.IntrospectDataStatusNotifierWatcher,
		},
	}
	err = conn.Export(introspect.NewIntrospectable(&node), path,
		"org.freedesktop.DBus.Introspectable")
	if err != nil {
		log.Printf("Failed to export introspection %v", err)
		return t
	}

	hostErr := t.RegisterStatusNotifierHost(conn.Names()[0], "")
	if hostErr != nil {
		fyne.LogError("Failed to register our systray host, another may already be running", hostErr)
	}

	watchErr := t.conn.AddMatchSignal(dbus.WithMatchInterface("org.freedesktop.DBus"), dbus.WithMatchObjectPath("/org/freedesktop/DBus"))
	_ = t.conn.AddMatchSignal(dbus.WithMatchInterface("org.kde.StatusNotifierItem"))
	if watchErr != nil {
		fyne.LogError("Failed to monitor systray name loss", watchErr)
	}

	t.signal = make(chan *dbus.Signal, 10)
	t.conn.Signal(t.signal)
	go func() {
		for v := range t.signal {
			switch v.Name {
			case "org.freedesktop.DBus.NameOwnerChanged":
				name := v.Body[0]
				newOwner := v.Body[2]
				if newOwner == "" {
					t.removeNode(dbus.Sender(name.(string)))
				}
			case "org.kde.StatusNotifierItem.NewIcon":
				t.lock.Lock()
				item, ok := t.nodes[dbus.Sender(v.Sender)]
				t.lock.Unlock()
				if ok {
					icon := t.fetchIcon(item)
					fyne.Do(func() {
						item.ico.SetIcon(icon)
					})
				}
			default:
				continue
			}
		}
	}()

	t.done = make(chan struct{})
	go t.monitorProcesses(t.done)

	return t
}

func (t *tray) Destroy() {
	if t.done != nil {
		close(t.done)
		t.done = nil
	}
	if t.conn != nil {
		if t.signal != nil {
			t.conn.RemoveSignal(t.signal)
			close(t.signal)
			t.signal = nil
		}
		if err := t.conn.Close(); err != nil {
			log.Printf("[systray] dbus close failed: %v", err)
		}
		t.conn = nil
	}
}

// removeNode drops a tray icon for the given sender, both from the visible
// tray and from our internal tracking map. Safe to call for unknown senders.
func (t *tray) removeNode(sender dbus.Sender) {
	t.lock.Lock()
	item, ok := t.nodes[sender]
	if ok {
		delete(t.nodes, sender)
	}
	t.lock.Unlock()
	if !ok {
		return
	}

	fyne.Do(func() {
		t.box.Remove(item.ico)
		t.box.Refresh()
	})
}

// monitorProcesses watches the processes backing each tray icon.
// We periodically confirm each backing process is still
// alive and remove the icon for any that have gone away.
func (t *tray) monitorProcesses(done <-chan struct{}) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
		}
		var dead []dbus.Sender
		t.lock.Lock()
		for sender, item := range t.nodes {
			if item.pid != 0 && !processAlive(item.pid) {
				dead = append(dead, sender)
			}
		}
		t.lock.Unlock()

		for _, sender := range dead {
			t.removeNode(sender)
		}
	}
}

// processID asks the bus for the process id behind a connection so we can keep
// an eye on it. It returns 0 if the owner could not be resolved.
func (t *tray) processID(sender dbus.Sender) uint32 {
	var pid uint32
	err := t.conn.BusObject().Call("org.freedesktop.DBus.GetConnectionUnixProcessID", 0,
		string(sender)).Store(&pid)
	if err != nil {
		return 0
	}
	return pid
}

// processAlive reports whether the given process id is still running.
// The process is considered gone if /proc has no entry for it, or if
// that entry reports the zombie ("Z") state.
func processAlive(pid uint32) bool {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return false // no /proc entry: the process is gone
	}

	if i := bytes.LastIndexByte(data, ')'); i >= 0 && i+2 < len(data) {
		return data[i+2] != 'Z'
	}
	return true
}

func (t *tray) RegisterStatusNotifierItem(service string, sender dbus.Sender) (err *dbus.Error) {
	// The service parameter can be either an object path ("/StatusNotifierItem")
	// or a bus name (":1.123"). When it's a bus name, use it as the destination
	// and default to "/StatusNotifierItem" as the object path (per the SNI spec).
	dest := string(sender)
	objPath := dbus.ObjectPath(service)
	if !strings.HasPrefix(service, "/") {
		dest = service
		objPath = "/StatusNotifierItem"
	}
	// Reject malformed paths — godbus would later log "invalid path name"
	// when we try to call methods on the proxy, with no way to clean up
	// the half-registered tray entry.
	if !objPath.IsValid() {
		log.Printf("[SYSTRAY] RegisterStatusNotifierItem: rejecting invalid object path %q from sender=%q", service, sender)
		return dbus.MakeFailedError(fmt.Errorf("invalid object path: %q", service))
	}
	log.Printf("[SYSTRAY] RegisterStatusNotifierItem: service=%q sender=%q dest=%q objPath=%q", service, sender, dest, objPath)
	ni := notifier.NewStatusNotifierItem(t.conn.Object(dest, objPath))

	t.lock.Lock()
	item, ok := t.nodes[sender]
	if !ok {
		var ico *multiButton
		ico = newMultiButton(func() {
			go t.activate(ni, dest)
		}, func() {
			go func() { // the app may be slow to answer: not on the Fyne thread
				ctx, cancel := callCtx()
				defer cancel()
				if m, err := ni.GetMenu(ctx); err == nil {
					fyne.Do(func() { t.showMenu(string(sender), m, ico) })
					return
				}

				// try secondary if primary not known
				_ = ni.ContextMenu(ctx, 5, 5)
			}()
		})
		ico.scroll = func(delta float32, horizontal bool) {
			direction := "vertical"
			if horizontal {
				direction = "horizontal"
			}
			go func() {
				ctx, cancel := callCtx()
				defer cancel()
				_ = ni.Scroll(ctx, int32(delta), direction)
			}()
		}

		ico.Importance = widget.LowImportance
		item = &node{ico: ico, ni: ni, pid: t.processID(sender)}
		t.nodes[sender] = item
		fyne.Do(func() {
			t.box.Add(ico)
		})
	}

	item.ni = ni
	t.lock.Unlock()

	icon := t.fetchIcon(item)
	fyne.Do(func() {
		item.ico.SetIcon(icon)
		t.box.Refresh()
	})

	return nil
}

func (t *tray) RegisterStatusNotifierHost(service string, sender dbus.Sender) (err *dbus.Error) {
	log.Println("Register Host", service, sender)

	// The signal Path is the OBJECT PATH from which the watcher emits — i.e.
	// /StatusNotifierWatcher, not the registering host's bus name (":1.2").
	// Wrapping the bus name in ObjectPath produced "dbus: invalid path name"
	// on every panel start because ":" is not legal in a path component.
	e := watcher.Emit(t.conn, &watcher.StatusNotifierWatcher_StatusNotifierHostRegisteredSignal{
		Path: dbus.ObjectPath(path),
		Body: &watcher.StatusNotifierWatcher_StatusNotifierHostRegisteredSignalBody{},
	})
	if e != nil {
		fyne.LogError("it was not emit the notification", e)
	}
	return nil
}

// trayCallTimeout bounds the calls to the apps behind the tray icons: one
// that hangs must not hang the panel.
const trayCallTimeout = time.Second

// callCtx is the context of the calls to a tray app: they give up after
// trayCallTimeout.
func callCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), trayCallTimeout)
}

func (t *tray) Metadata() tyde.ModuleMetadata {
	return trayMeta
}

func (t *tray) StatusAreaWidget() fyne.CanvasObject {
	return t.box
}

func (t *tray) parseMenu(parent int32, pos *fyne.Position, closer func()) fyne.CanvasObject {
	Y := pos.Y
	var items []*fyne.MenuItem
	ctx, cancel := callCtx()
	defer cancel()
	_, l, err := t.menu.GetLayout(ctx, parent, 1, nil)
	if err != nil {
		fyne.LogError("Tray menu", err)
	}
	for i, item := range l.V2 {
		// What a tray app sends is checked: a malformed menu must not crash the panel.
		data, ok := item.Value().([]interface{})
		if !ok || len(data) < 2 {
			continue
		}
		id, ok := data[0].(int32)
		if !ok {
			continue
		}
		items = append(items, t.parseMenuItem(id, t.menu, data[1], pos, i, closer))

		Y += theme.TextSize() + theme.Padding()*2
	}
	m := fyne.NewMenu("", items...)
	return widget.NewMenu(m)
}

func (t *tray) parseMenuItem(id int32, menu *menu.Dbusmenu, in interface{}, pos *fyne.Position, off int, closer func()) *fyne.MenuItem {
	ret := &fyne.MenuItem{}
	data, ok := in.(map[string]dbus.Variant)
	if !ok {
		ret.Disabled = true
		return ret
	}
	if ty, ok := data["type"]; ok {
		if ty.String() == "\"separator\"" {
			ret.IsSeparator = true
		}
	} else {
		ret.Label, _ = data["label"].Value().(string)
		if checkType, ok := data["toggle-type"]; ok && checkType.Value() == "checkmark" {
			if checkState, ok := data["toggle-state"]; ok {
				if state, ok := checkState.Value().(int32); ok && state > 0 {
					ret.Checked = true
				}
			}
		}
		ret.Action = func() {
			go func() {
				ctx, cancel := callCtx()
				defer cancel()
				err := menu.Event(ctx, id, "clicked", dbus.MakeVariant(id), uint32(time.Now().Unix()))
				if err != nil {
					fyne.LogError("Failed to message menu tap", err)
				}
			}()
			closer()
		}

		if s, ok := data["shortcut"]; ok {
			if short := parseShortcut(ret.Label, s); short != nil {
				ret.Shortcut = short
			}
		}
	}

	if i, ok := data["icon-data"]; ok {
		if png, ok := i.Value().([]byte); ok {
			ret.Icon = fyne.NewStaticResource(fmt.Sprintf("systray-icon-%d", id), png)
		}
	}
	if e, ok := data["enabled"]; ok && e.Value() == false {
		ret.Disabled = true
	}

	if t, ok := data["toggle-type"]; ok && t.String() == "\"checkmark\"" {
		if s, ok := data["toggle-state"]; ok && s.Value() == true {
			ret.Checked = true
		}
	}

	if s, ok := data["children-display"]; ok && s.String() == "\"submenu\"" {
		ret.Action = func() {
			w := fyne.CurrentApp().Driver().(deskDriver.Driver).CreateSplashWindow()
			w.SetOnClosed(closer)
			childPos := &fyne.Position{}

			w.SetContent(t.parseMenu(id, childPos, func() {
				w.Close()
				closer()
			}))

			size := w.Content().MinSize()
			w.Resize(size)
			sub := (*pos).AddXY(-size.Width, float32(off)*(18+theme.Padding()*4))
			screen := tyde.Instance().Screens().Primary()
			if sub.Y+size.Height > float32(screen.Height)/screen.CanvasScale() {
				sub.Y = float32(screen.Height)/screen.CanvasScale() - size.Height
			}
			childPos.X, childPos.Y = sub.X, sub.Y

			tyde.Instance().WindowManager().ShowOverlay(w, size, *childPos)
		}

		ret.ChildMenu = fyne.NewMenu("")
	}
	return ret
}

// activate brings the application behind a tray item forward on left click.
func (t *tray) activate(ni *notifier.StatusNotifierItem, dest string) {
	ctx, cancel := callCtx()
	defer cancel()
	if !wlipc.IsWaylandSession() {
		_ = ni.Activate(ctx, 5, 5)
		return
	}

	appID, _ := ni.GetId(ctx)
	wmClass := t.resolveWMClass(ni, appID, dest)
	log.Printf("[SYSTRAY] Left-click on tray item id=%q resolved=%q", appID, wmClass)

	// 1. Try IPC raise-by-class (for windows already mapped but behind others)
	if wmClass != "" {
		wlipc.RequestRaiseByClass(wmClass)
	}

	// 2. Call Activate via D-Bus (standard tray protocol)
	if err := ni.Activate(ctx, 5, 5); err != nil {
		log.Printf("[SYSTRAY] Activate failed: %v", err)
	}

	// 3. Launch the app executable as fallback.
	// For Electron apps (Slack, Discord), D-Bus Activate often does nothing.
	// Re-launching the app binary activates the existing instance via
	// Electron's single-instance lock.
	if wmClass != "" {
		go t.launchAppFallback(wmClass)
	}
}

// launchAppFallback starts the application of a tray icon again, through
// its desktop entry: for Electron apps (Slack, Discord), D-Bus Activate does
// nothing, and starting them again brings their running instance forward.
// Only an installed application whose name is the one the icon gives is
// started: the icon names itself, and must not choose what runs.
func (t *tray) launchAppFallback(wmClass string) {
	// Wait a bit to see if Activate/raiseByClass already worked
	time.Sleep(500 * time.Millisecond)

	desk := tyde.Instance()
	if desk == nil {
		return
	}
	app := icon.FindAppByName(wmClass, desk.IconProvider())
	if app == nil || !strings.EqualFold(app.Name(), wmClass) {
		log.Printf("[SYSTRAY] launchAppFallback: no application named %q", wmClass)
		return
	}
	log.Printf("[SYSTRAY] launchAppFallback: starting %q", app.Name())
	fyne.Do(func() {
		if err := desk.RunApp(app); err != nil {
			log.Printf("[SYSTRAY] launchAppFallback: failed: %v", err)
		}
	})
	time.Sleep(time.Second)
	wlipc.RequestRaiseByClass(wmClass)
}

// resolveWMClass tries to determine the real WM_CLASS for a tray item.
// Electron apps (Slack, Discord, etc.) report "chrome_status_icon_N" as their ID,
// which doesn't match the window's WM_CLASS. We extract the real app name from
// the icon theme path (e.g. "/run/user/1000/snap.slack/.org.chromium..." → "slack").
func (t *tray) resolveWMClass(ni *notifier.StatusNotifierItem, appID, dest string) string {
	ctx, cancel := callCtx()
	defer cancel()
	// If the ID doesn't look like a Chromium status icon, use it directly
	if !strings.HasPrefix(appID, "chrome_status_icon") {
		return appID
	}

	// Try to extract real app name from theme path
	// Snap: /run/user/1000/snap.slack/.org.chromium.Chromium.XXXXX
	// Flatpak: similar pattern with app name
	themePath, _ := ni.GetIconThemePath(ctx)
	if themePath != "" {
		// Look for "snap.<appname>" pattern
		if idx := strings.Index(themePath, "snap."); idx >= 0 {
			rest := themePath[idx+5:] // after "snap."
			if slashIdx := strings.IndexByte(rest, '/'); slashIdx > 0 {
				return rest[:slashIdx]
			}
			if dotIdx := strings.IndexByte(rest, '.'); dotIdx > 0 {
				return rest[:dotIdx]
			}
		}
		// Look for flatpak app ID pattern
		if idx := strings.Index(themePath, "flatpak"); idx >= 0 {
			parts := strings.Split(themePath, "/")
			for _, p := range parts {
				if strings.Contains(p, ".") && !strings.HasPrefix(p, ".") {
					// e.g. "com.slack.Slack" → use last segment
					segments := strings.Split(p, ".")
					return strings.ToLower(segments[len(segments)-1])
				}
			}
		}
		// .deb/native: theme path often contains app config dir
		// e.g. /home/user/.config/Slack/... → "Slack"
		if idx := strings.Index(themePath, "/.config/"); idx >= 0 {
			rest := themePath[idx+9:]
			if slashIdx := strings.IndexByte(rest, '/'); slashIdx > 0 {
				return strings.ToLower(rest[:slashIdx])
			}
		}
	}

	// Fallback: resolve process name from D-Bus sender PID
	if name := t.resolveProcessName(dest); name != "" {
		return name
	}

	// Fallback: try the icon name (sometimes reveals the app)
	iconName, _ := ni.GetIconName(ctx)
	if iconName != "" && !strings.HasPrefix(iconName, "status_icon") {
		return iconName
	}

	return appID
}

// resolveProcessName uses D-Bus to find the sender's PID and reads /proc/<pid>/cmdline
func (t *tray) resolveProcessName(dest string) string {
	var pid uint32
	err := t.conn.BusObject().Call("org.freedesktop.DBus.GetConnectionUnixProcessID", 0, dest).Store(&pid)
	if err != nil || pid == 0 {
		return ""
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil || len(data) == 0 {
		return ""
	}
	// cmdline is null-separated; first element is the binary path
	cmdline := string(data)
	if idx := strings.IndexByte(cmdline, 0); idx > 0 {
		cmdline = cmdline[:idx]
	}
	// Extract just the binary name from the path
	name := filepath.Base(cmdline)
	log.Printf("[SYSTRAY] resolveProcessName: pid=%d cmd=%q name=%q", pid, cmdline, name)
	return strings.ToLower(name)
}

func (t *tray) showMenu(sender string, name dbus.ObjectPath, from fyne.CanvasObject) {
	pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(from)
	w := fyne.CurrentApp().Driver().(deskDriver.Driver).CreateSplashWindow()
	t.menu = menu.NewDbusmenu(t.conn.Object(sender, name))
	w.SetContent(t.parseMenu(0, &pos, func() {
		w.Close()
	}))

	size := w.Content().MinSize()
	if size.IsZero() { // empty menu - weird but don't crash
		size = fyne.NewSquareSize(1)
	}
	w.Resize(size)

	pos.X -= size.Width
	desk := tyde.Instance()
	if desk == nil {
		return
	}
	screen := desk.Screens().Primary()
	if pos.Y+size.Height > float32(screen.Height)/screen.CanvasScale() {
		pos.Y = float32(screen.Height)/screen.CanvasScale() - size.Height
	}
	desk.WindowManager().ShowOverlay(w, size, pos)
}

// firstImage returns the first well-formed icon of those a tray app sent.
func firstImage(icons []struct {
	V0 int32
	V1 int32
	V2 []byte
},
) image.Image {
	for _, ic := range icons {
		if img := pixelsToImage(ic); img != nil {
			return img
		}
	}
	return nil
}

func (t *tray) fetchIcon(i *node) fyne.Resource {
	ctx, cancel := callCtx()
	defer cancel()
	// Try 1: Raw pixel data from the app
	ic, _ := i.ni.GetIconPixmap(ctx)
	if img := firstImage(ic); img != nil {
		unique := strconv.Itoa(resourceID) + ".png"
		resourceID++
		w := &bytes.Buffer{}
		_ = png.Encode(w, img)
		return fyne.NewStaticResource(unique, w.Bytes())
	}

	// Try 2: Icon name + optional theme path from the app
	name, _ := i.ni.GetIconName(ctx)
	themePath, _ := i.ni.GetIconThemePath(ctx)

	fullPath := ""
	if name != "" {
		// Try custom theme path first (if the app provides one)
		if themePath != "" {
			fullPath = filepath.Join(themePath, name+".png")
			if _, err := os.Stat(fullPath); err != nil {
				fullPath = appie.FdoLookupIconPathInTheme("64", filepath.Join(themePath, "hicolor"), "", name)
			}
		}
		// Fall back to system-wide icon lookup
		if fullPath == "" {
			fullPath = appie.FdoLookupIconPath("", 64, name)
		}
		// Fall back to Snap/Flatpak icon locations not covered by XDG_DATA_DIRS
		if fullPath == "" {
			fullPath = lookupIconExtraPaths(name)
		}
	}

	if fullPath != "" {
		img, err := os.ReadFile(fullPath)
		if err == nil {
			return fyne.NewStaticResource(name, img)
		}
		fyne.LogError("Failed to read status icon file", err)
	}

	// Try 3: Look up icon from .desktop files using icon name and app ID
	if desk := tyde.Instance(); desk != nil {
		appID, _ := i.ni.GetId(ctx)
		lookupNames := []string{}
		if name != "" {
			lookupNames = append(lookupNames, strings.ToLower(name))
		}
		if appID != "" && !strings.EqualFold(appID, name) {
			lookupNames = append(lookupNames, strings.ToLower(appID))
		}
		for _, lookupName := range lookupNames {
			apps := desk.IconProvider().FindAppsMatching(lookupName)
			if len(apps) > 0 {
				if res := apps[0].Icon("", 64); res != nil {
					return res
				}
			}
			// Also try extra paths with the app ID (e.g. "slack" for snap)
			if fp := lookupIconExtraPaths(lookupName); fp != "" {
				if img, err := os.ReadFile(fp); err == nil {
					return fyne.NewStaticResource(lookupName, img)
				}
			}
		}
	}

	log.Printf("[SYSTRAY] No icon found for tray item (name=%q, themePath=%q)", name, themePath)
	return wmtheme.BrokenImageIcon
}

// lookupIconExtraPaths searches for icons in Snap, Flatpak, and other non-standard locations
// that may not be covered by XDG_DATA_DIRS.
func lookupIconExtraPaths(name string) string {
	extensions := []string{".png", ".svg", ".xpm"}
	extraDirs := []string{
		"/snap/" + name + "/current/usr/share/pixmaps",
		"/snap/" + name + "/current/usr/share/icons/hicolor/512x512/apps",
		"/snap/" + name + "/current/usr/share/icons/hicolor/256x256/apps",
		"/snap/" + name + "/current/usr/share/icons/hicolor/128x128/apps",
		"/snap/" + name + "/current/usr/share/icons/hicolor/64x64/apps",
		"/snap/" + name + "/current/usr/share/icons/hicolor/48x48/apps",
		"/snap/" + name + "/current/usr/share/icons/hicolor/scalable/apps",
		"/var/lib/flatpak/exports/share/icons/hicolor/512x512/apps",
		"/var/lib/flatpak/exports/share/icons/hicolor/256x256/apps",
		"/var/lib/flatpak/exports/share/icons/hicolor/128x128/apps",
		"/var/lib/flatpak/exports/share/icons/hicolor/64x64/apps",
		"/var/lib/flatpak/exports/share/icons/hicolor/scalable/apps",
	}

	// Also check user Flatpak location
	if home, err := os.UserHomeDir(); err == nil {
		flatpakBase := filepath.Join(home, ".local/share/flatpak/exports/share/icons/hicolor")
		for _, size := range []string{"512x512", "256x256", "128x128", "64x64", "scalable"} {
			extraDirs = append(extraDirs, filepath.Join(flatpakBase, size, "apps"))
		}
	}

	for _, dir := range extraDirs {
		for _, ext := range extensions {
			path := filepath.Join(dir, name+ext)
			if _, err := os.Stat(path); err == nil {
				return path
			}
		}
	}
	return ""
}

func createPropSpec() map[string]map[string]*prop.Prop {
	return map[string]map[string]*prop.Prop{
		"org.kde.StatusNotifierWatcher": {
			"RegisteredStatusNotifierItems": {
				Value:    []string{},
				Writable: false,
				Emit:     prop.EmitTrue,
				Callback: nil,
			},
			"IsStatusNotifierHostRegistered": {
				Value:    true,
				Writable: false,
				Emit:     prop.EmitTrue,
				Callback: nil,
			},
			"ProtocolVersion": {
				Value:    int32(25),
				Writable: false,
				Emit:     prop.EmitTrue,
				Callback: nil,
			},
		},
	}
}

type img struct {
	w, h int
	data []byte
}

func (i *img) ColorModel() color.Model {
	return color.NRGBAModel
}

func (i *img) Bounds() image.Rectangle {
	return image.Rect(0, 0, i.w, i.h)
}

func (i *img) At(x, y int) color.Color {
	off := (y*i.w + x) * 4

	a, r, g, b := i.data[off], i.data[off+1], i.data[off+2], i.data[off+3]

	return color.NRGBA{r, g, b, a}
}

// maxTrayIconSide bounds the size of an icon a tray app sends.
const maxTrayIconSide = 1024

// pixelsToImage reads an ARGB32 icon a tray app sent; nil if its size and
// its data do not agree.
func pixelsToImage(in struct {
	V0 int32
	V1 int32
	V2 []byte
},
) image.Image {
	w, h := int(in.V0), int(in.V1)
	if w <= 0 || h <= 0 || w > maxTrayIconSide || h > maxTrayIconSide || len(in.V2) < w*h*4 {
		return nil
	}
	return &img{w, h, in.V2}
}
