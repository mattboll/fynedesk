#!/bin/bash
# Build a .deb package for FyneDesk Wayland compositor.
#
# Prerequisites:
#   - Compositor and panel must be built first (make compositor panel ctl)
#   - dpkg-deb (standard on Debian/Ubuntu)
#
# Usage:
#   make compositor panel ctl && ./scripts/build-deb.sh
#
# Output:
#   fynedesk_<version>_amd64.deb

set -euo pipefail

# --- Configuration ---

VERSION="${VERSION:-1.1.0}"
ARCH="${ARCH:-amd64}"
PKG_NAME="fynedesk"
PKG_DIR="/tmp/${PKG_NAME}_${VERSION}_${ARCH}"
MULTIARCH="$(dpkg-architecture -qDEB_HOST_MULTIARCH 2>/dev/null || echo "$(uname -m)-linux-gnu")"
WLROOTS_LIB="$HOME/.local/lib/$MULTIARCH"

# --- Preflight ---

for bin in compositor fynedesk-panel fynedesk-ctl; do
    if [ ! -f "./$bin" ]; then
        echo "ERROR: ./$bin not found. Run: make compositor panel ctl"
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
mkdir -p "$PKG_DIR/usr/share/doc/fynedesk"

# --- Install binaries ---

install -m 0755 compositor "$PKG_DIR/usr/bin/fynedesk-compositor"
install -m 0755 fynedesk-panel "$PKG_DIR/usr/bin/fynedesk-panel"
install -m 0755 fynedesk-ctl "$PKG_DIR/usr/bin/fynedesk-ctl"

# --- Install wlroots shared lib ---

# The compositor is linked against wlroots 0.17 in ~/.local. We bundle the
# shared library so the .deb works without requiring the user to build wlroots.
if [ -d "$WLROOTS_LIB" ]; then
    mkdir -p "$PKG_DIR/usr/lib/$MULTIARCH"
    for lib in "$WLROOTS_LIB"/libwlroots*.so*; do
        if [ -f "$lib" ]; then
            install -m 0644 "$lib" "$PKG_DIR/usr/lib/$MULTIARCH/"
        fi
    done
fi

# --- Install data files ---

install -m 0644 fynedesk-wayland.desktop "$PKG_DIR/usr/share/wayland-sessions/"

if [ -f portals/fynedesk.portal ]; then
    install -m 0644 portals/fynedesk.portal "$PKG_DIR/usr/share/xdg-desktop-portal/portals/"
fi

# Man pages
install -m 0644 docs/man/fynedesk-compositor.1 "$PKG_DIR/usr/share/man/man1/"
install -m 0644 docs/man/fynedesk-ctl.1 "$PKG_DIR/usr/share/man/man1/"

# Documentation
for doc in docs/install.md docs/keybindings.md docs/configuration.md; do
    if [ -f "$doc" ]; then
        install -m 0644 "$doc" "$PKG_DIR/usr/share/doc/fynedesk/"
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
Depends: libwayland-server0, libxkbcommon0, libinput10, libpixman-1-0, libegl1, libgles2, xwayland, libseat1
Recommends: foot, pipewire, wireplumber, grim, slurp
Suggests: papirus-icon-theme, noto-fonts
Maintainer: FyneDesk Contributors <fynedesk@fyshos.com>
Homepage: https://github.com/nicholasgasior/fynedesk
Description: Lightweight Wayland desktop environment written in Go
 FyneDesk is a full-featured Wayland compositor built with wlroots 0.17
 and the Fyne GUI toolkit. It provides a material design desktop with
 frosted glass effects, spring-curve animations, tiling mode, virtual
 desktops, trackpad gestures, and an IPC socket for scripting.
 .
 Includes: compositor, panel, app launcher, notification system,
 system tray, screen locker, and fynedesk-ctl CLI.
CTRL

# --- DEBIAN postinst ---

cat > "$PKG_DIR/DEBIAN/postinst" << POSTINST
#!/bin/sh
set -e
# Update library cache if we bundled wlroots
if [ -d /usr/lib/$MULTIARCH ] && command -v ldconfig >/dev/null; then
    ldconfig
fi
POSTINST
chmod 0755 "$PKG_DIR/DEBIAN/postinst"

# --- DEBIAN postrm ---

cat > "$PKG_DIR/DEBIAN/postrm" << 'POSTRM'
#!/bin/sh
set -e
if command -v ldconfig >/dev/null; then
    ldconfig
fi
POSTRM
chmod 0755 "$PKG_DIR/DEBIAN/postrm"

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
echo "Then log out and select 'FyneDesk (Wayland)' from your display manager."
