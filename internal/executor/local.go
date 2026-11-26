package executor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/internal/ports"
)

// LocalExecutor executes POSIX sh commands locally (for single-node mode and engine tests).
type LocalExecutor struct {
	RootDir string // optional chroot-like prefix for safe unit testing
}

func NewLocalExecutor(rootDir string) *LocalExecutor {
	return &LocalExecutor{RootDir: rootDir}
}

func (l *LocalExecutor) Connect(_ context.Context, _ ports.Target) (ports.Session, error) {
	return &localSession{rootDir: l.RootDir}, nil
}

func (l *LocalExecutor) Close() error { return nil }

type localSession struct {
	rootDir string
}

func (s *localSession) resolve(p string) string {
	if s.rootDir == "" {
		return p
	}
	return filepath.Join(s.rootDir, filepath.Clean("/"+p))
}

func (s *localSession) Run(ctx context.Context, cmd ports.Command) (ports.Result, error) {
	start := time.Now()
	// When rooted in a sandbox directory for tests, keep mkdir commands inside rootDir too.
	if s.rootDir != "" && strings.HasPrefix(cmd.Script, "mkdir -p ") {
		fields := strings.Fields(cmd.Script)
		if len(fields) >= 3 {
			dirArg := strings.Trim(fields[2], "'\"")
			if err := os.MkdirAll(s.resolve(dirArg), 0o755); err != nil {
				return ports.Result{ExitCode: 1, Duration: time.Since(start)}, err
			}
			return ports.Result{ExitCode: 0, Duration: time.Since(start)}, nil
		}
	}
	if cmd.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cmd.Timeout)
		defer cancel()
	}
	// #nosec G204 -- LocalExecutor intentionally runs engine-generated POSIX sh scripts
	c := exec.CommandContext(ctx, "sh", "-c", cmd.Script)
	c.Env = append(os.Environ(), "LC_ALL=C")
	if cmd.Stdin != nil {
		c.Stdin = cmd.Stdin
	}
	var outBuf, errBuf bytes.Buffer
	if cmd.Stdout != nil {
		c.Stdout = io.MultiWriter(&outBuf, cmd.Stdout)
	} else {
		c.Stdout = &outBuf
	}
	if cmd.Stderr != nil {
		c.Stderr = io.MultiWriter(&errBuf, cmd.Stderr)
	} else {
		c.Stderr = &errBuf
	}
	err := c.Run()
	res := ports.Result{
		Stdout:   outBuf.String(),
		Stderr:   errBuf.String(),
		Duration: time.Since(start),
	}
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			res.ExitCode = ee.ExitCode()
		} else {
			res.ExitCode = 1
		}
		return res, err
	}
	return res, nil
}

func (s *localSession) Upload(_ context.Context, r io.Reader, dst string, mode fs.FileMode, _ string) (string, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	targetPath := s.resolve(dst)
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return "", err
	}
	tmp := filepath.Join(filepath.Dir(targetPath), ".kinstall.tmp."+domain.NewID("f"))
	if err := os.WriteFile(tmp, data, mode.Perm()); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, targetPath); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return hex.EncodeToString(sum[:]), nil
}

func (s *localSession) Download(_ context.Context, src string, w io.Writer) error {
	b, err := os.ReadFile(s.resolve(src))
	if err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
}

func (s *localSession) Stat(_ context.Context, p string) (*ports.RemoteFileInfo, error) {
	full := s.resolve(p)
	st, err := os.Stat(full)
	if err != nil {
		if os.IsNotExist(err) {
			return &ports.RemoteFileInfo{Exists: false}, nil
		}
		return nil, err
	}
	info := &ports.RemoteFileInfo{
		Exists: true,
		IsDir:  st.IsDir(),
		Size:   st.Size(),
		Mode:   st.Mode().Perm(),
	}
	if !st.IsDir() {
		b, rerr := os.ReadFile(full)
		if rerr != nil {
			return nil, fmt.Errorf("read stat sha256: %w", rerr)
		}
		sum := sha256.Sum256(b)
		info.SHA256 = hex.EncodeToString(sum[:])
	}
	return info, nil
}

func (s *localSession) Close() error { return nil }
