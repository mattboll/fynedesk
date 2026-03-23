# Installing FyneDesk

## Quick Install (from source)

```bash
git clone https://github.com/nicholasgasior/fynedesk.git
cd fynedesk
make setup-wayland    # Install wlroots 0.17 to ~/.local
make wayland
sudo make wayland-install
```

After install, log out and select **FyneDesk (Wayland)** from your display manager (GDM, SDDM, greetd).

## Prerequisites

### Build dependencies

**Debian/Ubuntu:**
```bash
sudo apt-get install gcc libgl1-mesa-dev libegl1-mesa-dev \
    libgles2-mesa-dev libx11-dev xorg-dev libwayland-dev \
    libxkbcommon-dev libinput-dev libpixman-1-dev \
    meson ninja-build cmake
```

**Fedora:**
```bash
sudo dnf install gcc mesa-libGL-devel mesa-libEGL-devel \
    libX11-devel wayland-devel libxkbcommon-devel \
    libinput-devel pixman-devel meson ninja-build cmake
```

**Arch:**
```bash
sudo pacman -S gcc mesa wayland libxkbcommon libinput \
    pixman meson ninja cmake xorg-server-xwayland
```

### Go

Go 1.23 or later is required.

```bash
# Install from https://go.dev/dl/ or:
sudo snap install go --classic
```

### wlroots 0.17

FyneDesk requires wlroots 0.17 (not 0.18 or 0.19). The setup script builds and installs it to `~/.local`:

```bash
make setup-wayland
```

This downloads, builds, and installs wlroots 0.17 along with its dependencies. The library is installed to `~/.local/lib/x86_64-linux-gnu/` to avoid conflicts with system packages.

## Build targets

| Command | Description |
|---------|-------------|
| `make compositor` | Build the Wayland compositor |
| `make panel` | Build the panel (taskbar, widgets, launcher) |
| `make ctl` | Build the `fynedesk-ctl` CLI tool |
| `make emoji-picker` | Build the emoji picker |
| `make wayland-run` | Build and run in nested mode (inside existing desktop) |
| `make wayland-install` | Install compositor, panel, and .desktop file system-wide |

## Running

### As a session (recommended)

After `sudo make wayland-install`, log out and select **FyneDesk (Wayland)** from your display manager login screen.

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

```bash
sudo rm /usr/local/bin/fynedesk-compositor
sudo rm /usr/local/bin/fynedesk-panel
sudo rm /usr/local/share/wayland-sessions/fynedesk-wayland.desktop
```

## Troubleshooting

### Black screen after login

Check the compositor log:
```bash
journalctl --user -u fynedesk-compositor
# or:
cat /tmp/fynedesk-compositor.log
```

Common causes:
- Missing wlroots 0.17 in library path
- GPU driver issues (try `WLR_RENDERER=pixman fynedesk-compositor`)

### Panel not appearing

The panel starts automatically as a child process. If it doesn't appear:
```bash
# Check if panel binary exists:
which fynedesk-panel
# Run panel manually:
WAYLAND_DISPLAY=wayland-0 fynedesk-panel
```

### XWayland apps not starting

XWayland is started automatically. Check if it's available:
```bash
which Xwayland
# Install if missing:
sudo apt-get install xwayland  # Debian/Ubuntu
```
