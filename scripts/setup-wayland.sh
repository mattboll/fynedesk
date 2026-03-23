#!/bin/bash
# Setup script for FyneDesk Wayland compositor development
# This script installs wlroots 0.17 (required by Go bindings) and dependencies

set -e

WLROOTS_VERSION="0.17.4"
INSTALL_PREFIX="$HOME/.local"

echo "=== FyneDesk Wayland Setup ==="
echo ""

# Detect distro
if [ -f /etc/os-release ]; then
    . /etc/os-release
    DISTRO=$ID
else
    echo "Cannot detect distribution"
    exit 1
fi

echo "Detected: $DISTRO"
echo "wlroots version: $WLROOTS_VERSION"
echo "Install prefix: $INSTALL_PREFIX"
echo ""

# Check if wlroots 0.17 is already installed
if pkg-config --exists wlroots 2>/dev/null; then
    INSTALLED_VERSION=$(pkg-config --modversion wlroots 2>/dev/null || echo "unknown")
    if [[ "$INSTALLED_VERSION" == 0.17* ]]; then
        echo "✓ wlroots $INSTALLED_VERSION already installed"
        exit 0
    fi
fi

# Also check in ~/.local
export PKG_CONFIG_PATH="$INSTALL_PREFIX/lib/x86_64-linux-gnu/pkgconfig:$PKG_CONFIG_PATH"
if pkg-config --exists wlroots 2>/dev/null; then
    INSTALLED_VERSION=$(pkg-config --modversion wlroots 2>/dev/null || echo "unknown")
    if [[ "$INSTALLED_VERSION" == 0.17* ]]; then
        echo "✓ wlroots $INSTALLED_VERSION already installed in $INSTALL_PREFIX"
        exit 0
    fi
fi

echo "wlroots 0.17.x not found, installing..."
echo ""

# Install build dependencies
echo "=== Installing build dependencies ==="
case "$DISTRO" in
    debian|ubuntu|linuxmint|pop)
        sudo apt-get update
        sudo apt-get install -y \
            build-essential \
            meson \
            ninja-build \
            pkg-config \
            libwayland-dev \
            libxkbcommon-dev \
            libpixman-1-dev \
            libdrm-dev \
            libgbm-dev \
            libegl-dev \
            libgles2-mesa-dev \
            libinput-dev \
            libseat-dev \
            libxcb1-dev \
            libxcb-composite0-dev \
            libxcb-dri3-dev \
            libxcb-present-dev \
            libxcb-render0-dev \
            libxcb-render-util0-dev \
            libxcb-shm0-dev \
            libxcb-xfixes0-dev \
            libxcb-xinput-dev \
            libxcb-icccm4-dev \
            libxcb-ewmh-dev \
            libxcb-res0-dev \
            libxcb-errors-dev \
            hwdata \
            curl
        ;;
    fedora)
        sudo dnf install -y \
            gcc \
            meson \
            ninja-build \
            pkgconfig \
            wayland-devel \
            libxkbcommon-devel \
            pixman-devel \
            libdrm-devel \
            mesa-libgbm-devel \
            mesa-libEGL-devel \
            mesa-libGLES-devel \
            libinput-devel \
            libseat-devel \
            libxcb-devel \
            xcb-util-devel \
            xcb-util-renderutil-devel \
            xcb-util-wm-devel \
            xcb-util-errors-devel \
            hwdata \
            curl
        ;;
    arch|manjaro)
        sudo pacman -S --needed \
            base-devel \
            meson \
            ninja \
            wayland \
            libxkbcommon \
            pixman \
            libdrm \
            mesa \
            libinput \
            seatd \
            libxcb \
            xcb-util \
            xcb-util-renderutil \
            xcb-util-wm \
            xcb-util-errors \
            hwdata \
            curl
        ;;
    *)
        echo "Unsupported distribution: $DISTRO"
        echo "Please install wlroots 0.17 build dependencies manually"
        exit 1
        ;;
esac

echo ""
echo "=== Downloading wlroots $WLROOTS_VERSION ==="
TEMP_DIR=$(mktemp -d)
cd "$TEMP_DIR"

curl -sL "https://gitlab.freedesktop.org/wlroots/wlroots/-/archive/$WLROOTS_VERSION/wlroots-$WLROOTS_VERSION.tar.gz" | tar xz
cd "wlroots-$WLROOTS_VERSION"

echo ""
echo "=== Configuring wlroots ==="
meson setup build --prefix="$INSTALL_PREFIX" -Dexamples=false

echo ""
echo "=== Building wlroots ==="
ninja -C build

echo ""
echo "=== Installing wlroots to $INSTALL_PREFIX ==="
ninja -C build install

# Cleanup
cd /
rm -rf "$TEMP_DIR"

echo ""
echo "=== Setup complete ==="
echo ""
echo "wlroots $WLROOTS_VERSION installed to $INSTALL_PREFIX"
echo ""
echo "To build all Wayland components, run:"
echo "  make wayland"
echo ""
echo "To run in nested mode:"
echo "  make wayland-run"
echo ""
