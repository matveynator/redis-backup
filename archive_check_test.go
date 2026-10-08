//go:build !windows
// +build !windows

package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/binary"
	"fmt"
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

func writeTestMeta(t *testing.T, archive string, originalSize int64) {
	t.Helper()
	data := []byte(fmt.Sprintf("{\"original_size\":%d,\"snapshot_time\":1}\n", originalSize))
	if err := os.WriteFile(archive+".meta", data, 0600); err != nil {
		t.Fatal(err)
	}
}

func makeTestRDB(body []byte, checksum bool) []byte {
	data := append([]byte("REDIS0011"), body...)
	data = append(data, redisRDBEOF)
	var footer [8]byte
	if checksum {
		binary.LittleEndian.PutUint64(footer[:], redisCRC64Update(0, data))
	}
	return append(data, footer[:]...)
}

func TestValidateBackupArchiveFastValid(t *testing.T) {
	payload := makeTestRDB(nil, true)
	path := writeTestArchive(t, payload)
	writeTestMeta(t, path, int64(len(payload)))
	if err := validateBackupArchive(path); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateBackupArchiveFastRejectsNonRedisPayload(t *testing.T) {
	path := writeTestArchive(t, []byte("not a redis database"))
	if err := validateBackupArchive(path); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateBackupArchiveFastRejectsUndersizedSnapshot(t *testing.T) {
	payload := makeTestRDB(nil, true)
	path := writeTestArchive(t, payload)
	writeTestMeta(t, path, int64(len(payload))*2)
	if err := validateBackupArchive(path); err == nil {
		t.Fatal("expected snapshot size error")
	}
}

func TestValidateBackupArchiveDeepValidChecksumDisabled(t *testing.T) {
	path := writeTestArchive(t, makeTestRDB(nil, false))
	if err := validateBackupArchiveDeep(path); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateBackupArchiveDeepRejectsTruncatedArchive(t *testing.T) {
	path := writeTestArchive(t, makeTestRDB([]byte{1, 2, 3, 4, 5}, true))
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
	if err := validateBackupArchiveDeep(path); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateBackupArchiveDeepRejectsRecompressedPartialRDB(t *testing.T) {
	full := makeTestRDB([]byte{1, 2, 3, 4, 5}, true)
	partial := append([]byte(nil), full[:len(full)-10]...)
	path := writeTestArchive(t, partial)
	if err := validateBackupArchiveDeep(path); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateBackupArchiveDeepRejectsBodyCorruption(t *testing.T) {
	payload := makeTestRDB([]byte{1, 2, 3, 4, 5}, true)
	payload[10] ^= 0xff
	path := writeTestArchive(t, payload)
	if err := validateBackupArchiveDeep(path); err == nil {
		t.Fatal("expected checksum error")
	}
}

func TestValidateBackupArchiveDeepRejectsMissingEOF(t *testing.T) {
	payload := makeTestRDB(nil, true)
	payload[len(payload)-9] = 0x00
	binary.LittleEndian.PutUint64(payload[len(payload)-8:], redisCRC64Update(0, payload[:len(payload)-8]))
	path := writeTestArchive(t, payload)
	if err := validateBackupArchiveDeep(path); err == nil {
		t.Fatal("expected EOF error")
	}
}

func TestRedisCRC64KnownVector(t *testing.T) {
	const want uint64 = 0xe9c6d914c4b8d9ca
	if got := redisCRC64Update(0, []byte("123456789")); got != want {
		t.Fatalf("crc = %016x, want %016x", got, want)
	}
	got := redisCRC64Update(0, []byte("1234"))
	got = redisCRC64Update(got, []byte("56789"))
	if got != want {
		t.Fatalf("incremental crc = %016x, want %016x", got, want)
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
