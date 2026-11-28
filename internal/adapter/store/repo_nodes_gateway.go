package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/pkg/platform/crypto"
	"github.com/kinstall/kinstall/pkg/platform/db"
	"github.com/kinstall/kinstall/pkg/platform/id"
)

// ============================================================================
// Zero-Trust Nodes, Host Groups & Chunked Distribution Checkpoints
// ============================================================================

func (r *Repo) IssueEnrollToken(ctx context.Context, scope domain.Scope, labels map[string]string, ttl time.Duration, actorID string) (domain.EnrollToken, error) {
	if ttl <= 0 {
		ttl = domain.NodeEnrollTokenDefault
	}
	raw, err := crypto.RandomToken(32)
	if err != nil {
		return domain.EnrollToken{}, err
	}
	rawToken := "lo_enr_" + raw
	hash := crypto.SHA256Hex([]byte(rawToken))
	now := r.Now()
	exp := now.Add(ttl)
	tid := id.New("enr")

	tok := domain.EnrollToken{
		TenantID: scope.TenantID, AppID: scope.AppID, TokenID: tid, RawToken: rawToken,
		TokenHash: hash, Labels: labels, ExpiresAt: exp, CreatedBy: actorID, CreatedAt: now,
	}
	_, err = r.db.Exec(ctx,
		`INSERT INTO nodes_enroll_tokens (tenant_id, app_id, token_id, token_hash, labels_json, expires_at, used_at, used_by_node_id, created_by, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, '', '', ?, ?)`,
		scope.TenantID, scope.AppID, tid, hash, marshalJSON(labels), formatTime(exp), actorID, formatTime(now),
	)
	return tok, err
}

// EnrollNode consumes a one-time enroll token atomically and issues a dedicated per-node credential.
func (r *Repo) EnrollNode(ctx context.Context, rawEnrollToken, hostname, ipAddr, osArch, agentVer string) (domain.Node, string, error) {
	now := r.Now()
	nowStr := formatTime(now)
	tokenHash := crypto.SHA256Hex([]byte(rawEnrollToken))
	rawCred, err := crypto.RandomToken(32)
	if err != nil {
		return domain.Node{}, "", err
	}
	nodeCred := "lo_node_" + rawCred
	credHash := crypto.SHA256Hex([]byte(nodeCred))
	if osArch == "" {
		osArch = "linux/amd64"
	}
	if agentVer == "" {
		agentVer = "1.0.0"
	}

	var node domain.Node
	err = r.db.WithTx(ctx, func(ctx context.Context, tx *db.Tx) error {
		var tid, aid, tokID, labelsRaw, expStr, usedAt string
		if err := tx.QueryRow(ctx,
			`SELECT tenant_id, app_id, token_id, labels_json, expires_at, used_at FROM nodes_enroll_tokens WHERE token_hash=?`,
			tokenHash,
		).Scan(&tid, &aid, &tokID, &labelsRaw, &expStr, &usedAt); err != nil {
			return domain.NewError(domain.ErrUnauthorized, "INVALID_ENROLL_TOKEN", "注册令牌无效", nil)
		}
		if usedAt != "" {
			return domain.NewError(domain.ErrStateConflict, "ENROLL_TOKEN_USED", "注册令牌已被使用（严格一次性）", nil)
		}
		if now.After(parseTime(expStr)) {
			return domain.NewError(domain.ErrUnauthorized, "ENROLL_TOKEN_EXPIRED", "注册令牌已过期", nil)
		}

		nodeID := id.New("nod")
		res, err := tx.Exec(ctx,
			`UPDATE nodes_enroll_tokens SET used_at=?, used_by_node_id=? WHERE tenant_id=? AND app_id=? AND token_id=? AND used_at=''`,
			nowStr, nodeID, tid, aid, tokID,
		)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return domain.NewError(domain.ErrStateConflict, "ENROLL_TOKEN_USED", "注册令牌并发消费冲突", nil)
		}

		if _, err := tx.Exec(ctx,
			`INSERT INTO nodes_registry (tenant_id, app_id, node_id, hostname, ip_address, os_arch, agent_version, status, labels_json, token_hash, prev_token_hash, prev_expires_at, cred_version, last_seen_at, enrolled_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, 'ONLINE', ?, ?, '', '', 1, ?, ?, ?)`,
			tid, aid, nodeID, hostname, ipAddr, osArch, agentVer, labelsRaw, credHash, nowStr, nowStr, nowStr,
		); err != nil {
			return err
		}

		labels := make(map[string]string)
		unmarshalJSON(labelsRaw, &labels)
		node = domain.Node{
			TenantID: tid, AppID: aid, NodeID: nodeID, Hostname: hostname, IPAddress: ipAddr,
			OSArch: osArch, AgentVersion: agentVer, Status: domain.NodeOnline, Labels: labels,
			CredVersion: 1, LastSeenAt: now, EnrolledAt: now, UpdatedAt: now,
		}
		return nil
	})
	return node, nodeCred, err
}

// AuthenticateNode accepts either the active node credential OR the previous credential within its overlapping grace window!
func (r *Repo) AuthenticateNode(ctx context.Context, nodeID, rawCred string) (domain.Node, error) {
	hash := crypto.SHA256Hex([]byte(rawCred))
	var n domain.Node
	var st, labelsRaw, tokHash, prevHash, prevExpStr, ls, ea, ua string
	err := r.db.QueryRow(ctx,
		`SELECT tenant_id, app_id, node_id, hostname, ip_address, os_arch, agent_version, status, labels_json, token_hash, prev_token_hash, prev_expires_at, cred_version, last_seen_at, enrolled_at, updated_at
		 FROM nodes_registry WHERE node_id=?`,
		nodeID,
	).Scan(&n.TenantID, &n.AppID, &n.NodeID, &n.Hostname, &n.IPAddress, &n.OSArch, &n.AgentVersion, &st, &labelsRaw, &tokHash, &prevHash, &prevExpStr, &n.CredVersion, &ls, &ea, &ua)
	if err != nil {
		return domain.Node{}, domain.ErrUnauthorized
	}
	n.Status = domain.NodeStatus(st)
	if n.Status == domain.NodeRevoked {
		return domain.Node{}, domain.NewError(domain.ErrForbidden, "NODE_REVOKED", "节点凭据已吊销", nil)
	}
	valid := hash == tokHash
	if !valid && prevHash != "" && hash == prevHash {
		if exp := parseTime(prevExpStr); !exp.IsZero() && r.Now().Before(exp) {
			valid = true
		}
	}
	if !valid {
		return domain.Node{}, domain.ErrUnauthorized
	}
	n.Labels = make(map[string]string)
	unmarshalJSON(labelsRaw, &n.Labels)
	n.PrevExpiresAt = parseTimePtr(prevExpStr)
	n.LastSeenAt, n.EnrolledAt, n.UpdatedAt = parseTime(ls), parseTime(ea), parseTime(ua)
	return n, nil
}

func (r *Repo) RecordNodeHeartbeat(ctx context.Context, scope domain.Scope, nodeID, ipAddr, agentVer string) (domain.Node, error) {
	now := r.Now()
	nowStr := formatTime(now)
	err := r.db.WithScopeTx(ctx, dbScope(scope), func(ctx context.Context, tx *db.Tx) error {
		var curStatus string
		if err := tx.QueryRow(ctx,
			`SELECT status FROM nodes_registry WHERE tenant_id=? AND app_id=? AND node_id=?`,
			scope.TenantID, scope.AppID, nodeID,
		).Scan(&curStatus); err != nil {
			return mapNotFound(err, "node not found")
		}
		if curStatus == string(domain.NodeRevoked) {
			return domain.NewError(domain.ErrForbidden, "NODE_REVOKED", "节点已吊销", nil)
		}
		nextStatus := curStatus
		if curStatus != string(domain.NodeIsolated) {
			nextStatus = string(domain.NodeOnline)
		}
		_, err := tx.Exec(ctx,
			`UPDATE nodes_registry SET status=?, ip_address=CASE WHEN ?<>'' THEN ? ELSE ip_address END, agent_version=CASE WHEN ?<>'' THEN ? ELSE agent_version END, last_seen_at=?, updated_at=?
			 WHERE tenant_id=? AND app_id=? AND node_id=?`,
			nextStatus, ipAddr, ipAddr, agentVer, agentVer, nowStr, nowStr, scope.TenantID, scope.AppID, nodeID,
		)
		return err
	})
	if err != nil {
		return domain.Node{}, err
	}
	return r.GetNode(ctx, scope, nodeID)
}

// RotateNodeCredential performs zero-downtime dual-credential rotation: current token moves to prev_token_hash for graceWindow.
func (r *Repo) RotateNodeCredential(ctx context.Context, scope domain.Scope, nodeID string, graceWindow time.Duration) (domain.Node, string, error) {
	if graceWindow <= 0 {
		graceWindow = domain.NodeCredGraceWindow
	}
	raw, err := crypto.RandomToken(32)
	if err != nil {
		return domain.Node{}, "", err
	}
	newCred := "lo_node_" + raw
	newHash := crypto.SHA256Hex([]byte(newCred))
	now := r.Now()
	nowStr := formatTime(now)
	graceStr := formatTime(now.Add(graceWindow))

	err = r.db.WithScopeTx(ctx, dbScope(scope), func(ctx context.Context, tx *db.Tx) error {
		res, err := tx.Exec(ctx,
			`UPDATE nodes_registry
			 SET prev_token_hash=token_hash, prev_expires_at=?, token_hash=?, cred_version=cred_version+1, updated_at=?
			 WHERE tenant_id=? AND app_id=? AND node_id=? AND status<>'REVOKED'`,
			graceStr, newHash, nowStr, scope.TenantID, scope.AppID, nodeID,
		)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return domain.NotFoundError("node not found or already revoked")
		}
		return nil
	})
	if err != nil {
		return domain.Node{}, "", err
	}
	node, err := r.GetNode(ctx, scope, nodeID)
	return node, newCred, err
}

func (r *Repo) SetNodeStatus(ctx context.Context, scope domain.Scope, nodeID string, status domain.NodeStatus) (domain.Node, error) {
	nowStr := r.NowStr()
	var err error
	if status == domain.NodeRevoked {
		_, err = r.db.Exec(ctx,
			`UPDATE nodes_registry SET status='REVOKED', prev_token_hash='', prev_expires_at='', updated_at=?
			 WHERE tenant_id=? AND app_id=? AND node_id=?`,
			nowStr, scope.TenantID, scope.AppID, nodeID,
		)
	} else {
		_, err = r.db.Exec(ctx,
			`UPDATE nodes_registry SET status=?, updated_at=? WHERE tenant_id=? AND app_id=? AND node_id=?`,
			string(status), nowStr, scope.TenantID, scope.AppID, nodeID,
		)
	}
	if err != nil {
		return domain.Node{}, err
	}
	return r.GetNode(ctx, scope, nodeID)
}

type sweepNodeRec struct {
	tid, aid, nid string
	st            domain.NodeStatus
	lastSeen      time.Time
}

func (r *Repo) listSweepableNodes(ctx context.Context) ([]sweepNodeRec, error) {
	rows, err := r.db.Query(ctx, `SELECT tenant_id, app_id, node_id, status, last_seen_at FROM nodes_registry WHERE status IN ('ONLINE','DEGRADED')`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var list []sweepNodeRec
	for rows.Next() {
		var nr sweepNodeRec
		var st, ls string
		if err := rows.Scan(&nr.tid, &nr.aid, &nr.nid, &st, &ls); err != nil {
			return nil, err
		}
		nr.st = domain.NodeStatus(st)
		nr.lastSeen = parseTime(ls)
		list = append(list, nr)
	}
	return list, rows.Err()
}

func (r *Repo) SweepNodeHealth(ctx context.Context) (int, error) {
	now := r.Now()
	nowStr := formatTime(now)
	list, err := r.listSweepableNodes(ctx)
	if err != nil {
		return 0, err
	}

	updated := 0
	for _, nr := range list {
		next := domain.EvaluateNodeHealth(nr.st, nr.lastSeen, now)
		if next != nr.st {
			if _, err := r.db.Exec(ctx,
				`UPDATE nodes_registry SET status=?, updated_at=? WHERE tenant_id=? AND app_id=? AND node_id=?`,
				string(next), nowStr, nr.tid, nr.aid, nr.nid,
			); err == nil {
				updated++
			}
		}
	}
	return updated, nil
}

func (r *Repo) GetNode(ctx context.Context, scope domain.Scope, nodeID string) (domain.Node, error) {
	var n domain.Node
	var st, labelsRaw, prevExpStr, ls, ea, ua string
	err := r.db.QueryRow(ctx,
		`SELECT tenant_id, app_id, node_id, hostname, ip_address, os_arch, agent_version, status, labels_json, prev_expires_at, cred_version, last_seen_at, enrolled_at, updated_at
		 FROM nodes_registry WHERE tenant_id=? AND app_id=? AND node_id=?`,
		scope.TenantID, scope.AppID, nodeID,
	).Scan(&n.TenantID, &n.AppID, &n.NodeID, &n.Hostname, &n.IPAddress, &n.OSArch, &n.AgentVersion, &st, &labelsRaw, &prevExpStr, &n.CredVersion, &ls, &ea, &ua)
	if err != nil {
		return domain.Node{}, mapNotFound(err, "node not found")
	}
	n.Status = domain.NodeStatus(st)
	n.Labels = make(map[string]string)
	unmarshalJSON(labelsRaw, &n.Labels)
	n.PrevExpiresAt = parseTimePtr(prevExpStr)
	n.LastSeenAt, n.EnrolledAt, n.UpdatedAt = parseTime(ls), parseTime(ea), parseTime(ua)
	return n, nil
}

func (r *Repo) ListNodes(ctx context.Context, scope domain.Scope) ([]domain.Node, error) {
	q := `SELECT tenant_id, app_id, node_id, hostname, ip_address, os_arch, agent_version, status, labels_json, prev_expires_at, cred_version, last_seen_at, enrolled_at, updated_at
	      FROM nodes_registry`
	var args []any
	if scope.TenantID != "" && scope.AppID != "" {
		q += ` WHERE tenant_id=? AND app_id=?`
		args = append(args, scope.TenantID, scope.AppID)
	} else if scope.TenantID != "" {
		q += ` WHERE tenant_id=?`
		args = append(args, scope.TenantID)
	}
	q += ` ORDER BY enrolled_at DESC`
	rows, err := r.db.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Node
	for rows.Next() {
		var n domain.Node
		var st, labelsRaw, prevExpStr, ls, ea, ua string
		if err := rows.Scan(&n.TenantID, &n.AppID, &n.NodeID, &n.Hostname, &n.IPAddress, &n.OSArch, &n.AgentVersion, &st, &labelsRaw, &prevExpStr, &n.CredVersion, &ls, &ea, &ua); err != nil {
			return nil, err
		}
		n.Status = domain.NodeStatus(st)
		n.Labels = make(map[string]string)
		unmarshalJSON(labelsRaw, &n.Labels)
		n.PrevExpiresAt = parseTimePtr(prevExpStr)
		n.LastSeenAt, n.EnrolledAt, n.UpdatedAt = parseTime(ls), parseTime(ea), parseTime(ua)
		out = append(out, n)
	}
	return out, rows.Err()
}

func (r *Repo) CreateDistribution(ctx context.Context, scope domain.Scope, artifactFileID, targetNodeID, sha256Hex string, totalBytes int64) (domain.Distribution, error) {
	now := r.Now()
	nowStr := formatTime(now)
	did := id.New("dist")
	d := domain.Distribution{
		TenantID: scope.TenantID, AppID: scope.AppID, DistID: did, ArtifactFileID: artifactFileID,
		TargetNodeID: targetNodeID, TotalBytes: totalBytes, SHA256: sha256Hex, State: "PENDING",
		CreatedAt: now, UpdatedAt: now,
	}
	_, err := r.db.Exec(ctx,
		`INSERT INTO nodes_distributions (tenant_id, app_id, dist_id, artifact_file_id, target_node_id, total_bytes, verified_bytes, sha256, state, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, 0, ?, 'PENDING', ?, ?)`,
		scope.TenantID, scope.AppID, did, artifactFileID, targetNodeID, totalBytes, sha256Hex, nowStr, nowStr,
	)
	return d, err
}

// AckDistributionChunk is strictly idempotent per chunk_index: verified_bytes never double-counts!
func (r *Repo) AckDistributionChunk(ctx context.Context, scope domain.Scope, distID string, chunkIndex int, offset, chunkBytes int64, chunkSHA256 string) (domain.Distribution, error) {
	nowStr := r.NowStr()
	var out domain.Distribution
	err := r.db.WithScopeTx(ctx, dbScope(scope), func(ctx context.Context, tx *db.Tx) error {
		var totalBytes, verifiedBytes int64
		var state, artID, targetID, sha, ca string
		if err := tx.QueryRow(ctx,
			`SELECT artifact_file_id, target_node_id, total_bytes, verified_bytes, sha256, state, created_at
			 FROM nodes_distributions WHERE tenant_id=? AND app_id=? AND dist_id=?`,
			scope.TenantID, scope.AppID, distID,
		).Scan(&artID, &targetID, &totalBytes, &verifiedBytes, &sha, &state, &ca); err != nil {
			return mapNotFound(err, "distribution not found")
		}

		res, err := tx.Exec(ctx,
			`INSERT INTO nodes_distribution_checkpoints (tenant_id, app_id, dist_id, chunk_index, chunk_offset, chunk_bytes, chunk_sha256, acked_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT(tenant_id, app_id, dist_id, chunk_index) DO NOTHING`,
			scope.TenantID, scope.AppID, distID, chunkIndex, offset, chunkBytes, chunkSHA256, nowStr,
		)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			verifiedBytes += chunkBytes
			if verifiedBytes > totalBytes {
				verifiedBytes = totalBytes
			}
			if verifiedBytes >= totalBytes {
				state = "VERIFIED"
			} else {
				state = "STREAMING"
			}
			if _, err := tx.Exec(ctx,
				`UPDATE nodes_distributions SET verified_bytes=?, state=?, updated_at=? WHERE tenant_id=? AND app_id=? AND dist_id=?`,
				verifiedBytes, state, nowStr, scope.TenantID, scope.AppID, distID,
			); err != nil {
				return err
			}
		}
		out = domain.Distribution{
			TenantID: scope.TenantID, AppID: scope.AppID, DistID: distID, ArtifactFileID: artID,
			TargetNodeID: targetID, TotalBytes: totalBytes, VerifiedBytes: verifiedBytes, SHA256: sha,
			State: state, CreatedAt: parseTime(ca), UpdatedAt: r.Now(),
		}
		return nil
	})
	return out, err
}

// ============================================================================
// Dynamic Gateway Routes & Atomic Default Route Switch (LiveOps G01, G02)
// ============================================================================

func (r *Repo) ListGatewayRouters(ctx context.Context, scope domain.Scope) ([]domain.GatewayRouter, int64, error) {
	var routeVer int64 = 1
	_ = r.db.QueryRow(ctx, `SELECT route_version FROM gw_revisions WHERE tenant_id=? AND app_id=?`, scope.TenantID, scope.AppID).Scan(&routeVer)

	rows, err := r.db.Query(ctx,
		`SELECT tenant_id, app_id, router_id, name, listen_host, rate_limit_qps, burst, status, created_at, updated_at
		 FROM gw_routers WHERE tenant_id=? AND app_id=? ORDER BY created_at ASC`,
		scope.TenantID, scope.AppID,
	)
	if err != nil {
		return nil, routeVer, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.GatewayRouter
	for rows.Next() {
		var rt domain.GatewayRouter
		var ca, ua string
		if err := rows.Scan(&rt.TenantID, &rt.AppID, &rt.RouterID, &rt.Name, &rt.ListenHost, &rt.RateLimitQPS, &rt.Burst, &rt.Status, &ca, &ua); err != nil {
			return nil, routeVer, err
		}
		rt.RouteVersion = routeVer
		rt.CreatedAt, rt.UpdatedAt = parseTime(ca), parseTime(ua)
		out = append(out, rt)
	}
	return out, routeVer, rows.Err()
}

func (r *Repo) CreateGatewayRouter(ctx context.Context, scope domain.Scope, name, listenHost string, qps, burst int) (domain.GatewayRouter, error) {
	now := r.Now()
	nowStr := formatTime(now)
	if listenHost == "" {
		listenHost = "*"
	}
	if qps <= 0 {
		qps = 1000
	}
	if burst <= 0 {
		burst = 200
	}
	rid := id.New("rtr")
	rt := domain.GatewayRouter{
		TenantID: scope.TenantID, AppID: scope.AppID, RouterID: rid, Name: name, ListenHost: listenHost,
		RateLimitQPS: qps, Burst: burst, Status: "ACTIVE", CreatedAt: now, UpdatedAt: now,
	}
	_, err := r.db.Exec(ctx,
		`INSERT INTO gw_routers (tenant_id, app_id, router_id, name, listen_host, rate_limit_qps, burst, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, 'ACTIVE', ?, ?)`,
		scope.TenantID, scope.AppID, rid, name, listenHost, qps, burst, nowStr, nowStr,
	)
	if isUniqueViolation(err) {
		return domain.GatewayRouter{}, domain.NewError(domain.ErrAlreadyExists, "ROUTER_EXISTS", "网关路由器名称已存在", nil)
	}
	return rt, err
}

func (r *Repo) ListGatewayRoutes(ctx context.Context, scope domain.Scope, routerID string) ([]domain.GatewayRoute, int64, error) {
	var routeVer int64 = 1
	_ = r.db.QueryRow(ctx, `SELECT route_version FROM gw_revisions WHERE tenant_id=? AND app_id=?`, scope.TenantID, scope.AppID).Scan(&routeVer)

	q := `SELECT tenant_id, app_id, route_id, router_id, name, path_prefix, upstream_url, is_default, strip_prefix, timeout_ms, rate_limit_qps, status, version, created_at, updated_at
	      FROM gw_routes WHERE tenant_id=? AND app_id=?`
	args := []any{scope.TenantID, scope.AppID}
	if routerID != "" {
		q += ` AND router_id=?`
		args = append(args, routerID)
	}
	q += ` ORDER BY is_default DESC, created_at ASC`
	rows, err := r.db.Query(ctx, q, args...)
	if err != nil {
		return nil, routeVer, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.GatewayRoute
	for rows.Next() {
		var rt domain.GatewayRoute
		var isDef, strip int
		var ca, ua string
		if err := rows.Scan(&rt.TenantID, &rt.AppID, &rt.RouteID, &rt.RouterID, &rt.Name, &rt.PathPrefix, &rt.UpstreamURL, &isDef, &strip, &rt.TimeoutMs, &rt.RateLimitQPS, &rt.Status, &rt.Version, &ca, &ua); err != nil {
			return nil, routeVer, err
		}
		rt.IsDefault = isDef != 0
		rt.StripPrefix = strip != 0
		rt.RouteVersion = routeVer
		rt.CreatedAt, rt.UpdatedAt = parseTime(ca), parseTime(ua)
		out = append(out, rt)
	}
	return out, routeVer, rows.Err()
}

// CreateGatewayRoute atomically creates a route and bumps gw_revisions.route_version; if isDefault=true, swaps default atomically (G01/G02).
func (r *Repo) CreateGatewayRoute(ctx context.Context, scope domain.Scope, routerID, name, pathPrefix, upstreamURL string, isDefault, stripPrefix bool, timeoutMs, qps int) (domain.GatewayRoute, error) {
	now := r.Now()
	nowStr := formatTime(now)
	if timeoutMs <= 0 {
		timeoutMs = 30000
	}
	rid := id.New("rt")
	var routeVer int64
	err := r.db.WithScopeTx(ctx, dbScope(scope), func(ctx context.Context, tx *db.Tx) error {
		if _, err := tx.TryAdvisoryLock(ctx, "gw:"+scope.TenantID+":"+scope.AppID); err != nil {
			return err
		}
		if isDefault {
			if _, err := tx.Exec(ctx,
				`UPDATE gw_routes SET is_default=0, version=version+1, updated_at=? WHERE tenant_id=? AND app_id=? AND router_id=? AND is_default=1`,
				nowStr, scope.TenantID, scope.AppID, routerID,
			); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO gw_routes (tenant_id, app_id, route_id, router_id, name, path_prefix, upstream_url, is_default, strip_prefix, timeout_ms, rate_limit_qps, status, version, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'ACTIVE', 1, ?, ?)`,
			scope.TenantID, scope.AppID, rid, routerID, name, pathPrefix, upstreamURL, boolInt(isDefault), boolInt(stripPrefix), timeoutMs, qps, nowStr, nowStr,
		); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`UPDATE gw_revisions SET route_version=route_version+1, updated_at=? WHERE tenant_id=? AND app_id=?`,
			nowStr, scope.TenantID, scope.AppID,
		); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT route_version FROM gw_revisions WHERE tenant_id=? AND app_id=?`, scope.TenantID, scope.AppID).Scan(&routeVer)
	})
	return domain.GatewayRoute{
		TenantID: scope.TenantID, AppID: scope.AppID, RouteID: rid, RouterID: routerID,
		Name: name, PathPrefix: pathPrefix, UpstreamURL: upstreamURL, IsDefault: isDefault,
		StripPrefix: stripPrefix, TimeoutMs: timeoutMs, RateLimitQPS: qps, Status: "ACTIVE",
		Version: 1, RouteVersion: routeVer, CreatedAt: now, UpdatedAt: now,
	}, err
}

// SwitchDefaultRoute atomically clears the prior default route and promotes routeID inside a scope lock, incrementing route_version (LiveOps G01/G02).
func (r *Repo) SwitchDefaultRoute(ctx context.Context, scope domain.Scope, routeID string) (int64, error) {
	nowStr := r.NowStr()
	var routeVer int64
	err := r.db.WithScopeTx(ctx, dbScope(scope), func(ctx context.Context, tx *db.Tx) error {
		if _, err := tx.TryAdvisoryLock(ctx, "gw:"+scope.TenantID+":"+scope.AppID); err != nil {
			return err
		}
		var routerID string
		if err := tx.QueryRow(ctx,
			`SELECT router_id FROM gw_routes WHERE tenant_id=? AND app_id=? AND route_id=?`,
			scope.TenantID, scope.AppID, routeID,
		).Scan(&routerID); err != nil {
			return mapNotFound(err, "route not found")
		}
		if _, err := tx.Exec(ctx,
			`UPDATE gw_routes SET is_default=0, version=version+1, updated_at=? WHERE tenant_id=? AND app_id=? AND router_id=? AND is_default=1`,
			nowStr, scope.TenantID, scope.AppID, routerID,
		); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`UPDATE gw_routes SET is_default=1, version=version+1, updated_at=? WHERE tenant_id=? AND app_id=? AND route_id=?`,
			nowStr, scope.TenantID, scope.AppID, routeID,
		); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`UPDATE gw_revisions SET route_version=route_version+1, updated_at=? WHERE tenant_id=? AND app_id=?`,
			nowStr, scope.TenantID, scope.AppID,
		); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT route_version FROM gw_revisions WHERE tenant_id=? AND app_id=?`, scope.TenantID, scope.AppID).Scan(&routeVer)
	})
	return routeVer, err
}

func (r *Repo) ListGatewayTelemetry(ctx context.Context, scope domain.Scope) ([]domain.TelemetryBucket, error) {
	rows, err := r.db.Query(ctx,
		`SELECT tenant_id, app_id, router_id, bucket_kind, bucket_key, total_requests, error_requests, total_latency_ms, updated_at
		 FROM gw_telemetry_samples WHERE tenant_id=? AND app_id=? ORDER BY bucket_kind ASC, bucket_key DESC`,
		scope.TenantID, scope.AppID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.TelemetryBucket
	for rows.Next() {
		var b domain.TelemetryBucket
		var ua string
		if err := rows.Scan(&b.TenantID, &b.AppID, &b.RouterID, &b.BucketKind, &b.BucketKey, &b.TotalRequests, &b.ErrorRequests, &b.TotalLatencyMs, &ua); err != nil {
			return nil, err
		}
		if b.TotalRequests > 0 {
			b.AvgLatencyMs = float64(b.TotalLatencyMs) / float64(b.TotalRequests)
		}
		b.UpdatedAt = parseTime(ua)
		out = append(out, b)
	}
	return out, rows.Err()
}

// ============================================================================
// Artifacts — Verification Gate & Reference Integrity (LiveOps G03, G04, G05)
// ============================================================================

func (r *Repo) CreateArtifactFile(ctx context.Context, scope domain.Scope, filename, contentType, sha256Hex, storagePath, actorID string, sizeBytes int64) (domain.ArtifactFile, error) {
	now := r.Now()
	nowStr := formatTime(now)
	fid := id.New("file")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	f := domain.ArtifactFile{
		TenantID: scope.TenantID, AppID: scope.AppID, FileID: fid, Filename: filename,
		ContentType: contentType, SizeBytes: sizeBytes, SHA256: sha256Hex, StoragePath: storagePath,
		VerificationStatus: "PENDING", CreatedBy: actorID, CreatedAt: now,
	}
	_, err := r.db.Exec(ctx,
		`INSERT INTO artifact_files (tenant_id, app_id, file_id, filename, content_type, size_bytes, sha256, storage_path, verification_status, verified_at, created_by, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'PENDING', '', ?, ?)`,
		scope.TenantID, scope.AppID, fid, filename, contentType, sizeBytes, sha256Hex, storagePath, actorID, nowStr,
	)
	return f, err
}

func (r *Repo) VerifyArtifactFile(ctx context.Context, scope domain.Scope, fileID, expectedSHA256 string) (domain.ArtifactFile, error) {
	now := r.Now()
	nowStr := formatTime(now)
	var out domain.ArtifactFile
	err := r.db.WithScopeTx(ctx, dbScope(scope), func(ctx context.Context, tx *db.Tx) error {
		var actualSHA, fname, ctype, spath, cby, ca string
		var sz int64
		if err := tx.QueryRow(ctx,
			`SELECT filename, content_type, size_bytes, sha256, storage_path, created_by, created_at
			 FROM artifact_files WHERE tenant_id=? AND app_id=? AND file_id=?`,
			scope.TenantID, scope.AppID, fileID,
		).Scan(&fname, &ctype, &sz, &actualSHA, &spath, &cby, &ca); err != nil {
			return mapNotFound(err, "artifact file not found")
		}
		status := "VERIFIED"
		if expectedSHA256 != "" && expectedSHA256 != actualSHA {
			status = "REJECTED"
		}
		if _, err := tx.Exec(ctx,
			`UPDATE artifact_files SET verification_status=?, verified_at=? WHERE tenant_id=? AND app_id=? AND file_id=?`,
			status, nowStr, scope.TenantID, scope.AppID, fileID,
		); err != nil {
			return err
		}
		out = domain.ArtifactFile{
			TenantID: scope.TenantID, AppID: scope.AppID, FileID: fileID, Filename: fname,
			ContentType: ctype, SizeBytes: sz, SHA256: actualSHA, StoragePath: spath,
			VerificationStatus: status, VerifiedAt: &now, CreatedBy: cby, CreatedAt: parseTime(ca),
		}
		return nil
	})
	return out, err
}

// PromoteArtifactRelease enforces LiveOps G03/G04: fail-closed unless artifact_files.verification_status == 'VERIFIED'!
func (r *Repo) PromoteArtifactRelease(ctx context.Context, scope domain.Scope, name, version, fileID, notes, actorID string) (domain.ArtifactRelease, error) {
	now := r.Now()
	nowStr := formatTime(now)
	rid := id.New("rel")
	var rel domain.ArtifactRelease

	err := r.db.WithScopeTx(ctx, dbScope(scope), func(ctx context.Context, tx *db.Tx) error {
		var vStatus string
		if err := tx.QueryRow(ctx,
			`SELECT verification_status FROM artifact_files WHERE tenant_id=? AND app_id=? AND file_id=?`,
			scope.TenantID, scope.AppID, fileID,
		).Scan(&vStatus); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return domain.NotFoundError("artifact file not found in scope")
			}
			return err
		}
		if vStatus != "VERIFIED" {
			return domain.NewError(domain.ErrStateConflict, "ARTIFACT_NOT_VERIFIED", "制品尚未通过完整性验签（当前状态: "+vStatus+"），禁止晋升发布", nil)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO artifact_releases (tenant_id, app_id, release_id, name, version, file_id, status, notes, promoted_at, created_by, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, 'PROMOTED', ?, ?, ?, ?)`,
			scope.TenantID, scope.AppID, rid, name, version, fileID, notes, nowStr, actorID, nowStr,
		); err != nil {
			if isUniqueViolation(err) {
				return domain.NewError(domain.ErrAlreadyExists, "RELEASE_EXISTS", "同名同版本制品发布已存在", nil)
			}
			return err
		}
		rel = domain.ArtifactRelease{
			TenantID: scope.TenantID, AppID: scope.AppID, ReleaseID: rid, Name: name, Version: version,
			FileID: fileID, Status: "PROMOTED", Notes: notes, PromotedAt: &now, CreatedBy: actorID, CreatedAt: now,
		}
		return nil
	})
	return rel, err
}

// DeleteArtifactFile enforces LiveOps G05: refuses deleting any file blob referenced by releases or attachments.
func (r *Repo) DeleteArtifactFile(ctx context.Context, scope domain.Scope, fileID string) error {
	return r.db.WithScopeTx(ctx, dbScope(scope), func(ctx context.Context, tx *db.Tx) error {
		var relCnt, attCnt int
		_ = tx.QueryRow(ctx, `SELECT COUNT(*) FROM artifact_releases WHERE tenant_id=? AND app_id=? AND file_id=?`, scope.TenantID, scope.AppID, fileID).Scan(&relCnt)
		_ = tx.QueryRow(ctx, `SELECT COUNT(*) FROM artifact_attachments WHERE tenant_id=? AND app_id=? AND file_id=?`, scope.TenantID, scope.AppID, fileID).Scan(&attCnt)
		if relCnt > 0 || attCnt > 0 {
			return domain.NewError(domain.ErrReferenceInUse, "ARTIFACT_IN_USE", "制品文件仍被发布或附件引用，禁止删除", nil)
		}
		res, err := tx.Exec(ctx, `DELETE FROM artifact_files WHERE tenant_id=? AND app_id=? AND file_id=?`, scope.TenantID, scope.AppID, fileID)
		if err != nil {
			if isFKViolation(err) {
				return domain.NewError(domain.ErrReferenceInUse, "ARTIFACT_IN_USE", "制品文件被外键引用，禁止删除", nil)
			}
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return domain.NotFoundError("artifact file not found")
		}
		return nil
	})
}

func (r *Repo) ListArtifactFiles(ctx context.Context, scope domain.Scope) ([]domain.ArtifactFile, error) {
	rows, err := r.db.Query(ctx,
		`SELECT tenant_id, app_id, file_id, filename, content_type, size_bytes, sha256, storage_path, verification_status, verified_at, created_by, created_at
		 FROM artifact_files WHERE tenant_id=? AND app_id=? ORDER BY created_at DESC`,
		scope.TenantID, scope.AppID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.ArtifactFile
	for rows.Next() {
		var f domain.ArtifactFile
		var va, ca string
		if err := rows.Scan(&f.TenantID, &f.AppID, &f.FileID, &f.Filename, &f.ContentType, &f.SizeBytes, &f.SHA256, &f.StoragePath, &f.VerificationStatus, &va, &f.CreatedBy, &ca); err != nil {
			return nil, err
		}
		f.VerifiedAt, f.CreatedAt = parseTimePtr(va), parseTime(ca)
		out = append(out, f)
	}
	return out, rows.Err()
}
