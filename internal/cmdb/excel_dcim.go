package cmdb

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"net/netip"
	"strconv"
	"strings"

	"github.com/kinstall/kinstall/pkg/platform/id"
	"github.com/xuri/excelize/v2"
)

// =============================================================================
// Native Excel (.xlsx) Template Export, Data Export & Batch Import
// =============================================================================

func escapeExcelFormula(v string) string {
	if v == "" {
		return v
	}
	switch v[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + v
	default:
		return v
	}
}

func (s *Service) ExportModelTemplateXLSX(ctx context.Context, wsID, modelCode string) ([]byte, error) {
	attrs, err := s.ListModelAttrs(ctx, wsID, modelCode)
	if err != nil {
		return nil, err
	}
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	sheet := "CI_Import"
	_ = f.SetSheetName("Sheet1", sheet)

	headers := []string{"unique_key", "name", "env", "status", "owner", "tags"}
	labels := []string{"唯一标识(必填)", "资产名称(必填)", "环境(dev/sit/uat/beta/prod)", "状态(active/maintenance/retired)", "负责人", "标签(逗号分隔)"}
	if modelCode == "m_host" {
		headers = append(headers, "ssh_port", "ssh_user", "exec_mode")
		labels = append(labels, "SSH端口(默认22)", "SSH用户(默认root)", "执行模式(auto/agent/ssh)")
	}
	for _, a := range attrs {
		headers = append(headers, a.AttrKey)
		reqTag := ""
		if a.Required {
			reqTag = "*"
		}
		labels = append(labels, fmt.Sprintf("%s%s(%s)", a.Name, reqTag, a.FieldType))
	}

	headerStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "FFFFFF"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"1D4ED8"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})
	subStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Italic: true, Color: "475569"},
		Fill: excelize.Fill{Type: "pattern", Color: []string{"F1F5F9"}, Pattern: 1},
	})

	for i, h := range headers {
		col, _ := excelize.ColumnNumberToName(i + 1)
		_ = f.SetCellValue(sheet, col+"1", h)
		_ = f.SetCellValue(sheet, col+"2", labels[i])
		_ = f.SetCellStyle(sheet, col+"1", col+"1", headerStyle)
		_ = f.SetCellStyle(sheet, col+"2", col+"2", subStyle)
		_ = f.SetColWidth(sheet, col, col, 20)
	}

	// Add enum dropdown validation for env and enum attributes.
	envDV := excelize.NewDataValidation(true)
	envDV.Sqref = "C3:C500"
	_ = envDV.SetDropList([]string{"dev", "sit", "uat", "beta", "prod"})
	_ = f.AddDataValidation(sheet, envDV)

	baseCols := 6
	if modelCode == "m_host" {
		baseCols = 9
	}
	for idx, a := range attrs {
		if a.FieldType == "enum" && len(a.Options) > 0 {
			col, _ := excelize.ColumnNumberToName(baseCols + idx + 1)
			dv := excelize.NewDataValidation(true)
			dv.Sqref = fmt.Sprintf("%s3:%s500", col, col)
			_ = dv.SetDropList(a.Options)
			_ = f.AddDataValidation(sheet, dv)
		}
	}

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (s *Service) ExportModelCIsXLSX(ctx context.Context, wsID, modelCode string) ([]byte, error) {
	attrs, err := s.ListModelAttrs(ctx, wsID, modelCode)
	if err != nil {
		return nil, err
	}
	cis, err := s.ListCIs(ctx, wsID, modelCode, "", "")
	if err != nil {
		return nil, err
	}
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	sheet := "CI_Export"
	_ = f.SetSheetName("Sheet1", sheet)

	headers := []string{"unique_key", "name", "env", "status", "owner", "tags"}
	if modelCode == "m_host" {
		headers = append(headers, "ssh_port", "ssh_user", "exec_mode")
	}
	for _, a := range attrs {
		headers = append(headers, a.AttrKey)
	}
	for i, h := range headers {
		col, _ := excelize.ColumnNumberToName(i + 1)
		_ = f.SetCellValue(sheet, col+"1", h)
		_ = f.SetColWidth(sheet, col, col, 18)
	}

	for rIdx, ci := range cis {
		rowNum := rIdx + 2
		vals := []any{
			escapeExcelFormula(ci.UniqueKey),
			escapeExcelFormula(ci.Name),
			ci.Env,
			ci.Status,
			escapeExcelFormula(ci.Owner),
			escapeExcelFormula(strings.Join(ci.Tags, ",")),
		}
		if modelCode == "m_host" {
			port, user, mode := 22, "root", "auto"
			if ci.Host != nil {
				port, user, mode = ci.Host.SSHPort, ci.Host.SSHUser, ci.Host.ExecMode
			}
			vals = append(vals, port, user, mode)
		}
		for _, a := range attrs {
			if v, ok := ci.Data[a.AttrKey]; ok && v != nil {
				vals = append(vals, escapeExcelFormula(fmt.Sprint(v)))
			} else {
				vals = append(vals, "")
			}
		}
		for cIdx, v := range vals {
			col, _ := excelize.ColumnNumberToName(cIdx + 1)
			_ = f.SetCellValue(sheet, fmt.Sprintf("%s%d", col, rowNum), v)
		}
	}

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (s *Service) ImportModelCIsXLSX(ctx context.Context, wsID, actor, modelCode string, data []byte) (*ImportResult, error) {
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("无法解析 Excel (.xlsx) 文件: %w", err)
	}
	defer func() { _ = f.Close() }()
	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, errors.New("工作簿不含任何工作表")
	}
	rows, err := f.GetRows(sheets[0])
	if err != nil {
		return nil, err
	}
	if len(rows) < 2 {
		return nil, errors.New("表格表头或数据为空")
	}
	headers := make([]string, len(rows[0]))
	for i, h := range rows[0] {
		headers[i] = strings.ToLower(strings.TrimSpace(h))
	}

	res := &ImportResult{ModelCode: modelCode, Errors: make([]string, 0)}
	for rIdx := 1; rIdx < len(rows); rIdx++ {
		row := rows[rIdx]
		// Skip the descriptive template row 2 if present.
		if rIdx == 1 && len(row) > 0 && strings.Contains(row[0], "唯一标识") {
			continue
		}
		rowMap := make(map[string]string, len(headers))
		allEmpty := true
		for cIdx, h := range headers {
			if cIdx < len(row) {
				v := strings.TrimSpace(strings.TrimPrefix(row[cIdx], "'"))
				rowMap[h] = v
				if v != "" {
					allEmpty = false
				}
			}
		}
		if allEmpty {
			continue
		}

		req := UpsertCIRequest{
			ModelCode: modelCode,
			UniqueKey: rowMap["unique_key"],
			Name:      rowMap["name"],
			Env:       rowMap["env"],
			Status:    rowMap["status"],
			Owner:     rowMap["owner"],
			Tags:      splitNonEmpty(rowMap["tags"]),
			Data:      make(map[string]any),
		}
		for k, v := range rowMap {
			switch k {
			case "unique_key", "name", "env", "status", "owner", "tags", "ssh_port", "ssh_user", "exec_mode", "":
				continue
			default:
				if v != "" {
					req.Data[k] = v
				}
			}
		}
		if modelCode == "m_host" {
			port := 22
			if p, err := strconv.Atoi(rowMap["ssh_port"]); err == nil && p > 0 {
				port = p
			}
			ip := rowMap["ip"]
			if ip == "" {
				ip = req.UniqueKey
			}
			req.Host = &HostEndpoint{
				IP:       ip,
				SSHPort:  port,
				SSHUser:  defaultStr(rowMap["ssh_user"], "root"),
				ExecMode: defaultStr(rowMap["exec_mode"], "auto"),
			}
		}

		// Upsert: check if exists first to count Created vs Updated accurately.
		var existingID string
		qErr := s.store.QueryRow(ctx,
			`SELECT id FROM cmdb_ci_instances WHERE workspace_id = ? AND model_code = ? AND unique_key = ?`,
			wsID, modelCode, req.UniqueKey,
		).Scan(&existingID)
		if modelCode == "m_host" {
			if _, err := s.UpsertHostCI(ctx, wsID, actor, req); err != nil {
				res.Failed++
				res.Errors = append(res.Errors, fmt.Sprintf("第 %d 行: %v", rIdx+1, err))
				continue
			}
		} else if qErr == nil && existingID != "" {
			if _, err := s.UpdateCI(ctx, wsID, existingID, actor, req); err != nil {
				res.Failed++
				res.Errors = append(res.Errors, fmt.Sprintf("第 %d 行: %v", rIdx+1, err))
				continue
			}
		} else {
			if _, err := s.CreateCI(ctx, wsID, actor, req); err != nil {
				res.Failed++
				res.Errors = append(res.Errors, fmt.Sprintf("第 %d 行: %v", rIdx+1, err))
				continue
			}
		}
		if qErr == nil && existingID != "" {
			res.Updated++
		} else {
			res.Created++
		}
	}
	return res, nil
}

func splitNonEmpty(s string) []string {
	out := make([]string, 0)
	for _, p := range strings.Split(s, ",") {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func defaultStr(v, fb string) string {
	if strings.TrimSpace(v) == "" {
		return fb
	}
	return strings.TrimSpace(v)
}

// =============================================================================
// DCIM 42U Rack Elevation
// =============================================================================

func (s *Service) ListRacks(ctx context.Context, wsID string) ([]Rack, error) {
	rows, err := s.store.Query(ctx,
		`SELECT id, workspace_id, datacenter, room, name, u_height, power_kw, status, notes, created_at, updated_at
		 FROM dcim_racks WHERE workspace_id = ? ORDER BY datacenter ASC, name ASC`,
		wsID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]Rack, 0)
	for rows.Next() {
		var r Rack
		if err := rows.Scan(&r.ID, &r.WorkspaceID, &r.Datacenter, &r.Room, &r.Name, &r.UHeight, &r.PowerKW, &r.Status, &r.Notes, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		units, err := s.ListRackUnits(ctx, wsID, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].Units = units
		for _, u := range units {
			out[i].UsedU += u.USize
			out[i].UsedPowerW += u.PowerW
		}
	}
	return out, nil
}

func (s *Service) CreateRack(ctx context.Context, wsID, dc, room, name string, uHeight int, powerKW float64, notes string) (*Rack, error) {
	if strings.TrimSpace(dc) == "" || strings.TrimSpace(name) == "" {
		return nil, errors.New("机房与机柜名称不能为空")
	}
	if uHeight <= 0 || uHeight > 60 {
		uHeight = 42
	}
	if powerKW <= 0 {
		powerKW = 8.0
	}
	now := nowISO()
	r := &Rack{
		ID:          id.New("rack"),
		WorkspaceID: wsID,
		Datacenter:  strings.TrimSpace(dc),
		Room:        strings.TrimSpace(room),
		Name:        strings.TrimSpace(name),
		UHeight:     uHeight,
		PowerKW:     powerKW,
		Status:      "active",
		Notes:       notes,
		Units:       make([]RackUnit, 0),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	_, err := s.store.Exec(ctx,
		`INSERT INTO dcim_racks(id, workspace_id, datacenter, room, name, u_height, power_kw, status, notes, created_at, updated_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, wsID, r.Datacenter, r.Room, r.Name, r.UHeight, r.PowerKW, r.Status, r.Notes, now, now,
	)
	if err != nil {
		return nil, err
	}
	return r, nil
}

func (s *Service) DeleteRack(ctx context.Context, wsID, rackID string) error {
	_, err := s.store.Exec(ctx, `DELETE FROM dcim_racks WHERE workspace_id = ? AND id = ?`, wsID, rackID)
	return err
}

func (s *Service) ListRackUnits(ctx context.Context, wsID, rackID string) ([]RackUnit, error) {
	rows, err := s.store.Query(ctx,
		`SELECT id, workspace_id, rack_id, ci_id, device_name, model_code, u_start, u_size, face, power_w, color_tag, created_at
		 FROM dcim_rack_units WHERE workspace_id = ? AND rack_id = ? ORDER BY u_start DESC`,
		wsID, rackID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]RackUnit, 0)
	for rows.Next() {
		var u RackUnit
		if err := rows.Scan(&u.ID, &u.WorkspaceID, &u.RackID, &u.CIID, &u.DeviceName, &u.ModelCode, &u.UStart, &u.USize, &u.Face, &u.PowerW, &u.ColorTag, &u.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Service) MountRackUnit(ctx context.Context, wsID, rackID, ciID, deviceName, modelCode string, uStart, uSize int, face string, powerW int, colorTag string) (*RackUnit, error) {
	if deviceName == "" || uStart < 1 || uSize < 1 {
		return nil, errors.New("设备名称、起始 U 位和占用高度必须合法")
	}
	if face != "rear" {
		face = "front"
	}
	if colorTag == "" {
		colorTag = "primary"
	}
	var rackHeight int
	if err := s.store.QueryRow(ctx, `SELECT u_height FROM dcim_racks WHERE workspace_id = ? AND id = ?`, wsID, rackID).Scan(&rackHeight); err != nil {
		return nil, errors.New("机柜不存在")
	}
	uEnd := uStart + uSize - 1
	if uEnd > rackHeight {
		return nil, fmt.Errorf("机柜 U 位 %dU-%dU 超出最大高度 %dU", uStart, uEnd, rackHeight)
	}

	// Enforce strict U-slot non-overlap on the same rack face.
	existing, err := s.ListRackUnits(ctx, wsID, rackID)
	if err != nil {
		return nil, err
	}
	for _, ex := range existing {
		if ex.Face != face {
			continue
		}
		exEnd := ex.UStart + ex.USize - 1
		if uStart <= exEnd && ex.UStart <= uEnd {
			return nil, fmt.Errorf("机柜 U 位冲突: %dU-%dU 与已上架设备 %q (%dU-%dU) 重叠", uStart, uEnd, ex.DeviceName, ex.UStart, exEnd)
		}
	}

	u := &RackUnit{
		ID:          id.New("unit"),
		WorkspaceID: wsID,
		RackID:      rackID,
		CIID:        ciID,
		DeviceName:  deviceName,
		ModelCode:   defaultStr(modelCode, "m_host"),
		UStart:      uStart,
		USize:       uSize,
		Face:        face,
		PowerW:      powerW,
		ColorTag:    colorTag,
		CreatedAt:   nowISO(),
	}
	_, err = s.store.Exec(ctx,
		`INSERT INTO dcim_rack_units(id, workspace_id, rack_id, ci_id, device_name, model_code, u_start, u_size, face, power_w, color_tag, created_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		u.ID, wsID, u.RackID, u.CIID, u.DeviceName, u.ModelCode, u.UStart, u.USize, u.Face, u.PowerW, u.ColorTag, u.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (s *Service) UnmountRackUnit(ctx context.Context, wsID, unitID string) error {
	_, err := s.store.Exec(ctx, `DELETE FROM dcim_rack_units WHERE workspace_id = ? AND id = ?`, wsID, unitID)
	return err
}

// =============================================================================
// IPAM CIDR Prefix & Auto-Allocator
// =============================================================================

func (s *Service) ListPrefixes(ctx context.Context, wsID string) ([]IPAMPrefix, error) {
	rows, err := s.store.Query(ctx,
		`SELECT p.id, p.workspace_id, p.cidr, p.vlan_id, p.site, p.vrf, p.gateway, p.description, p.status, p.created_at, p.updated_at,
		        (SELECT COUNT(1) FROM ipam_addresses a WHERE a.prefix_id = p.id) AS used
		 FROM ipam_prefixes p WHERE p.workspace_id = ? ORDER BY p.cidr ASC`,
		wsID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]IPAMPrefix, 0)
	for rows.Next() {
		var p IPAMPrefix
		if err := rows.Scan(&p.ID, &p.WorkspaceID, &p.CIDR, &p.VLANID, &p.Site, &p.VRF, &p.Gateway, &p.Description, &p.Status, &p.CreatedAt, &p.UpdatedAt, &p.UsedIPs); err != nil {
			return nil, err
		}
		p.TotalIPs = usableHosts(p.CIDR)
		if p.TotalIPs > 0 {
			p.UtilPct = math.Round(float64(p.UsedIPs)*1000/float64(p.TotalIPs)) / 10
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func usableHosts(cidr string) int {
	pfx, err := netip.ParsePrefix(cidr)
	if err != nil || !pfx.Addr().Is4() {
		return 0
	}
	bits := pfx.Bits()
	if bits >= 31 {
		return 1 << (32 - bits)
	}
	if bits < 16 {
		return 65534
	}
	return (1 << (32 - bits)) - 2
}

func (s *Service) CreatePrefix(ctx context.Context, wsID, cidr string, vlanID int, site, vrf, gateway, desc string) (*IPAMPrefix, error) {
	pfx, err := netip.ParsePrefix(strings.TrimSpace(cidr))
	if err != nil || !pfx.Addr().Is4() {
		return nil, fmt.Errorf("无效的 IPv4 CIDR 网段: %q", cidr)
	}
	norm := pfx.Masked().String()
	now := nowISO()
	p := &IPAMPrefix{
		ID:          id.New("pfx"),
		WorkspaceID: wsID,
		CIDR:        norm,
		VLANID:      vlanID,
		Site:        site,
		VRF:         defaultStr(vrf, "default"),
		Gateway:     gateway,
		Description: desc,
		Status:      "active",
		TotalIPs:    usableHosts(norm),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	_, err = s.store.Exec(ctx,
		`INSERT INTO ipam_prefixes(id, workspace_id, cidr, vlan_id, site, vrf, gateway, description, status, created_at, updated_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		p.ID, wsID, p.CIDR, p.VLANID, p.Site, p.VRF, p.Gateway, p.Description, p.Status, now, now,
	)
	if err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Service) DeletePrefix(ctx context.Context, wsID, prefixID string) error {
	_, err := s.store.Exec(ctx, `DELETE FROM ipam_prefixes WHERE workspace_id = ? AND id = ?`, wsID, prefixID)
	return err
}

func (s *Service) ListAddresses(ctx context.Context, wsID, prefixID string) ([]IPAMAddress, error) {
	q := `SELECT id, workspace_id, prefix_id, ip, status, ci_id, dns_name, mac_addr, owner, description, created_at, updated_at
	      FROM ipam_addresses WHERE workspace_id = ?`
	args := []any{wsID}
	if prefixID != "" {
		q += ` AND prefix_id = ?`
		args = append(args, prefixID)
	}
	q += ` ORDER BY ip ASC`
	rows, err := s.store.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]IPAMAddress, 0)
	for rows.Next() {
		var a IPAMAddress
		if err := rows.Scan(&a.ID, &a.WorkspaceID, &a.PrefixID, &a.IP, &a.Status, &a.CIID, &a.DNSName, &a.MACAddr, &a.Owner, &a.Description, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// AllocateIP allocates a specific IP inside prefixID, or automatically selects the next available IP when ip == "".
func (s *Service) AllocateIP(ctx context.Context, wsID, prefixID, ipStr, status, ciID, dnsName, owner, desc string) (*IPAMAddress, error) {
	var cidr string
	if err := s.store.QueryRow(ctx, `SELECT cidr FROM ipam_prefixes WHERE workspace_id = ? AND id = ?`, wsID, prefixID).Scan(&cidr); err != nil {
		return nil, errors.New("所属 CIDR 网段不存在")
	}
	pfx, err := netip.ParsePrefix(cidr)
	if err != nil {
		return nil, err
	}
	pfx = pfx.Masked()

	existing, err := s.ListAddresses(ctx, wsID, prefixID)
	if err != nil {
		return nil, err
	}
	used := make(map[string]bool, len(existing))
	for _, a := range existing {
		used[a.IP] = true
	}

	targetIP := strings.TrimSpace(ipStr)
	if targetIP == "" {
		// Find first free usable host address.
		curr := pfx.Addr().Next()
		for pfx.Contains(curr) {
			next := curr.Next()
			if pfx.Bits() <= 30 && !pfx.Contains(next) {
				break // skip broadcast
			}
			if !used[curr.String()] {
				targetIP = curr.String()
				break
			}
			curr = next
		}
		if targetIP == "" {
			return nil, fmt.Errorf("网段 %s 已无可用空闲 IP", cidr)
		}
	} else {
		addr, perr := netip.ParseAddr(targetIP)
		if perr != nil || !pfx.Contains(addr) {
			return nil, fmt.Errorf("IP %q 不属于网段 %s", targetIP, cidr)
		}
		if used[targetIP] {
			return nil, fmt.Errorf("IP %q 已被占用", targetIP)
		}
	}

	now := nowISO()
	a := &IPAMAddress{
		ID:          id.New("ip"),
		WorkspaceID: wsID,
		PrefixID:    prefixID,
		IP:          targetIP,
		Status:      defaultStr(status, "active"),
		CIID:        ciID,
		DNSName:     dnsName,
		Owner:       owner,
		Description: desc,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	_, err = s.store.Exec(ctx,
		`INSERT INTO ipam_addresses(id, workspace_id, prefix_id, ip, status, ci_id, dns_name, mac_addr, owner, description, created_at, updated_at)
		 VALUES(?,?,?,?,?,?,?,'',?,?,?,?)`,
		a.ID, wsID, a.PrefixID, a.IP, a.Status, a.CIID, a.DNSName, a.Owner, a.Description, now, now,
	)
	if err != nil {
		return nil, err
	}
	return a, nil
}

func (s *Service) ReleaseIP(ctx context.Context, wsID, addrID string) error {
	_, err := s.store.Exec(ctx, `DELETE FROM ipam_addresses WHERE workspace_id = ? AND id = ?`, wsID, addrID)
	return err
}
