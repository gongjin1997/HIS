package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"al.essio.dev/pkg/shellescape"

	"github.com/kinstall/kinstall/internal/authz"
	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/internal/engine"
	"github.com/kinstall/kinstall/internal/executor/sshx"
	"github.com/kinstall/kinstall/internal/inventory"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// JobTemplate defines a reusable LiveOps-style daily operations / inspection / delivery script template.
type JobTemplate struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Category    string `json:"category"` // inspect | ops | app-deploy | security
	Description string `json:"description"`
	Sudo        bool   `json:"sudo"`
	TimeoutSec  int    `json:"timeoutSec"`
	Script      string `json:"script"`
}

// BatchJobRequest schedules a concurrent guarded batch script or package delivery job across CMDB hosts.
type BatchJobRequest struct {
	Name            string   `json:"name"`
	HostIDs         []string `json:"hostIds"`
	GroupID         string   `json:"groupId"`
	Script          string   `json:"script"`
	ArtifactPath    string   `json:"artifactPath,omitempty"` // optional artifact from offline catalog to stage first
	RemoteTargetDir string   `json:"remoteTargetDir,omitempty"`
	TimeoutSec      int      `json:"timeoutSec,omitempty"`
	Serial          bool     `json:"serial,omitempty"`
}

// ListJobTemplates returns curated production-safe daily operations templates.
func (f *Facade) ListJobTemplates() []JobTemplate {
	return []JobTemplate{
		{
			ID: "sys-inspect", Name: "系统资源与健康全面巡检", Category: "inspect",
			Description: "一键采集主机负载、内存水位、磁盘挂载使用率、时间同步与异常 Systemd 服务", Sudo: true, TimeoutSec: 60,
			Script: `echo "=== HOST: $(hostname) ($(uname -r)) ==="
uptime
echo "--- MEMORY ---"
free -h
echo "--- DISK USAGE ---"
df -hT -x tmpfs -x devtmpfs
echo "--- FAILED SYSTEMD UNITS ---"
systemctl --failed --no-pager || true
echo "--- TIME SYNC ---"
timedatectl status 2>/dev/null | head -n 8 || date`,
		},
		{
			ID: "port-process", Name: "监听端口与 Top 进程快照", Category: "inspect",
			Description: "盘点目标主机所有 TCP/UDP 监听端口及 CPU/内存占用最高的前 10 个进程", Sudo: true, TimeoutSec: 30,
			Script: `echo "=== LISTENING PORTS ==="
ss -lntup | head -n 60
echo "=== TOP 10 CPU PROCESSES ==="
ps -eo pid,ppid,user,%cpu,%mem,comm --sort=-%cpu | head -n 11
echo "=== TOP 10 MEMORY PROCESSES ==="
ps -eo pid,ppid,user,%cpu,%mem,comm --sort=-%mem | head -n 11`,
		},
		{
			ID: "disk-audit", Name: "磁盘大目录与日志膨胀排查", Category: "ops",
			Description: "定位 /var/log、/data、/opt、/var/lib/docker 等目录容量排行，辅助安全清理", Sudo: true, TimeoutSec: 60,
			Script: `echo "=== FILESYSTEM SUMMARY ==="
df -h -x tmpfs -x devtmpfs
echo "=== TOP DIRECTORIES UNDER /var/log & /data ==="
du -xh --max-depth=2 /var/log /data /opt/kinstall 2>/dev/null | sort -hr | head -n 25 || true
echo "=== JOURNALD DISK USAGE ==="
journalctl --disk-usage 2>/dev/null || true`,
		},
		{
			ID: "docker-health", Name: "Docker 容器与镜像空间巡检", Category: "inspect",
			Description: "核查宿主机 Docker 引擎版本、全部容器运行健康态及镜像/卷磁盘占用", Sudo: true, TimeoutSec: 30,
			Script: `if ! command -v docker >/dev/null 2>&1; then echo "Docker not installed on this host"; exit 0; fi
echo "=== DOCKER VERSION ==="
docker version --format 'Client={{.Client.Version}} Server={{.Server.Version}}'
echo "=== CONTAINERS ==="
docker ps -a --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}'
echo "=== DISK USAGE ==="
docker system df`,
		},
		{
			ID: "kinstall-services", Name: "已部署基础设施服务状态核查", Category: "inspect",
			Description: "批量检查本机全部 kinstall-* Systemd 单元与托管 Compose 容器健康状态", Sudo: true, TimeoutSec: 30,
			Script: `echo "=== KINSTALL SYSTEMD UNITS ==="
systemctl list-units 'kinstall-*' --all --no-pager || true
echo "=== KINSTALL COMPOSE STACKS ==="
for f in /opt/kinstall/compose/*/docker-compose.yml; do
  [ -f "$f" ] || continue
  echo "--- $f ---"
  (docker compose -f "$f" ps || docker-compose -f "$f" ps || true)
done`,
		},
		{
			ID: "app-package-deploy", Name: "业务应用包下发与发布脚本", Category: "app-deploy",
			Description: "搭配上方「下发离线制品包」将应用包推送至目标目录后自动解压、重启并做健康检查", Sudo: true, TimeoutSec: 180,
			Script: `APP_DIR="/opt/apps/release"
mkdir -p "$APP_DIR"
ls -lh "$APP_DIR"
echo "已就绪，请按业务应用启动命令自定义本脚本（如解压 tar.gz、systemctl restart his-app、curl 健康检查）"`,
		},
	}
}

// RunBatchJob validates script safety, resolves CMDB hosts or dynamic group, optionally stages an offline artifact,
// and queues a tracked Operation executed by Worker with real-time SSE output.
func (f *Facade) RunBatchJob(ctx context.Context, actor domain.Actor, projectID string, req BatchJobRequest) (*domain.Operation, error) {
	if err := authz.Authorize(actor, projectID, "host.write"); err != nil {
		return nil, err
	}
	script := strings.TrimSpace(req.Script)
	if script == "" {
		return nil, sdk.NewError(sdk.CodeAPIInvalidRequest, "JOB", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false,
			fmt.Errorf("job script cannot be empty"))
	}
	// Enforce LiveOps-grade ScriptRiskCheckService guard BEFORE queuing!
	if err := engine.ValidateCommandSafety(script); err != nil {
		f.recordAudit(ctx, actor, projectID, "job.blocked", "job", req.Name, "denied", err.Error(), nil)
		return nil, err
	}

	// Resolve target hosts (explicit IDs or CMDB Dynamic Group)
	targetHostIDs := append([]string(nil), req.HostIDs...)
	if len(targetHostIDs) == 0 && strings.TrimSpace(req.GroupID) != "" {
		grp, err := f.store.CMDB().GetDynamicGroup(ctx, req.GroupID)
		if err != nil {
			return nil, err
		}
		allHosts, err := f.store.Hosts().ListByProject(ctx, projectID)
		if err != nil {
			return nil, err
		}
		for _, h := range allHosts {
			if inventory.MatchDynamicGroup(h, grp.Rules) {
				targetHostIDs = append(targetHostIDs, h.ID)
			}
		}
	}
	if len(targetHostIDs) == 0 {
		return nil, sdk.NewError(sdk.CodeAPIInvalidRequest, "JOB", http.StatusUnprocessableEntity, sdk.CodeAPIInvalidRequest, false,
			fmt.Errorf("no target hosts selected for batch job"))
	}

	timeout := time.Duration(req.TimeoutSec) * time.Second
	if timeout <= 0 || timeout > 30*time.Minute {
		timeout = 5 * time.Minute
	}

	var tasks []sdk.TaskSpec
	// Optional: stage an offline artifact from catalog onto target hosts before running script!
	if artPath := strings.TrimSpace(req.ArtifactPath); artPath != "" {
		policyRoots, _ := f.projectLocalRoots(ctx, projectID)
		absPath, err := f.ensureArtifactStore().ResolvePath(artPath, policyRoots...)
		if err != nil {
			return nil, sdk.NewError(sdk.CodeSrcNotFound, "JOB", http.StatusNotFound, sdk.CodeSrcNotFound, false, err)
		}
		rawBytes, rerr := os.ReadFile(absPath) // #nosec G304 -- resolved strictly via catalog.Store.ResolvePath
		if rerr != nil {
			return nil, rerr
		}
		if len(rawBytes) > 32<<20 {
			return nil, sdk.NewError(sdk.CodeAPIInvalidRequest, "JOB", http.StatusRequestEntityTooLarge, sdk.CodeAPIInvalidRequest, false,
				fmt.Errorf("inline batch staging supports artifacts up to 32 MiB; use BinaryInstall or ImageLoad for larger bundles"))
		}
		remoteDir := strings.TrimSpace(req.RemoteTargetDir)
		if remoteDir == "" {
			remoteDir = "/opt/apps/release"
		}
		remoteDst := path.Join(remoteDir, filepath.Base(absPath))
		if err := engine.ValidateTargetPath(remoteDst); err != nil {
			return nil, err
		}
		tasks = append(tasks, sdk.FileSync(remoteDst, sdk.TemplateRef{Content: rawBytes}, 0o644, "").WithKey("stage-artifact"))
	}

	cmdTask := sdk.CmdRun(script, "").WithKey("batch-script")
	cmdTask.Timeout = timeout
	tasks = append(tasks, cmdTask)
	jobName := strings.TrimSpace(req.Name)
	if jobName == "" {
		jobName = "batch-job-" + time.Now().Format("150405")
	}

	pb := sdk.NewPipelineBuilder()
	pb.Phase(sdk.PhaseSpec{
		Name:   jobName,
		Hosts:  sdk.HostSelector{HostIDs: targetHostIDs},
		Serial: req.Serial,
		Tasks:  tasks,
	})
	pipeJSON, _ := json.Marshal(pb.Build())

	op := &domain.Operation{
		ID:           domain.NewID(domain.PrefixOperation),
		ProjectID:    projectID,
		TargetType:   "job",
		TargetID:     jobName,
		Action:       "exec",
		State:        domain.OpPending,
		RequestedBy:  actor.ID,
		PipelineJSON: pipeJSON,
	}
	if err := f.store.Operations().Create(ctx, op); err != nil {
		return nil, err
	}
	f.recordAudit(ctx, actor, projectID, "job.run", "job", op.ID, "success", "", map[string]any{
		"name": jobName, "hosts": len(targetHostIDs), "artifact": req.ArtifactPath,
	})
	return op, nil
}

// OpenHostPTY authorizes host.write, opens a real interactive SSH PTY session via sshPool, and records audit.
func (f *Facade) OpenHostPTY(ctx context.Context, actor domain.Actor, projectID, hostID string, cols, rows int) (*sshx.InteractivePTY, *domain.Host, error) {
	if err := authz.Authorize(actor, projectID, "host.write"); err != nil {
		return nil, nil, err
	}
	if f.sshPool == nil {
		return nil, nil, fmt.Errorf("SSH pool unavailable for interactive terminal")
	}
	h, err := f.store.Hosts().Get(ctx, hostID)
	if err != nil {
		return nil, nil, err
	}
	target, err := BuildHostTarget(ctx, f.store, f.secrets, h)
	if err != nil {
		return nil, nil, err
	}
	pty, err := f.sshPool.StartPTY(ctx, target, cols, rows)
	ZeroTargetAuth(&target)
	if err != nil {
		f.recordAudit(ctx, actor, projectID, "host.terminal.open", "host", h.ID, "failure", err.Error(), map[string]any{"host": h.Name})
		return nil, nil, err
	}
	f.recordAudit(ctx, actor, projectID, "host.terminal.open", "host", h.ID, "success", "", map[string]any{"host": h.Name, "address": h.Address})
	return pty, h, nil
}

// RecordTerminalClosed audits closure of an interactive Web SSH PTY session.
func (f *Facade) RecordTerminalClosed(ctx context.Context, actor domain.Actor, projectID, hostID, hostName string, duration time.Duration) {
	f.recordAudit(ctx, actor, projectID, "host.terminal.close", "host", hostID, "success", "", map[string]any{
		"host": hostName, "durationSec": int(duration.Seconds()),
	})
}

// keep shellescape referenced if future package commands use it
var _ = shellescape.Quote
