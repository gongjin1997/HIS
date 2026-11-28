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

// ExtractLimits guards tar extraction against ZipSlip and ZipBomb attacks.
type ExtractLimits struct {
	MaxFiles      int
	MaxFileBytes  int64
	MaxTotalBytes int64
}

// DefaultLimits returns conservative archive extraction bounds.
func DefaultLimits() ExtractLimits {
	return ExtractLimits{
		MaxFiles:      4096,
		MaxFileBytes:  256 << 20, // 256 MiB
		MaxTotalBytes: 1 << 30,   // 1 GiB
	}
}

// SafeJoin joins relPath under baseDir while rejecting null bytes, absolute escapes, and ".." traversal.
func SafeJoin(baseDir, relPath string) (string, error) {
	if strings.ContainsRune(relPath, '\x00') {
		return "", errors.New("path contains null byte")
	}
	absBase, err := filepath.Abs(filepath.Clean(baseDir))
	if err != nil {
		return "", fmt.Errorf("resolve base dir: %w", err)
	}
	cleanRel := filepath.Clean("/" + strings.ReplaceAll(relPath, "\\", "/"))
	cleanRel = strings.TrimPrefix(cleanRel, "/")
	joined := filepath.Join(absBase, cleanRel)

	rel, err := filepath.Rel(absBase, joined)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes base directory", relPath)
	}
	return joined, nil
}

// SafeExtractTar extracts regular files & directories from a tar stream into destDir.
// Rejects symlinks, hardlinks, device nodes, ZipSlip paths, and ZipBomb sizes.
func SafeExtractTar(r io.Reader, destDir string, limits ExtractLimits) ([]string, error) {
	if limits.MaxFiles <= 0 {
		limits = DefaultLimits()
	}
	if err := os.MkdirAll(destDir, 0o750); err != nil {
		return nil, err
	}
	tr := tar.NewReader(r)
	var (
		extracted  []string
		fileCount  int
		totalBytes int64
	)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read tar entry: %w", err)
		}
		fileCount++
		if fileCount > limits.MaxFiles {
			return nil, fmt.Errorf("tar archive exceeds max file count %d", limits.MaxFiles)
		}
		if strings.Contains(hdr.Name, "..") {
			return nil, fmt.Errorf("zip-slip path rejected: %q", hdr.Name)
		}
		target, err := SafeJoin(destDir, hdr.Name)
		if err != nil {
			return nil, err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o750); err != nil {
				return nil, err
			}
		case tar.TypeReg:
			if hdr.Size < 0 || hdr.Size > limits.MaxFileBytes {
				return nil, fmt.Errorf("file %q exceeds max size %d", hdr.Name, limits.MaxFileBytes)
			}
			if totalBytes+hdr.Size > limits.MaxTotalBytes {
				return nil, fmt.Errorf("archive exceeds max total size %d", limits.MaxTotalBytes)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
				return nil, err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
			if err != nil {
				return nil, err
			}
			written, copyErr := io.Copy(out, io.LimitReader(tr, limits.MaxFileBytes+1))
			closeErr := out.Close()
			if copyErr != nil {
				return nil, copyErr
			}
			if closeErr != nil {
				return nil, closeErr
			}
			if written > limits.MaxFileBytes {
				return nil, fmt.Errorf("file %q exceeded declared byte limit", hdr.Name)
			}
			totalBytes += written
			if totalBytes > limits.MaxTotalBytes {
				return nil, fmt.Errorf("archive exceeded total byte limit %d", limits.MaxTotalBytes)
			}
			extracted = append(extracted, target)
		default:
			return nil, fmt.Errorf("disallowed tar entry type %d for %q", hdr.Typeflag, hdr.Name)
		}
	}
	return extracted, nil
}

// AtomicWriteFile writes data to a sibling temp file, fsyncs, sets mode, and atomically renames.
func AtomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".kinstall-tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
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
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}
