//go:build !windows
// +build !windows

package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCopiesModeKeepsOnlyDailySnapshots(t *testing.T) {
	oldBackupPath, oldMaxCopies, oldKeepDays := backupPath, maxCopies, keepDays
	defer func() {
		backupPath, maxCopies, keepDays = oldBackupPath, oldMaxCopies, oldKeepDays
	}()

	backupPath = t.TempDir()
	maxCopies = 1
	keepDays = 30
	rdb := filepath.Join(t.TempDir(), "dump.rdb")
	if err := os.WriteFile(rdb, []byte("REDIS0011-test"), 0644); err != nil {
		t.Fatal(err)
	}

	host := "redis01"
	first := time.Date(2026, 10, 4, 0, 45, 1, 0, time.Local) // Sunday
	second := first.Add(8 * time.Hour)
	if got := backupInstance("6379", rdb, host, first); got == "" {
		t.Fatal("first backup failed")
	}
	if got := backupInstance("6379", rdb, host, second); got == "" {
		t.Fatal("second backup failed")
	}

	base := filepath.Join(backupPath, host, backupSubdir, "redis_6379")
	archives, err := filepath.Glob(filepath.Join(base, "daily", "*.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	if len(archives) != 1 {
		t.Fatalf("daily archives = %d, want 1", len(archives))
	}
	for _, name := range []string{"weekly", "monthly", "yearly"} {
		if _, err := os.Stat(filepath.Join(base, name)); !os.IsNotExist(err) {
			t.Fatalf("%s directory should not exist in --copies mode", name)
		}
	}
}
