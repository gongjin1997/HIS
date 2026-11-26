package source

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/internal/ports"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// ErrPresignNotSupported is returned by sources that cannot issue direct presigned URLs.
var ErrPresignNotSupported = errors.New("presigned direct download not supported")

// ArtifactSpec describes a media item to resolve (D3 §7.4).
type ArtifactSpec struct {
	LogicalPath string
	UpstreamURL string
	SHA256      string
}

// DeliveryPlan is the resolved delivery decision for a target host (D3 §7.4).
type DeliveryPlan struct {
	Mode          string // relay-cache | direct-presigned
	SourceUsed    string // local | obs | s3 | online
	LocalFilePath string
	DirectURL     string // caller MUST register in RedactWriter when Mode==direct-presigned!
	RedactedLabel string
	SHA256        string
}

// ArtifactSource is the strategy interface for Local, ObjectStore (OBS/S3/OSS/COS), and Online sources.
type ArtifactSource interface {
	Name() string
	Priority() int
	IsInternal() bool
	Exists(ctx context.Context, spec ArtifactSpec) (bool, error)
	FetchTo(ctx context.Context, spec ArtifactSpec, dst io.Writer) error
	PresignGet(ctx context.Context, spec ArtifactSpec, ttl time.Duration) (rawURL string, label string, err error)
}

// Resolver implements the Local / OBS·S3 / Online ternary resolution chain with singleflight dedup
// and strict offlineStrict policy enforcement (D3 §7, ADR-077).
type Resolver struct {
	sources       []ArtifactSource
	localRoots    []string
	cacheDir      string
	offlineStrict bool
	strictLocal   bool
	deliveryMode  string // auto | relay-cache | direct-presigned
	presignTTL    time.Duration
	sf            singleflight.Group
}

// NewResolver constructs a Resolver from explicit sources and policy flags.
func NewResolver(sources []ArtifactSource, localRoots []string, cacheDir string, offlineStrict, strictLocal bool, deliveryMode string, presignTTL time.Duration) *Resolver {
	if deliveryMode == "" {
		deliveryMode = "auto"
	}
	if presignTTL <= 0 || presignTTL > 30*time.Minute {
		presignTTL = 15 * time.Minute
	}
	return &Resolver{
		sources:       sources,
		localRoots:    localRoots,
		cacheDir:      cacheDir,
		offlineStrict: offlineStrict,
		strictLocal:   strictLocal,
		deliveryMode:  deliveryMode,
		presignTTL:    presignTTL,
	}
}

// Resolve executes the priority waterfall chain (local -> obs -> online by default) with singleflight cache dedup.
func (r *Resolver) ResolveSpec(ctx context.Context, spec ArtifactSpec, targetCanReachOBS bool) (*DeliveryPlan, error) {
	if len(spec.SHA256) != 64 {
		return nil, sdk.NewError(sdk.CodeSrcMissingSHA256, "SRC", http.StatusUnprocessableEntity, sdk.CodeSrcMissingSHA256, false,
			fmt.Errorf("artifact %q missing valid 64-hex sha256", spec.LogicalPath))
	}

	ordered := append([]ArtifactSource(nil), r.sources...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Priority() < ordered[j].Priority() })

	cachedPath := filepath.Join(r.cacheDir, filepath.FromSlash(spec.LogicalPath))
	var blocked []string

	for _, src := range ordered {
		name := strings.ToLower(src.Name())
		// strictLocal: allow ONLY local
		if r.strictLocal && name != "local" {
			blocked = append(blocked, name)
			continue
		}
		// offlineStrict: allow ONLY local and internal=true private OBS/S3; hard-block online & public OBS (KI-SRC-0005)!
		if r.offlineStrict && (name == "online" || ((name == "obs" || name == "s3") && !src.IsInternal())) {
			blocked = append(blocked, name)
			continue
		}

		if name == "local" {
			roots := append(append([]string(nil), r.localRoots...), r.cacheDir)
			for _, root := range roots {
				if root == "" {
					continue
				}
				p := filepath.Join(root, filepath.FromSlash(spec.LogicalPath))
				if verifyFileSHA256(p, spec.SHA256) == nil {
					return &DeliveryPlan{
						Mode:          "relay-cache",
						SourceUsed:    "local",
						LocalFilePath: p,
						RedactedLabel: "local://" + spec.LogicalPath,
						SHA256:        spec.SHA256,
					}, nil
				}
			}
			continue
		}

		exists, err := src.Exists(ctx, spec)
		if err != nil || !exists {
			continue
		}

		effMode := r.deliveryMode
		if effMode == "auto" {
			if targetCanReachOBS {
				effMode = "direct-presigned"
			} else {
				effMode = "relay-cache"
			}
		}
		if effMode == "direct-presigned" && targetCanReachOBS {
			rawURL, label, perr := src.PresignGet(ctx, spec, r.presignTTL)
			if perr == nil {
				return &DeliveryPlan{
					Mode:          "direct-presigned",
					SourceUsed:    name,
					DirectURL:     rawURL,
					RedactedLabel: label,
					SHA256:        spec.SHA256,
				}, nil
			}
		}

		// relay-cache: singleflight dedup ensures N concurrent hosts download only ONCE!
		val, serr, _ := r.sf.Do(spec.LogicalPath+"@"+spec.SHA256, func() (any, error) {
			if err := fetchAndVerifyAtomic(ctx, src, spec, cachedPath); err != nil {
				return nil, err
			}
			return cachedPath, nil
		})
		if serr != nil {
			continue
		}
		return &DeliveryPlan{
			Mode:          "relay-cache",
			SourceUsed:    name,
			LocalFilePath: val.(string),
			RedactedLabel: name + "://" + spec.LogicalPath,
			SHA256:        spec.SHA256,
		}, nil
	}

	if (r.offlineStrict || r.strictLocal) && len(blocked) > 0 {
		return nil, sdk.NewError(sdk.CodeSrcOfflineStrictDenied, "SRC", http.StatusUnprocessableEntity, sdk.CodeSrcOfflineStrictDenied, false,
			fmt.Errorf("artifact %q not in allowed offline sources; blocked: %s", spec.LogicalPath, strings.Join(blocked, ",")))
	}
	return nil, sdk.NewError(sdk.CodeSrcNotFound, "SRC", http.StatusNotFound, sdk.CodeSrcNotFound, false,
		fmt.Errorf("artifact %q not found across configured sources", spec.LogicalPath))
}

func verifyFileSHA256(p, want string) error {
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), want) {
		return sdk.NewError(sdk.CodeSrcDigestMismatch, "SRC", http.StatusUnprocessableEntity, sdk.CodeSrcDigestMismatch, false, fmt.Errorf("sha256 mismatch for %s", p))
	}
	return nil
}

func fetchAndVerifyAtomic(ctx context.Context, src ArtifactSource, spec ArtifactSpec, finalPath string) error {
	if err := os.MkdirAll(filepath.Dir(finalPath), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(finalPath), ".kinstall-fetch-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	h := sha256.New()
	if err := src.FetchTo(ctx, spec, io.MultiWriter(tmp, h)); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, spec.SHA256) {
		return sdk.NewError(sdk.CodeSrcDigestMismatch, "SRC", http.StatusUnprocessableEntity, sdk.CodeSrcDigestMismatch, false,
			fmt.Errorf("sha256 mismatch from %s for %s: got=%s want=%s", src.Name(), spec.LogicalPath, got, spec.SHA256))
	}
	return os.Rename(tmpPath, finalPath)
}

// --- Built-in Sources: Local, S3/OBS, Online ---

type LocalSource struct {
	priority int
}

func NewLocalSource(priority int) *LocalSource { return &LocalSource{priority: priority} }
func (s *LocalSource) Name() string            { return "local" }
func (s *LocalSource) Priority() int           { return s.priority }
func (s *LocalSource) IsInternal() bool        { return true }
func (s *LocalSource) Exists(context.Context, ArtifactSpec) (bool, error) {
	return false, nil
}
func (s *LocalSource) FetchTo(context.Context, ArtifactSpec, io.Writer) error {
	return errors.New("local source handled directly by resolver")
}
func (s *LocalSource) PresignGet(context.Context, ArtifactSpec, time.Duration) (string, string, error) {
	return "", "", ErrPresignNotSupported
}

// ObjectStoreSource supports Huawei OBS / Aliyun OSS / Tencent COS / AWS S3 / SeaweedFS / Ceph RGW
// using clean S3v4 signing WITHOUT aws-chunked trailing checksum headers (D3 §7.3 Trap 2).
type ObjectStoreSource struct {
	name      string
	priority  int
	internal  bool
	endpoint  string
	region    string
	bucket    string
	prefix    string
	accessKey string
	secretKey string
	pathStyle bool
	client    *http.Client
}

func NewObjectStoreSource(cfg domain.MediaSourceConfig, ak, sk string, hc *http.Client) *ObjectStoreSource {
	if hc == nil {
		hc = &http.Client{Timeout: 60 * time.Second}
	}
	name := cfg.Type
	if name == "" {
		name = "obs"
	}
	region := cfg.Region
	if region == "" {
		region = "cn-north-4"
	}
	return &ObjectStoreSource{
		name:      name,
		priority:  cfg.Priority,
		internal:  cfg.Internal,
		endpoint:  strings.TrimRight(cfg.Endpoint, "/"),
		region:    region,
		bucket:    cfg.Bucket,
		prefix:    strings.Trim(cfg.Prefix, "/"),
		accessKey: ak,
		secretKey: sk,
		pathStyle: true,
		client:    hc,
	}
}

func (s *ObjectStoreSource) Name() string     { return s.name }
func (s *ObjectStoreSource) Priority() int    { return s.priority }
func (s *ObjectStoreSource) IsInternal() bool { return s.internal }

func (s *ObjectStoreSource) objectKey(logicalPath string) string {
	clean := strings.TrimLeft(logicalPath, "/")
	if s.prefix == "" {
		return clean
	}
	return path.Join(s.prefix, clean)
}

func (s *ObjectStoreSource) objectURL(key string) string {
	return fmt.Sprintf("%s/%s/%s", s.endpoint, s.bucket, strings.TrimLeft(key, "/"))
}

func (s *ObjectStoreSource) Exists(ctx context.Context, spec ArtifactSpec) (bool, error) {
	u := s.objectURL(s.objectKey(spec.LogicalPath))
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, u, nil)
	if err != nil {
		return false, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return false, err
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK, nil
}

func (s *ObjectStoreSource) FetchTo(ctx context.Context, spec ArtifactSpec, dst io.Writer) error {
	u := s.objectURL(s.objectKey(spec.LogicalPath))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return sdk.NewError(sdk.CodeSrcPresignFailed, "SRC", http.StatusBadGateway, sdk.CodeSrcPresignFailed, true,
			fmt.Errorf("object store GET %s returned %d", spec.LogicalPath, resp.StatusCode))
	}
	_, err = io.Copy(dst, resp.Body)
	return err
}

// PresignGet produces a short-lived SigV4 query-authenticated GET URL (TTL <= 30m, D3 §7.3 Trap 4).
func (s *ObjectStoreSource) PresignGet(_ context.Context, spec ArtifactSpec, ttl time.Duration) (string, string, error) {
	if ttl <= 0 || ttl > 30*time.Minute {
		ttl = 15 * time.Minute
	}
	key := s.objectKey(spec.LogicalPath)
	base := s.objectURL(key)
	parsed, err := url.Parse(base)
	if err != nil {
		return "", "", err
	}
	now := time.Now().UTC()
	dateStamp := now.Format("20060102")
	amzDate := now.Format("20060102T150405Z")
	scope := fmt.Sprintf("%s/%s/s3/aws4_request", dateStamp, s.region)

	q := parsed.Query()
	q.Set("X-Amz-Algorithm", "AWS4-HMAC-SHA256")
	q.Set("X-Amz-Credential", s.accessKey+"/"+scope)
	q.Set("X-Amz-Date", amzDate)
	q.Set("X-Amz-Expires", fmt.Sprintf("%d", int(ttl.Seconds())))
	q.Set("X-Amz-SignedHeaders", "host")
	canonicalQuery := q.Encode()

	canonicalReq := strings.Join([]string{
		http.MethodGet,
		parsed.EscapedPath(),
		canonicalQuery,
		"host:" + parsed.Host + "\n",
		"host",
		"UNSIGNED-PAYLOAD",
	}, "\n")
	crHash := sha256.Sum256([]byte(canonicalReq))
	strToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		scope,
		hex.EncodeToString(crHash[:]),
	}, "\n")

	sig := signAWSV4(s.secretKey, dateStamp, s.region, "s3", strToSign)
	q.Set("X-Amz-Signature", sig)
	parsed.RawQuery = q.Encode()
	label := fmt.Sprintf("%s://%s/%s [PRESIGNED-REDACTED]", s.name, s.bucket, key)
	return parsed.String(), label, nil
}

func signAWSV4(secretKey, dateStamp, region, service, stringToSign string) string {
	kDate := hmacSHA256([]byte("AWS4"+secretKey), dateStamp)
	kRegion := hmacSHA256(kDate, region)
	kService := hmacSHA256(kRegion, service)
	kSigning := hmacSHA256(kService, "aws4_request")
	return hex.EncodeToString(hmacSHA256(kSigning, stringToSign))
}

func hmacSHA256(key []byte, data string) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(data))
	return mac.Sum(nil)
}

// OnlineSource fetches artifacts from allowlisted upstream URLs (D3 §7.2).
type OnlineSource struct {
	priority   int
	baseURL    string
	allowHosts map[string]bool
	client     *http.Client
}

func NewOnlineSource(priority int, baseURL string, allowHosts []string, hc *http.Client) *OnlineSource {
	if hc == nil {
		hc = &http.Client{Timeout: 60 * time.Second}
	}
	allow := make(map[string]bool)
	for _, h := range allowHosts {
		allow[strings.ToLower(strings.TrimSpace(h))] = true
	}
	return &OnlineSource{priority: priority, baseURL: strings.TrimRight(baseURL, "/"), allowHosts: allow, client: hc}
}

func (s *OnlineSource) Name() string     { return "online" }
func (s *OnlineSource) Priority() int    { return s.priority }
func (s *OnlineSource) IsInternal() bool { return false }

func (s *OnlineSource) targetURL(spec ArtifactSpec) (string, error) {
	raw := spec.UpstreamURL
	if raw == "" && s.baseURL != "" {
		raw = s.baseURL + "/" + strings.TrimLeft(spec.LogicalPath, "/")
	}
	if raw == "" {
		return "", errors.New("no upstream URL")
	}
	if len(s.allowHosts) > 0 {
		u, err := url.Parse(raw)
		if err != nil || !s.allowHosts[strings.ToLower(u.Hostname())] {
			return "", fmt.Errorf("upstream host not in allowHosts: %s", raw)
		}
	}
	return raw, nil
}

func (s *OnlineSource) Exists(ctx context.Context, spec ArtifactSpec) (bool, error) {
	u, err := s.targetURL(spec)
	if err != nil {
		return false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, u, nil)
	if err != nil {
		return false, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return false, err
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK, nil
}

func (s *OnlineSource) FetchTo(ctx context.Context, spec ArtifactSpec, dst io.Writer) error {
	u, err := s.targetURL(spec)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("online GET returned %d", resp.StatusCode)
	}
	_, err = io.Copy(dst, resp.Body)
	return err
}

func (s *OnlineSource) PresignGet(context.Context, ArtifactSpec, time.Duration) (string, string, error) {
	return "", "", ErrPresignNotSupported
}

// PolicyMediaResolver adapts Resolver to ports.MediaResolver driven by domain.MediaPolicy.
type PolicyMediaResolver struct {
	cacheDir string
	client   *http.Client
}

func NewPolicyMediaResolver(cacheDir string) *PolicyMediaResolver {
	return &PolicyMediaResolver{cacheDir: cacheDir, client: &http.Client{Timeout: 30 * time.Second}}
}

func (p *PolicyMediaResolver) Resolve(ctx context.Context, policy domain.MediaPolicy, ref sdk.MediaRef) (*ports.ResolvedMedia, error) {
	var sources []ArtifactSource
	var localRoots []string
	for _, sc := range policy.Sources {
		switch strings.ToLower(sc.Type) {
		case "local":
			sources = append(sources, NewLocalSource(sc.Priority))
			localRoots = append(localRoots, sc.Roots...)
		case "obs", "s3":
			sources = append(sources, NewObjectStoreSource(sc, "", "", p.client))
		case "online":
			sources = append(sources, NewOnlineSource(sc.Priority, sc.BaseURL, nil, p.client))
		}
	}
	res := NewResolver(sources, localRoots, p.cacheDir, policy.OfflineStrict, policy.StrictLocal, policy.DeliveryMode, 15*time.Minute)
	logicalPath := path.Join("binaries", ref.Product, ref.Version, ref.Arch, ref.Name)
	plan, err := res.ResolveSpec(ctx, ArtifactSpec{LogicalPath: logicalPath, SHA256: ref.ExpectedSHA}, policy.DeliveryMode == "direct-presigned")
	if err != nil {
		return nil, err
	}
	return &ports.ResolvedMedia{
		SourceType:   plan.SourceUsed,
		DeliveryMode: plan.Mode,
		LocalPath:    plan.LocalFilePath,
		URL:          plan.DirectURL,
		SHA256:       plan.SHA256,
		Presigned:    plan.Mode == "direct-presigned",
	}, nil
}

func (p *PolicyMediaResolver) ProbePolicy(ctx context.Context, policy domain.MediaPolicy) (map[string]any, error) {
	results := make([]map[string]any, 0, len(policy.Sources))
	for _, sc := range policy.Sources {
		item := map[string]any{
			"type":     sc.Type,
			"priority": sc.Priority,
			"internal": sc.Internal,
		}
		if policy.OfflineStrict && (sc.Type == "online" || ((sc.Type == "obs" || sc.Type == "s3") && !sc.Internal)) {
			item["status"] = "blocked"
			item["code"] = sdk.CodeSrcOfflineStrictDenied
			results = append(results, item)
			continue
		}
		switch strings.ToLower(sc.Type) {
		case "local":
			ok := false
			for _, r := range sc.Roots {
				if fi, err := os.Stat(r); err == nil && fi.IsDir() {
					ok = true
					break
				}
			}
			item["status"] = boolStatus(ok)
		case "obs", "s3":
			if sc.Endpoint == "" {
				item["status"] = "unconfigured"
			} else {
				req, err := http.NewRequestWithContext(ctx, http.MethodHead, sc.Endpoint, nil)
				if err == nil {
					resp, derr := p.client.Do(req)
					if derr == nil {
						_ = resp.Body.Close()
						item["status"] = "reachable"
						item["httpStatus"] = resp.StatusCode
					} else {
						item["status"] = "unreachable"
						item["error"] = derr.Error()
					}
				}
			}
		case "online":
			item["status"] = "configured"
		}
		results = append(results, item)
	}
	return map[string]any{
		"policyId":      policy.ID,
		"offlineStrict": policy.OfflineStrict,
		"deliveryMode":  policy.DeliveryMode,
		"sources":       results,
		"probedAt":      time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func boolStatus(ok bool) string {
	if ok {
		return "ok"
	}
	return "missing"
}
