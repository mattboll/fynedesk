#!/bin/bash
# Setup script for Tyde Wayland compositor development.
#
# The compositor targets wlroots 0.20 (pkg-config module "wlroots-0.20").
# The distribution package is used when available:
#   Debian forky/sid : libwlroots-0.20-dev
#   Arch             : wlroots0.20
#   Fedora 44+       : wlroots-devel (0.20.x)
# Otherwise wlroots is built from source into ~/.local (needs a recent
# wayland >= 1.24, wayland-protocols >= 1.47 and libdrm >= 2.4.129).

set -e

WLROOTS_VERSION="0.20.2"
WLROOTS_PC="wlroots-0.20"
INSTALL_PREFIX="$HOME/.local"
MULTIARCH="$(dpkg-architecture -qDEB_HOST_MULTIARCH 2>/dev/null || echo "$(uname -m)-linux-gnu")"

echo "=== Tyde Wayland Setup ==="
echo ""

if [ -f /etc/os-release ]; then
    . /etc/os-release
    DISTRO=$ID
else
    echo "Cannot detect distribution"
    exit 1
fi

echo "Detected: $DISTRO"
echo "wlroots:  $WLROOTS_PC"
echo ""

have_wlroots() {
    local v
    v=$(pkg-config --modversion "$WLROOTS_PC" 2>/dev/null) || return 1
    echo "✓ wlroots $v found ($(pkg-config --variable=prefix "$WLROOTS_PC"))"
}

if have_wlroots; then
    exit 0
fi
export PKG_CONFIG_PATH="$INSTALL_PREFIX/lib/$MULTIARCH/pkgconfig:$INSTALL_PREFIX/lib/pkgconfig:$PKG_CONFIG_PATH"
if have_wlroots; then
    exit 0
fi

# --- Distribution package ---

echo "=== Trying the distribution package ==="
case "$DISTRO" in
    debian|ubuntu|linuxmint|pop)
        sudo apt-get update
        if apt-cache show libwlroots-0.20-dev >/dev/null 2>&1; then
            sudo apt-get install -y libwlroots-0.20-dev
        fi
        ;;
    arch|manjaro|endeavouros)
        sudo pacman -S --needed wlroots0.20 || true
        ;;
    fedora)
        sudo dnf install -y 'pkgconfig(wlroots-0.20)' || true
        ;;
esac
if have_wlroots; then
    exit 0
fi

# --- Build from source ---

echo ""
echo "No $WLROOTS_PC package available, building wlroots $WLROOTS_VERSION from source."
echo ""
echo "=== Installing build dependencies ==="
case "$DISTRO" in
    debian|ubuntu|linuxmint|pop)
        sudo apt-get install -y \
            build-essential meson ninja-build pkg-config curl \
            libwayland-dev wayland-protocols libxkbcommon-dev libpixman-1-dev \
            libdrm-dev libgbm-dev libegl-dev libgles2-mesa-dev \
            libinput-dev libseat-dev libudev-dev libdisplay-info-dev libliftoff-dev \
            liblcms2-dev hwdata xwayland \
            libxcb1-dev libxcb-composite0-dev libxcb-dri3-dev libxcb-present-dev \
            libxcb-render0-dev libxcb-render-util0-dev libxcb-shm0-dev \
            libxcb-xfixes0-dev libxcb-xinput-dev libxcb-icccm4-dev \
            libxcb-ewmh-dev libxcb-res0-dev libxcb-errors-dev
        ;;
    fedora)
        sudo dnf install -y \
            gcc meson ninja-build pkgconfig curl \
            wayland-devel wayland-protocols-devel libxkbcommon-devel pixman-devel \
            libdrm-devel mesa-libgbm-devel mesa-libEGL-devel mesa-libGLES-devel \
            libinput-devel libseat-devel systemd-devel libdisplay-info-devel \
            libliftoff-devel lcms2-devel hwdata xorg-x11-server-Xwayland-devel \
            libxcb-devel xcb-util-devel xcb-util-renderutil-devel \
            xcb-util-wm-devel xcb-util-errors-devel
        ;;
    arch|manjaro|endeavouros)
        sudo pacman -S --needed \
            base-devel meson ninja curl wayland wayland-protocols libxkbcommon \
            pixman libdrm mesa libinput seatd libdisplay-info libliftoff lcms2 \
            hwdata xorg-xwayland libxcb xcb-util xcb-util-renderutil \
            xcb-util-wm xcb-util-errors
        ;;
    *)
        echo "Unsupported distribution: $DISTRO"
        echo "Please install wlroots $WLROOTS_VERSION (pkg-config: $WLROOTS_PC) manually."
        exit 1
        ;;
esac

echo ""
echo "=== Downloading wlroots $WLROOTS_VERSION ==="
TEMP_DIR=$(mktemp -d)
trap 'rm -rf "$TEMP_DIR"' EXIT
cd "$TEMP_DIR"
curl -sL "https://gitlab.freedesktop.org/wlroots/wlroots/-/archive/$WLROOTS_VERSION/wlroots-$WLROOTS_VERSION.tar.gz" | tar xz
cd "wlroots-$WLROOTS_VERSION"

echo ""
echo "=== Configuring wlroots ==="
meson setup build --prefix="$INSTALL_PREFIX" --libdir="lib/$MULTIARCH" -Dexamples=false -Dxwayland=enabled

echo ""
echo "=== Building and installing wlroots to $INSTALL_PREFIX ==="
ninja -C build
ninja -C build install

echo ""
have_wlroots
echo ""
echo "=== Setup complete ==="
echo ""
echo "To build all Wayland components, run:"
echo "  make wayland"
echo ""
echo "To run in nested mode:"
echo "  make wayland-run"
echo ""
