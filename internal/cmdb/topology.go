package cmdb

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/kinstall/kinstall/internal/domain"
)

// ListTopologyTree builds the cycle-guarded Business Mainline or Resource Infrastructure tree (bk-cmdb / LiveOps 03.3).
func (s *Service) ListTopologyTree(ctx context.Context, projectID, topoKind string) ([]TopologyNode, error) {
	if err := s.EnsureDefaults(ctx, projectID); err != nil {
		return nil, err
	}
	if topoKind != "resource" {
		topoKind = "business"
	}

	// Count bound hosts per topology node
	bindCounts := make(map[string]int)
	cRows, err := s.readDB.QueryRaw(ctx,
		`SELECT topo_node_id, COUNT(1) FROM cmdb_host_bindings WHERE project_id = ? GROUP BY topo_node_id`, projectID)
	if err == nil {
		defer func() { _ = cRows.Close() }()
		for cRows.Next() {
			var nid string
			var cnt int
			if err := cRows.Scan(&nid, &cnt); err == nil {
				bindCounts[nid] = cnt
			}
		}
	}

	rows, err := s.readDB.QueryRaw(ctx,
		`SELECT id, project_id, topo_kind, parent_id, model_code, name, service_template_id, metadata_json, sort_order, created_at
		 FROM cmdb_topology_nodes WHERE project_id = ? AND topo_kind = ? ORDER BY sort_order ASC, name ASC`, projectID, topoKind)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	flat := make([]TopologyNode, 0)
	for rows.Next() {
		var n TopologyNode
		var metaStr, cAt string
		if err := rows.Scan(&n.ID, &n.ProjectID, &n.TopoKind, &n.ParentID, &n.ModelCode, &n.Name, &n.ServiceTemplateID, &metaStr, &n.SortOrder, &cAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(metaStr), &n.Metadata)
		n.CreatedAt = parseTS(cAt)
		n.BoundHostCount = bindCounts[n.ID]
		flat = append(flat, n)
	}

	// Build hierarchical tree with depth guard (max depth 10)
	byParent := make(map[string][]TopologyNode)
	for _, n := range flat {
		byParent[n.ParentID] = append(byParent[n.ParentID], n)
	}
	var build func(parentID string, depth int, seen map[string]bool) []TopologyNode
	build = func(parentID string, depth int, seen map[string]bool) []TopologyNode {
		if depth > 10 {
			return nil
		}
		children := byParent[parentID]
		res := make([]TopologyNode, 0, len(children))
		for _, c := range children {
			if seen[c.ID] {
				continue
			}
			seen[c.ID] = true
			c.Children = build(c.ID, depth+1, seen)
			for _, ch := range c.Children {
				c.BoundHostCount += ch.BoundHostCount
			}
			res = append(res, c)
		}
		return res
	}
	return build("", 0, make(map[string]bool)), nil
}

func (s *Service) CreateTopologyNode(ctx context.Context, projectID string, n *TopologyNode) (*TopologyNode, error) {
	if err := s.EnsureDefaults(ctx, projectID); err != nil {
		return nil, err
	}
	n.Name = strings.TrimSpace(n.Name)
	if n.Name == "" {
		return nil, invalidErr("拓扑节点名称不能为空")
	}
	if n.TopoKind != "resource" {
		n.TopoKind = "business"
	}
	if n.ModelCode == "" {
		if n.TopoKind == "business" {
			n.ModelCode = "m_module"
		} else {
			n.ModelCode = "m_rack"
		}
	}
	if n.Metadata == nil {
		n.Metadata = map[string]any{}
	}
	metaB, _ := json.Marshal(n.Metadata)
	now := time.Now().UTC()
	n.ID = domain.NewID("topo")
	n.ProjectID = projectID
	n.CreatedAt = now
	_, err := s.writeDB.ExecRaw(ctx,
		`INSERT INTO cmdb_topology_nodes (id, project_id, topo_kind, parent_id, model_code, name, service_template_id, metadata_json, sort_order, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		n.ID, projectID, n.TopoKind, n.ParentID, n.ModelCode, n.Name, n.ServiceTemplateID, string(metaB), n.SortOrder, now.Format(time.RFC3339Nano),
	)
	if err != nil {
		return nil, err
	}
	return n, nil
}

func (s *Service) listChildTopoIDs(ctx context.Context, projectID, parentID string) []string {
	rows, err := s.readDB.QueryRaw(ctx, `SELECT id FROM cmdb_topology_nodes WHERE project_id = ? AND parent_id = ?`, projectID, parentID)
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()
	var childIDs []string
	for rows.Next() {
		var cid string
		if rows.Scan(&cid) == nil {
			childIDs = append(childIDs, cid)
		}
	}
	return childIDs
}

func (s *Service) DeleteTopologyNode(ctx context.Context, projectID, nodeID string) error {
	// Also detach children recursively to prevent orphans
	childIDs := s.listChildTopoIDs(ctx, projectID, nodeID)
	for _, cid := range childIDs {
		_ = s.DeleteTopologyNode(ctx, projectID, cid)
	}
	_, err := s.writeDB.ExecRaw(ctx, `DELETE FROM cmdb_topology_nodes WHERE project_id = ? AND id = ?`, projectID, nodeID)
	return err
}

// BindHostToTopoNode idempotently attaches a host to a module/zone node without ever mutating or deleting the host (LiveOps 03.3 fix).
func (s *Service) BindHostToTopoNode(ctx context.Context, projectID, hostID, topoNodeID string) error {
	if hostID == "" || topoNodeID == "" {
		return invalidErr("hostId and topoNodeId are required")
	}
	_, err := s.writeDB.ExecRaw(ctx,
		`INSERT INTO cmdb_host_bindings (id, project_id, host_id, topo_node_id, created_at)
		 VALUES (?, ?, ?, ?, ?) ON CONFLICT (project_id, host_id, topo_node_id) DO NOTHING`,
		domain.NewID("hbind"), projectID, hostID, topoNodeID, time.Now().UTC().Format(time.RFC3339Nano),
	)
	return err
}

func (s *Service) UnbindHostFromTopoNode(ctx context.Context, projectID, hostID, topoNodeID string) error {
	_, err := s.writeDB.ExecRaw(ctx,
		`DELETE FROM cmdb_host_bindings WHERE project_id = ? AND host_id = ? AND topo_node_id = ?`,
		projectID, hostID, topoNodeID,
	)
	return err
}

func (s *Service) ListBoundHostIDs(ctx context.Context, projectID, topoNodeID string) ([]string, error) {
	rows, err := s.readDB.QueryRaw(ctx,
		`SELECT host_id FROM cmdb_host_bindings WHERE project_id = ? AND topo_node_id = ?`, projectID, topoNodeID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]string, 0)
	for rows.Next() {
		var hid string
		if err := rows.Scan(&hid); err == nil {
			out = append(out, hid)
		}
	}
	return out, rows.Err()
}

// --- Service Templates (bk-cmdb standardization) ---

func (s *Service) ListServiceTemplates(ctx context.Context, projectID string) ([]ServiceTemplate, error) {
	if err := s.EnsureDefaults(ctx, projectID); err != nil {
		return nil, err
	}
	rows, err := s.readDB.QueryRaw(ctx,
		`SELECT id, project_id, name, category, description, processes_json, ports_json, default_params_json, created_at, updated_at
		 FROM cmdb_service_templates WHERE project_id = ? ORDER BY name ASC`, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]ServiceTemplate, 0)
	for rows.Next() {
		var t ServiceTemplate
		var pStr, portStr, dpStr, cAt, uAt string
		if err := rows.Scan(&t.ID, &t.ProjectID, &t.Name, &t.Category, &t.Description, &pStr, &portStr, &dpStr, &cAt, &uAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(pStr), &t.Processes)
		_ = json.Unmarshal([]byte(portStr), &t.Ports)
		_ = json.Unmarshal([]byte(dpStr), &t.DefaultParams)
		if t.Processes == nil {
			t.Processes = []ServiceTemplateProcess{}
		}
		if t.Ports == nil {
			t.Ports = []int{}
		}
		if t.DefaultParams == nil {
			t.DefaultParams = map[string]any{}
		}
		t.CreatedAt, t.UpdatedAt = parseTS(cAt), parseTS(uAt)
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Service) CreateServiceTemplate(ctx context.Context, projectID string, t *ServiceTemplate) (*ServiceTemplate, error) {
	t.Name = strings.TrimSpace(t.Name)
	if t.Name == "" {
		return nil, invalidErr("服务模板名称不能为空")
	}
	if t.Category == "" {
		t.Category = "业务服务"
	}
	if t.Processes == nil {
		t.Processes = []ServiceTemplateProcess{}
	}
	if t.Ports == nil {
		t.Ports = []int{}
	}
	if t.DefaultParams == nil {
		t.DefaultParams = map[string]any{}
	}
	pB, _ := json.Marshal(t.Processes)
	portB, _ := json.Marshal(t.Ports)
	dpB, _ := json.Marshal(t.DefaultParams)
	now := time.Now().UTC()
	t.ID = domain.NewID("stpl")
	t.ProjectID = projectID
	t.CreatedAt, t.UpdatedAt = now, now
	_, err := s.writeDB.ExecRaw(ctx,
		`INSERT INTO cmdb_service_templates (id, project_id, name, category, description, processes_json, ports_json, default_params_json, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, projectID, t.Name, t.Category, t.Description, string(pB), string(portB), string(dpB), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
	)
	if err != nil {
		return nil, conflictErr("服务模板名称已存在: " + err.Error())
	}
	return t, nil
}

func (s *Service) DeleteServiceTemplate(ctx context.Context, projectID, tplID string) error {
	_, err := s.writeDB.ExecRaw(ctx, `DELETE FROM cmdb_service_templates WHERE project_id = ? AND id = ?`, projectID, tplID)
	return err
}

func (s *Service) ApplyServiceTemplateToNode(ctx context.Context, projectID, topoNodeID, templateID string) error {
	_, err := s.writeDB.ExecRaw(ctx,
		`UPDATE cmdb_topology_nodes SET service_template_id = ? WHERE project_id = ? AND id = ?`,
		templateID, projectID, topoNodeID,
	)
	return err
}

// --- Whitelist Read-Only Collector Plugins (LiveOps 03.3 RCE Prevention) ---

func WhitelistCollectors() []CollectorPluginSpec {
	return []CollectorPluginSpec{
		{ID: "os_info", Name: "操作系统与架构采集", Category: "system", Description: "只读采集主机发行版、内核版本、CPU 架构与主机名", TargetModel: "m_host", Fields: []string{"inner_ip", "os_distro", "arch"}, ReadOnly: true},
		{ID: "cpu_info", Name: "CPU 型号与核心数采集", Category: "hardware", Description: "只读解析 /proc/cpuinfo 与系统 1/5/15 分钟负载", TargetModel: "m_host", Fields: []string{"cpu_cores"}, ReadOnly: true},
		{ID: "mem_info", Name: "物理内存与可用水位采集", Category: "hardware", Description: "只读解析 /proc/meminfo 总内存与可用内存", TargetModel: "m_host", Fields: []string{"mem_gb"}, ReadOnly: true},
		{ID: "disk_info", Name: "块设备与挂载点容量采集", Category: "storage", Description: "只读执行 df -P -B1 采集磁盘总容量与各分区使用率", TargetModel: "m_host", Fields: []string{"disk_gb"}, ReadOnly: true},
		{ID: "process_scan", Name: "运行进程与服务指纹发现", Category: "process", Description: "白名单只读扫描活跃守护进程并同步至 m_process CI", TargetModel: "m_process", Fields: []string{"binary_name", "run_user"}, ReadOnly: true},
		{ID: "ports_scan", Name: "本机 TCP/UDP 监听端口扫描", Category: "network", Description: "只读执行 ss -lntup 采集本机监听端口并回填进程 CI", TargetModel: "m_process", Fields: []string{"listen_port"}, ReadOnly: true},
		{ID: "os_users", Name: "系统登录账号基线扫描", Category: "security", Description: "只读解析 /etc/passwd 识别可登录系统账号与 UID", TargetModel: "m_host", Fields: []string{"os_users"}, ReadOnly: true},
	}
}

// SyncHostToCI upserts a real Host into the CMDB m_host CI table (and links detected processes to m_process CIs).
func (s *Service) SyncHostToCI(ctx context.Context, projectID, actor string, h *domain.Host) (*CIInstance, error) {
	if err := s.EnsureDefaults(ctx, projectID); err != nil {
		return nil, err
	}
	attrs := map[string]any{
		"inner_ip": h.Address,
	}
	if h.OSID != "" {
		attrs["os_distro"] = strings.TrimSpace(h.OSID + " " + h.OSVersion)
	}
	if h.Arch != "" {
		attrs["arch"] = h.Arch
	}
	if h.Facts != nil {
		if h.Facts.OS.PrettyName != "" {
			attrs["os_distro"] = h.Facts.OS.PrettyName
		} else if h.Facts.OS.ID != "" {
			attrs["os_distro"] = strings.TrimSpace(h.Facts.OS.ID + " " + h.Facts.OS.VersionID)
		}
		if h.Facts.CPUCores > 0 {
			attrs["cpu_cores"] = h.Facts.CPUCores
		}
		if h.Facts.MemMB > 0 {
			attrs["mem_gb"] = float64(h.Facts.MemMB) / 1024.0
		}
		var diskBytes int64
		for _, m := range h.Facts.Mounts {
			if m.MountPoint == "/" {
				diskBytes = m.TotalBytes
				break
			}
		}
		if diskBytes > 0 {
			attrs["disk_gb"] = int(diskBytes / (1024 * 1024 * 1024))
		}
		if h.Facts.Arch != "" {
			attrs["arch"] = h.Facts.Arch
		}
	}
	if h.FaultRack != "" {
		attrs["rack_slot"] = h.FaultRack
	}
	owner := ""
	if h.Labels != nil {
		owner = h.Labels["owner"]
	}
	status := "active"
	if h.State == domain.HostDraining || string(h.State) == "unreachable" || string(h.State) == "offline" {
		status = "offline"
	}

	existing, _ := s.findCIByUniqueOrName(ctx, projectID, "m_host", h.Address, h.Name)
	var hostCI *CIInstance
	var err error
	if existing != nil {
		hostCI, err = s.UpdateCIInstance(ctx, projectID, existing.ID, actor, &CIInstance{
			Name:      h.Name,
			UniqueKey: h.Address,
			Status:    status,
			Env:       "prod",
			Owner:     owner,
			Attrs:     attrs,
		})
	} else {
		hostCI, err = s.CreateCIInstance(ctx, projectID, actor, &CIInstance{
			ModelCode: "m_host",
			Name:      h.Name,
			UniqueKey: h.Address,
			Status:    status,
			Env:       "prod",
			Owner:     owner,
			Attrs:     attrs,
		})
	}
	if err != nil {
		return nil, err
	}

	// Sync detected host listening ports/processes into m_process CIs with hosts relation
	if h.Facts != nil {
		for _, lp := range h.Facts.ListeningPorts {
			procName := fmt.Sprintf("%s:%s/%d", h.Name, lp.Proto, lp.Port)
			binName := lp.Process
			if binName == "" {
				binName = fmt.Sprintf("listener-%d", lp.Port)
			}
			pCI, _ := s.findCIByUniqueOrName(ctx, projectID, "m_process", procName, procName)
			if pCI == nil {
				pCI, _ = s.CreateCIInstance(ctx, projectID, actor, &CIInstance{
					ModelCode: "m_process",
					Name:      procName,
					UniqueKey: procName,
					Status:    "active",
					Env:       "prod",
					Owner:     owner,
					Attrs: map[string]any{
						"binary_name": binName,
						"listen_port": lp.Port,
						"run_user":    h.SSHUser,
					},
				})
			}
			if pCI != nil {
				_, _ = s.CreateCIRelation(ctx, projectID, &CIRelation{
					SourceCIID:   hostCI.ID,
					TargetCIID:   pCI.ID,
					RelationType: "hosts",
					Label:        "监听进程",
				})
			}
		}
	}
	return hostCI, nil
}
