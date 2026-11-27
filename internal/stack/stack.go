package stack

import (
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"sigs.k8s.io/yaml"

	"github.com/kinstall/kinstall/internal/registry"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// SpecDoc models a declarative kinstall.io/v1alpha1 Stack manifest (D2 §4.1, ADR-067).
type SpecDoc struct {
	APIVersion string    `json:"apiVersion" yaml:"apiVersion"`
	Kind       string    `json:"kind" yaml:"kind"`
	Metadata   Metadata  `json:"metadata" yaml:"metadata"`
	Spec       StackBody `json:"spec" yaml:"spec"`
}

type Metadata struct {
	Name    string `json:"name" yaml:"name"`
	Project string `json:"project" yaml:"project"`
	Env     string `json:"env" yaml:"env"`
}

type StackBody struct {
	ExternalRefs []ExternalRef  `json:"externalRefs,omitempty" yaml:"externalRefs,omitempty"`
	Instances    []InstanceNode `json:"instances" yaml:"instances"`
}

// ExternalRef declares a read-only external/cloud managed service (OBS, DDS, RDS) NEVER modified on uninstall (D2 §4.2).
type ExternalRef struct {
	ID            string   `json:"id" yaml:"id"`
	Kind          string   `json:"kind" yaml:"kind"`
	Endpoints     []string `json:"endpoints" yaml:"endpoints"`
	CredentialRef string   `json:"credentialRef,omitempty" yaml:"credentialRef,omitempty"`
	TLSCA         string   `json:"tlsCA,omitempty" yaml:"tlsCA,omitempty"`
}

type InstanceNode struct {
	ID             string           `json:"id" yaml:"id"`
	Product        string           `json:"product" yaml:"product"`
	ProductVersion string           `json:"productVersion,omitempty" yaml:"productVersion,omitempty"`
	DeployTarget   sdk.DeployTarget `json:"deployTarget" yaml:"deployTarget"`
	DependsOn      []string         `json:"dependsOn,omitempty" yaml:"dependsOn,omitempty"`
	Spec           map[string]any   `json:"spec" yaml:"spec"`
}

// ResolvedDAG contains topologically layered install order and reverse uninstall order.
type ResolvedDAG struct {
	InstallLayers   [][]string          `json:"installLayers"`
	UninstallLayers [][]string          `json:"uninstallLayers"`
	Edges           map[string][]string `json:"edges"`
	ExternalRefIDs  []string            `json:"externalRefIds,omitempty"` // read-only; never included in UninstallLayers!
}

var (
	reInstanceRef = regexp.MustCompile(`\$\{instances\.([a-zA-Z0-9_-]+)\.outputs\.([a-zA-Z0-9_.-]+)\}`)
	reExternalRef = regexp.MustCompile(`\$\{externalRefs\.([a-zA-Z0-9_-]+)\.([a-zA-Z0-9_.-]+)\}`)
)

// Parse unmarshals a Stack YAML specification.
func Parse(raw []byte) (*SpecDoc, error) {
	var doc SpecDoc
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, sdk.NewError(sdk.CodeAPIInvalidRequest, "API", http.StatusUnprocessableEntity, sdk.CodeAPIInvalidRequest, false, err)
	}
	if doc.Kind != "Stack" {
		return nil, sdk.NewError(sdk.CodeAPIInvalidRequest, "API", http.StatusUnprocessableEntity, sdk.CodeAPIInvalidRequest, false, fmt.Errorf("expected kind=Stack, got %q", doc.Kind))
	}
	return &doc, nil
}

// ResolveAndValidate runs Kahn's topological sort for cycle detection (KI-STACK-0001) and statically validates
// all ${instances.<id>.outputs.<key>} & ${externalRefs.<id>.<field>} references before any SSH call (D2 §4.2, T-M4-05).
func ResolveAndValidate(doc *SpecDoc, reg *registry.Registry, credentialExists func(credID string) bool) (*ResolvedDAG, error) {
	nodes := make(map[string]InstanceNode, len(doc.Spec.Instances))
	inDegree := make(map[string]int, len(doc.Spec.Instances))
	adj := make(map[string][]string, len(doc.Spec.Instances))
	edges := make(map[string][]string, len(doc.Spec.Instances))

	extMap := make(map[string]ExternalRef, len(doc.Spec.ExternalRefs))
	var extIDs []string
	for _, er := range doc.Spec.ExternalRefs {
		extMap[er.ID] = er
		extIDs = append(extIDs, er.ID)
		if er.CredentialRef != "" && credentialExists != nil && !credentialExists(er.CredentialRef) {
			return nil, sdk.NewError(sdk.CodeStackOutputRefInvalid, "STACK", http.StatusUnprocessableEntity, sdk.CodeStackOutputRefInvalid, false,
				fmt.Errorf("externalRef %s references missing credential %s", er.ID, er.CredentialRef))
		}
	}
	sort.Strings(extIDs)

	for _, inst := range doc.Spec.Instances {
		if _, dup := nodes[inst.ID]; dup {
			return nil, sdk.NewError(sdk.CodeAPIInvalidRequest, "API", http.StatusUnprocessableEntity, sdk.CodeAPIInvalidRequest, false,
				fmt.Errorf("duplicate instance id %q in stack", inst.ID))
		}
		nodes[inst.ID] = inst
		inDegree[inst.ID] = 0
	}

	for _, inst := range doc.Spec.Instances {
		for _, dep := range inst.DependsOn {
			if _, ok := nodes[dep]; !ok {
				return nil, sdk.NewError(sdk.CodeStackOutputRefInvalid, "STACK", http.StatusUnprocessableEntity, sdk.CodeStackOutputRefInvalid, false,
					fmt.Errorf("instance %s dependsOn unknown instance %s", inst.ID, dep))
			}
			adj[dep] = append(adj[dep], inst.ID)
			edges[inst.ID] = append(edges[inst.ID], dep)
			inDegree[inst.ID]++
		}
	}

	// Kahn's topological sort grouped into parallel execution layers
	var layers [][]string
	visited := 0
	var queue []string
	for id, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, id)
		}
	}
	sort.Strings(queue)

	for len(queue) > 0 {
		sort.Strings(queue)
		layers = append(layers, queue)
		visited += len(queue)
		var next []string
		for _, u := range queue {
			for _, v := range adj[u] {
				inDegree[v]--
				if inDegree[v] == 0 {
					next = append(next, v)
				}
			}
		}
		queue = next
	}

	if visited != len(nodes) {
		return nil, sdk.NewError(sdk.CodeStackCycleDetected, "STACK", http.StatusUnprocessableEntity, sdk.CodeStackCycleDetected, false,
			fmt.Errorf("cycle detected in stack %q dependency graph", doc.Metadata.Name))
	}

	// Compute transitive dependsOn closure for each node & validate all ${instances.X.outputs.Y} and ${externalRefs.X.Y}
	closures := computeClosures(edges)
	for _, inst := range doc.Spec.Instances {
		if err := validateNodeRefs(inst, nodes, closures[inst.ID], extMap, reg); err != nil {
			return nil, err
		}
	}

	// Build strict reverse-topological UninstallLayers (NEVER includes externalRefs!)
	uninstall := make([][]string, len(layers))
	for i := range layers {
		cp := append([]string(nil), layers[len(layers)-1-i]...)
		uninstall[i] = cp
	}

	return &ResolvedDAG{
		InstallLayers:   layers,
		UninstallLayers: uninstall,
		Edges:           edges,
		ExternalRefIDs:  extIDs,
	}, nil
}

// ValidateSafeUninstall blocks destroying an instance if any external stack/instance still depends on it (KI-STACK-0002).
func ValidateSafeUninstall(instanceID string, activeReferrers []string) error {
	if len(activeReferrers) > 0 {
		return sdk.NewError(sdk.CodeStackReferencedInUse, "STACK", http.StatusConflict, sdk.CodeStackReferencedInUse, false,
			fmt.Errorf("instance %s is still referenced by: %s", instanceID, strings.Join(activeReferrers, ", ")))
	}
	return nil
}

func computeClosures(edges map[string][]string) map[string]map[string]bool {
	out := make(map[string]map[string]bool)
	var dfs func(id string, acc map[string]bool)
	dfs = func(id string, acc map[string]bool) {
		for _, dep := range edges[id] {
			if !acc[dep] {
				acc[dep] = true
				dfs(dep, acc)
			}
		}
	}
	for id := range edges {
		acc := make(map[string]bool)
		dfs(id, acc)
		out[id] = acc
	}
	return out
}

func validateNodeRefs(inst InstanceNode, nodes map[string]InstanceNode, depClosure map[string]bool, extMap map[string]ExternalRef, reg *registry.Registry) error {
	var stringsToScan []string
	collectStrings(inst.Spec, &stringsToScan)
	for _, s := range stringsToScan {
		for _, m := range reInstanceRef.FindAllStringSubmatch(s, -1) {
			targetID, outKey := m[1], m[2]
			if !depClosure[targetID] {
				return sdk.NewError(sdk.CodeStackOutputRefInvalid, "STACK", http.StatusUnprocessableEntity, sdk.CodeStackOutputRefInvalid, false,
					fmt.Errorf("instance %s references outputs of %s which is not in its dependsOn closure", inst.ID, targetID))
			}
			if reg != nil {
				upstream := nodes[targetID]
				if comp, err := reg.Resolve(upstream.Product, "", ""); err == nil {
					schema := comp.Meta().OutputSchema
					topKey := strings.SplitN(outKey, ".", 2)[0]
					if len(schema) > 0 && schema[outKey] == "" && schema[topKey] == "" {
						return sdk.NewError(sdk.CodeStackOutputRefInvalid, "STACK", http.StatusUnprocessableEntity, sdk.CodeStackOutputRefInvalid, false,
							fmt.Errorf("instance %s references undeclared output key %q on upstream product %s", inst.ID, outKey, upstream.Product))
					}
				}
			}
		}
		for _, m := range reExternalRef.FindAllStringSubmatch(s, -1) {
			extID := m[1]
			if _, ok := extMap[extID]; !ok {
				return sdk.NewError(sdk.CodeStackOutputRefInvalid, "STACK", http.StatusUnprocessableEntity, sdk.CodeStackOutputRefInvalid, false,
					fmt.Errorf("instance %s references undeclared externalRef %q", inst.ID, extID))
			}
		}
	}
	return nil
}

func collectStrings(v any, out *[]string) {
	switch x := v.(type) {
	case string:
		*out = append(*out, x)
	case map[string]any:
		for _, val := range x {
			collectStrings(val, out)
		}
	case []any:
		for _, val := range x {
			collectStrings(val, out)
		}
	}
}

// InterpolateSpec resolves ${instances.<id>.outputs.<key>} and ${externalRefs.<id>.<field>} placeholders
// recursively in a copy of spec using accumulated upstream outputs and read-only externalRefs.
func InterpolateSpec(spec map[string]any, upstreamOutputs map[string]map[string]any, externalRefs []ExternalRef) map[string]any {
	if len(spec) == 0 {
		return map[string]any{}
	}
	extMap := make(map[string]ExternalRef, len(externalRefs))
	for _, er := range externalRefs {
		extMap[er.ID] = er
	}
	res := interpolateValue(spec, upstreamOutputs, extMap)
	if m, ok := res.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func interpolateValue(v any, upstreamOutputs map[string]map[string]any, extMap map[string]ExternalRef) any {
	switch x := v.(type) {
	case string:
		out := reInstanceRef.ReplaceAllStringFunc(x, func(match string) string {
			sub := reInstanceRef.FindStringSubmatch(match)
			if len(sub) != 3 {
				return match
			}
			outs, ok := upstreamOutputs[sub[1]]
			if !ok {
				return match
			}
			if val, exists := outs[sub[2]]; exists && val != nil {
				return fmt.Sprint(val)
			}
			topKey := strings.SplitN(sub[2], ".", 2)[0]
			if val, exists := outs[topKey]; exists && val != nil {
				return fmt.Sprint(val)
			}
			return match
		})
		out = reExternalRef.ReplaceAllStringFunc(out, func(match string) string {
			sub := reExternalRef.FindStringSubmatch(match)
			if len(sub) != 3 {
				return match
			}
			er, ok := extMap[sub[1]]
			if !ok {
				return match
			}
			switch sub[2] {
			case "endpoint":
				if len(er.Endpoints) > 0 {
					return er.Endpoints[0]
				}
			case "endpoints":
				return strings.Join(er.Endpoints, ",")
			case "credentialRef":
				return er.CredentialRef
			case "tlsCA":
				return er.TLSCA
			case "kind":
				return er.Kind
			}
			return match
		})
		return out
	case map[string]any:
		cp := make(map[string]any, len(x))
		for k, val := range x {
			cp[k] = interpolateValue(val, upstreamOutputs, extMap)
		}
		return cp
	case []any:
		cp := make([]any, len(x))
		for i, val := range x {
			cp[i] = interpolateValue(val, upstreamOutputs, extMap)
		}
		return cp
	default:
		return v
	}
}
