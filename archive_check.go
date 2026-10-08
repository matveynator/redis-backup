//go:build !windows
// +build !windows

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"hash/crc64"
	"io"
	"os"
	"strconv"
)

const (
	redisRDBHeaderSize = 9
	redisRDBEOF        = 0xff
	redisCRC64Poly     = uint64(0x95ac9329ac4bc9b5)
)

var redisCRC64Table = crc64.MakeTable(redisCRC64Poly)

// validateBackupArchive is the lightweight check used by --check/Nagios.
// It intentionally reads only the gzip/tar headers and the first RDB bytes;
// it must not stream/decompress a multi-GB backup on every monitoring run.
//
// When snapshot metadata is available, the archived RDB size is compared with
// the size captured for that backup. This retains the useful size sanity check
// without comparing with a live dump.rdb that may have grown since backup.
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
	hdr, err := tr.Next()
	if err != nil {
		return fmt.Errorf("tar: %w", err)
	}
	if !hdr.FileInfo().Mode().IsRegular() {
		return fmt.Errorf("archive does not start with a regular RDB file")
	}
	if hdr.Size < redisRDBHeaderSize {
		return fmt.Errorf("RDB payload too small: %d bytes", hdr.Size)
	}

	header := make([]byte, redisRDBHeaderSize)
	if _, err := io.ReadFull(tr, header); err != nil {
		return fmt.Errorf("RDB header: %w", err)
	}
	if !isRedisRDBHeader(header) {
		return fmt.Errorf("invalid Redis RDB header")
	}

	if meta, err := readBackupMeta(archivePath); err == nil && meta.OriginalSize > 0 {
		// Keep the historical tolerance to avoid false alerts if the source RDB
		// changed slightly between archive creation and metadata capture.
		if float64(hdr.Size) < 0.95*float64(meta.OriginalSize) {
			return fmt.Errorf("RDB payload size %d is below 95%% of snapshot size %d", hdr.Size, meta.OriginalSize)
		}
	}
	return nil
}

// validateBackupArchiveDeep performs a full streaming validation of gzip/tar
// plus the Redis RDB EOF/checksum. It is deliberately separate from the normal
// monitoring path because validating a large compressed RDB is CPU intensive.
func validateBackupArchiveDeep(archivePath string) error {
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
			if err := validateRedisRDB(io.MultiReader(bytes.NewReader(header), tr)); err != nil {
				return fmt.Errorf("%s: %w", hdr.Name, err)
			}
			continue
		}
		if _, err := io.Copy(io.Discard, tr); err != nil {
			return fmt.Errorf("read %s: %w", hdr.Name, err)
		}
	}

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

// validateRedisRDB checks the RDB EOF/footer and, for RDB v5+, the CRC64
// written by Redis. The checksum covers the complete RDB through the 0xff EOF
// opcode and excludes only the final eight checksum bytes.
func validateRedisRDB(r io.Reader) error {
	header := make([]byte, redisRDBHeaderSize)
	if _, err := io.ReadFull(r, header); err != nil {
		return fmt.Errorf("RDB header: %w", err)
	}
	version, ok := redisRDBVersion(header)
	if !ok {
		return fmt.Errorf("invalid Redis RDB header")
	}
	if version < 5 {
		return validateLegacyRedisRDB(r)
	}

	crc := redisCRC64Update(0, header)
	var tail [8]byte
	tailLen := 0
	var dataBytes int64
	var lastData byte
	buf := make([]byte, 32*1024)

	for {
		n, err := r.Read(buf)
		if n > 0 {
			p := buf[:n]
			total := tailLen + len(p)
			if total <= len(tail) {
				copy(tail[tailLen:], p)
				tailLen = total
			} else {
				process := total - len(tail)
				fromTail := process
				if fromTail > tailLen {
					fromTail = tailLen
				}
				if fromTail > 0 {
					crc = redisCRC64Update(crc, tail[:fromTail])
					lastData = tail[fromTail-1]
					dataBytes += int64(fromTail)
					copy(tail[:], tail[fromTail:tailLen])
					tailLen -= fromTail
					process -= fromTail
				}
				if process > 0 {
					crc = redisCRC64Update(crc, p[:process])
					lastData = p[process-1]
					dataBytes += int64(process)
					p = p[process:]
				}
				copy(tail[tailLen:], p)
				tailLen += len(p)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("RDB body: %w", err)
		}
	}

	if tailLen != len(tail) {
		return fmt.Errorf("RDB checksum footer truncated")
	}
	if dataBytes == 0 || lastData != redisRDBEOF {
		return fmt.Errorf("RDB EOF opcode missing")
	}

	stored := binary.LittleEndian.Uint64(tail[:])
	if stored != 0 && stored != crc {
		return fmt.Errorf("RDB checksum mismatch: stored %016x, calculated %016x", stored, crc)
	}
	return nil
}

func validateLegacyRedisRDB(r io.Reader) error {
	var last byte
	var count int64
	buf := make([]byte, 32*1024)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			last = buf[n-1]
			count += int64(n)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("RDB body: %w", err)
		}
	}
	if count == 0 || last != redisRDBEOF {
		return fmt.Errorf("RDB EOF opcode missing")
	}
	return nil
}

func redisRDBVersion(header []byte) (int, bool) {
	if !isRedisRDBHeader(header) {
		return 0, false
	}
	v, err := strconv.Atoi(string(header[5:]))
	return v, err == nil
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

func redisCRC64Update(crc uint64, data []byte) uint64 {
	return ^crc64.Update(^crc, redisCRC64Table, data)
}
