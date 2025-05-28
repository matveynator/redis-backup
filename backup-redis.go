package main

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Runtime-overrideable defaults
var (
	backupPath string // root directory for all backups
	keepDays   int    // daily retention
)

// ANSI colors for terminal logs
const (
	green  = "\033[32m"
	yellow = "\033[33m"
	red    = "\033[31m"
	cyan   = "\033[36m"
	reset  = "\033[0m"
)

func main() {
	// define flags
	listFlag := flag.Bool("list", false, "List backups and exit")
	restoreFlag := flag.Bool("restore", false, "Interactive restore wizard")
	helpFlag := flag.Bool("help", false, "Show help and exit")

	flag.StringVar(&backupPath, "backup-path", "/backup", "Root directory for backups")
	flag.IntVar(&keepDays, "days", 30, "Days to keep daily backups")

	flag.Parse()

	switch {
	case *helpFlag:
		printHelp()
	case *listFlag:
		listBackups()
	case *restoreFlag:
		interactiveRestore()
	default:
		runBackup()
	}
}

/******************** HELP ********************/
func printHelp() {
	exe := filepath.Base(os.Args[0])
	fmt.Printf(`Usage: %s [options]

Options:
  --list                 list backups and exit
  --restore              interactive restore wizard
  --backup-path <dir>    root directory for backups (default: /backup)
  --days <n>             days to keep daily backups (default: 30)
  --help                 show help

By default *all* detected Redis instances are fully backed up.\n`, exe)

	host, _ := os.Hostname()
	ports := detectRedisPorts()
	if len(ports) == 0 {
		fmt.Println("No running redis-server processes detected.")
		return
	}

	now := time.Now()
	ts := now.Format("2006-01-02_15-04-05")
	fmt.Println("\nPlanned backup targets:")
	for _, port := range ports {
		dir := getRedisDir(port)
		file := getRedisRDB(port)
		if dir == "" || file == "" {
			continue
		}
		rdbPath := filepath.Join(dir, file)
		archive := filepath.Join(backupPath, host, "redis_"+port, "daily", fmt.Sprintf("%s_redis_%s.tar.gz", ts, port))
		fmt.Printf("  • Port %s: %s → %s\n", port, rdbPath, archive)
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
	root := filepath.Join(backupPath, host)
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
	root := filepath.Join(backupPath, host)
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
		backupInstance(port, rdbPath, host, now)
	}
}

/***************** REDIS HELPERS *******************/
func detectRedisPorts() []string {
	out, err := exec.Command("pgrep", "-a", "redis-server").Output()
	if err != nil {
		suggestSudo(err)
		log.Println("pgrep error:", err)
		return nil
	}
	var ports []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.Contains(line, "redis-server") {
			for _, fld := range strings.Fields(line) {
				if strings.Contains(fld, ":") {
					parts := strings.Split(fld, ":")
					ports = append(ports, parts[len(parts)-1])
				}
			}
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
func backupInstance(port, rdbPath, host string, now time.Time) {
	inst := "redis_" + port
	base := filepath.Join(backupPath, host, inst)
	daily := filepath.Join(base, "daily")
	weekly := filepath.Join(base, "weekly")
	monthly := filepath.Join(base, "monthly")
	yearly := filepath.Join(base, "yearly")
	for _, d := range []string{daily, weekly, monthly, yearly} {
		if err := os.MkdirAll(d, 0755); err != nil {
			suggestSudo(err)
			log.Printf("mkdir %s: %v", d, err)
			return
		}
	}

	ts := now.Format("2006-01-02_15-04-05")
	archive := filepath.Join(daily, fmt.Sprintf("%s_%s.tar.gz", ts, inst))

	log.Printf("%s📦 Archiving %s …%s", cyan, archive, reset)
	if err := createTarGz(archive, []string{rdbPath}); err != nil {
		suggestSudo(err)
		log.Printf("%sArchive error: %v%s", red, err, reset)
		return
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

	cleanupOldFiles(daily, keepDays)
	log.Printf("%s----------------------------------------%s", cyan, reset)
}

/********************** RESTORE ************************/
func restoreBackup(port, archiveName string) {
	host, _ := os.Hostname()
	inst := "redis_" + port
	archivePath := filepath.Join(backupPath, host, inst, "daily", archiveName)
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

	// capture original metadata if present
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
