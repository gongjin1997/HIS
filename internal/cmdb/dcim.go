package cmdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"net/netip"
	"strings"
	"time"

	"github.com/kinstall/kinstall/internal/domain"
)

// --- DCIM Rack & 42U Elevation Engine (NetBox-inspired) ---

func (s *Service) ListRacks(ctx context.Context, projectID string) ([]Rack, error) {
	if err := s.EnsureDefaults(ctx, projectID); err != nil {
		return nil, err
	}
	rows, err := s.readDB.QueryRaw(ctx,
		`SELECT id, project_id, site_name, room_name, name, u_height, power_kw, status, notes, created_at
		 FROM dcim_racks WHERE project_id = ? ORDER BY site_name ASC, name ASC`, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	racks := make([]Rack, 0)
	for rows.Next() {
		var r Rack
		var cAt string
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.SiteName, &r.RoomName, &r.Name, &r.UHeight, &r.PowerKW, &r.Status, &r.Notes, &cAt); err != nil {
			return nil, err
		}
		r.CreatedAt = parseTS(cAt)
		units, _ := s.ListRackUnits(ctx, projectID, r.ID)
		r.Units = units
		for _, u := range units {
			r.UsedU += u.USize
			r.UsedPowerW += u.PowerW
		}
		if r.UHeight > 0 {
			r.UtilPercent = math.Round((float64(r.UsedU)/float64(r.UHeight))*1000) / 10
		}
		racks = append(racks, r)
	}
	return racks, rows.Err()
}

func (s *Service) ListRackUnits(ctx context.Context, projectID, rackID string) ([]RackUnit, error) {
	rows, err := s.readDB.QueryRaw(ctx,
		`SELECT id, project_id, rack_id, ci_id, device_name, device_role, start_u, u_size, power_w, status, color_tag, created_at
		 FROM dcim_rack_units WHERE project_id = ? AND rack_id = ? ORDER BY start_u DESC`, projectID, rackID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]RackUnit, 0)
	for rows.Next() {
		var u RackUnit
		var cAt string
		if err := rows.Scan(&u.ID, &u.ProjectID, &u.RackID, &u.CIID, &u.DeviceName, &u.DeviceRole, &u.StartU, &u.USize, &u.PowerW, &u.Status, &u.ColorTag, &cAt); err != nil {
			return nil, err
		}
		u.CreatedAt = parseTS(cAt)
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Service) CreateRack(ctx context.Context, projectID string, r *Rack) (*Rack, error) {
	if err := s.EnsureDefaults(ctx, projectID); err != nil {
		return nil, err
	}
	r.SiteName = strings.TrimSpace(r.SiteName)
	r.Name = strings.TrimSpace(r.Name)
	if r.SiteName == "" || r.Name == "" {
		return nil, invalidErr("机房站点与机柜编号不能为空")
	}
	if r.RoomName == "" {
		r.RoomName = "主数据机房"
	}
	if r.UHeight <= 0 || r.UHeight > 60 {
		r.UHeight = 42
	}
	if r.PowerKW <= 0 {
		r.PowerKW = 8.0
	}
	if r.Status == "" {
		r.Status = "active"
	}
	now := time.Now().UTC()
	r.ID = domain.NewID("rack")
	r.ProjectID = projectID
	r.CreatedAt = now
	r.Units = []RackUnit{}
	_, err := s.writeDB.ExecRaw(ctx,
		`INSERT INTO dcim_racks (id, project_id, site_name, room_name, name, u_height, power_kw, status, notes, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, projectID, r.SiteName, r.RoomName, r.Name, r.UHeight, r.PowerKW, r.Status, r.Notes, now.Format(time.RFC3339Nano),
	)
	if err != nil {
		return nil, conflictErr("同站点下机柜编号已存在: " + err.Error())
	}
	return r, nil
}

func (s *Service) DeleteRack(ctx context.Context, projectID, rackID string) error {
	_, err := s.writeDB.ExecRaw(ctx, `DELETE FROM dcim_racks WHERE project_id = ? AND id = ?`, projectID, rackID)
	return err
}

// MountRackUnit validates strict [startU, startU+uSize-1] bounds and overlap conflict detection before mounting.
func (s *Service) MountRackUnit(ctx context.Context, projectID, rackID string, u *RackUnit) (*RackUnit, error) {
	var uHeight int
	err := s.readDB.ScanRaw(ctx, `SELECT u_height FROM dcim_racks WHERE project_id = ? AND id = ?`, []any{projectID, rackID}, &uHeight)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFoundErr("dcim_rack", rackID)
	}
	if err != nil {
		return nil, err
	}
	u.DeviceName = strings.TrimSpace(u.DeviceName)
	if u.DeviceName == "" {
		return nil, invalidErr("设备名称不能为空")
	}
	if u.USize <= 0 {
		u.USize = 1
	}
	endU := u.StartU + u.USize - 1
	if u.StartU < 1 || endU > uHeight {
		return nil, invalidErr(fmt.Sprintf("U 位范围 U%d-U%d 超出机柜有效高度 (1-%dU)", u.StartU, endU, uHeight))
	}

	existing, err := s.ListRackUnits(ctx, projectID, rackID)
	if err != nil {
		return nil, err
	}
	for _, ex := range existing {
		exEnd := ex.StartU + ex.USize - 1
		if u.StartU <= exEnd && endU >= ex.StartU {
			return nil, conflictErr(fmt.Sprintf("U 位冲突：U%d-U%d 与已上架设备「%s」(U%d-U%d) 重叠", u.StartU, endU, ex.DeviceName, ex.StartU, exEnd))
		}
	}

	if u.DeviceRole == "" {
		u.DeviceRole = "server"
	}
	if u.Status == "" {
		u.Status = "active"
	}
	if u.ColorTag == "" {
		switch u.DeviceRole {
		case "switch":
			u.ColorTag = "accent"
		case "firewall":
			u.ColorTag = "warn"
		case "storage":
			u.ColorTag = "info"
		default:
			u.ColorTag = "primary"
		}
	}
	now := time.Now().UTC()
	u.ID = domain.NewID("ru")
	u.ProjectID = projectID
	u.RackID = rackID
	u.CreatedAt = now
	_, err = s.writeDB.ExecRaw(ctx,
		`INSERT INTO dcim_rack_units (id, project_id, rack_id, ci_id, device_name, device_role, start_u, u_size, power_w, status, color_tag, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		u.ID, projectID, rackID, u.CIID, u.DeviceName, u.DeviceRole, u.StartU, u.USize, u.PowerW, u.Status, u.ColorTag, now.Format(time.RFC3339Nano),
	)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (s *Service) UnmountRackUnit(ctx context.Context, projectID, unitID string) error {
	_, err := s.writeDB.ExecRaw(ctx, `DELETE FROM dcim_rack_units WHERE project_id = ? AND id = ?`, projectID, unitID)
	return err
}

// --- IPAM Subnet & IP Allocation Engine (NetBox-inspired) ---

func usableHostsInCIDR(cidr string) int {
	p, err := netip.ParsePrefix(cidr)
	if err != nil {
		return 0
	}
	bits := p.Addr().BitLen() - p.Bits()
	if bits <= 0 {
		return 1
	}
	if bits > 20 {
		return 1 << 20 // cap display for massive /8 or IPv6 blocks
	}
	total := 1 << bits
	if p.Addr().Is4() && bits >= 2 {
		return total - 2
	}
	return total
}

func (s *Service) ListPrefixes(ctx context.Context, projectID string) ([]IPAMPrefix, error) {
	if err := s.EnsureDefaults(ctx, projectID); err != nil {
		return nil, err
	}
	usedCounts := make(map[string]int)
	cRows, err := s.readDB.QueryRaw(ctx,
		`SELECT prefix_id, COUNT(1) FROM ipam_addresses WHERE project_id = ? GROUP BY prefix_id`, projectID)
	if err == nil {
		defer func() { _ = cRows.Close() }()
		for cRows.Next() {
			var pid string
			var cnt int
			if err := cRows.Scan(&pid, &cnt); err == nil {
				usedCounts[pid] = cnt
			}
		}
	}

	rows, err := s.readDB.QueryRaw(ctx,
		`SELECT id, project_id, cidr, vlan_id, vrf, site_name, gateway, role, description, created_at
		 FROM ipam_prefixes WHERE project_id = ? ORDER BY cidr ASC`, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]IPAMPrefix, 0)
	for rows.Next() {
		var p IPAMPrefix
		var cAt string
		if err := rows.Scan(&p.ID, &p.ProjectID, &p.CIDR, &p.VLANID, &p.VRF, &p.SiteName, &p.Gateway, &p.Role, &p.Description, &cAt); err != nil {
			return nil, err
		}
		p.CreatedAt = parseTS(cAt)
		p.TotalUsable = usableHostsInCIDR(p.CIDR)
		p.UsedCount = usedCounts[p.ID]
		if p.TotalUsable > 0 {
			p.UtilPercent = math.Round((float64(p.UsedCount)/float64(p.TotalUsable))*1000) / 10
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Service) CreatePrefix(ctx context.Context, projectID string, p *IPAMPrefix) (*IPAMPrefix, error) {
	if err := s.EnsureDefaults(ctx, projectID); err != nil {
		return nil, err
	}
	parsed, err := netip.ParsePrefix(strings.TrimSpace(p.CIDR))
	if err != nil {
		return nil, invalidErr("非法 CIDR 网段格式，例如 10.20.10.0/24")
	}
	p.CIDR = parsed.Masked().String()
	if p.VRF == "" {
		p.VRF = "default"
	}
	if p.Role == "" {
		p.Role = "business"
	}
	if p.Gateway == "" && parsed.Addr().Is4() && parsed.Bits() <= 30 {
		p.Gateway = parsed.Masked().Addr().Next().String()
	}
	now := time.Now().UTC()
	p.ID = domain.NewID("pfx")
	p.ProjectID = projectID
	p.TotalUsable = usableHostsInCIDR(p.CIDR)
	p.CreatedAt = now
	_, err = s.writeDB.ExecRaw(ctx,
		`INSERT INTO ipam_prefixes (id, project_id, cidr, vlan_id, vrf, site_name, gateway, role, description, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, projectID, p.CIDR, p.VLANID, p.VRF, p.SiteName, p.Gateway, p.Role, p.Description, now.Format(time.RFC3339Nano),
	)
	if err != nil {
		return nil, conflictErr("网段已存在: " + err.Error())
	}
	return p, nil
}

func (s *Service) DeletePrefix(ctx context.Context, projectID, prefixID string) error {
	_, err := s.writeDB.ExecRaw(ctx, `DELETE FROM ipam_prefixes WHERE project_id = ? AND id = ?`, projectID, prefixID)
	return err
}

func (s *Service) ListIPAddresses(ctx context.Context, projectID, prefixID string) ([]IPAMAddress, error) {
	rows, err := s.readDB.QueryRaw(ctx,
		`SELECT id, project_id, prefix_id, ip_address, status, ci_id, host_id, hostname, mac_address, description, updated_at
		 FROM ipam_addresses WHERE project_id = ? AND prefix_id = ? ORDER BY ip_address ASC`, projectID, prefixID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]IPAMAddress, 0)
	for rows.Next() {
		var a IPAMAddress
		var uAt string
		if err := rows.Scan(&a.ID, &a.ProjectID, &a.PrefixID, &a.IPAddress, &a.Status, &a.CIID, &a.HostID, &a.Hostname, &a.MACAddress, &a.Description, &uAt); err != nil {
			return nil, err
		}
		a.UpdatedAt = parseTS(uAt)
		out = append(out, a)
	}
	return out, rows.Err()
}

// AllocateIP allocates either a specified IP inside the prefix or automatically finds the next available IP.
func (s *Service) AllocateIP(ctx context.Context, projectID, prefixID string, req *IPAMAddress) (*IPAMAddress, error) {
	var cidr string
	err := s.readDB.ScanRaw(ctx, `SELECT cidr FROM ipam_prefixes WHERE project_id = ? AND id = ?`, []any{projectID, prefixID}, &cidr)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFoundErr("ipam_prefix", prefixID)
	}
	if err != nil {
		return nil, err
	}
	pfx, err := netip.ParsePrefix(cidr)
	if err != nil {
		return nil, err
	}

	existing, err := s.ListIPAddresses(ctx, projectID, prefixID)
	if err != nil {
		return nil, err
	}
	taken := make(map[string]bool, len(existing))
	for _, e := range existing {
		taken[e.IPAddress] = true
	}

	ipStr := strings.TrimSpace(req.IPAddress)
	if ipStr == "" {
		// Find next available usable IP (skip network .0 and broadcast)
		cur := pfx.Masked().Addr().Next()
		found := ""
		for steps := 0; steps < 65536 && pfx.Contains(cur); steps++ {
			nextAddr := cur.Next()
			if pfx.Addr().Is4() && pfx.Bits() <= 30 && !pfx.Contains(nextAddr) {
				break // broadcast address
			}
			if !taken[cur.String()] {
				found = cur.String()
				break
			}
			cur = nextAddr
		}
		if found == "" {
			return nil, conflictErr("该 CIDR 网段已无可用空闲 IP 地址")
		}
		ipStr = found
	} else {
		addr, err := netip.ParseAddr(ipStr)
		if err != nil || !pfx.Contains(addr) {
			return nil, invalidErr(fmt.Sprintf("IP %q 不属于网段 %s", ipStr, cidr))
		}
		if taken[addr.String()] {
			return nil, conflictErr(fmt.Sprintf("IP %s 已被占用", addr.String()))
		}
		ipStr = addr.String()
	}

	if req.Status == "" {
		req.Status = "active"
	}
	now := time.Now().UTC()
	out := &IPAMAddress{
		ID:          domain.NewID("ip"),
		ProjectID:   projectID,
		PrefixID:    prefixID,
		IPAddress:   ipStr,
		Status:      req.Status,
		CIID:        req.CIID,
		HostID:      req.HostID,
		Hostname:    strings.TrimSpace(req.Hostname),
		MACAddress:  strings.TrimSpace(req.MACAddress),
		Description: strings.TrimSpace(req.Description),
		UpdatedAt:   now,
	}
	_, err = s.writeDB.ExecRaw(ctx,
		`INSERT INTO ipam_addresses (id, project_id, prefix_id, ip_address, status, ci_id, host_id, hostname, mac_address, description, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		out.ID, projectID, prefixID, out.IPAddress, out.Status, out.CIID, out.HostID, out.Hostname, out.MACAddress, out.Description, now.Format(time.RFC3339Nano),
	)
	if err != nil {
		return nil, conflictErr("分配 IP 失败: " + err.Error())
	}
	return out, nil
}

func (s *Service) ReleaseIP(ctx context.Context, projectID, ipID string) error {
	_, err := s.writeDB.ExecRaw(ctx, `DELETE FROM ipam_addresses WHERE project_id = ? AND id = ?`, projectID, ipID)
	return err
}
