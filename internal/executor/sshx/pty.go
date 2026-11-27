package sshx

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"golang.org/x/crypto/ssh"

	"github.com/kinstall/kinstall/internal/ports"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// InteractivePTY represents a live SSH PTY terminal session backed by the shared SSH Pool.
type InteractivePTY struct {
	pool    *Pool
	pc      *pooledClient
	session *ssh.Session
	stdin   io.WriteCloser
	stdout  io.Reader
}

// StartPTY opens a real interactive xterm-256color PTY shell on target via the SSH pool.
func (p *Pool) StartPTY(ctx context.Context, target ports.Target, cols, rows int) (*InteractivePTY, error) {
	if cols <= 0 {
		cols = 120
	}
	if rows <= 0 {
		rows = 32
	}
	pc, err := p.acquire(ctx, target)
	if err != nil {
		return nil, err
	}
	if err := pc.sem.Acquire(ctx, 1); err != nil {
		p.release(pc)
		return nil, sdk.NewError(sdk.CodeExecSSHFailed, "EXEC", http.StatusBadGateway, sdk.CodeExecSSHFailed, true, err)
	}
	sess, err := pc.client.NewSession()
	if err != nil {
		pc.sem.Release(1)
		p.evict(pc)
		p.release(pc)
		return nil, sdk.NewError(sdk.CodeExecSSHFailed, "EXEC", http.StatusBadGateway, sdk.CodeExecSSHFailed, true, err)
	}

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := sess.RequestPty("xterm-256color", rows, cols, modes); err != nil {
		_ = sess.Close()
		pc.sem.Release(1)
		p.release(pc)
		return nil, fmt.Errorf("request pty: %w", err)
	}
	stdin, err := sess.StdinPipe()
	if err != nil {
		_ = sess.Close()
		pc.sem.Release(1)
		p.release(pc)
		return nil, err
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		_ = sess.Close()
		pc.sem.Release(1)
		p.release(pc)
		return nil, err
	}
	sess.Stderr = nil // PTY merges stderr into stdout
	if err := sess.Shell(); err != nil {
		_ = sess.Close()
		pc.sem.Release(1)
		p.release(pc)
		return nil, fmt.Errorf("start shell: %w", err)
	}
	return &InteractivePTY{
		pool:    p,
		pc:      pc,
		session: sess,
		stdin:   stdin,
		stdout:  stdout,
	}, nil
}

func (t *InteractivePTY) Write(p []byte) (int, error) { return t.stdin.Write(p) }
func (t *InteractivePTY) Read(p []byte) (int, error)  { return t.stdout.Read(p) }

func (t *InteractivePTY) Resize(cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return nil
	}
	return t.session.WindowChange(rows, cols)
}

func (t *InteractivePTY) Wait() error {
	return t.session.Wait()
}

func (t *InteractivePTY) Close() error {
	if t.stdin != nil {
		_ = t.stdin.Close()
	}
	err := t.session.Close()
	t.pc.sem.Release(1)
	t.pool.release(t.pc)
	return err
}
