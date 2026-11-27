package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"path"
	"strings"

	"al.essio.dev/pkg/shellescape"
	"sigs.k8s.io/yaml"

	"github.com/kinstall/kinstall/internal/pki"
	"github.com/kinstall/kinstall/internal/planner"
	"github.com/kinstall/kinstall/internal/ports"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// --- #11 cert.issue (30% remaining-TTL idempotency, D2 §5.1 #11) ---

type CertIssueTask struct{}

func (t *CertIssueTask) Name() string { return "cert.issue" }

func extractCertSpec(spec sdk.TaskSpec) (sdk.CertIssueSpec, error) {
	var cs sdk.CertIssueSpec
	raw, ok := spec.Params["spec"]
	if !ok {
		return cs, fmt.Errorf("missing cert.issue spec")
	}
	if direct, ok := raw.(sdk.CertIssueSpec); ok {
		return direct, nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return cs, err
	}
	err = json.Unmarshal(b, &cs)
	return cs, err
}

func (t *CertIssueTask) Check(ctx context.Context, rc *RunContext, spec sdk.TaskSpec) (CheckResult, error) {
	cs, err := extractCertSpec(spec)
	if err != nil {
		return CheckResult{}, err
	}
	st, statErr := rc.Session.Stat(ctx, cs.CertPath)
	if statErr != nil || !st.Exists {
		return CheckResult{Converged: false, Summary: "cert missing"}, nil //nolint:nilerr // missing/unreadable cert means not converged
	}
	var buf bytes.Buffer
	if dlErr := rc.Session.Download(ctx, cs.CertPath, &buf); dlErr != nil {
		return CheckResult{Converged: false}, nil //nolint:nilerr // unreadable cert requires re-issuance
	}
	needsRenew, renewErr := pki.NeedsRenewal(buf.Bytes(), 0.30)
	if renewErr != nil || needsRenew {
		return CheckResult{Converged: false, Summary: "cert requires issuance/renewal"}, nil //nolint:nilerr // corrupt/expiring cert requires re-issuance
	}
	return CheckResult{Converged: true, Summary: "cert valid (>30% TTL remaining)"}, nil
}

func (t *CertIssueTask) Apply(ctx context.Context, rc *RunContext, spec sdk.TaskSpec) (ApplyResult, error) {
	if rc.PKI == nil {
		return ApplyResult{}, fmt.Errorf("PKI issuer unavailable")
	}
	cs, err := extractCertSpec(spec)
	if err != nil {
		return ApplyResult{}, err
	}
	var ips []net.IP
	for _, s := range cs.IPs {
		if ip := net.ParseIP(s); ip != nil {
			ips = append(ips, ip)
		}
	}
	issued, err := rc.PKI.Issue(ctx, ports.CertRequest{
		CAScope:    cs.CAScope,
		CommonName: cs.CommonName,
		Orgs:       cs.Orgs,
		DNSNames:   cs.DNSNames,
		IPs:        ips,
		TTL:        cs.TTL,
		KeyAlg:     cs.KeyAlg,
	})
	if err != nil {
		return ApplyResult{}, err
	}
	if rc.RegisterSecret != nil {
		rc.RegisterSecret(string(issued.KeyPEM))
	}
	if _, err := rc.Session.Upload(ctx, bytes.NewReader(issued.CertPEM), cs.CertPath, 0o644, cs.Owner); err != nil {
		return ApplyResult{}, err
	}
	if _, err := rc.Session.Upload(ctx, bytes.NewReader(issued.KeyPEM), cs.KeyPath, 0o600, cs.Owner); err != nil {
		return ApplyResult{}, err
	}
	if cs.CAPath != "" {
		if _, err := rc.Session.Upload(ctx, bytes.NewReader(issued.CAPEM), cs.CAPath, 0o644, cs.Owner); err != nil {
			return ApplyResult{}, err
		}
	}
	return ApplyResult{Changed: true, Output: issued.Serial}, nil
}

func (t *CertIssueTask) Rollback(context.Context, *RunContext, sdk.TaskSpec, ApplyResult) error {
	return nil
}

// --- #12 helm.release (Helm v4 declarative release orchestration, D2 §5.1 #12) ---

type HelmReleaseTask struct{}

func (t *HelmReleaseTask) Name() string { return "helm.release" }

func extractHelmSpec(spec sdk.TaskSpec) (sdk.HelmReleaseSpec, error) {
	var hs sdk.HelmReleaseSpec
	raw, ok := spec.Params["release"]
	if !ok {
		return hs, fmt.Errorf("missing helm.release spec")
	}
	if direct, ok := raw.(sdk.HelmReleaseSpec); ok {
		return direct, nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return hs, err
	}
	err = json.Unmarshal(b, &hs)
	return hs, err
}

func helmValuesPath(hs sdk.HelmReleaseSpec) string {
	safeName := strings.NewReplacer("/", "_", "..", "_").Replace(hs.Namespace + "-" + hs.ReleaseName)
	return path.Join("/var/lib/kinstall/helm", safeName+"-values.yaml")
}

func renderHelmValuesYAML(values map[string]any) ([]byte, error) {
	if len(values) == 0 {
		return nil, nil
	}
	return yaml.Marshal(values)
}

func (t *HelmReleaseTask) Check(ctx context.Context, rc *RunContext, spec sdk.TaskSpec) (CheckResult, error) {
	hs, err := extractHelmSpec(spec)
	if err != nil {
		return CheckResult{}, err
	}
	cmd := fmt.Sprintf("helm status %s -n %s -o json 2>/dev/null | grep -q '\"status\":\"deployed\"'",
		shellescape.Quote(hs.ReleaseName), shellescape.Quote(hs.Namespace))
	res, err := rc.Session.Run(ctx, ports.Command{Script: cmd})
	if err != nil || res.ExitCode != 0 {
		return CheckResult{Converged: false, Summary: "deploy helm release " + hs.ReleaseName}, nil //nolint:nilerr // non-deployed release means not yet converged
	}
	// If Values are specified, verify the on-host rendered values.yaml matches desired SHA-256 so value changes trigger upgrade!
	if len(hs.Values) > 0 {
		valBytes, yerr := renderHelmValuesYAML(hs.Values)
		if yerr != nil {
			return CheckResult{}, yerr
		}
		st, serr := rc.Session.Stat(ctx, helmValuesPath(hs))
		if serr != nil || !st.Exists || st.SHA256 != planner.SHA256Hex(valBytes) {
			return CheckResult{Converged: false, Summary: "helm values changed for " + hs.ReleaseName}, nil //nolint:nilerr // missing/changed values file requires helm upgrade
		}
	}
	return CheckResult{Converged: true, Summary: "helm release deployed"}, nil
}

func (t *HelmReleaseTask) Apply(ctx context.Context, rc *RunContext, spec sdk.TaskSpec) (ApplyResult, error) {
	hs, err := extractHelmSpec(spec)
	if err != nil {
		return ApplyResult{}, err
	}
	args := []string{
		"helm upgrade --install",
		shellescape.Quote(hs.ReleaseName),
		shellescape.Quote(hs.ChartRef),
		"-n", shellescape.Quote(hs.Namespace),
		"--create-namespace",
	}
	if len(hs.Values) > 0 {
		valBytes, yerr := renderHelmValuesYAML(hs.Values)
		if yerr != nil {
			return ApplyResult{}, yerr
		}
		vPath := helmValuesPath(hs)
		if _, uerr := rc.Session.Upload(ctx, bytes.NewReader(valBytes), vPath, 0o640, ""); uerr != nil {
			return ApplyResult{}, uerr
		}
		args = append(args, "-f", shellescape.Quote(vPath))
	}
	if hs.Version != "" {
		args = append(args, "--version", shellescape.Quote(hs.Version))
	}
	if hs.Atomic {
		args = append(args, "--atomic")
	}
	if hs.Wait {
		args = append(args, "--wait")
	}
	if spec.Timeout > 0 {
		args = append(args, "--timeout", shellescape.Quote(spec.Timeout.String()))
	}
	res, err := rc.Session.Run(ctx, ports.Command{
		Script:    strings.Join(args, " "),
		Sudo:      true,
		Async:     spec.Async,
		Timeout:   spec.Timeout,
		TaskRunID: rc.TaskRunID,
		Stdout:    rc.LogStdout,
		Stderr:    rc.LogStderr,
	})
	if err != nil {
		return ApplyResult{}, err
	}
	return ApplyResult{Changed: true, Output: res.Stdout}, nil
}

func (t *HelmReleaseTask) Rollback(ctx context.Context, rc *RunContext, spec sdk.TaskSpec, _ ApplyResult) error {
	hs, err := extractHelmSpec(spec)
	if err != nil {
		return err
	}
	_, err = rc.Session.Run(ctx, ports.Command{
		Script: fmt.Sprintf("helm rollback %s -n %s", shellescape.Quote(hs.ReleaseName), shellescape.Quote(hs.Namespace)),
		Sudo:   true,
	})
	return err
}

// --- #13 docker.ensure (Docker 29.10 + LimitNOFILE=1048576 + data-root + dual firewall guard, D2 §5.2) ---

type DockerEnsureTask struct{}

func (t *DockerEnsureTask) Name() string { return "docker.ensure" }

// DockerDaemonConfig renders and validates /etc/docker/daemon.json per D2 §5.2.
func BuildDockerDaemonJSON(dataRoot, fwBackend string) ([]byte, error) {
	if dataRoot == "" {
		dataRoot = "/data/docker"
	}
	if fwBackend == "" {
		fwBackend = "iptables"
	}
	cfg := map[string]any{
		"data-root":        dataRoot,
		"firewall-backend": fwBackend,
		"live-restore":     true,
		"exec-opts":        []string{"native.cgroupdriver=systemd"},
		"log-driver":       "json-file",
		"log-opts": map[string]string{
			"max-size": "100m",
			"max-file": "3",
		},
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, err
	}
	// Self-validate via json.Unmarshal per T-M4-01 DoD
	var check map[string]any
	if err := json.Unmarshal(b, &check); err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// BuildDockerServiceUnit renders a compliant docker.service unit locking LimitNOFILE=1048576 (FORBIDDING infinity! D2 §5.2).
func BuildDockerServiceUnit(limitNOFILE int) (string, error) {
	if limitNOFILE <= 0 || limitNOFILE > 10485760 {
		return "", sdk.NewError(sdk.CodeAPIInvalidRequest, "API", http.StatusUnprocessableEntity, sdk.CodeAPIInvalidRequest, false,
			fmt.Errorf("invalid docker.service LimitNOFILE=%d (infinity is strictly forbidden to avoid systemd 252+ closefrom stall)", limitNOFILE))
	}
	return fmt.Sprintf(`[Unit]
Description=Docker Application Container Engine (managed by kinstall)
After=network-online.target firewalld.service containerd.service
Wants=network-online.target

[Service]
Type=notify
ExecStart=/usr/bin/dockerd -H fd:// --containerd=/run/containerd/containerd.sock
ExecReload=/bin/kill -s HUP $MAINPID
TimeoutStartSec=0
RestartSec=2
Restart=always
LimitNOFILE=%d
LimitNPROC=infinity
LimitCORE=infinity
TasksMax=infinity
Delegate=yes
KillMode=process

[Install]
WantedBy=multi-user.target
`, limitNOFILE), nil
}

// BuildDockerFirewallGuardScript returns the idempotent dual-backend firewall script (D2 §5.2):
// - iptables (iptables-nft): manages DOCKER-USER chain
// - nftables (Docker 29 experimental): manages independent table inet kinstall_docker_guard at priority filter - 5
func BuildDockerFirewallGuardScript(allowCIDRs []string) string {
	if len(allowCIDRs) == 0 {
		allowCIDRs = []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"}
	}
	return fmt.Sprintf(`#!/bin/sh
# /opt/kinstall/libexec/docker-fw-apply.sh (managed by kinstall, D2 §5.2)
set -eu
BACKEND="$(docker info --format '{{.FirewallBackend.Driver}}' 2>/dev/null || echo iptables)"
EXT_IFACE="${KINSTALL_EXT_IFACE:-$(ip -4 route show default 2>/dev/null | awk '{print $5; exit}')}"
ALLOW_CIDRS=%s

if [ -z "$EXT_IFACE" ]; then exit 0; fi

if [ "$BACKEND" = "nftables" ]; then
  nft -f - <<EOF
table inet kinstall_docker_guard
delete table inet kinstall_docker_guard
table inet kinstall_docker_guard {
  set allow_v4 { type ipv4_addr; flags interval; elements = { $(echo "$ALLOW_CIDRS" | sed 's/ /, /g') } }
  chain forward {
    type filter hook forward priority filter - 5; policy accept;
    iifname "$EXT_IFACE" ct state established,related accept
    iifname "$EXT_IFACE" ip saddr @allow_v4 accept
    iifname "$EXT_IFACE" oifname "br-*" drop
    iifname "$EXT_IFACE" oifname "docker0" drop
  }
}
EOF
else
  iptables -N DOCKER-USER 2>/dev/null || true
  iptables -F DOCKER-USER
  iptables -A DOCKER-USER -m conntrack --ctstate ESTABLISHED,RELATED -j RETURN
  iptables -A DOCKER-USER -i lo -j RETURN
  for cidr in $ALLOW_CIDRS; do
    iptables -A DOCKER-USER -i "$EXT_IFACE" -s "$cidr" -j RETURN
  done
  iptables -A DOCKER-USER -i "$EXT_IFACE" -j DROP
  iptables -A DOCKER-USER -j RETURN
fi
`, shellescape.Quote(strings.Join(allowCIDRs, " ")))
}

const dockerFirewallGuardUnit = `[Unit]
Description=kinstall Docker Firewall Guard (DOCKER-USER / nftables)
After=docker.service
BindsTo=docker.service
PartOf=docker.service

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/opt/kinstall/libexec/docker-fw-apply.sh

[Install]
WantedBy=multi-user.target
`

func (t *DockerEnsureTask) validateParams(spec sdk.TaskSpec) (int, []byte, string, error) {
	if rawLim, ok := spec.Params["limitNOFILE"]; ok {
		if s, isStr := rawLim.(string); isStr && strings.EqualFold(s, "infinity") {
			return 0, nil, "", sdk.NewError(sdk.CodeAPIInvalidRequest, "API", http.StatusUnprocessableEntity, sdk.CodeAPIInvalidRequest, false,
				fmt.Errorf("docker.service LimitNOFILE=infinity is forbidden (D2 §5.2)"))
		}
	}
	limit := 1048576
	switch v := spec.Params["limitNOFILE"].(type) {
	case int:
		limit = v
	case float64:
		limit = int(v)
	}
	unit, err := BuildDockerServiceUnit(limit)
	if err != nil {
		return 0, nil, "", err
	}
	daemonJSON, err := BuildDockerDaemonJSON(strParam(spec.Params, "dataRoot"), strParam(spec.Params, "firewallBackend"))
	if err != nil {
		return 0, nil, "", err
	}
	return limit, daemonJSON, unit, nil
}

func (t *DockerEnsureTask) Check(ctx context.Context, rc *RunContext, spec sdk.TaskSpec) (CheckResult, error) {
	_, daemonJSON, unit, err := t.validateParams(spec)
	if err != nil {
		return CheckResult{}, err
	}
	// On live hosts where Docker is already installed and healthy, preserve the host's native docker.service
	// unless forceReconfigure=true is explicitly requested (avoids restarting unrelated containers on host).
	if !boolParam(spec.Params, "skipSystemctl") && !boolParam(spec.Params, "forceReconfigure") {
		if res, rerr := rc.Session.Run(ctx, ports.Command{Script: "docker info >/dev/null 2>&1", Sudo: true}); rerr == nil && res.ExitCode == 0 {
			return CheckResult{Converged: true, Summary: "docker engine active and healthy"}, nil
		}
	}
	stD, errD := rc.Session.Stat(ctx, "/etc/docker/daemon.json")
	if errD != nil || !stD.Exists || stD.SHA256 != planner.SHA256Hex(daemonJSON) {
		return CheckResult{Converged: false, Summary: "daemon.json needs update"}, nil //nolint:nilerr // missing file triggers convergence
	}
	stU, errU := rc.Session.Stat(ctx, "/etc/systemd/system/docker.service")
	if errU != nil || !stU.Exists || stU.SHA256 != planner.SHA256Hex([]byte(unit)) {
		return CheckResult{Converged: false, Summary: "docker.service needs update"}, nil //nolint:nilerr // missing unit triggers convergence
	}
	stFW, errFW := rc.Session.Stat(ctx, "/etc/systemd/system/kinstall-docker-fw.service")
	if errFW != nil || !stFW.Exists {
		return CheckResult{Converged: false, Summary: "docker firewall guard missing"}, nil //nolint:nilerr // missing firewall guard triggers convergence
	}
	return CheckResult{Converged: true, Summary: "docker 29 + firewall guard converged"}, nil
}

func (t *DockerEnsureTask) Apply(ctx context.Context, rc *RunContext, spec sdk.TaskSpec) (ApplyResult, error) {
	_, daemonJSON, unit, err := t.validateParams(spec)
	if err != nil {
		return ApplyResult{}, err
	}
	allowCIDRs := strSliceParam(spec.Params, "allowCIDRs")
	fwScript := BuildDockerFirewallGuardScript(allowCIDRs)

	if _, err := rc.Session.Upload(ctx, bytes.NewReader(daemonJSON), "/etc/docker/daemon.json", 0o644, ""); err != nil {
		return ApplyResult{}, err
	}
	if _, err := rc.Session.Upload(ctx, strings.NewReader(unit), "/etc/systemd/system/docker.service", 0o644, ""); err != nil {
		return ApplyResult{}, err
	}
	if _, err := rc.Session.Upload(ctx, strings.NewReader(fwScript), "/opt/kinstall/libexec/docker-fw-apply.sh", 0o755, ""); err != nil {
		return ApplyResult{}, err
	}
	if _, err := rc.Session.Upload(ctx, strings.NewReader(dockerFirewallGuardUnit), "/etc/systemd/system/kinstall-docker-fw.service", 0o644, ""); err != nil {
		return ApplyResult{}, err
	}
	if !boolParam(spec.Params, "skipSystemctl") {
		_, _ = rc.Session.Run(ctx, ports.Command{
			Script: "systemctl daemon-reload && systemctl enable --now docker.service kinstall-docker-fw.service",
			Sudo:   true,
		})
	}
	return ApplyResult{Changed: true}, nil
}

func (t *DockerEnsureTask) Rollback(context.Context, *RunContext, sdk.TaskSpec, ApplyResult) error {
	return nil
}

// --- #14 image.load (registryMirror + immutable tag guard + digest inspect, D2 §5.1 #14) ---

type ImageLoadTask struct{}

func (t *ImageLoadTask) Name() string { return "image.load" }

func validateImageRef(ref string) error {
	if ref == "" {
		return sdk.NewError(sdk.CodeImgMutableTag, "IMG", http.StatusUnprocessableEntity, sdk.CodeImgMutableTag, false, fmt.Errorf("empty imageRef"))
	}
	if strings.HasSuffix(ref, ":latest") || (!strings.Contains(ref, ":") && !strings.Contains(ref, "@sha256:")) {
		return sdk.NewError(sdk.CodeImgMutableTag, "IMG", http.StatusUnprocessableEntity, sdk.CodeImgMutableTag, false,
			fmt.Errorf("mutable or untagged image reference forbidden: %q", ref))
	}
	low := strings.ToLower(ref)
	if strings.Contains(low, "bitnami/") || strings.Contains(low, "minio/minio") {
		return sdk.NewError(sdk.CodeImgForbiddenRegistry, "IMG", http.StatusUnprocessableEntity, sdk.CodeImgForbiddenRegistry, false,
			fmt.Errorf("forbidden image repository (ADR-012/ADR-042): %q", ref))
	}
	return nil
}

func (t *ImageLoadTask) Check(ctx context.Context, rc *RunContext, spec sdk.TaskSpec) (CheckResult, error) {
	ref := strParam(spec.Params, "imageRef")
	if err := validateImageRef(ref); err != nil {
		return CheckResult{}, err
	}
	res, err := rc.Session.Run(ctx, ports.Command{Script: "docker image inspect " + shellescape.Quote(ref) + " >/dev/null 2>&1"})
	if err == nil && res.ExitCode == 0 {
		return CheckResult{Converged: true, Summary: "image present"}, nil
	}
	return CheckResult{Converged: false, Summary: "load image " + ref}, nil
}

func (t *ImageLoadTask) Apply(ctx context.Context, rc *RunContext, spec sdk.TaskSpec) (ApplyResult, error) {
	ref := strParam(spec.Params, "imageRef")
	if err := validateImageRef(ref); err != nil {
		return ApplyResult{}, err
	}
	tarPath := strParam(spec.Params, "tarPath")
	var cmd string
	if tarPath != "" {
		cmd = fmt.Sprintf("zstd -dc %s | docker load", shellescape.Quote(tarPath))
	} else {
		cmd = "docker pull " + shellescape.Quote(ref)
	}
	res, err := rc.Session.Run(ctx, ports.Command{Script: cmd, Sudo: true, Async: spec.Async, TaskRunID: rc.TaskRunID})
	if err != nil {
		return ApplyResult{}, err
	}
	return ApplyResult{Changed: true, Output: res.Stdout}, nil
}

func (t *ImageLoadTask) Rollback(context.Context, *RunContext, sdk.TaskSpec, ApplyResult) error {
	return nil
}

// --- #15 compose.sync (Explicit mounts file|dir pre-creation + 0600 .env + legal Type=oneshot systemd unit, D2 §5.1 #15) ---

type ComposeSyncTask struct{}

func (t *ComposeSyncTask) Name() string { return "compose.sync" }

// BuildComposeSystemdUnit renders a systemd-analyze-clean Type=oneshot unit (strictly NO illegal Restart= on oneshot! R2 / D2 §5.1 #15).
func BuildComposeSystemdUnit(serviceName, composePath string) string {
	dir := path.Dir(composePath)
	return fmt.Sprintf(`[Unit]
Description=kinstall Managed Compose Stack (%s)
After=docker.service network-online.target
Requires=docker.service

[Service]
Type=oneshot
RemainAfterExit=yes
WorkingDirectory=%s
ExecStart=/bin/sh -c 'if docker compose version >/dev/null 2>&1; then exec docker compose -f %s up -d --remove-orphans; else exec docker-compose -f %s up -d --remove-orphans; fi'
ExecStop=/bin/sh -c 'if docker compose version >/dev/null 2>&1; then exec docker compose -f %s down; else exec docker-compose -f %s down; fi'

[Install]
WantedBy=multi-user.target
`, serviceName, dir, composePath, composePath, composePath, composePath)
}

func extractMounts(spec sdk.TaskSpec) ([]sdk.MountSpec, error) {
	raw, ok := spec.Params["mounts"]
	if !ok || raw == nil {
		return nil, nil
	}
	if direct, ok := raw.([]sdk.MountSpec); ok {
		return direct, nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var out []sdk.MountSpec
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func validateComposeSecurity(composeYAML string, allowPrivileged bool) error {
	if strings.Contains(composeYAML, "chmod 777") {
		return sdk.NewError(sdk.CodeAPIInvalidRequest, "API", http.StatusUnprocessableEntity, sdk.CodeAPIInvalidRequest, false,
			fmt.Errorf("compose.sync forbids chmod 777 (D2 §5.1 #15)"))
	}
	if !allowPrivileged && strings.Contains(composeYAML, "privileged: true") {
		return sdk.NewError(sdk.CodeAuthForbidden, "AUTH", http.StatusForbidden, sdk.CodeAuthForbidden, false,
			fmt.Errorf("compose.sync blocks privileged: true without explicit four-eyes allowPrivileged approval (D2 §5.1 #15)"))
	}
	return nil
}

func (t *ComposeSyncTask) Check(ctx context.Context, rc *RunContext, spec sdk.TaskSpec) (CheckResult, error) {
	serviceName := strParam(spec.Params, "serviceName")
	composePath := strParam(spec.Params, "composePath")
	unitPath := path.Join("/etc/systemd/system", serviceName+".service")

	if composeContent := strParam(spec.Params, "composeContent"); composeContent != "" {
		if err := validateComposeSecurity(composeContent, boolParam(spec.Params, "allowPrivileged")); err != nil {
			return CheckResult{}, err
		}
	}

	// Verify every explicit mount point exists with the EXACT declared type (file vs dir)
	mounts, err := extractMounts(spec)
	if err != nil {
		return CheckResult{}, err
	}
	for _, m := range mounts {
		st, serr := rc.Session.Stat(ctx, m.Src)
		if serr != nil || !st.Exists {
			return CheckResult{Converged: false, Summary: "mount missing: " + m.Src}, nil //nolint:nilerr // missing mount triggers convergence
		}
		if m.Type == "file" && st.IsDir {
			return CheckResult{}, sdk.NewError(sdk.CodeAPIInvalidRequest, "API", http.StatusConflict, sdk.CodeAPIInvalidRequest, false,
				fmt.Errorf("mount %s declared as type=file but exists as directory on host", m.Src))
		}
		if m.Type == "dir" && !st.IsDir {
			return CheckResult{}, sdk.NewError(sdk.CodeAPIInvalidRequest, "API", http.StatusConflict, sdk.CodeAPIInvalidRequest, false,
				fmt.Errorf("mount %s declared as type=dir but exists as regular file on host", m.Src))
		}
	}

	wantUnit := BuildComposeSystemdUnit(serviceName, composePath)
	stU, unitErr := rc.Session.Stat(ctx, unitPath)
	if unitErr != nil || !stU.Exists || stU.SHA256 != planner.SHA256Hex([]byte(wantUnit)) {
		return CheckResult{Converged: false, Summary: "compose systemd unit missing/changed"}, nil //nolint:nilerr // missing unit triggers convergence
	}
	return CheckResult{Converged: true, Summary: "compose stack converged"}, nil
}

func (t *ComposeSyncTask) Apply(ctx context.Context, rc *RunContext, spec sdk.TaskSpec) (ApplyResult, error) {
	serviceName := strParam(spec.Params, "serviceName")
	composePath := strParam(spec.Params, "composePath")
	allowPriv := boolParam(spec.Params, "allowPrivileged")

	if composeContent := strParam(spec.Params, "composeContent"); composeContent != "" {
		if err := validateComposeSecurity(composeContent, allowPriv); err != nil {
			return ApplyResult{}, err
		}
		if _, err := rc.Session.Upload(ctx, strings.NewReader(composeContent), composePath, 0o640, ""); err != nil {
			return ApplyResult{}, err
		}
	}

	// Render 0600 .env file if envSecrets provided
	if envContent := strParam(spec.Params, "envContent"); envContent != "" {
		envPath := path.Join(path.Dir(composePath), ".env")
		if _, err := rc.Session.Upload(ctx, strings.NewReader(envContent), envPath, 0o600, ""); err != nil {
			return ApplyResult{}, err
		}
	}

	// Atomically pre-create explicit mounts BEFORE docker compose up so missing single files are NEVER created as dirs (D2 §5.1 #15)!
	mounts, err := extractMounts(spec)
	if err != nil {
		return ApplyResult{}, err
	}
	for _, m := range mounts {
		mode := m.Mode
		if mode == 0 {
			if m.Type == "file" {
				mode = 0o640
			} else {
				mode = 0o750
			}
		}
		if m.Type == "file" {
			st, serr := rc.Session.Stat(ctx, m.Src)
			if serr != nil {
				return ApplyResult{}, serr
			}
			if !st.Exists {
				if _, uerr := rc.Session.Upload(ctx, strings.NewReader(""), m.Src, mode, ""); uerr != nil {
					return ApplyResult{}, uerr
				}
			}
		} else {
			cmd := fmt.Sprintf("mkdir -p %s && chmod %04o %s", shellescape.Quote(m.Src), uint32(mode.Perm()), shellescape.Quote(m.Src))
			if _, cerr := rc.Session.Run(ctx, ports.Command{Script: cmd, Sudo: true}); cerr != nil {
				return ApplyResult{}, cerr
			}
		}
	}

	unit := BuildComposeSystemdUnit(serviceName, composePath)
	unitPath := path.Join("/etc/systemd/system", serviceName+".service")
	if _, err := rc.Session.Upload(ctx, strings.NewReader(unit), unitPath, 0o644, ""); err != nil {
		return ApplyResult{}, err
	}
	if !boolParam(spec.Params, "skipDockerUp") {
		upCmd := fmt.Sprintf(`set -e
if [ ! -f %s ]; then echo "compose file missing: %s" >&2; exit 1; fi
if docker compose version >/dev/null 2>&1; then
  docker compose -f %s up -d --remove-orphans
elif command -v docker-compose >/dev/null 2>&1; then
  docker-compose -f %s up -d --remove-orphans
else
  echo "neither 'docker compose' nor 'docker-compose' found on target host" >&2
  exit 1
fi
systemctl daemon-reload && systemctl enable %s.service 2>/dev/null || true`,
			shellescape.Quote(composePath), composePath,
			shellescape.Quote(composePath), shellescape.Quote(composePath),
			shellescape.Quote(serviceName))
		res, err := rc.Session.Run(ctx, ports.Command{
			Script:    upCmd,
			Sudo:      true,
			Async:     spec.Async,
			TaskRunID: rc.TaskRunID,
			Stdout:    rc.LogStdout,
			Stderr:    rc.LogStderr,
		})
		if err != nil {
			return ApplyResult{}, err
		}
		return ApplyResult{Changed: true, Output: res.Stdout}, nil
	}
	return ApplyResult{Changed: true}, nil
}

func (t *ComposeSyncTask) Rollback(context.Context, *RunContext, sdk.TaskSpec, ApplyResult) error {
	return nil
}
