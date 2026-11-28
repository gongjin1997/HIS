package gateway

import (
	"context"
	"fmt"
	"strings"

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

func (s *Service) ListRouters(ctx context.Context, scope domain.Scope) ([]domain.GatewayRouter, int64, error) {
	return s.repo.ListGatewayRouters(ctx, scope)
}

func (s *Service) CreateRouter(ctx context.Context, tc auth.TrustedContext, scope domain.Scope, name, listenHost string, qps, burst int) (domain.GatewayRouter, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return domain.GatewayRouter{}, domain.ValidationError("路由器名称不能为空")
	}
	rt, err := s.repo.CreateGatewayRouter(ctx, scope, name, listenHost, qps, burst)
	if err == nil {
		_ = s.repo.AppendAuditLog(ctx, domain.AuditLog{
			TenantID: scope.TenantID, AppID: scope.AppID, ActorID: tc.SubjectID, ActorName: tc.Username,
			Action: "GW_ROUTER_CREATE", ResourceType: "gateway_router", ResourceID: rt.RouterID, RequestID: tc.TraceID,
		}, "gateway", 201, "创建网关路由器 "+rt.Name)
	}
	return rt, err
}

func (s *Service) ListRoutes(ctx context.Context, scope domain.Scope, routerID string) ([]domain.GatewayRoute, int64, error) {
	return s.repo.ListGatewayRoutes(ctx, scope, routerID)
}

func (s *Service) CreateRoute(ctx context.Context, tc auth.TrustedContext, scope domain.Scope, routerID, name, pathPrefix, upstreamURL string, isDefault, stripPrefix bool, timeoutMs, qps int) (domain.GatewayRoute, error) {
	name, pathPrefix, upstreamURL = strings.TrimSpace(name), strings.TrimSpace(pathPrefix), strings.TrimSpace(upstreamURL)
	if routerID == "" || name == "" || pathPrefix == "" || upstreamURL == "" {
		return domain.GatewayRoute{}, domain.ValidationError("路由器、路由名、路径前缀与上游地址均不能为空")
	}
	if !strings.HasPrefix(pathPrefix, "/") {
		return domain.GatewayRoute{}, domain.ValidationError("路径前缀必须以 / 开头")
	}
	rt, err := s.repo.CreateGatewayRoute(ctx, scope, routerID, name, pathPrefix, upstreamURL, isDefault, stripPrefix, timeoutMs, qps)
	if err == nil {
		_ = s.repo.AppendAuditLog(ctx, domain.AuditLog{
			TenantID: scope.TenantID, AppID: scope.AppID, ActorID: tc.SubjectID, ActorName: tc.Username,
			Action: "GW_ROUTE_CREATE", ResourceType: "gateway_route", ResourceID: rt.RouteID, RequestID: tc.TraceID,
		}, "gateway", 201, fmt.Sprintf("创建动态路由 %s (%s -> %s, routeVer=%d)", rt.Name, rt.PathPrefix, rt.UpstreamURL, rt.RouteVersion))
	}
	return rt, err
}

func (s *Service) SwitchDefaultRoute(ctx context.Context, tc auth.TrustedContext, scope domain.Scope, routeID string) (int64, error) {
	ver, err := s.repo.SwitchDefaultRoute(ctx, scope, routeID)
	if err == nil {
		_ = s.repo.AppendAuditLog(ctx, domain.AuditLog{
			TenantID: scope.TenantID, AppID: scope.AppID, ActorID: tc.SubjectID, ActorName: tc.Username,
			Action: "GW_DEFAULT_ROUTE_SWITCH", ResourceType: "gateway_route", ResourceID: routeID, RequestID: tc.TraceID,
		}, "gateway", 200, fmt.Sprintf("原子切换默认路由至 %s (routeVersion=%d)", routeID, ver))
	}
	return ver, err
}

func (s *Service) ListTelemetry(ctx context.Context, scope domain.Scope) ([]domain.TelemetryBucket, error) {
	return s.repo.ListGatewayTelemetry(ctx, scope)
}

func (s *Service) CreateArtifactFile(ctx context.Context, tc auth.TrustedContext, scope domain.Scope, filename, contentType, sha256Hex, storagePath string, sizeBytes int64) (domain.ArtifactFile, error) {
	if strings.TrimSpace(filename) == "" || strings.TrimSpace(sha256Hex) == "" {
		return domain.ArtifactFile{}, domain.ValidationError("文件名与 SHA256 不能为空")
	}
	return s.repo.CreateArtifactFile(ctx, scope, filename, contentType, sha256Hex, storagePath, tc.SubjectID, sizeBytes)
}

func (s *Service) VerifyArtifactFile(ctx context.Context, scope domain.Scope, fileID, expectedSHA256 string) (domain.ArtifactFile, error) {
	return s.repo.VerifyArtifactFile(ctx, scope, fileID, expectedSHA256)
}

func (s *Service) PromoteArtifactRelease(ctx context.Context, tc auth.TrustedContext, scope domain.Scope, name, version, fileID, notes string) (domain.ArtifactRelease, error) {
	if strings.TrimSpace(name) == "" || strings.TrimSpace(version) == "" || strings.TrimSpace(fileID) == "" {
		return domain.ArtifactRelease{}, domain.ValidationError("发布名、版本与制品文件 ID 不能为空")
	}
	rel, err := s.repo.PromoteArtifactRelease(ctx, scope, name, version, fileID, notes, tc.SubjectID)
	if err == nil {
		_ = s.repo.AppendAuditLog(ctx, domain.AuditLog{
			TenantID: scope.TenantID, AppID: scope.AppID, ActorID: tc.SubjectID, ActorName: tc.Username,
			Action: "ARTIFACT_RELEASE_PROMOTE", ResourceType: "artifact_release", ResourceID: rel.ReleaseID, RequestID: tc.TraceID,
		}, "gateway", 201, fmt.Sprintf("晋升已验签制品发布 %s@%s", rel.Name, rel.Version))
	}
	return rel, err
}

func (s *Service) DeleteArtifactFile(ctx context.Context, scope domain.Scope, fileID string) error {
	return s.repo.DeleteArtifactFile(ctx, scope, fileID)
}

func (s *Service) ListArtifactFiles(ctx context.Context, scope domain.Scope) ([]domain.ArtifactFile, error) {
	return s.repo.ListArtifactFiles(ctx, scope)
}
