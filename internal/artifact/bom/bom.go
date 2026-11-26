package bom

import (
	"fmt"
	"net/http"
	"strings"

	"sigs.k8s.io/yaml"

	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// Document models <product>/bom-<ver>-rev.<YYYY.MM.N>.yaml (05 §3.6, D3 §1.2).
type Document struct {
	APIVersion string      `json:"apiVersion" yaml:"apiVersion"`
	Kind       string      `json:"kind" yaml:"kind"`
	Product    string      `json:"product" yaml:"product"`
	Version    string      `json:"version" yaml:"version"`
	Rev        string      `json:"revision" yaml:"revision"`
	Components []Component `json:"components" yaml:"components"`
	Images     []ImageLock `json:"images,omitempty" yaml:"images,omitempty"`
}

type Component struct {
	Name      string          `json:"name" yaml:"name"`
	Version   string          `json:"version" yaml:"version"`
	License   string          `json:"license" yaml:"license"` // SPDX identifier
	Artifacts []ArtifactEntry `json:"artifacts" yaml:"artifacts"`
}

type ArtifactEntry struct {
	Name        string `json:"name,omitempty" yaml:"name,omitempty"`
	Arch        string `json:"arch" yaml:"arch"`
	PageSizes   []int  `json:"pageSizes,omitempty" yaml:"pageSizes,omitempty"`
	Kind        string `json:"kind" yaml:"kind"`
	LogicalPath string `json:"logicalPath,omitempty" yaml:"logicalPath,omitempty"`
	URL         string `json:"url,omitempty" yaml:"url,omitempty"`
	SHA256      string `json:"sha256" yaml:"sha256"`
}

type ImageLock struct {
	Logical string `json:"logical" yaml:"logical"`
	Ref     string `json:"ref" yaml:"ref"`
	Digest  string `json:"digest" yaml:"digest"`
	License string `json:"license,omitempty" yaml:"license,omitempty"`
}

// Parse unmarshals a YAML BOM document.
func Parse(raw []byte) (*Document, error) {
	var d Document
	if err := yaml.Unmarshal(raw, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

func (d *Document) Revision() string { return d.Rev }

func (d *Document) HasArtifact(name, arch string, pageSize int) bool {
	_, ok := d.GetArtifact(name, arch, pageSize)
	return ok
}

func (d *Document) GetArtifact(name, arch string, pageSize int) (sdk.BOMArtifact, bool) {
	for _, c := range d.Components {
		for _, a := range c.Artifacts {
			artName := a.Name
			if artName == "" {
				artName = c.Name
			}
			if artName != name {
				continue
			}
			if a.Arch != "" && arch != "" && a.Arch != arch {
				continue
			}
			if pageSize > 0 && len(a.PageSizes) > 0 && !containsInt(a.PageSizes, pageSize) {
				continue
			}
			return sdk.BOMArtifact{
				Name:      artName,
				Version:   c.Version,
				Arch:      a.Arch,
				PageSizes: a.PageSizes,
				Kind:      a.Kind,
				URL:       a.URL,
				SHA256:    a.SHA256,
				License:   c.License,
			}, true
		}
	}
	return sdk.BOMArtifact{}, false
}

func (d *Document) ResolveImage(logical string) (string, bool) {
	for _, img := range d.Images {
		if img.Logical == logical {
			if img.Digest != "" && !strings.Contains(img.Ref, "@sha256:") {
				return img.Ref + "@" + img.Digest, true
			}
			return img.Ref, true
		}
	}
	return "", false
}

// RestrictedLicenses are forbidden from public official offline bundles (ADR-059, D3 §6.3).
var RestrictedLicenses = map[string]bool{
	"AGPL-3.0":      true,
	"AGPL-3.0-only": true,
	"SSPL-1.0":      true,
	"Elastic-2.0":   true,
	"ELv2":          true,
	"RSALv2":        true,
	"GPL-3.0":       true,
}

// ValidatePublicBundleCompliance enforces ADR-059 red-light license blocking on public bundles.
func (d *Document) ValidatePublicBundleCompliance() error {
	for _, c := range d.Components {
		if RestrictedLicenses[c.License] {
			return sdk.NewError(sdk.CodeArtifactVerifyFailed, "ART", http.StatusUnprocessableEntity, sdk.CodeArtifactVerifyFailed, false,
				fmt.Errorf("component %s uses restricted license %s forbidden in public offline bundles (ADR-059)", c.Name, c.License))
		}
	}
	for _, img := range d.Images {
		if RestrictedLicenses[img.License] {
			return sdk.NewError(sdk.CodeArtifactVerifyFailed, "ART", http.StatusUnprocessableEntity, sdk.CodeArtifactVerifyFailed, false,
				fmt.Errorf("image %s uses restricted license %s forbidden in public offline bundles (ADR-059)", img.Ref, img.License))
		}
	}
	return nil
}

func containsInt(list []int, v int) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
