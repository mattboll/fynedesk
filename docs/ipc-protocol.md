# FyneDesk IPC Protocol

FyneDesk uses a hybrid IPC system for communication between the compositor (`fynedesk-compositor`) and the panel (`fynedesk-panel`).

## Transport Mechanisms

### 1. Socket IPC (primary)

**Path**: `/run/user/$UID/fynedesk.sock` (UNIX domain socket)

**Protocol**: JSON-line — each message is a JSON object terminated by `\n`.

**Envelope**:

```json
{
  "type": "event|request|response",
  "id": 123,
  "name": "message-name",
  "data": {}
}
```

- `type`: `"event"` (push), `"request"` (client→compositor), or `"response"` (reply)
- `id`: correlation ID for request/response pairs (omitted for events)
- `name`: message name or status (`"ok"`, `"error"`)
- `data`: payload object (omitted when empty)

### 2. File-based IPC (fallback)

**Directory**: `~/.config/fynedesk/`

All writes use **atomic rename** (write to temp file, then `os.Rename`) to prevent readers from seeing truncated data. Files are polled every **100ms**.

Request files (panel writes, compositor reads) are **deleted after processing** for one-time delivery. State files (compositor writes, panel reads) persist and are overwritten on change.

All `Request*()` functions in `wlipc/` try the socket first, then fall back to file-based IPC transparently.

---

## Socket Events (compositor → clients)

Clients subscribe via the `subscribe` request. Events are pushed in real-time.

| Event | Payload | Description |
|-------|---------|-------------|
| `windows-state` | `WindowsState` | Window list changed |
| `desktop-state` | `DesktopState` | Active desktop or count changed |
| `notification` | `DBusNotification` | D-Bus notification received |
| `screenshot` | `ScreenshotEvent` | Screenshot saved |
| `clipboard-history` | `ClipboardHistory` | Clipboard entry added/removed |
| `keyboard-layout` | `KeyboardLayoutState` | Layout list or active layout changed |
| `volume-change` | `{timestamp}` | Volume key pressed |
| `brightness-change` | `{timestamp}` | Brightness key pressed |
| `launcher-request` | `{timestamp}` | Show app launcher (Super+Space) |
| `emoji-picker` | `EmojiPickerRequest` | Show emoji picker at position |
| `context-menu` | `ContextMenuRequest` | Right-click context menu |
| `clipboard-show` | `{timestamp}` | Show clipboard manager |
| `command-palette` | `{timestamp}` | Show command palette |
| `sidebar-toggle` | `{timestamp}` | Toggle sidebar (Raven) |
| `overview` | `{timestamp}` | Window overview mode |

## Socket Requests (clients → compositor)

| Request | Payload | Response | Description |
|---------|---------|----------|-------------|
| `subscribe` | `{events: [...]}` | `ok` | Register for events |
| `window-action` | `WindowActionRequest` | `ok/error` | Focus, close, minimize, etc. |
| `desktop-switch` | `{desktop: N}` | `ok/error` | Switch virtual desktop |
| `settings-changed` | `SettingsChanged` | `ok/error` | Notify prefs update |
| `layout-request` | `LayoutRequest` | `ok/error` | Reposition/mirror monitors |
| `keyboard-layout` | `{index: N}` | `ok/error` | Switch keyboard layout |
| `emoji-paste` | `{emoji: "..."}` | `ok/error` | Set clipboard to emoji |
| `clipboard-paste` | `{text: "..."}` | `ok/error` | Set clipboard to text |
| `clipboard-clear` | `{timestamp}` | `ok/error` | Clear clipboard history |
| `overlay` | `OverlayRequest` | `ok/error` | Position overlay window |
| `lock` | `{}` | `ok/error` | Lock screen |
| `logout` | `{}` | `ok/error` | End session |
| `restart` | `{}` | `ok/error` | Restart compositor |
| `list-windows` | `{}` | `WindowsState` | Synchronous window list |
| `get-desktop` | `{}` | `DesktopState` | Synchronous desktop state |
| `compositor-action` | `{action: "..."}` | `ok/error` | Dispatch named action |
| `window-preview` | `{window_id: "..."}` | base64 PNG | Get window thumbnail |

---

## File-Based IPC Reference

### State Files (compositor → panel, persistent)

#### desktop-state.json

```json
{"current": 0, "num_desks": 4}
```

Active virtual desktop index and total count.

#### windows-state.json

```json
{
  "windows": [
    {
      "id": "xdg-0",
      "title": "Terminal",
      "app_id": "foot",
      "desktop": 0,
      "output": "eDP-1",
      "focused": true,
      "iconic": false,
      "maximized": false,
      "fullscreened": false,
      "pinned": false,
      "is_panel": false,
      "x": 100.0, "y": 50.0,
      "w": 800.0, "h": 600.0
    }
  ],
  "timestamp": 1700000000000
}
```

Window IDs use `xdg-N` (XDG Shell) or `xway-N` (XWayland). Batched: compositor marks dirty on change, flushes once per frame.

#### keyboard-layout-state.json

```json
{
  "active_index": 0,
  "layouts": [
    {"layout": "us", "variant": "", "display_name": "English (US)"},
    {"layout": "fr", "variant": "bepo", "display_name": "French (BEPO)"}
  ],
  "timestamp": 1700000000000
}
```

#### clipboard-history.json

```json
{
  "entries": [
    {"text": "hello world", "timestamp": 1700000000001}
  ],
  "timestamp": 1700000000002
}
```

Persisted to disk for cross-session restoration.

### Request Files (panel → compositor, one-time)

| File | Writer | Structure | Purpose |
|------|--------|-----------|---------|
| `desktop-request.json` | `wlipc.RequestDesktopSwitch(N)` | `{desktop: N}` | Switch desktop |
| `window-action-request.json` | `wlipc.RequestWindowAction(id, action)` | see below | Window operation |
| `settings-changed.json` | `wlipc.NotifySettingsChanged(prefs)` | see below | Config update |
| `keyboard-layout-request.json` | `wlipc.RequestKeyboardLayout(N)` | `{index: N, timestamp: T}` | Switch layout |
| `mode-request.json` | `wlipc.RequestModeChange(idx, output)` | `{mode_index: N, output_name: ""}` | Display resolution |
| `scale-request.json` | `wlipc.RequestScaleChange(s, output)` | `{scale: 1.5, output_name: ""}` | Display scale |
| `layout-request.json` | `wlipc.RequestOutputLayout(req)` | see below | Monitor positioning |
| `overlay-request.json` | `wlipc.RequestOverlayPosition(...)` | `{title, x, y, width, height}` | Place popup window |
| `lock-request.json` | `wlipc.RequestLock()` | `{timestamp: T}` | Lock screen |
| `logout-request.json` | `wlipc.RequestLogout()` | `{timestamp: T}` | End session |
| `restart-request.json` | `wlipc.RequestRestart()` | `{timestamp: T}` | Restart compositor |
| `emoji-paste.json` | `wlipc.RequestEmojiPaste(emoji)` | `{emoji: "...", timestamp: T}` | Paste emoji |
| `clipboard-paste.json` | `wlipc.RequestClipboardPaste(text)` | `{text: "...", timestamp: T}` | Paste text |

#### Window Action Request

```json
{
  "window_id": "xdg-0",
  "action": "focus",
  "desktop": 0
}
```

Actions: `focus`, `close`, `iconify`, `uniconify`, `maximize`, `unmaximize`, `raise`, `set_desktop`, `pin`, `unpin`, `fullscreen`, `unfullscreen`.

#### Settings Changed

```json
{
  "timestamp": 1700000000000,
  "prefs": {
    "background": "/path/to/bg.jpg",
    "background_type": "image",
    "keyboardmodifier": 133,
    "naturalscroll": false,
    "nightlightenabled": true,
    "nightlighttemperature": 4500,
    "keybindings": "{\"quit\": [{\"key\": \"Escape\", \"mods\": [\"Alt\"]}]}"
  }
}
```

Includes a snapshot of Fyne preferences so the compositor doesn't need to re-read the prefs file.

#### Layout Request (multi-monitor)

```json
{
  "output_name": "HDMI-1",
  "position": "right",
  "relative_to": "eDP-1",
  "primary": false
}
```

Position values: `left`, `right`, `above`, `below`, `mirror`.

### Event Files (compositor → panel, one-time)

| File | Writer | Structure | Purpose |
|------|--------|-----------|---------|
| `launcher-request.json` | Compositor | `{timestamp: T}` | Show app launcher |
| `emoji-picker-request.json` | Compositor | `{x, y, timestamp}` | Show emoji picker |
| `context-menu-request.json` | Compositor | `{window_id, title, x, y}` | Right-click menu |
| `clipboard-show-request.json` | Compositor | `{timestamp: T}` | Show clipboard manager |
| `screenshot-event.json` | Compositor | `{file_path, timestamp}` | Screenshot taken |
| `volume-event.json` | Compositor | `{timestamp: T}` | Volume changed |
| `brightness-event.json` | Compositor | `{timestamp: T}` | Brightness changed |
| `dbus-notification.json` | Compositor | `{title, body, timeout, timestamp}` | D-Bus notification |

---

## Configuration

### config.toml

**Path**: `~/.config/fynedesk/config.toml`

```toml
[display]
background = "/path/to/wallpaper.jpg"
background_type = "image"  # image, matrix, starfield
icon_theme = "hicolor"
border_button_position = "Left"

[clock]
format = "24h"
show_seconds = false

[launcher]
icons = ["firefox", "foot"]
icon_size = 48
disable_taskbar = false
disable_zoom = false
zoom_scale = 2.0
narrow_left = true
bar_position = "left"  # left, bottom

[panel]
narrow_widget = false

[input]
keyboard_modifier = "Super"  # Super, Alt
natural_scroll = false
keyboard_layouts = ["fr:bepo_afnor", "us:"]

[night_light]
enabled = false
temperature = 4500  # Kelvin, 2700-6500

[screensaver]
type = "FyshOS"  # FyshOS, XScreensaver
show_clock = true
label = "FyneDesk"

[modules]
enabled = ["Sound", "Battery", "Virtual Desktops"]

[theme]
name = "default"
[theme.colors]
fynedeskTitlebarActive = "#2196f3"
```

### compositor-state.json (read-only)

Written by compositor on startup and resolution changes. Read by settings UI to populate display options.

```json
{
  "outputs": [
    {
      "output_name": "eDP-1",
      "modes": [
        {"index": 0, "width": 2560, "height": 1600, "refresh_rate": 60, "current": true},
        {"index": 1, "width": 1920, "height": 1200, "refresh_rate": 60, "custom": true, "aspect_ratio": "16:10"}
      ],
      "phys_width": 345,
      "phys_height": 215,
      "scale": 1.0,
      "width": 2560, "height": 1600,
      "x": 0, "y": 0,
      "primary": true
    }
  ]
}
```

---

## Implementation Notes

### Atomic Write Pattern

```go
tmp, _ := os.CreateTemp(dir, ".ipc-*")
tmp.Write(data)
tmp.Close()
os.Rename(tmp.Name(), targetPath)  // atomic on same filesystem
```

### Socket-First Fallback

```go
func RequestDesktopSwitch(desktop int) error {
    if trySendRequest(ReqDesktopSwitch, req) {
        return nil  // socket succeeded
    }
    // fall back to file
    return atomicWriteFile(path, data)
}
```

### Batching

Window state writes are batched: `writeWindowsState()` sets a dirty flag, `flushWindowsState()` writes once per render frame. This prevents flooding during rapid window changes.

### File Deletion After Read

Request files are deleted after successful JSON parsing. On parse error, the file is kept for retry on next poll (may have been a partial write).

---

## Debugging

```bash
# Watch IPC files
watch -n 0.1 'ls -la ~/.config/fynedesk/*.json 2>/dev/null'

# Read current state
cat ~/.config/fynedesk/windows-state.json | jq .
cat ~/.config/fynedesk/desktop-state.json | jq .

# Monitor compositor logs
journalctl --user -u fynedesk-compositor -f
```
