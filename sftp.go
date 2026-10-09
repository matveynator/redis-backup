//go:build !windows
// +build !windows

package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
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

var sftpProbeBatch = sftpBatch

func init() {
	flag.StringVar(&sftpConfFile, "sftp-conf", "/etc/sftp-backup.conf", "Path to SFTP configuration file")
	flag.StringVar(&sftpHost, "sftp-host", "", "Override SFTP host")
	flag.IntVar(&sftpPort, "sftp-port", 22, "SFTP port")
	flag.StringVar(&sftpUser, "sftp-user", "", "SFTP username")
	flag.StringVar(&sftpKeyFile, "sftp-key", "", "SFTP private key file (optional; OpenSSH defaults/agent are used when empty)")
	flag.StringVar(&sftpKnownHosts, "sftp-known-hosts", "", "known_hosts file (optional; OpenSSH default is used when empty)")
	flag.StringVar(&sftpRoot, "sftp-root", "auto", "Remote root directory for backups (default: auto-detect writable directory)")
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
	// Match the existing FTP override semantics: an explicit --sftp-host
	// replaces every target from the config file instead of adding to them.
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

func sftpCommand(acc sftpAccount) (*exec.Cmd, error) {
	bin, err := exec.LookPath("sftp")
	if err != nil {
		return nil, fmt.Errorf("sftp binary not found: %w", err)
	}
	args := []string{
		"-q", "-b", "-",
		"-P", strconv.Itoa(normalizedSFTPPort(acc.Port)),
		"-oBatchMode=yes",
		"-oConnectTimeout=10",
		"-oConnectionAttempts=2",
		"-oServerAliveInterval=15",
		"-oServerAliveCountMax=2",
		"-oStrictHostKeyChecking=accept-new",
	}
	if acc.KeyFile != "" {
		args = append(args, "-i", acc.KeyFile)
	}
	if acc.KnownHosts != "" {
		args = append(args, "-oUserKnownHostsFile="+acc.KnownHosts)
	}
	target := acc.Host
	if acc.User != "" {
		target = acc.User + "@" + acc.Host
	}
	args = append(args, target)
	return exec.Command(bin, args...), nil
}

func sftpBatch(acc sftpAccount, commands string) (string, error) {
	cmd, err := sftpCommand(acc)
	if err != nil {
		return "", err
	}
	cmd.Stdin = strings.NewReader(commands)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return stdout.String(), fmt.Errorf("host=%s port=%d user=%s: %s", acc.Host, normalizedSFTPPort(acc.Port), acc.User, msg)
	}
	return stdout.String(), nil
}

func sftpQuote(s string) string {
	return "\"" + strings.ReplaceAll(strings.ReplaceAll(s, "\\", "\\\\"), "\"", "\\\"") + "\""
}

func writableSFTPRootCandidates(acc sftpAccount) []string {
	root := normalizedSFTPRoot(acc.Root)
	if root != "auto" {
		return []string{root}
	}
	candidates := []string{".", "/data", "/backup", "/backups", "/upload", "/uploads"}
	if acc.User != "" {
		candidates = append(candidates, "/home/"+acc.User)
	}
	return candidates
}

func isTerminalSFTPProbeError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	terminal := []string{
		"sftp binary not found",
		"could not resolve hostname",
		"name or service not known",
		"temporary failure in name resolution",
		"nodename nor servname provided",
		"no address associated with hostname",
		"connection timed out",
		"operation timed out",
		"connection refused",
		"no route to host",
		"network is unreachable",
		"host is down",
		"connection reset",
		"connection closed",
		"closed by remote host",
		"lost connection",
		"broken pipe",
		"connection aborted",
		"subsystem request failed",
		"host key verification failed",
		"remote host identification has changed",
		"no matching host key type found",
		"no matching key exchange method found",
		"no matching cipher found",
		"permission denied (publickey",
		"authentication failed",
		"no supported authentication methods",
		"too many authentication failures",
		"kex_exchange_identification",
		"ssh_exchange_identification",
		"banner exchange",
		"received disconnect",
		"bad configuration option",
	}
	for _, needle := range terminal {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

func resolveWritableSFTPRoot(acc sftpAccount) (string, error) {
	root := normalizedSFTPRoot(acc.Root)
	explicitRoot := root != "auto"
	var failures []string
	for _, candidate := range writableSFTPRootCandidates(acc) {
		if err := probeWritableSFTPRoot(acc, candidate, explicitRoot); err == nil {
			return candidate, nil
		} else {
			failures = append(failures, fmt.Sprintf("%s: %v", candidate, err))
			if isTerminalSFTPProbeError(err) {
				break
			}
		}
	}
	if len(failures) == 0 {
		return "", fmt.Errorf("no SFTP writable-root candidates")
	}
	return "", fmt.Errorf("no writable SFTP root found (%s)", strings.Join(failures, "; "))
}

func probeWritableSFTPRoot(acc sftpAccount, root string, createRoot bool) error {
	probeName := fmt.Sprintf(".redis-backup-write-test-%d-%d", os.Getpid(), time.Now().UnixNano())
	probePath := probeName
	if root != "." {
		probePath = path.Join(root, probeName)
	}
	var commands strings.Builder
	if createRoot && root != "." && root != "/" {
		commands.WriteString(mkdirBatch(root))
	}
	fmt.Fprintf(&commands, "mkdir %s\nrmdir %s\n", sftpQuote(probePath), sftpQuote(probePath))
	_, err := sftpProbeBatch(acc, commands.String())
	return err
}

func remoteSFTPPath(acc sftpAccount, remoteRel string) string {
	rel := strings.TrimPrefix(filepath.ToSlash(remoteRel), "/")
	root := normalizedSFTPRoot(acc.Root)
	if root == "auto" || root == "." {
		return path.Clean(rel)
	}
	return path.Join(root, rel)
}

func mkdirBatch(remoteDir string) string {
	remoteDir = path.Clean(remoteDir)
	if remoteDir == "." || remoteDir == "/" {
		return ""
	}
	absolute := strings.HasPrefix(remoteDir, "/")
	parts := strings.Split(strings.TrimPrefix(remoteDir, "/"), "/")
	cur := ""
	var b strings.Builder
	for _, part := range parts {
		if part == "" || part == "." {
			continue
		}
		if cur == "" {
			if absolute {
				cur = "/" + part
			} else {
				cur = part
			}
		} else {
			cur = path.Join(cur, part)
		}
		fmt.Fprintf(&b, "-mkdir %s\n", sftpQuote(cur))
	}
	return b.String()
}

func uploadToSFTP(localPath, remoteRel string) {
	if !sftpEnabled || len(sftpAccounts) == 0 {
		return
	}
	for _, acc := range sftpAccounts {
		remotePath := remoteSFTPPath(acc, remoteRel)
		tmpPath := remotePath + ".part"
		batch := mkdirBatch(path.Dir(remotePath)) +
			fmt.Sprintf("put %s %s\nrename %s %s\n", sftpQuote(localPath), sftpQuote(tmpPath), sftpQuote(tmpPath), sftpQuote(remotePath))
		log.Printf("%s⇪ Uploading via SFTP to %s:%s%s", cyan, acc.Host, remotePath, reset)
		if _, err := sftpBatch(acc, batch); err != nil {
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

func listSFTPArchives(acc sftpAccount, remoteDir string) ([]string, error) {
	out, err := sftpBatch(acc, fmt.Sprintf("ls -1 %s\n", sftpQuote(remoteDir)))
	if err != nil {
		return nil, err
	}
	var files []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "sftp>") {
			continue
		}
		name := path.Base(line)
		if strings.HasSuffix(name, ".tar.gz") {
			files = append(files, name)
		}
	}
	sort.Strings(files)
	return files, nil
}

func archiveTimeFromName(name string) (time.Time, bool) {
	if len(name) < len("2006-01-02_15-04-05") {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation("2006-01-02_15-04-05", name[:19], time.Local)
	return t, err == nil
}

func rotateCopiesSFTP(acc sftpAccount, remoteDir string, copies int) {
	files, err := listSFTPArchives(acc, remoteDir)
	if err != nil || len(files) <= copies {
		return
	}
	var batch strings.Builder
	for _, name := range files[:len(files)-copies] {
		remote := path.Join(remoteDir, name)
		log.Printf("🧹 (SFTP) Deleting extra archive %s", remote)
		fmt.Fprintf(&batch, "rm %s\n", sftpQuote(remote))
	}
	if batch.Len() > 0 {
		_, _ = sftpBatch(acc, batch.String())
	}
}

func cleanupOldFilesSFTP(acc sftpAccount, remoteDir string, days int) {
	files, err := listSFTPArchives(acc, remoteDir)
	if err != nil {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -days)
	var batch strings.Builder
	for _, name := range files {
		t, ok := archiveTimeFromName(name)
		if !ok || !t.Before(cutoff) {
			continue
		}
		remote := path.Join(remoteDir, name)
		log.Printf("🧹 (SFTP) Deleting old archive %s", remote)
		fmt.Fprintf(&batch, "rm %s\n", sftpQuote(remote))
	}
	if batch.Len() > 0 {
		_, _ = sftpBatch(acc, batch.String())
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
		for _, port := range ports {
			if _, skip := excludePorts[port]; skip {
				continue
			}
			remoteDaily := remoteSFTPPath(acc, path.Join(host, backupSubdir, "redis_"+port, "daily"))
			files, err := listSFTPArchives(acc, remoteDaily)
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
	}
	return result
}
