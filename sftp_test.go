//go:build !windows
// +build !windows

package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseSFTPConfMultipleHosts(t *testing.T) {
	old := sftpAccounts
	defer func() { sftpAccounts = old }()
	sftpAccounts = nil

	file := filepath.Join(t.TempDir(), "sftp.conf")
	content := `
# first target
SFTP_HOST=backup1.example.com
SFTP_PORT=2222
SFTP_USER=backup01
SFTP_KEY=/root/.ssh/backup01
SFTP_ROOT=/backup-a

SFTP_HOST=backup2.example.com
SFTP_USER=backup02
SFTP_KNOWN_HOSTS=/etc/ssh/known_hosts.backup
SFTP_ROOT=backup-b
`
	if err := os.WriteFile(file, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	if err := parseSFTPConf(file); err != nil {
		t.Fatal(err)
	}
	if len(sftpAccounts) != 2 {
		t.Fatalf("got %d accounts, want 2", len(sftpAccounts))
	}
	if got := sftpAccounts[0]; got.Host != "backup1.example.com" || got.Port != 2222 || got.User != "backup01" || got.KeyFile != "/root/.ssh/backup01" || got.Root != "/backup-a" {
		t.Fatalf("unexpected first account: %+v", got)
	}
	if got := sftpAccounts[1]; got.Host != "backup2.example.com" || got.Port != 22 || got.User != "backup02" || got.KnownHosts != "/etc/ssh/known_hosts.backup" || got.Root != "/backup-b" {
		t.Fatalf("unexpected second account: %+v", got)
	}
}

func TestRemoteSFTPPath(t *testing.T) {
	acc := sftpAccount{Root: "/remote/root"}
	got := remoteSFTPPath(acc, "server-a/redis-backup/redis_6379/daily/a.tar.gz")
	want := "/remote/root/server-a/redis-backup/redis_6379/daily/a.tar.gz"
	if got != want {
		t.Fatalf("remoteSFTPPath = %q, want %q", got, want)
	}
}

func TestMkdirBatch(t *testing.T) {
	got := strings.Split(strings.TrimSpace(mkdirBatch("/a/b/c")), "\n")
	want := []string{
		`-mkdir "/a"`,
		`-mkdir "/a/b"`,
		`-mkdir "/a/b/c"`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mkdirBatch = %#v, want %#v", got, want)
	}
}

func TestArchiveTimeFromName(t *testing.T) {
	got, ok := archiveTimeFromName("2026-10-09_01-02-03_redis_6379.tar.gz")
	if !ok {
		t.Fatal("expected valid archive timestamp")
	}
	want := time.Date(2026, 10, 9, 1, 2, 3, 0, time.Local)
	if !got.Equal(want) {
		t.Fatalf("time = %v, want %v", got, want)
	}
	if _, ok := archiveTimeFromName("bad-name.tar.gz"); ok {
		t.Fatal("invalid archive name accepted")
	}
}

func TestNormalizedSFTPDefaults(t *testing.T) {
	if got := normalizedSFTPPort(0); got != 22 {
		t.Fatalf("port = %d, want 22", got)
	}
	if got := normalizedSFTPRoot("backup"); got != "/backup" {
		t.Fatalf("root = %q, want /backup", got)
	}
}
