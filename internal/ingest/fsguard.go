package ingest

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
)

const MaxDepth = 5

var IgnoredDirs = map[string]struct{}{
	"worktrees":         {},
	"node_modules":      {},
	".git":              {},
	"dist":              {},
	"build":             {},
	"brain":             {},
	".system_generated": {},
	"scratch":           {},
	"tmp":               {},
	".venv":             {},
	"venv":              {},
	"tokenizer-venv":    {},
	"__pycache__":       {},
}

var auxiliarySuffixRegex = regexp.MustCompile(`(?:-shm|-journal|\.lock|\.swp|\.swx|\.tmp|~)$`)

// IsIgnoredPath reports whether any path segment is in IgnoredDirs.
func IsIgnoredPath(rel string) bool {
	if rel == "" {
		return false
	}
	parts := strings.FieldsFunc(rel, func(r rune) bool { return r == '/' || r == '\\' })
	for _, part := range parts {
		if _, ok := IgnoredDirs[part]; ok {
			return true
		}
	}
	return false
}

// IsAuxiliaryFile reports whether a filename is an auxiliary SQLite or editor swap file.
func IsAuxiliaryFile(name string) bool {
	return auxiliarySuffixRegex.MatchString(name)
}

// OpenSafeReadOnly opens a regular file without following symlinks and without blocking on FIFOs/devices.
func OpenSafeReadOnly(path string) (*os.File, os.FileInfo, error) {
	linfo, err := os.Lstat(path)
	if err != nil {
		return nil, nil, err
	}
	if linfo.Mode()&os.ModeSymlink != 0 || !linfo.Mode().IsRegular() {
		return nil, nil, errors.New("not a regular file")
	}

	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	finfo, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	if !finfo.Mode().IsRegular() {
		_ = f.Close()
		return nil, nil, errors.New("opened descriptor is not a regular file")
	}
	return f, finfo, nil
}

// IsRegularFile checks os.Lstat without following symlinks.
func IsRegularFile(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode()&os.ModeSymlink == 0 && info.Mode().IsRegular()
}

// WalkBounded performs a depth-bounded directory walk skipping symlinks and IgnoredDirs.
func WalkBounded(root string, pattern *regexp.Regexp) ([]string, error) {
	var out []string
	var walkDir func(dir string, depth int)
	walkDir = func(dir string, depth int) {
		if depth > MaxDepth {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, d := range entries {
			if d.Type()&os.ModeSymlink != 0 {
				continue
			}
			full := filepath.Join(dir, d.Name())
			if d.IsDir() {
				if _, skip := IgnoredDirs[d.Name()]; !skip {
					walkDir(full, depth+1)
				}
				continue
			}
			if d.Type().IsRegular() && (pattern == nil || pattern.MatchString(d.Name())) {
				out = append(out, full)
			}
		}
	}
	walkDir(root, 0)
	sort.Strings(out)
	return out, nil
}

// SQLiteSig captures a zero-lock change fingerprint for a SQLite file and its -wal sidecar.
type SQLiteSig struct {
	Key      string
	LatestMs int64
	Inode    uint64
}

func statInode(info os.FileInfo) uint64 {
	if st, ok := info.Sys().(*syscall.Stat_t); ok && st != nil {
		return st.Ino
	}
	return 0
}

// SQLiteSignature computes a change fingerprint without opening SQLite or creating -shm.
func SQLiteSignature(path string) (*SQLiteSig, error) {
	linfo, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if linfo.Mode()&os.ModeSymlink != 0 || !linfo.Mode().IsRegular() {
		return nil, errors.New("not a regular sqlite file")
	}

	ino := statInode(linfo)
	mtimeNs := linfo.ModTime().UnixNano()
	latestMs := linfo.ModTime().UnixMilli()

	headerTag := ""
	if linfo.Size() >= 100 {
		if f, _, err := OpenSafeReadOnly(path); err == nil {
			hdr := make([]byte, 100)
			n, _ := io.ReadFull(f, hdr)
			_ = f.Close()
			if n == 100 {
				chg := binary.BigEndian.Uint32(hdr[24:28])
				ver := binary.BigEndian.Uint32(hdr[92:96])
				headerTag = fmt.Sprintf("%d:%d", chg, ver)
			}
		}
	}

	walSize := int64(-1)
	walMtimeNs := int64(-1)
	walTail := ""
	walPath := path + "-wal"
	if winfo, err := os.Lstat(walPath); err == nil && winfo.Mode()&os.ModeSymlink == 0 && winfo.Mode().IsRegular() {
		walSize = winfo.Size()
		walMtimeNs = winfo.ModTime().UnixNano()
		if wm := winfo.ModTime().UnixMilli(); wm > latestMs {
			latestMs = wm
		}
		if walSize >= 24 {
			if wf, _, err := OpenSafeReadOnly(walPath); err == nil {
				tail := make([]byte, 24)
				if n, err := wf.ReadAt(tail, walSize-24); err == nil && n == 24 {
					walTail = hex.EncodeToString(tail)
				}
				_ = wf.Close()
			}
		}
	}

	key := fmt.Sprintf("%d:%d:%d:%s:%d:%d:%s", ino, linfo.Size(), mtimeNs, headerTag, walSize, walMtimeNs, walTail)
	return &SQLiteSig{
		Key:      key,
		LatestMs: latestMs,
		Inode:    ino,
	}, nil
}

// TailSignature computes SHA-256 of the last min(4096, size) bytes of an open file.
func TailSignature(f *os.File, size int64) (string, error) {
	const sampleBytes = 4096
	n := size
	if n > sampleBytes {
		n = sampleBytes
	}
	if n <= 0 {
		sum := sha256.Sum256(nil)
		return hex.EncodeToString(sum[:]), nil
	}
	buf := make([]byte, n)
	readN, err := f.ReadAt(buf, size-n)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	sum := sha256.Sum256(buf[:readN])
	return hex.EncodeToString(sum[:]), nil
}
