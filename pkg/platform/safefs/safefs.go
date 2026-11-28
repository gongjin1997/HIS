package safefs

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type ExtractLimits struct {
	MaxFiles      int
	MaxTotalBytes int64
	MaxFileBytes  int64
}

func DefaultLimits() ExtractLimits {
	return ExtractLimits{MaxFiles: 4096, MaxTotalBytes: 1 << 30, MaxFileBytes: 256 << 20}
}

// SafeJoin prevents null-byte and directory traversal outside baseDir.
func SafeJoin(baseDir, relPath string) (string, error) {
	if strings.ContainsRune(relPath, 0) {
		return "", errors.New("null byte in path")
	}
	absBase, err := filepath.Abs(filepath.Clean(baseDir))
	if err != nil {
		return "", err
	}
	target := filepath.Clean(filepath.Join(absBase, relPath))
	if target != absBase && !strings.HasPrefix(target, absBase+string(os.PathSeparator)) {
		return "", fmt.Errorf("path traversal blocked: %q", relPath)
	}
	return target, nil
}

// SafeExtractTar blocks ZipSlip, ZipBomb, symlinks, and hardlinks during archive extraction.
func SafeExtractTar(r io.Reader, destDir string, lim ExtractLimits) ([]string, error) {
	if lim.MaxFiles <= 0 {
		lim = DefaultLimits()
	}
	if err := os.MkdirAll(destDir, 0o750); err != nil {
		return nil, err
	}
	tr := tar.NewReader(r)
	var (
		totalBytes int64
		files      int
		extracted  []string
	)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return extracted, nil
		}
		if err != nil {
			return nil, err
		}
		files++
		if files > lim.MaxFiles {
			return nil, errors.New("tar extract exceeded max file count")
		}
		if hdr.Typeflag == tar.TypeSymlink || hdr.Typeflag == tar.TypeLink {
			return nil, fmt.Errorf("symlink/hardlink forbidden in archive: %s", hdr.Name)
		}
		target, err := SafeJoin(destDir, hdr.Name)
		if err != nil {
			return nil, err
		}
		if hdr.Typeflag == tar.TypeDir {
			if err := os.MkdirAll(target, 0o750); err != nil {
				return nil, err
			}
			continue
		}
		if hdr.Typeflag != tar.TypeReg {
			return nil, fmt.Errorf("disallowed tar entry type %d: %s", hdr.Typeflag, hdr.Name)
		}
		if hdr.Size < 0 || hdr.Size > lim.MaxFileBytes {
			return nil, fmt.Errorf("file %s exceeds single file limit", hdr.Name)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			return nil, err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
		if err != nil {
			return nil, err
		}
		written, copyErr := io.Copy(out, io.LimitReader(tr, lim.MaxFileBytes+1))
		closeErr := out.Close()
		if copyErr != nil || closeErr != nil {
			return nil, fmt.Errorf("extract write failed: %w", errors.Join(copyErr, closeErr))
		}
		if written > lim.MaxFileBytes {
			return nil, errors.New("zipbomb detected: actual bytes exceeded limit")
		}
		totalBytes += written
		if totalBytes > lim.MaxTotalBytes {
			return nil, errors.New("tar extract exceeded total byte limit")
		}
		extracted = append(extracted, target)
	}
}

func AtomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".liveops-tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}
