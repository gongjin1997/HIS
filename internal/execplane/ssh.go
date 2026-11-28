package execplane

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"al.essio.dev/pkg/shellescape"
	"github.com/kinstall/kinstall/internal/cmdb"
	"github.com/kinstall/kinstall/pkg/platform/id"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/net/websocket"
)

// SSHTarget holds resolved runtime connection credentials (never persisted in plaintext).
type SSHTarget struct {
	Host       string
	Port       int
	User       string
	Password   string
	PrivateKey string
	Passphrase string
}

// CollectLinuxFactsScript is a strict whitelist read-only collector safe for production hosts.
const CollectLinuxFactsScript = `
set -e
OS_ID=$(. /etc/os-release 2>/dev/null && echo "${ID:-linux}" || echo "linux")
OS_VER=$(. /etc/os-release 2>/dev/null && echo "${VERSION_ID:-unknown}" || uname -r)
KERNEL=$(uname -r)
ARCH_RAW=$(uname -m)
case "$ARCH_RAW" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) ARCH="$ARCH_RAW" ;;
esac
CPU_CORES=$(nproc 2>/dev/null || grep -c ^processor /proc/cpuinfo 2>/dev/null || echo 1)
MEM_MB=$(awk '/MemTotal/ {printf "%d", $2/1024}' /proc/meminfo 2>/dev/null || echo 0)
MEM_AVAIL=$(awk '/MemAvailable/ {printf "%d", $2/1024}' /proc/meminfo 2>/dev/null || echo 0)
DISK_GB=$(df -BG / 2>/dev/null | awk 'NR==2 {gsub("G","",$2); print $2}' || echo 0)
LOAD_AVG=$(awk '{print $1" "$2" "$3}' /proc/loadavg 2>/dev/null || echo "0.00 0.00 0.00")
PORTS=$(ss -tlnH 2>/dev/null | awk '{print $4}' | awk -F: '{print $NF}' | sort -nu | tr '\n' ',' | sed 's/,$//' || true)
DOCKER_CNT=$(docker ps -q 2>/dev/null | wc -l | tr -d ' ' || echo 0)
echo "OS_ID=$OS_ID"
echo "OS_VERSION=$OS_VER"
echo "KERNEL=$KERNEL"
echo "ARCH=$ARCH"
echo "CPU_CORES=$CPU_CORES"
echo "MEM_MB=$MEM_MB"
echo "MEM_AVAIL_MB=$MEM_AVAIL"
echo "DISK_GB=$DISK_GB"
echo "LOAD_AVG=$LOAD_AVG"
echo "LISTEN_PORTS=$PORTS"
echo "DOCKER_CONTAINERS=$DOCKER_CNT"
`

func dialSSH(ctx context.Context, t SSHTarget) (*ssh.Client, error) {
	if strings.TrimSpace(t.Host) == "" {
		return nil, errors.New("SSH 主机地址不能为空")
	}
	port := t.Port
	if port <= 0 {
		port = 22
	}
	user := strings.TrimSpace(t.User)
	if user == "" {
		user = "root"
	}
	authMethods := make([]ssh.AuthMethod, 0, 2)
	if strings.TrimSpace(t.PrivateKey) != "" {
		var (
			signer ssh.Signer
			err    error
		)
		if t.Passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(t.PrivateKey), []byte(t.Passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey([]byte(t.PrivateKey))
		}
		if err != nil {
			return nil, fmt.Errorf("解析 SSH 私钥失败: %w", err)
		}
		authMethods = append(authMethods, ssh.PublicKeys(signer))
	}
	if t.Password != "" {
		authMethods = append(authMethods, ssh.Password(t.Password))
	}
	if len(authMethods) == 0 {
		return nil, errors.New("未配置 SSH 密码或私钥凭据")
	}

	cfg := &ssh.ClientConfig{
		User:            user,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // #nosec G106 -- enterprise CMDB dynamic inventory hosts
		Timeout:         15 * time.Second,
	}
	addr := net.JoinHostPort(t.Host, strconv.Itoa(port))
	var d net.Dialer
	d.Timeout = 12 * time.Second
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("连接 SSH %s 失败: %w", addr, err)
	}
	c, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("SSH 握手认证失败 (%s@%s): %w", user, addr, err)
	}
	return ssh.NewClient(c, chans, reqs), nil
}

// RunSSHCommand executes command over SSH and streams stdout/stderr lines in real time.
func RunSSHCommand(ctx context.Context, target SSHTarget, command string, onLine func(stream, line string)) (int, string, error) {
	client, err := dialSSH(ctx, target)
	if err != nil {
		return -1, "", err
	}
	defer func() { _ = client.Close() }()

	sess, err := client.NewSession()
	if err != nil {
		return -1, "", err
	}
	defer func() { _ = sess.Close() }()

	stdout, err := sess.StdoutPipe()
	if err != nil {
		return -1, "", err
	}
	stderr, err := sess.StderrPipe()
	if err != nil {
		return -1, "", err
	}

	if err := sess.Start(command); err != nil {
		return -1, "", err
	}

	var (
		outMu sync.Mutex
		buf   strings.Builder
		wg    sync.WaitGroup
	)
	readPipe := func(stream string, r io.Reader) {
		defer wg.Done()
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			line := sc.Text()
			outMu.Lock()
			buf.WriteString(line)
			buf.WriteByte('\n')
			outMu.Unlock()
			if onLine != nil {
				onLine(stream, line)
			}
		}
	}
	wg.Add(2)
	go readPipe("stdout", stdout)
	go readPipe("stderr", stderr)

	done := make(chan error, 1)
	go func() {
		wg.Wait()
		done <- sess.Wait()
	}()

	var waitErr error
	select {
	case <-ctx.Done():
		_ = sess.Signal(ssh.SIGKILL)
		_ = sess.Close()
		return -1, buf.String(), ctx.Err()
	case waitErr = <-done:
	}

	if waitErr != nil {
		var exitErr *ssh.ExitError
		if errors.As(waitErr, &exitErr) {
			return exitErr.ExitStatus(), buf.String(), nil
		}
		return -1, buf.String(), waitErr
	}
	return 0, buf.String(), nil
}

// UploadFileSFTP atomically uploads a local CAS artifact to remoteDest via SFTP + atomic rename.
func UploadFileSFTP(ctx context.Context, target SSHTarget, localPath, remoteDest string) error {
	client, err := dialSSH(ctx, target)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	sc, err := sftp.NewClient(client)
	if err != nil {
		return fmt.Errorf("打开 SFTP 子系统失败: %w", err)
	}
	defer func() { _ = sc.Close() }()

	src, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()

	stagePath := "/tmp/kinstall-stage-" + id.Raw()
	dst, err := sc.Create(stagePath)
	if err != nil {
		return fmt.Errorf("创建远端暂存文件失败: %w", err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		return err
	}
	if err := dst.Close(); err != nil {
		return err
	}

	// Atomically move into destination creating parent directory if needed.
	mvCmd := fmt.Sprintf("mkdir -p $(dirname %s) && mv -f %s %s",
		shellescape.Quote(remoteDest),
		shellescape.Quote(stagePath),
		shellescape.Quote(remoteDest),
	)
	sess, err := client.NewSession()
	if err != nil {
		return err
	}
	defer func() { _ = sess.Close() }()
	if out, err := sess.CombinedOutput(mvCmd); err != nil {
		return fmt.Errorf("原子落盘 %s 失败: %w (%s)", remoteDest, err, strings.TrimSpace(string(out)))
	}
	return nil
}

type wsPTYMsg struct {
	Type string `json:"type"` // input | resize | ping
	Data string `json:"data"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

// ServeWebSocketPTY bridges a browser xterm.js WebSocket directly to an interactive SSH PTY session.
func ServeWebSocketPTY(ws *websocket.Conn, target SSHTarget) {
	defer func() { _ = ws.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	client, err := dialSSH(ctx, target)
	cancel()
	if err != nil {
		_ = websocket.Message.Send(ws, fmt.Sprintf("\r\n\x1b[31m[Kinstall SSH] 连接失败: %v\x1b[0m\r\n", err))
		return
	}
	defer func() { _ = client.Close() }()

	sess, err := client.NewSession()
	if err != nil {
		_ = websocket.Message.Send(ws, fmt.Sprintf("\r\n\x1b[31m[Kinstall SSH] 创建会话失败: %v\x1b[0m\r\n", err))
		return
	}
	defer func() { _ = sess.Close() }()

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := sess.RequestPty("xterm-256color", 32, 110, modes); err != nil {
		_ = websocket.Message.Send(ws, fmt.Sprintf("\r\n\x1b[31m[Kinstall SSH] 申请 PTY 失败: %v\x1b[0m\r\n", err))
		return
	}

	stdin, err := sess.StdinPipe()
	if err != nil {
		return
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		return
	}
	stderr, err := sess.StderrPipe()
	if err != nil {
		return
	}

	if err := sess.Shell(); err != nil {
		_ = websocket.Message.Send(ws, fmt.Sprintf("\r\n\x1b[31m[Kinstall SSH] 启动 Shell 失败: %v\x1b[0m\r\n", err))
		return
	}

	var writeMu sync.Mutex
	sendOut := func(r io.Reader) {
		buf := make([]byte, 8192)
		for {
			n, rerr := r.Read(buf)
			if n > 0 {
				writeMu.Lock()
				_ = websocket.Message.Send(ws, string(buf[:n]))
				writeMu.Unlock()
			}
			if rerr != nil {
				return
			}
		}
	}
	go sendOut(stdout)
	go sendOut(stderr)

	go func() {
		for {
			var raw string
			if err := websocket.Message.Receive(ws, &raw); err != nil {
				_ = sess.Close()
				return
			}
			var msg wsPTYMsg
			if json.Unmarshal([]byte(raw), &msg) == nil && msg.Type != "" {
				switch msg.Type {
				case "resize":
					if msg.Cols > 0 && msg.Rows > 0 {
						_ = sess.WindowChange(msg.Rows, msg.Cols)
					}
				case "input":
					_, _ = io.WriteString(stdin, msg.Data)
				}
			} else {
				_, _ = io.WriteString(stdin, raw)
			}
		}
	}()

	_ = sess.Wait()
}

// ParseFactsOutput extracts structured telemetry from CollectLinuxFactsScript output.
func ParseFactsOutput(raw string) cmdb.HostEndpoint {
	ep := cmdb.HostEndpoint{
		Facts: make(map[string]any),
	}
	var memAvail int
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		switch k {
		case "OS_ID":
			ep.OSFamily = v
		case "OS_VERSION":
			ep.OSVersion = v
		case "KERNEL":
			ep.Kernel = v
		case "ARCH":
			ep.Arch = v
		case "CPU_CORES":
			ep.CPUCores, _ = strconv.Atoi(v)
		case "MEM_MB":
			ep.MemMB, _ = strconv.Atoi(v)
		case "MEM_AVAIL_MB":
			memAvail, _ = strconv.Atoi(v)
		case "DISK_GB":
			ep.DiskGB, _ = strconv.Atoi(v)
		case "LOAD_AVG":
			ep.LoadAvg = v
		case "LISTEN_PORTS":
			ep.Facts["listenPorts"] = v
		case "DOCKER_CONTAINERS":
			ep.Facts["dockerContainers"] = v
		}
	}
	if ep.MemMB > 0 && memAvail > 0 && memAvail <= ep.MemMB {
		ep.MemUsagePct = float64(ep.MemMB-memAvail) * 100.0 / float64(ep.MemMB)
	}
	ep.Facts["collectedAt"] = time.Now().UTC().Format(time.RFC3339)
	return ep
}
