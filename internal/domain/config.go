package domain

import "time"

type ConfigEntry struct {
	Key        string `json:"key"`
	Value      string `json:"value"`
	IsSecret   bool   `json:"is_secret"`
	Ciphertext string `json:"ciphertext,omitempty"` // v2:{ver}:{alg}:{nonce}:{ct} stored in DB; stripped from default API views
	Comment    string `json:"comment,omitempty"`
}

type Configuration struct {
	TenantID    string        `json:"tenant_id"`
	AppID       string        `json:"app_id"`
	CfgID       string        `json:"cfg_id"`
	ProjectID   string        `json:"project_id"`
	Name        string        `json:"name"`
	Format      string        `json:"format"` // kv | yaml | json | properties
	Description string        `json:"description"`
	Entries     []ConfigEntry `json:"entries"`
	Version     int64         `json:"version"`
	Deleted     bool          `json:"deleted"`
	CreatedBy   string        `json:"created_by"`
	UpdatedBy   string        `json:"updated_by"`
	CreatedAt   time.Time     `json:"created_at"`
	UpdatedAt   time.Time     `json:"updated_at"`
}

type ConfigSnapshot struct {
	TenantID      string        `json:"tenant_id"`
	AppID         string        `json:"app_id"`
	CfgID         string        `json:"cfg_id"`
	Version       int64         `json:"version"`
	Entries       []ConfigEntry `json:"entries"`
	ChangeSummary string        `json:"change_summary"`
	ChangedBy     string        `json:"changed_by"`
	CreatedAt     time.Time     `json:"created_at"`
}

type EnvMember struct {
	CfgID      string `json:"cfg_id"`
	CfgName    string `json:"cfg_name,omitempty"`
	CfgVersion int64  `json:"cfg_version,omitempty"`
	Priority   int    `json:"priority"`
}

type Environment struct {
	TenantID    string      `json:"tenant_id"`
	AppID       string      `json:"app_id"`
	EnvID       string      `json:"env_id"`
	ProjectID   string      `json:"project_id"`
	Code        string      `json:"code"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Version     int64       `json:"version"`
	Members     []EnvMember `json:"members"`
	CreatedBy   string      `json:"created_by"`
	UpdatedBy   string      `json:"updated_by"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
}

type EnvHistory struct {
	TenantID  string      `json:"tenant_id"`
	AppID     string      `json:"app_id"`
	EnvID     string      `json:"env_id"`
	Version   int64       `json:"version"`
	Members   []EnvMember `json:"members"`
	ChangedBy string      `json:"changed_by"`
	CreatedAt time.Time   `json:"created_at"`
}

type EnvConflict struct {
	Key             string   `json:"key"`
	Sources         []string `json:"sources"`
	EffectiveSource string   `json:"effective_source"`
}

type EnvViewCheck struct {
	EnvID     string            `json:"env_id"`
	Version   int64             `json:"version"`
	Merged    map[string]string `json:"merged"`
	Conflicts []EnvConflict     `json:"conflicts"`
}

// ComputeEnvViewCheck deterministically merges ordered configuration members (higher priority overrides lower)
// and detects colliding keys across configs without exposing sealed secret plaintexts.
func ComputeEnvViewCheck(env Environment, cfgs []Configuration) EnvViewCheck {
	byID := make(map[string]Configuration, len(cfgs))
	for _, c := range cfgs {
		byID[c.CfgID] = c
	}
	merged := make(map[string]string)
	origins := make(map[string][]string)
	winner := make(map[string]string)

	for _, m := range env.Members {
		cfg, ok := byID[m.CfgID]
		if !ok || cfg.Deleted {
			continue
		}
		for _, e := range cfg.Entries {
			val := e.Value
			if e.IsSecret {
				val = "******"
			}
			merged[e.Key] = val
			origins[e.Key] = append(origins[e.Key], cfg.Name)
			winner[e.Key] = cfg.Name
		}
	}

	var conflicts []EnvConflict
	for k, srcs := range origins {
		if len(srcs) > 1 {
			conflicts = append(conflicts, EnvConflict{
				Key:             k,
				Sources:         srcs,
				EffectiveSource: winner[k],
			})
		}
	}
	return EnvViewCheck{
		EnvID:     env.EnvID,
		Version:   env.Version,
		Merged:    merged,
		Conflicts: conflicts,
	}
}
