#!/bin/bash
# Run this when Firefox (or another client) freezes in the compositor.
# Usage: ./diagnose-freeze.sh [app-name]  (default: firefox)

APP="${1:-firefox}"
OUT="$HOME/freeze-report-$(date +%Y%m%d-%H%M%S).txt"

echo "=== FyneDesk Freeze Diagnostic ===" | tee "$OUT"
echo "Date: $(date)" | tee -a "$OUT"
echo "App: $APP" | tee -a "$OUT"
echo "" | tee -a "$OUT"

# Find PIDs
PIDS=$(pgrep -f "$APP" | head -10)
if [ -z "$PIDS" ]; then
    echo "No process found for '$APP'" | tee -a "$OUT"
    exit 1
fi
MAIN_PID=$(pgrep -o -f "$APP")
echo "PIDs: $PIDS (main: $MAIN_PID)" | tee -a "$OUT"

echo "" | tee -a "$OUT"
echo "=== Process State ===" | tee -a "$OUT"
ps -eo pid,stat,wchan:20,%cpu,%mem,comm | head -1 | tee -a "$OUT"
for pid in $PIDS; do
    ps -p "$pid" -o pid,stat,wchan:20,%cpu,%mem,comm --no-headers 2>/dev/null | tee -a "$OUT"
done

echo "" | tee -a "$OUT"
echo "=== Kernel Stack (main PID) ===" | tee -a "$OUT"
sudo cat "/proc/$MAIN_PID/stack" 2>/dev/null | tee -a "$OUT" || echo "(need sudo)" | tee -a "$OUT"

echo "" | tee -a "$OUT"
echo "=== Open FDs ===" | tee -a "$OUT"
FD_COUNT=$(ls "/proc/$MAIN_PID/fd" 2>/dev/null | wc -l)
echo "Count: $FD_COUNT" | tee -a "$OUT"

echo "" | tee -a "$OUT"
echo "=== GPU/DRM errors (last 30 dmesg lines) ===" | tee -a "$OUT"
dmesg --time-format iso 2>/dev/null | tail -30 | grep -iE 'gpu|drm|hang|reset|error|fence|timeout|oom|kill' | tee -a "$OUT"
if [ $? -ne 0 ]; then
    echo "(none)" | tee -a "$OUT"
fi

echo "" | tee -a "$OUT"
echo "=== XWayland Check ===" | tee -a "$OUT"
XWAY_PID=$(pgrep Xwayland)
if [ -n "$XWAY_PID" ]; then
    echo "Xwayland running (PID $XWAY_PID)" | tee -a "$OUT"
    XWAY_DISPLAY=$(ls /tmp/.X11-unix/ 2>/dev/null | head -1 | tr -d X)
    if [ -n "$XWAY_DISPLAY" ]; then
        xlsclients -display ":$XWAY_DISPLAY" 2>/dev/null | grep -i "$APP" | tee -a "$OUT"
        if [ $? -eq 0 ]; then
            echo "$APP is running via XWayland" | tee -a "$OUT"
        else
            echo "$APP may be running as native Wayland" | tee -a "$OUT"
        fi
    fi
else
    echo "No Xwayland running" | tee -a "$OUT"
fi

echo "" | tee -a "$OUT"
echo "=== Compositor Log (last 30 lines) ===" | tee -a "$OUT"
COMP_LOG="$HOME/.cache/fyne/com.fyshos.fynedesk/compositor.log"
if [ -f "$COMP_LOG" ]; then
    tail -30 "$COMP_LOG" | tee -a "$OUT"
else
    echo "(not found at $COMP_LOG)" | tee -a "$OUT"
fi

echo "" | tee -a "$OUT"
echo "=== Memory Pressure ===" | tee -a "$OUT"
free -h | tee -a "$OUT"

echo "" | tee -a "$OUT"
echo "=== Report saved to $OUT ==="
