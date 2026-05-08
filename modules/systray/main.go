//Note that you need to have github.com/knightpp/dbus-codegen-go installed from "custom" branch
//go:generate dbus-codegen-go -prefix org.kde -package notifier -output generated/notifier/status_notifier_item.go StatusNotifierItem.xml
//go:generate dbus-codegen-go -prefix org.kde -package watcher -output generated/watcher/status_notifier_watcher.go StatusNotifierWatcher.xml
//go:generate dbus-codegen-go -prefix com.canonical -package menu -output generated/menu/dbus_menu.go DbusMenu.xml

// Package systray implements a system tray module using the D-Bus StatusNotifierItem protocol for application indicators.
package systray

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/FyshOS/appie"
	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"

	"fyshos.com/fynedesk"
	"fyshos.com/fynedesk/modules/systray/generated/menu"
	"fyshos.com/fynedesk/modules/systray/generated/notifier"
	"fyshos.com/fynedesk/modules/systray/generated/watcher"
	wmtheme "fyshos.com/fynedesk/theme"
	"fyshos.com/fynedesk/wlipc"

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
	fynedesk.RegisterModule(trayMeta)
}

var trayMeta = fynedesk.ModuleMetadata{
	Name:        "SystemTray",
	NewInstance: NewTray,
}

type tray struct {
	conn   *dbus.Conn
	menu   *menu.Dbusmenu
	signal chan *dbus.Signal

	box     *fyne.Container
	nodesMu sync.RWMutex
	nodes   map[dbus.Sender]*node
}

type node struct {
	ico *trayIcon
	ni  *notifier.StatusNotifierItem
}

// trayIcon is a clickable icon widget that supports both left-click (Tapped)
// and right-click (TappedSecondary) for the system tray.
type trayIcon struct {
	widget.BaseWidget
	icon          fyne.Resource
	onTapped      func()
	onRightTapped func()
}

func newTrayIcon(onTapped, onRightTapped func()) *trayIcon {
	ti := &trayIcon{onTapped: onTapped, onRightTapped: onRightTapped}
	ti.ExtendBaseWidget(ti)
	return ti
}

func (ti *trayIcon) Tapped(*fyne.PointEvent) {
	if ti.onTapped != nil {
		ti.onTapped()
	}
}

func (ti *trayIcon) TappedSecondary(*fyne.PointEvent) {
	if ti.onRightTapped != nil {
		ti.onRightTapped()
	}
}

func (ti *trayIcon) SetIcon(res fyne.Resource) {
	ti.icon = res
	ti.Refresh()
}

func (ti *trayIcon) CreateRenderer() fyne.WidgetRenderer {
	img := widget.NewIcon(ti.icon)
	return &trayIconRenderer{icon: img, ti: ti}
}

type trayIconRenderer struct {
	icon *widget.Icon
	ti   *trayIcon
}

func (r *trayIconRenderer) Layout(size fyne.Size) {
	// Render icon at IconInlineSize (20dp) centered, matching widget.Button icon size.
	// This avoids upscaling small tray icons (typically 16-24px from Electron/Chromium).
	iconSz := theme.IconInlineSize()
	r.icon.Resize(fyne.NewSquareSize(iconSz))
	r.icon.Move(fyne.NewPos((size.Width-iconSz)/2, (size.Height-iconSz)/2))
}
func (r *trayIconRenderer) MinSize() fyne.Size {
	return fyne.NewSize(wmtheme.NarrowBarWidth, wmtheme.NarrowBarWidth)
}
func (r *trayIconRenderer) Refresh()                     { r.icon.SetResource(r.ti.icon); r.icon.Refresh() }
func (r *trayIconRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.icon} }
func (r *trayIconRenderer) Destroy()                     {}

// NewTray creates a new module that will show a system tray in the status area
func NewTray() fynedesk.Module {
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

	// TODO this is create watcher (optional)
	err = conn.ExportAll(t, path, "org.kde.StatusNotifierWatcher")
	if err != nil {
		fyne.LogError("Unable to register watcher", err)
		return t
	}

	_, err = conn.RequestName("org.kde.StatusNotifierWatcher", dbus.NameFlagDoNotQueue)
	if err != nil {
		log.Println("Failed to claim notifier watcher name", err)
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
	// End TODO

	hostErr := t.RegisterStatusNotifierHost(conn.Names()[0], "")
	if hostErr != nil {
		fyne.LogError("Failed to register our host with the notifier watcher, maybe no watcher running? %v", hostErr)
		return t
	}

	watchErr := t.conn.AddMatchSignal(dbus.WithMatchInterface("org.freedesktop.DBus"), dbus.WithMatchObjectPath("/org/freedesktop/DBus"))
	_ = t.conn.AddMatchSignal(dbus.WithMatchInterface("org.kde.StatusNotifierItem"))
	if watchErr != nil {
		fyne.LogError("Failed to monitor systray name loss", watchErr)
		return t
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
					sender := dbus.Sender(name.(string))
					t.nodesMu.Lock()
					item, ok := t.nodes[sender]
					if ok {
						delete(t.nodes, sender)
					}
					t.nodesMu.Unlock()
					if ok {
						t.box.Remove(item.ico)
						t.box.Refresh()
					}
				}
			case "org.kde.StatusNotifierItem.NewIcon":
				t.nodesMu.RLock()
				item, ok := t.nodes[dbus.Sender(v.Sender)]
				t.nodesMu.RUnlock()
				if ok {
					t.updateIcon(item)
				}
			default:
				log.Println("Also", v.Name)
				continue
			}
		}
	}()

	return t
}

func (t *tray) Destroy() {
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

	t.nodesMu.Lock()
	item, ok := t.nodes[sender]
	if !ok {
		var ico *trayIcon
		ico = newTrayIcon(
			// Left click: activate/restore the application
			func() {
				appID, _ := ni.GetId(t.conn.Context())
				wmClass := t.resolveWMClass(ni, appID, dest)
				log.Printf("[SYSTRAY] Left-click on tray item id=%q resolved=%q", appID, wmClass)

				// 1. Try IPC raise-by-class (for windows already mapped but behind others)
				if wmClass != "" {
					wlipc.RequestRaiseByClass(wmClass)
				}

				// 2. Call Activate via D-Bus (standard tray protocol)
				err := ni.Activate(t.conn.Context(), 5, 5)
				if err != nil {
					log.Printf("[SYSTRAY] Activate failed: %v", err)
				}

				// 3. Launch the app executable as fallback.
				// For Electron apps (Slack, Discord), D-Bus Activate often does nothing.
				// Re-launching the app binary activates the existing instance via
				// Electron's single-instance lock.
				if wmClass != "" {
					go t.launchAppFallback(wmClass)
				}
			},
			// Right click: show context menu
			func() {
				if m, err := ni.GetMenu(t.conn.Context()); err == nil {
					t.showMenu(string(sender), m, ico)
				}
			},
		)
		item = &node{ico, ni}
		t.nodes[sender] = item
		t.box.Add(ico)
	}

	t.nodes[sender].ni = ni
	t.nodesMu.Unlock()
	t.updateIcon(item)
	t.box.Refresh()

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

func (t *tray) Metadata() fynedesk.ModuleMetadata {
	return trayMeta
}

func (t *tray) StatusAreaWidget() fyne.CanvasObject {
	return t.box
}

func (t *tray) parseMenu(parent int32, pos *fyne.Position, closer func()) fyne.CanvasObject {
	Y := pos.Y
	var items []*fyne.MenuItem
	_, l, _ := t.menu.GetLayout(t.conn.Context(), parent, 1, nil)
	for i, item := range l.V2 {
		data := item.Value().([]interface{})
		items = append(items, t.parseMenuItem(data[0].(int32), t.menu, data[1], pos, i, closer))

		Y += theme.TextSize() + theme.Padding()*2
	}
	m := fyne.NewMenu("", items...)
	return widget.NewMenu(m)
}

func (t *tray) parseMenuItem(id int32, menu *menu.Dbusmenu, in interface{}, pos *fyne.Position, off int, closer func()) *fyne.MenuItem {
	data := in.(map[string]dbus.Variant)
	ret := &fyne.MenuItem{}
	if ty, ok := data["type"]; ok {
		if ty.String() == "\"separator\"" {
			ret.IsSeparator = true
		}
	} else {
		ret.Label = fmt.Sprintf("%s", data["label"].Value())
		if checkType, ok := data["toggle-type"]; ok && checkType.Value() == "checkmark" {
			if checkState, ok := data["toggle-state"]; ok && checkState.Value().(int32) > 0 {
				ret.Checked = true
			}
		}
		ret.Action = func() {
			err := menu.Event(t.conn.Context(), id, "clicked", dbus.MakeVariant(id), uint32(time.Now().Unix()))
			if err != nil {
				fyne.LogError("Failed to message menu tap", err)
			}
			closer()
		}
	}

	if i, ok := data["icon-data"]; ok {
		ret.Icon = fyne.NewStaticResource(fmt.Sprintf("systray-icon-%d", id), i.Value().([]byte))
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
			screen := fynedesk.Instance().Screens().Primary()
			if sub.Y+size.Height > float32(screen.Height)/screen.CanvasScale() {
				sub.Y = float32(screen.Height)/screen.CanvasScale() - size.Height
			}
			childPos.X, childPos.Y = sub.X, sub.Y

			fynedesk.Instance().WindowManager().ShowOverlay(w, size, *childPos)
		}

		ret.ChildMenu = fyne.NewMenu("")
	}
	return ret
}

// launchAppFallback tries to launch an app by its WM_CLASS name.
// This is a fallback for Electron apps where D-Bus Activate() does nothing.
// Re-launching the app binary activates the existing instance via single-instance lock.
func (t *tray) launchAppFallback(wmClass string) {
	// Wait a bit to see if Activate/raiseByClass already worked
	time.Sleep(500 * time.Millisecond)

	// Launch the app binary — for single-instance apps (Electron/Slack/Discord),
	// this activates the running instance instead of starting a new one.
	// Validate executable exists in PATH before running.
	binName := strings.ToLower(wmClass)
	binPath, err := exec.LookPath(binName)
	if err != nil {
		log.Printf("[SYSTRAY] launchAppFallback: %q not found in PATH", binName)
		return
	}
	cmd := exec.Command(binPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	log.Printf("[SYSTRAY] launchAppFallback: launching %q", cmd.Path)
	if err := cmd.Start(); err != nil {
		log.Printf("[SYSTRAY] launchAppFallback: failed: %v", err)
		return
	}

	// Wait up to 1s for the process to either run (single-instance hand-off,
	// keeps running) or exit cleanly (single-instance forwarder that quits
	// after activating the existing app). If it exits with a non-zero status,
	// the launch failed and raising would target a stale window.
	exitCh := make(chan error, 1)
	go func() { exitCh <- cmd.Wait() }()
	select {
	case err := <-exitCh:
		if err != nil {
			log.Printf("[SYSTRAY] launchAppFallback: process exited with error, skipping raise: %v", err)
			return
		}
	case <-time.After(1 * time.Second):
		// Still running — likely a normal app, fall through to raise
	}
	wlipc.RequestRaiseByClass(wmClass)
}

// resolveWMClass tries to determine the real WM_CLASS for a tray item.
// Electron apps (Slack, Discord, etc.) report "chrome_status_icon_N" as their ID,
// which doesn't match the window's WM_CLASS. We extract the real app name from
// the icon theme path (e.g. "/run/user/1000/snap.slack/.org.chromium..." → "slack").
func (t *tray) resolveWMClass(ni *notifier.StatusNotifierItem, appID, dest string) string {
	// If the ID doesn't look like a Chromium status icon, use it directly
	if !strings.HasPrefix(appID, "chrome_status_icon") {
		return appID
	}

	// Try to extract real app name from theme path
	// Snap: /run/user/1000/snap.slack/.org.chromium.Chromium.XXXXX
	// Flatpak: similar pattern with app name
	themePath, _ := ni.GetIconThemePath(t.conn.Context())
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
	iconName, _ := ni.GetIconName(t.conn.Context())
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
	w.Resize(size)

	pos.X -= size.Width
	desk := fynedesk.Instance()
	if desk == nil {
		return
	}
	screen := desk.Screens().Primary()
	if pos.Y+size.Height > float32(screen.Height)/screen.CanvasScale() {
		pos.Y = float32(screen.Height)/screen.CanvasScale() - size.Height
	}
	desk.WindowManager().ShowOverlay(w, size, pos)
}

func (t *tray) updateIcon(i *node) {
	// Log what the tray item reports for debugging icon lookup issues.
	dbgName, _ := i.ni.GetIconName(t.conn.Context())
	dbgID, _ := i.ni.GetId(t.conn.Context())
	dbgTheme, _ := i.ni.GetIconThemePath(t.conn.Context())
	log.Printf("[SYSTRAY] updateIcon: id=%q iconName=%q themePath=%q", dbgID, dbgName, dbgTheme)

	// Try 1: Raw pixel data from the app
	ic, _ := i.ni.GetIconPixmap(t.conn.Context())
	if len(ic) > 0 {
		img := pixelsToImage(ic[0])
		unique := strconv.Itoa(resourceID) + ".png"
		resourceID++
		w := &bytes.Buffer{}
		_ = png.Encode(w, img)
		i.ico.SetIcon(fyne.NewStaticResource(unique, w.Bytes()))
		return
	}

	// Try 2: Icon name + optional theme path from the app
	name, _ := i.ni.GetIconName(t.conn.Context())
	themePath, _ := i.ni.GetIconThemePath(t.conn.Context())

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
			i.ico.SetIcon(fyne.NewStaticResource(name, img))
			return
		}
		fyne.LogError("Failed to read status icon file", err)
	}

	// Try 3: Look up icon from .desktop files using icon name and app ID
	if desk := fynedesk.Instance(); desk != nil {
		appID, _ := i.ni.GetId(t.conn.Context())
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
				res := apps[0].Icon("", 64)
				if res != nil {
					i.ico.SetIcon(res)
					return
				}
			}
			// Also try extra paths with the app ID (e.g. "slack" for snap)
			if fullPath == "" {
				if fp := lookupIconExtraPaths(lookupName); fp != "" {
					if img, err := os.ReadFile(fp); err == nil {
						i.ico.SetIcon(fyne.NewStaticResource(lookupName, img))
						return
					}
				}
			}
		}
	}

	log.Printf("[SYSTRAY] No icon found for tray item (name=%q, themePath=%q)", name, themePath)
	i.ico.SetIcon(wmtheme.BrokenImageIcon)
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

func pixelsToImage(in struct {
	V0 int32
	V1 int32
	V2 []byte
}) image.Image {
	return &img{int(in.V0), int(in.V1), in.V2}
}
