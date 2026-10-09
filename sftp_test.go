//go:build !windows
// +build !windows

package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type fakeFileInfo struct {
	name string
	dir  bool
}

func (f fakeFileInfo) Name() string       { return f.name }
func (f fakeFileInfo) Size() int64        { return 0 }
func (f fakeFileInfo) Mode() os.FileMode  { if f.dir { return os.ModeDir | 0755 }; return 0644 }
func (f fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (f fakeFileInfo) IsDir() bool        { return f.dir }
func (f fakeFileInfo) Sys() any           { return nil }

type fakeSFTPFS struct {
	entries    []os.FileInfo
	mkdirErr   map[string]error
	mkdirCalls []string
	mkdirAll   []string
}

func (f *fakeSFTPFS) Mkdir(name string) error {
	f.mkdirCalls = append(f.mkdirCalls, name)
	for prefix, err := range f.mkdirErr {
		if strings.HasPrefix(name, prefix) {
			return err
		}
	}
	return nil
}

func (f *fakeSFTPFS) RemoveDirectory(string) error { return nil }
func (f *fakeSFTPFS) ReadDir(string) ([]os.FileInfo, error) { return f.entries, nil }
func (f *fakeSFTPFS) MkdirAll(name string) error {
	f.mkdirAll = append(f.mkdirAll, name)
	return nil
}

func TestParseSFTPConfMultipleHosts(t *testing.T) {
	old := sftpAccounts
	defer func() { sftpAccounts = old }()
	sftpAccounts = nil

	file := filepath.Join(t.TempDir(), "sftp.conf")
	content := `
SFTP_HOST=backup1.example.com
SFTP_PORT=2222
SFTP_USER=backup01
SFTP_KEY=/root/.ssh/backup01
SFTP_ROOT=/backup-a

SFTP_HOST=backup2.example.com
SFTP_USER=backup02
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
	if got := sftpAccounts[0]; got.Host != "backup1.example.com" || got.Port != 2222 || got.User != "backup01" || got.Root != "/backup-a" {
		t.Fatalf("unexpected first account: %+v", got)
	}
	if got := sftpAccounts[1]; got.Host != "backup2.example.com" || got.Port != 22 || got.User != "backup02" || got.Root != "/backup-b" {
		t.Fatalf("unexpected second account: %+v", got)
	}
}

func TestSFTPHostOverridesConfiguredTargets(t *testing.T) {
	oldAccounts, oldConf, oldHost := sftpAccounts, sftpConfFile, sftpHost
	oldPort, oldUser, oldKey, oldRoot := sftpPort, sftpUser, sftpKeyFile, sftpRoot
	defer func() {
		sftpAccounts, sftpConfFile, sftpHost = oldAccounts, oldConf, oldHost
		sftpPort, sftpUser, sftpKeyFile, sftpRoot = oldPort, oldUser, oldKey, oldRoot
	}()

	file := filepath.Join(t.TempDir(), "sftp.conf")
	if err := os.WriteFile(file, []byte("SFTP_HOST=old.example.com\nSFTP_USER=old\n"), 0600); err != nil {
		t.Fatal(err)
	}
	sftpConfFile = file
	sftpHost = "override.example.com"
	sftpPort = 2222
	sftpUser = "override"
	sftpKeyFile = "/root/.ssh/override"
	sftpRoot = "/override-root"

	got := loadSFTPAccounts()
	if len(got) != 1 || got[0].Host != "override.example.com" || got[0].Port != 2222 || got[0].Root != "/override-root" {
		t.Fatalf("unexpected override accounts: %+v", got)
	}
}

func TestDiscoverSFTPRootCandidatesUsesVisibleDirectoriesOnly(t *testing.T) {
	fs := &fakeSFTPFS{entries: []os.FileInfo{
		fakeFileInfo{name: "zeta", dir: true},
		fakeFileInfo{name: "file.txt", dir: false},
		fakeFileInfo{name: "data", dir: true},
	}}
	got, err := discoverSFTPRootCandidates(fs)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"data", "zeta"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates = %#v, want %#v", got, want)
	}
}

func TestAutoRootUsesCurrentDirectoryWhenWritable(t *testing.T) {
	fs := &fakeSFTPFS{}
	root, err := resolveWritableSFTPRootWithFS(sftpAccount{Root: "auto"}, fs)
	if err != nil {
		t.Fatal(err)
	}
	if root != "." {
		t.Fatalf("root = %q, want .", root)
	}
}

func TestAutoRootListsAndTriesOnlyExistingDirectories(t *testing.T) {
	fs := &fakeSFTPFS{
		entries: []os.FileInfo{
			fakeFileInfo{name: "readonly", dir: true},
			fakeFileInfo{name: "writable", dir: true},
		},
		mkdirErr: map[string]error{
			".redis-backup-write-test": errors.New("permission denied"),
			"readonly":                errors.New("permission denied"),
		},
	}
	root, err := resolveWritableSFTPRootWithFS(sftpAccount{Root: "auto"}, fs)
	if err != nil {
		t.Fatal(err)
	}
	if root != "writable" {
		t.Fatalf("root = %q, want writable", root)
	}
	for _, call := range fs.mkdirCalls {
		if call == "/data" || call == "/backup" || call == "/uploads" {
			t.Fatalf("unexpected guessed directory probe: %q", call)
		}
	}
}

func TestExplicitRootIsCreatedBeforeProbe(t *testing.T) {
	fs := &fakeSFTPFS{}
	root, err := resolveWritableSFTPRootWithFS(sftpAccount{Root: "/data/backups"}, fs)
	if err != nil {
		t.Fatal(err)
	}
	if root != "/data/backups" {
		t.Fatalf("root = %q, want /data/backups", root)
	}
	if !reflect.DeepEqual(fs.mkdirAll, []string{"/data/backups"}) {
		t.Fatalf("MkdirAll calls = %#v", fs.mkdirAll)
	}
}

func TestRemoteSFTPPath(t *testing.T) {
	if got := remoteSFTPPath(sftpAccount{Root: "/remote/root"}, "server/redis-backup/a.tar.gz"); got != "/remote/root/server/redis-backup/a.tar.gz" {
		t.Fatalf("path = %q", got)
	}
	if got := remoteSFTPPath(sftpAccount{Root: "."}, "server/redis-backup/a.tar.gz"); got != "server/redis-backup/a.tar.gz" {
		t.Fatalf("chroot path = %q", got)
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
}

func TestNormalizedSFTPDefaults(t *testing.T) {
	if got := normalizedSFTPPort(0); got != 22 {
		t.Fatalf("port = %d, want 22", got)
	}
	if got := normalizedSFTPRoot(""); got != "auto" {
		t.Fatalf("root = %q, want auto", got)
	}
}
