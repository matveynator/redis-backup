//go:build !windows
// +build !windows

package main

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
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

// Runtime-overrideable defaults
var (
	backupPath string // root directory for all backups
	keepDays   int    // daily retention in days (local)
	maxCopies  int    // leave only <n> newest daily *.tar.gz (0 = unlimited)

	// FTP related
	ftpConfFile   string
	ftpHost       string
	ftpUser       string
	ftpPass       string
	ftpKeepFactor int // remote retention multiplier
	ftpEnabled    bool

	// other runtime flags
	excludePortsCSV string
	checkHours      int
)

// internal helpers derived from flags
var excludePorts map[string]struct{}

// ANSI colors for terminal logs
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
	// Define flags
	listFlag := flag.Bool("list", false, "List backups and exit")
	restoreFlag := flag.Bool("restore", false, "Interactive restore wizard")
	helpFlag := flag.Bool("help", false, "Show help and exit")

	flag.StringVar(&backupPath, "backup-path", "/backup", "Root directory for backups")
	flag.IntVar(&keepDays, "days", 30, "Days to keep daily backups (local)")
	flag.IntVar(&maxCopies, "copies", 0, "Max number of daily snapshots to keep (0 = unlimited)")
	flag.IntVar(&maxCopies, "c", 0, "Alias for --copies")

	// New: exclusion list and check
	flag.StringVar(&excludePortsCSV, "exclude-ports", "", "Comma-separated list of Redis ports to skip during backup/check")
	flag.IntVar(&checkHours, "check", 0, "Run integrity check; value = max allowed hours since last backup. 0 disables check mode.")

	// New: FTP options
	flag.StringVar(&ftpConfFile, "ftp-conf", "/etc/ftp-backup.conf", "Path to FTP credentials file")
	flag.StringVar(&ftpHost, "ftp-host", "", "Override FTP host (otherwise taken from conf file)")
	flag.StringVar(&ftpUser, "ftp-user", "", "Override FTP username (otherwise taken from conf file)")
	flag.StringVar(&ftpPass, "ftp-pass", "", "Override FTP password (otherwise taken from conf file)")
	flag.IntVar(&ftpKeepFactor, "ftp-keep-factor", 4, "Retention multiplier for FTP (remoteKeepDays = keepDays * factor)")

	flag.Parse()

	// Prepare exclusion map
	excludePorts = make(map[string]struct{})
	if excludePortsCSV != "" {
		for _, p := range strings.Split(excludePortsCSV, ",") {
			excludePorts[strings.TrimSpace(p)] = struct{}{}
		}
	}

	// Check if we are running in check mode first
	if checkHours > 0 {
		runCheckMode()
		return
	}

	// Normal operational modes
	switch {
	case *helpFlag:
		printHelp()
	case *listFlag:
		listBackups()
	case *restoreFlag:
		interactiveRestore()
	case checkHours > 0:
		runCheckMode()

	default:
		// ← здесь «боевой» режим
		acquireLock()
		// гарантируем снятие лока даже при ^C / kill
		defer releaseLock()
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		go func() { <-sig; releaseLock(); os.Exit(1) }()

		initFTP()
		runBackup()
	}

}

/******************** HELP ********************/
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

	fmt.Printf("%sBACKUP CONTROL and MONITORING%s\n", cyan, reset)
	fmt.Println("  --exclude-ports <csv>     Comma‑separated list of Redis ports NOT to back up")
	fmt.Println("  --check <hours>           Verify freshness/size; CRITICAL if older than <hours>")

	fmt.Printf("%sFTP OFF‑SITE%s\n", cyan, reset)
	fmt.Println("  --ftp-conf <file>         Credentials file (default: /etc/ftp-backup.conf)")
	fmt.Println("  --ftp-host <host>         FTP host (overrides conf)")
	fmt.Println("  --ftp-user <user>         FTP username")
	fmt.Println("  --ftp-pass <pass>         FTP password")
	fmt.Println("  --ftp-keep-factor <n>     Store data on FTP n× longer than locally (default: 4)")

	fmt.Printf("%sEXAMPLES%s\n", cyan, reset)
	fmt.Printf("  # Basic backup\n  sudo %s\n\n", exe)
	fmt.Printf("  # Exclude session caches (ports 6380,6381)\n  sudo %s --exclude-ports 6380,6381\n\n", exe)
	fmt.Printf("  # Nagios check – CRITICAL if older than 25 h\n  %s --check 25\n", exe)

	// Live preview of detected Redis instances and RDB sizes
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
			fmt.Printf("  • port %s → %s  (%.1f MB)\n", port, rdbPath, size)
		}
	}
}

/**************** PERMISSION HELPERS *****************/
func suggestSudo(err error) {
	if err == nil {
		return
	}
	if os.IsPermission(err) || strings.Contains(err.Error(), "permission denied") {
		log.Printf("%sPermission denied – you may need to run with sudo%s", yellow, reset)
	}
}

/******************** LIST ********************/
func listBackups() {
	host, _ := os.Hostname()
	root := filepath.Join(backupPath, host, backupSubdir) // ← добавили backupSubdir

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

/**************** INTERACTIVE RESTORE *********/
func interactiveRestore() {
	host, _ := os.Hostname()
	root := filepath.Join(backupPath, host, backupSubdir) // ← добавили backupSubdir
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

	dailyDir := filepath.Join(root, "redis_"+port, "daily") // ← путь через backupSubdir
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

	fmt.Printf("%s⚠  Redis %s will be restored from %s. Continue? (y/N): %s",
		yellow, port, archive, reset)
	confirm, _ := reader.ReadString('\n')
	confirm = strings.ToLower(strings.TrimSpace(confirm))
	if confirm != "y" && confirm != "yes" {
		fmt.Println("Cancelled.")
		return
	}

	restoreBackup(port, archive) // restoreBackup тоже обновлён, см. ниже
}

/******************* BACKUP LOOP *******************/
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

		// FTP replication
		if ftpEnabled && archivePath != "" {
			remoteRel := strings.TrimPrefix(archivePath, backupPath)
			remoteRel = strings.TrimPrefix(remoteRel, string(os.PathSeparator))
			uploadToFTP(archivePath, remoteRel)
		}
		log.Printf("%s----------------------------------------%s", cyan, reset)
	}
}

/***************** REDIS HELPERS *******************/

func detectRedisPorts() []string {
	var ports []string

	conns, err := net.Connections("tcp")
	if err != nil {
		log.Println("net.Connections error:", err)
		return ports
	}

	for _, conn := range conns {
		if conn.Status != "LISTEN" || conn.Pid == 0 {
			continue
		}

		proc, err := process.NewProcess(conn.Pid)
		if err != nil {
			continue
		}

		name, err := proc.Name()
		if err != nil || !strings.Contains(strings.ToLower(name), "redis-server") {
			continue
		}

		if conn.Laddr.Port != 0 {
			ports = append(ports, strconv.Itoa(int(conn.Laddr.Port)))
		}
	}

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

/**************** BACKUP SINGLE INSTANCE ************/
func backupInstance(port, rdbPath, host string, now time.Time) string {
	inst := "redis_" + port
	base := filepath.Join(backupPath, host, backupSubdir, inst) // ← добавили backupSubdir

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

/********************** RESTORE ************************/
func restoreBackup(port, archiveName string) {
	host, _ := os.Hostname()
	inst := "redis_" + port
	archivePath := filepath.Join(backupPath, host, backupSubdir, // ← добавили backupSubdir
		inst, "daily", archiveName)

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

	// --- сохраняем старый RDB (если был) ---
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

/********************** FTP ***************************/
func initFTP() {
	// Determine if ftp should be enabled
	if _, err := os.Stat(ftpConfFile); err == nil {
		// parse file
		if err := parseFTPConf(ftpConfFile); err != nil {
			log.Printf("%sUnable to parse FTP conf: %v. FTP disabled.%s", red, err, reset)
			ftpEnabled = false
			return
		}
	}

	// Overrides from flags if provided
	if ftpHost != "" {
		ftpEnabled = true
	}

	if !ftpEnabled {
		return
	}

	log.Printf("%s🌐 FTP replication enabled → %s%s", cyan, ftpHost, reset)
}

func parseFTPConf(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.Contains(line, "=") {
			continue
		}
		kv := strings.SplitN(line, "=", 2)
		key := strings.Trim(kv[0], " \"")
		val := strings.Trim(kv[1], " \"")
		switch key {
		case "FTP_HOST":
			ftpHost = val
		case "FTP_USER":
			ftpUser = val
		case "FTP_PASS":
			ftpPass = val
		}
	}
	if ftpHost != "" {
		ftpEnabled = true
	}
	return scanner.Err()
}

func uploadToFTP(localPath, remoteRel string) {
	if !ftpEnabled {
		return
	}
	c, err := ftp.Dial(ftpHost + ":21")
	if err != nil {
		log.Printf("%sFTP dial: %v%s", red, err, reset)
		return
	}
	defer c.Quit()

	if err := c.Login(ftpUser, ftpPass); err != nil {
		log.Printf("%sFTP login: %v%s", red, err, reset)
		return
	}

	// Ensure remote directories exist
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
	log.Printf("%s⇪ Uploading to FTP: %s%s", cyan, remotePath, reset)
	if err := c.Stor(remotePath, f); err != nil {
		log.Printf("%sFTP upload error: %v%s", red, err, reset)
		return
	}

	// Remote retention cleanup (only for daily archives)
	if strings.Contains(remotePath, "/daily/") {
		remoteDailyDir := filepath.ToSlash(filepath.Dir(remotePath))
		cleanupOldFilesFTP(c, remoteDailyDir, keepDays*ftpKeepFactor)
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

/********************** CHECK MODE ********************/
func runCheckMode() {
	// —--- разбираем конфигурацию FTP, если она есть
	if _, err := os.Stat(ftpConfFile); err == nil {
		_ = parseFTPConf(ftpConfFile)
	}
	if ftpHost != "" { // могли переопределить флагами
		ftpEnabled = true
	}

	host, _ := os.Hostname()
	now := time.Now()
	threshold := now.Add(-time.Duration(checkHours) * time.Hour) // «свежесть» бэкапа

	problems := []string{}
	severity := 0 // 0-OK, 1-WARNING, 2-CRITICAL

	/************* ЛОКАЛЬНЫЕ БЭКАПЫ *************/
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
			problems = append(problems,
				fmt.Sprintf("Redis %s: older than %d h", port, checkHours))
			severity = max(severity, 2)
		}

		if fi, err := os.Stat(latestFile); err == nil {
			latestSetSize += fi.Size()
			latestFiles++
		}

		// усыхание архива
		currentRDB := filepath.Join(getRedisDir(port), getRedisRDB(port))
		if sizeOK, err := compareSizes(currentRDB, latestFile); err == nil && !sizeOK {
			problems = append(problems,
				fmt.Sprintf("Redis %s: backup size <75%%", port))
			severity = max(severity, 2)
		}
	}

	/************* ДИСК *************/
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

	/************* FTP-БЭКАПЫ (если задействован FTP) *************/
	var ftpLatestSetSize int64
	var ftpLatestFiles int
	if ftpEnabled {
		c, err := ftp.Dial(ftpHost+":21", ftp.DialWithTimeout(5*time.Second))
		if err != nil {
			problems = append(problems, fmt.Sprintf("FTP connect: %v", err))
			severity = max(severity, 2)
		} else {
			if err := c.Login(ftpUser, ftpPass); err != nil {
				problems = append(problems, fmt.Sprintf("FTP login: %v", err))
				severity = max(severity, 2)
			} else {
				for _, port := range ports {
					if _, skip := excludePorts[port]; skip {
						continue
					}
					remoteDaily := filepath.ToSlash(filepath.Join("/",
						host, backupSubdir, "redis_"+port, "daily"))
					latestPath, latestSize, latestTime := findLatestFTPArchive(c, remoteDaily)

					if latestPath == "" {
						problems = append(problems,
							fmt.Sprintf("FTP redis %s: NO BACKUP", port))
						severity = max(severity, 2)
						continue
					}
					if latestTime.Before(threshold) {
						problems = append(problems,
							fmt.Sprintf("FTP redis %s: older than %d h", port, checkHours))
						severity = max(severity, 2)
					}

					ftpLatestSetSize += latestSize
					ftpLatestFiles++
				}
			}
			_ = c.Quit()
		}
	}

	/************* КРАСИВЫЕ СТРОКИ МЕТРИК *************/
	localMetrics := fmt.Sprintf(
		"Backups total: %.1f MB (%d files); full set: %.1f MB; free: %.1f MB; can store ≈ %d full sets; used by backups: %.1f%%",
		humanMB(totalSize), latestFiles, humanMB(latestSetSize),
		humanMB(diskFree), copiesPossible, usedPct)

	ftpMetrics := ""
	if ftpEnabled && ftpLatestFiles > 0 {
		ftpMetrics = fmt.Sprintf("FTP latest full set: %.1f MB (%d files)",
			humanMB(ftpLatestSetSize), ftpLatestFiles)
	}

	/************* ВЫВОД ДЛЯ NAGIOS: СТАТУС → ДЕТАЛИ *************/
	var statusText string
	switch severity {
	case 2:
		statusText = "CRITICAL"
	case 1:
		statusText = "WARNING"
	default:
		statusText = "OK"
	}

	details := "all redis backups fresh and sufficiently sized. 👍"
	if len(problems) > 0 {
		details = strings.Join(problems, "; ")
	}

	fmt.Printf("%s: %s\n", statusText, details) // ← сначала статус
	fmt.Println(localMetrics)
	if ftpMetrics != "" {
		fmt.Println(ftpMetrics)
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

/***************** FTP helper: свежий архив *****************/
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
		if info, err := os.Stat(path); err == nil {
			if info.ModTime().After(newestTime) {
				newestTime = info.ModTime()
				newest = path
			}
		}
	}
	return newest, newestTime
}

func compareSizes(originalPath, archivePath string) (bool, error) {
	// size of original RDB
	origInfo, err := os.Stat(originalPath)
	if err != nil {
		return true, err // ignore if cannot stat original
	}
	origSize := origInfo.Size()

	// extract temp file to check size (stream) – we only need the uncompressed payload of tar entry
	f, err := os.Open(archivePath)
	if err != nil {
		return true, err
	}
	defer f.Close()
	gr, err := gzip.NewReader(f)
	if err != nil {
		return true, err
	}
	defer gr.Close()
	tr := tar.NewReader(gr)
	hdr, err := tr.Next()
	if err != nil {
		return true, err
	}
	backupSize := hdr.Size

	return float64(backupSize) >= 0.75*float64(origSize), nil
}

/********************** FILE OPS **********************/
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
			return err // уже есть файл
		}
		defer f.Close()
		_, _ = f.WriteString(strconv.Itoa(os.Getpid()))
		return nil // лока получена
	}

	if err := try(); err == nil {
		return
	}

	// файл существует – проверяем жив ли владелец
	data, _ := os.ReadFile(lockFile)
	if pid, _ := strconv.Atoi(strings.TrimSpace(string(data))); pid > 0 {
		if proc, _ := os.FindProcess(pid); proc != nil &&
			proc.Signal(syscall.Signal(0)) == nil {
			log.Fatalf("%sBackup already running (PID %d)%s", red, pid, reset)
		}
	}

	// владелец умер – удаляем «висящий» лок и пробуем ещё раз
	_ = os.Remove(lockFile)
	if err := try(); err != nil {
		log.Fatalf("%sCannot create lock file: %v%s", red, err, reset)
	}
}

func releaseLock() {
	_ = os.Remove(lockFile)
}

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

// isRedisHealthy returns true if redis-cli PING returns PONG *and* SAVE succeeds.
func isRedisHealthy(port string) bool {
	out, err := exec.Command("redis-cli", "-p", port, "--raw", "PING").Output()
	if err != nil || strings.TrimSpace(string(out)) != "PONG" {
		return false
	}
	if _, err := exec.Command("redis-cli", "-p", port, "SAVE").Output(); err != nil {
		return false
	}
	return true
}

// rotateCopies keeps only <copies> newest *.tar.gz in dir.
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
