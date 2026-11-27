package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"al.essio.dev/pkg/shellescape"

	"github.com/kinstall/kinstall/internal/authz"
	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/internal/ports"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// HostLiveStatus holds real-time SSH-observed runtime status of an instance on a single host.
type HostLiveStatus struct {
	HostID        string    `json:"hostId"`
	HostName      string    `json:"hostName"`
	Address       string    `json:"address"`
	Reachable     bool      `json:"reachable"`
	ServiceActive bool      `json:"serviceActive"`
	ServiceState  string    `json:"serviceState"` // active | inactive | failed | running | exited | unknown
	MainPID       int       `json:"mainPid,omitempty"`
	MemoryBytes   int64     `json:"memoryBytes,omitempty"`
	StartedAt     string    `json:"startedAt,omitempty"`
	ContainerInfo string    `json:"containerInfo,omitempty"`
	PortsUp       []int     `json:"portsUp,omitempty"`
	PortsDown     []int     `json:"portsDown,omitempty"`
	AppProbeOK    bool      `json:"appProbeOk"`
	AppProbeMsg   string    `json:"appProbeMsg,omitempty"`
	Error         string    `json:"error,omitempty"`
	LatencyMs     int64     `json:"latencyMs"`
	CheckedAt     time.Time `json:"checkedAt"`
}

// InstanceLiveProbeReport aggregates real SSH probe results across all hosts of an instance.
type InstanceLiveProbeReport struct {
	InstanceID   string           `json:"instanceId"`
	InstanceName string           `json:"instanceName"`
	Product      string           `json:"product"`
	DeployTarget sdk.DeployTarget `json:"deployTarget"`
	OverallState string           `json:"overallState"` // running | degraded | stopped | unreachable | unassigned
	UnitName     string           `json:"unitName"`
	ComposePath  string           `json:"composePath,omitempty"`
	Hosts        []HostLiveStatus `json:"hosts"`
	ProbedAt     time.Time        `json:"probedAt"`
}

type runtimeCoords struct {
	unitName      string
	composePath   string
	containerName string
	ports         []int
}

func resolveRuntimeCoords(inst *domain.Instance) runtimeCoords {
	prod := strings.ToLower(strings.TrimSpace(inst.Product))
	prefix := prod
	if prod == "postgresql" {
		prefix = "pg"
	}
	unit := fmt.Sprintf("kinstall-%s-%s.service", prefix, inst.Name)
	compose := fmt.Sprintf("/opt/kinstall/compose/%s-%s/docker-compose.yml", prefix, inst.Name)
	container := fmt.Sprintf("kinstall-%s-%s", prefix, inst.Name)
	if prod == "nginx" && inst.DeployTarget == sdk.TargetHostBinary {
		unit = "nginx.service"
	}
	if prod == "kubernetes" {
		unit = "kube-apiserver.service"
	}

	var portsList []int
	var spec map[string]any
	if len(inst.DesiredSpecJSON) > 0 {
		_ = json.Unmarshal(inst.DesiredSpecJSON, &spec)
	}
	extractPort := func(key string) {
		if raw, ok := spec[key]; ok {
			switch v := raw.(type) {
			case float64:
				if int(v) > 0 {
					portsList = append(portsList, int(v))
				}
			case int:
				if v > 0 {
					portsList = append(portsList, v)
				}
			}
		}
	}
	extractPort("port")
	if len(portsList) == 0 {
		switch prod {
		case "valkey", "redis":
			portsList = []int{6379}
		case "postgresql":
			portsList = []int{5432}
		case "mysql":
			portsList = []int{3306}
		case "nginx":
			portsList = []int{80}
		case "kubernetes":
			portsList = []int{6443}
		case "kafka":
			portsList = []int{9092}
		case "opensearch":
			portsList = []int{9200}
		case "sonarqube":
			portsList = []int{9000}
		case "jenkins", "reposilite":
			portsList = []int{8080}
		case "verdaccio":
			portsList = []int{4873}
		}
	}
	return runtimeCoords{
		unitName:      unit,
		composePath:   compose,
		containerName: container,
		ports:         portsList,
	}
}

func (f *Facade) resolveInstanceHosts(ctx context.Context, projectID string, inst *domain.Instance) ([]domain.Host, []int, error) {
	allHosts, err := f.store.Hosts().ListByProject(ctx, projectID)
	if err != nil {
		return nil, nil, err
	}
	hostByID := make(map[string]domain.Host, len(allHosts))
	for _, h := range allHosts {
		hostByID[h.ID] = h
	}
	seen := make(map[string]bool)
	var out []domain.Host
	portSet := make(map[int]bool)
	for _, h := range allHosts {
		claims, _ := f.store.Instances().ListClaimsByHost(ctx, h.ID)
		for _, c := range claims {
			if c.InstanceID != inst.ID {
				continue
			}
			if !seen[h.ID] {
				seen[h.ID] = true
				out = append(out, h)
			}
			for _, p := range c.Ports {
				if p.Port > 0 {
					portSet[p.Port] = true
				}
			}
		}
	}
	if len(out) == 0 {
		hintIDs, hintRoles := extractInstanceHostHints(inst)
		for _, id := range hintIDs {
			if h, ok := hostByID[id]; ok && !seen[h.ID] {
				seen[h.ID] = true
				out = append(out, h)
			}
		}
		for id := range hintRoles {
			if h, ok := hostByID[id]; ok && !seen[h.ID] {
				seen[h.ID] = true
				out = append(out, h)
			}
		}
	}
	var claimedPorts []int
	for p := range portSet {
		claimedPorts = append(claimedPorts, p)
	}
	return out, claimedPorts, nil
}

// ProbeInstanceLive executes real SSH probes on every host assigned to an instance to verify
// systemd unit / Docker container state, live TCP ports, PID, memory, and product health.
func (f *Facade) ProbeInstanceLive(ctx context.Context, actor domain.Actor, projectID, instID string) (*InstanceLiveProbeReport, error) {
	if err := authz.Authorize(actor, projectID, "resource.read"); err != nil {
		return nil, err
	}
	inst, err := f.store.Instances().Get(ctx, instID)
	if err != nil {
		return nil, err
	}
	if f.exec == nil {
		return nil, fmt.Errorf("executor unavailable")
	}
	coords := resolveRuntimeCoords(inst)
	hosts, claimedPorts, err := f.resolveInstanceHosts(ctx, projectID, inst)
	if err != nil {
		return nil, err
	}
	checkPorts := claimedPorts
	if len(checkPorts) == 0 {
		checkPorts = coords.ports
	}

	report := &InstanceLiveProbeReport{
		InstanceID:   inst.ID,
		InstanceName: inst.Name,
		Product:      inst.Product,
		DeployTarget: inst.DeployTarget,
		UnitName:     coords.unitName,
		ComposePath:  coords.composePath,
		Hosts:        make([]HostLiveStatus, 0, len(hosts)),
		ProbedAt:     time.Now().UTC(),
	}
	if len(hosts) == 0 {
		report.OverallState = "unassigned"
		return report, nil
	}

	var portStrs []string
	for _, p := range checkPorts {
		portStrs = append(portStrs, strconv.Itoa(p))
	}
	probeScript := buildLiveProbeScript(inst, coords, portStrs)

	activeHosts, reachableHosts := 0, 0
	for i := range hosts {
		h := hosts[i]
		start := time.Now()
		hs := HostLiveStatus{
			HostID:       h.ID,
			HostName:     h.Name,
			Address:      h.Address,
			ServiceState: "unknown",
			CheckedAt:    time.Now().UTC(),
		}
		target, terr := BuildHostTarget(ctx, f.store, f.secrets, &h)
		if terr != nil {
			hs.Error = terr.Error()
			hs.LatencyMs = time.Since(start).Milliseconds()
			report.Hosts = append(report.Hosts, hs)
			continue
		}
		sess, cerr := f.exec.Connect(ctx, target)
		ZeroTargetAuth(&target)
		if cerr != nil {
			hs.Error = cerr.Error()
			hs.LatencyMs = time.Since(start).Milliseconds()
			report.Hosts = append(report.Hosts, hs)
			continue
		}
		res, rerr := sess.Run(ctx, ports.Command{Script: probeScript, Sudo: true, Timeout: 15 * time.Second})
		_ = sess.Close()
		hs.LatencyMs = time.Since(start).Milliseconds()
		hs.Reachable = true
		reachableHosts++
		if rerr != nil {
			hs.Error = rerr.Error()
			report.Hosts = append(report.Hosts, hs)
			continue
		}
		parseLiveProbeOutput(res.Stdout, checkPorts, &hs)
		if hs.ServiceActive {
			activeHosts++
		}
		report.Hosts = append(report.Hosts, hs)
	}

	switch {
	case reachableHosts == 0:
		report.OverallState = "unreachable"
	case activeHosts == len(hosts):
		report.OverallState = "running"
	case activeHosts > 0:
		report.OverallState = "degraded"
	default:
		report.OverallState = "stopped"
	}

	// Persist real observed state on Instance so list/dashboard reflect ground truth!
	switch report.OverallState {
	case "running":
		inst.State = "ready"
	case "degraded", "stopped", "unreachable":
		inst.State = report.OverallState
	}
	_ = f.store.Instances().Update(ctx, inst)
	return report, nil
}

func buildLiveProbeScript(inst *domain.Instance, coords runtimeCoords, portStrs []string) string {
	isContainer := inst.DeployTarget == sdk.TargetHostContainer
	portsArg := strings.Join(portStrs, " ")
	appCheck := "echo 'APP=ok:port-checked'"
	switch strings.ToLower(inst.Product) {
	case "postgresql":
		if isContainer {
			appCheck = fmt.Sprintf(`if docker exec %s pg_isready -U postgres >/dev/null 2>&1; then echo "APP=ok:pg_isready"; else echo "APP=fail:pg_isready"; fi`, shellescape.Quote(coords.containerName))
		} else {
			appCheck = `if command -v pg_isready >/dev/null 2>&1 && pg_isready -q; then echo "APP=ok:pg_isready"; else echo "APP=warn:tcp-only"; fi`
		}
	case "mysql":
		if isContainer {
			appCheck = fmt.Sprintf(`if docker exec %s mysqladmin ping -h 127.0.0.1 --silent >/dev/null 2>&1; then echo "APP=ok:mysqladmin-ping"; else echo "APP=fail:mysqladmin-ping"; fi`, shellescape.Quote(coords.containerName))
		}
	case "valkey", "redis":
		if isContainer {
			appCheck = fmt.Sprintf(`if docker exec %s valkey-cli ping >/dev/null 2>&1 || docker exec %s redis-cli ping >/dev/null 2>&1; then echo "APP=ok:pong"; else echo "APP=warn:auth-or-tcp"; fi`, shellescape.Quote(coords.containerName), shellescape.Quote(coords.containerName))
		}
	case "nginx":
		if len(portStrs) > 0 {
			appCheck = fmt.Sprintf(`if curl -fsS --max-time 2 http://127.0.0.1:%s/healthz >/dev/null 2>&1 || curl -sI --max-time 2 http://127.0.0.1:%s/ >/dev/null 2>&1; then echo "APP=ok:http-200"; else echo "APP=fail:http-unreachable"; fi`, portStrs[0], portStrs[0])
		}
	case "kubernetes":
		appCheck = `if curl -kfsS --max-time 3 https://127.0.0.1:6443/readyz >/dev/null 2>&1; then echo "APP=ok:apiserver-readyz"; else echo "APP=fail:apiserver-not-ready"; fi`
	}

	return fmt.Sprintf(`#!/bin/sh
export LC_ALL=C
IS_CONTAINER=%t
UNIT=%s
CONTAINER=%s
COMPOSE=%s
PORTS=%s

if [ "$IS_CONTAINER" = "true" ]; then
  if command -v docker >/dev/null 2>&1; then
    CST="$(docker inspect -f '{{.State.Status}}|{{.State.Pid}}|{{.State.StartedAt}}|{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$CONTAINER" 2>/dev/null || true)"
    if [ -z "$CST" ] && [ -f "$COMPOSE" ]; then
      CID="$( (docker compose -f "$COMPOSE" ps -q 2>/dev/null || docker-compose -f "$COMPOSE" ps -q 2>/dev/null) | head -n1 )"
      if [ -n "$CID" ]; then
        CST="$(docker inspect -f '{{.State.Status}}|{{.State.Pid}}|{{.State.StartedAt}}|{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$CID" 2>/dev/null || true)"
      fi
    fi
    echo "CONTAINER=$CST"
  fi
fi

UST="$(systemctl is-active "$UNIT" 2>/dev/null || echo inactive)"
PID="$(systemctl show -p MainPID --value "$UNIT" 2>/dev/null || echo 0)"
MEM="$(systemctl show -p MemoryCurrent --value "$UNIT" 2>/dev/null || echo 0)"
START="$(systemctl show -p ActiveEnterTimestamp --value "$UNIT" 2>/dev/null || true)"
echo "UNIT_STATE=$UST"
echo "MAIN_PID=$PID"
echo "MEM_BYTES=$MEM"
echo "STARTED_AT=$START"

UP=""
DOWN=""
for p in $PORTS; do
  if ss -lntH 2>/dev/null | awk '{print $4}' | grep -Eq ":$p$"; then
    UP="$UP $p"
  elif command -v nc >/dev/null 2>&1 && nc -z -w 1 127.0.0.1 "$p" >/dev/null 2>&1; then
    UP="$UP $p"
  else
    DOWN="$DOWN $p"
  fi
done
echo "PORTS_UP=$UP"
echo "PORTS_DOWN=$DOWN"
%s
`, isContainer, shellescape.Quote(coords.unitName), shellescape.Quote(coords.containerName), shellescape.Quote(coords.composePath), shellescape.Quote(portsArg), appCheck)
}

func parseLiveProbeOutput(stdout string, checkPorts []int, hs *HostLiveStatus) {
	kv := map[string]string{}
	for _, line := range strings.Split(stdout, "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			kv[k] = strings.TrimSpace(v)
		}
	}
	if cst := kv["CONTAINER"]; cst != "" {
		parts := strings.Split(cst, "|")
		if len(parts) >= 1 && parts[0] != "" {
			hs.ServiceState = parts[0]
			hs.ServiceActive = parts[0] == "running"
		}
		if len(parts) >= 2 {
			hs.MainPID, _ = strconv.Atoi(parts[1])
		}
		if len(parts) >= 3 {
			hs.StartedAt = parts[2]
		}
		if len(parts) >= 4 {
			hs.ContainerInfo = fmt.Sprintf("container=%s health=%s", parts[0], parts[3])
		}
	} else {
		hs.ServiceState = kv["UNIT_STATE"]
		if hs.ServiceState == "" {
			hs.ServiceState = "inactive"
		}
		hs.ServiceActive = hs.ServiceState == "active"
		hs.MainPID, _ = strconv.Atoi(kv["MAIN_PID"])
		if mem, err := strconv.ParseInt(kv["MEM_BYTES"], 10, 64); err == nil && mem > 0 && mem < 1<<50 {
			hs.MemoryBytes = mem
		}
		hs.StartedAt = kv["STARTED_AT"]
	}

	for _, f := range strings.Fields(kv["PORTS_UP"]) {
		if p, err := strconv.Atoi(f); err == nil && p > 0 {
			hs.PortsUp = append(hs.PortsUp, p)
		}
	}
	for _, f := range strings.Fields(kv["PORTS_DOWN"]) {
		if p, err := strconv.Atoi(f); err == nil && p > 0 {
			hs.PortsDown = append(hs.PortsDown, p)
		}
	}
	if app := kv["APP"]; app != "" {
		status, msg, _ := strings.Cut(app, ":")
		hs.AppProbeOK = status == "ok" || status == "warn"
		hs.AppProbeMsg = msg
	}
	// If all checked ports are listening and app probe passed, treat service as active even if systemd unit differs
	if !hs.ServiceActive && len(checkPorts) > 0 && len(hs.PortsDown) == 0 && len(hs.PortsUp) == len(checkPorts) && hs.AppProbeOK {
		hs.ServiceActive = true
		hs.ServiceState = "running"
	}
}

// ControlInstanceService executes a real start / stop / restart / reload on every host bound to an instance,
// writes an audit entry, and immediately re-probes live state.
func (f *Facade) ControlInstanceService(ctx context.Context, actor domain.Actor, projectID, instID, action string) (*InstanceLiveProbeReport, error) {
	if err := authz.Authorize(actor, projectID, "instance.apply"); err != nil {
		return nil, err
	}
	act := strings.ToLower(strings.TrimSpace(action))
	if act != "start" && act != "stop" && act != "restart" && act != "reload" {
		return nil, sdk.NewError(sdk.CodeAPIInvalidRequest, "INST", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false,
			fmt.Errorf("unsupported service control action %q (allowed: start|stop|restart|reload)", action))
	}
	inst, err := f.store.Instances().Get(ctx, instID)
	if err != nil {
		return nil, err
	}
	if f.exec == nil {
		return nil, fmt.Errorf("executor unavailable")
	}
	coords := resolveRuntimeCoords(inst)
	hosts, _, err := f.resolveInstanceHosts(ctx, projectID, inst)
	if err != nil {
		return nil, err
	}
	if len(hosts) == 0 {
		return nil, sdk.NewError(sdk.CodeAPIInvalidRequest, "INST", http.StatusUnprocessableEntity, sdk.CodeAPIInvalidRequest, false,
			fmt.Errorf("instance %s has no assigned hosts", inst.Name))
	}

	var ctrlScript string
	if inst.DeployTarget == sdk.TargetHostContainer {
		composeAct := act
		if act == "reload" {
			composeAct = "restart"
		}
		if act == "start" {
			ctrlScript = fmt.Sprintf(`if [ -f %s ]; then (docker compose -f %s up -d || docker-compose -f %s up -d); else docker start %s; fi`,
				shellescape.Quote(coords.composePath), shellescape.Quote(coords.composePath), shellescape.Quote(coords.composePath), shellescape.Quote(coords.containerName))
		} else {
			ctrlScript = fmt.Sprintf(`if [ -f %s ]; then (docker compose -f %s %s || docker-compose -f %s %s); else docker %s %s; fi`,
				shellescape.Quote(coords.composePath), shellescape.Quote(coords.composePath), composeAct, shellescape.Quote(coords.composePath), composeAct, composeAct, shellescape.Quote(coords.containerName))
		}
	} else {
		sysAct := act
		if act == "reload" {
			sysAct = "reload-or-restart"
		}
		ctrlScript = fmt.Sprintf("systemctl %s %s", sysAct, shellescape.Quote(coords.unitName))
	}

	var errs []string
	for i := range hosts {
		h := hosts[i]
		target, terr := BuildHostTarget(ctx, f.store, f.secrets, &h)
		if terr != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", h.Name, terr))
			continue
		}
		sess, cerr := f.exec.Connect(ctx, target)
		ZeroTargetAuth(&target)
		if cerr != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", h.Name, cerr))
			continue
		}
		_, rerr := sess.Run(ctx, ports.Command{Script: ctrlScript, Sudo: true, Timeout: 60 * time.Second})
		_ = sess.Close()
		if rerr != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", h.Name, rerr))
		}
	}

	resState := "success"
	if len(errs) > 0 {
		resState = "failure"
	}
	f.recordAudit(ctx, actor, projectID, "instance.control."+act, "instance", inst.ID, resState, strings.Join(errs, "; "), map[string]any{
		"name": inst.Name, "product": inst.Product, "hosts": len(hosts),
	})
	if len(errs) > 0 {
		return nil, fmt.Errorf("service %s failed: %s", act, strings.Join(errs, "; "))
	}
	return f.ProbeInstanceLive(ctx, actor, projectID, instID)
}

// FetchInstanceLogs pulls real service logs via SSH (`docker compose logs` or `journalctl -u`).
func (f *Facade) FetchInstanceLogs(ctx context.Context, actor domain.Actor, projectID, instID, hostID string, lines int) (map[string]any, error) {
	if err := authz.Authorize(actor, projectID, "resource.read"); err != nil {
		return nil, err
	}
	if lines <= 0 || lines > 2000 {
		lines = 200
	}
	inst, err := f.store.Instances().Get(ctx, instID)
	if err != nil {
		return nil, err
	}
	if f.exec == nil {
		return nil, fmt.Errorf("executor unavailable")
	}
	hosts, _, err := f.resolveInstanceHosts(ctx, projectID, inst)
	if err != nil {
		return nil, err
	}
	if len(hosts) == 0 {
		return nil, sdk.NewError(sdk.CodeAPIInvalidRequest, "INST", http.StatusUnprocessableEntity, sdk.CodeAPIInvalidRequest, false,
			fmt.Errorf("instance %s has no assigned hosts", inst.Name))
	}
	chosen := hosts[0]
	if hostID != "" {
		for _, h := range hosts {
			if h.ID == hostID {
				chosen = h
				break
			}
		}
	}
	coords := resolveRuntimeCoords(inst)
	var logScript string
	if inst.DeployTarget == sdk.TargetHostContainer {
		logScript = fmt.Sprintf(`if [ -f %s ]; then (docker compose -f %s logs --tail=%d --no-color 2>&1 || docker-compose -f %s logs --tail=%d --no-color 2>&1); else docker logs --tail=%d %s 2>&1; fi`,
			shellescape.Quote(coords.composePath), shellescape.Quote(coords.composePath), lines, shellescape.Quote(coords.composePath), lines, lines, shellescape.Quote(coords.containerName))
	} else {
		logScript = fmt.Sprintf(`journalctl -u %s -n %d --no-pager 2>&1`, shellescape.Quote(coords.unitName), lines)
	}

	target, err := BuildHostTarget(ctx, f.store, f.secrets, &chosen)
	if err != nil {
		return nil, err
	}
	sess, err := f.exec.Connect(ctx, target)
	ZeroTargetAuth(&target)
	if err != nil {
		return nil, err
	}
	defer func() { _ = sess.Close() }()

	res, err := sess.Run(ctx, ports.Command{Script: logScript, Sudo: true, Timeout: 20 * time.Second})
	if err != nil && res.Stdout == "" {
		return nil, err
	}
	return map[string]any{
		"instanceId": inst.ID,
		"hostId":     chosen.ID,
		"hostName":   chosen.Name,
		"unitName":   coords.unitName,
		"lines":      lines,
		"logs":       res.Stdout + res.Stderr,
		"fetchedAt":  time.Now().UTC(),
	}, nil
}
