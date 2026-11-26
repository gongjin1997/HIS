package gitops

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"sigs.k8s.io/yaml"

	"github.com/kinstall/kinstall/internal/domain"
)

// Canonical GitOps CI exit codes (T-M3-11, G2 §2).
const (
	ExitNoChanges       = 0
	ExitChangesPlanned  = 2
	ExitDriftBlocked    = 3
	ExitApprovalWaiting = 4
)

// ProjectBundle is the deterministic GitOps YAML export format for a project's desired state.
type ProjectBundle struct {
	APIVersion string             `json:"apiVersion"`
	Kind       string             `json:"kind"`
	Project    string             `json:"project"`
	Instances  []InstanceManifest `json:"instances"`
}

type InstanceManifest struct {
	Name           string          `json:"name"`
	Env            string          `json:"env"`
	Product        string          `json:"product"`
	ProductVersion string          `json:"productVersion"`
	DeployTarget   string          `json:"deployTarget"`
	Spec           json.RawMessage `json:"spec"`
}

// DiffEntry summarizes a GitOps desired-vs-live drift entry.
type DiffEntry struct {
	InstanceName string `json:"instanceName"`
	Action       string `json:"action"` // create | update | delete | unchanged
	BeforeHash   string `json:"beforeHash,omitempty"`
	AfterHash    string `json:"afterHash,omitempty"`
}

// ExportYAML serializes live instances deterministically into a GitOps YAML bundle.
func ExportYAML(projectName string, insts []domain.Instance) ([]byte, error) {
	sorted := append([]domain.Instance(nil), insts...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	bundle := ProjectBundle{
		APIVersion: "kinstall.io/v1alpha1",
		Kind:       "ProjectBundle",
		Project:    projectName,
		Instances:  make([]InstanceManifest, 0, len(sorted)),
	}
	for _, it := range sorted {
		bundle.Instances = append(bundle.Instances, InstanceManifest{
			Name:           it.Name,
			Env:            it.Env,
			Product:        it.Product,
			ProductVersion: it.ProductVersion,
			DeployTarget:   string(it.DeployTarget),
			Spec:           it.DesiredSpecJSON,
		})
	}
	return yaml.Marshal(bundle)
}

// Diff compares a desired GitOps YAML bundle against current live instances and returns deterministic diff + CI exit code.
func Diff(desiredYAML []byte, live []domain.Instance) ([]DiffEntry, int, error) {
	var desired ProjectBundle
	if err := yaml.Unmarshal(desiredYAML, &desired); err != nil {
		return nil, 1, fmt.Errorf("parse gitops bundle: %w", err)
	}
	liveMap := make(map[string]domain.Instance, len(live))
	for _, l := range live {
		liveMap[l.Name] = l
	}
	seen := make(map[string]bool, len(desired.Instances))
	var entries []DiffEntry
	hasChanges := false

	for _, d := range desired.Instances {
		seen[d.Name] = true
		afterH := manifestHash(d.Product, d.ProductVersion, d.DeployTarget, d.Spec)
		l, ok := liveMap[d.Name]
		if !ok {
			hasChanges = true
			entries = append(entries, DiffEntry{InstanceName: d.Name, Action: "create", AfterHash: afterH})
			continue
		}
		beforeH := manifestHash(l.Product, l.ProductVersion, string(l.DeployTarget), l.DesiredSpecJSON)
		if beforeH == afterH {
			entries = append(entries, DiffEntry{InstanceName: d.Name, Action: "unchanged", BeforeHash: beforeH, AfterHash: afterH})
		} else {
			hasChanges = true
			entries = append(entries, DiffEntry{InstanceName: d.Name, Action: "update", BeforeHash: beforeH, AfterHash: afterH})
		}
	}
	for _, l := range live {
		if !seen[l.Name] {
			hasChanges = true
			entries = append(entries, DiffEntry{
				InstanceName: l.Name,
				Action:       "delete",
				BeforeHash:   manifestHash(l.Product, l.ProductVersion, string(l.DeployTarget), l.DesiredSpecJSON),
			})
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].InstanceName < entries[j].InstanceName })
	if hasChanges {
		return entries, ExitChangesPlanned, nil
	}
	return entries, ExitNoChanges, nil
}

func manifestHash(product, ver, target string, spec []byte) string {
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "%s|%s|%s|", product, ver, target)
	var canonical any
	if len(spec) > 0 && json.Unmarshal(spec, &canonical) == nil {
		if norm, err := json.Marshal(canonical); err == nil {
			spec = norm
		}
	}
	_, _ = h.Write(spec)
	return hex.EncodeToString(h.Sum(nil))[:16]
}
