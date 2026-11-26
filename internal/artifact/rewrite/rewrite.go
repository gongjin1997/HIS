package rewrite

import (
	"fmt"
	"net/http"
	"strings"

	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// MirrorConfig defines global registryMirror rewriting rules (D3 §2, R2).
type MirrorConfig struct {
	MirrorHost string            `json:"mirrorHost" yaml:"mirrorHost"` // e.g. "registry.corp.local:15000"
	PrefixMap  map[string]string `json:"prefixMap" yaml:"prefixMap"`   // registry -> mirror prefix (default keeps registry name)
}

var defaultUpstreamRegistries = map[string]string{
	"docker.io":       "docker.io",
	"registry.k8s.io": "registry.k8s.io",
	"ghcr.io":         "ghcr.io",
	"quay.io":         "quay.io",
	"gcr.io":          "gcr.io",
}

// RewriteImage rewrites any upstream image reference (including bare names like "valkey/valkey:9.1.4"
// or "nginx:1.27") to the configured private mirror, blocking :latest, Bitnami, and MinIO CE (D3 §2).
func RewriteImage(ref string, cfg MirrorConfig) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", sdk.NewError(sdk.CodeImgMutableTag, "IMG", http.StatusUnprocessableEntity, sdk.CodeImgMutableTag, false, fmt.Errorf("empty image ref"))
	}
	if strings.HasSuffix(ref, ":latest") || (!strings.Contains(ref, ":") && !strings.Contains(ref, "@sha256:")) {
		return "", sdk.NewError(sdk.CodeImgMutableTag, "IMG", http.StatusUnprocessableEntity, sdk.CodeImgMutableTag, false,
			fmt.Errorf("mutable or missing tag forbidden: %q", ref))
	}
	low := strings.ToLower(ref)
	if strings.Contains(low, "bitnami/") || strings.Contains(low, "minio/minio") {
		return "", sdk.NewError(sdk.CodeImgForbiddenRegistry, "IMG", http.StatusUnprocessableEntity, sdk.CodeImgForbiddenRegistry, false,
			fmt.Errorf("forbidden image repository: %q", ref))
	}
	if cfg.MirrorHost == "" {
		return ref, nil
	}
	if strings.Contains(cfg.MirrorHost, "://") || strings.Contains(cfg.MirrorHost, "/") {
		return "", sdk.NewError(sdk.CodeImgRewriteInvalid, "IMG", http.StatusUnprocessableEntity, sdk.CodeImgRewriteInvalid, false,
			fmt.Errorf("mirrorHost must be host[:port] without scheme/path: %q", cfg.MirrorHost))
	}

	reg, repoAndTag := splitRegistry(ref)
	// Already pointing to the private mirror
	if strings.EqualFold(reg, cfg.MirrorHost) {
		return ref, nil
	}
	prefix := reg
	if mapped, ok := cfg.PrefixMap[reg]; ok {
		prefix = strings.Trim(mapped, "/")
	} else if def, ok := defaultUpstreamRegistries[reg]; ok {
		prefix = def
	}
	if prefix == "" {
		return fmt.Sprintf("%s/%s", cfg.MirrorHost, repoAndTag), nil
	}
	return fmt.Sprintf("%s/%s/%s", cfg.MirrorHost, prefix, repoAndTag), nil
}

func splitRegistry(ref string) (string, string) {
	parts := strings.SplitN(ref, "/", 2)
	if len(parts) == 1 {
		// Bare official image e.g. "nginx:1.27" -> docker.io/library/nginx:1.27
		return "docker.io", "library/" + parts[0]
	}
	first := parts[0]
	if strings.Contains(first, ".") || strings.Contains(first, ":") || first == "localhost" {
		return first, parts[1]
	}
	// Implicit docker.io namespace e.g. "valkey/valkey:9.1.4"
	return "docker.io", ref
}

// RenderContainerdHostsTOML generates containerd /etc/containerd/certs.d/<mirror>/hosts.toml with TLS CA (zero insecure-registries!).
func RenderContainerdHostsTOML(mirrorHost, caPath string) string {
	return fmt.Sprintf(`server = "https://%s"

[host."https://%s"]
  capabilities = ["pull", "resolve"]
  ca = %q
`, mirrorHost, mirrorHost, caPath)
}
