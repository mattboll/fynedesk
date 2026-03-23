#!/bin/bash
# Safe test runner for FyneDesk
#
# WARNING: Do NOT use "go test ./..." — it compiles cmd/compositor/ which
# links wlroots CGO and can crash your graphical session.
#
# This script explicitly lists only the safe test packages.

set -e

PACKAGES=(
    ./cmd/fynedesk_runner/
    ./internal/ui/
    ./internal/x11/
    ./internal/x11/wm/
    ./modules/launcher/
    ./modules/status/
    ./theme/
    ./wm/
    ./wlipc/
)

echo "Running tests for ${#PACKAGES[@]} packages..."
go test -tags ci "${PACKAGES[@]}" "$@"
echo "All tests passed."
