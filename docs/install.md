# Installing Tyde

## Quick Install (from source)

```bash
git clone https://github.com/FyshOS/tyde.git
cd tyde
make setup-wayland    # Install wlroots 0.20 (distribution package or ~/.local)
make wayland
sudo make wayland-install
```

After install, log out and select **Tyde (Wayland)** from your display manager (GDM, SDDM, greetd).

## Prerequisites

### Build dependencies

The built-in lock screen authenticates through PAM, so the PAM headers are
required. meson and ninja are only needed when wlroots has to be built from
source, and `make setup-wayland` installs them itself in that case.

**Debian:**
```bash
sudo apt-get install gcc pkg-config libgl1-mesa-dev libegl1-mesa-dev \
    libgles2-mesa-dev libx11-dev xorg-dev libwayland-dev \
    libxkbcommon-dev libinput-dev libpixman-1-dev libpam0g-dev xwayland
```

**Fedora:**
```bash
sudo dnf install gcc pkgconf mesa-libGL-devel mesa-libEGL-devel \
    libX11-devel wayland-devel libxkbcommon-devel \
    libinput-devel pixman-devel pam-devel xorg-x11-server-Xwayland
```

**Arch:**
```bash
sudo pacman -S gcc pkgconf mesa wayland libxkbcommon libinput \
    pixman pam xorg-xwayland
```

### Go

Go 1.25 or later is required.

```bash
# Install from https://go.dev/dl/ or:
sudo snap install go --classic
```

### wlroots 0.20

Tyde requires wlroots 0.20 (pkg-config module `wlroots-0.20`). Use the
distribution package when available:

| Distribution          | Package              |
|-----------------------|----------------------|
| Debian forky / sid    | `libwlroots-0.20-dev` |
| Arch                  | `wlroots0.20`        |
| Fedora 44+            | `wlroots-devel`      |
| NixOS 26.05+          | `wlroots_0_20` (see `flake.nix`) |

Otherwise the setup script builds wlroots 0.20 from source and installs it to
`~/.local` (this needs wayland >= 1.24, wayland-protocols >= 1.47 and
libdrm >= 2.4.129):

```bash
make setup-wayland
```

No Ubuntu release packages wlroots 0.20, and Ubuntu 24.04 LTS ships a
wayland that is too old to build it (1.22): use Debian forky/sid, Arch,
Fedora 44+ or NixOS instead.

The installed compositor links against `libwlroots-0.20.so`: install the
runtime library (`libwlroots-0.20` on Debian) before `sudo make wayland-install`.

## Build targets

| Command | Description |
|---------|-------------|
| `make compositor` | Build the Wayland compositor |
| `make panel` | Build the panel (taskbar, widgets, launcher) |
| `make ctl` | Build the `tyde_wmctl` CLI tool |
| `make emoji-picker` | Build the emoji picker |
| `make chooser` | Build the screen sharing chooser |
| `make wayland-run` | Build and run in nested mode (inside existing desktop) |
| `make wayland` | Build all of the above |
| `make wayland-install` | Install the Wayland binaries, session and portal files system-wide |

## Running

### As a session (recommended)

After `sudo make wayland-install`, log out and select **Tyde (Wayland)** from your display manager login screen.

### Nested mode (development)

Run inside your current desktop session for testing:

```bash
make wayland-run
```

This opens a window running the full compositor. Applications launched inside it use `WAYLAND_DISPLAY=wayland-1`.

### Testing with clients

While the compositor is running (nested or session):

```bash
# From another terminal:
WAYLAND_DISPLAY=wayland-1 foot         # Terminal
WAYLAND_DISPLAY=wayland-1 firefox      # Browser
WAYLAND_DISPLAY=wayland-1 nautilus     # File manager
```

## Uninstalling

`make wayland-install` installs into `/usr/local` when that directory exists,
`/usr` otherwise (override with `PREFIX=`). With the default `/usr/local`:

```bash
sudo rm /usr/local/bin/tyde_compositor /usr/local/bin/tyde_panel \
    /usr/local/bin/tyde_wmctl /usr/local/bin/tyde_emoji /usr/local/bin/tyde_chooser
sudo rm /usr/local/share/wayland-sessions/tyde-wayland.desktop
sudo rm /usr/local/share/xdg-desktop-portal/portals/tyde.portal
sudo rm /usr/local/share/man/man1/tyde_compositor.1 \
    /usr/local/share/man/man1/tyde_panel.1 \
    /usr/local/share/man/man1/tyde_wmctl.1 \
    /usr/local/share/man/man5/tyde.5
```

Tyde also writes `~/.config/xdg-desktop-portal-wlr/Tyde` (see
[Screen sharing](configuration.md#screen-sharing)); remove it too.

## Troubleshooting

### Black screen after login

Check the compositor log (written by the crash-recovery runner, the previous
run's log is overwritten at each start):
```bash
cat ~/.cache/fyne/com.fyshos.tyde/compositor.log
```

Common causes:
- Missing `libwlroots-0.20.so` (install the distribution package, or check `ldd $(which tyde_compositor)`)
- GPU driver issues (try `WLR_RENDERER=pixman tyde_compositor`)

### Panel not appearing

The panel starts automatically as a child process (an X11 client running
through XWayland, restarted if it exits). If it doesn't appear:
```bash
# Check if panel binary exists:
which tyde_panel
# Look for panel errors in the compositor log:
grep -i panel ~/.cache/fyne/com.fyshos.tyde/compositor.log
```
To run it by hand from a terminal inside the session, give it the primary
output geometry (width, height, x, y), e.g. `tyde_panel 1920 1080 0 0`;
`DISPLAY` must point at the session's XWayland display.

### XWayland apps not starting

XWayland is started automatically. Check if it's available:
```bash
which Xwayland
# Install if missing:
sudo apt-get install xwayland  # Debian/Ubuntu
```
