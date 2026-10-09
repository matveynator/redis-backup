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

func TestSFTPHostOverridesConfiguredTargets(t *testing.T) {
	oldAccounts := sftpAccounts
	oldConf := sftpConfFile
	oldHost := sftpHost
	oldPort := sftpPort
	oldUser := sftpUser
	oldKey := sftpKeyFile
	oldKnownHosts := sftpKnownHosts
	oldRoot := sftpRoot
	defer func() {
		sftpAccounts = oldAccounts
		sftpConfFile = oldConf
		sftpHost = oldHost
		sftpPort = oldPort
		sftpUser = oldUser
		sftpKeyFile = oldKey
		sftpKnownHosts = oldKnownHosts
		sftpRoot = oldRoot
	}()

	file := filepath.Join(t.TempDir(), "sftp.conf")
	content := `
SFTP_HOST=old1.example.com
SFTP_USER=old1

SFTP_HOST=old2.example.com
SFTP_USER=old2
`
	if err := os.WriteFile(file, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	sftpConfFile = file
	sftpHost = "override.example.com"
	sftpPort = 2222
	sftpUser = "override"
	sftpKeyFile = "/root/.ssh/override"
	sftpKnownHosts = "/root/.ssh/known_hosts"
	sftpRoot = "/override-root"

	gotAccounts := loadSFTPAccounts()
	if len(gotAccounts) != 1 {
		t.Fatalf("got %d accounts, want exactly 1 CLI override account: %+v", len(gotAccounts), gotAccounts)
	}
	got := gotAccounts[0]
	if got.Host != "override.example.com" || got.Port != 2222 || got.User != "override" || got.KeyFile != "/root/.ssh/override" || got.KnownHosts != "/root/.ssh/known_hosts" || got.Root != "/override-root" {
		t.Fatalf("unexpected override account: %+v", got)
	}
}

func TestWritableSFTPRootCandidatesAuto(t *testing.T) {
	got := writableSFTPRootCandidates(sftpAccount{Root: "auto", User: "backup08"})
	want := []string{".", "/data", "/backup", "/backups", "/upload", "/uploads", "/home/backup08"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates = %#v, want %#v", got, want)
	}
}

func TestWritableSFTPRootCandidatesExplicit(t *testing.T) {
	got := writableSFTPRootCandidates(sftpAccount{Root: "/data"})
	want := []string{"/data"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates = %#v, want %#v", got, want)
	}
}

func TestExplicitSFTPRootCreatedBeforeProbe(t *testing.T) {
	oldProbe := sftpProbeBatch
	defer func() { sftpProbeBatch = oldProbe }()

	var commands string
	sftpProbeBatch = func(_ sftpAccount, batch string) (string, error) {
		commands = batch
		return "", nil
	}

	root, err := resolveWritableSFTPRoot(sftpAccount{Root: "/data/backups"})
	if err != nil {
		t.Fatal(err)
	}
	if root != "/data/backups" {
		t.Fatalf("root = %q, want /data/backups", root)
	}
	for _, want := range []string{
		`-mkdir "/data"`,
		`-mkdir "/data/backups"`,
		`mkdir "/data/backups/.redis-backup-write-test-`,
	} {
		if !strings.Contains(commands, want) {
			t.Fatalf("probe commands %q do not contain %q", commands, want)
		}
	}
}

func TestTerminalSFTPProbeErrorStopsAutoCandidates(t *testing.T) {
	oldProbe := sftpProbeBatch
	defer func() { sftpProbeBatch = oldProbe }()

	calls := 0
	sftpProbeBatch = func(_ sftpAccount, _ string) (string, error) {
		calls++
		return "", errors.New("ssh: Could not resolve hostname backup.invalid: Name or service not known")
	}

	_, err := resolveWritableSFTPRoot(sftpAccount{Root: "auto", Host: "backup.invalid", User: "backup08"})
	if err == nil {
		t.Fatal("expected probe failure")
	}
	if calls != 1 {
		t.Fatalf("probe calls = %d, want 1 after terminal connection failure", calls)
	}
}

func TestDirectoryPermissionFailureTriesNextAutoCandidate(t *testing.T) {
	oldProbe := sftpProbeBatch
	defer func() { sftpProbeBatch = oldProbe }()

	calls := 0
	sftpProbeBatch = func(_ sftpAccount, _ string) (string, error) {
		calls++
		if calls == 1 {
			return "", errors.New("remote mkdir \".redis-backup-write-test\": Permission denied")
		}
		return "", nil
	}

	root, err := resolveWritableSFTPRoot(sftpAccount{Root: "auto", User: "backup08"})
	if err != nil {
		t.Fatal(err)
	}
	if root != "/data" {
		t.Fatalf("root = %q, want /data", root)
	}
	if calls != 2 {
		t.Fatalf("probe calls = %d, want 2", calls)
	}
}

func TestTerminalSFTPProbeClassification(t *testing.T) {
	terminal := []string{
		"Could not resolve hostname backup.invalid: Name or service not known",
		"connect to host example port 22: Connection refused",
		"ssh: connect to host example port 22: Connection timed out",
		"backup@example: Permission denied (publickey).",
		"Host key verification failed.",
		"REMOTE HOST IDENTIFICATION HAS CHANGED!",
		"Couldn't read packet: Connection reset by peer",
	}
	for _, msg := range terminal {
		if !isTerminalSFTPProbeError(errors.New(msg)) {
			t.Errorf("expected terminal classification for %q", msg)
		}
	}
	if isTerminalSFTPProbeError(errors.New(`remote mkdir "/data": Permission denied`)) {
		t.Fatal("directory permission error must remain path-level, not terminal")
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

func TestRemoteSFTPPathChrootCurrentDirectory(t *testing.T) {
	acc := sftpAccount{Root: "."}
	got := remoteSFTPPath(acc, "server-a/redis-backup/redis_6379/daily/a.tar.gz")
	want := "server-a/redis-backup/redis_6379/daily/a.tar.gz"
	if got != want {
		t.Fatalf("remoteSFTPPath = %q, want %q", got, want)
	}
}

func TestMkdirBatchAbsolute(t *testing.T) {
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

func TestMkdirBatchRelative(t *testing.T) {
	got := strings.Split(strings.TrimSpace(mkdirBatch("a/b/c")), "\n")
	want := []string{
		`-mkdir "a"`,
		`-mkdir "a/b"`,
		`-mkdir "a/b/c"`,
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
	if got := normalizedSFTPRoot(""); got != "auto" {
		t.Fatalf("root = %q, want auto", got)
	}
	if got := normalizedSFTPRoot("data"); got != "/data" {
		t.Fatalf("root = %q, want /data", got)
	}
}
