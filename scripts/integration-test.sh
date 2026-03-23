#!/bin/bash
# Integration test for FyneDesk Wayland compositor.
#
# Launches the compositor in headless mode (WLR_BACKENDS=headless) and
# verifies IPC responses via fynedesk-ctl. Requires compositor and
# fynedesk-ctl binaries to be built first.
#
# Usage:
#   make compositor && make ctl && ./scripts/integration-test.sh
#
# Environment:
#   COMPOSITOR_BIN  path to compositor binary (default: ./compositor)
#   CTL_BIN         path to fynedesk-ctl binary (default: ./fynedesk-ctl)
#   TIMEOUT         max seconds to wait for compositor startup (default: 10)

set -euo pipefail

COMPOSITOR_BIN="${COMPOSITOR_BIN:-./compositor}"
CTL_BIN="${CTL_BIN:-./fynedesk-ctl}"
TIMEOUT="${TIMEOUT:-10}"
COMP_PID=0
PASS=0
FAIL=0
TOTAL=0

# Cleanup on exit
cleanup() {
    if [ "$COMP_PID" -ne 0 ] && kill -0 "$COMP_PID" 2>/dev/null; then
        kill "$COMP_PID" 2>/dev/null || true
        wait "$COMP_PID" 2>/dev/null || true
    fi
    rm -f "$XDG_RUNTIME_DIR/fynedesk.sock" 2>/dev/null || true
}
trap cleanup EXIT

# --- Helpers ---

pass() {
    TOTAL=$((TOTAL + 1))
    PASS=$((PASS + 1))
    echo "  PASS: $1"
}

fail() {
    TOTAL=$((TOTAL + 1))
    FAIL=$((FAIL + 1))
    echo "  FAIL: $1"
    if [ -n "${2:-}" ]; then
        echo "        $2"
    fi
}

assert_contains() {
    local output="$1"
    local pattern="$2"
    local test_name="$3"
    if echo "$output" | grep -q "$pattern"; then
        pass "$test_name"
    else
        fail "$test_name" "expected pattern '$pattern' in output: $output"
    fi
}

assert_json_field() {
    local json="$1"
    local field="$2"
    local expected="$3"
    local test_name="$4"
    local actual
    actual=$(echo "$json" | python3 -c "import sys,json; print(json.load(sys.stdin)$field)" 2>/dev/null || echo "PARSE_ERROR")
    if [ "$actual" = "$expected" ]; then
        pass "$test_name"
    else
        fail "$test_name" "expected $field=$expected, got $actual"
    fi
}

# --- Preflight checks ---

echo "=== FyneDesk Integration Tests ==="
echo ""

if [ ! -x "$COMPOSITOR_BIN" ]; then
    echo "ERROR: Compositor binary not found at $COMPOSITOR_BIN"
    echo "Run: make compositor"
    exit 1
fi

if [ ! -x "$CTL_BIN" ]; then
    echo "ERROR: fynedesk-ctl binary not found at $CTL_BIN"
    echo "Run: make ctl"
    exit 1
fi

# Use a temporary XDG_RUNTIME_DIR to avoid conflicts with running compositor
export XDG_RUNTIME_DIR=$(mktemp -d)
trap 'cleanup; rm -rf "$XDG_RUNTIME_DIR"' EXIT

# --- Start compositor ---

echo "Starting compositor (headless)..."

# wlroots env for finding wlroots 0.17
MULTIARCH="$(dpkg-architecture -qDEB_HOST_MULTIARCH 2>/dev/null || echo "$(uname -m)-linux-gnu")"
WLROOTS_LIB="$HOME/.local/lib/$MULTIARCH"
export LD_LIBRARY_PATH="${WLROOTS_LIB}:${LD_LIBRARY_PATH:-}"
export PKG_CONFIG_PATH="${WLROOTS_LIB}/pkgconfig:${PKG_CONFIG_PATH:-}"

WLR_BACKENDS=headless WLR_HEADLESS_OUTPUTS=1 \
    "$COMPOSITOR_BIN" > /tmp/integration-test-compositor.log 2>&1 &
COMP_PID=$!

# Wait for socket to appear
elapsed=0
while [ ! -S "$XDG_RUNTIME_DIR/fynedesk.sock" ]; do
    if ! kill -0 "$COMP_PID" 2>/dev/null; then
        echo "ERROR: Compositor exited prematurely"
        cat /tmp/integration-test-compositor.log
        exit 1
    fi
    sleep 0.5
    elapsed=$((elapsed + 1))
    if [ "$elapsed" -ge "$((TIMEOUT * 2))" ]; then
        echo "ERROR: Timed out waiting for IPC socket"
        cat /tmp/integration-test-compositor.log
        kill "$COMP_PID" 2>/dev/null || true
        exit 1
    fi
done

echo "Compositor started (PID=$COMP_PID), socket ready."
echo ""

# --- Test: list-windows ---

echo "--- list-windows ---"
output=$("$CTL_BIN" windows --json 2>&1) || true
# With no clients, should return empty windows array
assert_contains "$output" '"windows"' "list-windows returns JSON with windows field"

# Parse as JSON (windows can be null or [] when no clients are connected)
if echo "$output" | python3 -c "import sys,json; d=json.load(sys.stdin); assert d['windows'] is None or isinstance(d['windows'], list)" 2>/dev/null; then
    pass "list-windows returns valid JSON"
else
    fail "list-windows returns valid JSON" "output: $output"
fi

echo ""

# --- Test: get-desktop ---

echo "--- get-desktop ---"
output=$("$CTL_BIN" desktop 2>&1) || true
assert_contains "$output" "Desktop" "get-desktop returns desktop info"

echo ""

# --- Test: switch-desktop ---

echo "--- switch-desktop ---"
"$CTL_BIN" switch-desktop 1 2>&1 || true
sleep 0.2
output=$("$CTL_BIN" desktop 2>&1) || true
assert_contains "$output" "2" "switch-desktop changes to desktop 2 (1-indexed display)"

echo ""

# --- Test: switch-desktop back ---

echo "--- switch-desktop back ---"
"$CTL_BIN" switch-desktop 0 2>&1 || true
sleep 0.2
output=$("$CTL_BIN" desktop 2>&1) || true
assert_contains "$output" "1" "switch-desktop back to desktop 1"

echo ""

# --- Test: config ---

echo "--- config ---"
output=$("$CTL_BIN" config 2>&1) || true
assert_contains "$output" "config.toml" "config command shows path"

echo ""

# --- Test: socket is writable ---

echo "--- socket connectivity ---"
if [ -S "$XDG_RUNTIME_DIR/fynedesk.sock" ]; then
    pass "IPC socket exists and is a socket file"
else
    fail "IPC socket exists and is a socket file"
fi

echo ""

# --- Test: compositor is still alive ---

echo "--- compositor health ---"
if kill -0 "$COMP_PID" 2>/dev/null; then
    pass "Compositor still running after all tests"
else
    fail "Compositor still running after all tests" "PID $COMP_PID exited"
fi

echo ""

# --- Summary ---

echo "=== Results: $PASS/$TOTAL passed, $FAIL failed ==="

if [ "$FAIL" -gt 0 ]; then
    echo ""
    echo "Compositor log:"
    tail -20 /tmp/integration-test-compositor.log
    exit 1
fi

echo "All integration tests passed."
