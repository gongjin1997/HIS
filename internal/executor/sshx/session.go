package sshx

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"al.essio.dev/pkg/shellescape"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/internal/ports"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

type sshSession struct {
	pool   *Pool
	pc     *pooledClient
	target ports.Target
}

func (s *sshSession) Close() error {
	s.pool.release(s.pc)
	return nil
}

func (s *sshSession) workDir(cmd ports.Command) string {
	if cmd.WorkDir != "" {
		return cmd.WorkDir
	}
	if s.target.WorkDir != "" {
		return s.target.WorkDir
	}
	return "/var/lib/kinstall/tmp"
}

// Run executes a POSIX sh command either synchronously or via Async Detached (D1 §2.3-§2.4).
func (s *sshSession) Run(ctx context.Context, cmd ports.Command) (ports.Result, error) {
	if cmd.Async {
		return s.runAsyncDetached(ctx, cmd)
	}
	return s.runSync(ctx, cmd)
}

func (s *sshSession) runSync(ctx context.Context, cmd ports.Command) (ports.Result, error) {
	start := time.Now()
	if cmd.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cmd.Timeout)
		defer cancel()
	}
	if err := s.pc.sem.Acquire(ctx, 1); err != nil {
		return ports.Result{}, sdk.NewError(sdk.CodeExecSSHFailed, "EXEC", http.StatusBadGateway, sdk.CodeExecSSHFailed, true, err)
	}
	defer s.pc.sem.Release(1)

	sess, err := s.pc.client.NewSession()
	if err != nil {
		s.pool.evict(s.pc)
		return ports.Result{}, sdk.NewError(sdk.CodeExecSSHFailed, "EXEC", http.StatusBadGateway, sdk.CodeExecSSHFailed, true, err)
	}
	defer func() { _ = sess.Close() }()

	var stdoutBuf, stderrBuf bytes.Buffer
	if cmd.Stdout != nil {
		sess.Stdout = io.MultiWriter(&stdoutBuf, cmd.Stdout)
	} else {
		sess.Stdout = &stdoutBuf
	}
	if cmd.Stderr != nil {
		sess.Stderr = io.MultiWriter(&stderrBuf, cmd.Stderr)
	} else {
		sess.Stderr = &stderrBuf
	}

	wrappedScript, stdinReader := s.wrapCommand(cmd)
	if stdinReader != nil {
		sess.Stdin = stdinReader
	}

	if err := sess.Start(wrappedScript); err != nil {
		return ports.Result{}, sdk.NewError(sdk.CodeExecSSHFailed, "EXEC", http.StatusBadGateway, sdk.CodeExecSSHFailed, true, err)
	}

	doneCh := make(chan error, 1)
	go func() {
		doneCh <- sess.Wait()
	}()

	var runErr error
	select {
	case runErr = <-doneCh:
	case <-ctx.Done():
		_ = sess.Close()
		runErr = ctx.Err()
	}

	res := ports.Result{
		Stdout:   stdoutBuf.String(),
		Stderr:   stderrBuf.String(),
		Duration: time.Since(start),
	}

	if runErr != nil {
		var exitErr *ssh.ExitError
		if errors.As(runErr, &exitErr) {
			res.ExitCode = exitErr.ExitStatus()
			return res, classifyExecFailure(res.Stderr, res.ExitCode, cmd.Sudo)
		}
		if errors.Is(runErr, context.DeadlineExceeded) || errors.Is(runErr, context.Canceled) {
			return res, sdk.NewError(sdk.CodeEngCancelled, "ENG", http.StatusGatewayTimeout, sdk.CodeEngCancelled, false, runErr)
		}
		return res, sdk.NewError(sdk.CodeExecSSHFailed, "EXEC", http.StatusBadGateway, sdk.CodeExecSSHFailed, true, runErr)
	}
	return res, nil
}

// wrapCommand ensures POSIX sh + LC_ALL=C and sudo-rs#1668 compliant elevation (D1 §2.4).
// Never requests a PTY, never regex-matches localized PAM prompts, and never mixes sudo password with command stdin.
func (s *sshSession) wrapCommand(cmd ports.Command) (string, io.Reader) {
	base := fmt.Sprintf("LC_ALL=C PATH=\"/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:$PATH\" sh -c %s", shellescape.Quote(cmd.Script))
	if !cmd.Sudo || s.target.User == "root" || s.target.SudoMode == ports.SudoNone {
		return base, cmd.Stdin
	}
	if s.target.SudoMode == ports.SudoPassword && len(s.target.Auth.SudoPassword) > 0 && cmd.Stdin == nil {
		// sudo -S --prompt "" -- sh -c ... (compatible with both classic sudo and Ubuntu 26.04 sudo-rs)
		elevated := fmt.Sprintf("sudo -S --prompt '' -- %s", base)
		pwLine := string(s.target.Auth.SudoPassword) + "\n"
		return elevated, strings.NewReader(pwLine)
	}
	// Mode A default: NOPASSWD
	return "sudo -n -- " + base, cmd.Stdin
}

func classifyExecFailure(stderr string, exitCode int, isSudo bool) error {
	low := strings.ToLower(stderr)
	if isSudo && (strings.Contains(low, "sorry, try again") ||
		strings.Contains(low, "not in the sudoers") ||
		strings.Contains(low, "a password is required") ||
		strings.Contains(low, "a terminal is required") ||
		strings.Contains(low, "incorrect password")) {
		return sdk.NewError(sdk.CodeExecSudoFailed, "EXEC", http.StatusForbidden, sdk.CodeExecSudoFailed, false,
			fmt.Errorf("sudo failed (exit %d): %s", exitCode, strings.TrimSpace(stderr)))
	}
	if strings.Contains(low, "kysec") || strings.Contains(low, "fapolicyd") || strings.Contains(low, "operation not permitted") && strings.Contains(low, "noexec") {
		return sdk.NewError(sdk.CodeExecSecurityBlocked, "EXEC", http.StatusUnprocessableEntity, sdk.CodeExecSecurityBlocked, false,
			fmt.Errorf("security control blocked execution (exit %d): %s", exitCode, strings.TrimSpace(stderr)))
	}
	return fmt.Errorf("remote command failed (exit %d): %s", exitCode, strings.TrimSpace(stderr))
}

// runAsyncDetached launches a script under /var/lib/kinstall/tmp/runs/<id> with nohup setsid
// and polls incrementally so SSH drops cannot abort long tasks (D1 §2.3).
func (s *sshSession) runAsyncDetached(ctx context.Context, cmd ports.Command) (ports.Result, error) {
	start := time.Now()
	runID := cmd.TaskRunID
	if runID == "" {
		runID = domain.NewID(domain.PrefixTaskRun)
	}
	runDir := path.Join(s.workDir(cmd), "runs", runID)

	// 1. Create run directory and write script.sh (0700)
	mkCmd := fmt.Sprintf("mkdir -p %s && chmod 0700 %s", shellescape.Quote(runDir), shellescape.Quote(runDir))
	if _, err := s.runSync(ctx, ports.Command{Script: mkCmd, Sudo: cmd.Sudo}); err != nil {
		return ports.Result{}, err
	}
	scriptPath := path.Join(runDir, "script.sh")
	if _, err := s.Upload(ctx, strings.NewReader(cmd.Script), scriptPath, 0o700, ""); err != nil {
		return ports.Result{}, err
	}

	// 2. Launch detached process writing pid (PGID), stdout.log, stderr.log, and atomic rc.tmp -> rc
	launch := fmt.Sprintf(`RUN_DIR=%s
nohup setsid sh -c '
  echo $$ > "$1/pid"
  LC_ALL=C PATH="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:$PATH" sh "$1/script.sh" >"$1/stdout.log" 2>"$1/stderr.log"
  rc=$?
  printf "%%d\n" "$rc" > "$1/rc.tmp" && mv -f "$1/rc.tmp" "$1/rc"
' _ "$RUN_DIR" </dev/null >/dev/null 2>&1 &
sleep 0.1
cat "$RUN_DIR/pid" 2>/dev/null || echo 0`, shellescape.Quote(runDir))

	launchRes, err := s.runSync(ctx, ports.Command{Script: launch, Sudo: cmd.Sudo})
	if err != nil {
		return ports.Result{}, err
	}
	pgid, _ := strconv.Atoi(strings.TrimSpace(launchRes.Stdout))
	res, err := s.AttachAsync(ctx, runDir, 0, cmd.Sudo, cmd.Stdout, cmd.Stderr)
	res.Duration = time.Since(start)
	if res.RemotePGID == 0 {
		res.RemotePGID = pgid
	}
	return res, err
}

// AttachAsync attaches to an existing remote Async Detached run directory (used for crash recovery & reconnects, D1 §2.3 & D2 §2.2).
func (s *sshSession) AttachAsync(ctx context.Context, runDir string, startOffset int64, sudo bool, stdoutW, stderrW io.Writer) (ports.Result, error) {
	offset := startOffset
	var fullOut, fullErr bytes.Buffer
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		pollScript := fmt.Sprintf(`RUN_DIR=%s
OFF=%d
PGID="$(cat "$RUN_DIR/pid" 2>/dev/null || echo 0)"
ALIVE=0
if [ "$PGID" -gt 0 ] 2>/dev/null && kill -0 "$PGID" 2>/dev/null; then ALIVE=1; fi
RC=""
if [ -f "$RUN_DIR/rc" ]; then RC="$(cat "$RUN_DIR/rc")"; fi
SIZE="$(wc -c < "$RUN_DIR/stdout.log" 2>/dev/null | tr -d ' ' || echo 0)"
printf 'META:%%s:%%s:%%s:%%s\n' "$PGID" "$ALIVE" "$RC" "$SIZE"
if [ "$SIZE" -gt "$OFF" ] 2>/dev/null; then
  tail -c +$((OFF + 1)) "$RUN_DIR/stdout.log" 2>/dev/null || true
fi`, shellescape.Quote(runDir), offset)

		pr, err := s.runSync(ctx, ports.Command{Script: pollScript, Sudo: sudo})
		if err != nil {
			if ctx.Err() != nil {
				// Kill remote process group on cancellation (D1 §2.3)
				_ = s.killPGID(runDir, sudo)
				return ports.Result{RunDir: runDir, LogOffset: offset}, sdk.NewError(sdk.CodeEngCancelled, "ENG", http.StatusGatewayTimeout, sdk.CodeEngCancelled, false, ctx.Err())
			}
			// Transient SSH failure: wait and keep polling
			select {
			case <-ctx.Done():
				return ports.Result{RunDir: runDir, LogOffset: offset}, ctx.Err()
			case <-ticker.C:
				continue
			}
		}

		firstNL := strings.IndexByte(pr.Stdout, '\n')
		metaLine := pr.Stdout
		chunk := ""
		if firstNL >= 0 {
			metaLine = pr.Stdout[:firstNL]
			chunk = pr.Stdout[firstNL+1:]
		}
		if chunk != "" {
			fullOut.WriteString(chunk)
			if stdoutW != nil {
				_, _ = io.WriteString(stdoutW, chunk)
			}
		}

		parts := strings.Split(strings.TrimPrefix(metaLine, "META:"), ":")
		var pgid, alive int
		var rcStr string
		var newSize int64
		if len(parts) >= 4 {
			pgid, _ = strconv.Atoi(parts[0])
			alive, _ = strconv.Atoi(parts[1])
			rcStr = strings.TrimSpace(parts[2])
			newSize, _ = strconv.ParseInt(strings.TrimSpace(parts[3]), 10, 64)
			if newSize > offset {
				offset = newSize
			}
		}

		if rcStr != "" {
			// Read stderr log as well
			errRes, _ := s.runSync(ctx, ports.Command{Script: "cat " + shellescape.Quote(path.Join(runDir, "stderr.log")) + " 2>/dev/null || true", Sudo: sudo})
			if errRes.Stdout != "" {
				fullErr.WriteString(errRes.Stdout)
				if stderrW != nil {
					_, _ = io.WriteString(stderrW, errRes.Stdout)
				}
			}
			rc, _ := strconv.Atoi(rcStr)
			outRes := ports.Result{
				ExitCode:   rc,
				Stdout:     fullOut.String(),
				Stderr:     fullErr.String(),
				RunDir:     runDir,
				RemotePGID: pgid,
				LogOffset:  offset,
			}
			if rc != 0 {
				return outRes, classifyExecFailure(outRes.Stderr, rc, sudo)
			}
			return outRes, nil
		}

		if alive == 0 {
			// Re-check rc once to close race between process exit and atomic mv rc.tmp -> rc
			recheck, _ := s.runSync(ctx, ports.Command{Script: "cat " + shellescape.Quote(path.Join(runDir, "rc")) + " 2>/dev/null || true", Sudo: sudo})
			if trimmed := strings.TrimSpace(recheck.Stdout); trimmed != "" {
				rc, _ := strconv.Atoi(trimmed)
				outRes := ports.Result{ExitCode: rc, Stdout: fullOut.String(), Stderr: fullErr.String(), RunDir: runDir, RemotePGID: pgid, LogOffset: offset}
				if rc != 0 {
					return outRes, fmt.Errorf("async command failed (exit %d)", rc)
				}
				return outRes, nil
			}
			return ports.Result{RunDir: runDir, RemotePGID: pgid, LogOffset: offset},
				sdk.NewError(sdk.CodeEngAsyncOrphaned, "ENG", http.StatusInternalServerError, sdk.CodeEngAsyncOrphaned, true,
					fmt.Errorf("remote async process in %s vanished without rc", runDir))
		}

		select {
		case <-ctx.Done():
			_ = s.killPGID(runDir, sudo)
			return ports.Result{RunDir: runDir, RemotePGID: pgid, LogOffset: offset}, sdk.NewError(sdk.CodeEngCancelled, "ENG", http.StatusGatewayTimeout, sdk.CodeEngCancelled, false, ctx.Err())
		case <-ticker.C:
		}
	}
}

func (s *sshSession) killPGID(runDir string, sudo bool) error {
	bg, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	killCmd := fmt.Sprintf(`PGID="$(cat %s 2>/dev/null || echo 0)"
if [ "$PGID" -gt 0 ] 2>/dev/null; then
  kill -TERM -- "-$PGID" 2>/dev/null || true
  sleep 1
  kill -KILL -- "-$PGID" 2>/dev/null || true
fi`, shellescape.Quote(path.Join(runDir, "pid")))
	_, err := s.runSync(bg, ports.Command{Script: killCmd, Sudo: sudo})
	return err
}

// Upload implements atomic file replacement across Channel 1 (SFTP concurrent writes)
// and Channel 2 (SSH stdin streaming fallback when SFTP is disabled) (D1 §2.5).
// For non-root SudoPassword targets, stages through /tmp (user-writable) then atomically promotes
// into the destination directory under sudo -S to avoid permission denied and EXDEV cross-device races.
func (s *sshSession) Upload(ctx context.Context, r io.Reader, dst string, mode fs.FileMode, owner string) (string, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	localSHA := hex.EncodeToString(sum[:])

	dir := path.Dir(dst)
	needSudo := s.target.User != "root" && s.target.SudoMode != ports.SudoNone
	stageViaTmp := s.target.User != "root" && s.target.SudoMode == ports.SudoPassword
	tmpID := domain.NewID("f")
	tmpPath := path.Join(dir, ".kinstall.tmp."+tmpID)
	uploadPath := tmpPath
	if stageViaTmp {
		uploadPath = path.Join("/tmp", ".kinstall.stage."+tmpID)
	}

	// Ensure parent directory exists
	if _, err := s.runSync(ctx, ports.Command{Script: "mkdir -p " + shellescape.Quote(dir), Sudo: needSudo}); err != nil {
		return "", err
	}

	// Channel 1: Try SFTP with UseConcurrentWrites(true) + MaxPacket(64 KiB)
	usedSFTP := false
	if s.target.User == "root" || s.target.SudoMode == ports.SudoNone || stageViaTmp {
		if err := s.uploadViaSFTP(ctx, data, uploadPath); err == nil {
			usedSFTP = true
		}
	}

	// Channel 2: Automatic fallback to SSH stdin streaming if SFTP unavailable or sudo required
	if !usedSFTP {
		if err := s.uploadViaStdin(ctx, data, uploadPath, !stageViaTmp && s.target.SudoMode == ports.SudoNoPasswd); err != nil {
			return "", err
		}
	}

	// Verify remote SHA-256, apply mode + explicit user:group ownership (uutils safe!), fsync, atomic rename
	chownPart := ""
	if owner != "" {
		chownPart = fmt.Sprintf("chown %s %s && ", shellescape.Quote(owner), shellescape.Quote(tmpPath))
	}
	promotePart := ""
	if stageViaTmp {
		promotePart = fmt.Sprintf("cp -f %s %s && rm -f %s\n",
			shellescape.Quote(uploadPath), shellescape.Quote(tmpPath), shellescape.Quote(uploadPath))
	}
	finalize := fmt.Sprintf(`set -e
GOT="$(sha256sum %s | awk '{print $1}')"
if [ "$GOT" != %s ]; then rm -f %s; echo "sha256 mismatch: $GOT" >&2; exit 1; fi
%schmod %04o %s
%smv -f %s %s
if command -v restorecon >/dev/null 2>&1 && command -v getenforce >/dev/null 2>&1 && [ "$(getenforce 2>/dev/null)" = "Enforcing" ]; then
  restorecon %s 2>/dev/null || true
fi`,
		shellescape.Quote(uploadPath),
		shellescape.Quote(localSHA),
		shellescape.Quote(uploadPath),
		promotePart,
		uint32(mode.Perm()),
		shellescape.Quote(tmpPath),
		chownPart,
		shellescape.Quote(tmpPath),
		shellescape.Quote(dst),
		shellescape.Quote(dst),
	)
	if _, err := s.runSync(ctx, ports.Command{Script: finalize, Sudo: needSudo}); err != nil {
		if stageViaTmp {
			_, _ = s.runSync(ctx, ports.Command{Script: "rm -f " + shellescape.Quote(uploadPath), Sudo: false})
		}
		return "", err
	}
	return localSHA, nil
}

func (s *sshSession) uploadViaSFTP(ctx context.Context, data []byte, tmpPath string) error {
	if err := s.pc.sem.Acquire(ctx, 1); err != nil {
		return err
	}
	defer s.pc.sem.Release(1)

	sc, err := sftp.NewClient(s.pc.client, sftp.UseConcurrentWrites(true), sftp.MaxPacket(64*1024))
	if err != nil {
		return err
	}
	defer func() { _ = sc.Close() }()

	f, err := sc.Create(tmpPath)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, bytes.NewReader(data)); err != nil {
		_ = f.Close()
		_ = sc.Remove(tmpPath)
		return err
	}
	return f.Close()
}

func (s *sshSession) uploadViaStdin(ctx context.Context, data []byte, tmpPath string, useSudo bool) error {
	cmd := ports.Command{
		Script: "cat > " + shellescape.Quote(tmpPath),
		Stdin:  bytes.NewReader(data),
		Sudo:   useSudo,
	}
	_, err := s.runSync(ctx, cmd)
	return err
}

// Download fetches a remote file into w.
func (s *sshSession) Download(ctx context.Context, src string, w io.Writer) error {
	res, err := s.runSync(ctx, ports.Command{
		Script: "cat " + shellescape.Quote(src),
		Sudo:   s.target.SudoMode != ports.SudoNone,
	})
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, res.Stdout)
	return err
}

// Stat inspects remote file existence, type, size, mode, and SHA-256.
func (s *sshSession) Stat(ctx context.Context, filePath string) (*ports.RemoteFileInfo, error) {
	q := shellescape.Quote(filePath)
	script := fmt.Sprintf(`if [ ! -e %s ]; then echo "MISSING"; exit 0; fi
if [ -d %s ]; then echo "DIR"; exit 0; fi
SZ="$(wc -c < %s | tr -d ' ')"
SHA="$(sha256sum %s | awk '{print $1}')"
MODE="$(stat -c %%a %s 2>/dev/null || echo 644)"
printf 'FILE:%%s:%%s:%%s\n' "$SZ" "$MODE" "$SHA"`, q, q, q, q, q)

	res, err := s.runSync(ctx, ports.Command{Script: script, Sudo: s.target.SudoMode != ports.SudoNone})
	if err != nil {
		return nil, err
	}
	line := strings.TrimSpace(res.Stdout)
	if line == "MISSING" || line == "" {
		return &ports.RemoteFileInfo{Exists: false}, nil
	}
	if line == "DIR" {
		return &ports.RemoteFileInfo{Exists: true, IsDir: true}, nil
	}
	parts := strings.Split(strings.TrimPrefix(line, "FILE:"), ":")
	info := &ports.RemoteFileInfo{Exists: true}
	if len(parts) >= 3 {
		info.Size, _ = strconv.ParseInt(parts[0], 10, 64)
		m, _ := strconv.ParseUint(parts[1], 8, 32)
		info.Mode = fs.FileMode(m)
		info.SHA256 = parts[2]
	}
	return info, nil
}
