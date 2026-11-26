package bundle

import (
	"fmt"
	"net/http"
	"time"

	"github.com/kinstall/kinstall/internal/domain"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// Manifest models the bundle manifest.json schema (D3 §6.2).
type Manifest struct {
	SchemaVersion      string      `json:"schemaVersion"`
	BundleID           string      `json:"bundleId"`
	BundleType         string      `json:"bundleType"` // full | incremental
	BaseRevision       string      `json:"baseRevision,omitempty"`
	TargetRevision     string      `json:"targetRevision"`
	MinPlatformVersion string      `json:"minPlatformVersion"`
	CreatedAt          time.Time   `json:"createdAt"`
	Architectures      []string    `json:"architectures"`
	Files              []FileEntry `json:"files"`
}

type FileEntry struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// DiffManifests computes an incremental patch manifest containing ONLY added or hash-changed files (D3 §6.2, T-M1-16).
func DiffManifests(base, target Manifest) Manifest {
	baseMap := make(map[string]string, len(base.Files))
	for _, f := range base.Files {
		baseMap[f.Path] = f.SHA256
	}
	var delta []FileEntry
	for _, f := range target.Files {
		if prevSHA, ok := baseMap[f.Path]; !ok || prevSHA != f.SHA256 {
			delta = append(delta, f)
		}
	}
	return Manifest{
		SchemaVersion:      "v1alpha1",
		BundleID:           domain.NewID("bnd_"),
		BundleType:         "incremental",
		BaseRevision:       base.TargetRevision,
		TargetRevision:     target.TargetRevision,
		MinPlatformVersion: target.MinPlatformVersion,
		CreatedAt:          time.Now().UTC(),
		Architectures:      target.Architectures,
		Files:              delta,
	}
}

// ValidateIncrementalImport verifies that an incremental bundle's required baseRevision exists locally (KI-BND-0001).
func ValidateIncrementalImport(inc Manifest, installedRevisions map[string]bool) error {
	if inc.BundleType != "incremental" {
		return nil
	}
	if !installedRevisions[inc.BaseRevision] {
		return sdk.NewError(sdk.CodeBundleMissingBaseRev, "BND", http.StatusUnprocessableEntity, sdk.CodeBundleMissingBaseRev, false,
			fmt.Errorf("incremental bundle requires baseRevision %q which is not installed locally", inc.BaseRevision))
	}
	return nil
}
