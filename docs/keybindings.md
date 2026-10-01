# Keyboard Shortcuts

All shortcuts are configurable via Settings > Keyboard. The **WM modifier** defaults to Super (Windows key) and can be changed to Alt.

## Window Management

| Shortcut | Action |
|----------|--------|
| Alt+F4 | Close window |
| F11 | Toggle fullscreen |
| Super+Up / Super+F10 | Maximize / restore |
| Super+Down / Super+F9 | Minimize |
| Super+Left | Snap window left |
| Super+Right | Snap window right |
| Super+Tab | Switch app (next) |
| Super+Shift+Tab | Switch app (previous) |

## Virtual Desktops

| Shortcut | Action |
|----------|--------|
| Ctrl+Alt+Left | Previous desktop |
| Ctrl+Alt+Right | Next desktop |
| Super+1 | Switch to desktop 1 |
| Super+2 | Switch to desktop 2 |
| Super+3 | Switch to desktop 3 |
| Super+4 | Switch to desktop 4 |
| Super+Shift+1 | Move window to desktop 1 |
| Super+Shift+2 | Move window to desktop 2 |
| Super+Shift+3 | Move window to desktop 3 |
| Super+Shift+4 | Move window to desktop 4 |
| Ctrl+Alt+Shift+Left | Move window to previous desktop |
| Ctrl+Alt+Shift+Right | Move window to next desktop |

## Applications

| Shortcut | Action |
|----------|--------|
| Super+T / Super+Enter | Open terminal |
| Super+` | Toggle dropdown terminal |
| Super+Space | App launcher |
| Super+P | Command palette |
| Super+. | Emoji picker |
| Super+V | Clipboard manager |

## Desktop Features

| Shortcut | Action |
|----------|--------|
| Super+W | Window overview (Exposé) |
| Super+F | Focus mode: dim everything but the focused window |
| Super+G | Go to the coding agent waiting for you (again: the next one) |
| Super+= / Super+- | Magnifier: enlarge / shrink the screen around the pointer |
| Super+Alt+scroll | Magnifier, with the wheel or the touchpad |
| Super+0 | Magnifier off |
| Super+A | Toggle sidebar (Raven) |
| Super+L | Lock screen |
| PrintScreen | Screenshot (the screen under the pointer) |
| Super+PrintScreen | Screenshot (every screen) |
| Shift+PrintScreen | Screenshot (region: drag a zone, or click a window) |
| Ctrl+PrintScreen | Screenshot (window) |
| Ctrl+Shift+PrintScreen | Copy the text of a zone (OCR, needs tesseract) |
| Super+Shift+PrintScreen | Screenshot of the screen under the pointer, in 3 seconds |
| Super+Shift+R | Record a zone or a window to ~/Videos (again: stop) |

## Tiling Mode

| Shortcut | Action |
|----------|--------|
| Super+Shift+T | Toggle tiling mode |
| Super+Shift+J | Swap master |
| Super+Shift+H | Shrink master ratio |
| Super+Shift+L | Grow master ratio |
| Super+Shift+F | Toggle float (untile window) |

## Media Keys

| Key | Action |
|-----|--------|
| Volume Up | Raise volume |
| Volume Down | Lower volume |
| Mute | Toggle mute |
| Brightness Up | Increase brightness |
| Brightness Down | Decrease brightness |
| Calculator | Open calculator |

## System

| Shortcut | Action |
|----------|--------|
| Alt+Escape | Quit compositor |
| Ctrl+Alt+Backspace | Emergency logout |

## Trackpad Gestures

| Gesture | Action |
|---------|--------|
| 3-finger swipe left/right | Switch desktop (the desktops follow the fingers) |
| 3-finger swipe up | Window overview |
| 4-finger swipe up | App launcher |

## Customizing

Open Settings > Keyboard to remap any shortcut. Bindings are stored in `~/.config/tyde/config.toml` under the `[keybindings]` section.

You can also edit bindings directly:

```toml
# ~/.config/tyde/config.toml
[keybindings]
quit = "Alt+Escape"
open_terminal = "Super+t"
show_launcher = "Super+space"
# ... see all actions in Settings > Keyboard
```
