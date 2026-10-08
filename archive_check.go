//go:build !windows
// +build !windows

package main

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
)

const redisRDBHeaderSize = 9

// validateBackupArchive verifies the latest backup itself instead of comparing
// it with the current live dump.rdb, whose size may legitimately change after
// the backup was created.
func validateBackupArchive(archivePath string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("gzip: %w", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	rdbFiles := 0

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar: %w", err)
		}
		if !hdr.FileInfo().Mode().IsRegular() {
			continue
		}

		header := make([]byte, redisRDBHeaderSize)
		n, readErr := io.ReadFull(tr, header)
		if readErr != nil && readErr != io.EOF && readErr != io.ErrUnexpectedEOF {
			return fmt.Errorf("read %s: %w", hdr.Name, readErr)
		}

		if n == redisRDBHeaderSize && isRedisRDBHeader(header) {
			rdbFiles++
		}

		// Consume the complete tar entry. This forces gzip/tar to detect a
		// truncated payload instead of validating only the tar header.
		if _, err := io.Copy(io.Discard, tr); err != nil {
			return fmt.Errorf("read %s: %w", hdr.Name, err)
		}
	}

	// Read through the gzip trailer so its checksum is verified too.
	if _, err := io.Copy(io.Discard, gr); err != nil {
		return fmt.Errorf("gzip checksum: %w", err)
	}

	if rdbFiles == 0 {
		return fmt.Errorf("archive does not contain a Redis RDB file")
	}
	if rdbFiles > 1 {
		return fmt.Errorf("archive contains %d Redis RDB files", rdbFiles)
	}

	return nil
}

func isRedisRDBHeader(header []byte) bool {
	if len(header) != redisRDBHeaderSize || string(header[:5]) != "REDIS" {
		return false
	}
	for _, b := range header[5:] {
		if b < '0' || b > '9' {
			return false
		}
	}
	return true
}
