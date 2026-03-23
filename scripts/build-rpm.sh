#!/bin/bash
# Build an RPM package for FyneDesk Wayland compositor.
#
# Prerequisites:
#   - Compositor and panel must be built first (make compositor panel ctl)
#   - rpmbuild (rpm-build package on Fedora, rpm-build on openSUSE)
#
# Usage:
#   make compositor panel ctl && ./scripts/build-rpm.sh
#
# Output:
#   fynedesk-<version>-1.<dist>.x86_64.rpm

set -euo pipefail

# --- Configuration ---

VERSION="${VERSION:-1.1.0}"
ARCH="${ARCH:-x86_64}"
PKG_NAME="fynedesk"
MULTIARCH="$(dpkg-architecture -qDEB_HOST_MULTIARCH 2>/dev/null || echo "$(uname -m)-linux-gnu")"
WLROOTS_LIB="$HOME/.local/lib/$MULTIARCH"
RPMBUILD_DIR="${RPMBUILD_DIR:-$HOME/rpmbuild}"
SPEC_FILE="$(dirname "$0")/../packaging/fynedesk.spec"

# --- Preflight ---

for bin in compositor fynedesk-panel fynedesk-ctl; do
    if [ ! -f "./$bin" ]; then
        echo "ERROR: ./$bin not found. Run: make compositor panel ctl"
        exit 1
    fi
done

if ! command -v rpmbuild &>/dev/null; then
    echo "ERROR: rpmbuild not found."
    echo "  Fedora/RHEL:  sudo dnf install rpm-build"
    echo "  openSUSE:     sudo zypper install rpm-build"
    exit 1
fi

echo "Building ${PKG_NAME}-${VERSION}-1.${ARCH}.rpm..."

# --- Set up rpmbuild tree ---

mkdir -p "$RPMBUILD_DIR"/{BUILD,RPMS,SOURCES,SPECS,SRPMS}

# --- Copy sources into SOURCES ---

SOURCES="$RPMBUILD_DIR/SOURCES"

# Binaries
install -m 0755 compositor      "$SOURCES/compositor"
install -m 0755 fynedesk-panel   "$SOURCES/fynedesk-panel"
install -m 0755 fynedesk-ctl     "$SOURCES/fynedesk-ctl"

# wlroots shared libs
mkdir -p "$SOURCES/wlroots-libs"
if [ -d "$WLROOTS_LIB" ]; then
    for lib in "$WLROOTS_LIB"/libwlroots*.so*; do
        [ -f "$lib" ] && install -m 0644 "$lib" "$SOURCES/wlroots-libs/"
    done
fi

# Data files
install -m 0644 fynedesk-wayland.desktop "$SOURCES/"
[ -f portals/fynedesk.portal ] && install -m 0644 portals/fynedesk.portal "$SOURCES/"

# Man pages
for man in docs/man/fynedesk-compositor.1 docs/man/fynedesk-panel.1 docs/man/fynedesk-ctl.1 docs/man/fynedesk.5; do
    [ -f "$man" ] && install -m 0644 "$man" "$SOURCES/"
done

# Documentation
for doc in docs/install.md docs/keybindings.md docs/configuration.md; do
    [ -f "$doc" ] && install -m 0644 "$doc" "$SOURCES/"
done

# --- Copy spec ---

cp "$SPEC_FILE" "$RPMBUILD_DIR/SPECS/${PKG_NAME}.spec"

# --- Build RPM ---

rpmbuild -bb \
    --define "_topdir $RPMBUILD_DIR" \
    --define "version $VERSION" \
    --target "$ARCH" \
    "$RPMBUILD_DIR/SPECS/${PKG_NAME}.spec"

# --- Copy result ---

RPM_FILE=$(find "$RPMBUILD_DIR/RPMS/$ARCH/" -name "${PKG_NAME}-${VERSION}*.rpm" | head -1)
if [ -n "$RPM_FILE" ]; then
    cp "$RPM_FILE" "./"
    BASENAME=$(basename "$RPM_FILE")
    echo ""
    echo "Built: $BASENAME"
    echo ""
    rpm -qip "./$BASENAME"
    echo ""
    echo "Install with: sudo dnf install ./$BASENAME"
    echo "  or:         sudo rpm -ivh ./$BASENAME"
    echo "Then log out and select 'FyneDesk (Wayland)' from your display manager."
else
    echo "ERROR: RPM not found in $RPMBUILD_DIR/RPMS/$ARCH/"
    exit 1
fi
