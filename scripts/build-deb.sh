#!/bin/bash
# Build a .deb package for Tyde Wayland compositor.
#
# Prerequisites:
#   - Compositor and panel must be built first (make compositor panel ctl)
#   - dpkg-deb (standard on Debian/Ubuntu)
#
# Usage:
#   make compositor panel ctl && ./scripts/build-deb.sh
#
# Output:
#   tyde_<version>_amd64.deb

set -euo pipefail

# --- Configuration ---

VERSION="${VERSION:-1.1.0}"
ARCH="${ARCH:-amd64}"
PKG_NAME="tyde"
PKG_DIR="/tmp/${PKG_NAME}_${VERSION}_${ARCH}"

# --- Preflight ---

for bin in tyde_compositor tyde_panel tyde_wmctl tyde_emoji tyde_chooser; do
    if [ ! -f "./$bin" ]; then
        echo "ERROR: ./$bin not found. Run: make wayland"
        exit 1
    fi
done

if ! command -v dpkg-deb &>/dev/null; then
    echo "ERROR: dpkg-deb not found. Install dpkg."
    exit 1
fi

echo "Building ${PKG_NAME}_${VERSION}_${ARCH}.deb..."

# --- Clean ---

rm -rf "$PKG_DIR"

# --- Create directory structure ---

mkdir -p "$PKG_DIR/DEBIAN"
mkdir -p "$PKG_DIR/usr/bin"
mkdir -p "$PKG_DIR/usr/share/wayland-sessions"
mkdir -p "$PKG_DIR/usr/share/xdg-desktop-portal/portals"
mkdir -p "$PKG_DIR/usr/share/man/man1"
mkdir -p "$PKG_DIR/usr/share/doc/tyde"

# --- Install binaries ---

install -m 0755 tyde_compositor "$PKG_DIR/usr/bin/tyde_compositor"
install -m 0755 tyde_panel "$PKG_DIR/usr/bin/tyde_panel"
install -m 0755 tyde_wmctl "$PKG_DIR/usr/bin/tyde_wmctl"
install -m 0755 tyde_emoji "$PKG_DIR/usr/bin/tyde_emoji"
install -m 0755 tyde_chooser "$PKG_DIR/usr/bin/tyde_chooser"

# wlroots is not bundled: the package depends on the distribution's
# libwlroots-0.20 (Debian forky/sid and later).

# --- Install data files ---

install -m 0644 tyde-wayland.desktop "$PKG_DIR/usr/share/wayland-sessions/"

if [ -f portals/tyde.portal ]; then
    install -m 0644 portals/tyde.portal "$PKG_DIR/usr/share/xdg-desktop-portal/portals/"
fi

# Man pages
install -m 0644 docs/man/tyde_compositor.1 "$PKG_DIR/usr/share/man/man1/"
install -m 0644 docs/man/tyde_wmctl.1 "$PKG_DIR/usr/share/man/man1/"

# Documentation
for doc in docs/install.md docs/keybindings.md docs/configuration.md; do
    if [ -f "$doc" ]; then
        install -m 0644 "$doc" "$PKG_DIR/usr/share/doc/tyde/"
    fi
done

# --- DEBIAN control file ---

INSTALLED_SIZE=$(du -sk "$PKG_DIR" | cut -f1)

cat > "$PKG_DIR/DEBIAN/control" << CTRL
Package: ${PKG_NAME}
Version: ${VERSION}
Section: x11
Priority: optional
Architecture: ${ARCH}
Installed-Size: ${INSTALLED_SIZE}
Depends: libwlroots-0.20, libwayland-server0, libxkbcommon0, libinput10, libpixman-1-0, libegl1, libgles2, xwayland, libseat1
Recommends: foot, pipewire, wireplumber, grim, slurp
Suggests: papirus-icon-theme, noto-fonts
Maintainer: Tyde Contributors <tyde@fyshos.com>
Homepage: https://github.com/FyshOS/tyde
Description: Lightweight Wayland desktop environment written in Go
 Tyde is a full-featured Wayland compositor built with wlroots 0.20
 and the Fyne GUI toolkit. It provides a material design desktop with
 frosted glass effects, spring-curve animations, tiling mode, virtual
 desktops, trackpad gestures, and an IPC socket for scripting.
 .
 Includes: compositor, panel, app launcher, notification system,
 system tray, screen locker, and tyde_wmctl CLI.
CTRL

# --- Build .deb ---

dpkg-deb --build --root-owner-group "$PKG_DIR"
mv "${PKG_DIR}.deb" "./${PKG_NAME}_${VERSION}_${ARCH}.deb"

# --- Cleanup ---

rm -rf "$PKG_DIR"

echo ""
echo "Built: ${PKG_NAME}_${VERSION}_${ARCH}.deb"
echo ""
dpkg-deb --info "./${PKG_NAME}_${VERSION}_${ARCH}.deb"
echo ""
echo "Install with: sudo dpkg -i ${PKG_NAME}_${VERSION}_${ARCH}.deb"
echo "Then log out and select 'Tyde (Wayland)' from your display manager."
