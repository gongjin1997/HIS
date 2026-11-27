package catalog

import (
	"context"
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
	"sync"
	"time"

	"github.com/kinstall/kinstall/internal/artifact/bundle"
	"github.com/kinstall/kinstall/internal/domain"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// ArtifactEntry represents a real verified file in the local offline artifact repository.
type ArtifactEntry struct {
	ID          string    `json:"id"`
	Category    string    `json:"category"` // binaries | images | bundles | charts | runtimes
	Product     string    `json:"product"`
	Version     string    `json:"version"`
	Arch        string    `json:"arch"`
	PageSize    int       `json:"pageSize,omitempty"`
	FileName    string    `json:"fileName"`
	LogicalPath string    `json:"logicalPath"`
	AbsPath     string    `json:"absPath"`
	SizeBytes   int64     `json:"sizeBytes"`
	SHA256      string    `json:"sha256"`
	ModifiedAt  time.Time `json:"modifiedAt"`
}

type shaCacheItem struct {
	size  int64
	mtime int64
	sha   string
}

// Store indexes, serves, uploads, and prefetches real offline artifacts on local disk.
type Store struct {
	primaryRoot string
	extraRoots  []string
	client      *http.Client

	mu    sync.RWMutex
	cache map[string]shaCacheItem
}

// NewStore creates an Artifact Catalog Store rooted at primaryRoot (created automatically if missing).
func NewStore(primaryRoot string, extraRoots ...string) *Store {
	primaryRoot = filepath.Clean(primaryRoot)
	_ = os.MkdirAll(primaryRoot, 0o750)
	for _, cat := range []string{"binaries", "images", "bundles", "charts", "runtimes"} {
		_ = os.MkdirAll(filepath.Join(primaryRoot, cat), 0o750)
	}
	return &Store{
		primaryRoot: primaryRoot,
		extraRoots:  extraRoots,
		client:      &http.Client{Timeout: 10 * time.Minute},
		cache:       make(map[string]shaCacheItem),
	}
}

func (s *Store) PrimaryRoot() string { return s.primaryRoot }

func (s *Store) allRoots(policyRoots []string) []string {
	seen := make(map[string]bool)
	var out []string
	add := func(r string) {
		r = strings.TrimSpace(r)
		if r == "" {
			return
		}
		c := filepath.Clean(r)
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	add(s.primaryRoot)
	for _, r := range policyRoots {
		add(r)
	}
	for _, r := range s.extraRoots {
		add(r)
	}
	return out
}

// Scan discovers all real artifacts across the primary store and configured policy local roots.
func (s *Store) Scan(policyRoots ...string) ([]ArtifactEntry, error) {
	roots := s.allRoots(policyRoots)
	seenLogical := make(map[string]bool)
	out := make([]ArtifactEntry, 0)

	for _, root := range roots {
		fi, err := os.Stat(root)
		if err != nil || !fi.IsDir() {
			continue
		}
		_ = filepath.WalkDir(root, func(p string, d os.DirEntry, werr error) error {
			if werr != nil || d.IsDir() {
				return nil //nolint:nilerr // best-effort directory scan skips unreadable paths
			}
			name := d.Name()
			if strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".sha256") || strings.HasSuffix(name, ".tmp") {
				return nil
			}
			info, ierr := d.Info()
			if ierr != nil || !info.Mode().IsRegular() {
				return nil //nolint:nilerr // skip vanished or non-regular files during scan
			}
			rel, rerr := filepath.Rel(root, p)
			if rerr != nil {
				return nil //nolint:nilerr // skip path outside root
			}
			logical := filepath.ToSlash(rel)
			if seenLogical[logical] {
				return nil
			}
			seenLogical[logical] = true

			sha, serr := s.fileSHA256(p, info)
			if serr != nil {
				return nil //nolint:nilerr // skip temporarily unreadable file during scan
			}
			entry := parseLogicalMetadata(logical, p, info, sha)
			out = append(out, entry)
			return nil
		})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Category != out[j].Category {
			return out[i].Category < out[j].Category
		}
		if out[i].Product != out[j].Product {
			return out[i].Product < out[j].Product
		}
		return out[i].LogicalPath < out[j].LogicalPath
	})
	return out, nil
}

func parseLogicalMetadata(logical, absPath string, info os.FileInfo, sha string) ArtifactEntry {
	parts := strings.Split(logical, "/")
	cat := "binaries"
	var prod string
	ver := "latest"
	arch := "amd64"
	fileName := parts[len(parts)-1]

	switch len(parts) {
	case 1:
		prod = strings.TrimSuffix(fileName, filepath.Ext(fileName))
	case 2:
		cat = normalizeCategory(parts[0])
		prod = strings.TrimSuffix(fileName, filepath.Ext(fileName))
	case 3:
		cat = normalizeCategory(parts[0])
		prod = parts[1]
	case 4:
		cat = normalizeCategory(parts[0])
		prod = parts[1]
		ver = parts[2]
	default:
		cat = normalizeCategory(parts[0])
		prod = parts[1]
		ver = parts[2]
		arch = parts[3]
	}

	pageSize := 4096
	low := strings.ToLower(logical)
	if strings.Contains(low, "64k") || strings.Contains(low, "65536") {
		pageSize = 65536
	}
	if strings.Contains(low, "arm64") || strings.Contains(low, "aarch64") {
		arch = "arm64"
	} else if strings.Contains(low, "amd64") || strings.Contains(low, "x86_64") {
		arch = "amd64"
	} else if strings.Contains(low, "noarch") || cat == "charts" {
		arch = "noarch"
	}

	idHash := sha256.Sum256([]byte(logical))
	return ArtifactEntry{
		ID:          "art_" + hex.EncodeToString(idHash[:6]),
		Category:    cat,
		Product:     prod,
		Version:     ver,
		Arch:        arch,
		PageSize:    pageSize,
		FileName:    fileName,
		LogicalPath: logical,
		AbsPath:     absPath,
		SizeBytes:   info.Size(),
		SHA256:      sha,
		ModifiedAt:  info.ModTime().UTC(),
	}
}

func normalizeCategory(c string) string {
	switch strings.ToLower(strings.TrimSpace(c)) {
	case "binaries", "binary", "bin":
		return "binaries"
	case "images", "image", "oci":
		return "images"
	case "bundles", "bundle":
		return "bundles"
	case "charts", "chart", "helm":
		return "charts"
	case "runtimes", "runtime":
		return "runtimes"
	default:
		return "binaries"
	}
}

func (s *Store) fileSHA256(absPath string, info os.FileInfo) (string, error) {
	s.mu.RLock()
	if cached, ok := s.cache[absPath]; ok && cached.size == info.Size() && cached.mtime == info.ModTime().UnixNano() {
		s.mu.RUnlock()
		return cached.sha, nil
	}
	s.mu.RUnlock()

	// Check sidecar .sha256 if newer than file
	sidecar := absPath + ".sha256"
	if sci, err := os.Stat(sidecar); err == nil && !sci.ModTime().Before(info.ModTime()) {
		if raw, rerr := os.ReadFile(sidecar); rerr == nil { // #nosec G304 -- sidecar derived from scanned store file
			fields := strings.Fields(string(raw))
			if len(fields) > 0 && len(fields[0]) == 64 {
				sha := strings.ToLower(fields[0])
				s.mu.Lock()
				s.cache[absPath] = shaCacheItem{size: info.Size(), mtime: info.ModTime().UnixNano(), sha: sha}
				s.mu.Unlock()
				return sha, nil
			}
		}
	}

	f, err := os.Open(absPath) // #nosec G304 -- scanned inside configured artifact root
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	sha := hex.EncodeToString(h.Sum(nil))
	s.mu.Lock()
	s.cache[absPath] = shaCacheItem{size: info.Size(), mtime: info.ModTime().UnixNano(), sha: sha}
	s.mu.Unlock()
	return sha, nil
}

// FindMatch searches the local artifact catalog for a binary/tarball matching product, name, version, arch, and pageSize.
func (s *Store) FindMatch(product, name, version, arch string, pageSize int, matchPageSize bool, policyRoots []string) (*ArtifactEntry, bool) {
	entries, err := s.Scan(policyRoots...)
	if err != nil {
		return nil, false
	}
	wantProd := strings.ToLower(strings.TrimSpace(product))
	wantName := strings.ToLower(strings.TrimSpace(name))
	wantVer := strings.TrimSpace(version)
	wantArch := strings.ToLower(strings.TrimSpace(arch))
	if wantArch == "" {
		wantArch = "amd64"
	}

	var best *ArtifactEntry
	bestScore := -1
	for i := range entries {
		e := &entries[i]
		if e.Category != "binaries" && e.Category != "runtimes" && e.Category != "bundles" {
			continue
		}
		if matchPageSize && pageSize == 65536 && e.PageSize != 65536 {
			continue
		}
		ep := strings.ToLower(e.Product)
		ef := strings.ToLower(e.FileName)
		matchesProd := (wantProd != "" && (ep == wantProd || strings.Contains(ef, wantProd))) ||
			(wantName != "" && (ep == wantName || strings.Contains(ef, wantName)))
		if !matchesProd {
			continue
		}
		score := 0
		if ep == wantProd || ep == wantName {
			score += 20
		}
		if wantVer != "" && e.Version == wantVer {
			score += 30
		} else if wantVer != "" && strings.Contains(ef, wantVer) {
			score += 20
		}
		switch e.Arch {
		case wantArch:
			score += 25
		case "noarch":
			score += 5
		default:
			continue
		}
		if wantName != "" && (ef == wantName || strings.HasPrefix(ef, wantName+"-") || strings.HasPrefix(ef, wantName+".")) {
			score += 15
		}
		if score > bestScore {
			bestScore = score
			best = e
		}
	}
	return best, best != nil
}

func sanitizeSegment(seg, fallback string) (string, error) {
	s := strings.TrimSpace(seg)
	if s == "" {
		s = fallback
	}
	if strings.Contains(s, "..") || strings.ContainsAny(s, "/\\") {
		return "", sdk.NewError(sdk.CodeAPIInvalidRequest, "ART", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false,
			fmt.Errorf("invalid artifact path segment %q", seg))
	}
	return s, nil
}

// SaveUpload writes an uploaded artifact atomically to primaryRoot/<category>/<product>/<version>/<arch>/<fileName>
// while computing SHA-256 and writing a .sha256 sidecar.
func (s *Store) SaveUpload(category, product, version, arch, fileName string, r io.Reader) (*ArtifactEntry, error) {
	cat := normalizeCategory(category)
	prod, err := sanitizeSegment(product, "generic")
	if err != nil {
		return nil, err
	}
	ver, err := sanitizeSegment(version, "1.0.0")
	if err != nil {
		return nil, err
	}
	ar, err := sanitizeSegment(arch, "amd64")
	if err != nil {
		return nil, err
	}
	fn, err := sanitizeSegment(filepath.Base(fileName), "artifact.bin")
	if err != nil {
		return nil, err
	}

	dir := filepath.Join(s.primaryRoot, cat, prod, ver, ar)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	dst := filepath.Join(dir, fn)
	tmp := dst + ".tmp." + domain.NewID("u")

	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640) // #nosec G304 -- sanitized path inside primaryRoot
	if err != nil {
		return nil, err
	}
	h := sha256.New()
	n, cerr := io.Copy(io.MultiWriter(f, h), r)
	closeErr := f.Close()
	if cerr != nil {
		_ = os.Remove(tmp)
		return nil, cerr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return nil, closeErr
	}
	if n == 0 {
		_ = os.Remove(tmp)
		return nil, sdk.NewError(sdk.CodeAPIInvalidRequest, "ART", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, errors.New("empty artifact upload"))
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return nil, err
	}
	sha := hex.EncodeToString(h.Sum(nil))
	_ = os.WriteFile(dst+".sha256", []byte(fmt.Sprintf("%s  %s\n", sha, fn)), 0o640)

	info, err := os.Stat(dst)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.cache[dst] = shaCacheItem{size: info.Size(), mtime: info.ModTime().UnixNano(), sha: sha}
	s.mu.Unlock()

	logical := path.Join(cat, prod, ver, ar, fn)
	entry := parseLogicalMetadata(logical, dst, info, sha)
	return &entry, nil
}

// PrefetchURL downloads a remote file into the local offline artifact pool and calculates its SHA-256.
func (s *Store) PrefetchURL(ctx context.Context, rawURL, category, product, version, arch, fileName string) (*ArtifactEntry, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, sdk.NewError(sdk.CodeAPIInvalidRequest, "ART", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false,
			fmt.Errorf("invalid prefetch URL %q (http/https required)", rawURL))
	}
	if strings.TrimSpace(fileName) == "" {
		fileName = path.Base(u.Path)
		if fileName == "" || fileName == "/" || fileName == "." {
			fileName = product + "-" + version + ".tar.gz"
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, sdk.NewError(sdk.CodeSrcNotFound, "SRC", http.StatusBadGateway, sdk.CodeSrcNotFound, true, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, sdk.NewError(sdk.CodeSrcNotFound, "SRC", http.StatusBadGateway, sdk.CodeSrcNotFound, true,
			fmt.Errorf("prefetch HTTP %d for %s", resp.StatusCode, u.String()))
	}
	return s.SaveUpload(category, product, version, arch, fileName, resp.Body)
}

// Delete removes an artifact by logicalPath from the primary artifact root.
func (s *Store) Delete(logicalPath string) error {
	clean := path.Clean("/" + strings.TrimSpace(logicalPath))
	clean = strings.TrimPrefix(clean, "/")
	if clean == "" || strings.Contains(clean, "..") {
		return sdk.NewError(sdk.CodeAPIInvalidRequest, "ART", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, errors.New("invalid logicalPath"))
	}
	target := filepath.Join(s.primaryRoot, filepath.FromSlash(clean))
	rel, err := filepath.Rel(s.primaryRoot, target)
	if err != nil || strings.HasPrefix(rel, "..") {
		return sdk.NewError(sdk.CodeAuthForbidden, "ART", http.StatusForbidden, sdk.CodeAuthForbidden, false, errors.New("path outside artifact store"))
	}
	if err := os.Remove(target); err != nil {
		return err
	}
	_ = os.Remove(target + ".sha256")
	s.mu.Lock()
	delete(s.cache, target)
	s.mu.Unlock()
	return nil
}

// ResolvePath safely resolves a logicalPath across all configured roots.
func (s *Store) ResolvePath(logicalPath string, policyRoots ...string) (string, error) {
	clean := path.Clean("/" + strings.TrimSpace(logicalPath))
	clean = strings.TrimPrefix(clean, "/")
	if clean == "" || strings.Contains(clean, "..") {
		return "", errors.New("invalid logicalPath")
	}
	for _, root := range s.allRoots(policyRoots) {
		cand := filepath.Join(root, filepath.FromSlash(clean))
		if fi, err := os.Stat(cand); err == nil && fi.Mode().IsRegular() {
			return cand, nil
		}
	}
	return "", os.ErrNotExist
}

// ExportBOM produces a canonical bundle.Manifest covering every artifact currently in the store.
func (s *Store) ExportBOM(policyRoots ...string) (bundle.Manifest, error) {
	entries, err := s.Scan(policyRoots...)
	if err != nil {
		return bundle.Manifest{}, err
	}
	archSet := make(map[string]bool)
	files := make([]bundle.FileEntry, 0, len(entries))
	for _, e := range entries {
		files = append(files, bundle.FileEntry{
			Path:   e.LogicalPath,
			Size:   e.SizeBytes,
			SHA256: e.SHA256,
		})
		if e.Arch != "" && e.Arch != "noarch" {
			archSet[e.Arch] = true
		}
	}
	arches := make([]string, 0, len(archSet))
	for a := range archSet {
		arches = append(arches, a)
	}
	sort.Strings(arches)
	if len(arches) == 0 {
		arches = []string{"amd64"}
	}
	return bundle.Manifest{
		SchemaVersion:      "v1alpha1",
		BundleID:           domain.NewID("bnd_"),
		BundleType:         "full",
		TargetRevision:     time.Now().UTC().Format("2006.01.02-150405"),
		MinPlatformVersion: "0.1.0",
		CreatedAt:          time.Now().UTC(),
		Architectures:      arches,
		Files:              files,
	}, nil
}
