//go:build !windows
// +build !windows

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func writeTestArchive(t *testing.T, payload []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "backup.tar.gz")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "dump.rdb", Mode: 0600, Size: int64(len(payload))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestValidateBackupArchiveValid(t *testing.T) {
	path := writeTestArchive(t, append([]byte("REDIS0011"), bytes.Repeat([]byte{0x42}, 1024)...))
	if err := validateBackupArchive(path); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateBackupArchiveRejectsNonRedisPayload(t *testing.T) {
	path := writeTestArchive(t, []byte("not a redis database"))
	if err := validateBackupArchive(path); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateBackupArchiveRejectsTruncatedArchive(t *testing.T) {
	path := writeTestArchive(t, append([]byte("REDIS0011"), bytes.Repeat([]byte{0x42}, 8192)...))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 16 {
		t.Fatal("archive unexpectedly small")
	}
	if err := os.WriteFile(path, data[:len(data)-8], 0600); err != nil {
		t.Fatal(err)
	}
	if err := validateBackupArchive(path); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestRedisRDBHeader(t *testing.T) {
	for _, tc := range []struct {
		header string
		ok     bool
	}{
		{"REDIS0011", true},
		{"REDIS0009", true},
		{"REDIS00A1", false},
		{"NOTRD0011", false},
		{"REDIS001", false},
	} {
		if got := isRedisRDBHeader([]byte(tc.header)); got != tc.ok {
			t.Fatalf("isRedisRDBHeader(%q) = %v, want %v", tc.header, got, tc.ok)
		}
	}
}
