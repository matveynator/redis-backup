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
	base := filepath.Join(backupPath, host, backupSubdir, "redis_6379")
	for _, name := range []string{"weekly", "monthly", "yearly"} {
		dir := filepath.Join(base, name)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "legacy.tar.gz"), []byte("legacy"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	first := time.Date(2026, 10, 4, 0, 45, 1, 0, time.Local)
	second := first.Add(8 * time.Hour)
	if got := backupInstance("6379", rdb, host, first); got == "" {
		t.Fatal("first backup failed")
	}
	if got := backupInstance("6379", rdb, host, second); got == "" {
		t.Fatal("second backup failed")
	}

	archives, err := filepath.Glob(filepath.Join(base, "daily", "*.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	if len(archives) != 1 {
		t.Fatalf("daily archives = %d, want 1", len(archives))
	}
	meta, err := filepath.Glob(filepath.Join(base, "daily", "*.tar.gz.meta"))
	if err != nil {
		t.Fatal(err)
	}
	if len(meta) != 1 {
		t.Fatalf("daily metadata files = %d, want 1", len(meta))
	}
	for _, name := range []string{"weekly", "monthly", "yearly"} {
		if _, err := os.Stat(filepath.Join(base, name)); !os.IsNotExist(err) {
			t.Fatalf("%s directory should not exist in --copies mode", name)
		}
	}
}
