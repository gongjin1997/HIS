package oci

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"

	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// ImageInspectResult captures pure-Go OCI image metadata extracted without docker CLI or docker.sock (LiveOps 03.7).
type ImageInspectResult struct {
	Digest       string `json:"digest"`
	Architecture string `json:"architecture"`
	OS           string `json:"os"`
	Layers       int    `json:"layers"`
	Created      string `json:"created,omitempty"`
}

// PushRequest configures a zero-CLI native Go OCI image push to Harbor/SWR/Registry.
type PushRequest struct {
	TarballPath  string
	TargetRef    string
	ExpectedArch string // amd64 | arm64 (optional; when set, mismatch fails closed!)
	Username     string
	Password     string
	InsecureTLS  bool
}

// PushResult reports the pushed digest and verified architecture.
type PushResult struct {
	TargetRef    string `json:"targetRef"`
	Digest       string `json:"digest"`
	Architecture string `json:"architecture"`
	OS           string `json:"os"`
	Layers       int    `json:"layers"`
	DurationMs   int64  `json:"durationMs"`
}

// InspectTarball inspects a Docker Save or OCI tarball strictly in pure Go (zero docker CLI / zero docker.sock).
func InspectTarball(tarballPath string) (*ImageInspectResult, error) {
	img, err := tarball.ImageFromPath(tarballPath, nil)
	if err != nil {
		return nil, sdk.NewError(sdk.CodeAPIInvalidRequest, "OCI", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false,
			fmt.Errorf("解析镜像 tar 包失败（仅支持 docker save / OCI tarball）: %w", err))
	}
	cfg, err := img.ConfigFile()
	if err != nil {
		return nil, fmt.Errorf("read oci image config: %w", err)
	}
	digest, err := img.Digest()
	if err != nil {
		return nil, fmt.Errorf("compute oci digest: %w", err)
	}
	layers, _ := img.Layers()
	created := ""
	if !cfg.Created.IsZero() {
		created = cfg.Created.Time.UTC().Format(time.RFC3339)
	}
	return &ImageInspectResult{
		Digest:       digest.String(),
		Architecture: cfg.Architecture,
		OS:           cfg.OS,
		Layers:       len(layers),
		Created:      created,
	}, nil
}

// PushTarballNative streams an image tarball directly to an OCI registry (Harbor/SWR/Zot) via google/go-containerregistry,
// enforcing fail-closed CPU architecture verification to prevent amd64/arm64 mix-ups in production (LiveOps 03.7 §3.2).
func PushTarballNative(ctx context.Context, req PushRequest) (*PushResult, error) {
	start := time.Now()
	targetRef := strings.TrimSpace(req.TargetRef)
	if targetRef == "" {
		return nil, sdk.NewError(sdk.CodeAPIInvalidRequest, "OCI", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false,
			fmt.Errorf("targetRef 不能为空，例如 harbor.internal/library/nginx:1.27.4"))
	}

	img, err := tarball.ImageFromPath(req.TarballPath, nil)
	if err != nil {
		return nil, sdk.NewError(sdk.CodeAPIInvalidRequest, "OCI", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false,
			fmt.Errorf("读取镜像归档失败: %w", err))
	}
	cfg, err := img.ConfigFile()
	if err != nil {
		return nil, err
	}

	expected := strings.ToLower(strings.TrimSpace(req.ExpectedArch))
	detected := strings.ToLower(strings.TrimSpace(cfg.Architecture))
	if expected != "" && detected != "" && expected != detected {
		return nil, sdk.NewError(sdk.CodeAPIInvalidRequest, "OCI", http.StatusUnprocessableEntity, sdk.CodeAPIInvalidRequest, false,
			fmt.Errorf("镜像架构校验失败（Fail-Closed 阻断）：期望架构 %q，实际镜像架构 %q（防止信创 ARM64 / x86_64 混推引发 exec format error）", expected, detected))
	}

	var nameOpts []name.Option
	if req.InsecureTLS {
		nameOpts = append(nameOpts, name.Insecure)
	}
	ref, err := name.ParseReference(targetRef, nameOpts...)
	if err != nil {
		return nil, sdk.NewError(sdk.CodeAPIInvalidRequest, "OCI", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false,
			fmt.Errorf("非法 OCI 镜像引用 %q: %w", targetRef, err))
	}

	opts := []remote.Option{remote.WithContext(ctx)}
	if req.Username != "" || req.Password != "" {
		opts = append(opts, remote.WithAuth(&authn.Basic{Username: req.Username, Password: req.Password}))
	}
	if req.InsecureTLS {
		tr := http.DefaultTransport.(*http.Transport).Clone()
		// #nosec G402 -- operator explicitly opted into insecure internal registry TLS
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		opts = append(opts, remote.WithTransport(tr))
	}

	if err := remote.Write(ref, img, opts...); err != nil {
		return nil, fmt.Errorf("推送 OCI 镜像至 %s 失败: %w", targetRef, err)
	}
	digest, _ := img.Digest()
	layers, _ := img.Layers()
	return &PushResult{
		TargetRef:    ref.String(),
		Digest:       digest.String(),
		Architecture: cfg.Architecture,
		OS:           cfg.OS,
		Layers:       len(layers),
		DurationMs:   time.Since(start).Milliseconds(),
	}, nil
}
