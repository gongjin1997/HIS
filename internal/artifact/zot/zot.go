package zot

import "encoding/json"

// EdgeConfigParams configures a Zot v2.1.23 primary or regional Pull-Through edge cache (D3 §5.2).
type EdgeConfigParams struct {
	RootDir        string
	ListenPort     string
	TLSCert        string
	TLSKey         string
	HTPasswdPath   string
	UpstreamURL    string
	UpstreamCreds  string
	UpstreamCADir  string
	EnableOnDemand bool
}

// RenderConfig produces a Zot v2.1.23 JSON config enforcing compat=["docker2s2"], dedupe,
// 24h scrub integrity checks, and onDemand pull-through without polling Docker Hub (D3 §5.2).
func RenderConfig(p EdgeConfigParams) ([]byte, error) {
	if p.RootDir == "" {
		p.RootDir = "/var/lib/kinstall/zot-edge"
	}
	if p.ListenPort == "" {
		p.ListenPort = "15000"
	}
	cfg := map[string]any{
		"distSpecVersion": "1.1.1",
		"storage": map[string]any{
			"rootDirectory": p.RootDir,
			"dedupe":        true,
			"gc":            true,
			"gcDelay":       "24h",
			"gcInterval":    "24h",
		},
		"http": map[string]any{
			"address": "0.0.0.0",
			"port":    p.ListenPort,
			"compat":  []string{"docker2s2"},
			"tls": map[string]string{
				"cert": p.TLSCert,
				"key":  p.TLSKey,
			},
			"auth": map[string]any{
				"htpasswd":  map[string]string{"path": p.HTPasswdPath},
				"failDelay": 3,
			},
		},
		"extensions": map[string]any{
			"scrub": map[string]any{
				"enable":   true,
				"interval": "24h",
			},
		},
	}
	if p.UpstreamURL != "" {
		cfg["extensions"].(map[string]any)["sync"] = map[string]any{
			"enable":          true,
			"credentialsFile": p.UpstreamCreds,
			"registries": []map[string]any{{
				"urls":       []string{p.UpstreamURL},
				"onDemand":   true,
				"maxRetries": 3,
				"retryDelay": "5s",
				"tlsVerify":  true,
				"certDir":    p.UpstreamCADir,
				"content":    []map[string]string{{"prefix": "**"}},
			}},
		}
	}
	return json.MarshalIndent(cfg, "", "  ")
}
