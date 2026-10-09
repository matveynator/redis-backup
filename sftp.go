//go:build !windows
// +build !windows

package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

type sftpAccount struct {
	Host       string
	Port       int
	User       string
	KeyFile    string
	KnownHosts string
	Root       string
}

type sftpCheckResult struct {
	Problems    []string
	Severity    int
	LatestFiles int
}

type nativeSFTPSession struct {
	ssh  *ssh.Client
	sftp *sftp.Client
}

func (s *nativeSFTPSession) Close() {
	if s == nil {
		return
	}
	if s.sftp != nil {
		_ = s.sftp.Close()
	}
	if s.ssh != nil {
		_ = s.ssh.Close()
	}
}

type sftpDiscoveryFS interface {
	Mkdir(string) error
	RemoveDirectory(string) error
	ReadDir(string) ([]os.FileInfo, error)
	MkdirAll(string) error
}

var (
	sftpConfFile     string
	sftpHost         string
	sftpPort         int
	sftpUser         string
	sftpKeyFile      string
	sftpKnownHosts   string
	sftpRoot         string
	sftpKeepFactor   int
	sftpAccounts     []sftpAccount
	sftpEnabled      bool
	sftpInitProblems []string
)

var openNativeSFTP = dialNativeSFTP

func init() {
	flag.StringVar(&sftpConfFile, "sftp-conf", "/etc/sftp-backup.conf", "Path to SFTP configuration file")
	flag.StringVar(&sftpHost, "sftp-host", "", "Override SFTP host")
	flag.IntVar(&sftpPort, "sftp-port", 22, "SFTP port")
	flag.StringVar(&sftpUser, "sftp-user", "", "SFTP username")
	flag.StringVar(&sftpKeyFile, "sftp-key", "", "SFTP private key file (optional; SSH agent/default keys are used when empty)")
	flag.StringVar(&sftpKnownHosts, "sftp-known-hosts", "", "Deprecated: host keys are accepted automatically")
	flag.StringVar(&sftpRoot, "sftp-root", "auto", "Remote root directory for backups (default: discover writable directory)")
	flag.IntVar(&sftpKeepFactor, "sftp-keep-factor", 4, "Retention multiplier for SFTP")
}

func initSFTP() {
	sftpInitProblems = nil
	configured := loadSFTPAccounts()
	sftpAccounts = nil
	for _, acc := range configured {
		log.Printf("%s🔎 SFTP probing %s:%d (user %s) for a writable directory%s", cyan, acc.Host, acc.Port, acc.User, reset)
		root, err := resolveWritableSFTPRoot(acc)
		if err != nil {
			problem := fmt.Sprintf("SFTP %s:%d user %s: %v", acc.Host, acc.Port, acc.User, err)
			sftpInitProblems = append(sftpInitProblems, problem)
			log.Printf("%s%s; target disabled for this run%s", red, problem, reset)
			continue
		}
		acc.Root = root
		sftpAccounts = append(sftpAccounts, acc)
		log.Printf("%s🔐 SFTP replication target → %s:%d (user %s), writable root %s%s", cyan, acc.Host, acc.Port, acc.User, acc.Root, reset)
	}
	sftpEnabled = len(configured) > 0
}

func loadSFTPAccounts() []sftpAccount {
	if sftpHost != "" {
		return []sftpAccount{{
			Host:       sftpHost,
			Port:       normalizedSFTPPort(sftpPort),
			User:       sftpUser,
			KeyFile:    sftpKeyFile,
			KnownHosts: sftpKnownHosts,
			Root:       normalizedSFTPRoot(sftpRoot),
		}}
	}

	sftpAccounts = nil
	if sftpConfFile != "" {
		if _, err := os.Stat(sftpConfFile); err == nil {
			if err := parseSFTPConf(sftpConfFile); err != nil {
				log.Printf("%sSFTP config %s: %v%s", yellow, sftpConfFile, err, reset)
			}
		}
	}
	return append([]sftpAccount(nil), sftpAccounts...)
}

func parseSFTPConf(file string) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()

	var cur sftpAccount
	commit := func() {
		if cur.Host == "" {
			return
		}
		cur.Port = normalizedSFTPPort(cur.Port)
		cur.Root = normalizedSFTPRoot(cur.Root)
		sftpAccounts = append(sftpAccounts, cur)
		cur = sftpAccount{}
	}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		kv := strings.SplitN(line, "=", 2)
		key := strings.TrimSpace(kv[0])
		val := strings.Trim(strings.TrimSpace(kv[1]), "\"")
		if key == "SFTP_HOST" && cur.Host != "" {
			commit()
		}
		switch key {
		case "SFTP_HOST":
			cur.Host = val
		case "SFTP_PORT":
			if p, err := strconv.Atoi(val); err == nil {
				cur.Port = p
			}
		case "SFTP_USER":
			cur.User = val
		case "SFTP_KEY":
			cur.KeyFile = val
		case "SFTP_KNOWN_HOSTS":
			cur.KnownHosts = val
		case "SFTP_ROOT":
			cur.Root = val
		}
	}
	commit()
	return scanner.Err()
}

func normalizedSFTPPort(p int) int {
	if p <= 0 || p > 65535 {
		return 22
	}
	return p
}

func normalizedSFTPRoot(root string) string {
	root = strings.TrimSpace(root)
	if root == "" || strings.EqualFold(root, "auto") {
		return "auto"
	}
	if root == "." {
		return "."
	}
	if !strings.HasPrefix(root, "/") {
		root = "/" + root
	}
	return path.Clean(root)
}

func loadSSHSigner(file string) (ssh.Signer, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	return ssh.ParsePrivateKey(data)
}

func sshAuthMethods(acc sftpAccount) ([]ssh.AuthMethod, io.Closer, error) {
	var methods []ssh.AuthMethod
	var agentConn net.Conn

	if acc.KeyFile != "" {
		signer, err := loadSSHSigner(acc.KeyFile)
		if err != nil {
			return nil, nil, fmt.Errorf("read SFTP key %s: %w", acc.KeyFile, err)
		}
		methods = append(methods, ssh.PublicKeys(signer))
	} else {
		if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
			if conn, err := net.DialTimeout("unix", sock, 3*time.Second); err == nil {
				agentConn = conn
				methods = append(methods, ssh.PublicKeysCallback(agent.NewClient(conn).Signers))
			}
		}
		home, _ := os.UserHomeDir()
		for _, name := range []string{"id_ed25519", "id_ecdsa", "id_rsa"} {
			if home == "" {
				break
			}
			file := filepath.Join(home, ".ssh", name)
			if signer, err := loadSSHSigner(file); err == nil {
				methods = append(methods, ssh.PublicKeys(signer))
			}
		}
	}

	if len(methods) == 0 {
		if agentConn != nil {
			_ = agentConn.Close()
		}
		return nil, nil, fmt.Errorf("no usable SSH private key or agent found")
	}
	return methods, agentConn, nil
}

func dialNativeSFTP(acc sftpAccount) (*nativeSFTPSession, error) {
	methods, agentConn, err := sshAuthMethods(acc)
	if err != nil {
		return nil, err
	}
	if agentConn != nil {
		defer agentConn.Close()
	}

	config := &ssh.ClientConfig{
		User:            acc.User,
		Auth:            methods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}
	addr := net.JoinHostPort(acc.Host, strconv.Itoa(normalizedSFTPPort(acc.Port)))
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("connect %s: %w", addr, err)
	}
	_ = conn.SetDeadline(time.Now().Add(20 * time.Second))
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, config)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("SSH %s: %w", addr, err)
	}
	_ = conn.SetDeadline(time.Time{})
	sshClient := ssh.NewClient(sshConn, chans, reqs)
	sftpClient, err := sftp.NewClient(sshClient)
	if err != nil {
		_ = sshClient.Close()
		return nil, fmt.Errorf("SFTP subsystem %s: %w", addr, err)
	}
	return &nativeSFTPSession{ssh: sshClient, sftp: sftpClient}, nil
}

func remoteJoin(root, rel string) string {
	rel = strings.TrimPrefix(filepath.ToSlash(rel), "/")
	if root == "." || root == "auto" || root == "" {
		return path.Clean(rel)
	}
	return path.Join(root, rel)
}

func ensureRemoteDir(client sftpDiscoveryFS, dir string) error {
	dir = path.Clean(dir)
	if dir == "." || dir == "/" {
		return nil
	}
	return client.MkdirAll(dir)
}

func probeWritableDir(client sftpDiscoveryFS, dir string) error {
	probe := fmt.Sprintf(".redis-backup-write-test-%d-%d", os.Getpid(), time.Now().UnixNano())
	probePath := probe
	if dir != "." {
		probePath = path.Join(dir, probe)
	}
	if err := client.Mkdir(probePath); err != nil {
		return err
	}
	return client.RemoveDirectory(probePath)
}

func discoverSFTPRootCandidates(client sftpDiscoveryFS) ([]string, error) {
	entries, err := client.ReadDir(".")
	if err != nil {
		return nil, err
	}
	var dirs []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == "." || name == ".." || name == "" {
			continue
		}
		dirs = append(dirs, name)
	}
	sort.Strings(dirs)
	return dirs, nil
}

func resolveWritableSFTPRootWithFS(acc sftpAccount, client sftpDiscoveryFS) (string, error) {
	root := normalizedSFTPRoot(acc.Root)
	if root != "auto" {
		if err := ensureRemoteDir(client, root); err != nil {
			return "", fmt.Errorf("create SFTP root %s: %w", root, err)
		}
		if err := probeWritableDir(client, root); err != nil {
			return "", fmt.Errorf("SFTP root %s is not writable: %w", root, err)
		}
		return root, nil
	}

	if err := probeWritableDir(client, "."); err == nil {
		return ".", nil
	}

	candidates, err := discoverSFTPRootCandidates(client)
	if err != nil {
		return "", fmt.Errorf("list SFTP directories: %w", err)
	}
	var failures []string
	for _, candidate := range candidates {
		if err := probeWritableDir(client, candidate); err == nil {
			return candidate, nil
		} else {
			failures = append(failures, fmt.Sprintf("%s: %v", candidate, err))
		}
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("current SFTP directory is not writable and contains no subdirectories")
	}
	return "", fmt.Errorf("no writable SFTP root among visible directories (%s)", strings.Join(failures, "; "))
}

func resolveWritableSFTPRoot(acc sftpAccount) (string, error) {
	session, err := openNativeSFTP(acc)
	if err != nil {
		return "", err
	}
	defer session.Close()
	return resolveWritableSFTPRootWithFS(acc, session.sftp)
}

func remoteSFTPPath(acc sftpAccount, remoteRel string) string {
	return remoteJoin(normalizedSFTPRoot(acc.Root), remoteRel)
}

func uploadToSFTP(localPath, remoteRel string) {
	if !sftpEnabled || len(sftpAccounts) == 0 {
		return
	}
	for _, acc := range sftpAccounts {
		remotePath := remoteSFTPPath(acc, remoteRel)
		log.Printf("%s⇪ Uploading via SFTP to %s:%s%s", cyan, acc.Host, remotePath, reset)
		if err := uploadOneSFTP(acc, localPath, remotePath); err != nil {
			log.Printf("%sSFTP upload %s: %v%s", red, acc.Host, err, reset)
			continue
		}
		remoteDaily := path.Dir(remotePath)
		if strings.Contains("/"+remotePath, "/daily/") {
			if maxCopies > 0 {
				rotateCopiesSFTP(acc, remoteDaily, maxCopies*sftpKeepFactor)
			} else {
				cleanupOldFilesSFTP(acc, remoteDaily, keepDays*sftpKeepFactor)
			}
		}
	}
}

func uploadOneSFTP(acc sftpAccount, localPath, remotePath string) error {
	session, err := openNativeSFTP(acc)
	if err != nil {
		return err
	}
	defer session.Close()

	if err := ensureRemoteDir(session.sftp, path.Dir(remotePath)); err != nil {
		return err
	}
	local, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer local.Close()

	tmpPath := remotePath + ".part"
	remote, err := session.sftp.Create(tmpPath)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(remote, local)
	closeErr := remote.Close()
	if copyErr != nil {
		_ = session.sftp.Remove(tmpPath)
		return copyErr
	}
	if closeErr != nil {
		_ = session.sftp.Remove(tmpPath)
		return closeErr
	}
	_ = session.sftp.Remove(remotePath)
	if err := session.sftp.Rename(tmpPath, remotePath); err != nil {
		_ = session.sftp.Remove(tmpPath)
		return err
	}
	return nil
}

func listSFTPArchivesWithClient(client *sftp.Client, remoteDir string) ([]string, error) {
	entries, err := client.ReadDir(remoteDir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".tar.gz") {
			continue
		}
		files = append(files, entry.Name())
	}
	sort.Strings(files)
	return files, nil
}

func listSFTPArchives(acc sftpAccount, remoteDir string) ([]string, error) {
	session, err := openNativeSFTP(acc)
	if err != nil {
		return nil, err
	}
	defer session.Close()
	return listSFTPArchivesWithClient(session.sftp, remoteDir)
}

func archiveTimeFromName(name string) (time.Time, bool) {
	if len(name) < len("2006-01-02_15-04-05") {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation("2006-01-02_15-04-05", name[:19], time.Local)
	return t, err == nil
}

func rotateCopiesSFTP(acc sftpAccount, remoteDir string, copies int) {
	session, err := openNativeSFTP(acc)
	if err != nil {
		return
	}
	defer session.Close()
	files, err := listSFTPArchivesWithClient(session.sftp, remoteDir)
	if err != nil || len(files) <= copies {
		return
	}
	for _, name := range files[:len(files)-copies] {
		remote := path.Join(remoteDir, name)
		log.Printf("🧹 (SFTP) Deleting extra archive %s", remote)
		_ = session.sftp.Remove(remote)
	}
}

func cleanupOldFilesSFTP(acc sftpAccount, remoteDir string, days int) {
	session, err := openNativeSFTP(acc)
	if err != nil {
		return
	}
	defer session.Close()
	files, err := listSFTPArchivesWithClient(session.sftp, remoteDir)
	if err != nil {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -days)
	for _, name := range files {
		t, ok := archiveTimeFromName(name)
		if !ok || !t.Before(cutoff) {
			continue
		}
		remote := path.Join(remoteDir, name)
		log.Printf("🧹 (SFTP) Deleting old archive %s", remote)
		_ = session.sftp.Remove(remote)
	}
}

func checkSFTPBackups(host string, ports []string, threshold time.Time) sftpCheckResult {
	result := sftpCheckResult{}
	for _, problem := range sftpInitProblems {
		result.Problems = append(result.Problems, problem)
		result.Severity = max(result.Severity, 2)
	}
	if !sftpEnabled || len(sftpAccounts) == 0 {
		return result
	}
	expectedCopies := 0
	if maxCopies > 0 {
		expectedCopies = maxCopies * sftpKeepFactor
	}
	for _, acc := range sftpAccounts {
		session, err := openNativeSFTP(acc)
		if err != nil {
			result.Problems = append(result.Problems, fmt.Sprintf("SFTP %s: %v", acc.Host, err))
			result.Severity = max(result.Severity, 2)
			continue
		}
		for _, port := range ports {
			if _, skip := excludePorts[port]; skip {
				continue
			}
			remoteDaily := remoteSFTPPath(acc, path.Join(host, backupSubdir, "redis_"+port, "daily"))
			files, err := listSFTPArchivesWithClient(session.sftp, remoteDaily)
			if err != nil {
				result.Problems = append(result.Problems, fmt.Sprintf("SFTP %s redis %s: %v", acc.Host, port, err))
				result.Severity = max(result.Severity, 2)
				continue
			}
			if len(files) == 0 {
				result.Problems = append(result.Problems, fmt.Sprintf("SFTP %s redis %s: NO BACKUP", acc.Host, port))
				result.Severity = max(result.Severity, 2)
				continue
			}
			latest := files[len(files)-1]
			if latestTime, ok := archiveTimeFromName(latest); !ok || latestTime.Before(threshold) {
				result.Problems = append(result.Problems, fmt.Sprintf("SFTP %s redis %s: older than %d h", acc.Host, port, checkHours))
				result.Severity = max(result.Severity, 2)
			}
			if expectedCopies > 0 && len(files) < expectedCopies {
				result.Problems = append(result.Problems, fmt.Sprintf("SFTP %s redis %s: only %d/%d copies", acc.Host, port, len(files), expectedCopies))
				result.Severity = max(result.Severity, 1)
			}
			result.LatestFiles++
		}
		session.Close()
	}
	return result
}
