package registry

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/Masterminds/semver/v3"

	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// Registry manages built-in, declarative, and external plugins.
// Built at startup in cmd/kinstall/plugins.go and frozen read-only before serving traffic (D1 §1.2 #7).
type Registry struct {
	mu         sync.RWMutex
	frozen     bool
	components map[string]sdk.Component
	byProduct  map[string]sdk.Component
	extensions map[string][]sdk.Extension
}

// New constructs an unfrozen Registry.
func New() *Registry {
	return &Registry{
		components: make(map[string]sdk.Component),
		byProduct:  make(map[string]sdk.Component),
		extensions: make(map[string][]sdk.Extension),
	}
}

// RegisterComponent registers a Component plugin explicitly (no init()).
func (r *Registry) RegisterComponent(c sdk.Component) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.frozen {
		return fmt.Errorf("registry is frozen")
	}
	meta := c.Meta()
	if meta.SDKVersion != sdk.SDKAPIVersion {
		return sdk.NewError(sdk.CodePluginIncompatible, "PLG", http.StatusUnprocessableEntity, sdk.CodePluginIncompatible, false,
			fmt.Errorf("plugin %s targets SDK %s, platform expects %s", meta.Name, meta.SDKVersion, sdk.SDKAPIVersion))
	}
	if _, exists := r.components[meta.Name]; exists {
		return fmt.Errorf("duplicate component plugin %q", meta.Name)
	}
	r.components[meta.Name] = c
	r.byProduct[strings.ToLower(meta.Name)] = c
	for _, pr := range meta.Products {
		r.byProduct[strings.ToLower(pr.Product)] = c
	}
	return nil
}

// RegisterExtension registers an Extension plugin for a named extension point.
func (r *Registry) RegisterExtension(ext sdk.Extension) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.frozen {
		return fmt.Errorf("registry is frozen")
	}
	meta := ext.Meta()
	if meta.SDKVersion != sdk.SDKAPIVersion {
		return sdk.NewError(sdk.CodePluginIncompatible, "PLG", http.StatusUnprocessableEntity, sdk.CodePluginIncompatible, false,
			fmt.Errorf("extension %s targets SDK %s", meta.Name, meta.SDKVersion))
	}
	pt := ext.Point()
	r.extensions[pt] = append(r.extensions[pt], ext)
	return nil
}

// Freeze locks the registry against runtime mutation.
func (r *Registry) Freeze() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.frozen = true
}

// Resolve finds a Component plugin by product or plugin name and validates version & deployTarget compatibility.
func (r *Registry) Resolve(product string, target sdk.DeployTarget, version string) (sdk.Component, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.byProduct[strings.ToLower(product)]
	if !ok {
		return nil, sdk.NewError(sdk.CodePluginNotFound, "PLG", http.StatusNotFound, sdk.CodePluginNotFound, false,
			fmt.Errorf("product %q not registered", product))
	}
	meta := c.Meta()
	if len(meta.Products) == 0 {
		return c, nil
	}
	for _, pr := range meta.Products {
		if !strings.EqualFold(pr.Product, product) && !strings.EqualFold(meta.Name, product) {
			continue
		}
		if target != "" && len(pr.Targets) > 0 && !containsTarget(pr.Targets, target) {
			return nil, sdk.NewError(sdk.CodePluginIncompatible, "PLG", http.StatusUnprocessableEntity, sdk.CodePluginIncompatible, false,
				fmt.Errorf("product %s does not support target %s", product, target))
		}
		if version != "" && pr.Versions != "" {
			constraint, err := semver.NewConstraint(pr.Versions)
			if err == nil {
				sv, verr := semver.NewVersion(version)
				if verr == nil && !constraint.Check(sv) {
					return nil, sdk.NewError(sdk.CodePluginIncompatible, "PLG", http.StatusUnprocessableEntity, sdk.CodePluginIncompatible, false,
						fmt.Errorf("product %s version %s outside supported constraint %s", product, version, pr.Versions))
				}
			}
		}
		return c, nil
	}
	return c, nil
}

// ListComponents returns all registered component plugins sorted by name.
func (r *Registry) ListComponents() []sdk.PluginMeta {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]sdk.PluginMeta, 0, len(r.components))
	for _, c := range r.components {
		out = append(out, c.Meta())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Extensions returns all registered extensions for a given point (e.g. "kubernetes.cni").
func (r *Registry) Extensions(point string) []sdk.Extension {
	r.mu.RLock()
	defer r.mu.RUnlock()
	list := r.extensions[point]
	out := make([]sdk.Extension, len(list))
	copy(out, list)
	return out
}

func containsTarget(list []sdk.DeployTarget, t sdk.DeployTarget) bool {
	for _, item := range list {
		if item == t {
			return true
		}
	}
	return false
}
