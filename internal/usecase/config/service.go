package config

import (
	"context"
	"fmt"
	"strings"

	"github.com/kinstall/kinstall/internal/adapter/store"
	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/pkg/platform/auth"
	"github.com/kinstall/kinstall/pkg/platform/crypto"
)

const secretMask = "******"

type Service struct {
	repo  *store.Repo
	codec *crypto.EnvelopeCodec
}

func New(repo *store.Repo, codec *crypto.EnvelopeCodec) *Service {
	return &Service{repo: repo, codec: codec}
}

func maskConfig(c domain.Configuration) domain.Configuration {
	out := c
	out.Entries = make([]domain.ConfigEntry, len(c.Entries))
	for i, e := range c.Entries {
		cp := e
		if cp.IsSecret {
			cp.Value = secretMask
			cp.Ciphertext = "" // never leak raw ciphertext envelope on normal list/get
		}
		out.Entries[i] = cp
	}
	return out
}

func (s *Service) sealEntries(scope domain.Scope, cfgKey string, incoming []domain.ConfigEntry, existing map[string]domain.ConfigEntry) ([]domain.ConfigEntry, error) {
	out := make([]domain.ConfigEntry, 0, len(incoming))
	for _, e := range incoming {
		k := strings.TrimSpace(e.Key)
		if k == "" {
			return nil, domain.ValidationError("配置键名不能为空")
		}
		cp := domain.ConfigEntry{Key: k, Value: e.Value, IsSecret: e.IsSecret, Comment: e.Comment}
		if cp.IsSecret {
			if (cp.Value == "" || cp.Value == secretMask) && existing != nil {
				if prev, ok := existing[k]; ok && prev.Ciphertext != "" {
					cp.Ciphertext = prev.Ciphertext
					cp.Value = secretMask
					out = append(out, cp)
					continue
				}
			}
			if s.codec == nil {
				return nil, domain.ValidationError("KMS 加密编解码器未初始化")
			}
			aad := []byte(fmt.Sprintf("%s:%s:%s:%s", scope.TenantID, scope.AppID, cfgKey, k))
			ct, err := s.codec.Seal([]byte(cp.Value), aad)
			if err != nil {
				return nil, fmt.Errorf("seal secret %q: %w", k, err)
			}
			cp.Ciphertext = ct
			cp.Value = secretMask
		}
		out = append(out, cp)
	}
	return out, nil
}

func (s *Service) ListConfigurations(ctx context.Context, scope domain.Scope, projectID string) ([]domain.Configuration, error) {
	list, err := s.repo.ListConfigurations(ctx, scope, projectID)
	if err != nil {
		return nil, err
	}
	for i := range list {
		list[i] = maskConfig(list[i])
	}
	return list, nil
}

func (s *Service) GetConfiguration(ctx context.Context, scope domain.Scope, cfgID string) (domain.Configuration, error) {
	c, err := s.repo.GetConfiguration(ctx, scope, cfgID)
	if err != nil {
		return domain.Configuration{}, err
	}
	return maskConfig(c), nil
}

func (s *Service) CreateConfiguration(ctx context.Context, tc auth.TrustedContext, scope domain.Scope, projectID, name, format, desc string, entries []domain.ConfigEntry) (domain.Configuration, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return domain.Configuration{}, domain.ValidationError("配置集名称不能为空")
	}
	if projectID == "" {
		projectID = scope.ProjectID
	}
	sealed, err := s.sealEntries(scope, name, entries, nil)
	if err != nil {
		return domain.Configuration{}, err
	}
	c, err := s.repo.CreateConfiguration(ctx, scope, projectID, name, format, desc, tc.SubjectID, sealed)
	if err == nil {
		_ = s.repo.AppendAuditLog(ctx, domain.AuditLog{
			TenantID: scope.TenantID, AppID: scope.AppID, ActorID: tc.SubjectID, ActorName: tc.Username,
			Action: "CONFIG_CREATE", ResourceType: "configuration", ResourceID: c.CfgID, RequestID: tc.TraceID,
		}, "config", 201, "创建配置集 "+c.Name+" (v1)")
	}
	return maskConfig(c), err
}

func (s *Service) UpdateConfiguration(ctx context.Context, tc auth.TrustedContext, scope domain.Scope, cfgID string, expectedVer int64, desc, changeSummary string, entries []domain.ConfigEntry) (domain.Configuration, error) {
	prev, err := s.repo.GetConfiguration(ctx, scope, cfgID)
	if err != nil {
		return domain.Configuration{}, err
	}
	prevMap := make(map[string]domain.ConfigEntry, len(prev.Entries))
	for _, e := range prev.Entries {
		prevMap[e.Key] = e
	}
	sealed, err := s.sealEntries(scope, prev.Name, entries, prevMap)
	if err != nil {
		return domain.Configuration{}, err
	}
	c, err := s.repo.UpdateConfiguration(ctx, scope, cfgID, expectedVer, desc, changeSummary, tc.SubjectID, sealed)
	if err == nil {
		_ = s.repo.AppendAuditLog(ctx, domain.AuditLog{
			TenantID: scope.TenantID, AppID: scope.AppID, ActorID: tc.SubjectID, ActorName: tc.Username,
			Action: "CONFIG_UPDATE", ResourceType: "configuration", ResourceID: c.CfgID, RequestID: tc.TraceID,
		}, "config", 200, fmt.Sprintf("更新配置集 %s 至 v%d (%s)", c.Name, c.Version, changeSummary))
	}
	return maskConfig(c), err
}

func (s *Service) TombstoneConfiguration(ctx context.Context, tc auth.TrustedContext, scope domain.Scope, cfgID string, expectedVer int64) error {
	err := s.repo.TombstoneConfiguration(ctx, scope, cfgID, expectedVer, tc.SubjectID)
	if err == nil {
		_ = s.repo.AppendAuditLog(ctx, domain.AuditLog{
			TenantID: scope.TenantID, AppID: scope.AppID, ActorID: tc.SubjectID, ActorName: tc.Username,
			Action: "CONFIG_TOMBSTONE", ResourceType: "configuration", ResourceID: cfgID, RequestID: tc.TraceID,
		}, "config", 200, "软删配置集 "+cfgID)
	}
	return err
}

func (s *Service) ListHistory(ctx context.Context, scope domain.Scope, cfgID string) ([]domain.ConfigSnapshot, error) {
	snaps, err := s.repo.ListConfigHistory(ctx, scope, cfgID)
	if err != nil {
		return nil, err
	}
	for i := range snaps {
		for j := range snaps[i].Entries {
			if snaps[i].Entries[j].IsSecret {
				snaps[i].Entries[j].Value = secretMask
				snaps[i].Entries[j].Ciphertext = ""
			}
		}
	}
	return snaps, nil
}

// RevealSecret decrypts a single KMS-sealed configuration entry and writes a mandatory tamper-evident audit entry!
func (s *Service) RevealSecret(ctx context.Context, tc auth.TrustedContext, scope domain.Scope, cfgID, key, reason string) (string, error) {
	if s.codec == nil {
		return "", domain.ValidationError("KMS 加密编解码器未初始化")
	}
	c, err := s.repo.GetConfiguration(ctx, scope, cfgID)
	if err != nil {
		return "", err
	}
	for _, e := range c.Entries {
		if e.Key == key {
			if !e.IsSecret {
				return e.Value, nil
			}
			// Try AAD bound to cfg.Name first, then cfg.CfgID (for seeded records)
			aad1 := []byte(fmt.Sprintf("%s:%s:%s:%s", scope.TenantID, scope.AppID, c.Name, key))
			pt, decErr := s.codec.Open(e.Ciphertext, aad1)
			if decErr != nil {
				aad2 := []byte(fmt.Sprintf("%s:%s:%s:%s", scope.TenantID, scope.AppID, c.CfgID, key))
				pt, decErr = s.codec.Open(e.Ciphertext, aad2)
			}
			if decErr != nil {
				return "", fmt.Errorf("decrypt secret %q: %w", key, decErr)
			}
			_ = s.repo.AppendAuditLog(ctx, domain.AuditLog{
				TenantID: scope.TenantID, AppID: scope.AppID, ActorID: tc.SubjectID, ActorName: tc.Username,
				Action: "CONFIG_SECRET_REVEAL", ResourceType: "configuration", ResourceID: cfgID + ":" + key,
				RequestID: tc.TraceID, DetailJSON: reason,
			}, "config", 200, fmt.Sprintf("审计揭示加密配置 %s/%s（原因: %s）", c.Name, key, reason))
			return string(pt), nil
		}
	}
	return "", domain.NotFoundError("configuration key not found")
}

func (s *Service) ListEnvironments(ctx context.Context, scope domain.Scope, projectID string) ([]domain.Environment, error) {
	return s.repo.ListEnvironments(ctx, scope, projectID)
}

func (s *Service) CreateEnvironment(ctx context.Context, tc auth.TrustedContext, scope domain.Scope, projectID, code, name, desc string, members []domain.EnvMember) (domain.Environment, error) {
	code, name = strings.TrimSpace(code), strings.TrimSpace(name)
	if code == "" || name == "" {
		return domain.Environment{}, domain.ValidationError("环境编码与名称不能为空")
	}
	if projectID == "" {
		projectID = scope.ProjectID
	}
	e, err := s.repo.CreateEnvironment(ctx, scope, projectID, code, name, desc, tc.SubjectID, members)
	if err == nil {
		_ = s.repo.AppendAuditLog(ctx, domain.AuditLog{
			TenantID: scope.TenantID, AppID: scope.AppID, ActorID: tc.SubjectID, ActorName: tc.Username,
			Action: "ENV_CREATE", ResourceType: "environment", ResourceID: e.EnvID, RequestID: tc.TraceID,
		}, "config", 201, "创建配置环境 "+e.Name)
	}
	return e, err
}

func (s *Service) UpdateEnvironmentMembers(ctx context.Context, tc auth.TrustedContext, scope domain.Scope, envID string, expectedVer int64, members []domain.EnvMember) (domain.Environment, error) {
	e, err := s.repo.UpdateEnvironmentMembers(ctx, scope, envID, expectedVer, tc.SubjectID, members)
	if err == nil {
		_ = s.repo.AppendAuditLog(ctx, domain.AuditLog{
			TenantID: scope.TenantID, AppID: scope.AppID, ActorID: tc.SubjectID, ActorName: tc.Username,
			Action: "ENV_MEMBERS_UPDATE", ResourceType: "environment", ResourceID: e.EnvID, RequestID: tc.TraceID,
		}, "config", 200, fmt.Sprintf("更新环境 %s 配置成员列表至 v%d", e.Name, e.Version))
	}
	return e, err
}

func (s *Service) ViewCheckEnvironment(ctx context.Context, scope domain.Scope, envID string) (domain.EnvViewCheck, error) {
	env, err := s.repo.GetEnvironment(ctx, scope, envID)
	if err != nil {
		return domain.EnvViewCheck{}, err
	}
	cfgs := make([]domain.Configuration, 0, len(env.Members))
	for _, m := range env.Members {
		c, err := s.repo.GetConfiguration(ctx, scope, m.CfgID)
		if err == nil {
			cfgs = append(cfgs, maskConfig(c))
		}
	}
	return domain.ComputeEnvViewCheck(env, cfgs), nil
}
