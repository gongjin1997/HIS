package nodes

import (
	"context"
	"strings"
	"time"

	"github.com/kinstall/kinstall/internal/adapter/store"
	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/pkg/platform/auth"
)

type Service struct {
	repo *store.Repo
}

func New(repo *store.Repo) *Service {
	return &Service{repo: repo}
}

func (s *Service) IssueEnrollToken(ctx context.Context, tc auth.TrustedContext, scope domain.Scope, labels map[string]string, ttl time.Duration) (domain.EnrollToken, error) {
	tok, err := s.repo.IssueEnrollToken(ctx, scope, labels, ttl, tc.SubjectID)
	if err == nil {
		_ = s.repo.AppendAuditLog(ctx, domain.AuditLog{
			TenantID: scope.TenantID, AppID: scope.AppID, ActorID: tc.SubjectID, ActorName: tc.Username,
			Action: "NODE_ENROLL_TOKEN_ISSUE", ResourceType: "enroll_token", ResourceID: tok.TokenID, RequestID: tc.TraceID,
		}, "nodes", 201, "签发零信任节点一次性注册令牌")
	}
	return tok, err
}

func (s *Service) EnrollNode(ctx context.Context, rawToken, hostname, ipAddr, osArch, agentVer string) (domain.Node, string, error) {
	rawToken, hostname = strings.TrimSpace(rawToken), strings.TrimSpace(hostname)
	if rawToken == "" || hostname == "" {
		return domain.Node{}, "", domain.ValidationError("注册令牌和主机名不能为空")
	}
	return s.repo.EnrollNode(ctx, rawToken, hostname, ipAddr, osArch, agentVer)
}

func (s *Service) AuthenticateNode(ctx context.Context, nodeID, rawCred string) (domain.Node, error) {
	return s.repo.AuthenticateNode(ctx, nodeID, rawCred)
}

func (s *Service) Heartbeat(ctx context.Context, node domain.Node, ipAddr, agentVer string) (domain.Node, error) {
	scope := domain.Scope{TenantID: node.TenantID, AppID: node.AppID}
	return s.repo.RecordNodeHeartbeat(ctx, scope, node.NodeID, ipAddr, agentVer)
}

func (s *Service) RotateCredential(ctx context.Context, tc auth.TrustedContext, scope domain.Scope, nodeID string, graceWindow time.Duration) (domain.Node, string, error) {
	n, newCred, err := s.repo.RotateNodeCredential(ctx, scope, nodeID, graceWindow)
	if err == nil {
		_ = s.repo.AppendAuditLog(ctx, domain.AuditLog{
			TenantID: scope.TenantID, AppID: scope.AppID, ActorID: tc.SubjectID, ActorName: tc.Username,
			Action: "NODE_CRED_ROTATE", ResourceType: "node", ResourceID: nodeID, RequestID: tc.TraceID,
		}, "nodes", 200, "执行零信任节点双轨凭据轮换 "+n.Hostname)
	}
	return n, newCred, err
}

func (s *Service) SetStatus(ctx context.Context, tc auth.TrustedContext, scope domain.Scope, nodeID string, status domain.NodeStatus) (domain.Node, error) {
	n, err := s.repo.SetNodeStatus(ctx, scope, nodeID, status)
	if err == nil {
		_ = s.repo.AppendAuditLog(ctx, domain.AuditLog{
			TenantID: scope.TenantID, AppID: scope.AppID, ActorID: tc.SubjectID, ActorName: tc.Username,
			Action: "NODE_STATUS_" + string(status), ResourceType: "node", ResourceID: nodeID, RequestID: tc.TraceID,
		}, "nodes", 200, "变更节点状态为 "+string(status))
	}
	return n, err
}

func (s *Service) SweepHealth(ctx context.Context) (int, error) {
	return s.repo.SweepNodeHealth(ctx)
}

func (s *Service) ListNodes(ctx context.Context, scope domain.Scope) ([]domain.Node, error) {
	return s.repo.ListNodes(ctx, scope)
}

func (s *Service) CreateDistribution(ctx context.Context, scope domain.Scope, artifactFileID, targetNodeID, sha256Hex string, totalBytes int64) (domain.Distribution, error) {
	if totalBytes <= 0 || targetNodeID == "" {
		return domain.Distribution{}, domain.ValidationError("目标节点与总字节数不能为空")
	}
	return s.repo.CreateDistribution(ctx, scope, artifactFileID, targetNodeID, sha256Hex, totalBytes)
}

func (s *Service) AckDistributionChunk(ctx context.Context, scope domain.Scope, distID string, chunkIndex int, offset, chunkBytes int64, chunkSHA256 string) (domain.Distribution, error) {
	if chunkIndex < 0 || chunkBytes <= 0 {
		return domain.Distribution{}, domain.ValidationError("非法分片校验参数")
	}
	return s.repo.AckDistributionChunk(ctx, scope, distID, chunkIndex, offset, chunkBytes, chunkSHA256)
}
