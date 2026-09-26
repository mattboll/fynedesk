package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestPruneCrashLogs(t *testing.T) {
	dir := t.TempDir()
	for i := 1; i <= 13; i++ {
		name := filepath.Join(dir, fmt.Sprintf("compositor-crash-2026-09-%02dT10-00-00.log", i))
		if err := os.WriteFile(name, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	pruneCrashLogs(dir, 10)
	logs, _ := filepath.Glob(filepath.Join(dir, "compositor-crash-*.log"))
	if len(logs) != 10 || filepath.Base(logs[0]) != "compositor-crash-2026-09-04T10-00-00.log" {
		t.Errorf("kept %d, oldest %s", len(logs), filepath.Base(logs[0]))
	}
}
