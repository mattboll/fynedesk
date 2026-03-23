# Configuration

FyneDesk stores its configuration in `~/.config/fynedesk/config.toml`. Most settings can be changed through the graphical Settings panel (right-click desktop > Settings, or Super+A > Settings).

## Configuration file

```toml
# ~/.config/fynedesk/config.toml

[appearance]
background = "~/Pictures/wallpaper.jpg"
background_type = "image"       # image | matrix | starfield
clock_format = "24h"            # 24h | 12h
icon_theme = "Papirus"

[layout]
bar_position = "left"           # left | bottom
narrow_bar = true               # Narrow vertical bar (36px)
narrow_widget = false           # Narrow widget panel
natural_scroll = true
border_buttons = "right"        # left | right (titlebar button position)

[keyboard]
modifier = "Super"              # Super | Alt (WM modifier key)
layouts = ["us", "fr:bepo_afnor"]

[keybindings]
quit = "Alt+Escape"
open_terminal = "Super+t"
show_launcher = "Super+space"
# Full list: see docs/keybindings.md
```

## Settings UI

The settings panel provides graphical access to all configuration:

- **Appearance** — Wallpaper, icon theme, clock format
- **Layout** — Bar position (left/bottom), narrow mode, titlebar buttons
- **Display** — Resolution, scaling, multi-monitor arrangement
- **Keyboard** — Modifier key, layouts, all keybindings
- **Theme** — Titlebar colors, panel colors, accent color

## IPC Socket

FyneDesk exposes a UNIX socket at `$XDG_RUNTIME_DIR/fynedesk.sock` for scripting.

### Using fynedesk-ctl

```bash
fynedesk-ctl windows              # List all windows
fynedesk-ctl windows --json       # JSON output
fynedesk-ctl desktop              # Show current desktop
fynedesk-ctl focus xdg-3          # Focus a window
fynedesk-ctl close xdg-3          # Close a window
fynedesk-ctl minimize xdg-3       # Minimize
fynedesk-ctl maximize xdg-3       # Maximize
fynedesk-ctl switch-desktop 2     # Switch to desktop 3 (0-indexed)
fynedesk-ctl lock                 # Lock screen
fynedesk-ctl subscribe windows-state desktop-state   # Stream events
```

### Socket protocol

Messages are JSON lines over UNIX socket:

```json
{"type":"request","id":1,"name":"list-windows","data":null}
{"type":"response","id":1,"name":"ok","data":{"windows":[...],"timestamp":123}}
```

Available events for subscription:
- `windows-state` — Window list changes
- `desktop-state` — Desktop switches
- `notification` — D-Bus notifications
- `volume-change` — Volume changes
- `brightness-change` — Brightness changes
- `keyboard-layout` — Layout changes

## Environment variables

| Variable | Description |
|----------|-------------|
| `FYNEDESK_TERMINAL` | Preferred terminal emulator (default: auto-detect) |
| `FYNE_SCALE` | UI scale factor for the panel |
| `WLR_BACKENDS` | wlroots backend (`wayland` for nested, `headless` for testing) |
| `WLR_RENDERER` | wlroots renderer (`gles2` default, `pixman` for software rendering) |
