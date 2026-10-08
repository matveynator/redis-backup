//go:build !windows
// +build !windows

package main

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jlaffaye/ftp"
	"github.com/shirou/gopsutil/v3/net"
	"github.com/shirou/gopsutil/v3/process"
)

var (
	backupPath     string
	keepDays       int
	maxCopies      int
	saveTimeoutSec int

	ftpConfFile          string
	ftpHost              string
	ftpUser              string
	ftpPass              string
	ftpKeepFactor        int
	ftpEnabled           bool
	ftpKeepFactorFlagged bool

	excludePortsCSV string
	checkHours      int
)

type ftpAccount struct {
	Host string
	User string
	Pass string
}

var ftpAccounts []ftpAccount
var excludePorts map[string]struct{}

const (
	green  = "\033[32m"
	yellow = "\033[33m"
	red    = "\033[31m"
	cyan   = "\033[36m"
	reset  = "\033[0m"
)

const lockFile = "/tmp/redis_backup.lock"
const backupSubdir = "redis-backup"

func main() {
	listFlag := flag.Bool("list", false, "List backups and exit")
	restoreFlag := flag.Bool("restore", false, "Interactive restore wizard")
	helpFlag := flag.Bool("help", false, "Show help and exit")

	flag.StringVar(&backupPath, "backup-path", "/backup", "Root directory for backups")
	flag.IntVar(&keepDays, "days", 30, "Days to keep daily backups (local)")
	flag.IntVar(&maxCopies, "copies", 0, "Max number of daily snapshots to keep (0 = unlimited)")
	flag.IntVar(&saveTimeoutSec, "save-timeout", 600, "Seconds to wait until Redis finishes BGSAVE (default: 600)")
	flag.IntVar(&maxCopies, "c", 0, "Alias for --copies")
	flag.StringVar(&excludePortsCSV, "exclude-ports", "", "Comma-separated list of Redis ports to skip during backup/check")
	flag.IntVar(&checkHours, "check", 0, "Run integrity check; value = max allowed hours since last backup. 0 disables check mode.")
	flag.StringVar(&ftpConfFile, "ftp-conf", "/etc/ftp-backup.conf", "Path to FTP credentials file")
	flag.StringVar(&ftpHost, "ftp-host", "", "Override FTP host (otherwise taken from conf file)")
	flag.StringVar(&ftpUser, "ftp-user", "", "Override FTP username (otherwise taken from conf file)")
	flag.StringVar(&ftpPass, "ftp-pass", "", "Override FTP password (otherwise taken from conf file)")
	flag.IntVar(&ftpKeepFactor, "ftp-keep-factor", 4, "Retention multiplier for FTP (remoteKeepDays = keepDays * factor)")

	flag.Parse()
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "ftp-keep-factor" {
			ftpKeepFactorFlagged = true
		}
	})
	if !ftpKeepFactorFlagged && maxCopies == 1 {
		ftpKeepFactor = 4
	}

	excludePorts = make(map[string]struct{})
	if excludePortsCSV != "" {
		for _, p := range strings.Split(excludePortsCSV, ",") {
			excludePorts[strings.TrimSpace(p)] = struct{}{}
		}
	}

	if checkHours > 0 {
		runCheckMode()
		return
	}

	switch {
	case *helpFlag:
		printHelp()
	case *listFlag:
		listBackups()
	case *restoreFlag:
		interactiveRestore()
	default:
		acquireLock()
		defer releaseLock()
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		go func() { <-sig; releaseLock(); os.Exit(1) }()
		initFTP()
		initSFTP()
		runBackup()
	}
}

func printHelp() {
	exe := filepath.Base(os.Args[0])
	fmt.Printf("%s🚀 Smart & Friendly Redis Backup Tool%s\n", cyan, reset)
	fmt.Printf("%sBuilt by CHICHA — good dog, great backups. 🐕💾%s\n\n", cyan, reset)
	fmt.Printf("%sUSAGE%s\n  %s [flags]\n\n", cyan, reset, exe)
	fmt.Printf("%sGENERAL FLAGS%s\n", cyan, reset)
	fmt.Println("  --list                    List existing backups and exit")
	fmt.Println("  --restore                 Start interactive restore wizard")
	fmt.Println("  --backup-path <dir>       Root directory for backups (default: /backup)")
	fmt.Println("  --days <n>                Days to keep local daily backups (default: 30)")
	fmt.Println("  --copies, -c <n>          Keep only <n> newest daily backups (0 = unlimited)")
	fmt.Println("  --save-timeout <sec>      Max seconds to wait until Redis finishes BGSAVE (default: 600)")
	fmt.Printf("%sBACKUP CONTROL and MONITORING%s\n", cyan, reset)
	fmt.Println("  --exclude-ports <csv>     Comma-separated list of Redis ports NOT to back up")
	fmt.Println("  --check <hours>           Verify freshness and archive integrity; CRITICAL if older than <hours>")
	fmt.Printf("%sFTP OFF-SITE%s\n", cyan, reset)
	fmt.Println("  --ftp-conf <file>         Credentials file (default: /etc/ftp-backup.conf)")
	fmt.Println("  --ftp-host <host>         FTP host (overrides conf)")
	fmt.Println("  --ftp-user <user>         FTP username")
	fmt.Println("  --ftp-pass <pass>         FTP password")
	fmt.Println("  --ftp-keep-factor <n>     Store data on FTP n× longer than locally (default: 4)")
	fmt.Printf("%sSFTP OFF-SITE%s\n", cyan, reset)
	fmt.Println("  --sftp-conf <file>        Configuration file (default: /etc/sftp-backup.conf)")
	fmt.Println("  --sftp-host <host>        SFTP host")
	fmt.Println("  --sftp-port <port>        SFTP port (default: 22)")
	fmt.Println("  --sftp-user <user>        SFTP username")
	fmt.Println("  --sftp-key <file>         Private key; OpenSSH defaults/agent are used when empty")
	fmt.Println("  --sftp-known-hosts <file> known_hosts override")
	fmt.Println("  --sftp-root <dir>         Remote backup root (default: /)")
	fmt.Println("  --sftp-keep-factor <n>    Store data on SFTP n× longer than locally (default: 4)")
	fmt.Printf("%sEXAMPLES%s\n", cyan, reset)
	fmt.Printf("  # Basic backup\n  sudo %s\n\n", exe)
	fmt.Printf("  # Exclude session caches (ports 6380,6381)\n  sudo %s --exclude-ports 6380,6381\n\n", exe)
	fmt.Printf("  # Nagios check – CRITICAL if older than 25 h\n  %s --check 25\n", exe)
	fmt.Printf("\n%sDETECTED REDIS TARGETS%s\n", cyan, reset)
	ports := detectRedisPorts()
	if len(ports) == 0 {
		fmt.Println("  (no running redis-server instances found)")
		return
	}
	for _, port := range ports {
		dir := getRedisDir(port)
		file := getRedisRDB(port)
		if dir == "" || file == "" {
			continue
		}
		rdbPath := filepath.Join(dir, file)
		if info, err := os.Stat(rdbPath); err == nil {
			size := float64(info.Size()) / (1024 * 1024)
			fmt.Printf("  • port %s → %s  (%.1f MB)\n", port, rdbPath, size)
		}
	}
}

func suggestSudo(err error) {
	if err == nil {
		return
	}
	if os.IsPermission(err) || strings.Contains(err.Error(), "permission denied") {
		log.Printf("%sPermission denied – you may need to run with sudo%s", yellow, reset)
	}
}

func listBackups() {
	host, _ := os.Hostname()
	root := filepath.Join(backupPath, host, backupSubdir)
	entries, err := os.ReadDir(root)
	if err != nil {
		suggestSudo(err)
		log.Fatalf("%sCannot open %s: %v%s", red, root, err, reset)
	}
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "redis_") {
			daily := filepath.Join(root, e.Name(), "daily")
			fmt.Printf("%s📂 %s%s\n", cyan, e.Name(), reset)
			files, _ := os.ReadDir(daily)
			for _, f := range files {
				fmt.Printf("  • %s\n", f.Name())
			}
		}
	}
}

func interactiveRestore() {
	host, _ := os.Hostname()
	root := filepath.Join(backupPath, host, backupSubdir)
	reader := bufio.NewReader(os.Stdin)
	dirs, err := os.ReadDir(root)
	if err != nil {
		suggestSudo(err)
		fmt.Printf("%sCannot open %s: %v%s\n", red, root, err, reset)
		return
	}
	var ports []string
	for _, d := range dirs {
		if d.IsDir() && strings.HasPrefix(d.Name(), "redis_") {
			ports = append(ports, strings.TrimPrefix(d.Name(), "redis_"))
		}
	}
	if len(ports) == 0 {
		fmt.Printf("%sNo backups found.%s\n", red, reset)
		return
	}
	fmt.Println("Select Redis port to restore:")
	for i, p := range ports {
		fmt.Printf("  [%d] %s\n", i+1, p)
	}
	fmt.Print(">>> ")
	line, _ := reader.ReadString('\n')
	idx, _ := strconv.Atoi(strings.TrimSpace(line))
	if idx < 1 || idx > len(ports) {
		fmt.Println("Invalid choice")
		return
	}
	port := ports[idx-1]
	dailyDir := filepath.Join(root, "redis_"+port, "daily")
	files, err := os.ReadDir(dailyDir)
	if err != nil {
		suggestSudo(err)
		fmt.Printf("%sCannot read %s: %v%s\n", red, dailyDir, err, reset)
		return
	}
	if len(files) == 0 {
		fmt.Printf("%sNo archives for port %s%s\n", red, port, reset)
		return
	}
	fmt.Println("Select archive:")
	for i, f := range files {
		fmt.Printf("  [%d] %s\n", i+1, f.Name())
	}
	fmt.Print(">>> ")
	line, _ = reader.ReadString('\n')
	idx, _ = strconv.Atoi(strings.TrimSpace(line))
	if idx < 1 || idx > len(files) {
		fmt.Println("Invalid choice")
		return
	}
	archive := files[idx-1].Name()
	fmt.Printf("%s⚠  Redis %s will be restored from %s. Continue? (y/N): %s", yellow, port, archive, reset)
	confirm, _ := reader.ReadString('\n')
	confirm = strings.ToLower(strings.TrimSpace(confirm))
	if confirm != "y" && confirm != "yes" {
		fmt.Println("Cancelled.")
		return
	}
	restoreBackup(port, archive)
}

func runBackup() {
	now := time.Now()
	host, _ := os.Hostname()
	ports := detectRedisPorts()
	if len(ports) == 0 {
		log.Println("❌ No redis-server processes found.")
		return
	}
	for _, port := range ports {
		if _, skip := excludePorts[port]; skip {
			log.Printf("%sSkipping Redis %s (excluded)%s", yellow, port, reset)
			continue
		}
		if !isRedisHealthy(port) {
			log.Printf("%sRedis %s is not readable – skipping backup%s", yellow, port, reset)
			continue
		}
		dir := getRedisDir(port)
		file := getRedisRDB(port)
		if dir == "" || file == "" {
			log.Printf("⚠  Redis %s: cannot determine dir or RDB file\n", port)
			continue
		}
		rdbPath := filepath.Join(dir, file)
		if _, err := os.Stat(rdbPath); err != nil {
			suggestSudo(err)
			log.Printf("%sFile not found or inaccessible: %s%s", red, rdbPath, reset)
			continue
		}
		log.Printf("%s✔ Redis %s → %s%s", green, port, rdbPath, reset)
		archivePath := backupInstance(port, rdbPath, host, now)
		if archivePath != "" && (ftpEnabled || sftpEnabled) {
			remoteRel := strings.TrimPrefix(archivePath, backupPath)
			remoteRel = strings.TrimPrefix(remoteRel, string(os.PathSeparator))
			if ftpEnabled {
				uploadToFTP(archivePath, remoteRel)
			}
			if sftpEnabled {
				uploadToSFTP(archivePath, remoteRel)
			}
		}
		log.Printf("%s----------------------------------------%s", cyan, reset)
	}
}

func detectRedisPorts() []string {
	seen := make(map[string]struct{})
	var ports []string
	conns, err := net.Connections("tcp")
	if err != nil {
		log.Println("net.Connections error:", err)
		return nil
	}
	for _, c := range conns {
		if c.Status != "LISTEN" || c.Pid == 0 || c.Laddr.Port == 0 {
			continue
		}
		proc, _ := process.NewProcess(c.Pid)
		name, _ := proc.Name()
		if !strings.Contains(strings.ToLower(name), "redis-server") {
			continue
		}
		seen[strconv.Itoa(int(c.Laddr.Port))] = struct{}{}
	}
	for p := range seen {
		ports = append(ports, p)
	}
	sort.Strings(ports)
	return ports
}

func getRedisDir(port string) string {
	out, err := exec.Command("redis-cli", "-p", port, "CONFIG", "GET", "dir").Output()
	if err != nil {
		suggestSudo(err)
		return ""
	}
	parts := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(parts) >= 2 {
		return strings.TrimSpace(parts[1])
	}
	return ""
}

func getRedisRDB(port string) string {
	out, err := exec.Command("redis-cli", "-p", port, "CONFIG", "GET", "dbfilename").Output()
	if err != nil {
		suggestSudo(err)
		return ""
	}
	parts := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(parts) >= 2 {
		return strings.TrimSpace(parts[1])
	}
	return ""
}

func backupInstance(port, rdbPath, host string, now time.Time) string {
	inst := "redis_" + port
	base := filepath.Join(backupPath, host, backupSubdir, inst)
	daily := filepath.Join(base, "daily")
	weekly := filepath.Join(base, "weekly")
	monthly := filepath.Join(base, "monthly")
	yearly := filepath.Join(base, "yearly")
	for _, d := range []string{daily, weekly, monthly, yearly} {
		if err := os.MkdirAll(d, 0755); err != nil {
			suggestSudo(err)
			log.Printf("mkdir %s: %v", d, err)
			return ""
		}
	}
	ts := now.Format("2006-01-02_15-04-05")
	archive := filepath.Join(daily, fmt.Sprintf("%s_%s.tar.gz", ts, inst))
	log.Printf("%s📦 Archiving %s …%s", cyan, archive, reset)
	if err := createTarGz(archive, []string{rdbPath}); err != nil {
		suggestSudo(err)
		log.Printf("%sArchive error: %v%s", red, err, reset)
		return ""
	}
	if err := writeBackupMeta(archive, rdbPath); err != nil {
		log.Printf("%sFailed to store backup metadata for %s: %v%s", yellow, archive, err, reset)
	}
	printFileSize(archive)
	if now.Weekday() == time.Sunday {
		copyFile(archive, filepath.Join(weekly, filepath.Base(archive)))
	}
	if now.Day() == 1 {
		copyFile(archive, filepath.Join(monthly, filepath.Base(archive)))
	}
	if now.YearDay() == 1 {
		copyFile(archive, filepath.Join(yearly, filepath.Base(archive)))
	}
	if maxCopies > 0 {
		rotateCopies(daily, maxCopies)
	} else {
		cleanupOldFiles(daily, keepDays)
	}
	return archive
}

func restoreBackup(port, archiveName string) {
	host, _ := os.Hostname()
	inst := "redis_" + port
	archivePath := filepath.Join(backupPath, host, backupSubdir, inst, "daily", archiveName)
	if _, err := os.Stat(archivePath); err != nil {
		suggestSudo(err)
		log.Fatalf("%sArchive %s not found%s", red, archivePath, reset)
	}
	restoreDir := getRedisDir(port)
	fileName := getRedisRDB(port)
	if restoreDir == "" || fileName == "" {
		log.Fatalf("%sCannot determine Redis directory for port %s%s", red, port, reset)
	}
	currentFile := filepath.Join(restoreDir, fileName)
	var origUID, origGID int
	var origMode os.FileMode
	if info, err := os.Stat(currentFile); err == nil {
		if stat, ok := info.Sys().(*syscall.Stat_t); ok {
			origUID = int(stat.Uid)
			origGID = int(stat.Gid)
		}
		origMode = info.Mode()
		backupName := currentFile + ".backup"
		log.Printf("%s🔁 Renaming current RDB → %s%s", yellow, backupName, reset)
		if err := os.Rename(currentFile, backupName); err != nil {
			suggestSudo(err)
			log.Fatalf("%sCannot rename current file: %v%s", red, err, reset)
		}
	}
	log.Printf("%s🔄 Extracting %s → %s%s", cyan, archiveName, restoreDir, reset)
	if err := extractTarGz(archivePath, restoreDir); err != nil {
		suggestSudo(err)
		log.Fatalf("%sRestore error: %v%s", red, err, reset)
	}
	newFile := filepath.Join(restoreDir, fileName)
	if origMode != 0 {
		_ = os.Chmod(newFile, origMode)
	}
	if origUID != 0 || origGID != 0 {
		_ = os.Chown(newFile, origUID, origGID)
	}
	log.Printf("%s✔ Restore complete%s", green, reset)
}

func initFTP() {
	if _, err := os.Stat(ftpConfFile); err == nil {
		_ = parseFTPConf(ftpConfFile)
	}
	if ftpHost != "" {
		ftpAccounts = []ftpAccount{{Host: ftpHost, User: ftpUser, Pass: ftpPass}}
	}
	ftpEnabled = len(ftpAccounts) > 0
	if !ftpEnabled {
		return
	}
	for _, acc := range ftpAccounts {
		log.Printf("%s🌐 FTP replication target → %s (user %s)%s", cyan, acc.Host, acc.User, reset)
	}
}

func parseFTPConf(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var cur ftpAccount
	commit := func() {
		if cur.Host != "" && cur.User != "" && cur.Pass != "" {
			ftpAccounts = append(ftpAccounts, cur)
		}
		cur = ftpAccount{}
	}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		kv := strings.SplitN(line, "=", 2)
		key := strings.Trim(kv[0], " \"")
		val := strings.Trim(kv[1], " \"")
		switch key {
		case "FTP_HOST":
			if cur.Host != "" {
				commit()
			}
			cur.Host = val
		case "FTP_USER":
			cur.User = val
		case "FTP_PASS":
			cur.Pass = val
		}
	}
	commit()
	return scanner.Err()
}

func uploadToSingleFTP(acc ftpAccount, localPath, remoteRel string) {
	c, err := ftp.Dial(acc.Host + ":21")
	if err != nil {
		log.Printf("%sFTP dial %s: %v%s", red, acc.Host, err, reset)
		return
	}
	defer c.Quit()
	if err := c.Login(acc.User, acc.Pass); err != nil {
		log.Printf("%sFTP login %s: %v%s", red, acc.Host, err, reset)
		return
	}
	parts := strings.Split(filepath.Dir(remoteRel), string(os.PathSeparator))
	cwd := "/"
	for _, p := range parts {
		if p == "" {
			continue
		}
		cwd = filepath.Join(cwd, p)
		_ = c.MakeDir(cwd)
	}
	f, err := os.Open(localPath)
	if err != nil {
		log.Printf("%sFTP open local: %v%s", red, err, reset)
		return
	}
	defer f.Close()
	remotePath := filepath.ToSlash(remoteRel)
	log.Printf("%s⇪ Uploading to %s: %s%s", cyan, acc.Host, remotePath, reset)
	if err := c.Stor(remotePath, f); err != nil {
		log.Printf("%sFTP upload %s: %v%s", red, acc.Host, err, reset)
		return
	}
	if strings.Contains(remotePath, "/daily/") {
		remoteDailyDir := filepath.ToSlash(filepath.Dir(remotePath))
		if maxCopies > 0 {
			rotateCopiesFTP(c, remoteDailyDir, maxCopies*ftpKeepFactor)
		} else {
			cleanupOldFilesFTP(c, remoteDailyDir, keepDays*ftpKeepFactor)
		}
	}
}

func uploadToFTP(localPath, remoteRel string) {
	if !ftpEnabled {
		return
	}
	for _, acc := range ftpAccounts {
		uploadToSingleFTP(acc, localPath, remoteRel)
	}
}

func cleanupOldFilesFTP(c *ftp.ServerConn, dir string, days int) {
	entries, err := c.List(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -days)
	for _, e := range entries {
		if e.Type != ftp.EntryTypeFile {
			continue
		}
		if e.Time.Before(cutoff) {
			remoteFile := filepath.ToSlash(filepath.Join(dir, e.Name))
			log.Printf("🧹 (FTP) Deleting old archive %s", remoteFile)
			_ = c.Delete(remoteFile)
		}
	}
}

func runCheckMode() {
	if _, err := os.Stat(ftpConfFile); err == nil {
		_ = parseFTPConf(ftpConfFile)
	}
	if len(ftpAccounts) > 0 {
		ftpEnabled = true
	}
	if ftpHost != "" {
		ftpEnabled = true
	}
	initSFTP()

	host, _ := os.Hostname()
	now := time.Now()
	threshold := now.Add(-time.Duration(checkHours) * time.Hour)
	problems := []string{}
	severity := 0
	totalSize, _ := dirSize(filepath.Join(backupPath, host, backupSubdir))
	var latestSetSize int64
	var latestFiles int

	ports := detectRedisPorts()
	for _, port := range ports {
		if _, skip := excludePorts[port]; skip {
			continue
		}
		inst := "redis_" + port
		dailyDir := filepath.Join(backupPath, host, backupSubdir, inst, "daily")
		latestFile, latestMTime := findLatestArchive(dailyDir)
		if latestFile == "" {
			problems = append(problems, fmt.Sprintf("Redis %s: NO BACKUP", port))
			severity = max(severity, 2)
			continue
		}
		if latestMTime.Before(threshold) {
			problems = append(problems, fmt.Sprintf("Redis %s: older than %d h", port, checkHours))
			severity = max(severity, 2)
		}
		if fi, err := os.Stat(latestFile); err == nil {
			latestSetSize += fi.Size()
			latestFiles++
		}
		if err := validateBackupArchive(latestFile); err != nil {
			problems = append(problems, fmt.Sprintf("Redis %s: backup integrity check failed: %v", port, err))
			severity = max(severity, 2)
		}
	}

	var copiesPossible int64
	var diskFree, diskTotal int64
	var usedPct float64
	if latestSetSize > 0 {
		var errDisk error
		diskTotal, diskFree, errDisk = diskUsage(backupPath)
		if errDisk != nil {
			problems = append(problems, fmt.Sprintf("disk stat: %v", errDisk))
		}
		copiesPossible = diskFree / latestSetSize
		usedPct = float64(totalSize) / float64(diskTotal) * 100
		if copiesPossible < 5 || usedPct >= 90 {
			severity = max(severity, 2)
			problems = append(problems, "disk pressure CRITICAL")
		} else if copiesPossible < 24 || usedPct >= 80 {
			severity = max(severity, 1)
			problems = append(problems, "disk pressure WARNING")
		}
	}

	var ftpLatestSetSize int64
	var ftpLatestFiles int
	if ftpEnabled {
		expectedFtpCopies := 0
		if maxCopies > 0 {
			expectedFtpCopies = maxCopies * ftpKeepFactor
		}
		for _, acc := range ftpAccounts {
			c, err := ftp.Dial(acc.Host+":21", ftp.DialWithTimeout(5*time.Second))
			if err != nil {
				problems = append(problems, fmt.Sprintf("FTP connect %s: %v", acc.Host, err))
				severity = max(severity, 2)
				continue
			}
			if err := c.Login(acc.User, acc.Pass); err != nil {
				problems = append(problems, fmt.Sprintf("FTP login %s: %v", acc.Host, err))
				severity = max(severity, 2)
				_ = c.Quit()
				continue
			}
			for _, port := range ports {
				if _, skip := excludePorts[port]; skip {
					continue
				}
				remoteDaily := filepath.ToSlash(filepath.Join("/", host, backupSubdir, "redis_"+port, "daily"))
				latestPath, latestSize, latestTime := findLatestFTPArchive(c, remoteDaily)
				if latestPath == "" {
					problems = append(problems, fmt.Sprintf("FTP %s redis %s: NO BACKUP", acc.Host, port))
					severity = max(severity, 2)
					continue
				}
				if latestTime.Before(threshold) {
					problems = append(problems, fmt.Sprintf("FTP %s redis %s: older than %d h", acc.Host, port, checkHours))
					severity = max(severity, 2)
				}
				if expectedFtpCopies > 0 {
					entries, _ := c.List(remoteDaily)
					var cnt int
					for _, e := range entries {
						if e.Type == ftp.EntryTypeFile && strings.HasSuffix(e.Name, ".tar.gz") {
							cnt++
						}
					}
					if cnt < expectedFtpCopies {
						problems = append(problems, fmt.Sprintf("FTP %s redis %s: only %d/%d copies", acc.Host, port, cnt, expectedFtpCopies))
						severity = max(severity, 1)
					}
				}
				ftpLatestSetSize += latestSize
				ftpLatestFiles++
			}
			_ = c.Quit()
		}
	}

	sftpResult := checkSFTPBackups(host, ports, threshold)
	if len(sftpResult.Problems) > 0 {
		problems = append(problems, sftpResult.Problems...)
	}
	severity = max(severity, sftpResult.Severity)

	localMetrics := fmt.Sprintf("Backups total: %.1f MB (%d files); full set: %.1f MB; free: %.1f MB; can store ≈ %d full sets; used by backups: %.1f%%", humanMB(totalSize), latestFiles, humanMB(latestSetSize), humanMB(diskFree), copiesPossible, usedPct)
	ftpMetrics := ""
	if ftpEnabled && ftpLatestFiles > 0 {
		ftpMetrics = fmt.Sprintf("FTP latest full set: %.1f MB (%d files)", humanMB(ftpLatestSetSize), ftpLatestFiles)
	}
	sftpMetrics := ""
	if sftpEnabled && sftpResult.LatestFiles > 0 {
		sftpMetrics = fmt.Sprintf("SFTP latest full set: %d files", sftpResult.LatestFiles)
	}
	var statusText string
	switch severity {
	case 2:
		statusText = "CRITICAL"
	case 1:
		statusText = "WARNING"
	default:
		statusText = "OK"
	}
	details := "all redis backups fresh and valid. 👍"
	if len(problems) > 0 {
		details = strings.Join(problems, "; ")
	}
	fmt.Printf("%s: %s\n", statusText, details)
	fmt.Println(localMetrics)
	if ftpMetrics != "" {
		fmt.Println(ftpMetrics)
	}
	if sftpMetrics != "" {
		fmt.Println(sftpMetrics)
	}
	switch severity {
	case 2:
		os.Exit(2)
	case 1:
		os.Exit(1)
	default:
		os.Exit(0)
	}
}

func findLatestFTPArchive(c *ftp.ServerConn, dir string) (string, int64, time.Time) {
	entries, err := c.List(dir)
	if err != nil || len(entries) == 0 {
		return "", 0, time.Time{}
	}
	var newest string
	var newestTime time.Time
	var newestSize int64
	for _, e := range entries {
		if e.Type != ftp.EntryTypeFile || !strings.HasSuffix(e.Name, ".tar.gz") {
			continue
		}
		if e.Time.After(newestTime) {
			newestTime = e.Time
			newest = filepath.ToSlash(filepath.Join(dir, e.Name))
			newestSize = int64(e.Size)
		}
	}
	return newest, newestSize, newestTime
}

func max(a, b int) int {
	if b > a {
		return b
	}
	return a
}

func findLatestArchive(dir string) (string, time.Time) {
	files, err := os.ReadDir(dir)
	if err != nil || len(files) == 0 {
		return "", time.Time{}
	}
	var newest string
	var newestTime time.Time
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".tar.gz") {
			continue
		}
		path := filepath.Join(dir, f.Name())
		if info, err := os.Stat(path); err == nil && info.ModTime().After(newestTime) {
			newestTime = info.ModTime()
			newest = path
		}
	}
	return newest, newestTime
}

type backupMeta struct {
	OriginalSize int64 `json:"original_size"`
	SnapshotTime int64 `json:"snapshot_time"`
}

func compareSizes(originalPath, archivePath string) (bool, error) {
	backupSize, err := archivedPayloadSize(archivePath)
	if err != nil {
		return true, err
	}
	referenceSize, err := referenceRDBSize(originalPath, archivePath)
	if err != nil {
		return true, err
	}
	return float64(backupSize) >= 0.95*float64(referenceSize), nil
}

func archivedPayloadSize(archivePath string) (int64, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	gr, err := gzip.NewReader(f)
	if err != nil {
		return 0, err
	}
	defer gr.Close()
	tr := tar.NewReader(gr)
	hdr, err := tr.Next()
	if err != nil {
		return 0, err
	}
	return hdr.Size, nil
}

func referenceRDBSize(originalPath, archivePath string) (int64, error) {
	if meta, err := readBackupMeta(archivePath); err == nil && meta.OriginalSize > 0 {
		return meta.OriginalSize, nil
	}
	info, err := os.Stat(originalPath)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

func writeBackupMeta(archivePath, originalPath string) error {
	info, err := os.Stat(originalPath)
	if err != nil {
		return err
	}
	meta := backupMeta{OriginalSize: info.Size(), SnapshotTime: info.ModTime().Unix()}
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return os.WriteFile(archivePath+".meta", append(data, '\n'), 0644)
}

func readBackupMeta(archivePath string) (backupMeta, error) {
	data, err := os.ReadFile(archivePath + ".meta")
	if err != nil {
		return backupMeta{}, err
	}
	var meta backupMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return backupMeta{}, err
	}
	return meta, nil
}

func createTarGz(dst string, files []string) error {
	out, err := os.Create(dst)
	if err != nil {
		suggestSudo(err)
		return err
	}
	defer out.Close()
	gw := gzip.NewWriter(out)
	defer gw.Close()
	tw := tar.NewWriter(gw)
	defer tw.Close()
	for _, file := range files {
		info, err := os.Stat(file)
		if err != nil {
			suggestSudo(err)
			return err
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = filepath.Base(file)
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		f, err := os.Open(file)
		if err != nil {
			suggestSudo(err)
			return err
		}
		if _, err := io.Copy(tw, f); err != nil {
			f.Close()
			return err
		}
		f.Close()
	}
	return nil
}

func extractTarGz(src, dest string) error {
	f, err := os.Open(src)
	if err != nil {
		suggestSudo(err)
		return err
	}
	defer f.Close()
	gr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gr.Close()
	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		outPath := filepath.Join(dest, hdr.Name)
		of, err := os.Create(outPath)
		if err != nil {
			suggestSudo(err)
			return err
		}
		if _, err := io.Copy(of, tr); err != nil {
			of.Close()
			return err
		}
		of.Close()
		_ = os.Chmod(outPath, os.FileMode(hdr.Mode))
		_ = os.Chown(outPath, hdr.Uid, hdr.Gid)
	}
	return nil
}

func copyFile(src, dst string) {
	in, err := os.Open(src)
	if err != nil {
		suggestSudo(err)
		log.Printf("open %s: %v", src, err)
		return
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		suggestSudo(err)
		log.Printf("create %s: %v", dst, err)
		return
	}
	defer out.Close()
	_, _ = io.Copy(out, in)
	_ = os.Chmod(dst, 0644)
}

func printFileSize(path string) {
	if info, err := os.Stat(path); err == nil {
		size := float64(info.Size()) / (1024 * 1024)
		log.Printf("%s💾 Archive size: %.2f MB%s", green, size, reset)
	}
}

func cleanupOldFiles(dir string, days int) {
	files, _ := filepath.Glob(filepath.Join(dir, "*.tar.gz"))
	cutoff := time.Now().AddDate(0, 0, -days)
	for _, f := range files {
		if info, err := os.Stat(f); err == nil && info.ModTime().Before(cutoff) {
			log.Printf("🧹 Deleting old archive %s", filepath.Base(f))
			_ = os.Remove(f)
		}
	}
}

func acquireLock() {
	try := func() error {
		f, err := os.OpenFile(lockFile, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err != nil {
			return err
		}
		defer f.Close()
		_, _ = f.WriteString(strconv.Itoa(os.Getpid()))
		return nil
	}
	if err := try(); err == nil {
		return
	}
	data, _ := os.ReadFile(lockFile)
	if pid, _ := strconv.Atoi(strings.TrimSpace(string(data))); pid > 0 {
		if proc, _ := os.FindProcess(pid); proc != nil && proc.Signal(syscall.Signal(0)) == nil {
			log.Fatalf("%sBackup already running (PID %d)%s", red, pid, reset)
		}
	}
	_ = os.Remove(lockFile)
	if err := try(); err != nil {
		log.Fatalf("%sCannot create lock file: %v%s", red, err, reset)
	}
}

func releaseLock() { _ = os.Remove(lockFile) }

func dirSize(root string) (int64, error) {
	var sum int64
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".tar.gz") {
			return err
		}
		if fi, err := os.Stat(path); err == nil {
			sum += fi.Size()
		}
		return nil
	})
	return sum, err
}

func humanMB(b int64) float64 { return float64(b) / (1024 * 1024) }

func isRedisHealthy(port string) bool {
	out, err := exec.Command("redis-cli", "-p", port, "--raw", "PING").Output()
	if err != nil || strings.TrimSpace(string(out)) != "PONG" {
		return false
	}
	beforeStr, err := exec.Command("redis-cli", "-p", port, "--raw", "LASTSAVE").Output()
	if err != nil {
		return false
	}
	before, _ := strconv.ParseInt(strings.TrimSpace(string(beforeStr)), 10, 64)
	_, _ = exec.Command("redis-cli", "-p", port, "BGSAVE").Output()
	deadline := time.Now().Add(time.Duration(saveTimeoutSec) * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(2 * time.Second)
		afterStr, err := exec.Command("redis-cli", "-p", port, "--raw", "LASTSAVE").Output()
		if err != nil {
			return false
		}
		after, _ := strconv.ParseInt(strings.TrimSpace(string(afterStr)), 10, 64)
		if after > before {
			return true
		}
	}
	return false
}

func rotateCopies(dir string, copies int) {
	files, _ := filepath.Glob(filepath.Join(dir, "*.tar.gz"))
	if len(files) <= copies {
		return
	}
	sort.Slice(files, func(i, j int) bool {
		fi, _ := os.Stat(files[i])
		fj, _ := os.Stat(files[j])
		return fi.ModTime().After(fj.ModTime())
	})
	for _, f := range files[copies:] {
		log.Printf("🧹 Deleting extra archive %s", filepath.Base(f))
		_ = os.Remove(f)
	}
}

func rotateCopiesFTP(c *ftp.ServerConn, dir string, copies int) {
	entries, err := c.List(dir)
	if err != nil {
		return
	}
	var files []*ftp.Entry
	for _, e := range entries {
		if e.Type == ftp.EntryTypeFile && strings.HasSuffix(e.Name, ".tar.gz") {
			files = append(files, e)
		}
	}
	if len(files) <= copies {
		return
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Time.After(files[j].Time) })
	for _, e := range files[copies:] {
		remoteFile := filepath.ToSlash(filepath.Join(dir, e.Name))
		log.Printf("🧹 (FTP) Deleting extra archive %s", remoteFile)
		_ = c.Delete(remoteFile)
	}
}