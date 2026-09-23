#!/bin/bash
# Test runner for Tyde.
#
# The Wayland compositor and its wlroots layer are left out: they need the
# wlroots 0.20 development files, and the compositor tests are run by the CI
# compositor job. The tests keep away from the user's session themselves
# (temporary XDG directories, no display, no session bus).

set -e

mapfile -t PACKAGES < <(go list ./... | grep -v -e /cmd/tyde_compositor -e /internal/wayland/compositor -e /internal/wayland/wlr)

echo "Running tests for ${#PACKAGES[@]} packages..."
go test -tags ci "${PACKAGES[@]}" "$@"
echo "All tests passed."
