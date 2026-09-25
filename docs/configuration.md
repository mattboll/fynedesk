# Configuration

Tyde stores its configuration in `~/.config/tyde/config.toml`. Most settings can be changed through the graphical Settings panel (right-click desktop > Settings, or Super+A > Settings).

## Configuration file

```toml
# ~/.config/tyde/config.toml

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

[windows]
wobbly = true                   # dragged windows bend like jelly
blur = true                     # frosted glass behind the panel, menus and notifications (Wayland)

[power]
lock_timeout_min = 5            # idle minutes before the screen locks (0 = never)
blank_timeout_min = 6           # before it is turned off
suspend_timeout_min = 0         # before the machine sleeps
suspend_action = "suspend"      # suspend | hibernate | hybrid-sleep | nothing

[keybindings]
quit = "Alt+Escape"
open_terminal = "Super+t"
show_launcher = "Super+space"
# Full list: see docs/keybindings.md
```

## Settings UI

The settings panel provides graphical access to all configuration:

- **Appearance** — Computer type, colour scheme, accessibility, language, clock, fonts, screensaver, icon theme
- **Background** — Image (fill mode, colour), time-of-day folder or animation, per monitor
- **Color Scheme** — Themes and colour overrides (titlebars, panels, accent)
- **Dock** — Bar position (left/bottom), icons, size and zoom
- **Desktops**, **Keyboard** (layouts and keybindings), **Window Rules**, **Modules**, **AI**
- **Account**, **Display** (resolution, scale, arrangement, duplicate or turn off a screen), **Network**, **Time/Date**,
  **Power**, **Calendar**, **Advanced** (natural scroll, window gaps, wobbly windows, frosted glass, power profile)

## Frosted glass

In a Wayland session, what lies behind the dock, the side panel, the menus, the
launcher and the notifications is blurred, and their backgrounds let it show
through. It needs the GLES2 renderer: with another one (pixman) they stay
opaque. Turn it off with `blur = false` under `[windows]`, or in Settings >
Advanced.

## Window places

Tyde remembers where each application's windows are for each set of screens
plugged in: maximized, on a half or a quarter of a screen, or left free, and on
which desktop. Plug a screen in or out and the windows go back where you had
them with those screens; a window that opens goes where the same one was.
Arrange the windows once for each set of screens: the places are learnt a
moment after you move them, and kept in `~/.config/tyde/window-places.json`.
A window rule for an application takes precedence over its remembered place.

## Idle curtain

Twenty seconds before the screen locks, turns off or the machine sleeps for
want of use, the screen slowly darkens; moving the mouse or pressing a key
lifts the curtain and puts the countdown back to zero. Nothing darkens while an
application holds the screensaver off (a video playing, a presentation).

## Notifications

Popups stack in the top-right corner, three at most. Tyde follows the hints of
the freedesktop specification: a notification with the same id (or the same
`x-canonical-private-synchronous` / `x-dunst-stack-tag`) replaces the previous
one instead of piling up, `urgency=low` only goes to the history, `critical`
stays until dismissed, and `transient` leaves no trace in the history. An
application that closes its notification removes it from the screen and the
history. The actions an application offers (*Reply*, *Mark as read*…) are
buttons of its popup.

## Coding agents

When herdr (a terminal workspace manager for coding agents) runs, the widget
panel lists its agents (Claude Code, Codex…): those waiting for an answer,
those at work and, folded, the idle ones; a click shows the agent in herdr. Tyde tells you once when an
agent finishes or asks something, unless you are looking at it, and makes the
herdr window glow until you have seen it. The agents are read from the herdr
socket (`$HERDR_SOCKET_PATH`, by default `~/.config/herdr/herdr.sock`), so no
`notify-send` hook is needed in the agents.

It all comes with the **Coding Agents** module, turned on once when herdr is
found (Settings > Modules). Turned off, nothing of it is left: no widget, no
notification, no glow, and Super+G is free.

## Phone

The **Phone** module (turned on once when KDE Connect or `adb` is found) links
phones with Tyde, from the phone in the widget panel.

**KDE Connect**: with the KDE Connect app on the phone and `kdeconnectd` on the
computer (package `kdeconnect`), pair the phone from its window (or accept
its request). The panel then shows its battery; the notifications of the phone
pop up in Tyde with their actions (*Reply* opens the reply window); the
clipboard is shared; the window rings the phone, sends it files and opens its
files in the file manager. Tyde starts `kdeconnectd` itself.

**Debugging over Wi-Fi**, without Android Studio: *Connect a phone* shows a
QR code; on the phone, in Developer options > Wireless debugging, choose
*Pair device with QR code* and scan it. Tyde finds the phone on the local
network, pairs and connects it with `adb`, so `adb`, Gradle, Flutter and the
other tools see it. A phone paired once is connected again whenever it is on
the same network. With [scrcpy](https://github.com/Genymobile/scrcpy)
installed, *Screen* shows the phone's screen in a window, to use it with the
mouse and the keyboard.

## Screen sharing

Screen sharing goes through `xdg-desktop-portal-wlr`. When an application
(Firefox, Chrome, Meet…) starts sharing, `tyde_chooser` asks whether to share a
whole screen or a single window; only that window is sent, even when other
windows or notifications cover it.

The compositor configures the portal at startup in
`~/.config/xdg-desktop-portal-wlr/Tyde`, which the portal reads before its
generic `config` file. To use your own settings instead, remove the first line
of that file (`# Written by Tyde…`): Tyde then leaves it alone.

Sandboxed (Flatpak) applications cannot capture the screen or windows, nor
type into other applications, directly: they have to go through the portal.

## IPC Socket

Tyde exposes a UNIX socket at `$XDG_RUNTIME_DIR/tyde-compositor.sock` for scripting.

### Using tyde_wmctl

```bash
tyde_wmctl windows              # List all windows
tyde_wmctl windows --json       # JSON output
tyde_wmctl desktop              # Show current desktop
tyde_wmctl focus xdg-3          # Focus a window
tyde_wmctl close xdg-3          # Close a window
tyde_wmctl minimize xdg-3       # Minimize
tyde_wmctl maximize xdg-3       # Maximize
tyde_wmctl switch-desktop 2     # Switch to desktop 3 (0-indexed)
tyde_wmctl lock                 # Lock screen
tyde_wmctl subscribe windows-state desktop-state   # Stream events
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
| `TYDE_TERMINAL` | Preferred terminal emulator (default: auto-detect) |
| `FYNE_SCALE` | UI scale factor for the panel |
| `WLR_BACKENDS` | wlroots backend (`wayland` for nested, `headless` for testing) |
| `WLR_RENDERER` | wlroots renderer (`gles2` default, `pixman` for software rendering) |
